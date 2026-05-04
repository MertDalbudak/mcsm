package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubPaperAPI mounts a fake /v2/projects/paper/... endpoint that
// returns the build list, build detail, and download bytes.
func stubPaperAPI(t *testing.T, mcVersion string, builds []int, jarBytes []byte) (string, string) {
	t.Helper()
	jarSum := sha256.Sum256(jarBytes)
	jarSumHex := hex.EncodeToString(jarSum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/projects/paper/versions/"+mcVersion, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"builds": builds})
	})
	mux.HandleFunc("/v2/projects/paper/versions/"+mcVersion+"/builds/", func(w http.ResponseWriter, r *http.Request) {
		// Could be /builds/{n} or /builds/{n}/downloads/{name}
		path := r.URL.Path
		if strings.Contains(path, "/downloads/") {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(jarBytes)
			return
		}
		// Build detail
		json.NewEncoder(w).Encode(map[string]any{
			"time": "2026-05-04T12:00:00.000Z",
			"downloads": map[string]any{
				"application": map[string]any{
					"name":   "paper-" + mcVersion + "-99.jar",
					"sha256": jarSumHex,
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, jarSumHex
}

func TestPaperUpdater_Latest(t *testing.T) {
	url, expectedSum := stubPaperAPI(t, "1.21.4", []int{50, 80, 99, 70}, []byte("fake-jar"))
	u := &PaperUpdater{BaseURL: url, HTTP: http.DefaultClient}
	rel, err := u.Latest(context.Background(), "1.21.4")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Build != 99 { // largest in [50,80,99,70]
		t.Errorf("build: got %d want 99", rel.Build)
	}
	if rel.SHA256 != expectedSum {
		t.Errorf("sha256: got %s want %s", rel.SHA256, expectedSum)
	}
	if !strings.HasSuffix(rel.DownloadURL, "/downloads/paper-1.21.4-99.jar") {
		t.Errorf("download url: %s", rel.DownloadURL)
	}
}

func TestPaperUpdater_NoVersion(t *testing.T) {
	u := NewPaperUpdater()
	_, err := u.Latest(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty mc_version")
	}
}

func TestPaperUpdater_NoBuilds(t *testing.T) {
	url, _ := stubPaperAPI(t, "9.9.9", []int{}, nil)
	u := &PaperUpdater{BaseURL: url, HTTP: http.DefaultClient}
	_, err := u.Latest(context.Background(), "9.9.9")
	if err == nil || !strings.Contains(err.Error(), "no builds") {
		t.Errorf("expected no-builds error, got %v", err)
	}
}

func TestDownload_RoundtripWithSHA256(t *testing.T) {
	url, sum := stubPaperAPI(t, "1.21.4", []int{1}, []byte("hello-jar-content"))
	u := &PaperUpdater{BaseURL: url, HTTP: http.DefaultClient}
	rel, err := u.Latest(context.Background(), "1.21.4")
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "paper.jar")
	if err := Download(context.Background(), http.DefaultClient, rel, dest); err != nil {
		t.Fatalf("Download: %v", err)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello-jar-content" {
		t.Errorf("body: %q", string(body))
	}
	got := sha256.Sum256(body)
	if hex.EncodeToString(got[:]) != sum {
		t.Errorf("sha mismatch")
	}
}

func TestDownload_SHA256Mismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/jar", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("real content"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	rel := &Release{
		DownloadURL: srv.URL + "/jar",
		SHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
	}
	dest := filepath.Join(t.TempDir(), "paper.jar")
	err := Download(context.Background(), http.DefaultClient, rel, dest)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("expected mismatch error, got %v", err)
	}
	// On failure, dest should not exist (only the .part file was written and removed).
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("dest should not exist on failure, got: %v", err)
	}
}

func TestDownload_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "paper.jar")
	rel := &Release{DownloadURL: srv.URL + "/anything"}
	err := Download(context.Background(), http.DefaultClient, rel, dest)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("expected HTTP 503 surfaced, got %v", err)
	}
}
