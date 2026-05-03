package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/MertDalbudak/mcsm/internal/lock"
)

// Store is a thread-safe wrapper around the latest Catalog. It refreshes
// itself in the background on the configured interval, and exposes
// Snapshot() for handlers and Refresh() for forced re-scans (POST
// /discovery/refresh, or whenever a slot operation needs a fresh view).
type Store struct {
	roots          []string
	instanceName   string
	staleThreshold time.Duration
	interval       time.Duration

	mu  sync.RWMutex
	cur *Catalog
}

// New constructs a Store. The first scan happens during NewWithFirstScan;
// New only sets up state, so the constructor itself can't return errors.
func New(roots []string, instanceName string, interval time.Duration) *Store {
	return &Store{
		roots:          roots,
		instanceName:   instanceName,
		staleThreshold: lock.DefaultStaleAfter,
		interval:       interval,
		cur:            &Catalog{ScannedAt: time.Time{}},
	}
}

// Snapshot returns the most recently scanned catalog. Never nil; an
// uninitialized store returns a Catalog with zero ScannedAt.
func (s *Store) Snapshot() *Catalog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Refresh forces an immediate scan and stores the result. Returns the
// new catalog (also retrievable via Snapshot).
func (s *Store) Refresh(ctx context.Context) (*Catalog, error) {
	cat, err := Scan(s.roots, s.instanceName, s.staleThreshold)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cur = cat
	s.mu.Unlock()
	slog.Debug("discovery refreshed", "servers", len(cat.Servers))
	return cat, nil
}

// Run blocks, refreshing on the configured interval until ctx is canceled.
// Performs an immediate first scan before the timer loop so handlers
// don't see an empty catalog at boot.
func (s *Store) Run(ctx context.Context) {
	if _, err := s.Refresh(ctx); err != nil {
		slog.Error("discovery: initial scan", "err", err)
	}
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.Refresh(ctx); err != nil {
				slog.Error("discovery: scheduled scan", "err", err)
			}
		}
	}
}
