package api

import (
	"net/http"
	"strings"

	"webclash/internal/core"
	"webclash/internal/util"
)

func (s *Server) handleProfilesList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.core.ListProfiles())
}

func (s *Server) handleProfilesAdd(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	m, _ := body.(map[string]any)
	for _, f := range []struct{ key, msg string }{
		{"name", "名称格式不正确"},
		{"url", "订阅链接格式不正确"},
		{"content", "配置内容格式不正确"},
	} {
		if v, present := m[f.key]; present {
			if _, isStr := v.(string); !isStr {
				writeError(w, util.NewHttpError(400, f.msg))
				return
			}
		}
	}
	input := core.AddProfileInput{Settings: m["settings"]}
	if v, ok := m["name"].(string); ok {
		input.Name = &v
	}
	if v, ok := m["url"].(string); ok {
		input.URL = &v
	}
	if v, ok := m["content"].(string); ok {
		input.Content = &v
	}
	meta, err := s.core.AddProfile(input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleProfileSettings(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	meta, err := s.core.UpdateProfileSettings(PathParam(r, "id"), body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleProfileInfo(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	m, _ := body.(map[string]any)
	input := core.UpdateProfileInput{}
	if v, present := m["name"]; present {
		name, ok := v.(string)
		if !ok {
			writeError(w, util.NewHttpError(400, "名称格式不正确"))
			return
		}
		input.Name = &name
	}
	if v, present := m["url"]; present {
		raw, ok := v.(string)
		if !ok {
			writeError(w, util.NewHttpError(400, "订阅链接格式不正确"))
			return
		}
		input.URL = &raw
	}
	meta, err := s.core.UpdateProfileInfo(PathParam(r, "id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleProfileDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.core.DeleteProfile(PathParam(r, "id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleProfileUpdate(w http.ResponseWriter, r *http.Request) {
	meta, err := s.core.UpdateProfile(PathParam(r, "id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleProfilesUpdateAll(w http.ResponseWriter, r *http.Request) {
	res, err := s.core.UpdateAllProfiles()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleProfileActivate(w http.ResponseWriter, r *http.Request) {
	meta, err := s.core.ActivateProfile(PathParam(r, "id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleProfileRawGet(w http.ResponseWriter, r *http.Request) {
	res, err := s.core.ReadProfileRaw(PathParam(r, "id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleProfileRawPut(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	m, _ := body.(map[string]any)
	content, ok := m["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		writeError(w, util.NewHttpError(400, "配置内容不能为空"))
		return
	}
	meta, err := s.core.SaveProfileRaw(PathParam(r, "id"), content)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"meta": meta})
}
