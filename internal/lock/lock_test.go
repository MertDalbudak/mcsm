package lock

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupServerDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, ".mcsm"), 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

func writeOwner(t *testing.T, dir string, o Owner) {
	t.Helper()
	body, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".mcsm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRead_NoFile(t *testing.T) {
	dir := setupServerDir(t)
	o, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if o != nil {
		t.Errorf("expected nil owner, got %+v", o)
	}
}

func TestRead_Garbled(t *testing.T) {
	dir := setupServerDir(t)
	if err := os.WriteFile(Path(dir), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Read(dir)
	if err == nil {
		t.Error("expected parse error")
	}
}

func TestClassify(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name     string
		owner    *Owner
		instance string
		want     State
	}{
		{"free", nil, "node-a", StateFree},
		{"owned-self", &Owner{Instance: "node-a", Heartbeat: now}, "node-a", StateOwnedSelf},
		{"owned-other", &Owner{Instance: "node-b", Heartbeat: now}, "node-a", StateOwnedOther},
		{"stale", &Owner{Instance: "node-b", Heartbeat: now.Add(-2 * time.Minute)}, "node-a", StateStale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.owner, tc.instance, DefaultStaleAfter)
			if got != tc.want {
				t.Errorf("Classify: got %s want %s", got, tc.want)
			}
		})
	}
}

func TestTryAcquire_Free(t *testing.T) {
	dir := setupServerDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := TryAcquire(ctx, dir, "node-a", "creative", "host", 1234, false)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	defer h.Release()
	o, _ := Read(dir)
	if o == nil || o.Instance != "node-a" || o.Slot != "creative" {
		t.Errorf("owner persisted incorrectly: %+v", o)
	}
}

func TestTryAcquire_AlreadyHeldByOther(t *testing.T) {
	dir := setupServerDir(t)
	writeOwner(t, dir, Owner{
		Instance: "node-b", Slot: "other", Host: "h",
		PID: 1, StartedAt: time.Now().UTC(), Heartbeat: time.Now().UTC(),
	})
	_, err := TryAcquire(context.Background(), dir, "node-a", "creative", "h", 1, false)
	if !errors.Is(err, ErrAlreadyHeld) {
		t.Errorf("expected ErrAlreadyHeld, got %v", err)
	}
}

func TestTryAcquire_StaleWithoutForce(t *testing.T) {
	dir := setupServerDir(t)
	writeOwner(t, dir, Owner{
		Instance: "node-b", Slot: "other", Heartbeat: time.Now().Add(-5 * time.Minute),
	})
	_, err := TryAcquire(context.Background(), dir, "node-a", "creative", "h", 1, false)
	if !errors.Is(err, ErrAlreadyHeld) || !strings.Contains(err.Error(), "stale") {
		t.Errorf("expected stale ErrAlreadyHeld, got %v", err)
	}
}

func TestTryAcquire_StaleWithForceSucceeds(t *testing.T) {
	dir := setupServerDir(t)
	writeOwner(t, dir, Owner{
		Instance: "node-b", Slot: "other", Heartbeat: time.Now().Add(-5 * time.Minute),
	})
	h, err := TryAcquire(context.Background(), dir, "node-a", "creative", "h", 1, true)
	if err != nil {
		t.Fatalf("TryAcquire force: %v", err)
	}
	defer h.Release()
	o, _ := Read(dir)
	if o.Instance != "node-a" {
		t.Errorf("steal failed: %+v", o)
	}
}

func TestTryAcquire_OwnedSelfReentry(t *testing.T) {
	dir := setupServerDir(t)
	h1, err := TryAcquire(context.Background(), dir, "node-a", "creative", "h", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	first := h1.Owner().StartedAt
	time.Sleep(10 * time.Millisecond)
	// re-acquire by same (instance, slot) should refresh, not fail
	h2, err := TryAcquire(context.Background(), dir, "node-a", "creative", "h", 1, false)
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if !h2.Owner().Heartbeat.After(first) {
		t.Error("expected heartbeat to advance on re-acquire")
	}
	h2.Release()
}

func TestTryAcquire_OwnedSelfDifferentSlot(t *testing.T) {
	dir := setupServerDir(t)
	h, err := TryAcquire(context.Background(), dir, "node-a", "slot-1", "h", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()
	_, err = TryAcquire(context.Background(), dir, "node-a", "slot-2", "h", 1, false)
	if !errors.Is(err, ErrAlreadyHeld) {
		t.Errorf("expected ErrAlreadyHeld for cross-slot, got %v", err)
	}
}

func TestRelease_RemovesFile(t *testing.T) {
	dir := setupServerDir(t)
	h, err := TryAcquire(context.Background(), dir, "node-a", "s", "h", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Errorf("owner.json should be gone, stat err: %v", err)
	}
}

func TestRemove_Idempotent(t *testing.T) {
	dir := setupServerDir(t)
	if err := Remove(dir); err != nil {
		t.Errorf("Remove on missing file should be noop, got: %v", err)
	}
}
