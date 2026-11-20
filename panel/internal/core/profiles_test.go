package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseClashConfigValidation(t *testing.T) {
	if _, err := parseClashConfig("proxies: []\n"); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if _, err := parseClashConfig("rules: [MATCH,DIRECT]\n"); err != nil {
		t.Fatalf("rules-only rejected: %v", err)
	}
	cases := []struct{ text, want string }{
		{"just a string", "顶层必须是对象"},
		{"- a\n- b\n", "顶层必须是对象"},
		{"{}", "未找到 proxies"},
		{"foo: bar\n", "未找到 proxies"},
		{"proxies: [unclosed\n", "YAML 解析失败"},
	}
	for _, c := range cases {
		_, err := parseClashConfig(c.text)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("parseClashConfig(%q) err = %v, want contains %q", c.text, err, c.want)
		}
	}
}

func TestNameFromSubscriptionUrl(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://example.com/sub#MyName", "MyName"},
		{"https://example.com/sub", "example.com"},
		{"https://example.com/#%E8%8A%82%E7%82%B9", "节点"},
		{"", ""},
		{"https://example.com/#%20", "example.com"},
	}
	for _, c := range cases {
		if got := nameFromSubscriptionUrl(c.url); got != c.want {
			t.Fatalf("nameFromSubscriptionUrl(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestParseRuleSetText(t *testing.T) {
	got, err := parseRuleSet("  # comment\nDOMAIN,a.com\n\n  DOMAIN,b.com  \r\n", "text")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 || got[0] != "DOMAIN,a.com" || got[1] != "DOMAIN,b.com" {
		t.Fatalf("text rules = %v", got)
	}
}

func TestParseRuleSetYaml(t *testing.T) {
	got, err := parseRuleSet("payload:\n  - DOMAIN,x\n  - 123\n", "yaml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 || got[0] != "DOMAIN,x" || got[1] != "123" {
		t.Fatalf("yaml rules = %v", got)
	}
	if _, err := parseRuleSet("foo: bar\n", "yaml"); err == nil {
		t.Fatal("expected missing-payload error")
	}
}

func TestParseRuleSetTooLarge(t *testing.T) {
	big := strings.Repeat("a", maxRuleSetChars+1)
	_, err := parseRuleSet(big, "text")
	if err == nil || !strings.Contains(err.Error(), "规则集内容过大") {
		t.Fatalf("expected 413, got %v", err)
	}
}

func TestFetchSubscription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "clash.meta" {
			t.Errorf("UA = %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("subscription-userinfo", "upload=100; download=200; total=1000; expire=1893456000")
		_, _ = w.Write([]byte("proxies: []\nrules: [MATCH,DIRECT]\n"))
	}))
	defer srv.Close()

	text, ui, err := fetchSubscription(srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(text, "proxies") {
		t.Fatalf("text = %q", text)
	}
	if ui == nil || ui.Upload == nil || *ui.Upload != 100 || ui.Total == nil || *ui.Total != 1000 {
		t.Fatalf("userInfo = %+v", ui)
	}
}

func TestFetchSubscriptionErrors(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("   "))
	}))
	defer empty.Close()
	if _, _, err := fetchSubscription(empty.URL); err == nil || !strings.Contains(err.Error(), "订阅内容为空") {
		t.Fatalf("empty: %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not: a clash config\n"))
	}))
	defer bad.Close()
	if _, _, err := fetchSubscription(bad.URL); err == nil || !strings.Contains(err.Error(), "未找到 proxies") {
		t.Fatalf("bad yaml: %v", err)
	}

	httpErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer httpErr.Close()
	if _, _, err := fetchSubscription(httpErr.URL); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("http 500: %v", err)
	}

	if _, _, err := fetchSubscription("ftp://example.com"); err == nil || !strings.Contains(err.Error(), "仅支持 http/https") {
		t.Fatalf("scheme: %v", err)
	}
	if _, _, err := fetchSubscription("not a url"); err == nil || !strings.Contains(err.Error(), "格式不正确") {
		t.Fatalf("malformed: %v", err)
	}
}
