// Package lock implements cross-instance ownership of Minecraft server
// directories. Each running server has a <server-dir>/.mcsm/owner.json
// file with a heartbeat that lets other mcsm instances see which one is
// driving that server, even when they share the directory over NFS or a
// bind mount. Phase 2A implements read + stale detection. Acquire /
// release / heartbeat land with the slot manager in Phase 2B.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// File is the on-disk filename inside .mcsm/.
const File = "owner.json"

// DefaultStaleAfter is how long without a heartbeat before a lock is
// considered stale (and eligible for steal). Heartbeat cadence in the
// holder is 10s, so 60s gives 6 missed beats — enough that brief GC
// pauses, NFS hiccups, or container restarts won't trigger a false steal.
const DefaultStaleAfter = 60 * time.Second

// Owner is the JSON shape on disk. Times are RFC 3339.
type Owner struct {
	Instance  string    `json:"instance"`
	Slot      string    `json:"slot"`
	Host      string    `json:"host"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Heartbeat time.Time `json:"heartbeat"`
}

// Path returns the absolute owner.json path for a server directory.
func Path(serverDir string) string {
	return filepath.Join(serverDir, ".mcsm", File)
}

// Read returns the parsed owner file, or (nil, nil) if it doesn't exist.
// A real IO/parse error propagates.
func Read(serverDir string) (*Owner, error) {
	p := Path(serverDir)
	raw, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var o Owner
	if err := json.Unmarshal(raw, &o); err != nil {
		// A garbled owner file shouldn't break discovery — return as if
		// nobody owns it. This case appears in logs but is recoverable.
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &o, nil
}

// IsStale reports whether the heartbeat is older than threshold.
// Callers may also want to confirm liveness with an SLP probe before
// stealing — this only checks file timestamps.
func IsStale(o *Owner, threshold time.Duration) bool {
	if o == nil {
		return false
	}
	return time.Since(o.Heartbeat) > threshold
}

// State enumerates the externally-visible ownership states from
// docs/api.md §2.2.
type State string

const (
	StateFree        State = "free"
	StateOwnedSelf   State = "owned-self"
	StateOwnedOther  State = "owned-other"
	StateStale       State = "stale"
)

// Classify returns the State for an owner record from the perspective of
// instanceName. Use DefaultStaleAfter for staleThreshold unless you have
// a reason to tighten it.
func Classify(o *Owner, instanceName string, staleThreshold time.Duration) State {
	if o == nil {
		return StateFree
	}
	if IsStale(o, staleThreshold) {
		return StateStale
	}
	if o.Instance == instanceName {
		return StateOwnedSelf
	}
	return StateOwnedOther
}

// Remove deletes the owner.json file. Used by:
//   - normal release (slot stop)
//   - explicit steal (DELETE /discovery/{id}/lock?force=true on a stale lock)
//   - explicit unlock-self (DELETE /discovery/{id}/lock when owned-self)
//
// Returns nil if the file is already absent.
func Remove(serverDir string) error {
	err := os.Remove(Path(serverDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
