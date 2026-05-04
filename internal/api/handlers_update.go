package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/MertDalbudak/mcsm/internal/backup"
	"github.com/MertDalbudak/mcsm/internal/serverid"
	"github.com/MertDalbudak/mcsm/internal/update"
)

// GET /api/v1/slots/{name}/server/update
//
// Returns the latest available release for the server's flavor +
// version. The current installed version isn't tracked yet (we'd need
// to read the jar manifest), so the response just says "here's what's
// available for X" — clients can compare against what they last
// installed if they care.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	_, _, dir, ok := s.requireSlotAndServer(w, r)
	if !ok {
		return
	}
	cfg, err := serverid.Read(dir)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if cfg.Type != serverid.FlavorPaper {
		WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
			"automatic updates currently support flavor=paper only", map[string]any{"flavor": cfg.Type})
		return
	}

	// MC version is not in cfg yet (Phase 2A left .Version empty); the
	// caller passes ?mc_version=1.21.4 to disambiguate. If absent, error.
	mcVersion := r.URL.Query().Get("mc_version")
	if mcVersion == "" {
		WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
			"mc_version query parameter is required (e.g. ?mc_version=1.21.4)", nil)
		return
	}

	u := update.NewPaperUpdater()
	rctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	rel, err := u.Latest(rctx, mcVersion)
	if err != nil {
		WriteError(w, r, http.StatusBadGateway, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, r, http.StatusOK, rel)
}

type applyUpdateReq struct {
	MCVersion string `json:"mc_version"`
	Backup    bool   `json:"backup"` // create a snapshot before swapping
}

// POST /api/v1/slots/{name}/server/update
//
// Confirmation flow: the slot must be stopped (we don't auto-stop the
// JVM here — operators decide when downtime is acceptable). On success,
// the new jar replaces the current launch jar and a backup of the old
// one is kept as paper.jar.previous.
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	name, serverID, dir, ok := s.requireSlotAndServer(w, r)
	if !ok {
		return
	}
	sl, _ := s.slotMgr.Get(name)
	if !sl.State().Terminal() {
		WriteError(w, r, http.StatusConflict, CodeSlotBusy,
			"update requires the slot to be stopped first", map[string]any{
				"slot": name, "current_state": sl.State(),
			})
		return
	}
	var body applyUpdateReq
	if !DecodeJSON(w, r, &body) {
		return
	}
	if body.MCVersion == "" {
		WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
			"mc_version is required", nil)
		return
	}
	cfg, err := serverid.Read(dir)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if cfg.Type != serverid.FlavorPaper {
		WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
			"only flavor=paper supported", map[string]any{"flavor": cfg.Type})
		return
	}

	if body.Backup && s.backups != nil {
		_, err := s.backups.Create(r.Context(), serverID, dir, backup.CreateOptions{
			Label: "pre-update", Mode: backup.ModeOffline,
		})
		if err != nil {
			WriteError(w, r, http.StatusInternalServerError, CodeInternal,
				"pre-update backup failed: "+err.Error(), nil)
			return
		}
	}

	u := update.NewPaperUpdater()
	rctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rel, err := u.Latest(rctx, body.MCVersion)
	if err != nil {
		WriteError(w, r, http.StatusBadGateway, CodeInternal, err.Error(), nil)
		return
	}

	dctx, cancel2 := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel2()
	stagingPath := filepath.Join(dir, ".mcsm", "paper-staging.jar")
	if err := update.Download(dctx, nil, rel, stagingPath); err != nil {
		WriteError(w, r, http.StatusBadGateway, CodeInternal, err.Error(), nil)
		return
	}

	finalPath := filepath.Join(dir, "paper.jar")
	prevPath := filepath.Join(dir, "paper.jar.previous")
	// Move existing → previous (best effort; first-time install has no prior).
	_ = swapFile(finalPath, prevPath)
	if err := swapFile(stagingPath, finalPath); err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal,
			"install: "+err.Error(), nil)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]any{
		"installed": rel,
		"previous":  filepath.Base(prevPath),
		"server_id": serverID,
	})
}

// swapFile renames src → dst, replacing dst if it exists. Cross-FS
// scenarios are not handled (we only swap within the same dir).
func swapFile(src, dst string) error {
	return os.Rename(src, dst)
}
