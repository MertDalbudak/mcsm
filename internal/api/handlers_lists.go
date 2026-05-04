package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/MertDalbudak/mcsm/internal/slot"
)

// readJSONFile loads a JSON document from inside a server directory.
// Returns (nil, nil) when the file doesn't exist — many of these list
// files are only created the first time the corresponding command is
// used (e.g. whitelist.json doesn't exist if you never whitelisted).
func readJSONFile(dir, name string, dst any) error {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return json.Unmarshal(raw, dst)
}

func (s *Server) requireMounted(w http.ResponseWriter, r *http.Request) (*slot.Slot, string, bool) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return nil, "", false
	}
	srv := sl.MountedServer()
	if srv == nil {
		WriteError(w, r, http.StatusConflict, CodeServerNotRunning,
			"no server is mounted in this slot", map[string]any{"slot": name})
		return nil, "", false
	}
	return sl, srv.Path, true
}

type whitelistEntry struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type whitelistResponse struct {
	Enabled bool             `json:"enabled"`
	Players []whitelistEntry `json:"players"`
}

// GET /api/v1/slots/{name}/server/whitelist
func (s *Server) handleWhitelistGet(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.requireMounted(w, r)
	if !ok {
		return
	}
	var entries []whitelistEntry
	if err := readJSONFile(dir, "whitelist.json", &entries); err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal,
			err.Error(), nil)
		return
	}
	if entries == nil {
		entries = []whitelistEntry{}
	}
	enabled := false
	if props, _ := readPropsAt(dir); props["white-list"] == "true" {
		enabled = true
	}
	WriteJSON(w, r, http.StatusOK, whitelistResponse{
		Enabled: enabled,
		Players: entries,
	})
}

type bannedPlayer struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	Created string `json:"created"`
	Source  string `json:"source"`
	Expires string `json:"expires"`
	Reason  string `json:"reason"`
}

type bannedIP struct {
	IP      string `json:"ip"`
	Created string `json:"created"`
	Source  string `json:"source"`
	Expires string `json:"expires"`
	Reason  string `json:"reason"`
}

// GET /api/v1/slots/{name}/server/banlist
func (s *Server) handleBanlistGet(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.requireMounted(w, r)
	if !ok {
		return
	}
	var entries []bannedPlayer
	if err := readJSONFile(dir, "banned-players.json", &entries); err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal,
			err.Error(), nil)
		return
	}
	if entries == nil {
		entries = []bannedPlayer{}
	}
	WriteJSON(w, r, http.StatusOK, map[string]any{"players": entries})
}

// GET /api/v1/slots/{name}/server/banlist/ips
func (s *Server) handleBanlistIPsGet(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.requireMounted(w, r)
	if !ok {
		return
	}
	var entries []bannedIP
	if err := readJSONFile(dir, "banned-ips.json", &entries); err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal,
			err.Error(), nil)
		return
	}
	if entries == nil {
		entries = []bannedIP{}
	}
	WriteJSON(w, r, http.StatusOK, map[string]any{"ips": entries})
}
