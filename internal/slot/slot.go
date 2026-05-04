package slot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/MertDalbudak/mcsm/internal/config"
	"github.com/MertDalbudak/mcsm/internal/discord"
	"github.com/MertDalbudak/mcsm/internal/discovery"
	"github.com/MertDalbudak/mcsm/internal/events"
	"github.com/MertDalbudak/mcsm/internal/gameplay"
	"github.com/MertDalbudak/mcsm/internal/lock"
	"github.com/MertDalbudak/mcsm/internal/logtail"
	"github.com/MertDalbudak/mcsm/internal/process"
	"github.com/MertDalbudak/mcsm/internal/rcon"
	"github.com/MertDalbudak/mcsm/internal/serverid"
	"github.com/MertDalbudak/mcsm/internal/slp"
)

// rconLocalPort is the loopback port mcsm assigns to RCON. Computed as
// slot.port + rconPortOffset so two slots on the same host don't collide.
// Bound to 127.0.0.1 only — never exposed externally.
const rconPortOffset = 10000

// healthProbeInterval is how often we SLP-poll a "starting" or "running"
// server to detect liveness and player counts.
const healthProbeInterval = 5 * time.Second

// healthProbeTimeout bounds each individual SLP probe.
const healthProbeTimeout = 2 * time.Second

// startupBudget caps how long we wait for a starting server to become
// SLP-healthy before declaring crash.
const startupBudget = 5 * time.Minute

// Errors returned by Slot operations. Match docs/api.md error codes
// where possible; the API layer converts these to HTTP status + envelope.
var (
	ErrSlotBusy           = errors.New("slot is not in a startable state")
	ErrServerNotMounted   = errors.New("no server is mounted in this slot")
	ErrServerNotRunning   = errors.New("server is not running")
	ErrServerIncompatible = errors.New("server does not satisfy slot constraints")
	ErrNotStopping        = errors.New("slot is not currently stopping")
)

// Slot is one configured slot. Methods are goroutine-safe.
type Slot struct {
	cfg          config.Slot
	instanceName string
	host         string
	disco        *discovery.Store

	mu         sync.RWMutex
	state      State
	stateSince time.Time
	lastErr    *LastError

	// Filled in while a server is mounted:
	server      *discovery.Server
	process     *process.Process
	lock        *lock.Held
	rconClient  *rcon.Client
	rconAddr    string // host:port
	rconPass    string // session password (regenerated each start)
	startedAt   time.Time
	mountedID   string
	cancelCtx   context.CancelFunc
	slp         *SLPInfo
	tailer      *logtail.Tailer
	bus         *events.Bus // always non-nil, even when idle (created in New)
	bot         *discord.Bot
	tempProvider DiscordTempProvider // optional; injected via SetTempProvider

	// Stop coordination:
	stopOnce sync.Once
	stopping bool
	stopAbort chan struct{} // closed by AbortStop() to unblock the grace timer
}

// New constructs an idle Slot bound to the supervising instance.
func New(cfg config.Slot, instanceName, host string, disco *discovery.Store) *Slot {
	return &Slot{
		cfg:          cfg,
		instanceName: instanceName,
		host:         host,
		disco:        disco,
		state:        StateIdle,
		stateSince:   time.Now().UTC(),
		bus:          events.NewBus(),
	}
}

// Events returns the slot's event bus. Subscribers receive state
// transitions and gameplay events for as long as the slot is alive
// (the bus survives mount/unmount cycles).
func (s *Slot) Events() *events.Bus { return s.bus }

// Tailer returns the active log tailer, or nil when the slot is idle.
// Used by handlers that want to subscribe to live log streams.
func (s *Slot) Tailer() *logtail.Tailer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tailer
}

// Name returns the slot's configured name.
func (s *Slot) Name() string { return s.cfg.Name }

// Snapshot returns a serializable view of the slot's current state.
func (s *Slot) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{
		Name:          s.cfg.Name,
		Port:          s.cfg.Port,
		PublicAddress: s.cfg.PublicAddress,
		State:         s.state,
		StateSince:    s.stateSince,
		AutoMount:     s.cfg.AutoMount,
		LastError:     s.lastErr,
	}
	if s.server != nil {
		snap.MountedServerID = s.server.ID
		snap.MountedServer = &Mounted{
			ID:            s.server.ID,
			Name:          s.server.Name,
			Type:          s.server.Type,
			Version:       s.server.Version,
			Path:          s.server.Path,
			StartedAt:     s.startedAt,
			RconConnected: s.rconClient != nil,
			SLP:           s.slp,
		}
		if s.process != nil {
			snap.MountedServer.PID = s.process.PID()
		}
	}
	return snap
}

// State returns the current state without taking other locks.
func (s *Slot) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// transition is the one-stop place to update state + state_since +
// optionally lastErr. Holds the write lock for the duration.
func (s *Slot) transition(to State, lastErr *LastError) {
	s.mu.Lock()
	from := s.state
	s.state = to
	s.stateSince = time.Now().UTC()
	if lastErr != nil {
		s.lastErr = lastErr
	} else if to == StateIdle {
		s.lastErr = nil
	}
	s.mu.Unlock()
	slog.Info("slot: state transition",
		"slot", s.cfg.Name,
		"from", from,
		"to", to,
	)
	s.bus.Publish(events.Event{
		Type: events.TypeState,
		From: string(from),
		To:   string(to),
	})
}

// StartOptions controls a Start call.
type StartOptions struct {
	ServerID string
	Force    bool // steal stale lock
}

// Start mounts the requested server in this slot and spawns the JVM.
// Returns immediately after the lock is acquired and process is spawned;
// the slot transitions through starting → running asynchronously.
func (s *Slot) Start(ctx context.Context, opts StartOptions) (Snapshot, error) {
	s.mu.Lock()
	if !s.state.CanStartFrom() {
		cur := s.state
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("%w: current state is %s", ErrSlotBusy, cur)
	}
	s.state = StateMounting
	s.stateSince = time.Now().UTC()
	s.lastErr = nil
	s.stopping = false
	s.stopOnce = sync.Once{}
	s.mu.Unlock()

	srv := s.disco.Snapshot().FindByID(opts.ServerID)
	if srv == nil {
		// Try a refresh in case the catalog is stale.
		fresh, _ := s.disco.Refresh(ctx)
		if fresh != nil {
			srv = fresh.FindByID(opts.ServerID)
		}
	}
	if srv == nil {
		s.failMount("server_not_found", fmt.Sprintf("no server with id %s", opts.ServerID))
		return s.Snapshot(), fmt.Errorf("server_not_found: %s", opts.ServerID)
	}

	if err := s.checkAccepts(srv); err != nil {
		s.failMount("server_incompatible", err.Error())
		return s.Snapshot(), fmt.Errorf("%w: %v", ErrServerIncompatible, err)
	}

	cfg, err := serverid.Read(srv.Path)
	if err != nil {
		s.failMount("config_error", err.Error())
		return s.Snapshot(), fmt.Errorf("read server config: %w", err)
	}

	// Regenerate RCON password every mount so leaks don't persist.
	pass, err := rcon.GenPassword()
	if err != nil {
		s.failMount("internal", err.Error())
		return s.Snapshot(), fmt.Errorf("rcon password: %w", err)
	}
	rconPort := s.cfg.Port + rconPortOffset

	// Patch server.properties with mcsm-managed values. We do this even
	// if the user has set them — slot port is authoritative.
	patch := map[string]string{
		"server-port":   strconv.Itoa(s.cfg.Port),
		"query.port":    strconv.Itoa(s.cfg.Port),
		"enable-query":  "true",
		"enable-rcon":   "true",
		"rcon.port":     strconv.Itoa(rconPort),
		"rcon.password": pass,
		// Force RCON to bind loopback only. Older versions ignore this;
		// newer Paper honors it.
		"broadcast-rcon-to-ops": "false",
	}
	if err := serverid.PatchProperties(srv.Path, patch); err != nil {
		s.failMount("properties_error", err.Error())
		return s.Snapshot(), fmt.Errorf("patch properties: %w", err)
	}

	// Acquire the cross-instance lock.
	held, err := lock.TryAcquire(context.Background(), srv.Path,
		s.instanceName, s.cfg.Name, s.host, os.Getpid(), opts.Force)
	if err != nil {
		s.failMount("server_in_use", err.Error())
		return s.Snapshot(), err
	}

	plan, err := serverid.PlanLaunch(cfg, srv.Path)
	if err != nil {
		_ = held.Release()
		s.failMount("launch_plan", err.Error())
		return s.Snapshot(), fmt.Errorf("plan launch: %w", err)
	}

	proc, err := process.Start(process.Spec{
		Dir:     plan.Dir,
		Command: plan.Command,
		Args:    plan.Args,
	})
	if err != nil {
		_ = held.Release()
		s.failMount("spawn", err.Error())
		return s.Snapshot(), fmt.Errorf("spawn java: %w", err)
	}

	bgCtx, cancel := context.WithCancel(context.Background())
	tailer := logtail.NewTailer(filepath.Join(srv.Path, "logs", "latest.log"), 5000)

	s.mu.Lock()
	s.server = srv
	s.process = proc
	s.lock = held
	s.rconAddr = fmt.Sprintf("127.0.0.1:%d", rconPort)
	s.rconPass = pass
	s.startedAt = time.Now().UTC()
	s.mountedID = srv.ID
	s.cancelCtx = cancel
	s.state = StateStarting
	s.stateSince = time.Now().UTC()
	s.slp = nil
	s.tailer = tailer
	s.mu.Unlock()

	go s.watchExit()
	go s.healthLoop(bgCtx)
	go s.rconConnectLoop(bgCtx, rconPort, pass)
	go tailer.Run(bgCtx)
	go s.detectGameplayEvents(bgCtx, tailer, cfg)

	if cfg.Discord.Token != "" && len(cfg.Discord.Channels) > 0 {
		go s.startDiscordBot(bgCtx, cfg)
	}

	slog.Info("slot: mounted",
		"slot", s.cfg.Name,
		"server_id", srv.ID,
		"path", srv.Path,
		"pid", proc.PID(),
		"rcon_port", rconPort,
	)
	return s.Snapshot(), nil
}

// failMount cleans up after a mount that failed before java started.
func (s *Slot) failMount(code, msg string) {
	s.mu.Lock()
	s.state = StateError
	s.stateSince = time.Now().UTC()
	s.lastErr = &LastError{Code: code, Message: msg, At: time.Now().UTC()}
	s.server = nil
	s.process = nil
	s.lock = nil
	s.rconClient = nil
	s.mu.Unlock()
	slog.Warn("slot: mount failed", "slot", s.cfg.Name, "code", code, "err", msg)
}

func (s *Slot) checkAccepts(srv *discovery.Server) error {
	a := s.cfg.Accepts
	if len(a.Types) > 0 {
		ok := false
		for _, t := range a.Types {
			if t == srv.Type {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("type %q not in slot accepts.types %v", srv.Type, a.Types)
		}
	}
	// max_memory_mb is checked against -Xmx if present in java.args.
	// Best-effort parse; anything unparseable is allowed through.
	if a.MaxMemoryMB > 0 {
		cfg, err := serverid.Read(srv.Path)
		if err == nil {
			if mb := parseXmxMB(cfg.Java.Args); mb > 0 && mb > a.MaxMemoryMB {
				return fmt.Errorf("server -Xmx %d MB exceeds slot max %d MB", mb, a.MaxMemoryMB)
			}
		}
	}
	return nil
}

// StopOptions controls a Stop call.
type StopOptions struct {
	GracefulSeconds   int
	BroadcastEvery    int
	BroadcastTemplate string
	KillGrace         time.Duration
}

// Stop initiates graceful shutdown. Returns once the stop is *initiated*
// (state → stopping); actual exit happens asynchronously.
func (s *Slot) Stop(ctx context.Context, opts StopOptions) (Snapshot, error) {
	s.mu.RLock()
	cur := s.state
	s.mu.RUnlock()
	if !cur.CanStopFrom() {
		return s.Snapshot(), fmt.Errorf("%w: current state is %s", ErrSlotBusy, cur)
	}
	if opts.GracefulSeconds <= 0 {
		opts.GracefulSeconds = 30
	}
	if opts.KillGrace <= 0 {
		opts.KillGrace = 10 * time.Second
	}

	s.transition(StateStopping, nil)
	s.mu.Lock()
	s.stopping = true
	s.stopAbort = make(chan struct{})
	abort := s.stopAbort
	proc := s.process
	rc := s.rconClient
	s.mu.Unlock()

	go func() {
		// Best-effort RCON "stop" to trigger save+shutdown.
		if rc != nil {
			cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = rc.Cmd(cctx, "stop")
			cancel()
		}
		grace := time.Duration(opts.GracefulSeconds) * time.Second
		if proc == nil {
			return
		}
		select {
		case <-proc.ExitChannel():
			// Clean exit before grace expired.
		case <-abort:
			// Operator called AbortStop. Don't escalate; revert to running
			// (the process is presumed still alive — RCON stop may not
			// have shut things down on every flavor).
			slog.Info("slot: stop aborted by operator", "slot", s.cfg.Name)
			s.mu.Lock()
			if s.state == StateStopping {
				s.stopping = false
				s.stopAbort = nil
			}
			s.mu.Unlock()
			s.transition(StateRunning, nil)
			return
		case <-time.After(grace):
			slog.Warn("slot: graceful timeout, escalating", "slot", s.cfg.Name)
			tctx, cancel := context.WithTimeout(context.Background(), opts.KillGrace+10*time.Second)
			_ = proc.Terminate(tctx, opts.KillGrace)
			cancel()
		}
	}()
	return s.Snapshot(), nil
}

// AbortStop cancels an in-progress graceful shutdown if the grace timer
// hasn't fired yet. Returns ErrNotStopping if the slot isn't stopping.
//
// Note: if the RCON "stop" command already convinced Minecraft to shut
// down, the process will exit anyway and the abort is a no-op (the
// watchExit goroutine wins the race). This call only short-circuits the
// SIGTERM/SIGKILL escalation that mcsm would otherwise drive.
func (s *Slot) AbortStop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateStopping || s.stopAbort == nil {
		return ErrNotStopping
	}
	close(s.stopAbort)
	s.stopAbort = nil
	return nil
}

// Restart is Stop then Start with the same server id once the slot
// reaches idle/crashed.
func (s *Slot) Restart(ctx context.Context, opts StopOptions) (Snapshot, error) {
	s.mu.RLock()
	id := s.mountedID
	s.mu.RUnlock()
	if id == "" {
		return s.Snapshot(), ErrServerNotMounted
	}
	if _, err := s.Stop(ctx, opts); err != nil {
		return s.Snapshot(), err
	}
	// Wait for the slot to reach a startable state, then Start.
	go func() {
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			if s.State().CanStartFrom() {
				if _, err := s.Start(context.Background(), StartOptions{ServerID: id}); err != nil {
					slog.Error("slot: restart Start failed", "slot", s.cfg.Name, "err", err)
				}
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		slog.Error("slot: restart never reached startable state", "slot", s.cfg.Name)
	}()
	return s.Snapshot(), nil
}

// SaveControlForBackup returns a backup.SaveController that flushes the
// world via RCON. Only works while the slot is running and RCON is
// connected; returns nil otherwise (the backup package treats nil as
// "skip pause/resume", which is correct for offline backups).
func (s *Slot) SaveControlForBackup() *RconSaveControl {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state != StateRunning || s.rconClient == nil {
		return nil
	}
	return &RconSaveControl{rc: s.rconClient}
}

// RconSaveControl implements backup.SaveController on top of RCON.
type RconSaveControl struct {
	rc *rcon.Client
}

// Pause: save-off then save-all flush.
func (c *RconSaveControl) Pause(ctx context.Context) error {
	if _, err := c.rc.Cmd(ctx, "save-off"); err != nil {
		return err
	}
	_, err := c.rc.Cmd(ctx, "save-all flush")
	return err
}

// Resume: save-on so autosaves continue.
func (c *RconSaveControl) Resume(ctx context.Context) error {
	_, err := c.rc.Cmd(ctx, "save-on")
	return err
}

// Logs returns up to n most recent captured log lines (oldest first).
// Empty when nothing is mounted.
func (s *Slot) Logs(n int) []process.LogLine {
	s.mu.RLock()
	p := s.process
	s.mu.RUnlock()
	if p == nil {
		return nil
	}
	return p.Logs(n)
}

// MountedServer returns a snapshot of the discovered.Server currently
// mounted (nil when idle). Returned value is a copy; safe to read without
// further locking.
func (s *Slot) MountedServer() *discovery.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.server == nil {
		return nil
	}
	srv := *s.server
	return &srv
}

// Command sends a raw RCON command to the mounted server.
func (s *Slot) Command(ctx context.Context, cmd string) (string, error) {
	s.mu.RLock()
	rc := s.rconClient
	state := s.state
	s.mu.RUnlock()
	if state != StateRunning {
		return "", ErrServerNotRunning
	}
	if rc == nil {
		return "", fmt.Errorf("rcon: not connected")
	}
	return rc.Cmd(ctx, cmd)
}

// detectGameplayEvents subscribes to the tailer and emits typed events
// on the slot bus: joins, leaves, deaths, chat, kicks. Optional
// behaviours (anti-toxicity, ban-flying) are driven by feature flags
// loaded from the per-server .mcsm/config.yaml.
//
// We only look at INFO entries from "Server thread" to avoid being
// fooled by player chat that happens to contain "joined the game".
func (s *Slot) detectGameplayEvents(ctx context.Context, t *logtail.Tailer, cfg *serverid.Config) {
	sub := t.Subscribe(64)
	defer t.Unsubscribe(sub)

	tox := buildToxicityChecker(cfg)
	banFlying := featureBool(cfg.Features, "ban_flying", false)
	emitDeath := featureBool(cfg.Features, "death_messages", true)
	emitChat := !tox.Empty() // we only emit chat events when there's a consumer

	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-sub:
			if !ok {
				return
			}
			if e.Level != "INFO" || e.Thread != "Server thread" {
				continue
			}
			msg := e.Message

			// joins / leaves
			switch {
			case len(msg) > 17 && hasSuffix(msg, " joined the game"):
				s.bus.Publish(events.Event{
					Type: events.TypePlayerJoin, At: e.TS,
					Player: msg[:len(msg)-len(" joined the game")],
				})
				continue
			case len(msg) > 15 && hasSuffix(msg, " left the game"):
				s.bus.Publish(events.Event{
					Type: events.TypePlayerLeave, At: e.TS,
					Player: msg[:len(msg)-len(" left the game")],
				})
				continue
			}

			// chat → optional toxicity action
			if player, text, ok := gameplay.ChatMessage(msg); ok {
				if emitChat {
					s.bus.Publish(events.Event{
						Type: events.TypeChat, At: e.TS,
						Player: player, Message: text,
					})
				}
				if !tox.Empty() && tox.Match(text) {
					s.runToxicityAction(player)
				}
				continue
			}

			// flying-kick → optional auto-ban
			if banFlying {
				if player, ok := gameplay.FlyingKick(msg); ok {
					s.bus.Publish(events.Event{
						Type: events.TypePlayerKick, At: e.TS,
						Player: player, Reason: "flying",
					})
					s.runFlyingBan(player)
					continue
				}
			}

			// deaths
			if emitDeath {
				if d, ok := gameplay.ParseDeath(msg); ok {
					s.bus.Publish(events.Event{
						Type:    events.TypePlayerDeath, At: e.TS,
						Player:  d.Player,
						Killer:  d.Killer,
						Cause:   string(d.Cause),
						Message: msg,
					})
					s.notifyDiscord("💀 " + msg)
					continue
				}
			}
		}
	}
}

// runToxicityAction issues an in-game warning. Future versions may
// kick/mute/escalate; for now we just say-warn the offender.
func (s *Slot) runToxicityAction(player string) {
	rc := s.currentRcon()
	if rc == nil {
		return
	}
	rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = rc.Cmd(rctx, fmt.Sprintf("tellraw %s {\"text\":\"[mcsm] Watch your language.\",\"color\":\"red\"}", player))
}

// runFlyingBan issues a 24-hour ban via RCON.
func (s *Slot) runFlyingBan(player string) {
	rc := s.currentRcon()
	if rc == nil {
		return
	}
	rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = rc.Cmd(rctx, fmt.Sprintf("ban %s Flying not allowed", player))
}

func (s *Slot) currentRcon() *rcon.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rconClient
}

// buildToxicityChecker converts the per-server features map into a
// concrete checker. Words live under features.anti_toxicity_words.
func buildToxicityChecker(cfg *serverid.Config) *gameplay.ToxicityChecker {
	if cfg == nil || cfg.Features == nil {
		return gameplay.NewToxicityChecker(nil)
	}
	raw, ok := cfg.Features["anti_toxicity_words"]
	if !ok {
		return gameplay.NewToxicityChecker(nil)
	}
	switch v := raw.(type) {
	case []string:
		return gameplay.NewToxicityChecker(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, w := range v {
			if s, ok := w.(string); ok {
				out = append(out, s)
			}
		}
		return gameplay.NewToxicityChecker(out)
	}
	return gameplay.NewToxicityChecker(nil)
}

// featureBool reads a boolean feature flag with a default. Accepts
// raw bool, "true"/"false" strings.
func featureBool(features map[string]any, key string, def bool) bool {
	if features == nil {
		return def
	}
	raw, ok := features[key]
	if !ok {
		return def
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return def
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// watchExit blocks on the process exit channel and updates state.
func (s *Slot) watchExit() {
	s.mu.RLock()
	proc := s.process
	s.mu.RUnlock()
	if proc == nil {
		return
	}
	info := <-proc.ExitChannel()

	s.mu.Lock()
	stopping := s.stopping
	if s.cancelCtx != nil {
		s.cancelCtx()
		s.cancelCtx = nil
	}
	if s.rconClient != nil {
		_ = s.rconClient.Close()
		s.rconClient = nil
	}
	if s.lock != nil {
		_ = s.lock.Release()
		s.lock = nil
	}
	s.process = nil
	s.server = nil
	s.mountedID = ""
	s.slp = nil
	s.mu.Unlock()

	// Tear down the Discord session — outside the slot lock so the
	// gateway close doesn't block other operations.
	s.closeDiscordBot()

	switch {
	case stopping && info.Reason == "normal":
		s.transition(StateIdle, nil)
	case stopping && info.Reason == "killed":
		// We killed it because graceful didn't work — still expected,
		// land in idle but record the killed reason.
		s.transition(StateIdle, &LastError{
			Code:    "killed_after_grace",
			Message: fmt.Sprintf("process killed after grace period (signal %v)", info.Signal),
			At:      info.At,
		})
	default:
		s.transition(StateCrashed, &LastError{
			Code:    "process_exited_nonzero",
			Message: fmt.Sprintf("java exited with code %d (reason: %s)", info.ExitCode, info.Reason),
			At:      info.At,
		})
	}
}

// healthLoop SLP-polls the slot's port. While starting, the first
// successful probe transitions state → running. While running, it
// keeps the SLP info field fresh.
func (s *Slot) healthLoop(ctx context.Context) {
	deadline := time.Now().Add(startupBudget)
	t := time.NewTicker(healthProbeInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
		st, err := slp.Probe(probeCtx, "127.0.0.1", s.cfg.Port)
		cancel()

		s.mu.Lock()
		state := s.state
		if err == nil {
			motd := ""
			if d, ok := st.Description.(string); ok {
				motd = d
			}
			s.slp = &SLPInfo{
				Online:    true,
				Players:   PlayersInfo{Online: st.Players.Online, Max: st.Players.Max},
				MOTD:      motd,
				LatencyMS: st.LatencyMS,
				SampledAt: time.Now().UTC(),
			}
		} else if state == StateRunning {
			// Mark as offline but keep the slot running until process
			// exits — SLP can flicker under load.
			if s.slp != nil {
				s.slp.Online = false
				s.slp.SampledAt = time.Now().UTC()
			}
		}
		s.mu.Unlock()

		if err == nil && state == StateStarting {
			s.transition(StateRunning, nil)
		}
		if state == StateStarting && time.Now().After(deadline) {
			slog.Error("slot: startup budget exhausted",
				"slot", s.cfg.Name,
				"port", s.cfg.Port,
				"budget", startupBudget,
			)
			s.transition(StateError, &LastError{
				Code:    "startup_timeout",
				Message: fmt.Sprintf("server did not become SLP-healthy within %s", startupBudget),
				At:      time.Now().UTC(),
			})
			// Trigger a kill so we don't leak the process.
			s.mu.RLock()
			proc := s.process
			s.mu.RUnlock()
			if proc != nil {
				go proc.Terminate(context.Background(), 5*time.Second)
			}
			return
		}
	}
}

// rconConnectLoop tries to establish the RCON connection until the
// process exits or context is canceled. Once connected, the client
// stays in s.rconClient until the slot tears down.
func (s *Slot) rconConnectLoop(ctx context.Context, port int, pass string) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		c, err := rcon.Dial(dctx, "127.0.0.1", port, pass)
		cancel()
		if err != nil {
			continue
		}
		s.mu.Lock()
		// Close any preexisting client before storing the new one.
		if s.rconClient != nil {
			_ = s.rconClient.Close()
		}
		s.rconClient = c
		s.mu.Unlock()
		slog.Info("slot: rcon connected", "slot", s.cfg.Name, "addr", addr)
		return
	}
}

// parseXmxMB extracts the megabyte value from a -Xmx flag in args.
// Accepts -XmxNg, -XmxNm, -XmxNk. Returns 0 if not present or unparseable.
func parseXmxMB(args []string) int {
	for _, a := range args {
		if len(a) > 4 && (a[:4] == "-Xmx") {
			body := a[4:]
			if len(body) < 2 {
				continue
			}
			unit := body[len(body)-1]
			numStr := body[:len(body)-1]
			n, err := strconv.Atoi(numStr)
			if err != nil {
				continue
			}
			switch unit {
			case 'g', 'G':
				return n * 1024
			case 'm', 'M':
				return n
			case 'k', 'K':
				return n / 1024
			}
		}
	}
	return 0
}
