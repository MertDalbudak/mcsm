package metrics

import (
	"bytes"
	"strings"
	"testing"
)

func TestRegistry_GaugeAndCounter(t *testing.T) {
	r := NewRegistry()
	g := r.MustRegisterGauge("temp_celsius", "CPU temp", "sensor")
	c := r.MustRegisterCounter("hits_total", "Hit counter", "route")

	g.Set(42.5, "thermal0")
	g.Set(50.0, "thermal0") // overwrite
	g.Set(31.1, "thermal1")
	c.Inc("a")
	c.Inc("a")
	c.Add(5, "b")

	var buf bytes.Buffer
	if _, err := r.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		"# HELP temp_celsius CPU temp",
		"# TYPE temp_celsius gauge",
		`temp_celsius{sensor="thermal0"} 50`,
		`temp_celsius{sensor="thermal1"} 31.1`,
		"# HELP hits_total Hit counter",
		"# TYPE hits_total counter",
		`hits_total{route="a"} 2`,
		`hits_total{route="b"} 5`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRegistry_NoLabels(t *testing.T) {
	r := NewRegistry()
	g := r.MustRegisterGauge("uptime_seconds", "uptime")
	g.Set(123)
	var buf bytes.Buffer
	r.WriteTo(&buf)
	if !strings.Contains(buf.String(), "uptime_seconds 123") {
		t.Errorf("output: %s", buf.String())
	}
}

func TestRegistry_LabelMismatchIgnored(t *testing.T) {
	r := NewRegistry()
	g := r.MustRegisterGauge("x", "x", "a", "b")
	g.Set(1, "only-one") // wrong arity → silently ignored
	g.Set(2, "a", "b")   // correct
	var buf bytes.Buffer
	r.WriteTo(&buf)
	if !strings.Contains(buf.String(), `x{a="a",b="b"} 2`) {
		t.Errorf("output: %s", buf.String())
	}
	if strings.Contains(buf.String(), "only-one") {
		t.Error("wrong-arity Set should not produce output")
	}
}

func TestRegistry_LabelEscaping(t *testing.T) {
	r := NewRegistry()
	g := r.MustRegisterGauge("x", "x", "label")
	g.Set(1, `with "quotes" and \\back`)
	var buf bytes.Buffer
	r.WriteTo(&buf)
	if !strings.Contains(buf.String(), `\"quotes\"`) {
		t.Errorf("quotes not escaped: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `\\\\back`) {
		t.Errorf("backslashes not escaped: %s", buf.String())
	}
}

func TestRegistry_StableOutputOrder(t *testing.T) {
	r := NewRegistry()
	g := r.MustRegisterGauge("x", "x", "k")
	g.Set(1, "z")
	g.Set(2, "a")
	g.Set(3, "m")
	var buf1, buf2 bytes.Buffer
	r.WriteTo(&buf1)
	r.WriteTo(&buf2)
	if buf1.String() != buf2.String() {
		t.Error("output should be stable across calls")
	}
	// And alphabetically sorted.
	got := buf1.String()
	idxA := strings.Index(got, `k="a"`)
	idxM := strings.Index(got, `k="m"`)
	idxZ := strings.Index(got, `k="z"`)
	if !(idxA < idxM && idxM < idxZ) {
		t.Errorf("series not sorted: a@%d m@%d z@%d\noutput:\n%s", idxA, idxM, idxZ, got)
	}
}

func TestCollectors_NewWiresEverything(t *testing.T) {
	c := NewCollectors()
	c.InstanceInfo.Set(1, "test", "v1")
	c.SlotState.Set(1, "creative", "running")
	c.PeerReachable.Set(1, "node-b")
	c.AuditEntries.Inc("slot.start", "ok")

	var buf bytes.Buffer
	c.Registry().WriteTo(&buf)
	out := buf.String()
	for _, want := range []string{
		`mcsm_instance_info{name="test",version="v1"} 1`,
		`mcsm_slot_state{slot="creative",state="running"} 1`,
		`mcsm_peer_reachable{peer_name="node-b"} 1`,
		`mcsm_audit_entries_total{kind="slot.start",result="ok"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
