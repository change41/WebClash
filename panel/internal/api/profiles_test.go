package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const localYAML = `proxies: []
rules:
  - MATCH,DIRECT
`

func addLocal(t *testing.T, s *Server, cookies []*http.Cookie, body string) string {
	t.Helper()
	r := do(s, "POST", "/api/profiles", body, cookies)
	if r.code != 200 {
		t.Fatalf("add profile: %d %s", r.code, r.body)
	}
	id, _ := jsonField(t, r.body, "id").(string)
	if id == "" {
		t.Fatalf("no id in %s", r.body)
	}
	return id
}

func TestProfileLocalCRUD(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)

	id := addLocal(t, s, cookies, `{"content":"`+strings.ReplaceAll(localYAML, "\n", `\n`)+`"}`)

	list := do(s, "GET", "/api/profiles", "", cookies)
	if list.code != 200 {
		t.Fatalf("list: %d", list.code)
	}
	if jsonField(t, list.body, "activeId") != id {
		t.Fatalf("first profile should auto-activate: %s", list.body)
	}
	profiles, _ := jsonField(t, list.body, "profiles").([]any)
	if len(profiles) != 1 {
		t.Fatalf("profiles = %v", profiles)
	}
	p0, _ := profiles[0].(map[string]any)
	if p0["type"] != "local" || p0["name"] != "配置 1" {
		t.Fatalf("meta = %v", p0)
	}

	// rename
	r := do(s, "PUT", "/api/profiles/"+id, `{"name":"  新名字  "}`, cookies)
	if r.code != 200 || jsonField(t, r.body, "name") != "新名字" {
		t.Fatalf("rename: %d %s", r.code, r.body)
	}
	if r := do(s, "PUT", "/api/profiles/"+id, `{"name":"   "}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "名称不能为空" {
		t.Fatalf("empty rename: %d %s", r.code, r.body)
	}
	if r := do(s, "PUT", "/api/profiles/"+id, `{"name":123}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "名称格式不正确" {
		t.Fatalf("non-string rename: %d %s", r.code, r.body)
	}
	if r := do(s, "PUT", "/api/profiles/"+id, `{"url":"https://example.com/sub"}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "本地配置没有订阅链接" {
		t.Fatalf("local url update: %d %s", r.code, r.body)
	}
	if r := do(s, "PUT", "/api/profiles/"+id, `{"url":123}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "订阅链接格式不正确" {
		t.Fatalf("non-string url update: %d %s", r.code, r.body)
	}
	if r := do(s, "PUT", "/api/profiles/"+id, `{}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "没有可修改的订阅信息" {
		t.Fatalf("empty update: %d %s", r.code, r.body)
	}

	// raw get / put
	raw := do(s, "GET", "/api/profiles/"+id+"/raw", "", cookies)
	if raw.code != 200 || jsonField(t, raw.body, "content") == nil {
		t.Fatalf("raw get: %d %s", raw.code, raw.body)
	}
	if r := do(s, "PUT", "/api/profiles/"+id+"/raw", `{"content":"   "}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "配置内容不能为空" {
		t.Fatalf("empty raw: %d %s", r.code, r.body)
	}
	newYAML := "proxies: []\nrules:\n  - MATCH,PROXY\n"
	r = do(s, "PUT", "/api/profiles/"+id+"/raw", `{"content":"`+strings.ReplaceAll(newYAML, "\n", `\n`)+`"}`, cookies)
	if r.code != 200 || jsonField(t, r.body, "meta") == nil {
		t.Fatalf("raw put: %d %s", r.code, r.body)
	}

	// per-profile settings
	r = do(s, "PUT", "/api/profiles/"+id+"/settings", `{"intervalHours":12,"testTimeout":3000}`, cookies)
	if r.code != 200 {
		t.Fatalf("settings: %d %s", r.code, r.body)
	}
	settings, _ := jsonField(t, r.body, "settings").(map[string]any)
	if settings["intervalHours"] != float64(12) || settings["testTimeout"] != float64(3000) {
		t.Fatalf("settings = %v", settings)
	}

	// delete
	if r := do(s, "DELETE", "/api/profiles/"+id, "", cookies); r.code != 200 || jsonField(t, r.body, "ok") != true {
		t.Fatalf("delete: %d %s", r.code, r.body)
	}
	list = do(s, "GET", "/api/profiles", "", cookies)
	if jsonField(t, list.body, "activeId") != nil {
		t.Fatalf("activeId should be null after deleting last: %s", list.body)
	}
}

func TestProfileActivateAndDeleteFallback(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	id1 := addLocal(t, s, cookies, `{"content":"`+strings.ReplaceAll(localYAML, "\n", `\n`)+`"}`)
	id2 := addLocal(t, s, cookies, `{"name":"second","content":"`+strings.ReplaceAll(localYAML, "\n", `\n`)+`"}`)

	if r := do(s, "POST", "/api/profiles/"+id2+"/activate", "", cookies); r.code != 200 {
		t.Fatalf("activate: %d %s", r.code, r.body)
	}
	list := do(s, "GET", "/api/profiles", "", cookies)
	if jsonField(t, list.body, "activeId") != id2 {
		t.Fatalf("activeId = %v, want %s", jsonField(t, list.body, "activeId"), id2)
	}
	// deleting the active one falls back to the first remaining
	if r := do(s, "DELETE", "/api/profiles/"+id2, "", cookies); r.code != 200 {
		t.Fatalf("delete active: %d", r.code)
	}
	list = do(s, "GET", "/api/profiles", "", cookies)
	if jsonField(t, list.body, "activeId") != id1 {
		t.Fatalf("fallback activeId = %v, want %s", jsonField(t, list.body, "activeId"), id1)
	}
}

func TestProfileValidationAnd404(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)

	if r := do(s, "POST", "/api/profiles", `{"name":123}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "名称格式不正确" {
		t.Fatalf("name validation: %d %s", r.code, r.body)
	}
	if r := do(s, "POST", "/api/profiles", `{"url":123}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "订阅链接格式不正确" {
		t.Fatalf("url validation: %d %s", r.code, r.body)
	}
	if r := do(s, "POST", "/api/profiles", `{"content":123}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "配置内容格式不正确" {
		t.Fatalf("content validation: %d %s", r.code, r.body)
	}
	if r := do(s, "POST", "/api/profiles", `{}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "请提供订阅链接或 YAML 内容" {
		t.Fatalf("empty add: %d %s", r.code, r.body)
	}

	missing := "deadbeefdeadbeef"
	for _, c := range []struct{ method, uri, body string }{
		{"PUT", "/api/profiles/" + missing, `{"name":"x"}`},
		{"DELETE", "/api/profiles/" + missing, ""},
		{"POST", "/api/profiles/" + missing + "/activate", ""},
		{"POST", "/api/profiles/" + missing + "/update", ""},
		{"GET", "/api/profiles/" + missing + "/raw", ""},
		{"PUT", "/api/profiles/" + missing + "/settings", `{}`},
	} {
		if r := do(s, c.method, c.uri, c.body, cookies); r.code != 404 || jsonField(t, r.body, "error") != "配置不存在" {
			t.Fatalf("%s %s: %d %s", c.method, c.uri, r.code, r.body)
		}
	}
}

func TestProfileRemoteAddAndUpdate(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("subscription-userinfo", "upload=1; download=2; total=3")
		_, _ = w.Write([]byte("proxies: []\nrules: [MATCH,DIRECT]\n"))
	}))
	defer srv.Close()

	s := newTestServer(t)
	cookies := loginCookies(t, s)
	id := addLocal(t, s, cookies, `{"url":"`+srv.URL+`#我的订阅"}`)

	list := do(s, "GET", "/api/profiles", "", cookies)
	profiles, _ := jsonField(t, list.body, "profiles").([]any)
	p0, _ := profiles[0].(map[string]any)
	if p0["type"] != "remote" || p0["name"] != "我的订阅" {
		t.Fatalf("remote meta = %v", p0)
	}
	if p0["userInfo"] == nil {
		t.Fatalf("userInfo missing: %v", p0)
	}

	// update-all refreshes the remote profile
	r := do(s, "POST", "/api/profiles/update-all", "", cookies)
	if r.code != 200 {
		t.Fatalf("update-all: %d %s", r.code, r.body)
	}
	updated, _ := jsonField(t, r.body, "updated").([]any)
	if len(updated) != 1 || updated[0] != id {
		t.Fatalf("updated = %v", updated)
	}
	if hits < 2 {
		t.Fatalf("subscription should be fetched again, hits=%d", hits)
	}

	// single update
	if r := do(s, "POST", "/api/profiles/"+id+"/update", "", cookies); r.code != 200 {
		t.Fatalf("update: %d %s", r.code, r.body)
	}
}

func TestProfileRemoteInfoAndReadOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("subscription-userinfo", "upload=1; download=2; total=3")
		_, _ = w.Write([]byte("proxies: []\nrules: [MATCH,DIRECT]\n"))
	}))
	defer srv.Close()

	s := newTestServer(t)
	cookies := loginCookies(t, s)
	id := addLocal(t, s, cookies, `{"url":"`+srv.URL+`","name":"旧名字"}`)

	// rename only
	r := do(s, "PUT", "/api/profiles/"+id, `{"name":"新名字"}`, cookies)
	if r.code != 200 || jsonField(t, r.body, "name") != "新名字" {
		t.Fatalf("rename: %d %s", r.code, r.body)
	}

	// remote content is viewable but read-only
	raw := do(s, "GET", "/api/profiles/"+id+"/raw", "", cookies)
	if raw.code != 200 || !strings.Contains(raw.body, "MATCH,DIRECT") {
		t.Fatalf("raw get: %d %s", raw.code, raw.body)
	}
	r = do(s, "PUT", "/api/profiles/"+id+"/raw", `{"content":"proxies: []\nrules: [MATCH,REJECT]\n"}`, cookies)
	if r.code != 400 || jsonField(t, r.body, "error") != "订阅配置内容为只读，请使用订阅设置中的扩展规则" {
		t.Fatalf("remote raw put: %d %s", r.code, r.body)
	}

	// switching the URL downloads the new subscription immediately
	var newHits int
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		newHits++
		_, _ = w.Write([]byte("proxies: []\nrules: [MATCH,REJECT]\n"))
	}))
	defer srv2.Close()
	r = do(s, "PUT", "/api/profiles/"+id, `{"url":"`+srv2.URL+`"}`, cookies)
	if r.code != 200 || jsonField(t, r.body, "url") != srv2.URL {
		t.Fatalf("url update: %d %s", r.code, r.body)
	}
	if newHits != 1 {
		t.Fatalf("new subscription should be fetched once, hits=%d", newHits)
	}
	raw = do(s, "GET", "/api/profiles/"+id+"/raw", "", cookies)
	if !strings.Contains(raw.body, "MATCH,REJECT") {
		t.Fatalf("raw content not refreshed: %s", raw.body)
	}
	if jsonField(t, r.body, "userInfo") != nil {
		t.Fatalf("userInfo should be cleared when the new URL sends none: %s", r.body)
	}

	// invalid and empty URLs are rejected
	if r := do(s, "PUT", "/api/profiles/"+id, `{"url":"ftp://example.com/sub"}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "订阅链接仅支持 http/https" {
		t.Fatalf("bad scheme: %d %s", r.code, r.body)
	}
	if r := do(s, "PUT", "/api/profiles/"+id, `{"url":"  "}`, cookies); r.code != 400 || jsonField(t, r.body, "error") != "订阅链接不能为空" {
		t.Fatalf("empty url: %d %s", r.code, r.body)
	}
}

func TestProfileLocalCannotUpdate(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	id := addLocal(t, s, cookies, `{"content":"`+strings.ReplaceAll(localYAML, "\n", `\n`)+`"}`)
	if r := do(s, "POST", "/api/profiles/"+id+"/update", "", cookies); r.code != 400 || jsonField(t, r.body, "error") != "本地配置无法从订阅更新" {
		t.Fatalf("local update: %d %s", r.code, r.body)
	}
}
