package mihomo

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"webclash/internal/config"
)

func parseBase(t *testing.T, text string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("parse base: %v", err)
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return nil
}

func buildToMap(t *testing.T, base *yaml.Node, s config.Settings) map[string]any {
	t.Helper()
	out := BuildConfig(base, s)
	b, err := yaml.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatalf("reparse: %v\n%s", err, b)
	}
	return m
}

const subscriptionYAML = `
mixed-port: 1080
external-ui: ./ui
external-controller: 0.0.0.0:9999
secret: original-secret
unified-delay: false
proxies:
  - name: "节点A"
    type: ss
    server: 1.2.3.4
    port: 8388
    cipher: aes-256-gcm
    password: "123456"
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - "节点A"
rule-providers:
  reject:
    type: http
    behavior: domain
    url: "https://example.com/reject.yaml"
    path: ./ruleset/reject.yaml
    interval: 86400
dns:
  enable: false
  nameserver-policy:
    "geosite:cn": "223.5.5.5"
  fake-ip-filter:
    - "*.lan"
rules:
  - "GEOSITE,cn,DIRECT"
  - "MATCH,PROXY"
`

func TestBuildConfigInjectsPanelKeys(t *testing.T) {
	s := config.DefaultSettings()
	s.Controller.Port = 9090
	s.Controller.Secret = "panel-secret"
	m := buildToMap(t, parseBase(t, subscriptionYAML), s)

	if m["external-ui"] != nil {
		t.Fatal("external-ui should be removed")
	}
	if m["external-controller"] != "127.0.0.1:9090" {
		t.Fatalf("external-controller = %v", m["external-controller"])
	}
	if m["secret"] != "panel-secret" {
		t.Fatalf("secret = %v", m["secret"])
	}
	if m["mixed-port"] != 7890 {
		t.Fatalf("mixed-port = %v", m["mixed-port"])
	}
	// unified-delay was explicitly false in the base → preserved (not undefined).
	if m["unified-delay"] != false {
		t.Fatalf("unified-delay = %v, want false (preserved)", m["unified-delay"])
	}
	if m["tcp-concurrent"] != true {
		t.Fatalf("tcp-concurrent = %v, want true (default)", m["tcp-concurrent"])
	}
	if m["find-process-mode"] != "strict" {
		t.Fatalf("find-process-mode = %v", m["find-process-mode"])
	}
	if m["port"] != nil || m["socks-port"] != nil {
		t.Fatalf("legacy ports should be removed: %v %v", m["port"], m["socks-port"])
	}
}

func TestBuildConfigPreservesPassthrough(t *testing.T) {
	s := config.DefaultSettings()
	base := parseBase(t, subscriptionYAML)
	out := BuildConfig(base, s)
	b, err := yaml.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(b)

	// Numeric-looking password must stay a STRING (quoted), not become an int.
	if !strings.Contains(text, `password: "123456"`) && !strings.Contains(text, `password: '123456'`) {
		t.Fatalf("password lost string typing:\n%s", text)
	}
	m := buildToMap(t, parseBase(t, subscriptionYAML), s)
	proxies, _ := m["proxies"].([]any)
	if len(proxies) != 1 {
		t.Fatalf("proxies = %v", m["proxies"])
	}
	p0, _ := proxies[0].(map[string]any)
	if p0["password"] != "123456" {
		t.Fatalf("password reparsed as %T %v, want string", p0["password"], p0["password"])
	}
	if p0["name"] != "节点A" {
		t.Fatalf("unicode name = %v", p0["name"])
	}
	// proxy-groups and rule-providers survive untouched.
	if _, ok := m["proxy-groups"].([]any); !ok {
		t.Fatalf("proxy-groups lost: %v", m["proxy-groups"])
	}
	rp, _ := m["rule-providers"].(map[string]any)
	if rp["reject"] == nil {
		t.Fatalf("rule-providers.reject lost: %v", rp)
	}
	// DNS extensions (nameserver-policy / fake-ip-filter) preserved.
	dns, _ := m["dns"].(map[string]any)
	if dns["nameserver-policy"] == nil {
		t.Fatalf("nameserver-policy lost: %v", dns)
	}
	if dns["fake-ip-filter"] == nil {
		t.Fatalf("fake-ip-filter lost: %v", dns)
	}
	if dns["enable"] != true {
		t.Fatalf("dns.enable should be panel-controlled true, got %v", dns["enable"])
	}
	// Original rules preserved (non-empty).
	rules, _ := m["rules"].([]any)
	if len(rules) != 2 || rules[0] != "GEOSITE,cn,DIRECT" {
		t.Fatalf("rules = %v", rules)
	}
}

func TestBuildConfigBuiltinFallbacks(t *testing.T) {
	s := config.DefaultSettings()
	m := buildToMap(t, nil, s)
	proxies, _ := m["proxies"].([]any)
	if proxies == nil || len(proxies) != 0 {
		t.Fatalf("builtin proxies = %v, want []", m["proxies"])
	}
	rules, _ := m["rules"].([]any)
	if len(rules) != 1 || rules[0] != "MATCH,DIRECT" {
		t.Fatalf("builtin rules = %v, want [MATCH,DIRECT]", m["rules"])
	}
	if m["external-controller"] != "127.0.0.1:9090" {
		t.Fatalf("external-controller = %v", m["external-controller"])
	}
}

func TestBuildConfigEmptyRulesFallback(t *testing.T) {
	s := config.DefaultSettings()
	base := parseBase(t, "proxies: []\nrules: []\n")
	m := buildToMap(t, base, s)
	rules, _ := m["rules"].([]any)
	if len(rules) != 1 || rules[0] != "MATCH,DIRECT" {
		t.Fatalf("empty rules fallback = %v", m["rules"])
	}
}

func TestBuildConfigAllowLanBindAddress(t *testing.T) {
	s := config.DefaultSettings()
	s.General.AllowLan = true
	s.General.BindAddress = ""
	m := buildToMap(t, nil, s)
	if m["bind-address"] != "*" {
		t.Fatalf("bind-address = %v, want *", m["bind-address"])
	}
	s.General.AllowLan = false
	m = buildToMap(t, parseBase(t, "bind-address: 1.2.3.4\nrules: [MATCH,DIRECT]\n"), s)
	if m["bind-address"] != nil {
		t.Fatalf("bind-address should be deleted when allowLan false, got %v", m["bind-address"])
	}
}

func TestBuildConfigStandalonePorts(t *testing.T) {
	s := config.DefaultSettings()
	s.Ports.HTTP = 7891
	s.Ports.SOCKS = 7892
	m := buildToMap(t, parseBase(t, subscriptionYAML), s)
	if m["port"] != 7891 {
		t.Fatalf("port = %v, want 7891", m["port"])
	}
	if m["socks-port"] != 7892 {
		t.Fatalf("socks-port = %v, want 7892", m["socks-port"])
	}
	if m["mixed-port"] != 7890 {
		t.Fatalf("mixed-port = %v, want 7890", m["mixed-port"])
	}
}

func TestBuildConfigDisabledPortsRemoved(t *testing.T) {
	s := config.DefaultSettings()
	base := parseBase(t, "port: 1234\nsocks-port: 1235\nrules: [MATCH,DIRECT]\n")
	m := buildToMap(t, base, s)
	if m["port"] != nil || m["socks-port"] != nil {
		t.Fatalf("disabled ports should remove subscription entries: %v %v", m["port"], m["socks-port"])
	}
}

func TestBuildConfigPreservesSubscriptionListeners(t *testing.T) {
	s := config.DefaultSettings()
	base := parseBase(t, `
listeners:
  - name: custom-in
    type: socks
    port: 1080
rules: [MATCH,DIRECT]
`)
	m := buildToMap(t, base, s)
	ls, _ := m["listeners"].([]any)
	if len(ls) != 1 {
		t.Fatalf("listeners = %v, want the subscription entry untouched", m["listeners"])
	}
	l, _ := ls[0].(map[string]any)
	if l["name"] != "custom-in" || l["port"] != 1080 {
		t.Fatalf("listener = %v", l)
	}
}

func TestBuildConfigAuthentication(t *testing.T) {
	s := config.DefaultSettings()
	s.Authentication = []config.InboundUser{
		{Username: "alice", Password: "secret"},
		{Username: "bob", Password: "p@ss:with:colon"},
	}
	m := buildToMap(t, parseBase(t, "authentication:\n  - old:pass\nrules: [MATCH,DIRECT]\n"), s)
	auth, _ := m["authentication"].([]any)
	if len(auth) != 2 || auth[0] != "alice:secret" || auth[1] != "bob:p@ss:with:colon" {
		t.Fatalf("authentication = %#v", m["authentication"])
	}

	s.Authentication = []config.InboundUser{}
	m = buildToMap(t, parseBase(t, "authentication:\n  - old:pass\nrules: [MATCH,DIRECT]\n"), s)
	if _, ok := m["authentication"]; ok {
		t.Fatalf("subscription authentication should be removed when panel list is empty: %v", m["authentication"])
	}
}

func TestBuildConfigPrependsGlobalCustomRules(t *testing.T) {
	s := config.DefaultSettings()
	s.CustomRules = []string{"DOMAIN,custom.test,DIRECT"}
	m := buildToMap(t, parseBase(t, subscriptionYAML), s)
	rules, _ := m["rules"].([]any)
	want := []string{"DOMAIN,custom.test,DIRECT", "GEOSITE,cn,DIRECT", "MATCH,PROXY"}
	if len(rules) != len(want) {
		t.Fatalf("rules = %v, want %v", rules, want)
	}
	for i := range want {
		if rules[i] != want[i] {
			t.Fatalf("rules[%d] = %v, want %v", i, rules[i], want[i])
		}
	}
}

func TestBuildConfigCustomRulesWithBuiltinFallback(t *testing.T) {
	s := config.DefaultSettings()
	s.CustomRules = []string{"DOMAIN,custom.test,DIRECT"}
	m := buildToMap(t, nil, s)
	rules, _ := m["rules"].([]any)
	want := []string{"DOMAIN,custom.test,DIRECT", "MATCH,DIRECT"}
	if len(rules) != len(want) || rules[0] != want[0] || rules[1] != want[1] {
		t.Fatalf("rules = %v, want %v", rules, want)
	}
}

func TestPrependRulesNoop(t *testing.T) {
	if got := PrependRules(nil, []string{"DOMAIN,a,DIRECT"}); got != nil {
		t.Fatal("nil config should stay nil")
	}
	cfg := parseBase(t, "rules:\n  - MATCH,DIRECT\n")
	PrependRules(cfg, nil)
	if rules := MapGet(cfg, "rules"); rules == nil || len(rules.Content) != 1 {
		t.Fatalf("empty rules should not change config: %+v", rules)
	}
}
