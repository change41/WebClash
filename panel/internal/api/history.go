package api

import (
	"net/http"
	"strconv"
)

// handleConnHistory returns recently closed connections kept in panel memory
// (newest first). ?limit= caps the page (default 500); the response "limit" is
// the configured retention capacity so the UI can explain eviction.
func (s *Server) handleConnHistory(w http.ResponseWriter, r *http.Request) {
	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connections": s.core.ConnHistory(limit),
		"limit":       s.core.ConnHistoryLimit(),
	})
}
