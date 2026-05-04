package slot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MertDalbudak/mcsm/internal/discord"
	"github.com/MertDalbudak/mcsm/internal/serverid"
	"github.com/MertDalbudak/mcsm/internal/slp"
)

// DiscordTempProvider is the seam mcsm uses to feed CPU temperature
// into Discord's /temp command without making the slot package depend
// on the system package directly. main.go wires it up at boot.
type DiscordTempProvider interface {
	Snapshot() (celsius float64, ok bool)
}

// SetTempProvider attaches a temperature source for the Discord bot's
// /temp command. Pass nil to disable.
func (s *Slot) SetTempProvider(p DiscordTempProvider) {
	s.mu.Lock()
	s.tempProvider = p
	s.mu.Unlock()
}

// startDiscordBot connects a Discord session for the mounted server
// and stores the bot on the slot. Called from Slot.Start in a
// goroutine — gateway open is slow and we don't want to delay java
// startup waiting on it.
func (s *Slot) startDiscordBot(ctx context.Context, cfg *serverid.Config) {
	bot, err := discord.Connect(ctx, cfg.Discord.Token, cfg.Discord.Channels, &discordAdapter{s: s})
	if err != nil || bot == nil {
		return
	}
	s.mu.Lock()
	if s.bot != nil {
		_ = s.bot.Close()
	}
	s.bot = bot
	srvName := ""
	if s.server != nil {
		srvName = s.server.Name
	}
	s.mu.Unlock()
	bot.Notify(ctx, "✅ "+srvName+" is starting up.")
}

// closeDiscordBot tears down the bot if connected. Called from watchExit.
func (s *Slot) closeDiscordBot() {
	s.mu.Lock()
	bot := s.bot
	s.bot = nil
	srvName := ""
	if s.server != nil {
		srvName = s.server.Name
	}
	s.mu.Unlock()
	if bot == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	bot.Notify(ctx, "🛑 "+srvName+" stopped.")
	_ = bot.Close()
}

// notifyDiscord sends a message if a bot is connected. Best-effort.
func (s *Slot) notifyDiscord(msg string) {
	s.mu.RLock()
	bot := s.bot
	s.mu.RUnlock()
	if bot == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	bot.Notify(ctx, msg)
}

// discordAdapter implements discord.Provider on top of a Slot. It runs
// without holding the slot lock — every method takes its own
// short-lived snapshot of what it needs.
type discordAdapter struct{ s *Slot }

func (a *discordAdapter) ServerName() string {
	if srv := a.s.MountedServer(); srv != nil {
		return srv.Name
	}
	return "<unmounted>"
}

func (a *discordAdapter) PublicAddress() string {
	a.s.mu.RLock()
	defer a.s.mu.RUnlock()
	if a.s.cfg.PublicAddress != "" {
		return fmt.Sprintf("%s:%d", a.s.cfg.PublicAddress, a.s.cfg.Port)
	}
	return fmt.Sprintf("localhost:%d", a.s.cfg.Port)
}

// Players: SLP for online/max, RCON `list` for the names.
func (a *discordAdapter) Players(ctx context.Context) ([]string, int, error) {
	a.s.mu.RLock()
	port := a.s.cfg.Port
	a.s.mu.RUnlock()

	st, err := slp.Probe(ctx, "127.0.0.1", port)
	if err != nil {
		return nil, 0, err
	}
	max := st.Players.Max

	resp, err := a.s.Command(ctx, "list")
	if err != nil {
		return nil, max, nil // SLP succeeded; treat name list as empty
	}
	names := []string{}
	if i := strings.Index(resp, ":"); i >= 0 {
		body := strings.TrimSpace(resp[i+1:])
		if body != "" {
			for _, n := range strings.Split(body, ",") {
				n = strings.TrimSpace(n)
				if n != "" {
					names = append(names, n)
				}
			}
		}
	}
	return names, max, nil
}

func (a *discordAdapter) Version(ctx context.Context) (string, error) {
	st, err := slp.Probe(ctx, "127.0.0.1", a.portRO())
	if err != nil {
		return "", err
	}
	return st.Version.Name, nil
}

func (a *discordAdapter) Temperature(ctx context.Context) (float64, bool) {
	a.s.mu.RLock()
	tp := a.s.tempProvider
	a.s.mu.RUnlock()
	if tp == nil {
		return 0, false
	}
	return tp.Snapshot()
}

// Banlist reads banned-players.json from the server dir.
func (a *discordAdapter) Banlist(ctx context.Context) ([]string, error) {
	srv := a.s.MountedServer()
	if srv == nil {
		return nil, nil
	}
	body, err := os.ReadFile(filepath.Join(srv.Path, "banned-players.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Name != "" {
			out = append(out, e.Name)
		}
	}
	return out, nil
}

func (a *discordAdapter) portRO() int {
	a.s.mu.RLock()
	defer a.s.mu.RUnlock()
	return a.s.cfg.Port
}
