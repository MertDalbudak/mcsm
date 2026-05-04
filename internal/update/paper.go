// Package update implements server-jar update lookups + downloads.
// v1 covers PaperMC; the Updater interface is the seam for adding
// other flavors (Fabric, Vanilla, Forge) later.
//
// Workflow:
//
//   1. Latest(ctx, version) → returns the newest published build for
//      the given Minecraft version, plus a download URL and SHA256.
//   2. Download(ctx, dl, dest) → streams the jar to dest with checksum
//      verification.
//
// API handlers and the slot manager wire these together with the
// confirmation flow documented in api.md (auto_update: confirm | auto).
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Release describes one available server build.
type Release struct {
	Flavor       string `json:"flavor"`        // "paper" | ...
	MCVersion    string `json:"mc_version"`    // "1.21.4"
	Build        int    `json:"build"`
	DownloadURL  string `json:"download_url"`
	SHA256       string `json:"sha256,omitempty"`
	JARName      string `json:"jar_name"`      // suggested filename
	PublishedAt  time.Time `json:"published_at,omitempty"`
}

// Updater is the strategy interface — one implementation per flavor.
type Updater interface {
	Flavor() string
	// Latest returns the newest build available for mcVersion.
	Latest(ctx context.Context, mcVersion string) (*Release, error)
}

// PaperUpdater talks to the PaperMC v2 API.
type PaperUpdater struct {
	BaseURL string // override for tests; default https://api.papermc.io
	HTTP    *http.Client
}

// NewPaperUpdater returns an updater with sane defaults.
func NewPaperUpdater() *PaperUpdater {
	return &PaperUpdater{
		BaseURL: "https://api.papermc.io",
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *PaperUpdater) Flavor() string { return "paper" }

// Latest queries:
//
//	GET /v2/projects/paper/versions/<mcVersion>      → { "builds": [..., 580] }
//	GET /v2/projects/paper/versions/<mcVersion>/builds/<n>
//	  → { "downloads": { "application": { "name": "...", "sha256": "..." } }, "time": "..." }
//
// Then constructs the download URL.
func (p *PaperUpdater) Latest(ctx context.Context, mcVersion string) (*Release, error) {
	if mcVersion == "" {
		return nil, fmt.Errorf("paper: mc_version is required")
	}
	base := strings.TrimRight(p.BaseURL, "/")
	versionURL := base + "/v2/projects/paper/versions/" + url.PathEscape(mcVersion)
	var v struct {
		Builds []int `json:"builds"`
	}
	if err := p.getJSON(ctx, versionURL, &v); err != nil {
		return nil, fmt.Errorf("paper: list builds: %w", err)
	}
	if len(v.Builds) == 0 {
		return nil, fmt.Errorf("paper: no builds for %s", mcVersion)
	}
	sort.Ints(v.Builds)
	build := v.Builds[len(v.Builds)-1]

	buildURL := fmt.Sprintf("%s/v2/projects/paper/versions/%s/builds/%d",
		base, url.PathEscape(mcVersion), build)
	var b struct {
		Time      time.Time `json:"time"`
		Downloads struct {
			Application struct {
				Name   string `json:"name"`
				SHA256 string `json:"sha256"`
			} `json:"application"`
		} `json:"downloads"`
	}
	if err := p.getJSON(ctx, buildURL, &b); err != nil {
		return nil, fmt.Errorf("paper: build detail: %w", err)
	}
	if b.Downloads.Application.Name == "" {
		return nil, fmt.Errorf("paper: build %d has no application download", build)
	}
	dl := fmt.Sprintf("%s/v2/projects/paper/versions/%s/builds/%d/downloads/%s",
		base, url.PathEscape(mcVersion), build, b.Downloads.Application.Name)
	return &Release{
		Flavor:      "paper",
		MCVersion:   mcVersion,
		Build:       build,
		DownloadURL: dl,
		SHA256:      b.Downloads.Application.SHA256,
		JARName:     b.Downloads.Application.Name,
		PublishedAt: b.Time,
	}, nil
}

// Download streams the release into dest, verifying SHA256 if non-empty.
// Writes to dest+".part" and renames on success — never leaves a half-
// written jar around for the JVM to load.
func Download(ctx context.Context, client *http.Client, rel *Release, dest string) error {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		os.Remove(tmp) // remove on any error path; rename below clears it
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.DownloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, hasher), resp.Body); err != nil {
		return fmt.Errorf("download: copy: %w", err)
	}
	if err := out.Close(); err != nil {
		return err
	}

	if rel.SHA256 != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(got, rel.SHA256) {
			return fmt.Errorf("download: sha256 mismatch: got %s want %s", got, rel.SHA256)
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	return nil
}

func (p *PaperUpdater) getJSON(ctx context.Context, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mcsm/2.x")
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
