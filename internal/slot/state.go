// Package slot implements the per-slot state machine that mounts a
// discovered server, drives its lifecycle through the documented states
// (idle → mounting → starting → running → stopping → idle, plus crashed/
// error edges), and exposes RCON-backed control to the API.
package slot

import "time"

// State is the externally-visible slot state from docs/api.md §2.1.
type State string

const (
	StateIdle     State = "idle"
	StateMounting State = "mounting"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateCrashed  State = "crashed"
	StateError    State = "error"
)

// Terminal returns true for states the slot will not leave on its own.
func (s State) Terminal() bool {
	switch s {
	case StateIdle, StateCrashed, StateError:
		return true
	}
	return false
}

// CanStartFrom reports whether a Start call is valid in this state.
func (s State) CanStartFrom() bool {
	switch s {
	case StateIdle, StateCrashed, StateError:
		return true
	}
	return false
}

// CanStopFrom reports whether a Stop call is valid in this state.
func (s State) CanStopFrom() bool {
	return s == StateRunning || s == StateStarting
}

// LastError is populated on the slot when it lands in StateError or
// StateCrashed. Surfaced via GET /slots/{name}.
type LastError struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// Snapshot is a point-in-time view of a slot, safe to serialize.
// Returned by handlers; never includes mutable internal state.
type Snapshot struct {
	Name             string     `json:"name"`
	Port             int        `json:"port"`
	PublicAddress    string     `json:"public_address,omitempty"`
	State            State      `json:"state"`
	StateSince       time.Time  `json:"state_since"`
	MountedServerID  string     `json:"mounted_server_id,omitempty"`
	MountedServer    *Mounted   `json:"mounted_server,omitempty"`
	AutoMount        string     `json:"auto_mount,omitempty"`
	LastError        *LastError `json:"last_error"`
}

// Mounted is the runtime view of the server currently in this slot.
// Filled in once the slot reaches StateStarting and beyond.
type Mounted struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	Version       string    `json:"version,omitempty"`
	Path          string    `json:"path"`
	PID           int       `json:"pid,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	RconConnected bool      `json:"rcon_connected"`
	SLP           *SLPInfo  `json:"slp,omitempty"`
}

// SLPInfo is the most recent server-list-ping result, refreshed by the
// background health probe.
type SLPInfo struct {
	Online    bool      `json:"online"`
	Players   PlayersInfo `json:"players"`
	MOTD      string    `json:"motd,omitempty"`
	LatencyMS int64     `json:"latency_ms"`
	SampledAt time.Time `json:"sampled_at"`
}

type PlayersInfo struct {
	Online int `json:"online"`
	Max    int `json:"max"`
}
