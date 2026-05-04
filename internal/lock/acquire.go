package lock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HeartbeatInterval is how often a held lock refreshes its heartbeat
// timestamp. 10s gives a 6× safety margin against DefaultStaleAfter.
const HeartbeatInterval = 10 * time.Second

// ErrAlreadyHeld is returned when TryAcquire finds a fresh owner.json
// from another instance. Callers should report 409 server_in_use.
var ErrAlreadyHeld = errors.New("server is already locked by another instance")

// Held is a held lock. The heartbeat goroutine runs until Release is
// called or ctx is canceled.
type Held struct {
	serverDir string
	owner     Owner

	cancel context.CancelFunc
	done   chan struct{}

	mu sync.Mutex
}

// TryAcquire attempts to write a fresh owner.json for serverDir.
// Behavior by current state:
//
//   - free: writes owner.json and returns Held.
//   - stale: if force=true, removes the stale lock and acquires; else
//     returns ErrAlreadyHeld.
//   - owned-self (this instance, this slot): refreshes the existing lock
//     and returns Held — idempotent re-acquire.
//   - owned-self (this instance, different slot): returns ErrAlreadyHeld;
//     the operator must release first (something is misconfigured).
//   - owned-other (fresh): returns ErrAlreadyHeld regardless of force.
//
// Acquire is racy across instances on its own — two writers can both see
// "free" and both succeed. The protocol relies on Minecraft's own
// session.lock binding the world directory; the second java instance
// will fail to start, the slot will transition to crashed, and the
// loser's heartbeat goroutine will release. For shared NFS mounts this
// is good enough; if you need stricter ordering, route start requests
// through a single coordinating instance.
func TryAcquire(ctx context.Context, serverDir, instance, slot, host string, pid int, force bool) (*Held, error) {
	cur, err := Read(serverDir)
	if err != nil {
		return nil, err
	}
	state := Classify(cur, instance, DefaultStaleAfter)
	switch state {
	case StateOwnedSelf:
		if cur.Slot != slot {
			return nil, fmt.Errorf("%w: held by self under slot %q", ErrAlreadyHeld, cur.Slot)
		}
		// fall through and rewrite with fresh heartbeat
	case StateStale:
		if !force {
			return nil, fmt.Errorf("%w: stale lock from %s/%s (heartbeat %s ago); force to steal",
				ErrAlreadyHeld, cur.Instance, cur.Slot, time.Since(cur.Heartbeat).Round(time.Second))
		}
		slog.Warn("lock: stealing stale lock",
			"server_dir", serverDir,
			"prev_instance", cur.Instance,
			"prev_slot", cur.Slot,
			"heartbeat_age", time.Since(cur.Heartbeat).String(),
		)
	case StateOwnedOther:
		return nil, fmt.Errorf("%w: held by %s/%s, heartbeat %s ago",
			ErrAlreadyHeld, cur.Instance, cur.Slot, time.Since(cur.Heartbeat).Round(time.Second))
	case StateFree:
		// proceed
	}

	now := time.Now().UTC()
	o := Owner{
		Instance:  instance,
		Slot:      slot,
		Host:      host,
		PID:       pid,
		StartedAt: now,
		Heartbeat: now,
	}
	if err := writeOwnerAtomic(serverDir, &o); err != nil {
		return nil, err
	}

	hbCtx, cancel := context.WithCancel(ctx)
	h := &Held{
		serverDir: serverDir,
		owner:     o,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	go h.heartbeatLoop(hbCtx)
	return h, nil
}

// Refresh updates the heartbeat timestamp on disk. Called by the
// internal heartbeat loop; exposed for tests and for forced refresh
// after a slow operation that we don't want to look like a stall.
func (h *Held) Refresh() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.owner.Heartbeat = time.Now().UTC()
	return writeOwnerAtomic(h.serverDir, &h.owner)
}

// Release stops the heartbeat goroutine and removes owner.json. Safe to
// call multiple times.
func (h *Held) Release() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
		<-h.done
	}
	return Remove(h.serverDir)
}

// Owner returns a snapshot of the lock's owner record.
func (h *Held) Owner() Owner {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.owner
}

func (h *Held) heartbeatLoop(ctx context.Context) {
	defer close(h.done)
	t := time.NewTicker(HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := h.Refresh(); err != nil {
				slog.Error("lock: heartbeat refresh",
					"server_dir", h.serverDir,
					"err", err,
				)
			}
		}
	}
}

// writeOwnerAtomic writes owner.json via temp-file + rename. The temp
// file lives in the same directory as the final file so rename is
// guaranteed atomic on the same filesystem. Creates .mcsm/ if absent.
func writeOwnerAtomic(serverDir string, o *Owner) error {
	dir := filepath.Join(serverDir, ".mcsm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	body, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal owner: %w", err)
	}
	final := filepath.Join(dir, File)
	tmp, err := os.CreateTemp(dir, ".owner.json.tmp.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, final); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
