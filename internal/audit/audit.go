// Package audit records mutating API operations to an append-only JSONL
// file, plus an in-memory ring of the most recent entries for fast
// /audit responses without scanning the whole file.
//
// Layout: <data_dir>/audit-YYYY-MM.jsonl, one JSON record per line.
// Rotation is by month, automatic — the writer chooses the file for
// each Append based on time.Now().
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Entry is one audit record. Shape matches docs/api.md §8 audit response.
type Entry struct {
	ID      uint64         `json:"id"`
	At      time.Time      `json:"at"`
	Actor   Actor          `json:"actor"`
	Kind    string         `json:"kind"`     // e.g. "slot.start"
	Subject map[string]any `json:"subject"`  // route-specific
	Result  string         `json:"result"`   // "ok" | "error"
	Status  int            `json:"status"`   // HTTP status code
	TraceID string         `json:"trace_id,omitempty"`
}

type Actor struct {
	Kind string `json:"kind"`           // "token" | "system"
	Name string `json:"name,omitempty"` // token name
}

// Logger persists Entry records and answers cursor-paginated queries.
type Logger struct {
	dir       string
	retention time.Duration

	mu      sync.RWMutex
	nextID  uint64
	ring    []Entry // last RingCap entries; oldest first
	ringCap int

	stopJanitor chan struct{}
}

// RingCap is how many recent entries the logger keeps in memory.
const RingCap = 1000

// New creates the logger. Creates dir if needed.
func New(dir string, retention time.Duration) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("audit: mkdir %s: %w", dir, err)
	}
	l := &Logger{
		dir:         dir,
		retention:   retention,
		ringCap:     RingCap,
		ring:        make([]Entry, 0, RingCap),
		stopJanitor: make(chan struct{}),
	}
	// Seed nextID from the most recent file's max id, so IDs are
	// monotonic across process restarts.
	id, err := l.scanMaxID()
	if err != nil {
		slog.Warn("audit: scanMaxID", "err", err)
	}
	l.nextID = id + 1
	// Backfill the ring from the latest file so /audit returns
	// recent entries even immediately after boot.
	l.backfillRing()
	return l, nil
}

// Append writes one entry and returns the ID assigned to it.
// Errors are logged but never propagate — audit failures must never
// break the API response.
func (l *Logger) Append(ctx context.Context, e Entry) uint64 {
	l.mu.Lock()
	e.ID = l.nextID
	l.nextID++
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	l.appendRingLocked(e)
	l.mu.Unlock()

	// Write to disk outside the lock.
	if err := l.writeFile(e); err != nil {
		slog.Error("audit: write", "err", err, "id", e.ID, "kind", e.Kind)
	}
	return e.ID
}

func (l *Logger) appendRingLocked(e Entry) {
	if len(l.ring) == l.ringCap {
		copy(l.ring, l.ring[1:])
		l.ring[len(l.ring)-1] = e
	} else {
		l.ring = append(l.ring, e)
	}
}

// Query is a filter for List.
type Query struct {
	Since  time.Time
	Until  time.Time
	Actor  string
	Kind   string
	Limit  int    // ≤ 1000; default 200
	Cursor string // entry id; results are "newer than" cursor
}

// List returns the most recent entries matching q, plus a cursor for
// pagination (the newest returned entry's id; pass back as Cursor).
//
// Reads from the in-memory ring first; if Since pre-dates the ring,
// falls back to reading on-disk files (newest first).
func (l *Logger) List(q Query) (entries []Entry, nextCursor string) {
	if q.Limit <= 0 {
		q.Limit = 200
	}
	if q.Limit > 1000 {
		q.Limit = 1000
	}
	cursor := uint64(0)
	if q.Cursor != "" {
		if v, err := strconv.ParseUint(q.Cursor, 10, 64); err == nil {
			cursor = v
		}
	}

	l.mu.RLock()
	ring := append([]Entry(nil), l.ring...)
	l.mu.RUnlock()

	out := make([]Entry, 0, q.Limit)
	// Walk newest → oldest (UI typically wants newest first).
	for i := len(ring) - 1; i >= 0; i-- {
		e := ring[i]
		if e.ID <= cursor {
			break
		}
		if !matches(e, q) {
			continue
		}
		out = append(out, e)
		if len(out) == q.Limit {
			break
		}
	}

	if len(out) > 0 {
		nextCursor = strconv.FormatUint(out[len(out)-1].ID, 10)
	}
	return out, nextCursor
}

func matches(e Entry, q Query) bool {
	if !q.Since.IsZero() && e.At.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && e.At.After(q.Until) {
		return false
	}
	if q.Actor != "" && e.Actor.Name != q.Actor {
		return false
	}
	if q.Kind != "" && e.Kind != q.Kind {
		return false
	}
	return true
}

// RunJanitor periodically deletes audit files older than retention.
// Runs daily; cheap.
func (l *Logger) RunJanitor(ctx context.Context) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	l.gc()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.gc()
		}
	}
}

// --- internals ---

func (l *Logger) currentFile() string {
	return filepath.Join(l.dir, "audit-"+time.Now().UTC().Format("2006-01")+".jsonl")
}

func (l *Logger) writeFile(e Entry) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	f, err := os.OpenFile(l.currentFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(body)
	return err
}

func (l *Logger) scanMaxID() (uint64, error) {
	files, err := os.ReadDir(l.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	maxID := uint64(0)
	for _, fi := range files {
		if fi.IsDir() {
			continue
		}
		name := fi.Name()
		if len(name) < 6 || name[:6] != "audit-" {
			continue
		}
		f, err := os.Open(filepath.Join(l.dir, name))
		if err != nil {
			continue
		}
		// Cheap last-line scan via tail: read whole file (audit files
		// are at most ~1MB/month for moderate fleets).
		body, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}
		// Walk backward to find the last non-empty line.
		end := len(body)
		for end > 0 && (body[end-1] == '\n' || body[end-1] == '\r') {
			end--
		}
		start := end
		for start > 0 && body[start-1] != '\n' {
			start--
		}
		if start == end {
			continue
		}
		var e Entry
		if err := json.Unmarshal(body[start:end], &e); err == nil && e.ID > maxID {
			maxID = e.ID
		}
	}
	return maxID, nil
}

func (l *Logger) backfillRing() {
	path := l.currentFile()
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		return
	}
	// Last RingCap lines.
	lines := splitLinesLast(body, l.ringCap)
	for _, line := range lines {
		var e Entry
		if err := json.Unmarshal(line, &e); err == nil {
			l.ring = append(l.ring, e)
		}
	}
}

func splitLinesLast(b []byte, n int) [][]byte {
	out := make([][]byte, 0)
	end := len(b)
	for end > 0 && len(out) < n {
		// trim trailing \n
		for end > 0 && (b[end-1] == '\n' || b[end-1] == '\r') {
			end--
		}
		if end == 0 {
			break
		}
		start := end
		for start > 0 && b[start-1] != '\n' {
			start--
		}
		out = append([][]byte{b[start:end]}, out...)
		end = start
	}
	return out
}

func (l *Logger) gc() {
	if l.retention <= 0 {
		return
	}
	cutoff := time.Now().UTC().Add(-l.retention)
	files, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	for _, fi := range files {
		if fi.IsDir() {
			continue
		}
		info, err := fi.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(l.dir, fi.Name()))
		}
	}
}
