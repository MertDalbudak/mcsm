package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// WriteJSON serializes v with the given status code. On encoding failure
// the response will be partially written; we log but cannot recover.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "err", err, "trace_id", TraceIDFromContext(r.Context()))
	}
}

// DecodeJSON parses the request body into dst. On failure it writes the
// error response itself and returns false; callers can simply return.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		WriteError(w, r, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
		return false
	}
	return true
}
