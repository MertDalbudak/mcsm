package metrics

// Collectors wraps every mcsm-side gauge/counter so call sites read
// fluently (m.SlotState.Set(...)) instead of manipulating the Registry.
type Collectors struct {
	registry *Registry

	// Process / instance
	InstanceInfo *Gauge

	// Slots
	SlotState        *Gauge
	SlotUptimeSec    *Gauge
	ServerPlayers    *Gauge
	ServerPlayersMax *Gauge
	ServerRestarts   *Counter

	// RCON
	RconCmdsTotal *Counter

	// Locks
	LockStealsTotal *Counter

	// System
	TempCelsius *Gauge

	// API
	APIReqs *Counter

	// Peers
	PeerReachable *Gauge
	PeerRTT       *Gauge

	// Audit
	AuditEntries *Counter
}

// NewCollectors constructs the registry and registers every series
// documented in api.md §8 metrics.
func NewCollectors() *Collectors {
	r := NewRegistry()
	return &Collectors{
		registry: r,

		InstanceInfo: r.MustRegisterGauge("mcsm_instance_info",
			"Static info about this MCSM instance (always 1).",
			"name", "version"),

		SlotState: r.MustRegisterGauge("mcsm_slot_state",
			"Current state of each slot (1 for the active state, 0 for others).",
			"slot", "state"),
		SlotUptimeSec: r.MustRegisterGauge("mcsm_slot_uptime_seconds",
			"Seconds since the slot last entered its current state.",
			"slot"),
		ServerPlayers: r.MustRegisterGauge("mcsm_server_players_online",
			"Currently online players (from SLP).",
			"slot", "server_id"),
		ServerPlayersMax: r.MustRegisterGauge("mcsm_server_players_max",
			"Server max-players (from SLP).",
			"slot", "server_id"),
		ServerRestarts: r.MustRegisterCounter("mcsm_server_restart_total",
			"Number of times a server has restarted in this slot.",
			"slot", "reason"),

		RconCmdsTotal: r.MustRegisterCounter("mcsm_rcon_command_total",
			"Total RCON commands issued.",
			"slot"),

		LockStealsTotal: r.MustRegisterCounter("mcsm_lock_steal_total",
			"Number of times a stale lock was stolen.",
			"server_id"),

		TempCelsius: r.MustRegisterGauge("mcsm_system_temperature_celsius",
			"Most recent CPU temperature reading.",
			"sensor"),

		APIReqs: r.MustRegisterCounter("mcsm_api_request_total",
			"Total API requests served.",
			"route", "method", "status"),

		PeerReachable: r.MustRegisterGauge("mcsm_peer_reachable",
			"1 if the peer responded to its last ping, else 0.",
			"peer_name"),
		PeerRTT: r.MustRegisterGauge("mcsm_peer_rtt_seconds",
			"Last measured round-trip-time to a peer in seconds.",
			"peer_name"),

		AuditEntries: r.MustRegisterCounter("mcsm_audit_entries_total",
			"Total audit log entries written.",
			"kind", "result"),
	}
}

// Registry exposes the underlying registry for the /metrics handler.
func (c *Collectors) Registry() *Registry { return c.registry }
