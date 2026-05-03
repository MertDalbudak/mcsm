package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/MertDalbudak/mcsm/internal/buildinfo"
)

type versionResponse struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type instanceResponse struct {
	Name            string         `json:"name"`
	Version         string         `json:"version"`
	Build           buildBlock     `json:"build"`
	StartedAt       time.Time      `json:"started_at"`
	UptimeSeconds   int64          `json:"uptime_seconds"`
	DiscoveryRoots  []string       `json:"discovery_roots"`
	SlotCount       int            `json:"slot_count"`
	Platform        platformBlock  `json:"platform"`
}

type buildBlock struct {
	Commit string `json:"commit"`
	Date   string `json:"date"`
}

type platformBlock struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	Java string `json:"java"` // populated in Phase 2 once we exec `java --version`
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz returns 503 until startup completes. Phase 1 always reports
// ready once the HTTP server has bound; later phases tighten this (lock
// dir writable, slots resolved, peers attempted, etc.).
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !s.ready.Load() {
		WriteError(w, r, http.StatusServiceUnavailable, CodeNotReady, "instance is starting", nil)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, r, http.StatusOK, versionResponse{
		Version: buildinfo.Version,
		Commit:  buildinfo.Commit,
		Date:    buildinfo.Date,
	})
}

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, r, http.StatusOK, instanceResponse{
		Name:    s.cfg.Instance.Name,
		Version: buildinfo.Version,
		Build: buildBlock{
			Commit: buildinfo.Commit,
			Date:   buildinfo.Date,
		},
		StartedAt:      s.startedAt,
		UptimeSeconds:  int64(time.Since(s.startedAt).Seconds()),
		DiscoveryRoots: s.cfg.Discovery.Roots,
		SlotCount:      len(s.cfg.Slots),
		Platform: platformBlock{
			OS:   runtime.GOOS,
			Arch: runtime.GOARCH,
			Java: "", // TODO Phase 2
		},
	})
}

// handleOpenAPI is a placeholder until the spec generator lands. Returning
// a stub keeps the route declared so clients don't get 404 surprises.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, r, http.StatusOK, map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "MCSM API",
			"version": "1.0.0",
		},
		"x-mcsm-status": "spec generation not yet implemented",
	})
}
