package discovery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MertDalbudak/mcsm/internal/lock"
)

// makeServer seeds a server directory under root with the given id, name,
// flavor jar, and optional owner.json.
func makeServer(t *testing.T, root, id, name, flavor string, owner *lock.Owner) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, ".mcsm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.properties"),
		[]byte("level-name=world\nmotd="+name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	jar := "server.jar"
	switch flavor {
	case "paper":
		jar = "paper.jar"
	case "fabric":
		jar = "fabric-server-launch.jar"
	}
	os.Create(filepath.Join(dir, jar))
	if err := os.WriteFile(filepath.Join(dir, ".mcsm", "config.yaml"),
		[]byte("id: "+id+"\nname: "+name+"\ntype: "+flavor+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if owner != nil {
		body, _ := json.Marshal(owner)
		os.WriteFile(filepath.Join(dir, ".mcsm", "owner.json"), body, 0o600)
	}
	return dir
}

func TestScan_FindsAllServers(t *testing.T) {
	root := t.TempDir()
	makeServer(t, root, "01900000-0000-7000-8000-000000000001", "alpha", "paper", nil)
	makeServer(t, root, "01900000-0000-7000-8000-000000000002", "bravo", "vanilla", nil)
	// Non-server directory: no server.properties, no .mcsm
	os.MkdirAll(filepath.Join(root, "junk"), 0o755)

	cat, err := Scan([]string{root}, "self", lock.DefaultStaleAfter)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Servers) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(cat.Servers), cat.Servers)
	}
	// Sorted by name then path.
	if cat.Servers[0].Name != "alpha" || cat.Servers[1].Name != "bravo" {
		t.Errorf("not sorted: %+v", cat.Servers)
	}
}

func TestScan_OwnershipClassification(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	stale := now.Add(-5 * time.Minute)

	makeServer(t, root, "id-free", "free", "paper", nil)
	makeServer(t, root, "id-self", "self", "paper", &lock.Owner{
		Instance: "me", Slot: "creative", Heartbeat: now, StartedAt: now,
	})
	makeServer(t, root, "id-other", "other", "paper", &lock.Owner{
		Instance: "node-b", Heartbeat: now, StartedAt: now,
	})
	makeServer(t, root, "id-stale", "stale", "paper", &lock.Owner{
		Instance: "node-z", Heartbeat: stale, StartedAt: stale,
	})

	cat, err := Scan([]string{root}, "me", lock.DefaultStaleAfter)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]lock.State{}
	for _, s := range cat.Servers {
		got[s.Name] = s.Ownership.State
	}
	cases := map[string]lock.State{
		"free":  lock.StateFree,
		"self":  lock.StateOwnedSelf,
		"other": lock.StateOwnedOther,
		"stale": lock.StateStale,
	}
	for name, want := range cases {
		if got[name] != want {
			t.Errorf("%s: got %s want %s", name, got[name], want)
		}
	}
}

func TestScan_DuplicateIDsKeepFirst(t *testing.T) {
	root := t.TempDir()
	makeServer(t, root, "same-id", "alpha", "paper", nil)
	makeServer(t, root, "same-id", "bravo", "paper", nil)
	cat, _ := Scan([]string{root}, "self", lock.DefaultStaleAfter)
	if len(cat.Servers) != 1 {
		t.Errorf("expected dedup, got %d", len(cat.Servers))
	}
}

func TestScan_BadRootSkipped(t *testing.T) {
	root := t.TempDir()
	makeServer(t, root, "id-1", "alpha", "paper", nil)
	cat, err := Scan([]string{root, "/no/such/path"}, "self", lock.DefaultStaleAfter)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Servers) != 1 {
		t.Errorf("missing root should skip, got %d servers", len(cat.Servers))
	}
}

func TestStore_Snapshot_ZeroBeforeRefresh(t *testing.T) {
	s := New([]string{t.TempDir()}, "self", time.Minute)
	cat := s.Snapshot()
	if cat == nil {
		t.Fatal("Snapshot should never be nil")
	}
	if !cat.ScannedAt.IsZero() {
		t.Errorf("expected zero ScannedAt before first scan, got %v", cat.ScannedAt)
	}
}

func TestStore_Refresh_PopulatesSnapshot(t *testing.T) {
	root := t.TempDir()
	makeServer(t, root, "id-x", "alpha", "paper", nil)
	s := New([]string{root}, "self", time.Minute)
	cat, err := s.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Servers) != 1 {
		t.Fatalf("got %d", len(cat.Servers))
	}
	snap := s.Snapshot()
	if len(snap.Servers) != 1 {
		t.Errorf("snapshot mismatch: %+v", snap)
	}
}

func TestCatalog_FindByID(t *testing.T) {
	c := &Catalog{Servers: []Server{{ID: "abc"}, {ID: "def"}}}
	if c.FindByID("abc") == nil {
		t.Error("expected to find abc")
	}
	if c.FindByID("xyz") != nil {
		t.Error("expected nil for missing id")
	}
}
