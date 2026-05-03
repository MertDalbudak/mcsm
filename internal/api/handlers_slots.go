package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/MertDalbudak/mcsm/internal/lock"
	"github.com/MertDalbudak/mcsm/internal/slot"
)

type slotsListResponse struct {
	Slots []slot.Snapshot `json:"slots"`
}

func (s *Server) handleSlotsList(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, r, http.StatusOK, slotsListResponse{
		Slots: s.slotMgr.Snapshots(),
	})
}

func (s *Server) handleSlotGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return
	}
	WriteJSON(w, r, http.StatusOK, sl.Snapshot())
}

type startReq struct {
	ServerID string `json:"server_id"`
	Force    bool   `json:"force"`
}

func (s *Server) handleSlotStart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return
	}
	var body startReq
	if !DecodeJSON(w, r, &body) {
		return
	}
	if body.ServerID == "" {
		WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
			"server_id is required", nil)
		return
	}
	snap, err := sl.Start(r.Context(), slot.StartOptions{
		ServerID: body.ServerID,
		Force:    body.Force,
	})
	if err != nil {
		mapSlotError(w, r, err, name)
		return
	}
	WriteJSON(w, r, http.StatusAccepted, snap)
}

type stopReq struct {
	GracefulSeconds   int    `json:"graceful_seconds,omitempty"`
	BroadcastEvery    int    `json:"broadcast_every,omitempty"`
	BroadcastTemplate string `json:"broadcast_template,omitempty"`
	KillGrace         string `json:"kill_grace,omitempty"`
}

func (s *Server) handleSlotStop(w http.ResponseWriter, r *http.Request) {
	s.slotMutate(w, r, func(sl *slot.Slot, body stopReq) (slot.Snapshot, error) {
		return sl.Stop(r.Context(), stopReqToOptions(body))
	})
}

func (s *Server) handleSlotRestart(w http.ResponseWriter, r *http.Request) {
	s.slotMutate(w, r, func(sl *slot.Slot, body stopReq) (slot.Snapshot, error) {
		return sl.Restart(r.Context(), stopReqToOptions(body))
	})
}

func (s *Server) handleSlotAbortStop(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return
	}
	// Phase 2: abort-stop is best-effort. The grace timer is internal to
	// Slot.Stop; we don't yet cancel mid-grace. Returning 409 keeps the
	// API contract honest about the limitation rather than silently lying.
	WriteError(w, r, http.StatusConflict, CodeNotStopping,
		"abort-stop not implemented in this build; grace period will run to completion",
		map[string]any{"slot": sl.Name(), "current_state": sl.State()})
}

// slotMutate is the shared pattern for stop/restart: lookup → decode → call.
func (s *Server) slotMutate(w http.ResponseWriter, r *http.Request,
	fn func(*slot.Slot, stopReq) (slot.Snapshot, error),
) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return
	}
	var body stopReq
	if r.ContentLength > 0 {
		if !DecodeJSON(w, r, &body) {
			return
		}
	}
	snap, err := fn(sl, body)
	if err != nil {
		mapSlotError(w, r, err, name)
		return
	}
	WriteJSON(w, r, http.StatusAccepted, snap)
}

func stopReqToOptions(b stopReq) slot.StopOptions {
	o := slot.StopOptions{
		GracefulSeconds:   b.GracefulSeconds,
		BroadcastEvery:    b.BroadcastEvery,
		BroadcastTemplate: b.BroadcastTemplate,
	}
	if b.KillGrace != "" {
		if d, err := time.ParseDuration(b.KillGrace); err == nil {
			o.KillGrace = d
		}
	}
	return o
}

// mapSlotError translates internal/slot error types into the documented
// HTTP status + error code envelope.
func mapSlotError(w http.ResponseWriter, r *http.Request, err error, slotName string) {
	switch {
	case errors.Is(err, slot.ErrSlotBusy):
		WriteError(w, r, http.StatusConflict, CodeSlotBusy, err.Error(),
			map[string]any{"slot": slotName})
	case errors.Is(err, slot.ErrServerNotMounted):
		WriteError(w, r, http.StatusConflict, CodeServerNotRunning, err.Error(),
			map[string]any{"slot": slotName})
	case errors.Is(err, slot.ErrServerNotRunning):
		WriteError(w, r, http.StatusConflict, CodeServerNotRunning, err.Error(),
			map[string]any{"slot": slotName})
	case errors.Is(err, slot.ErrServerIncompatible):
		WriteError(w, r, http.StatusBadRequest, CodeServerIncompatible, err.Error(),
			map[string]any{"slot": slotName})
	case errors.Is(err, slot.ErrNotStopping):
		WriteError(w, r, http.StatusConflict, CodeNotStopping, err.Error(),
			map[string]any{"slot": slotName})
	case errors.Is(err, lock.ErrAlreadyHeld):
		WriteError(w, r, http.StatusConflict, CodeServerInUse, err.Error(),
			map[string]any{"slot": slotName})
	default:
		WriteError(w, r, http.StatusInternalServerError, CodeInternal, err.Error(),
			map[string]any{"slot": slotName})
	}
}
