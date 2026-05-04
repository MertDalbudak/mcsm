package backup

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newServerDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	must(t, os.WriteFile(filepath.Join(d, "server.properties"), []byte("motd=hi\n"), 0o600))
	must(t, os.MkdirAll(filepath.Join(d, "world", "region"), 0o755))
	must(t, os.WriteFile(filepath.Join(d, "world", "level.dat"), []byte("level"), 0o600))
	must(t, os.WriteFile(filepath.Join(d, "world", "region", "r.0.0.mca"), []byte("region-data"), 0o600))
	must(t, os.MkdirAll(filepath.Join(d, "logs"), 0o755))
	must(t, os.WriteFile(filepath.Join(d, "logs", "latest.log"), []byte("log line"), 0o600))
	must(t, os.MkdirAll(filepath.Join(d, ".mcsm"), 0o755))
	must(t, os.WriteFile(filepath.Join(d, ".mcsm", "config.yaml"), []byte("id: x\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(d, ".mcsm", "owner.json"), []byte("{}"), 0o600))
	// junk that should be excluded
	must(t, os.WriteFile(filepath.Join(d, "heap.hprof"), []byte("dump"), 0o600))
	return d
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type fakeSaveControl struct {
	paused, resumed bool
	pauseErr        error
}

func (f *fakeSaveControl) Pause(context.Context) error  { f.paused = true; return f.pauseErr }
func (f *fakeSaveControl) Resume(context.Context) error { f.resumed = true; return nil }

func TestStore_CreateOnline_IncludesWorldExcludesJunk(t *testing.T) {
	src := newServerDir(t)
	store, err := New(filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	sc := &fakeSaveControl{}
	meta, err := store.Create(context.Background(), "srv1", src, CreateOptions{
		Label:       "before-update",
		Mode:        ModeOnline,
		IncludeLogs: false,
		SaveControl: sc,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !sc.paused || !sc.resumed {
		t.Errorf("save control not invoked: paused=%v resumed=%v", sc.paused, sc.resumed)
	}
	if meta.Mode != ModeOnline || meta.Label != "before-update" {
		t.Errorf("meta wrong: %+v", meta)
	}
	if meta.FileCount == 0 || meta.BytesTotal == 0 {
		t.Errorf("meta counts zero: %+v", meta)
	}
	// Inspect the archive.
	z, err := zip.OpenReader(filepath.Join(store.serverDir("srv1"), meta.ID+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	have := map[string]bool{}
	for _, f := range z.File {
		have[f.Name] = true
	}
	for _, want := range []string{
		"server.properties",
		"world/level.dat",
		"world/region/r.0.0.mca",
		".mcsm/config.yaml",
	} {
		if !have[want] {
			t.Errorf("expected %q in archive; got %v", want, keys(have))
		}
	}
	for _, banned := range []string{
		".mcsm/owner.json",
		"logs/latest.log",
		"heap.hprof",
	} {
		if have[banned] {
			t.Errorf("did not expect %q in archive (have keys: %v)", banned, keys(have))
		}
	}
}

func TestStore_CreateOnline_IncludeLogs(t *testing.T) {
	src := newServerDir(t)
	store, _ := New(filepath.Join(t.TempDir(), "backups"))
	meta, err := store.Create(context.Background(), "s", src, CreateOptions{
		Mode:        ModeOnline,
		IncludeLogs: true,
		SaveControl: &fakeSaveControl{},
	})
	if err != nil {
		t.Fatal(err)
	}
	z, _ := zip.OpenReader(filepath.Join(store.serverDir("s"), meta.ID+".zip"))
	defer z.Close()
	saw := false
	for _, f := range z.File {
		if f.Name == "logs/latest.log" {
			saw = true
		}
	}
	if !saw {
		t.Error("expected logs/latest.log when IncludeLogs=true")
	}
}

func TestStore_CreateOffline_NoSaveControlCall(t *testing.T) {
	src := newServerDir(t)
	store, _ := New(filepath.Join(t.TempDir(), "backups"))
	sc := &fakeSaveControl{}
	_, err := store.Create(context.Background(), "s", src, CreateOptions{
		Mode:        ModeOffline,
		SaveControl: sc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sc.paused || sc.resumed {
		t.Errorf("save control should not be invoked offline: %+v", sc)
	}
}

func TestStore_ListNewestFirst(t *testing.T) {
	src := newServerDir(t)
	store, _ := New(filepath.Join(t.TempDir(), "backups"))
	for _, label := range []string{"a", "b", "c"} {
		// space them out so timestamps differ
		_, err := store.Create(context.Background(), "s", src, CreateOptions{
			Label: label, Mode: ModeOffline,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.List("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("len=%d", len(list))
	}
	// Newest first → expect c, b, a (or all same second; tolerate any order if so).
	if !(list[0].CreatedAt.After(list[1].CreatedAt) || list[0].CreatedAt.Equal(list[1].CreatedAt)) {
		t.Errorf("not sorted newest-first: %v", list)
	}
}

func TestStore_GetMissing(t *testing.T) {
	store, _ := New(filepath.Join(t.TempDir(), "backups"))
	_, err := store.Get("s", "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_Delete(t *testing.T) {
	src := newServerDir(t)
	store, _ := New(filepath.Join(t.TempDir(), "backups"))
	meta, _ := store.Create(context.Background(), "s", src, CreateOptions{Mode: ModeOffline})
	if err := store.Delete("s", meta.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.zipPath("s", meta.ID)); !os.IsNotExist(err) {
		t.Errorf("zip not removed: %v", err)
	}
	if _, err := os.Stat(store.metaPath("s", meta.ID)); !os.IsNotExist(err) {
		t.Errorf("meta not removed: %v", err)
	}
	// Delete again is idempotent? No — we return ErrNotFound on second.
	if err := store.Delete("s", meta.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on second delete, got %v", err)
	}
}

func TestStore_Roundtrip_BackupRestore(t *testing.T) {
	src := newServerDir(t)
	store, _ := New(filepath.Join(t.TempDir(), "backups"))
	meta, err := store.Create(context.Background(), "s", src, CreateOptions{Mode: ModeOffline})
	if err != nil {
		t.Fatal(err)
	}
	// Fresh, empty target dir.
	target := t.TempDir()
	if err := store.Restore(context.Background(), "s", meta.ID, target); err != nil {
		t.Fatal(err)
	}
	// Spot-check restored files.
	for _, want := range []string{"server.properties", "world/level.dat", "world/region/r.0.0.mca"} {
		if _, err := os.Stat(filepath.Join(target, want)); err != nil {
			t.Errorf("missing %s after restore: %v", want, err)
		}
	}
}

func TestStore_Restore_PathTraversalRefused(t *testing.T) {
	store, _ := New(filepath.Join(t.TempDir(), "backups"))
	// Build a malicious archive by hand.
	srvDir := filepath.Join(t.TempDir(), "srv")
	os.MkdirAll(srvDir, 0o755)
	zipPath := store.zipPath("s", "evil")
	os.MkdirAll(filepath.Dir(zipPath), 0o755)
	zf, _ := os.Create(zipPath)
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("../../etc/evil")
	w.Write([]byte("nope"))
	zw.Close()
	zf.Close()
	// Also seed a metadata file so internal path bookkeeping is happy.
	os.WriteFile(store.metaPath("s", "evil"), []byte(`{"id":"evil","server_id":"s"}`), 0o600)

	target := t.TempDir()
	err := store.Restore(context.Background(), "s", "evil", target)
	if err == nil || !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "escape") {
		t.Errorf("expected refusal of path traversal, got: %v", err)
	}
}

func TestSanitizeLabel(t *testing.T) {
	cases := map[string]string{
		"hello world":          "hello-world",
		"file/with/slashes":    "filewithslashes",
		"with-dash_and_under":  "with-dash_and_under",
		"weird ! chars @ here": "weird--chars--here",
	}
	for in, want := range cases {
		if got := sanitizeLabel(in); got != want {
			t.Errorf("sanitizeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
