package logtail

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Tailer follows a Minecraft log file (typically logs/latest.log) and
// emits parsed entries to subscribers and a bounded ring of recent
// history. Handles file truncation (size shrunk → re-read from start)
// and rotation (file removed/renamed → reopen on create) — both happen
// when Paper restarts.
//
// Construction does not open the file. Run() opens it (waiting up to
// ~10s if it doesn't exist yet, since logs/latest.log appears a moment
// after java starts).
type Tailer struct {
	path     string
	historyN int

	mu      sync.RWMutex
	history []LogEntry // ring; oldest first

	bcast *Broadcaster
}

// NewTailer constructs a Tailer rooted at the given log file path.
// historyN ≤ 0 → 5000.
func NewTailer(path string, historyN int) *Tailer {
	if historyN <= 0 {
		historyN = 5000
	}
	return &Tailer{
		path:     path,
		historyN: historyN,
		history:  make([]LogEntry, 0, historyN),
		bcast:    NewBroadcaster(),
	}
}

// Subscribe returns a buffered channel that receives every entry as it
// arrives. Subscribers that fall behind drop messages (the WS layer
// should consume promptly). Caller must Unsubscribe when done.
func (t *Tailer) Subscribe(buf int) Subscriber {
	return t.bcast.Subscribe(buf)
}

// Unsubscribe removes ch from the broadcaster and closes it.
func (t *Tailer) Unsubscribe(ch Subscriber) {
	t.bcast.Unsubscribe(ch)
}

// Recent returns up to n most recent entries (oldest first).
func (t *Tailer) Recent(n int) []LogEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if n <= 0 || n > len(t.history) {
		n = len(t.history)
	}
	out := make([]LogEntry, n)
	copy(out, t.history[len(t.history)-n:])
	return out
}

// Since returns entries with TS strictly after the cursor, oldest first.
// Bounded by historyN.
func (t *Tailer) Since(cursor time.Time) []LogEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]LogEntry, 0)
	for _, e := range t.history {
		if e.TS.After(cursor) {
			out = append(out, e)
		}
	}
	return out
}

// Run blocks, tailing the file until ctx is canceled. Errors during
// run are logged but don't stop the loop — file may be temporarily
// unreadable during rotation, etc.
func (t *Tailer) Run(ctx context.Context) {
	defer t.bcast.Close()

	if err := t.waitForFile(ctx, 10*time.Second); err != nil {
		slog.Warn("logtail: log file did not appear", "path", t.path, "err", err)
		return
	}

	for {
		if err := t.tailOnce(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			slog.Warn("logtail: restarting after error", "path", t.path, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		// tailOnce returned nil → file gone or rotated; loop to reopen.
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// waitForFile blocks until the path exists or timeout / ctx fires.
// Watches the parent directory for create events (avoids polling).
func (t *Tailer) waitForFile(ctx context.Context, timeout time.Duration) error {
	if _, err := os.Stat(t.path); err == nil {
		return nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.Add(filepath.Dir(t.path)); err != nil {
		return fmt.Errorf("watch parent: %w", err)
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		if _, err := os.Stat(t.path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("file not created within %s", timeout)
		case ev := <-w.Events:
			if ev.Name == t.path && ev.Has(fsnotify.Create) {
				return nil
			}
		case err := <-w.Errors:
			slog.Warn("logtail: watcher error", "err", err)
		}
	}
}

// tailOnce opens the file, seeks to end (if it has content already we
// emit the last 200 lines as backfill), then watches for appends with
// fsnotify. Returns nil when the file is rotated/removed (caller will
// loop back through waitForFile + tailOnce).
func (t *Tailer) tailOnce(ctx context.Context) error {
	f, err := os.Open(t.path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	// Backfill: read the existing tail of the file so subscribers see
	// some history immediately. We cap at the last 200 lines to avoid
	// dumping a huge file on startup.
	t.backfill(f, 200)

	// Seek to end and resume from there.
	pos, _ := f.Seek(0, io.SeekEnd)

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.Add(t.path); err != nil {
		return fmt.Errorf("watch file: %w", err)
	}
	// Also watch the parent so we notice rotation (file removed/renamed).
	if err := w.Add(filepath.Dir(t.path)); err != nil {
		return fmt.Errorf("watch parent: %w", err)
	}

	r := bufio.NewReaderSize(f, 64*1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-w.Events:
			switch {
			case ev.Has(fsnotify.Write) && ev.Name == t.path:
				newPos, err := t.readUntilEOF(r, &pos, f)
				if err != nil {
					slog.Warn("logtail: read", "path", t.path, "err", err)
				}
				_ = newPos
			case ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename):
				if ev.Name == t.path {
					return nil // signal rotation
				}
			case ev.Has(fsnotify.Create) && ev.Name == t.path:
				return nil // file recreated → reopen from scratch
			}
		case err := <-w.Errors:
			slog.Warn("logtail: watcher error", "err", err)
		}
	}
}

// backfill reads the last N lines of an opened file and emits them as
// already-captured entries. Used at startup so subscribers see some
// history. Skip on huge files (>1MB) — just seek to end.
func (t *Tailer) backfill(f *os.File, n int) {
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return
	}
	if st.Size() > 1<<20 {
		return // too big; skip backfill
	}
	body, err := io.ReadAll(f)
	if err != nil {
		return
	}
	// Re-seek to start so the main loop sees the same bytes via reader.
	_, _ = f.Seek(0, io.SeekStart)

	// Split on newlines; drop trailing empty (from final \n) and bound to N.
	all := strings.Split(string(body), "\n")
	if len(all) > 0 && all[len(all)-1] == "" {
		all = all[:len(all)-1]
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	now := time.Now()
	for _, l := range all {
		if l == "" {
			continue
		}
		t.emit(Parse(l, now))
	}
}

// readUntilEOF reads lines from the reader, emitting each one. Returns
// the new file position. Stops at EOF (the watcher fires again when
// more bytes arrive).
func (t *Tailer) readUntilEOF(r *bufio.Reader, pos *int64, f *os.File) (int64, error) {
	// Truncation detection: if file size is smaller than last seen pos,
	// reset and re-read from start.
	if st, err := f.Stat(); err == nil && st.Size() < *pos {
		_, _ = f.Seek(0, io.SeekStart)
		r.Reset(f)
		*pos = 0
	}
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			*pos += int64(len(line))
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed != "" {
				t.emit(Parse(trimmed, time.Now()))
			}
		}
		if err == io.EOF {
			return *pos, nil
		}
		if err != nil {
			return *pos, err
		}
	}
}

func (t *Tailer) emit(e LogEntry) {
	t.mu.Lock()
	if len(t.history) == t.historyN {
		copy(t.history, t.history[1:])
		t.history[len(t.history)-1] = e
	} else {
		t.history = append(t.history, e)
	}
	t.mu.Unlock()
	t.bcast.Publish(e)
}
