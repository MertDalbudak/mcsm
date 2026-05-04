package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/MertDalbudak/mcsm/internal/audit"
)

type auditResponse struct {
	Entries    []audit.Entry `json:"entries"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// GET /api/v1/audit
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		WriteError(w, r, http.StatusServiceUnavailable, CodeInternal,
			"audit log is disabled (set audit.enabled: true in config)", nil)
		return
	}
	q := r.URL.Query()
	query := audit.Query{
		Actor:  q.Get("actor"),
		Kind:   q.Get("kind"),
		Cursor: q.Get("cursor"),
	}
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
				"since must be RFC3339", nil)
			return
		}
		query.Since = t
	}
	if v := q.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
				"until must be RFC3339", nil)
			return
		}
		query.Until = t
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			WriteError(w, r, http.StatusBadRequest, CodeValidationFailed,
				"limit must be a positive integer", nil)
			return
		}
		query.Limit = n
	}

	entries, cursor := s.audit.List(query)
	WriteJSON(w, r, http.StatusOK, auditResponse{
		Entries:    entries,
		NextCursor: cursor,
	})
}
