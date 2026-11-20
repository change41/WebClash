package api

import (
	"net/http"

	"webclash/internal/util"
)

func (s *Server) handleSystemProxyStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.core.SystemProxyStatus())
}

func (s *Server) handleSystemProxyEnable(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	confirmed := false
	if m, ok := body.(map[string]any); ok {
		confirmed, _ = m["confirmed"].(bool)
	}
	if !confirmed {
		writeError(w, util.NewHttpError(http.StatusBadRequest, "请完成二次确认后再开启系统代理"))
		return
	}
	status, err := s.core.EnableSystemProxy()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   status,
		"settings": publicSettings(s.core.Settings()),
	})
}

func (s *Server) handleSystemProxyDisable(w http.ResponseWriter, r *http.Request) {
	status, err := s.core.DisableSystemProxy()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   status,
		"settings": publicSettings(s.core.Settings()),
	})
}

func (s *Server) handleSystemProxySync(w http.ResponseWriter, r *http.Request) {
	if _, err := s.core.SyncSystemProxy(); err != nil {
		writeError(w, util.NewHttpError(http.StatusInternalServerError, util.ErrorMessage(err)))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   s.core.SystemProxyStatus(),
		"settings": publicSettings(s.core.Settings()),
	})
}
