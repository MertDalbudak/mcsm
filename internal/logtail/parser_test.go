package logtail

import (
	"testing"
	"time"
)

func TestParse_PaperFormat(t *testing.T) {
	captured := time.Date(2026, 5, 4, 12, 0, 5, 0, time.UTC)
	got := Parse("[12:00:01] [Server thread/INFO]: Steve joined the game", captured)
	if got.Thread != "Server thread" || got.Level != "INFO" {
		t.Errorf("thread/level: %+v", got)
	}
	if got.Message != "Steve joined the game" {
		t.Errorf("message: %q", got.Message)
	}
	if got.TS.Hour() != 12 || got.TS.Minute() != 0 || got.TS.Second() != 1 {
		t.Errorf("TS time-of-day: %v", got.TS)
	}
	if got.TS.Year() != 2026 || got.TS.Month() != 5 || got.TS.Day() != 4 {
		t.Errorf("TS date should come from captured: %v", got.TS)
	}
	if got.Raw == "" {
		t.Error("Raw should be preserved")
	}
}

func TestParse_OldSpigotFormat(t *testing.T) {
	captured := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	got := Parse("[09:55:30 WARN]: Something happened", captured)
	if got.Level != "WARN" {
		t.Errorf("level: %q", got.Level)
	}
	if got.Thread != "" {
		t.Errorf("thread should be empty for short format: %q", got.Thread)
	}
	if got.Message != "Something happened" {
		t.Errorf("message: %q", got.Message)
	}
}

func TestParse_NonConforming(t *testing.T) {
	captured := time.Now()
	got := Parse("Some random output without a header", captured)
	if got.Level != "" || got.Thread != "" {
		t.Errorf("expected empty level/thread, got %+v", got)
	}
	if got.Message != "Some random output without a header" {
		t.Errorf("message should be raw line: %q", got.Message)
	}
	if !got.TS.Equal(captured) {
		t.Errorf("TS should fall back to capturedAt")
	}
}

func TestParse_MidnightRollover(t *testing.T) {
	// Captured at 00:00:05 UTC, log line says 23:59:55 (yesterday)
	captured := time.Date(2026, 5, 4, 0, 0, 5, 0, time.UTC)
	got := Parse("[23:59:55] [Server thread/INFO]: Late event", captured)
	// Should be yesterday (May 3) because parsed time is more than 1h ahead
	if got.TS.Day() != 3 {
		t.Errorf("expected May 3 due to rollover, got %v", got.TS)
	}
}

func TestParse_TrimsCarriageReturn(t *testing.T) {
	got := Parse("[12:00:01] [Server thread/INFO]: hi\r", time.Now())
	if got.Message != "hi" {
		t.Errorf("CR not trimmed: %q", got.Message)
	}
}
