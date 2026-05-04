package serverid

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadProperties(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "server.properties")
	body := `# This is a comment
! Bang comment
motd=Hello World
max-players=20
white-list=true
empty-value=
   spaced-key  =   spaced-value
key:colon-style=ok
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	props, err := ReadProperties(p)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"motd":             "Hello World",
		"max-players":      "20",
		"white-list":       "true",
		"empty-value":      "",
		"spaced-key":       "spaced-value",
		"key":              "colon-style=ok",
	}
	// "key" matches because the colon comes before the equals — we
	// take the earliest separator.
	for k, want := range cases {
		if got := props[k]; got != want {
			t.Errorf("%q: got %q want %q", k, got, want)
		}
	}
	if _, ok := props["This is a comment"]; ok {
		t.Error("comments should not be parsed")
	}
}

func TestPatchProperties_PreservesAndUpdates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "server.properties")
	original := `# top comment
motd=Old MOTD

max-players=10
view-distance=8
`
	if err := os.WriteFile(p, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PatchProperties(dir, map[string]string{
		"motd":         "New MOTD",
		"new-key":      "appended",
		"max-players":  "20",
	}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	body := string(out)

	// Comments preserved
	if !strings.Contains(body, "# top comment") {
		t.Error("comment lost")
	}
	// Untouched key intact
	if !strings.Contains(body, "view-distance=8") {
		t.Error("untouched key altered")
	}
	// Updated keys
	if !strings.Contains(body, "motd=New MOTD") {
		t.Errorf("motd not updated: %s", body)
	}
	if !strings.Contains(body, "max-players=20") {
		t.Errorf("max-players not updated: %s", body)
	}
	if strings.Contains(body, "max-players=10") {
		t.Errorf("old value still present: %s", body)
	}
	// Appended new key
	if !strings.Contains(body, "new-key=appended") {
		t.Errorf("new key not appended: %s", body)
	}
}

func TestPatchProperties_AtomicReplace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(p, []byte("k=v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PatchProperties(dir, map[string]string{"k": "v2"}); err != nil {
		t.Fatal(err)
	}
	// Confirm no stragglers (the temp file should have been renamed).
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".server.properties.tmp") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}

func TestDetectFlavor(t *testing.T) {
	cases := []struct {
		name string
		seed func(dir string)
		want string
	}{
		{"paper-stable", func(d string) { os.Create(filepath.Join(d, "paper.jar")) }, FlavorPaper},
		{"paper-versioned", func(d string) { os.Create(filepath.Join(d, "paper-1.21.4-1.jar")) }, FlavorPaper},
		{"paper-vh-only", func(d string) { os.Create(filepath.Join(d, "version_history.json")) }, FlavorPaper},
		{"vanilla", func(d string) { os.Create(filepath.Join(d, "server.jar")) }, FlavorVanilla},
		{"fabric", func(d string) { os.Create(filepath.Join(d, "fabric-server-launch.jar")) }, FlavorFabric},
		{"forge-jar", func(d string) { os.Create(filepath.Join(d, "forge-1.20-installer.jar")) }, FlavorForge},
		{"unknown", func(d string) {}, FlavorUnknown},
		{"fabric-wins-over-paper", func(d string) {
			os.Create(filepath.Join(d, "fabric-server-launch.jar"))
			os.Create(filepath.Join(d, "paper.jar"))
		}, FlavorFabric},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.seed(dir)
			got, err := DetectFlavor(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestIsLikelyServerDir(t *testing.T) {
	t.Run("with_properties", func(t *testing.T) {
		d := t.TempDir()
		os.Create(filepath.Join(d, "server.properties"))
		if !IsLikelyServerDir(d) {
			t.Error("expected true")
		}
	})
	t.Run("with_mcsm_config", func(t *testing.T) {
		d := t.TempDir()
		os.MkdirAll(filepath.Join(d, ConfigDir), 0o755)
		os.Create(filepath.Join(d, ConfigDir, ConfigFile))
		if !IsLikelyServerDir(d) {
			t.Error("expected true")
		}
	})
	t.Run("empty_dir", func(t *testing.T) {
		if IsLikelyServerDir(t.TempDir()) {
			t.Error("expected false")
		}
	})
}

func TestRead_NotAServer(t *testing.T) {
	d := t.TempDir()
	_, err := Read(d)
	if !errors.Is(err, ErrNotAServer) {
		t.Errorf("expected ErrNotAServer, got %v", err)
	}
}

func TestInitialize_GeneratesIDAndFlavor(t *testing.T) {
	d := t.TempDir()
	os.Create(filepath.Join(d, "paper.jar"))
	cfg, err := Initialize(d, "Test Server")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ID == "" {
		t.Error("ID not generated")
	}
	if cfg.Type != FlavorPaper {
		t.Errorf("Type = %s, want paper", cfg.Type)
	}
	if cfg.Name != "Test Server" {
		t.Errorf("Name = %q", cfg.Name)
	}
	// Re-initialize should be a no-op (return existing).
	cfg2, err := Initialize(d, "Different Name")
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.ID != cfg.ID {
		t.Errorf("ID changed on re-initialize: %q vs %q", cfg.ID, cfg2.ID)
	}
}

func TestPlanLaunch(t *testing.T) {
	cases := []struct {
		name    string
		flavor  string
		seed    func(dir string)
		wantJar string
		wantErr bool
	}{
		{"paper-stable", FlavorPaper, func(d string) { os.Create(filepath.Join(d, "paper.jar")) }, "paper.jar", false},
		{"paper-versioned", FlavorPaper, func(d string) {
			os.Create(filepath.Join(d, "paper-1.21.0-1.jar"))
			os.Create(filepath.Join(d, "paper-1.21.4-50.jar"))
		}, "paper-1.21.4-50.jar", false},
		{"vanilla", FlavorVanilla, func(d string) { os.Create(filepath.Join(d, "server.jar")) }, "server.jar", false},
		{"fabric", FlavorFabric, func(d string) {
			os.Create(filepath.Join(d, "fabric-server-launch.jar"))
		}, "fabric-server-launch.jar", false},
		{"missing-paper", FlavorPaper, func(d string) {}, "", true},
		{"unknown-flavor", "exotic", func(d string) { os.Create(filepath.Join(d, "x.jar")) }, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			tc.seed(d)
			cfg := &Config{Type: tc.flavor}
			plan, err := PlanLaunch(cfg, d)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if plan.Command != "java" {
				t.Errorf("Command: %s", plan.Command)
			}
			// jar should be the last positional arg before "nogui"
			if plan.Args[len(plan.Args)-1] != "nogui" {
				t.Errorf("expected last arg nogui, got %v", plan.Args)
			}
			if plan.Args[len(plan.Args)-2] != tc.wantJar {
				t.Errorf("jar: got %q want %q", plan.Args[len(plan.Args)-2], tc.wantJar)
			}
		})
	}
}

func TestPlanLaunch_DefaultJVMArgs(t *testing.T) {
	d := t.TempDir()
	os.Create(filepath.Join(d, "paper.jar"))
	cfg := &Config{Type: FlavorPaper}
	plan, err := PlanLaunch(cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Args, "-XX:+UseG1GC") {
		t.Errorf("expected default Aikar's flags, got %v", plan.Args)
	}
}

func TestPlanLaunch_CustomJVMArgs(t *testing.T) {
	d := t.TempDir()
	os.Create(filepath.Join(d, "paper.jar"))
	cfg := &Config{Type: FlavorPaper, Java: JavaConfig{Args: []string{"-Xmx2G"}}}
	plan, err := PlanLaunch(cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Args, "-Xmx2G") {
		t.Errorf("custom args missing: %v", plan.Args)
	}
	if contains(plan.Args, "-XX:+UseG1GC") {
		t.Errorf("default args should not be merged when custom set: %v", plan.Args)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
