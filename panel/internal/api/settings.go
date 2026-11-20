package api

import (
	"net/http"

	"webclash/internal/util"
)

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": publicSettings(s.core.Settings()),
		"config": map[string]any{
			"path":            s.core.ConfigPath,
			"dataDir":         s.core.DataDir,
			"lastConfigAt":    s.core.LastConfigAt(),
			"lastConfigError": s.core.LastConfigError(),
		},
	})
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	// Node: typeof body === 'object' && body !== null (arrays qualify too).
	switch body.(type) {
	case map[string]any, []any:
	default:
		writeError(w, util.NewHttpError(400, "请求体不合法"))
		return
	}
	result, err := s.core.SaveSettings(body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"result":   result,
		"settings": publicSettings(s.core.Settings()),
	})
}

func (s *Server) handleControllerSecret(w http.ResponseWriter, r *http.Request) {
	secret, err := s.core.RotateControllerSecret()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"secret":   secret,
		"settings": publicSettings(s.core.Settings()),
	})
}
