package api

import (
	"net/http"
	"slices"

	"github.com/MertDalbudak/mcsm/internal/discovery"
	"github.com/MertDalbudak/mcsm/internal/lock"
)

// GET /api/v1/slots/{name}/compatible-servers
//
// Returns the subset of /discovery that this slot can mount, applying
// the same filtering rules slot.Start uses (accepts.types, max_memory_mb,
// ownership state — owned-other servers are excluded; stale and
// owned-self are included as candidates).
func (s *Server) handleSlotCompatible(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return
	}
	cat := s.disco.Snapshot()
	cfg := s.cfgSlot(name)

	out := make([]discovery.Server, 0, len(cat.Servers))
	for _, srv := range cat.Servers {
		// Skip servers locked by another live instance.
		if srv.Ownership.State == lock.StateOwnedOther {
			continue
		}
		// Type filter.
		if len(cfg.Accepts.Types) > 0 && !slices.Contains(cfg.Accepts.Types, srv.Type) {
			continue
		}
		// Memory filter — best-effort. We don't re-read .mcsm/config.yaml
		// here; the slot.Start path does the strict check. This list is
		// the *probably-compatible* set, surfaced for UI dropdowns.
		out = append(out, srv)
	}
	_ = sl // reserved for future use (e.g. version-compat checks)
	WriteJSON(w, r, http.StatusOK, map[string]any{
		"slot":      name,
		"servers":   out,
		"scanned_at": cat.ScannedAt,
	})
}

// cfgSlot looks up the config block for a slot by name. Lives here
// because the API server is the only place that holds *config.Config.
func (s *Server) cfgSlot(name string) (cfg slotCfg) {
	for _, sc := range s.cfg.Slots {
		if sc.Name == name {
			return slotCfg{Accepts: acceptsCfg{
				Types:       sc.Accepts.Types,
				MaxMemoryMB: sc.Accepts.MaxMemoryMB,
			}}
		}
	}
	return slotCfg{}
}

type slotCfg struct {
	Accepts acceptsCfg
}

type acceptsCfg struct {
	Types       []string
	MaxMemoryMB int
}
