package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/MertDalbudak/mcsm/internal/discovery"
	"github.com/MertDalbudak/mcsm/internal/lock"
	"github.com/MertDalbudak/mcsm/internal/peers"
)

type federationSource struct {
	Instance string `json:"instance"`
	Self     bool   `json:"self"`
	OK       bool   `json:"ok"`
	RTTMS    int64  `json:"rtt_ms,omitempty"`
	Error    string `json:"error,omitempty"`
}

// federationFetch runs a fetcher against every reachable peer in
// parallel. Returns the per-peer source rows and the per-peer raw
// responses. If a peer fails, OK=false and Error is populated.
func (s *Server) federationFetch(ctx context.Context,
	fetcher func(context.Context, *peers.Client) (map[string]any, error),
) ([]federationSource, map[string]map[string]any) {
	var (
		mu      sync.Mutex
		sources = []federationSource{}
		raws    = map[string]map[string]any{}
		wg      sync.WaitGroup
	)
	if s.peers != nil {
		for _, c := range s.peers.Clients() {
			c := c
			wg.Add(1)
			go func() {
				defer wg.Done()
				start := time.Now()
				pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				out, err := fetcher(pctx, c)
				mu.Lock()
				defer mu.Unlock()
				row := federationSource{Instance: c.Name(), RTTMS: time.Since(start).Milliseconds()}
				if err != nil {
					row.Error = err.Error()
				} else {
					row.OK = true
					raws[c.Name()] = out
				}
				sources = append(sources, row)
			}()
		}
	}
	wg.Wait()
	// Self always last in slice but conventionally listed first.
	sources = append([]federationSource{{Instance: s.cfg.Instance.Name, Self: true, OK: true}}, sources...)
	return sources, raws
}

type federationDiscoveryResp struct {
	ScannedAt time.Time           `json:"scanned_at"`
	Sources   []federationSource  `json:"sources"`
	Servers   []discovery.Server  `json:"servers"`
}

type federationServer struct {
	discovery.Server
	ReachableVia string `json:"reachable_via,omitempty"`
}

// GET /api/v1/federation/discovery
func (s *Server) handleFederationDiscovery(w http.ResponseWriter, r *http.Request) {
	sources, raws := s.federationFetch(r.Context(), func(ctx context.Context, c *peers.Client) (map[string]any, error) {
		return c.Discovery(ctx)
	})

	// Self
	selfCat := s.disco.Snapshot()
	merged := map[string]discovery.Server{}
	for _, sv := range selfCat.Servers {
		merged[sv.ID] = sv
	}

	// Merge each peer
	for instance, raw := range raws {
		servers, _ := raw["servers"].([]any)
		for _, item := range servers {
			b, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id, _ := b["id"].(string)
			if id == "" {
				continue
			}
			peerSrv := decodeRemoteServer(b)
			cur, present := merged[id]
			if !present {
				// DiscoveredRoot doesn't translate across instances —
				// clear it so clients don't treat it as locally-rooted.
				peerSrv.DiscoveredRoot = ""
				merged[id] = peerSrv
				continue
			}
			// Conflict resolution: pick the freshest heartbeat.
			if remoteFresher(peerSrv.Ownership, cur.Ownership) {
				merged[id] = peerSrv
			}
			_ = instance
		}
	}

	out := federationDiscoveryResp{
		ScannedAt: time.Now().UTC(),
		Sources:   sources,
	}
	for _, sv := range merged {
		out.Servers = append(out.Servers, sv)
	}
	WriteJSON(w, r, http.StatusOK, out)
}

// decodeRemoteServer projects a generic JSON object back into the typed
// discovery.Server shape. Any field we don't recognize is ignored.
func decodeRemoteServer(in map[string]any) discovery.Server {
	out := discovery.Server{}
	if v, ok := in["id"].(string); ok {
		out.ID = v
	}
	if v, ok := in["name"].(string); ok {
		out.Name = v
	}
	if v, ok := in["type"].(string); ok {
		out.Type = v
	}
	if v, ok := in["version"].(string); ok {
		out.Version = v
	}
	if v, ok := in["path"].(string); ok {
		out.Path = v
	}
	if v, ok := in["level_name"].(string); ok {
		out.LevelName = v
	}
	if own, ok := in["ownership"].(map[string]any); ok {
		out.Ownership = decodeOwnership(own)
	}
	return out
}

func decodeOwnership(in map[string]any) discovery.Ownership {
	out := discovery.Ownership{}
	if v, ok := in["state"].(string); ok {
		out.State = lock.State(v)
	}
	if v, ok := in["instance"].(string); ok {
		out.Instance = v
	}
	if v, ok := in["slot"].(string); ok {
		out.Slot = v
	}
	if v, ok := in["host"].(string); ok {
		out.Host = v
	}
	if v, ok := in["pid"].(float64); ok {
		out.PID = int(v)
	}
	if v, ok := in["heartbeat"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			out.Heartbeat = &t
		}
	}
	if v, ok := in["since"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			out.Since = &t
		}
	}
	return out
}

// remoteFresher returns true if the remote ownership record is newer
// than the current one (later heartbeat). Used to resolve federation
// duplicates per the spec ("freshest heartbeat wins").
func remoteFresher(remote, current discovery.Ownership) bool {
	if remote.Heartbeat == nil {
		return false
	}
	if current.Heartbeat == nil {
		return true
	}
	return remote.Heartbeat.After(*current.Heartbeat)
}

type federationSlotsResp struct {
	Sources []federationSource `json:"sources"`
	Slots   []map[string]any   `json:"slots"`
}

// GET /api/v1/federation/slots
//
// Each slot is decorated with `instance` so clients know where to
// address mutations.
func (s *Server) handleFederationSlots(w http.ResponseWriter, r *http.Request) {
	sources, raws := s.federationFetch(r.Context(), func(ctx context.Context, c *peers.Client) (map[string]any, error) {
		return c.Slots(ctx)
	})

	out := federationSlotsResp{Sources: sources, Slots: []map[string]any{}}

	// Self
	for _, snap := range s.slotMgr.Snapshots() {
		row := map[string]any{
			"instance":           s.cfg.Instance.Name,
			"name":               snap.Name,
			"port":               snap.Port,
			"public_address":     snap.PublicAddress,
			"state":              string(snap.State),
			"state_since":        snap.StateSince,
			"mounted_server_id":  snap.MountedServerID,
		}
		out.Slots = append(out.Slots, row)
	}

	for instance, raw := range raws {
		list, _ := raw["slots"].([]any)
		for _, item := range list {
			b, ok := item.(map[string]any)
			if !ok {
				continue
			}
			b["instance"] = instance
			out.Slots = append(out.Slots, b)
		}
	}
	WriteJSON(w, r, http.StatusOK, out)
}
