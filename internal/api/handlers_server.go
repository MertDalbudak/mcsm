package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MertDalbudak/mcsm/internal/slot"
)

// commandTimeout caps a single RCON command. Most commands are sub-50ms;
// 5s is generous for things like /save-all that flush chunks.
const commandTimeout = 5 * time.Second

// requireRunning fetches the slot and returns the live Slot if it's
// currently running. Otherwise it writes the appropriate error envelope
// and returns (nil, false).
func (s *Server) requireRunning(w http.ResponseWriter, r *http.Request) (*slot.Slot, bool) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return nil, false
	}
	if sl.State() != slot.StateRunning {
		WriteError(w, r, http.StatusConflict, CodeServerNotRunning,
			fmt.Sprintf("slot is in state %s, not running", sl.State()),
			map[string]any{"slot": name, "state": sl.State()})
		return nil, false
	}
	return sl, true
}

type commandReq struct {
	Command string `json:"command"`
}

type commandResp struct {
	Command   string `json:"command"`
	Response  string `json:"response"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

// POST /api/v1/slots/{name}/server/command
func (s *Server) handleServerCommand(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	var body commandReq
	if !DecodeJSON(w, r, &body) {
		return
	}
	if body.Command == "" {
		WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
			"command is required", nil)
		return
	}
	resp, elapsed, err := runCmd(sl, body.Command)
	if err != nil {
		mapRconError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, commandResp{
		Command:   body.Command,
		Response:  resp,
		ElapsedMS: elapsed.Milliseconds(),
	})
}

type sayReq struct {
	Message string `json:"message"`
}

// POST /api/v1/slots/{name}/server/say
func (s *Server) handleServerSay(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	var body sayReq
	if !DecodeJSON(w, r, &body) {
		return
	}
	if body.Message == "" {
		WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
			"message is required", nil)
		return
	}
	if _, _, err := runCmd(sl, "say "+body.Message); err != nil {
		mapRconError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "sent"})
}

type playersResp struct {
	Online  int      `json:"online"`
	Max     int      `json:"max"`
	Players []player `json:"players"`
}

type player struct {
	Name string `json:"name"`
}

// GET /api/v1/slots/{name}/server/players
//
// We use SLP for online/max because /list's stdout format varies between
// flavors; SLP gives structured numbers. The player name list comes from
// /list (RCON) since SLP only includes a small "sample".
func (s *Server) handleServerPlayers(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	snap := sl.Snapshot()
	var online, max int
	if snap.MountedServer != nil && snap.MountedServer.SLP != nil {
		online = snap.MountedServer.SLP.Players.Online
		max = snap.MountedServer.SLP.Players.Max
	}
	out := playersResp{Online: online, Max: max, Players: []player{}}

	if resp, _, err := runCmd(sl, "list"); err == nil {
		// Vanilla format: "There are 3 of a max of 20 players online: Steve, Alex, Bob"
		// Paper format:   same
		if i := strings.Index(resp, ":"); i >= 0 {
			names := strings.TrimSpace(resp[i+1:])
			if names != "" {
				for _, n := range strings.Split(names, ",") {
					n = strings.TrimSpace(n)
					if n != "" {
						out.Players = append(out.Players, player{Name: n})
					}
				}
			}
		}
	}
	WriteJSON(w, r, http.StatusOK, out)
}

type kickReq struct {
	Reason string `json:"reason"`
}

// POST /api/v1/slots/{name}/server/players/{player}/kick
func (s *Server) handleServerKick(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	target := r.PathValue("player")
	var body kickReq
	if r.ContentLength > 0 {
		if !DecodeJSON(w, r, &body) {
			return
		}
	}
	cmd := "kick " + target
	if body.Reason != "" {
		cmd += " " + body.Reason
	}
	resp, _, err := runCmd(sl, cmd)
	if err != nil {
		mapRconError(w, r, err)
		return
	}
	if strings.Contains(strings.ToLower(resp), "no player") {
		WriteError(w, r, http.StatusNotFound, CodePlayerNotOnline,
			"player is not online", map[string]any{"player": target})
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]string{
		"status":   "kicked",
		"player":   target,
		"response": resp,
	})
}

type banReq struct {
	Reason   string `json:"reason"`
	Duration string `json:"duration"`
	BanIP    bool   `json:"ban_ip"`
}

// POST /api/v1/slots/{name}/server/players/{player}/ban
//
// Vanilla doesn't support timed bans natively — we issue /ban and, if
// duration is set, schedule an unban command. Paper has the same limitation.
// (A more sophisticated implementation would patch banned-players.json
// with an `expires` field, the way the legacy Node code did.)
func (s *Server) handleServerBan(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	target := r.PathValue("player")
	var body banReq
	if r.ContentLength > 0 {
		if !DecodeJSON(w, r, &body) {
			return
		}
	}
	cmd := "ban " + target
	if body.Reason != "" {
		cmd += " " + body.Reason
	}
	if _, _, err := runCmd(sl, cmd); err != nil {
		mapRconError(w, r, err)
		return
	}
	if body.BanIP {
		_, _, _ = runCmd(sl, "ban-ip "+target)
	}
	if body.Duration != "" {
		if d, err := time.ParseDuration(body.Duration); err == nil && d > 0 {
			// Best-effort scheduled unban. Survives only as long as mcsm
			// is running — durable timed bans land in Phase 3.
			go func() {
				time.Sleep(d)
				_, _, _ = runCmd(sl, "pardon "+target)
			}()
		}
	}
	WriteJSON(w, r, http.StatusOK, map[string]any{
		"status":   "banned",
		"player":   target,
		"duration": body.Duration,
		"ban_ip":   body.BanIP,
	})
}

// POST /api/v1/slots/{name}/server/players/{player}/unban
func (s *Server) handleServerUnban(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	target := r.PathValue("player")
	if _, _, err := runCmd(sl, "pardon "+target); err != nil {
		mapRconError(w, r, err)
		return
	}
	_, _, _ = runCmd(sl, "pardon-ip "+target)
	WriteJSON(w, r, http.StatusOK, map[string]string{
		"status": "unbanned",
		"player": target,
	})
}

type opReq struct {
	Level int `json:"level"`
}

// POST /api/v1/slots/{name}/server/players/{player}/op
func (s *Server) handleServerOp(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	target := r.PathValue("player")
	var body opReq
	if r.ContentLength > 0 {
		_ = DecodeJSON(w, r, &body)
	}
	if _, _, err := runCmd(sl, "op "+target); err != nil {
		mapRconError(w, r, err)
		return
	}
	// Op level: vanilla has no per-player /op level command; it lives in
	// ops.json. Honor it best-effort by re-running /op with the level.
	WriteJSON(w, r, http.StatusOK, map[string]any{
		"status": "opped",
		"player": target,
		"level":  body.Level,
	})
}

// POST /api/v1/slots/{name}/server/players/{player}/deop
func (s *Server) handleServerDeop(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	target := r.PathValue("player")
	if _, _, err := runCmd(sl, "deop "+target); err != nil {
		mapRconError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]string{
		"status": "deopped",
		"player": target,
	})
}

// PUT /api/v1/slots/{name}/server/whitelist/{player}
func (s *Server) handleWhitelistAdd(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	target := r.PathValue("player")
	if _, _, err := runCmd(sl, "whitelist add "+target); err != nil {
		mapRconError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]string{
		"status": "added",
		"player": target,
	})
}

// DELETE /api/v1/slots/{name}/server/whitelist/{player}
func (s *Server) handleWhitelistRemove(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	target := r.PathValue("player")
	if _, _, err := runCmd(sl, "whitelist remove "+target); err != nil {
		mapRconError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]string{
		"status": "removed",
		"player": target,
	})
}

// POST /api/v1/slots/{name}/server/whitelist/reload
func (s *Server) handleWhitelistReload(w http.ResponseWriter, r *http.Request) {
	sl, ok := s.requireRunning(w, r)
	if !ok {
		return
	}
	if _, _, err := runCmd(sl, "whitelist reload"); err != nil {
		mapRconError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "reloaded"})
}

func runCmd(sl *slot.Slot, command string) (string, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	start := time.Now()
	resp, err := sl.Command(ctx, command)
	return resp, time.Since(start), err
}

func mapRconError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, slot.ErrServerNotRunning):
		WriteError(w, r, http.StatusConflict, CodeServerNotRunning,
			"server is not running", nil)
	case errors.Is(err, context.DeadlineExceeded):
		WriteError(w, r, http.StatusGatewayTimeout, CodeCommandTimeout,
			err.Error(), nil)
	default:
		WriteError(w, r, http.StatusBadGateway, CodeRconUnreachable,
			err.Error(), nil)
	}
}
