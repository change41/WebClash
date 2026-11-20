package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"webclash/internal/config"
	"webclash/internal/util"
)

const (
	sessionCookie = "webclash_session"
	sessionTTLMs  = 7 * 24 * 3600 * 1000
)

// cookieValue replicates auth.ts parseCookieHeader for a single key: split on
// ';', first '=', trim both sides, decodeURIComponent with raw fallback.
func cookieValue(r *http.Request, name string) string {
	header := r.Header.Get("Cookie")
	if header == "" {
		return ""
	}
	for _, part := range strings.Split(header, ";") {
		idx := strings.IndexByte(part, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(part[:idx])
		if key != name {
			continue
		}
		value := strings.TrimSpace(part[idx+1:])
		if decoded, err := url.PathUnescape(value); err == nil {
			return decoded
		}
		return value
	}
	return ""
}

func (s *Server) isAuthenticated(r *http.Request) bool {
	return util.VerifySession(cookieValue(r, sessionCookie), s.core.Settings().Auth.Secret)
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request) {
	token := util.SignSession(s.core.Settings().Auth.Secret, sessionTTLMs)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionTTLMs / 1000,
		Secure:   isHTTPS(r),
	})
}

func clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:    sessionCookie,
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0),
	})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	settings := s.core.Settings()
	hasPassword := settings.Auth.PasswordHash != nil && *settings.Auth.PasswordHash != ""
	writeJSON(w, http.StatusOK, map[string]any{
		"name":          util.AppName,
		"version":       util.AppVersion(),
		"authenticated": s.isAuthenticated(r),
		"hasPassword":   hasPassword,
		"kernelRunning": s.core.Kernel.IsRunning(),
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	m, _ := body.(map[string]any)
	password, ok := m["password"].(string)
	if !ok || password == "" {
		writeError(w, util.NewHttpError(400, "请输入密码"))
		return
	}
	hash := ""
	if h := s.core.Settings().Auth.PasswordHash; h != nil {
		hash = *h
	}
	err = s.limiter.login(clientIP(r), password, func(p string) bool {
		return util.VerifyPassword(p, hash)
	})
	if err != nil {
		writeError(w, err)
		return
	}
	s.issueSession(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	clearSession(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAuthPassword(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthenticated(r) {
		writeError(w, util.NewHttpError(401, "未登录"))
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	m, _ := body.(map[string]any)
	next, ok := m["next"].(string)
	if !ok {
		writeError(w, util.NewHttpError(400, "新密码格式不正确"))
		return
	}
	var current *string
	if c, ok := m["current"].(string); ok {
		current = &c
	}
	if err := s.core.ChangePassword(current, next); err != nil {
		writeError(w, err)
		return
	}
	s.issueSession(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// publicSettings is a small alias so route files read like the Node oracle.
func publicSettings(s config.Settings) config.PublicSettings { return config.ToPublic(s) }
