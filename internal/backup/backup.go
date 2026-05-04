// Package backup creates and restores ZIP archives of a server's
// directory tree. Two modes:
//
//   Online   — best for low-downtime: ask RCON to flush the world
//              (save-off → save-all flush → copy → save-on). Mounted
//              server keeps running.
//   Offline  — best for integrity: stop the slot, copy, restart.
//
// Backups live under <data_dir>/backups/<server-id>/<timestamp>-<label>.zip.
// Metadata next to each archive in a sibling .json file lets List/Restore
// work without reading the archive.
package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Mode describes how a backup was taken.
type Mode string

const (
	ModeOnline  Mode = "online"
	ModeOffline Mode = "offline"
)

// Meta is the per-archive metadata persisted alongside the .zip.
type Meta struct {
	ID         string    `json:"id"`         // <timestamp>-<label> (URL-safe)
	ServerID   string    `json:"server_id"`
	Label      string    `json:"label"`
	Mode       Mode      `json:"mode"`
	CreatedAt  time.Time `json:"created_at"`
	BytesTotal int64     `json:"bytes_total"`
	FileCount  int       `json:"file_count"`
	Includes   Includes  `json:"includes"`
}

// Includes records what was put in the archive (so Restore is honest
// about what it doesn't have).
type Includes struct {
	World      bool `json:"world"`
	Properties bool `json:"properties"`
	Logs       bool `json:"logs"`
}

// Store owns the on-disk backups directory.
type Store struct {
	root string

	mu sync.Mutex // serializes per-store writes
}

// New creates a Store rooted at <data_dir>/backups.
func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("backup: mkdir %s: %w", root, err)
	}
	return &Store{root: root}, nil
}

func (s *Store) serverDir(serverID string) string {
	return filepath.Join(s.root, serverID)
}

func (s *Store) metaPath(serverID, id string) string {
	return filepath.Join(s.serverDir(serverID), id+".json")
}

func (s *Store) zipPath(serverID, id string) string {
	return filepath.Join(s.serverDir(serverID), id+".zip")
}

// CreateOptions tweaks what goes into the archive.
type CreateOptions struct {
	Label       string
	Mode        Mode
	IncludeLogs bool

	// SaveControl is invoked (online mode only) to call save-off then
	// save-all flush before copying, and save-on after. The hook lets
	// callers choose between RCON and any future control mechanism.
	SaveControl SaveController
}

// SaveController is what online backups need from the live server: a
// way to disable autosaves and force a flush, then re-enable.
type SaveController interface {
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
}

// Create takes a snapshot of serverDir into a new archive owned by serverID.
// Returns the persisted Meta record on success.
func (s *Store) Create(ctx context.Context, serverID, serverDir string, opts CreateOptions) (*Meta, error) {
	if opts.Mode == "" {
		opts.Mode = ModeOnline
	}
	if opts.Mode != ModeOnline && opts.Mode != ModeOffline {
		return nil, fmt.Errorf("backup: invalid mode %q", opts.Mode)
	}
	id := buildID(opts.Label)

	if err := os.MkdirAll(s.serverDir(serverID), 0o750); err != nil {
		return nil, fmt.Errorf("backup: mkdir: %w", err)
	}

	if opts.Mode == ModeOnline && opts.SaveControl != nil {
		if err := opts.SaveControl.Pause(ctx); err != nil {
			return nil, fmt.Errorf("backup: save-off: %w", err)
		}
		defer func() {
			rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = opts.SaveControl.Resume(rctx)
		}()
	}

	tmp := s.zipPath(serverID, id) + ".part"
	zfile, err := os.Create(tmp)
	if err != nil {
		return nil, fmt.Errorf("backup: create archive: %w", err)
	}
	zw := zip.NewWriter(zfile)

	meta := &Meta{
		ID:        id,
		ServerID:  serverID,
		Label:     opts.Label,
		Mode:      opts.Mode,
		CreatedAt: time.Now().UTC(),
		Includes: Includes{
			World:      true,
			Properties: true,
			Logs:       opts.IncludeLogs,
		},
	}

	walkErr := filepath.WalkDir(serverDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, err := filepath.Rel(serverDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldExclude(rel, opts.IncludeLogs) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Always store using forward slashes inside the archive.
		archiveName := filepath.ToSlash(rel)
		if d.IsDir() {
			archiveName += "/"
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = archiveName
		header.Method = zip.Deflate
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		n, err := io.Copy(w, f)
		if err != nil {
			return err
		}
		meta.BytesTotal += n
		meta.FileCount++
		return nil
	})

	if cerr := zw.Close(); cerr != nil && walkErr == nil {
		walkErr = cerr
	}
	if zerr := zfile.Close(); zerr != nil && walkErr == nil {
		walkErr = zerr
	}
	if walkErr != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("backup: walk: %w", walkErr)
	}

	final := s.zipPath(serverID, id)
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("backup: rename: %w", err)
	}
	if err := s.writeMeta(serverID, meta); err != nil {
		// Archive exists but metadata write failed; clean up to avoid
		// orphaned archives that can't be listed.
		os.Remove(final)
		return nil, err
	}
	return meta, nil
}

// shouldExclude returns true for paths that don't belong in a backup.
// Notably we never include .mcsm/owner.json (transient) or core/heap dumps.
func shouldExclude(rel string, includeLogs bool) bool {
	switch {
	case rel == ".mcsm" || strings.HasPrefix(rel, ".mcsm/"):
		// .mcsm/config.yaml IS valuable; only owner.json is transient.
		// We include the directory but skip owner.json explicitly:
		return rel == filepath.Join(".mcsm", "owner.json")
	case rel == "logs" || strings.HasPrefix(rel, "logs/"):
		return !includeLogs
	case strings.HasSuffix(rel, ".jfr"),
		strings.HasSuffix(rel, ".hprof"),
		strings.HasPrefix(filepath.Base(rel), "core."):
		return true
	case rel == "cache" || strings.HasPrefix(rel, "cache/"):
		return true
	}
	return false
}

func (s *Store) writeMeta(serverID string, m *Meta) error {
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(serverID, m.ID), body, 0o640)
}

// List returns all backups for a server, newest first.
func (s *Store) List(serverID string) ([]Meta, error) {
	dir := s.serverDir(serverID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Meta, 0)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m Meta
		if err := json.Unmarshal(body, &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Get returns one backup's metadata, or ErrNotFound.
func (s *Store) Get(serverID, id string) (*Meta, error) {
	body, err := os.ReadFile(s.metaPath(serverID, id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Delete removes both the archive and its metadata. Idempotent.
func (s *Store) Delete(serverID, id string) error {
	z := s.zipPath(serverID, id)
	mp := s.metaPath(serverID, id)
	zerr := os.Remove(z)
	merr := os.Remove(mp)
	if zerr != nil && !errors.Is(zerr, os.ErrNotExist) {
		return zerr
	}
	if merr != nil && !errors.Is(merr, os.ErrNotExist) {
		return merr
	}
	if errors.Is(zerr, os.ErrNotExist) && errors.Is(merr, os.ErrNotExist) {
		return ErrNotFound
	}
	return nil
}

// Restore extracts a backup back into the server directory. Caller is
// responsible for ensuring the slot is stopped — this function refuses
// nothing, but extracting over a running server will produce an
// inconsistent world. The handler enforces this.
//
// Existing files are overwritten; files present on disk but not in the
// archive are left in place (we don't do a full mirror).
func (s *Store) Restore(ctx context.Context, serverID, id, serverDir string) error {
	z := s.zipPath(serverID, id)
	zr, err := zip.OpenReader(z)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("restore: open: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Defense-in-depth: refuse path traversal in archive entries.
		clean := filepath.Clean(f.Name)
		if strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "/") {
			return fmt.Errorf("restore: refusing unsafe path %q", f.Name)
		}
		dst := filepath.Join(serverDir, clean)
		if !strings.HasPrefix(dst, filepath.Clean(serverDir)+string(os.PathSeparator)) &&
			dst != filepath.Clean(serverDir) {
			return fmt.Errorf("restore: refusing escape from server dir: %q", dst)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, f.Mode()); err != nil {
				return fmt.Errorf("restore: mkdir %s: %w", dst, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("restore: mkdir parent: %w", err)
		}
		if err := extractFile(f, dst); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("restore: open entry %s: %w", f.Name, err)
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return fmt.Errorf("restore: write %s: %w", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("restore: copy %s: %w", dst, err)
	}
	return nil
}

// buildID composes a backup id from a timestamp + sanitized label.
func buildID(label string) string {
	ts := time.Now().UTC().Format("20060102-150405")
	if label == "" {
		return ts
	}
	return ts + "-" + sanitizeLabel(label)
}

func sanitizeLabel(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-' || c == '_':
			out = append(out, c)
		case c == ' ':
			out = append(out, '-')
		}
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return string(out)
}

// ErrNotFound is returned by Get/Delete/Restore when the id is unknown.
var ErrNotFound = errors.New("backup not found")
