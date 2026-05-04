package api

import "net/http"

// GET /api/v1/peers
func (s *Server) handlePeersList(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		WriteJSON(w, r, http.StatusOK, map[string]any{"peers": []any{}})
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]any{"peers": s.peers.Status()})
}

// POST /api/v1/peers/refresh
func (s *Server) handlePeersRefresh(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		WriteJSON(w, r, http.StatusOK, map[string]any{"peers": []any{}})
		return
	}
	statuses := s.peers.PingAll(r.Context())
	WriteJSON(w, r, http.StatusOK, map[string]any{"peers": statuses})
}
