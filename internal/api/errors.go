package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// APIError is the wire-format error envelope documented in api.md §1.8.
type APIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	TraceID string         `json:"trace_id,omitempty"`
}

type errorEnvelope struct {
	Error APIError `json:"error"`
}

// Stable error codes — keep in sync with docs/api.md §10 "Error catalog".
const (
	CodeBadRequest         = "bad_request"
	CodeValidationFailed   = "validation_failed"
	CodeServerIncompatible = "server_incompatible"
	CodeMissingToken       = "missing_token"
	CodeInvalidToken       = "invalid_token"
	CodeScopeDenied        = "scope_denied"
	CodeNotFound           = "not_found"
	CodeSlotNotFound       = "slot_not_found"
	CodeServerNotFound     = "server_not_found"
	CodePlayerNotOnline    = "player_not_online"
	CodeBackupNotFound     = "backup_not_found"
	CodePeerNotFound       = "peer_not_found"
	CodeSlotBusy           = "slot_busy"
	CodeServerNotRunning   = "server_not_running"
	CodeServerInUse        = "server_in_use"
	CodeLockHeld           = "lock_held"
	CodeNotStopping        = "not_stopping"
	CodePeerConflict       = "peer_conflict"
	CodeInstanceLocked     = "instance_locked"
	CodeRateLimited        = "rate_limited"
	CodeInternal           = "internal"
	CodeRconUnreachable    = "rcon_unreachable"
	CodePeerUnreachable    = "peer_unreachable"
	CodeNotReady           = "not_ready"
	CodeCommandTimeout     = "command_timeout"
	CodeMethodNotAllowed   = "method_not_allowed"
	CodeNotImplemented     = "not_implemented"
)

// WriteError serializes an error response with the given status code.
// The trace id (if any) is pulled from request context.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, msg string, details map[string]any) {
	env := errorEnvelope{Error: APIError{
		Code:    code,
		Message: msg,
		Details: details,
		TraceID: TraceIDFromContext(r.Context()),
	}}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(env); err != nil {
		slog.Error("write error response", "err", err, "trace_id", env.Error.TraceID)
	}
}
