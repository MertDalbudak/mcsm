package slot

import (
	"errors"
	"sort"

	"github.com/MertDalbudak/mcsm/internal/config"
	"github.com/MertDalbudak/mcsm/internal/discovery"
)

// ErrSlotNotFound is returned by Manager.Get when the name doesn't match
// any configured slot.
var ErrSlotNotFound = errors.New("slot not found")

// Manager owns every slot configured on this instance. Constructed once
// at boot from config.Slots; the set of slots cannot change at runtime
// (a config reload requires a process restart).
type Manager struct {
	slots map[string]*Slot
	order []string // preserves config order for List()
}

// NewManager builds slots for every entry in cfg.Slots.
func NewManager(cfg *config.Config, host string, disco *discovery.Store) *Manager {
	m := &Manager{slots: make(map[string]*Slot, len(cfg.Slots))}
	for _, sc := range cfg.Slots {
		m.slots[sc.Name] = New(sc, cfg.Instance.Name, host, disco)
		m.order = append(m.order, sc.Name)
	}
	return m
}

// Get returns the slot with the given name, or ErrSlotNotFound.
func (m *Manager) Get(name string) (*Slot, error) {
	s, ok := m.slots[name]
	if !ok {
		return nil, ErrSlotNotFound
	}
	return s, nil
}

// List returns all slots in config order.
func (m *Manager) List() []*Slot {
	out := make([]*Slot, 0, len(m.order))
	for _, n := range m.order {
		out = append(out, m.slots[n])
	}
	return out
}

// Snapshots returns a Snapshot for every slot, in config order.
func (m *Manager) Snapshots() []Snapshot {
	out := make([]Snapshot, 0, len(m.order))
	for _, n := range m.order {
		out = append(out, m.slots[n].Snapshot())
	}
	return out
}

// SortedNames returns slot names sorted lexically. Used by tests.
func (m *Manager) SortedNames() []string {
	out := append([]string(nil), m.order...)
	sort.Strings(out)
	return out
}
