package api

import (
	"net/http"
	"strings"

	"github.com/MertDalbudak/mcsm/internal/audit"
)

// auditLogger returns middleware that records every mutating request
// (POST/PUT/PATCH/DELETE) into the audit log after the handler runs.
//
// Read endpoints are intentionally not audited — they don't change
// state and the volume would dominate the log.
func (s *Server) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrap to capture status. Reuses the recorder type from middleware.go.
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		if s.audit == nil {
			return
		}
		if !shouldAudit(r.Method, r.URL.Path) {
			return
		}

		actor := audit.Actor{Kind: "anonymous"}
		if rec.tokenName != "" {
			actor = audit.Actor{Kind: "token", Name: rec.tokenName}
		}
		result := "ok"
		if rec.status >= 400 {
			result = "error"
		}
		kind := classifyRoute(r.Method, r.URL.Path)
		s.audit.Append(r.Context(), audit.Entry{
			Actor:   actor,
			Kind:    kind,
			Subject: extractSubject(r),
			Result:  result,
			Status:  rec.status,
			TraceID: TraceIDFromContext(r.Context()),
		})
		if s.metrics != nil {
			s.metrics.AuditEntries.Inc(kind, result)
		}
	})
}

// shouldAudit returns true for mutating routes. Skips: GET, /metrics,
// /healthz, /readyz.
func shouldAudit(method, path string) bool {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return false
	}
	switch path {
	case "/healthz", "/readyz":
		return false
	}
	return true
}

// classifyRoute turns "POST /api/v1/slots/creative/start" into "slot.start".
// Used as the audit Entry.Kind so the UI can render typed icons.
func classifyRoute(method, path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// drop "api"/"v1" prefix
	if len(parts) >= 2 && parts[0] == "api" {
		parts = parts[2:]
	}
	if len(parts) == 0 {
		return method + ".unknown"
	}
	switch parts[0] {
	case "discovery":
		if len(parts) == 1 {
			return "discovery.refresh"
		}
		if len(parts) >= 3 && parts[2] == "lock" {
			return "discovery.unlock"
		}
	case "slots":
		if len(parts) >= 3 {
			leaf := parts[len(parts)-1]
			if len(parts) >= 4 && parts[2] == "server" {
				return "server." + strings.Join(parts[3:], ".")
			}
			return "slot." + leaf
		}
	case "peers":
		return "peers." + strings.Join(parts[1:], ".")
	}
	return strings.ToLower(method) + "." + strings.Join(parts, ".")
}

// extractSubject pulls likely-interesting path values into the subject map.
func extractSubject(r *http.Request) map[string]any {
	out := map[string]any{}
	if v := r.PathValue("name"); v != "" {
		out["slot"] = v
	}
	if v := r.PathValue("server_id"); v != "" {
		out["server_id"] = v
	}
	if v := r.PathValue("player"); v != "" {
		out["player"] = v
	}
	if v := r.PathValue("id"); v != "" {
		out["id"] = v
	}
	return out
}
