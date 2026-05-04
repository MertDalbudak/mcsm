package api

import "net/http"

type temperatureResp struct {
	Celsius   float64                  `json:"celsius"`
	Sensor    string                   `json:"sensor"`
	SampledAt string                   `json:"sampled_at,omitempty"`
	History   []map[string]any         `json:"history"`
}

// GET /api/v1/system/temperature
func (s *Server) handleSystemTemperature(w http.ResponseWriter, r *http.Request) {
	if s.temp == nil {
		WriteError(w, r, http.StatusServiceUnavailable, CodeInternal,
			"temperature monitoring is not configured (system.temperature.sensor unset)",
			nil)
		return
	}
	last, hist := s.temp.Snapshot()
	out := temperatureResp{
		Sensor:  s.temp.Sensor(),
		History: make([]map[string]any, 0, len(hist)),
	}
	if last != nil {
		out.Celsius = last.Celsius
		out.SampledAt = last.At.Format("2006-01-02T15:04:05.000Z")
	}
	for _, p := range hist {
		out.History = append(out.History, map[string]any{
			"at":      p.At,
			"celsius": p.Celsius,
		})
	}
	WriteJSON(w, r, http.StatusOK, out)
}
