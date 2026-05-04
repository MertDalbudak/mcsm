package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/MertDalbudak/mcsm/internal/events"
	"github.com/MertDalbudak/mcsm/internal/logtail"
	"github.com/coder/websocket"
)

// WS /api/v1/slots/{name}/server/logs/stream
//
// Backfills `?tail=N` (default 0) entries, then streams live tail
// frames (NDJSON-shape, one LogEntry per text frame) until the client
// disconnects or the slot stops.
func (s *Server) handleServerLogsStream(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		writeNotFound(w, r, CodeSlotNotFound, "no slot with that name",
			map[string]any{"slot": name})
		return
	}
	tailer := sl.Tailer()
	if tailer == nil {
		WriteError(w, r, http.StatusConflict, CodeServerNotRunning,
			"no server is mounted in this slot", map[string]any{"slot": name})
		return
	}

	c, err := websocket.Accept(w, r, s.wsAcceptOptions())
	if err != nil {
		slog.Warn("ws logs: accept", "err", err)
		return
	}
	defer wsCloseNormal(c)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Optional backfill before subscribing to the live stream so the
	// client sees a coherent (no-gap) sequence.
	q := r.URL.Query()
	if tailStr := q.Get("tail"); tailStr != "" {
		if n, err := strconv.Atoi(tailStr); err == nil && n > 0 {
			if n > maxTail {
				n = maxTail
			}
			for _, e := range tailer.Recent(n) {
				if err := wsWriteJSON(ctx, c, e); err != nil {
					return
				}
			}
		}
	}
	if sinceStr := q.Get("since"); sinceStr != "" {
		if t, err := time.Parse(time.RFC3339Nano, sinceStr); err == nil {
			for _, e := range tailer.Since(t) {
				if err := wsWriteJSON(ctx, c, e); err != nil {
					return
				}
			}
		}
	}

	sub := tailer.Subscribe(64)
	defer tailer.Unsubscribe(sub)

	go wsKeepAlive(ctx, c)
	readDone := wsReadControl(ctx, c)

	for {
		select {
		case <-ctx.Done():
			return
		case <-readDone:
			return
		case e, ok := <-sub:
			if !ok {
				return
			}
			if err := wsWriteJSON(ctx, c, e); err != nil {
				return
			}
		}
	}
}

// WS /api/v1/slots/{name}/events
//
// Streams slot lifecycle and gameplay events (state transitions,
// player_join, player_leave, tps_sample, error). Frames have the shape
// from docs/api.md §4 (Slots → events WS).
func (s *Server) handleSlotEventsStream(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		writeNotFound(w, r, CodeSlotNotFound, "no slot with that name",
			map[string]any{"slot": name})
		return
	}
	bus := sl.Events()
	if bus == nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal,
			"slot has no event bus", map[string]any{"slot": name})
		return
	}

	c, err := websocket.Accept(w, r, s.wsAcceptOptions())
	if err != nil {
		slog.Warn("ws events: accept", "err", err)
		return
	}
	defer wsCloseNormal(c)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sub, unsub := bus.Subscribe()
	defer unsub()

	go wsKeepAlive(ctx, c)
	readDone := wsReadControl(ctx, c)

	for {
		select {
		case <-ctx.Done():
			return
		case <-readDone:
			return
		case e, ok := <-sub:
			if !ok {
				return
			}
			if err := wsWriteJSON(ctx, c, eventToFrame(e)); err != nil {
				return
			}
		}
	}
}

// eventToFrame renders an events.Event into the documented WS frame.
// We could ship the raw struct, but the spec specifies a per-type
// shape so we project explicitly to keep the contract honest.
func eventToFrame(e events.Event) map[string]any {
	out := map[string]any{
		"type": string(e.Type),
		"at":   e.At,
	}
	switch e.Type {
	case events.TypeState:
		out["from"] = e.From
		out["to"] = e.To
	case events.TypePlayerJoin, events.TypePlayerLeave:
		out["player"] = e.Player
	case events.TypeTPSSample:
		out["tps_1m"] = e.TPS1m
		out["tps_5m"] = e.TPS5m
	case events.TypeError:
		out["code"] = e.Code
		out["message"] = e.Message
	case events.TypePlayerDeath:
		out["player"] = e.Player
		if e.Killer != "" {
			out["killer"] = e.Killer
		}
		out["cause"] = e.Cause
		out["message"] = e.Message
	case events.TypePlayerKick:
		out["player"] = e.Player
		out["reason"] = e.Reason
	case events.TypeChat:
		out["player"] = e.Player
		out["message"] = e.Message
	}
	return out
}

// shadow ensures we use logtail.LogEntry (avoid unused import)
var _ logtail.LogEntry
