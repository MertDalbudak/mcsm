package peers

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/MertDalbudak/mcsm/internal/config"
)

// PeerStatus is the externally-visible reachability record for one peer.
type PeerStatus struct {
	Name          string     `json:"name"`
	URL           string     `json:"url"`
	Reachable     bool       `json:"reachable"`
	LastSeen      *time.Time `json:"last_seen,omitempty"`
	LastAttempt   time.Time  `json:"last_attempt"`
	RTTMS         int64      `json:"rtt_ms,omitempty"`
	RemoteVersion string     `json:"remote_version,omitempty"`
	RemoteName    string     `json:"remote_name,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

// PingObserver is called after every individual ping. Used to drive
// Prometheus collectors without making the peers package depend on
// internal/metrics.
type PingObserver func(name string, reachable bool, rttMS int64)

// Pool owns the configured peer set + the most recent ping result for
// each. Goroutine-safe.
type Pool struct {
	clients      []*Client
	pingInterval time.Duration
	timeout      time.Duration
	observer     PingObserver

	mu     sync.RWMutex
	status map[string]*PeerStatus
}

// SetObserver registers a callback fired after every individual ping.
// Pass nil to clear. Goroutine-safe.
func (p *Pool) SetObserver(o PingObserver) {
	p.mu.Lock()
	p.observer = o
	p.mu.Unlock()
}

// NewPool builds clients from cfg.Peers.
func NewPool(cfg config.PeersConfig) *Pool {
	p := &Pool{
		pingInterval: cfg.PingInterval,
		timeout:      cfg.Timeout,
		status:       map[string]*PeerStatus{},
	}
	for _, pc := range cfg.Peers {
		c := NewClient(pc.Name, pc.URL, pc.Token, p.timeout)
		p.clients = append(p.clients, c)
		p.status[pc.Name] = &PeerStatus{Name: pc.Name, URL: c.URL()}
	}
	return p
}

// Clients returns the configured client set in declaration order.
func (p *Pool) Clients() []*Client { return p.clients }

// Status returns a copy of every peer's most recent ping record, sorted
// by name for stable JSON output.
func (p *Pool) Status() []PeerStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]PeerStatus, 0, len(p.status))
	for _, s := range p.status {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Run blocks, pinging every peer on the configured interval until ctx
// cancels. Performs an immediate first round so fresh boots have data.
func (p *Pool) Run(ctx context.Context) {
	if len(p.clients) == 0 {
		return
	}
	p.PingAll(ctx)
	t := time.NewTicker(p.pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.PingAll(ctx)
		}
	}
}

// PingAll pings every peer in parallel. Used by Run and by the
// /peers/refresh endpoint.
func (p *Pool) PingAll(ctx context.Context) []PeerStatus {
	var wg sync.WaitGroup
	for _, c := range p.clients {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.pingOne(ctx, c)
		}()
	}
	wg.Wait()
	return p.Status()
}

func (p *Pool) pingOne(ctx context.Context, c *Client) {
	pctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	start := time.Now().UTC()
	res, err := c.Ping(pctx)

	p.mu.Lock()
	st := p.status[c.Name()]
	if st == nil {
		p.mu.Unlock()
		return
	}
	st.LastAttempt = time.Now().UTC()
	if err != nil {
		st.Reachable = false
		st.LastError = err.Error()
		st.RTTMS = 0
		obs := p.observer
		p.mu.Unlock()
		if obs != nil {
			obs(c.Name(), false, 0)
		}
		slog.Debug("peer unreachable", "peer", c.Name(), "err", err)
		return
	}
	now := time.Now().UTC()
	st.Reachable = true
	st.LastError = ""
	st.LastSeen = &now
	st.RTTMS = time.Since(start).Milliseconds()
	st.RemoteName = res.Name
	st.RemoteVersion = res.Version
	rtt := st.RTTMS
	obs := p.observer
	p.mu.Unlock()
	if obs != nil {
		obs(c.Name(), true, rtt)
	}
}

// Get returns the client with the given name, or nil. Used by handlers
// that proxy a specific peer.
func (p *Pool) Get(name string) *Client {
	for _, c := range p.clients {
		if c.Name() == name {
			return c
		}
	}
	return nil
}
