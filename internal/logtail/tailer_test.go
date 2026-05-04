package logtail

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestTailer_BackfillAndAppend(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "latest.log")
	if err := os.WriteFile(logPath, []byte(
		"[12:00:01] [Server thread/INFO]: First\n"+
			"[12:00:02] [Server thread/INFO]: Second\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tail := NewTailer(logPath, 100)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sub := tail.Subscribe(64)
	go tail.Run(ctx)

	// Wait for backfill.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(tail.Recent(10)) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := len(tail.Recent(10)); got < 2 {
		t.Fatalf("backfill: got %d entries, want >=2", got)
	}

	// Append a new line.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		f.WriteString("[12:00:03] [Server thread/INFO]: Third\n")
		f.Close()
	}()

	// Wait for the live entry on the subscriber channel.
	select {
	case e, ok := <-sub:
		if !ok {
			t.Fatal("subscriber channel closed unexpectedly")
		}
		if e.Message == "" {
			t.Errorf("empty message: %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for new line")
	}
}

// TestTailer_Rotation models Paper's startup rotation: the existing
// logs/latest.log is renamed to a date-stamped archive, then a fresh
// file is created at the same path. fsnotify reports Remove + Create;
// tailOnce returns on Remove and the Run loop re-opens, picking up the
// new content via backfill.
//
// In-place truncation (`:> latest.log`) is intentionally not covered:
// no MC server flavor does it, and reliable detection would require
// inode tracking — out of scope for this version.
func TestTailer_Rotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "latest.log")
	if err := os.WriteFile(logPath, []byte("[12:00:01] [Server thread/INFO]: A\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tail := NewTailer(logPath, 100)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	sub := tail.Subscribe(64)
	go tail.Run(ctx)
	time.Sleep(200 * time.Millisecond)

	if err := os.Rename(logPath, logPath+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(
		"[13:00:00] [Server thread/INFO]: AfterRotate\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	saw := false
	for time.Now().Before(deadline) && !saw {
		select {
		case e := <-sub:
			if e.Message == "AfterRotate" {
				saw = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !saw {
		t.Errorf("expected to see post-rotation line within deadline")
	}
}

func TestBroadcaster_FanOutAndUnsubscribe(t *testing.T) {
	b := NewBroadcaster()
	a := b.Subscribe(8)
	c := b.Subscribe(8)
	b.Publish(LogEntry{Message: "x"})
	b.Publish(LogEntry{Message: "y"})

	got := func(ch Subscriber) []string {
		out := []string{}
		for i := 0; i < 2; i++ {
			select {
			case e := <-ch:
				out = append(out, e.Message)
			case <-time.After(time.Second):
				return out
			}
		}
		return out
	}
	if g := got(a); len(g) != 2 {
		t.Errorf("subscriber a: %v", g)
	}
	if g := got(c); len(g) != 2 {
		t.Errorf("subscriber c: %v", g)
	}
	b.Unsubscribe(a)
	b.Publish(LogEntry{Message: "z"})
	// 'a' should be closed; 'c' should still receive.
	select {
	case _, ok := <-a:
		if ok {
			t.Error("expected 'a' closed")
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for 'a' close")
	}
	select {
	case e := <-c:
		if e.Message != "z" {
			t.Errorf("got %q want z", e.Message)
		}
	case <-time.After(time.Second):
		t.Error("c didn't receive z")
	}
}

func TestBroadcaster_SlowSubscriberDropsMessages(t *testing.T) {
	b := NewBroadcaster()
	slow := b.Subscribe(2) // small buffer
	for i := 0; i < 50; i++ {
		b.Publish(LogEntry{Message: "x"})
	}
	// We expect slow to have at most buffer entries (others dropped).
	// The test just confirms Publish didn't deadlock — we made it here.
	if got := len(slow); got > 2 {
		t.Errorf("slow buffer overflowed: %d", got)
	}
	// Drain whatever arrived (if any).
	go func() {
		for range slow {
		}
	}()
	b.Close()

	// Publishing concurrently with many subscribers should also not panic.
	b2 := NewBroadcaster()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := b2.Subscribe(4)
			for range s {
			}
		}()
	}
	for i := 0; i < 100; i++ {
		b2.Publish(LogEntry{Message: "y"})
	}
	b2.Close()
	wg.Wait()
}
