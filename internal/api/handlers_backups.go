package api

import (
	"net/http"
	"path/filepath"

	"github.com/MertDalbudak/mcsm/internal/backup"
)

// requireSlotAndServer fetches the slot + the mounted server's path.
// Returns (nil, false) and writes the error envelope on failure.
func (s *Server) requireSlotAndServer(w http.ResponseWriter, r *http.Request) (slotName, serverID, serverDir string, ok bool) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return "", "", "", false
	}
	srv := sl.MountedServer()
	if srv == nil {
		WriteError(w, r, http.StatusConflict, CodeServerNotRunning,
			"no server is mounted in this slot", map[string]any{"slot": name})
		return "", "", "", false
	}
	return name, srv.ID, srv.Path, true
}

// GET /api/v1/slots/{name}/server/backups
func (s *Server) handleBackupsList(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		WriteError(w, r, http.StatusServiceUnavailable, CodeInternal,
			"backups disabled (instance.data_dir unset)", nil)
		return
	}
	_, serverID, _, ok := s.requireSlotAndServer(w, r)
	if !ok {
		return
	}
	list, err := s.backups.List(serverID)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]any{
		"server_id": serverID,
		"backups":   list,
	})
}

type createBackupReq struct {
	Label       string `json:"label"`
	StopServer  bool   `json:"stop_server"`
	IncludeLogs bool   `json:"include_logs"`
}

// POST /api/v1/slots/{name}/server/backups
//
// stop_server == false → online (RCON save-off / flush / save-on).
// stop_server == true  → caller is responsible for stopping the slot
//   first; we don't bounce the slot here. Returns 409 if the slot is
//   running (operator must stop it explicitly to acknowledge downtime).
func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		WriteError(w, r, http.StatusServiceUnavailable, CodeInternal,
			"backups disabled (instance.data_dir unset)", nil)
		return
	}
	name, serverID, serverDir, ok := s.requireSlotAndServer(w, r)
	if !ok {
		return
	}
	var body createBackupReq
	if r.ContentLength > 0 {
		if !DecodeJSON(w, r, &body) {
			return
		}
	}

	sl, _ := s.slotMgr.Get(name)
	mode := backup.ModeOnline
	opts := backup.CreateOptions{
		Label:       body.Label,
		Mode:        mode,
		IncludeLogs: body.IncludeLogs,
	}
	if body.StopServer {
		// Offline mode requires the slot to be idle/crashed; refuse if running.
		opts.Mode = backup.ModeOffline
		if !sl.State().Terminal() {
			WriteError(w, r, http.StatusConflict, CodeSlotBusy,
				"stop_server=true requires the slot to be idle first; call POST /stop and wait for state=idle",
				map[string]any{"slot": name, "current_state": sl.State()})
			return
		}
	} else {
		// Online — wire RCON save control if available.
		if sc := sl.SaveControlForBackup(); sc != nil {
			opts.SaveControl = sc
		}
	}

	meta, err := s.backups.Create(r.Context(), serverID, serverDir, opts)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal,
			"backup create: "+err.Error(), nil)
		return
	}
	WriteJSON(w, r, http.StatusCreated, meta)
}

// GET /api/v1/slots/{name}/server/backups/{id}
func (s *Server) handleBackupGet(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		WriteError(w, r, http.StatusServiceUnavailable, CodeInternal,
			"backups disabled (instance.data_dir unset)", nil)
		return
	}
	_, serverID, _, ok := s.requireSlotAndServer(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	meta, err := s.backups.Get(serverID, id)
	if err == backup.ErrNotFound {
		WriteError(w, r, http.StatusNotFound, CodeBackupNotFound,
			"backup not found", map[string]any{"id": id})
		return
	}
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, r, http.StatusOK, meta)
}

// DELETE /api/v1/slots/{name}/server/backups/{id}
func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		WriteError(w, r, http.StatusServiceUnavailable, CodeInternal,
			"backups disabled (instance.data_dir unset)", nil)
		return
	}
	_, serverID, _, ok := s.requireSlotAndServer(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.backups.Delete(serverID, id); err == backup.ErrNotFound {
		WriteError(w, r, http.StatusNotFound, CodeBackupNotFound, "backup not found",
			map[string]any{"id": id})
		return
	} else if err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]string{"id": id, "deleted": "true"})
}

// POST /api/v1/slots/{name}/server/backups/{id}/restore
//
// Restore requires the slot to be terminal (idle/crashed) — extracting
// over a running server produces an inconsistent world.
func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		WriteError(w, r, http.StatusServiceUnavailable, CodeInternal,
			"backups disabled (instance.data_dir unset)", nil)
		return
	}
	name, serverID, serverDir, ok := s.requireSlotAndServer(w, r)
	if !ok {
		return
	}
	sl, _ := s.slotMgr.Get(name)
	if !sl.State().Terminal() {
		WriteError(w, r, http.StatusConflict, CodeSlotBusy,
			"restore requires the slot to be stopped first",
			map[string]any{"slot": name, "current_state": sl.State()})
		return
	}
	id := r.PathValue("id")
	if err := s.backups.Restore(r.Context(), serverID, id, serverDir); err == backup.ErrNotFound {
		WriteError(w, r, http.StatusNotFound, CodeBackupNotFound,
			"backup not found", map[string]any{"id": id})
		return
	} else if err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal,
			"restore: "+err.Error(), nil)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]any{
		"id":         id,
		"restored":   true,
		"server_id":  serverID,
		"server_dir": filepath.Clean(serverDir),
	})
}
