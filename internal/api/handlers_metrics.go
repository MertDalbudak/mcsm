package api

import "net/http"

// GET /metrics
//
// Auth is optional per config (metrics.require_auth: false by default).
// The router wires the middleware accordingly.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.metrics == nil {
		WriteError(w, r, http.StatusServiceUnavailable, CodeInternal,
			"metrics are disabled (metrics.enabled: false)", nil)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = s.metrics.Registry().WriteTo(w)
}
