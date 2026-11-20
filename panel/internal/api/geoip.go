package api

import "net/http"

// handleGeoIPStatus reports the opt-in feature state. enable/source come from
// settings; readiness/files/version come from the manager. Always 200 (even when
// disabled) so the frontend can render the toggle without a pre-check.
func (s *Server) handleGeoIPStatus(w http.ResponseWriter, r *http.Request) {
	gs := s.core.Settings().GeoIP
	st := s.core.GeoIPStatus()
	writeJSON(w, http.StatusOK, map[string]any{
		"enable":          gs.Enable,
		"source":          gs.Source,
		"ready":           st.Ready,
		"downloading":     st.Downloading,
		"files":           st.Files,
		"lastCheckAt":     st.LastCheckAt,
		"updateAvailable": st.UpdateAvailable,
		"error":           st.Error,
	})
}

func (s *Server) handleGeoIPCheck(w http.ResponseWriter, r *http.Request) {
	res, err := s.core.GeoIPCheck(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGeoIPUpdate(w http.ResponseWriter, r *http.Request) {
	if err := s.core.GeoIPUpdate(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleGeoIPLookup(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var ips []string
	if m, ok := body.(map[string]any); ok {
		if arr, ok := m["ips"].([]any); ok {
			for _, v := range arr {
				if str, ok := v.(string); ok {
					ips = append(ips, str)
				}
			}
		}
	}
	results, err := s.core.GeoIPLookup(r.Context(), ips)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}
