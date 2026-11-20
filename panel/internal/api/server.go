package api

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"webclash/internal/core"
	"webclash/internal/webui"
)

const maxBodyBytes = 32 * 1024 * 1024

var publicAPI = map[string]bool{
	"/api/auth/status": true,
	"/api/auth/login":  true,
	"/api/health":      true,
}

type route struct {
	method  string
	segs    []string
	handler http.HandlerFunc
}

// Server is the HTTP panel server (port of app.ts buildApp). It uses a literal
// segment router rather than http.ServeMux so traversal/dot paths are NOT 301
// redirected — matching Fastify, which treats them as unmatched (→ SPA / 404).
type Server struct {
	core    *core.Core
	limiter *rateLimiter
	table   *webui.Table
	routes  []route
	wsConns sync.Map
}

type paramsKey struct{}

// New builds the server, loads embedded assets and registers G2 routes.
func New(c *core.Core) *Server {
	s := &Server{
		core:    c,
		limiter: newRateLimiter(),
		table:   webui.Load(),
	}
	s.add(http.MethodGet, "/api/health", s.handleHealth)
	s.add(http.MethodGet, "/api/auth/status", s.handleAuthStatus)
	s.add(http.MethodPost, "/api/auth/login", s.handleAuthLogin)
	s.add(http.MethodPost, "/api/auth/logout", s.handleAuthLogout)
	s.add(http.MethodPost, "/api/auth/password", s.handleAuthPassword)
	s.add(http.MethodGet, "/api/settings", s.handleSettingsGet)
	s.add(http.MethodPut, "/api/settings", s.handleSettingsPut)
	s.add(http.MethodPost, "/api/settings/controller-secret", s.handleControllerSecret)
	s.add(http.MethodGet, "/api/profiles", s.handleProfilesList)
	s.add(http.MethodPost, "/api/profiles", s.handleProfilesAdd)
	s.add(http.MethodPost, "/api/profiles/update-all", s.handleProfilesUpdateAll)
	s.add(http.MethodGet, "/api/profiles/{id}/raw", s.handleProfileRawGet)
	s.add(http.MethodPut, "/api/profiles/{id}/raw", s.handleProfileRawPut)
	s.add(http.MethodPut, "/api/profiles/{id}/settings", s.handleProfileSettings)
	s.add(http.MethodPut, "/api/profiles/{id}", s.handleProfileInfo)
	s.add(http.MethodDelete, "/api/profiles/{id}", s.handleProfileDelete)
	s.add(http.MethodPost, "/api/profiles/{id}/update", s.handleProfileUpdate)
	s.add(http.MethodPost, "/api/profiles/{id}/activate", s.handleProfileActivate)
	s.add(http.MethodGet, "/api/kernel/status", s.handleKernelStatus)
	s.add(http.MethodGet, "/api/kernel/logs", s.handleKernelLogs)
	s.add(http.MethodPost, "/api/kernel/start", s.handleKernelStart)
	s.add(http.MethodPost, "/api/kernel/stop", s.handleKernelStop)
	s.add(http.MethodPost, "/api/kernel/restart", s.handleKernelRestart)
	s.add(http.MethodPost, "/api/kernel/check-update", s.handleKernelCheckUpdate)
	s.add(http.MethodPost, "/api/kernel/download", s.handleKernelDownload)
	s.add(http.MethodPost, "/api/kernel/cancel-download", s.handleKernelCancelDownload)
	s.add(http.MethodGet, "/api/clash/version", s.handleClashVersion)
	s.add(http.MethodGet, "/api/clash/configs", s.handleClashConfigsGet)
	s.add(http.MethodPatch, "/api/clash/configs", s.handleClashConfigsPatch)
	s.add(http.MethodGet, "/api/clash/proxies", s.handleClashProxies)
	s.add(http.MethodPost, "/api/clash/select", s.handleClashSelect)
	s.add(http.MethodGet, "/api/clash/proxy-delay", s.handleClashProxyDelay)
	s.add(http.MethodGet, "/api/clash/group-delay", s.handleClashGroupDelay)
	s.add(http.MethodGet, "/api/clash/providers", s.handleClashProviders)
	s.add(http.MethodPost, "/api/clash/provider-update", s.handleClashProviderUpdate)
	s.add(http.MethodGet, "/api/clash/provider-healthcheck", s.handleClashProviderHealthcheck)
	s.add(http.MethodGet, "/api/clash/connections", s.handleClashConnectionsGet)
	s.add(http.MethodDelete, "/api/clash/connections", s.handleClashConnectionsDelete)
	s.add(http.MethodDelete, "/api/clash/connections/{id}", s.handleClashConnectionDelete)
	s.add(http.MethodGet, "/api/connections/history", s.handleConnHistory)
	s.add(http.MethodGet, "/api/system/status", s.handleSystemStatus)
	s.add(http.MethodGet, "/api/traffic/history", s.handleTrafficHistory)
	s.add(http.MethodGet, "/api/clash/rules", s.handleClashRules)
	s.add(http.MethodGet, "/api/clash/rule-providers", s.handleClashRuleProviders)
	s.add(http.MethodGet, "/api/clash/rule-provider-content", s.handleClashRuleProviderContent)
	s.add(http.MethodGet, "/api/clash/dns-query", s.handleClashDNSQuery)
	s.add(http.MethodGet, "/api/geoip/status", s.handleGeoIPStatus)
	s.add(http.MethodPost, "/api/geoip/check", s.handleGeoIPCheck)
	s.add(http.MethodPost, "/api/geoip/update", s.handleGeoIPUpdate)
	s.add(http.MethodPost, "/api/geoip/lookup", s.handleGeoIPLookup)
	return s
}

func (s *Server) add(method, pattern string, h http.HandlerFunc) {
	segs := splitPath(pattern)
	s.routes = append(s.routes, route{method: method, segs: segs, handler: h})
	if method == http.MethodGet {
		s.routes = append(s.routes, route{method: http.MethodHead, segs: segs, handler: h})
	}
}

func splitPath(p string) []string {
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}

func matchSegs(pat, segs []string) (map[string]string, bool) {
	if len(pat) != len(segs) {
		return nil, false
	}
	var params map[string]string
	for i, p := range pat {
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			if params == nil {
				params = map[string]string{}
			}
			params[p[1:len(p)-1]] = segs[i]
			continue
		}
		if p != segs[i] {
			return nil, false
		}
	}
	return params, true
}

// PathParam returns a wildcard segment value captured by the router.
func PathParam(r *http.Request, name string) string {
	if params, ok := r.Context().Value(paramsKey{}).(map[string]string); ok {
		return params[name]
	}
	return ""
}

func (s *Server) matchAPI(r *http.Request, pathname string) (*http.Request, http.HandlerFunc) {
	segs := splitPath(pathname)
	for _, rt := range s.routes {
		if rt.method != r.Method {
			continue
		}
		if params, ok := matchSegs(rt.segs, segs); ok {
			if params != nil {
				r = r.WithContext(context.WithValue(r.Context(), paramsKey{}, params))
			}
			return r, rt.handler
		}
	}
	return r, nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"kernel": s.core.Kernel.State(),
	})
}

// ServeHTTP applies the body limit + auth gate, then routes API or static/SPA.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	pathname := strings.SplitN(r.RequestURI, "?", 2)[0]
	if strings.HasPrefix(pathname, "/api/") && !publicAPI[pathname] && !s.isAuthenticated(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "未登录或会话已过期"})
		return
	}
	if strings.HasPrefix(pathname, "/ws/") && isWebSocketUpgrade(r) {
		s.handleWS(w, r)
		return
	}
	if r2, h := s.matchAPI(r, pathname); h != nil {
		h(w, r2)
		return
	}
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && s.serveStatic(w, r) {
		return
	}
	s.notFound(w, r)
}
