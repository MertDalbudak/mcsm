// Package logtail parses Minecraft server log lines into structured
// LogEntry records. Phase 2C reads from the process supervisor's
// stdout ring buffer; a future fsnotify-based file tailer will replace
// the source without changing the parser or the wire format.
package logtail

import (
	"regexp"
	"strings"
	"time"
)

// LogEntry is the wire-format log line documented in api.md §6.
type LogEntry struct {
	TS      time.Time `json:"ts"`
	Thread  string    `json:"thread,omitempty"`
	Level   string    `json:"level,omitempty"`
	Source  *string   `json:"source"`
	Message string    `json:"message"`
	Raw     string    `json:"raw"`
}

// mcLineRe matches the Minecraft Java log header:
//
//	[12:00:01] [Server thread/INFO]: Steve joined the game
//	[12:00:01] [Server thread/WARN]: ...
//	[12:00:01 INFO]: ...     ← older Spigot-style
//
// The thread/level pair is optional so we still get a useful entry for
// non-conforming lines (Forge mod loaders, JVM crashes, etc.).
var mcLineRe = regexp.MustCompile(
	`^\[(\d{2}:\d{2}:\d{2})\]\s*\[([^/\]]+)/([A-Z]+)\]:\s*(.*)$`,
)

// shortLineRe handles the older `[HH:MM:SS LEVEL]: message` format.
var shortLineRe = regexp.MustCompile(
	`^\[(\d{2}:\d{2}:\d{2})\s+([A-Z]+)\]:\s*(.*)$`,
)

// Parse converts a single raw log line into a LogEntry. capturedAt is
// the wall-clock time mcsm read the line from java's stdout — used as
// the date component, since the log line itself only carries time-of-day.
//
// If the line doesn't match any known format, returns a LogEntry with
// just Raw and Message=line so callers don't have to handle nil.
func Parse(line string, capturedAt time.Time) LogEntry {
	line = strings.TrimRight(line, "\r")
	if m := mcLineRe.FindStringSubmatch(line); m != nil {
		return LogEntry{
			TS:      mergeTime(capturedAt, m[1]),
			Thread:  m[2],
			Level:   m[3],
			Message: m[4],
			Raw:     line,
		}
	}
	if m := shortLineRe.FindStringSubmatch(line); m != nil {
		return LogEntry{
			TS:      mergeTime(capturedAt, m[1]),
			Level:   m[2],
			Message: m[3],
			Raw:     line,
		}
	}
	return LogEntry{
		TS:      capturedAt,
		Message: line,
		Raw:     line,
	}
}

// mergeTime applies the HH:MM:SS from the log line on top of the
// captured day/timezone. If parsing the time-of-day fails, falls back
// to the capture time.
func mergeTime(captured time.Time, hms string) time.Time {
	t, err := time.ParseInLocation("15:04:05", hms, captured.Location())
	if err != nil {
		return captured
	}
	merged := time.Date(
		captured.Year(), captured.Month(), captured.Day(),
		t.Hour(), t.Minute(), t.Second(), 0,
		captured.Location(),
	)
	// Handle the midnight-rollover case: if the parsed time is in the
	// future relative to capture (more than ~1h ahead), the line is
	// probably from yesterday.
	if merged.Sub(captured) > time.Hour {
		merged = merged.Add(-24 * time.Hour)
	}
	return merged
}
