package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webclash/internal/core"
	"webclash/internal/util"
)

const testPassword = "test-pass-123"

func newTestServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("WEBUI_PASSWORD", testPassword)
	dir := t.TempDir()
	// Keep unit tests offline and deterministic: disable kernel autostart.
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"kernel":{"autoStart":false}}`), 0o600); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	c := core.New(dir)
	if err := c.Init(); err != nil {
		t.Fatalf("core.Init: %v", err)
	}
	t.Cleanup(func() { _ = c.Shutdown() })
	return New(c)
}

type resp struct {
	code    int
	header  http.Header
	body    string
	cookies []*http.Cookie
}

func do(s *Server, method, uri, body string, cookies []*http.Cookie) resp {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, "http://panel.local"+uri, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, "http://panel.local"+uri, nil)
	}
	r.RequestURI = uri
	r.RemoteAddr = "1.2.3.4:5678"
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	res := rec.Result()
	return resp{code: res.StatusCode, header: res.Header, body: rec.Body.String(), cookies: res.Cookies()}
}

func loginCookies(t *testing.T, s *Server) []*http.Cookie {
	t.Helper()
	r := do(s, "POST", "/api/auth/login", `{"password":"`+testPassword+`"}`, nil)
	if r.code != 200 {
		t.Fatalf("login failed: %d %s", r.code, r.body)
	}
	if len(r.cookies) == 0 {
		t.Fatal("login did not set a session cookie")
	}
	return r.cookies
}

func jsonField(t *testing.T, body, key string) any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("bad JSON %q: %v", body, err)
	}
	return m[key]
}

func TestHealthPublic(t *testing.T) {
	s := newTestServer(t)
	r := do(s, "GET", "/api/health", "", nil)
	if r.code != 200 {
		t.Fatalf("health code = %d", r.code)
	}
	if jsonField(t, r.body, "ok") != true || jsonField(t, r.body, "kernel") != "stopped" {
		t.Fatalf("health body = %s", r.body)
	}
}

func TestAuthStatusPublic(t *testing.T) {
	s := newTestServer(t)
	r := do(s, "GET", "/api/auth/status", "", nil)
	if r.code != 200 {
		t.Fatalf("status code = %d", r.code)
	}
	if jsonField(t, r.body, "authenticated") != false {
		t.Fatalf("expected unauthenticated: %s", r.body)
	}
	if jsonField(t, r.body, "hasPassword") != true {
		t.Fatalf("expected hasPassword true: %s", r.body)
	}
}

func TestSettingsRequiresAuth(t *testing.T) {
	s := newTestServer(t)
	r := do(s, "GET", "/api/settings", "", nil)
	if r.code != 401 {
		t.Fatalf("code = %d, want 401", r.code)
	}
	if jsonField(t, r.body, "error") != "未登录或会话已过期" {
		t.Fatalf("error envelope = %s", r.body)
	}
}

func TestLoginMissingPassword(t *testing.T) {
	s := newTestServer(t)
	r := do(s, "POST", "/api/auth/login", `{}`, nil)
	if r.code != 400 || jsonField(t, r.body, "error") != "请输入密码" {
		t.Fatalf("got %d %s", r.code, r.body)
	}
}

func TestLoginRateLimit(t *testing.T) {
	s := newTestServer(t)
	for i := 0; i < 5; i++ {
		r := do(s, "POST", "/api/auth/login", `{"password":"wrong"}`, nil)
		if r.code != 401 || jsonField(t, r.body, "error") != "密码错误" {
			t.Fatalf("attempt %d: got %d %s", i+1, r.code, r.body)
		}
	}
	r := do(s, "POST", "/api/auth/login", `{"password":"wrong"}`, nil)
	if r.code != 429 {
		t.Fatalf("6th attempt code = %d, want 429", r.code)
	}
	if msg, _ := jsonField(t, r.body, "error").(string); !strings.Contains(msg, "60 秒后再试") {
		t.Fatalf("429 message = %s", r.body)
	}
	// Even the correct password is rejected while blocked.
	r = do(s, "POST", "/api/auth/login", `{"password":"`+testPassword+`"}`, nil)
	if r.code != 429 {
		t.Fatalf("blocked correct login code = %d, want 429", r.code)
	}
}

func TestLoginSuccessAndAuthenticatedSettings(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	r := do(s, "GET", "/api/settings", "", cookies)
	if r.code != 200 {
		t.Fatalf("settings code = %d", r.code)
	}
	settings, _ := jsonField(t, r.body, "settings").(map[string]any)
	if settings == nil {
		t.Fatalf("missing settings: %s", r.body)
	}
	auth, _ := settings["auth"].(map[string]any)
	if auth["hasPassword"] != true {
		t.Fatalf("public auth = %v", auth)
	}
	if _, leaked := auth["passwordHash"]; leaked {
		t.Fatal("passwordHash leaked in public settings")
	}
	if _, leaked := auth["secret"]; leaked {
		t.Fatal("auth.secret leaked in public settings")
	}
	cfg, _ := jsonField(t, r.body, "config").(map[string]any)
	if cfg["dataDir"] == nil || cfg["path"] == nil {
		t.Fatalf("config block = %v", cfg)
	}
}

func TestSettingsPutValidation(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	r := do(s, "PUT", "/api/settings", `"a string"`, cookies)
	if r.code != 400 || jsonField(t, r.body, "error") != "请求体不合法" {
		t.Fatalf("string body: got %d %s", r.code, r.body)
	}
	r = do(s, "PUT", "/api/settings", `{"general":{"mode":"global"}}`, cookies)
	if r.code != 200 {
		t.Fatalf("valid put code = %d %s", r.code, r.body)
	}
	if jsonField(t, r.body, "result") != "saved" {
		t.Fatalf("result = %s", r.body)
	}
	settings, _ := jsonField(t, r.body, "settings").(map[string]any)
	general, _ := settings["general"].(map[string]any)
	if general["mode"] != "global" {
		t.Fatalf("mode not applied: %v", general)
	}
}

func TestControllerSecretRotates(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	before := s.core.Settings().Controller.Secret
	r := do(s, "POST", "/api/settings/controller-secret", "", cookies)
	if r.code != 200 {
		t.Fatalf("code = %d %s", r.code, r.body)
	}
	secret, _ := jsonField(t, r.body, "secret").(string)
	if secret == "" || secret == before {
		t.Fatalf("secret did not rotate: before=%q after=%q", before, secret)
	}
	if s.core.Settings().Controller.Secret != secret {
		t.Fatal("returned secret differs from persisted setting")
	}
}

func TestPasswordChangeFlow(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)

	if r := do(s, "POST", "/api/auth/password", `{"next":"newpass-123"}`, nil); r.code != 401 {
		t.Fatalf("unauth password change code = %d, want 401", r.code)
	}
	if r := do(s, "POST", "/api/auth/password", `{"current":"bad","next":"newpass-123"}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "当前密码不正确" {
		t.Fatalf("wrong current: got %d %s", r.code, r.body)
	}
	if r := do(s, "POST", "/api/auth/password", `{"current":"`+testPassword+`","next":"abc"}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "新密码长度至少 6 位" {
		t.Fatalf("short next: got %d %s", r.code, r.body)
	}
	r := do(s, "POST", "/api/auth/password", `{"current":"`+testPassword+`","next":"newpass-123"}`, cookies)
	if r.code != 200 || jsonField(t, r.body, "ok") != true {
		t.Fatalf("valid change: got %d %s", r.code, r.body)
	}
	// Old session is invalidated (secret rotated); new cookie works.
	if old := do(s, "GET", "/api/settings", "", cookies); old.code != 401 {
		t.Fatalf("old session still valid: %d", old.code)
	}
	if len(r.cookies) == 0 {
		t.Fatal("password change did not reissue session")
	}
	if fresh := do(s, "GET", "/api/settings", "", r.cookies); fresh.code != 200 {
		t.Fatalf("new session invalid: %d", fresh.code)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	r := do(s, "POST", "/api/auth/logout", "", cookies)
	if r.code != 200 || jsonField(t, r.body, "ok") != true {
		t.Fatalf("logout: got %d %s", r.code, r.body)
	}
	var cleared bool
	for _, c := range r.cookies {
		if c.Name == sessionCookie && (c.Value == "" || c.MaxAge < 0 || !c.Expires.IsZero()) {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("logout did not clear session cookie: %v", r.cookies)
	}
}

func TestStaticAndSPA(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)

	root := do(s, "GET", "/", "", nil)
	if root.code != 200 {
		t.Fatalf("GET / code = %d", root.code)
	}
	if root.header.Get("Accept-Ranges") != "none" {
		t.Fatalf("Accept-Ranges = %q", root.header.Get("Accept-Ranges"))
	}
	etag := root.header.Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag on /")
	}

	head := do(s, "HEAD", "/", "", nil)
	if head.code != 200 || head.body != "" {
		t.Fatalf("HEAD / code=%d body=%q", head.code, head.body)
	}
	if head.header.Get("Content-Length") == "" {
		t.Fatal("HEAD / missing Content-Length")
	}

	r304 := doWithHeader(s, "GET", "/", map[string]string{"If-None-Match": etag})
	if r304.code != 304 {
		t.Fatalf("If-None-Match code = %d, want 304", r304.code)
	}
	if r304.header.Get("Content-Length") != "" {
		t.Fatalf("304 should omit Content-Length, got %q", r304.header.Get("Content-Length"))
	}

	if spa := do(s, "GET", "/proxies", "", nil); spa.code != 200 {
		t.Fatalf("SPA /proxies code = %d", spa.code)
	}
	if r := do(s, "GET", "/api/nope", "", cookies); r.code != 404 || jsonField(t, r.body, "error") != "接口不存在" {
		t.Fatalf("GET /api/nope: got %d %s", r.code, r.body)
	}
	if r := do(s, "POST", "/api/nope", "", cookies); r.code != 404 {
		t.Fatalf("POST /api/nope code = %d, want 404", r.code)
	}
}

func doWithHeader(s *Server, method, uri string, headers map[string]string) resp {
	r := httptest.NewRequest(method, "http://panel.local"+uri, nil)
	r.RequestURI = uri
	r.RemoteAddr = "1.2.3.4:5678"
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	res := rec.Result()
	return resp{code: res.StatusCode, header: res.Header, body: rec.Body.String(), cookies: res.Cookies()}
}

func TestTraversalNoRedirect(t *testing.T) {
	s := newTestServer(t)
	for _, uri := range []string{"/assets/../index.html", "/..%2f..%2fetc/passwd"} {
		r := do(s, "GET", uri, "", nil)
		if r.code == 301 || r.code == 302 {
			t.Fatalf("traversal %q produced redirect %d (must normalize like Node)", uri, r.code)
		}
		if r.code != 200 && r.code != 400 && r.code != 404 {
			t.Fatalf("traversal %q unexpected code %d", uri, r.code)
		}
	}
}

func TestRateLimiterUnit(t *testing.T) {
	rl := newRateLimiter()
	verify := func(p string) bool { return p == "ok" }
	for i := 0; i < 5; i++ {
		if err := rl.login("ip", "bad", verify); err == nil {
			t.Fatal("expected failure")
		}
	}
	err := rl.login("ip", "bad", verify)
	if err == nil || !strings.Contains(err.Error(), "尝试次数过多") {
		t.Fatalf("expected block error, got %v", err)
	}
	if he, ok := err.(*util.HttpError); !ok || he.StatusCode != 429 {
		t.Fatalf("expected 429 HttpError, got %v", err)
	}
	// Success on a different IP clears its own record only.
	if err := rl.login("other", "ok", verify); err != nil {
		t.Fatalf("other ip login: %v", err)
	}
}
