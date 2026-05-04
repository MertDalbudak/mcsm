package api

import (
	"net/http"
	"strconv"

	"github.com/MertDalbudak/mcsm/internal/serverid"
)

// reservedKeys are server.properties keys mcsm manages. PATCH rejects
// these in the rejected[] list. They're rewritten on every slot mount.
var reservedKeys = map[string]bool{
	"server-port":           true,
	"query.port":            true,
	"enable-rcon":           true,
	"rcon.port":             true,
	"rcon.password":         true,
	"broadcast-rcon-to-ops": true,
}

type propertiesResponse struct {
	Values  map[string]any `json:"values"`
	RawPath string         `json:"raw_path"`
}

// GET /api/v1/slots/{name}/server/properties
//
// Returns the parsed properties. We don't require state==running because
// you might want to inspect properties for a slot whose server crashed.
func (s *Server) handleServerPropertiesGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return
	}
	srv := sl.MountedServer()
	if srv == nil {
		WriteError(w, r, http.StatusConflict, CodeServerNotRunning,
			"no server is mounted in this slot", map[string]any{"slot": name})
		return
	}
	props, err := serverid.ReadProperties(serverid.PropertiesPath(srv.Path))
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal,
			err.Error(), nil)
		return
	}
	WriteJSON(w, r, http.StatusOK, propertiesResponse{
		Values:  coerceTypes(props),
		RawPath: serverid.PropertiesPath(srv.Path),
	})
}

type propertiesPatchReq struct {
	Values map[string]any `json:"values"`
}

type propertiesPatchResp struct {
	Applied         []string `json:"applied"`
	RequiresRestart []string `json:"requires_restart"`
	Rejected        []string `json:"rejected"`
}

// requiresRestart lists keys whose effect only takes hold on next boot.
// Conservative: most properties require a restart in vanilla; Paper
// hot-reloads a few via /reload. We err on the side of marking them
// requires_restart so clients prompt the operator to restart.
var requiresRestart = map[string]bool{
	"view-distance":             true,
	"simulation-distance":       true,
	"max-players":               true,
	"difficulty":                false, // /difficulty applies live
	"gamemode":                  true,
	"online-mode":               true,
	"white-list":                false, // /whitelist on/off applies live
	"enforce-whitelist":         false,
	"motd":                      true,
	"level-name":                true,
	"level-seed":                true,
	"level-type":                true,
	"spawn-protection":          false,
	"pvp":                       true,
	"hardcore":                  true,
	"allow-flight":              true,
	"resource-pack":             true,
	"resource-pack-sha1":        true,
}

// PATCH /api/v1/slots/{name}/server/properties
func (s *Server) handleServerPropertiesPatch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sl, err := s.slotMgr.Get(name)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, CodeSlotNotFound,
			"no slot with that name", map[string]any{"slot": name})
		return
	}
	srv := sl.MountedServer()
	if srv == nil {
		WriteError(w, r, http.StatusConflict, CodeServerNotRunning,
			"no server is mounted in this slot", map[string]any{"slot": name})
		return
	}

	var body propertiesPatchReq
	if !DecodeJSON(w, r, &body) {
		return
	}
	if len(body.Values) == 0 {
		WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
			"values must contain at least one entry", nil)
		return
	}

	patch := make(map[string]string, len(body.Values))
	resp := propertiesPatchResp{
		Applied:         []string{},
		RequiresRestart: []string{},
		Rejected:        []string{},
	}
	for k, v := range body.Values {
		if reservedKeys[k] {
			resp.Rejected = append(resp.Rejected, k)
			continue
		}
		patch[k] = stringifyAny(v)
		resp.Applied = append(resp.Applied, k)
		if requiresRestart[k] {
			resp.RequiresRestart = append(resp.RequiresRestart, k)
		}
	}
	if len(patch) > 0 {
		if err := serverid.PatchProperties(srv.Path, patch); err != nil {
			WriteError(w, r, http.StatusInternalServerError, CodeInternal,
				err.Error(), nil)
			return
		}
	}
	WriteJSON(w, r, http.StatusOK, resp)
}

// coerceTypes turns string-valued properties into typed values where
// the type is unambiguous (bool, int). Everything else stays a string.
// Makes /properties responses friendlier for JSON-typed UIs.
func coerceTypes(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch v {
		case "true":
			out[k] = true
		case "false":
			out[k] = false
		default:
			if n, err := strconv.Atoi(v); err == nil {
				out[k] = n
			} else {
				out[k] = v
			}
		}
	}
	return out
}

func stringifyAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(x)
	case float64:
		// JSON decoder gives us float64 for numbers; render integers as ints.
		if x == float64(int(x)) {
			return strconv.Itoa(int(x))
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return ""
	}
}
