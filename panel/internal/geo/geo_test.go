package geo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"webclash/internal/mihomo"
)

func parseCfg(t *testing.T, text string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func toMap(t *testing.T, cfg *yaml.Node) map[string]any {
	t.Helper()
	b, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatalf("reparse: %v\n%s", err, b)
	}
	return m
}

func rulesOf(t *testing.T, cfg *yaml.Node) []string {
	t.Helper()
	m := toMap(t, cfg)
	raw, _ := m["rules"].([]any)
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.(string))
	}
	return out
}

func TestAnalyze(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want Needed
	}{
		{"empty", "mode: rule\n", Needed{}},
		{"no geo rules", "rules:\n  - DOMAIN-SUFFIX,example.com,DIRECT\n  - MATCH,DIRECT\n", Needed{}},
		{"geosite rule", "rules:\n  - GEOSITE,cn,DIRECT\n", Needed{GeoSite: true}},
		{"geoip rule", "rules:\n  - GEOIP,CN,DIRECT\n", Needed{GeoIP: true}},
		{"both rules", "rules:\n  - GEOSITE,cn,DIRECT\n  - GEOIP,CN,DIRECT\n", Needed{GeoIP: true, GeoSite: true}},
		{"lowercase rule token", "rules:\n  - geosite,cn,DIRECT\n", Needed{GeoSite: true}},
		{"dns nameserver-policy geosite", "dns:\n  nameserver-policy:\n    \"geosite:cn\":\n      - 223.5.5.5\n", Needed{GeoSite: true}},
		{"dns fake-ip-filter geoip", "dns:\n  fake-ip-filter:\n    - \"geoip:cn\"\n", Needed{GeoIP: true}},
		{"dns both refs", "dns:\n  nameserver-policy:\n    \"geosite:cn\": [223.5.5.5]\n    \"geoip:cn\": [119.29.29.29]\n", Needed{GeoIP: true, GeoSite: true}},
		{"rule plus dns", "rules:\n  - GEOSITE,cn,DIRECT\ndns:\n  fake-ip-filter:\n    - \"geoip:private\"\n", Needed{GeoIP: true, GeoSite: true}},
		{"dns uppercase ref", "dns:\n  nameserver-policy:\n    \"GEOSITE:CN\": [223.5.5.5]\n", Needed{GeoSite: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Analyze(parseCfg(t, tc.yaml))
			if got != tc.want {
				t.Fatalf("Analyze = %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestApplyPresentKeepsRules(t *testing.T) {
	cfg := parseCfg(t, "rules:\n  - GEOSITE,cn,DIRECT\n  - GEOIP,CN,DIRECT\n  - MATCH,PROXY\n")
	warnings := Apply(cfg, Present{GeoIP: true, GeoSite: true}, "")
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	m := toMap(t, cfg)
	if m["geodata-mode"] != true {
		t.Fatalf("geodata-mode = %v want true", m["geodata-mode"])
	}
	if m["geo-auto-update"] != false {
		t.Fatalf("geo-auto-update = %v want false", m["geo-auto-update"])
	}
	geox, ok := m["geox-url"].(map[string]any)
	if !ok {
		t.Fatalf("geox-url missing: %v", m["geox-url"])
	}
	for _, k := range []string{"geoip", "geosite", "mmdb", "asn"} {
		if _, ok := geox[k]; !ok {
			t.Fatalf("geox-url missing key %q: %v", k, geox)
		}
	}
	rules := rulesOf(t, cfg)
	if len(rules) != 3 || rules[0] != "GEOSITE,cn,DIRECT" || rules[1] != "GEOIP,CN,DIRECT" {
		t.Fatalf("rules should be retained, got %v", rules)
	}
}

func TestApplyDegradeBothMissing(t *testing.T) {
	cfg := parseCfg(t, "rules:\n  - GEOSITE,cn,DIRECT\n  - GEOIP,CN,DIRECT\n  - DOMAIN-SUFFIX,example.com,DIRECT\n")
	warnings := Apply(cfg, Present{}, "")
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "geosite.dat 缺失") || !strings.Contains(warnings[1], "geoip.dat 缺失") {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	rules := rulesOf(t, cfg)
	if len(rules) != 1 || rules[0] != "DOMAIN-SUFFIX,example.com,DIRECT" {
		t.Fatalf("geo rules should be stripped, got %v", rules)
	}
	// geox-url still set so mihomo never self-downloads.
	if _, ok := toMap(t, cfg)["geox-url"]; !ok {
		t.Fatal("geox-url should still be present after degrade")
	}
}

func TestApplyDegradeToMatchDirect(t *testing.T) {
	cfg := parseCfg(t, "rules:\n  - GEOSITE,cn,DIRECT\n  - GEOIP,CN,DIRECT\n")
	Apply(cfg, Present{}, "")
	rules := rulesOf(t, cfg)
	if len(rules) != 1 || rules[0] != "MATCH,DIRECT" {
		t.Fatalf("empty rules should fall back to MATCH,DIRECT, got %v", rules)
	}
}

func TestApplyDegradeOnlyGeosite(t *testing.T) {
	cfg := parseCfg(t, "rules:\n  - GEOSITE,cn,DIRECT\n  - GEOIP,CN,DIRECT\n")
	warnings := Apply(cfg, Present{GeoIP: true}, "")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "geosite.dat 缺失") {
		t.Fatalf("expected only geosite warning, got %v", warnings)
	}
	rules := rulesOf(t, cfg)
	if len(rules) != 1 || rules[0] != "GEOIP,CN,DIRECT" {
		t.Fatalf("GEOIP should be retained, got %v", rules)
	}
}

func TestApplyStripsDNSRefs(t *testing.T) {
	cfg := parseCfg(t, `dns:
  enable: true
  nameserver-policy:
    "geosite:cn":
      - 223.5.5.5
    "geoip:cn":
      - 119.29.29.29
    "domain:example.com":
      - 1.1.1.1
  fake-ip-filter:
    - "geosite:private"
    - "*.lan"
`)
	Apply(cfg, Present{}, "")
	m := toMap(t, cfg)
	dns := m["dns"].(map[string]any)
	nsp := dns["nameserver-policy"].(map[string]any)
	if _, ok := nsp["geosite:cn"]; ok {
		t.Fatalf("geosite:cn should be stripped: %v", nsp)
	}
	if _, ok := nsp["geoip:cn"]; ok {
		t.Fatalf("geoip:cn should be stripped: %v", nsp)
	}
	if _, ok := nsp["domain:example.com"]; !ok {
		t.Fatalf("non-geo policy should be retained: %v", nsp)
	}
	fip := dns["fake-ip-filter"].([]any)
	if len(fip) != 1 || fip[0] != "*.lan" {
		t.Fatalf("geosite fake-ip-filter entry should be stripped, got %v", fip)
	}
	if dns["enable"] != true {
		t.Fatalf("dns.enable should be untouched: %v", dns["enable"])
	}
}

func TestApplyNoGeoNoWarnings(t *testing.T) {
	cfg := parseCfg(t, "rules:\n  - DOMAIN-SUFFIX,example.com,DIRECT\n  - MATCH,DIRECT\n")
	warnings := Apply(cfg, Present{}, "")
	if len(warnings) != 0 {
		t.Fatalf("no geo refs => no warnings, got %v", warnings)
	}
	rules := rulesOf(t, cfg)
	if len(rules) != 2 {
		t.Fatalf("rules should be untouched, got %v", rules)
	}
}

func TestApplyCustomMirrorGeox(t *testing.T) {
	cfg := parseCfg(t, "rules:\n  - MATCH,DIRECT\n")
	Apply(cfg, Present{GeoIP: true, GeoSite: true}, "https://mirror.example.com/")
	geox := toMap(t, cfg)["geox-url"].(map[string]any)
	if !strings.HasPrefix(geox["geoip"].(string), "https://mirror.example.com/") {
		t.Fatalf("geox geoip should use custom mirror, got %v", geox["geoip"])
	}
}

func TestCheckFiles(t *testing.T) {
	dir := t.TempDir()
	if got := CheckFiles(dir); got != (Present{}) {
		t.Fatalf("empty dir => %+v", got)
	}
	// valid geoip.dat (>=1KB), tiny geosite.dat (<1KB)
	writeDat(t, filepath.Join(dir, "geoip.dat"), 2048)
	writeDat(t, filepath.Join(dir, "geosite.dat"), 16)
	got := CheckFiles(dir)
	if !got.GeoIP || got.GeoSite {
		t.Fatalf("geoip valid / geosite tiny => %+v", got)
	}
	// grow geosite.dat to valid
	writeDat(t, filepath.Join(dir, "geosite.dat"), 4096)
	if got := CheckFiles(dir); got != (Present{GeoIP: true, GeoSite: true}) {
		t.Fatalf("both valid => %+v", got)
	}
}

func TestCheckFilesRejectsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "geoip.dat"), 0o755); err != nil {
		t.Fatal(err)
	}
	if CheckFiles(dir).GeoIP {
		t.Fatal("a directory named geoip.dat must not count as present")
	}
}

// writeDat writes a deterministic binary blob (not HTML, not tiny) of size n.
func writeDat(t *testing.T, path string, n int) {
	t.Helper()
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStripRulesPreservesOrder(t *testing.T) {
	cfg := parseCfg(t, "rules:\n  - DOMAIN,a.com,DIRECT\n  - GEOSITE,cn,DIRECT\n  - DOMAIN,b.com,PROXY\n  - GEOIP,CN,DIRECT\n")
	n := stripRules(cfg, "GEOSITE")
	if n != 1 {
		t.Fatalf("removed = %d want 1", n)
	}
	rules := rulesOf(t, cfg)
	want := []string{"DOMAIN,a.com,DIRECT", "DOMAIN,b.com,PROXY", "GEOIP,CN,DIRECT"}
	if strings.Join(rules, "|") != strings.Join(want, "|") {
		t.Fatalf("order not preserved: %v", rules)
	}
}

func TestGeoxURLKeys(t *testing.T) {
	cfg := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	Apply(cfg, Present{GeoIP: true, GeoSite: true}, "")
	geox := mihomo.MapGet(cfg, "geox-url")
	if geox == nil || geox.Kind != yaml.MappingNode {
		t.Fatalf("geox-url should be a mapping, got %v", geox)
	}
	if mihomo.MapGet(geox, "mmdb") == nil || mihomo.MapGet(geox, "asn") == nil {
		t.Fatal("geox-url should include mmdb and asn")
	}
}

func TestApplyNeutralizesFallbackFilter(t *testing.T) {
	// No explicit fallback-filter: mihomo defaults geoip:true, which would load
	// GeoIP.dat at init. Degrade must force geoip:false even with no geo rules.
	cfg := parseCfg(t, "dns:\n  enable: true\nrules:\n  - MATCH,DIRECT\n")
	warnings := Apply(cfg, Present{}, "")
	if len(warnings) != 0 {
		t.Fatalf("no geo refs => no warnings, got %v", warnings)
	}
	dns := toMap(t, cfg)["dns"].(map[string]any)
	ff, ok := dns["fallback-filter"].(map[string]any)
	if !ok {
		t.Fatalf("fallback-filter should be created: %v", dns)
	}
	if ff["geoip"] != false {
		t.Fatalf("fallback-filter.geoip must be false when geoip.dat missing: %v", ff)
	}
	if gs, ok := ff["geosite"].([]any); !ok || len(gs) != 0 {
		t.Fatalf("fallback-filter.geosite must be empty when geosite.dat missing: %v", ff["geosite"])
	}
}

func TestApplyFallbackFilterPreservedWhenPresent(t *testing.T) {
	cfg := parseCfg(t, "dns:\n  enable: true\n  fallback-filter:\n    geoip: true\n    geosite:\n      - gfw\nrules:\n  - MATCH,DIRECT\n")
	Apply(cfg, Present{GeoIP: true, GeoSite: true}, "")
	ff := toMap(t, cfg)["dns"].(map[string]any)["fallback-filter"].(map[string]any)
	if ff["geoip"] != true {
		t.Fatalf("fallback-filter.geoip should stay true when present: %v", ff)
	}
	gs := ff["geosite"].([]any)
	if len(gs) != 1 || gs[0] != "gfw" {
		t.Fatalf("fallback-filter.geosite should be retained when present: %v", gs)
	}
}

func TestApplyFallbackFilterExplicitGeositeStripped(t *testing.T) {
	cfg := parseCfg(t, "dns:\n  enable: true\n  fallback-filter:\n    geoip: true\n    geoip-code: CN\n    geosite:\n      - gfw\nrules:\n  - MATCH,DIRECT\n")
	Apply(cfg, Present{}, "")
	ff := toMap(t, cfg)["dns"].(map[string]any)["fallback-filter"].(map[string]any)
	if ff["geoip"] != false {
		t.Fatalf("geoip should be disabled: %v", ff)
	}
	if gs, ok := ff["geosite"].([]any); !ok || len(gs) != 0 {
		t.Fatalf("explicit geosite list should be cleared: %v", ff["geosite"])
	}
}
