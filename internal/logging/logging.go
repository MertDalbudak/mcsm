// Package logging configures the process-wide slog logger.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/MertDalbudak/mcsm/internal/config"
)

// Setup builds a slog.Logger from config and installs it as the default.
// Returns the logger and the io.Writer in case the caller wants to close
// it (when output is a file).
func Setup(cfg config.Logging) (*slog.Logger, io.Writer, error) {
	w, err := openOutput(cfg.Output)
	if err != nil {
		return nil, nil, err
	}
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, w, err
	}
	opts := &slog.HandlerOptions{Level: level}

	var h slog.Handler
	switch cfg.Format {
	case "json":
		h = slog.NewJSONHandler(w, opts)
	case "text":
		h = slog.NewTextHandler(w, opts)
	default:
		return nil, w, fmt.Errorf("unknown log format %q", cfg.Format)
	}

	logger := slog.New(h)
	slog.SetDefault(logger)
	return logger, w, nil
}

func openOutput(out string) (io.Writer, error) {
	switch out {
	case "", "stdout":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	default:
		f, err := os.OpenFile(out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
		if err != nil {
			return nil, fmt.Errorf("open log file %q: %w", out, err)
		}
		return f, nil
	}
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", s)
	}
}
