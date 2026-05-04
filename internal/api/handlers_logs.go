package api

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MertDalbudak/mcsm/internal/logtail"
	"github.com/MertDalbudak/mcsm/internal/slot"
)

const (
	defaultTail = 200
	maxTail     = 5000
)

type logsResponse struct {
	Entries   []logtail.LogEntry `json:"entries"`
	Truncated bool               `json:"truncated"`
}

// GET /api/v1/slots/{name}/server/logs
//
// Phase 2C reads from the process supervisor's stdout ring buffer.
// Phase 2D will swap in an fsnotify-backed file tailer for logs/latest.log
// without changing this response shape.
func (s *Server) handleServerLogs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return
	}

	q := r.URL.Query()

	tailStr := q.Get("tail")
	sinceStr := q.Get("since")
	if tailStr != "" && sinceStr != "" {
		WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
			"tail and since are mutually exclusive", nil)
		return
	}

	tail := defaultTail
	if tailStr != "" {
		n, err := strconv.Atoi(tailStr)
		if err != nil || n < 1 {
			WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
				"tail must be a positive integer", nil)
			return
		}
		if n > maxTail {
			n = maxTail
		}
		tail = n
	}

	var since time.Time
	if sinceStr != "" {
		t, err := time.Parse(time.RFC3339Nano, sinceStr)
		if err != nil {
			WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
				"since must be RFC3339, e.g. 2026-05-04T10:00:00Z", nil)
			return
		}
		since = t
	}

	levels := q["level"] // repeatable

	// Pull from the slot. With a since cursor we ask for the full buffer
	// and filter in handler — the ring is only ~2000 lines so this is cheap.
	requested := tail
	if !since.IsZero() {
		requested = maxTail
	}
	raw := sl.Logs(requested)

	out := logsResponse{Entries: make([]logtail.LogEntry, 0, len(raw))}
	for _, l := range raw {
		entry := logtail.Parse(l.Text, l.At)
		if !since.IsZero() && !entry.TS.After(since) {
			continue
		}
		if len(levels) > 0 && entry.Level != "" && !slices.Contains(levels, strings.ToLower(entry.Level)) {
			// case-insensitive match
			matched := false
			for _, want := range levels {
				if strings.EqualFold(want, entry.Level) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out.Entries = append(out.Entries, entry)
	}

	// If we asked for tail=N and got back fewer, the ring is full and
	// older entries are gone — call that out for clients that care.
	out.Truncated = sl.State() != slot.StateIdle && len(raw) >= maxTail

	WriteJSON(w, r, http.StatusOK, out)
}
