// Package metrics implements a minimal Prometheus exposition emitter.
// We don't pull in client_golang because our needs are modest (gauges
// + counters + histograms) and the spec format is straightforward.
//
//	# HELP <name> <help>
//	# TYPE <name> <type>
//	<name>{label="value",...} <number> [timestamp]
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Registry holds the current snapshot for every metric. Goroutine-safe.
type Registry struct {
	mu      sync.RWMutex
	metrics map[string]*metric
}

func NewRegistry() *Registry {
	return &Registry{metrics: map[string]*metric{}}
}

// MustRegisterGauge declares a gauge with the given name + help.
// Returns a setter that records value for a specific label set.
func (r *Registry) MustRegisterGauge(name, help string, labels ...string) *Gauge {
	m := r.upsert(name, help, "gauge", labels)
	return &Gauge{m: m}
}

// MustRegisterCounter declares a monotonic counter.
func (r *Registry) MustRegisterCounter(name, help string, labels ...string) *Counter {
	m := r.upsert(name, help, "counter", labels)
	return &Counter{m: m}
}

func (r *Registry) upsert(name, help, mtype string, labels []string) *metric {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.metrics[name]
	if !ok {
		m = &metric{
			name:   name,
			help:   help,
			mtype:  mtype,
			labels: append([]string(nil), labels...),
			series: map[string]float64{},
		}
		r.metrics[name] = m
	}
	return m
}

// WriteTo writes the entire snapshot to w in Prometheus exposition format.
// Implements io.WriterTo so it can be plugged into http.ResponseWriter directly.
func (r *Registry) WriteTo(w io.Writer) (int64, error) {
	r.mu.RLock()
	names := make([]string, 0, len(r.metrics))
	for n := range r.metrics {
		names = append(names, n)
	}
	sort.Strings(names)
	snap := make([]metricSnapshot, 0, len(names))
	for _, n := range names {
		snap = append(snap, r.metrics[n].snapshot())
	}
	r.mu.RUnlock()

	var n int64
	for _, m := range snap {
		k, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", m.name, escapeHelp(m.help), m.name, m.mtype)
		n += int64(k)
		if err != nil {
			return n, err
		}
		// Stable line order: sort label-key strings.
		keys := make([]string, 0, len(m.series))
		for k := range m.series {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			line := m.name
			if k != "" {
				line += "{" + k + "}"
			}
			line += " " + formatFloat(m.series[k]) + "\n"
			j, err := io.WriteString(w, line)
			n += int64(j)
			if err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

// --- internal types ---

type metric struct {
	mu     sync.Mutex
	name   string
	help   string
	mtype  string
	labels []string
	series map[string]float64
}

type metricSnapshot struct {
	name   string
	help   string
	mtype  string
	series map[string]float64
}

func (m *metric) snapshot() metricSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]float64, len(m.series))
	for k, v := range m.series {
		out[k] = v
	}
	return metricSnapshot{name: m.name, help: m.help, mtype: m.mtype, series: out}
}

func (m *metric) set(labels []string, v float64) {
	if len(labels) != len(m.labels) {
		return
	}
	m.mu.Lock()
	m.series[labelKey(m.labels, labels)] = v
	m.mu.Unlock()
}

func (m *metric) add(labels []string, delta float64) {
	if len(labels) != len(m.labels) {
		return
	}
	m.mu.Lock()
	k := labelKey(m.labels, labels)
	m.series[k] += delta
	m.mu.Unlock()
}

// labelKey serializes a label set into a Prometheus-style stable string.
func labelKey(names, values []string) string {
	if len(names) == 0 {
		return ""
	}
	pairs := make([]string, len(names))
	for i := range names {
		pairs[i] = names[i] + "=\"" + escapeLabel(values[i]) + "\""
	}
	return strings.Join(pairs, ",")
}

func escapeLabel(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\n", "\\n")
	return v
}

func escapeHelp(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\n", "\\n")
	return v
}

func formatFloat(v float64) string {
	// Whole numbers render without decimals.
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// --- public surface ---

// Gauge is a Set-able value.
type Gauge struct{ m *metric }

func (g *Gauge) Set(value float64, labels ...string) { g.m.set(labels, value) }

// Counter is an Add-only monotonic value.
type Counter struct{ m *metric }

func (c *Counter) Inc(labels ...string)              { c.m.add(labels, 1) }
func (c *Counter) Add(value float64, labels ...string) { c.m.add(labels, value) }
