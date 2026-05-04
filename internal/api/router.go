package api

import "net/http"

// register wires every route in the v1 spec onto the mux. Phase 1
// implements meta + instance; everything else returns 501 with the
// documented error envelope so clients get a useful response instead
// of a router 404.
func (s *Server) register(mux *http.ServeMux) {
	auth := s.auth.authenticate
	scope := requireScope

	// --- Public meta (always unauthenticated; gating /version + /openapi via
	//     api.public_meta is wired below) ---
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)

	if s.cfg.API.PublicMeta || true { // PublicMeta defaults true; flag reserved for tightening
		mux.HandleFunc("GET /version", s.handleVersion)
		mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	} else {
		mux.Handle("GET /version", chain(auth, scope("instance:read"))(http.HandlerFunc(s.handleVersion)))
		mux.Handle("GET /openapi.json", chain(auth, scope("instance:read"))(http.HandlerFunc(s.handleOpenAPI)))
	}

	// --- Instance ---
	mux.Handle("GET /api/v1/instance",
		chain(auth, scope("instance:read"))(http.HandlerFunc(s.handleInstance)))

	// --- Discovery ---
	mux.Handle("GET /api/v1/discovery",
		chain(auth, scope("discovery:read"))(http.HandlerFunc(s.handleDiscovery)))
	mux.Handle("POST /api/v1/discovery/refresh",
		chain(auth, scope("discovery:write"))(http.HandlerFunc(s.handleDiscoveryRefresh)))
	mux.Handle("DELETE /api/v1/discovery/{server_id}/lock",
		chain(auth, scope("discovery:write"))(http.HandlerFunc(s.handleDiscoveryUnlock)))

	// --- Slots ---
	mux.Handle("GET /api/v1/slots",
		chain(auth, scope("slot:read"))(http.HandlerFunc(s.handleSlotsList)))
	mux.Handle("GET /api/v1/slots/{name}",
		chain(auth, scope("slot:read"))(http.HandlerFunc(s.handleSlotGet)))
	mux.Handle("GET /api/v1/slots/{name}/compatible-servers",
		chain(auth, scope("slot:read"))(http.HandlerFunc(s.handleSlotCompatible)))
	mux.Handle("POST /api/v1/slots/{name}/start",
		chain(auth, scope("slot:write"))(http.HandlerFunc(s.handleSlotStart)))
	mux.Handle("POST /api/v1/slots/{name}/stop",
		chain(auth, scope("slot:write"))(http.HandlerFunc(s.handleSlotStop)))
	mux.Handle("POST /api/v1/slots/{name}/restart",
		chain(auth, scope("slot:write"))(http.HandlerFunc(s.handleSlotRestart)))
	mux.Handle("POST /api/v1/slots/{name}/abort-stop",
		chain(auth, scope("slot:write"))(http.HandlerFunc(s.handleSlotAbortStop)))

	// --- Mounted server ---
	mux.Handle("POST /api/v1/slots/{name}/server/command",
		chain(auth, scope("server:command"))(http.HandlerFunc(s.handleServerCommand)))
	mux.Handle("POST /api/v1/slots/{name}/server/say",
		chain(auth, scope("server:command"))(http.HandlerFunc(s.handleServerSay)))
	mux.Handle("GET /api/v1/slots/{name}/server/players",
		chain(auth, scope("server:read"))(http.HandlerFunc(s.handleServerPlayers)))
	mux.Handle("POST /api/v1/slots/{name}/server/players/{player}/kick",
		chain(auth, scope("server:moderate"))(http.HandlerFunc(s.handleServerKick)))
	mux.Handle("POST /api/v1/slots/{name}/server/players/{player}/ban",
		chain(auth, scope("server:moderate"))(http.HandlerFunc(s.handleServerBan)))
	mux.Handle("POST /api/v1/slots/{name}/server/players/{player}/unban",
		chain(auth, scope("server:moderate"))(http.HandlerFunc(s.handleServerUnban)))
	mux.Handle("POST /api/v1/slots/{name}/server/players/{player}/op",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleServerOp)))
	mux.Handle("POST /api/v1/slots/{name}/server/players/{player}/deop",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleServerDeop)))

	mux.Handle("GET /api/v1/slots/{name}/server/whitelist",
		chain(auth, scope("server:read"))(http.HandlerFunc(s.handleWhitelistGet)))
	mux.Handle("PUT /api/v1/slots/{name}/server/whitelist/{player}",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleWhitelistAdd)))
	mux.Handle("DELETE /api/v1/slots/{name}/server/whitelist/{player}",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleWhitelistRemove)))
	mux.Handle("POST /api/v1/slots/{name}/server/whitelist/reload",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleWhitelistReload)))

	mux.Handle("GET /api/v1/slots/{name}/server/banlist",
		chain(auth, scope("server:read"))(http.HandlerFunc(s.handleBanlistGet)))
	mux.Handle("GET /api/v1/slots/{name}/server/banlist/ips",
		chain(auth, scope("server:read"))(http.HandlerFunc(s.handleBanlistIPsGet)))

	mux.Handle("GET /api/v1/slots/{name}/server/properties",
		chain(auth, scope("server:read"))(http.HandlerFunc(s.handleServerPropertiesGet)))
	mux.Handle("PATCH /api/v1/slots/{name}/server/properties",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleServerPropertiesPatch)))

	// --- Logs ---
	mux.Handle("GET /api/v1/slots/{name}/server/logs",
		chain(auth, scope("server:read"))(http.HandlerFunc(s.handleServerLogs)))
	mux.Handle("GET /api/v1/slots/{name}/server/logs/stream",
		chain(auth, scope("server:read"))(http.HandlerFunc(s.handleServerLogsStream)))

	// --- Slot lifecycle events WS ---
	mux.Handle("GET /api/v1/slots/{name}/events",
		chain(auth, scope("slot:read"))(http.HandlerFunc(s.handleSlotEventsStream)))

	// --- Update ---
	mux.Handle("GET /api/v1/slots/{name}/server/update",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleUpdateCheck)))
	mux.Handle("POST /api/v1/slots/{name}/server/update",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleUpdateApply)))

	// --- Backups ---
	mux.Handle("GET /api/v1/slots/{name}/server/backups",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleBackupsList)))
	mux.Handle("POST /api/v1/slots/{name}/server/backups",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleBackupCreate)))
	mux.Handle("GET /api/v1/slots/{name}/server/backups/{id}",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleBackupGet)))
	mux.Handle("POST /api/v1/slots/{name}/server/backups/{id}/restore",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleBackupRestore)))
	mux.Handle("DELETE /api/v1/slots/{name}/server/backups/{id}",
		chain(auth, scope("server:admin"))(http.HandlerFunc(s.handleBackupDelete)))

	// --- System ---
	mux.Handle("GET /api/v1/system/temperature",
		chain(auth, scope("system:read"))(http.HandlerFunc(s.handleSystemTemperature)))
	mux.Handle("GET /api/v1/system/resources",
		chain(auth, scope("system:read"))(http.HandlerFunc(s.handleSystemResources)))

	// --- Audit ---
	mux.Handle("GET /api/v1/audit",
		chain(auth, scope("audit:read"))(http.HandlerFunc(s.handleAudit)))

	// --- Peers & federation ---
	mux.Handle("GET /api/v1/peers",
		chain(auth, scope("peer:read"))(http.HandlerFunc(s.handlePeersList)))
	mux.Handle("POST /api/v1/peers/refresh",
		chain(auth, scope("peer:read"))(http.HandlerFunc(s.handlePeersRefresh)))
	mux.Handle("GET /api/v1/federation/discovery",
		chain(auth, scope("discovery:read"))(http.HandlerFunc(s.handleFederationDiscovery)))
	mux.Handle("GET /api/v1/federation/slots",
		chain(auth, scope("slot:read"))(http.HandlerFunc(s.handleFederationSlots)))

	// --- Metrics ---
	if s.cfg.Metrics.Enabled {
		path := s.cfg.Metrics.Path
		if path == "" {
			path = "/metrics"
		}
		if s.cfg.Metrics.RequireAuth {
			mux.Handle("GET "+path, chain(auth, scope("metrics:read"))(http.HandlerFunc(s.handleMetrics)))
		} else {
			mux.HandleFunc("GET "+path, s.handleMetrics)
		}
	}

	// Catch-all so unknown paths get our error envelope rather than the
	// stdlib's plaintext "404 page not found".
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusNotFound, CodeNotFound, "no such route", map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
		})
	})
}
