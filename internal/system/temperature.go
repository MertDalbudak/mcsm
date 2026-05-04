// Package system implements local-host metrics: CPU temperature now,
// resources (mem/load/disk) in Phase 2D.
package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sample is one temperature reading.
type Sample struct {
	At      time.Time `json:"at"`
	Celsius float64   `json:"celsius"`
}

// Temperature is the rolling history of recent samples plus the most
// recent value. Goroutine-safe.
type Temperature struct {
	sensor    string
	interval  time.Duration
	historyN  int

	mu      sync.RWMutex
	last    *Sample
	history []Sample // ring; oldest first
}

// NewTemperature constructs a sampler. Pass historyN=0 for default 60.
// Returns nil + error if the sensor file isn't readable; callers can
// then disable temperature in the API surface.
func NewTemperature(sensor string, interval time.Duration, historyN int) (*Temperature, error) {
	if sensor == "" {
		return nil, errors.New("temperature: sensor path is empty")
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if historyN <= 0 {
		historyN = 60
	}
	t := &Temperature{
		sensor:   sensor,
		interval: interval,
		historyN: historyN,
		history:  make([]Sample, 0, historyN),
	}
	// Take an immediate sample so the first /system/temperature call
	// returns data instead of "not yet sampled".
	if _, err := t.sample(); err != nil {
		return t, fmt.Errorf("initial sample: %w", err)
	}
	return t, nil
}

// Run blocks, sampling on the configured interval until ctx is canceled.
// Errors from individual samples are stored on the Temperature (last
// error) but don't stop the loop — sensor files can be temporarily
// unavailable on some hardware.
func (t *Temperature) Run(ctx context.Context) {
	tk := time.NewTicker(t.interval)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			_, _ = t.sample()
		}
	}
}

// Snapshot returns the most recent sample (or nil) and a copy of the
// rolling history.
func (t *Temperature) Snapshot() (*Sample, []Sample) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	last := t.last
	hist := append([]Sample(nil), t.history...)
	return last, hist
}

// Sensor returns the configured sensor path, for inclusion in API responses.
func (t *Temperature) Sensor() string { return t.sensor }

func (t *Temperature) sample() (Sample, error) {
	raw, err := os.ReadFile(t.sensor)
	if err != nil {
		return Sample{}, err
	}
	s := strings.TrimSpace(string(raw))
	milli, err := strconv.Atoi(s)
	if err != nil {
		// Some sensors expose floating-point degrees directly.
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return Sample{}, fmt.Errorf("parse %q: %w", s, err)
		}
		return t.append(Sample{At: time.Now().UTC(), Celsius: f}), nil
	}
	c := float64(milli) / 1000.0
	return t.append(Sample{At: time.Now().UTC(), Celsius: c}), nil
}

func (t *Temperature) append(s Sample) Sample {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.last = &s
	if len(t.history) == t.historyN {
		copy(t.history, t.history[1:])
		t.history[len(t.history)-1] = s
	} else {
		t.history = append(t.history, s)
	}
	return s
}
