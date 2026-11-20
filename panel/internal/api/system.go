package api

import "net/http"

// handleSystemStatus returns the latest host CPU/memory/load sample. It works
// without a running kernel (ready=false until the first CPU window lands).
func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.core.SystemStatus())
}

// handleTrafficHistory returns the in-memory 24h of 1-minute average rates.
func (s *Server) handleTrafficHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.core.TrafficHistory())
}
