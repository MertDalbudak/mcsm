package api

import (
	"net/http"
	"slices"
	"time"

	"github.com/MertDalbudak/mcsm/internal/discovery"
	"github.com/MertDalbudak/mcsm/internal/lock"
)

type discoveryResponse struct {
	ScannedAt time.Time           `json:"scanned_at"`
	Servers   []discovery.Server  `json:"servers"`
}

// handleDiscovery — GET /api/v1/discovery.
// Supports ?state=<...> and ?type=<...> filters (each repeatable).
func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	cat := s.disco.Snapshot()

	stateFilter := r.URL.Query()["state"]
	typeFilter := r.URL.Query()["type"]

	servers := cat.Servers
	if len(stateFilter) > 0 || len(typeFilter) > 0 {
		filtered := make([]discovery.Server, 0, len(servers))
		for _, sv := range servers {
			if len(stateFilter) > 0 && !slices.Contains(stateFilter, string(sv.Ownership.State)) {
				continue
			}
			if len(typeFilter) > 0 && !slices.Contains(typeFilter, sv.Type) {
				continue
			}
			filtered = append(filtered, sv)
		}
		servers = filtered
	}

	WriteJSON(w, r, http.StatusOK, discoveryResponse{
		ScannedAt: cat.ScannedAt,
		Servers:   servers,
	})
}

// handleDiscoveryRefresh — POST /api/v1/discovery/refresh.
// Forces a scan and returns the fresh catalog.
func (s *Server) handleDiscoveryRefresh(w http.ResponseWriter, r *http.Request) {
	cat, err := s.disco.Refresh(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal,
			"refresh failed: "+err.Error(), nil)
		return
	}
	WriteJSON(w, r, http.StatusOK, discoveryResponse{
		ScannedAt: cat.ScannedAt,
		Servers:   cat.Servers,
	})
}

// handleDiscoveryUnlock — DELETE /api/v1/discovery/{server_id}/lock.
//
// Allowed when the lock is owned-self (normal release) or stale.
// Stealing a stale lock from another instance requires ?force=true.
// Stealing a fresh owned-other lock is never allowed (409 lock_held).
func (s *Server) handleDiscoveryUnlock(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("server_id")
	force := r.URL.Query().Get("force") == "true"

	cat := s.disco.Snapshot()
	target := cat.FindByID(id)
	if target == nil {
		// Try a fresh scan in case the catalog is stale before failing.
		fresh, _ := s.disco.Refresh(r.Context())
		if fresh != nil {
			target = fresh.FindByID(id)
		}
		if target == nil {
			WriteError(w, r, http.StatusNotFound, CodeServerNotFound,
				"no server with that id in discovery", map[string]any{"server_id": id})
			return
		}
	}

	switch target.Ownership.State {
	case lock.StateFree:
		// Idempotent — already unlocked.
		WriteJSON(w, r, http.StatusOK, map[string]any{
			"server_id": id,
			"state":     lock.StateFree,
			"action":    "noop",
		})
		return

	case lock.StateOwnedSelf, lock.StateStale:
		if target.Ownership.State == lock.StateStale && !force {
			WriteError(w, r, http.StatusConflict, CodeLockHeld,
				"lock is stale; pass ?force=true to steal",
				map[string]any{
					"server_id": id,
					"owned_by":  target.Ownership.Instance,
					"slot":      target.Ownership.Slot,
				})
			return
		}
		if err := lock.Remove(target.Path); err != nil {
			WriteError(w, r, http.StatusInternalServerError, CodeInternal,
				"remove owner.json: "+err.Error(), nil)
			return
		}
		_, _ = s.disco.Refresh(r.Context())
		WriteJSON(w, r, http.StatusOK, map[string]any{
			"server_id": id,
			"state":     lock.StateFree,
			"action":    "released",
		})
		return

	case lock.StateOwnedOther:
		WriteError(w, r, http.StatusConflict, CodeLockHeld,
			"lock is held by another live instance",
			map[string]any{
				"server_id": id,
				"owned_by":  target.Ownership.Instance,
				"slot":      target.Ownership.Slot,
				"heartbeat": target.Ownership.Heartbeat,
			})
		return
	}
}
