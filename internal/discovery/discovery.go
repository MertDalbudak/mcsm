// Package discovery scans configured root directories for Minecraft
// server installs and produces a typed catalog with ownership state. The
// catalog is the input to the API's GET /discovery endpoint and to the
// slot manager's "what can I mount here" decision.
package discovery

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MertDalbudak/mcsm/internal/lock"
	"github.com/MertDalbudak/mcsm/internal/serverid"
)

// Server is one discovered Minecraft server. Optional fields may be
// empty strings if we couldn't determine them.
type Server struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Type           string      `json:"type"`
	Version        string      `json:"version"`
	Path           string      `json:"path"`
	DiscoveredRoot string      `json:"discovered_root"`
	LevelName      string      `json:"level_name"`
	IconB64        *string     `json:"icon_b64"`
	Ownership      Ownership   `json:"ownership"`
}

// Ownership mirrors docs/api.md §2.2. Only the State field is always
// present; the rest are zero values when not applicable.
type Ownership struct {
	State     lock.State `json:"state"`
	Instance  string     `json:"instance,omitempty"`
	Slot      string     `json:"slot,omitempty"`
	Host      string     `json:"host,omitempty"`
	PID       int        `json:"pid,omitempty"`
	Since     *time.Time `json:"since,omitempty"`
	Heartbeat *time.Time `json:"heartbeat,omitempty"`
}

// Catalog is the snapshot returned by Scan. Servers are sorted by Name
// then Path so list output is stable across calls.
type Catalog struct {
	ScannedAt time.Time `json:"scanned_at"`
	Servers   []Server  `json:"servers"`
}

// FindByID returns the server with the given id, or nil.
func (c *Catalog) FindByID(id string) *Server {
	for i := range c.Servers {
		if c.Servers[i].ID == id {
			return &c.Servers[i]
		}
	}
	return nil
}

// Scan walks every root and returns one Catalog with all valid servers.
// Errors on individual servers are logged but don't fail the whole scan
// — discovery should be best-effort so one broken directory can't take
// down the API. The instanceName + staleThreshold are needed to classify
// ownership state from this instance's perspective.
func Scan(roots []string, instanceName string, staleThreshold time.Duration) (*Catalog, error) {
	cat := &Catalog{ScannedAt: time.Now().UTC()}
	seenIDs := map[string]string{} // id → first path; used to flag duplicates

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			slog.Warn("discovery: skip root", "root", root, "err", err)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			serverDir := filepath.Join(root, e.Name())
			if !serverid.IsLikelyServerDir(serverDir) {
				continue
			}
			s, err := scanOne(serverDir, root, instanceName, staleThreshold)
			if err != nil {
				if errors.Is(err, serverid.ErrNotAServer) {
					// Has server.properties but not yet initialized — that's
					// fine, it just won't appear in the catalog. The operator
					// can run `mcsm migrate` to seed .mcsm/config.yaml.
					slog.Debug("discovery: server dir without .mcsm/config.yaml", "dir", serverDir)
					continue
				}
				slog.Warn("discovery: skip server dir", "dir", serverDir, "err", err)
				continue
			}
			if firstPath, dup := seenIDs[s.ID]; dup {
				slog.Warn("discovery: duplicate server id",
					"id", s.ID,
					"first_path", firstPath,
					"second_path", s.Path,
				)
				continue
			}
			seenIDs[s.ID] = s.Path
			cat.Servers = append(cat.Servers, *s)
		}
	}
	sort.Slice(cat.Servers, func(i, j int) bool {
		if cat.Servers[i].Name != cat.Servers[j].Name {
			return cat.Servers[i].Name < cat.Servers[j].Name
		}
		return cat.Servers[i].Path < cat.Servers[j].Path
	})
	return cat, nil
}

func scanOne(serverDir, root, instanceName string, staleThreshold time.Duration) (*Server, error) {
	cfg, err := serverid.Read(serverDir)
	if err != nil {
		return nil, err
	}

	level := ""
	if props, perr := serverid.ReadProperties(filepath.Join(serverDir, "server.properties")); perr == nil {
		level = props["level-name"]
	}

	// Re-detect flavor on every scan so a Vanilla → Paper migration is
	// picked up automatically. Falls back to whatever cfg.Type said.
	flavor, derr := serverid.DetectFlavor(serverDir)
	if derr != nil || flavor == serverid.FlavorUnknown {
		flavor = cfg.Type
	}

	owner, _ := lock.Read(serverDir)
	state := lock.Classify(owner, instanceName, staleThreshold)
	own := Ownership{State: state}
	if owner != nil && state != lock.StateFree {
		own.Instance = owner.Instance
		own.Slot = owner.Slot
		own.Host = owner.Host
		own.PID = owner.PID
		t1 := owner.StartedAt
		t2 := owner.Heartbeat
		own.Since = &t1
		own.Heartbeat = &t2
	}

	return &Server{
		ID:             cfg.ID,
		Name:           cfg.Name,
		Type:           flavor,
		Version:        "", // TODO: read from version_history.json (Paper) / jar manifest
		Path:           serverDir,
		DiscoveredRoot: root,
		LevelName:      level,
		IconB64:        nil, // TODO: read server-icon.png
		Ownership:      own,
	}, nil
}

// ErrNotConfigured indicates a server directory was found but had no
// .mcsm/config.yaml. Currently used only by tests/CLI; Scan() filters
// these out itself.
var ErrNotConfigured = fmt.Errorf("server directory has no %s", filepath.Join(serverid.ConfigDir, serverid.ConfigFile))
