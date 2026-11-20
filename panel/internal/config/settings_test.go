package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"webclash/internal/util"
)

func strPtr(s string) *string { return &s }
func f64(v float64) *float64  { return &v }

// --- Node JSON.stringify goldens (2-space, no HTML escape, no trailing newline) ---

const settingsGolden = `{
  "server": {
    "port": 7788,
    "host": "0.0.0.0"
  },
  "auth": {
    "passwordHash": null,
    "secret": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "general": {
    "mode": "rule",
    "logLevel": "info",
    "allowLan": false,
    "bindAddress": "a<b>&c",
    "ipv6": false
  },
  "ports": {
    "mixed": 7890,
    "http": 0,
    "socks": 0
  },
  "authentication": [],
  "controller": {
    "port": 9090,
    "secret": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  },
  "tun": {
    "enable": false,
    "stack": "mixed",
    "autoRoute": true,
    "autoDetectInterface": true,
    "dnsHijack": [
      "any:53"
    ]
  },
  "dns": {
    "enable": true,
    "listen": "0.0.0.0:1053",
    "enhancedMode": "fake-ip",
    "fakeIpRange": "198.18.0.1/16",
    "nameservers": [
      "https://223.5.5.5/dns-query",
      "https://doh.pub/dns-query"
    ],
    "fallback": [
      "https://1.1.1.1/dns-query",
      "https://8.8.8.8/dns-query"
    ],
    "defaultNameservers": [
      "223.5.5.5",
      "119.29.29.29"
    ]
  },
  "kernel": {
    "autoStart": true,
    "autoDownload": true,
    "binaryPath": "",
    "version": "latest",
    "mirror": ""
  },
  "geoip": {
    "enable": false,
    "source": "ip2region"
  },
  "connHistory": {
    "limit": 3000
  },
  "customRules": []
}`

const publicGolden = `{
  "server": {
    "port": 7788,
    "host": "0.0.0.0"
  },
  "auth": {
    "hasPassword": false
  },
  "general": {
    "mode": "rule",
    "logLevel": "info",
    "allowLan": false,
    "bindAddress": "a<b>&c",
    "ipv6": false
  },
  "ports": {
    "mixed": 7890,
    "http": 0,
    "socks": 0
  },
  "authentication": [],
  "controller": {
    "port": 9090,
    "secret": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  },
  "tun": {
    "enable": false,
    "stack": "mixed",
    "autoRoute": true,
    "autoDetectInterface": true,
    "dnsHijack": [
      "any:53"
    ]
  },
  "dns": {
    "enable": true,
    "listen": "0.0.0.0:1053",
    "enhancedMode": "fake-ip",
    "fakeIpRange": "198.18.0.1/16",
    "nameservers": [
      "https://223.5.5.5/dns-query",
      "https://doh.pub/dns-query"
    ],
    "fallback": [
      "https://1.1.1.1/dns-query",
      "https://8.8.8.8/dns-query"
    ],
    "defaultNameservers": [
      "223.5.5.5",
      "119.29.29.29"
    ]
  },
  "kernel": {
    "autoStart": true,
    "autoDownload": true,
    "binaryPath": "",
    "version": "latest",
    "mirror": ""
  },
  "geoip": {
    "enable": false,
    "source": "ip2region"
  },
  "connHistory": {
    "limit": 3000
  },
  "customRules": []
}`

const metaGolden = `{
  "id": "abc123",
  "name": "测试订阅",
  "type": "remote",
  "url": "https://example.com/sub?token=x<y>",
  "createdAt": 1700000000000,
  "updatedAt": 1700000001000,
  "userInfo": {
    "upload": 100,
    "download": 200,
    "total": 300,
    "expire": 1800000000
  },
  "settings": {
    "intervalHours": 6,
    "testUrl": "https://cp.cloudflare.com/generate_204",
    "testTimeout": 5000,
    "customRules": []
  }
}`

const localGolden = `{
  "id": "def456",
  "name": "本地",
  "type": "local",
  "createdAt": 1700000000000,
  "updatedAt": 1700000000000,
  "settings": {
    "intervalHours": 0,
    "testUrl": "https://cp.cloudflare.com/generate_204",
    "testTimeout": 5000,
    "customRules": []
  }
}`

func fixedSettings() Settings {
	s := DefaultSettings()
	s.Auth.Secret = strings.Repeat("a", 64)
	s.Auth.PasswordHash = nil
	s.Controller.Secret = strings.Repeat("b", 48)
	s.General.BindAddress = "a<b>&c"
	return s
}

func TestSettingsSerializationGolden(t *testing.T) {
	b, err := util.MarshalJSONIndent(fixedSettings())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != settingsGolden {
		t.Errorf("settings JSON mismatch:\n got: %s\nwant: %s", b, settingsGolden)
	}
}

func TestPublicSerializationGolden(t *testing.T) {
	b, err := util.MarshalJSONIndent(ToPublic(fixedSettings()))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != publicGolden {
		t.Errorf("public JSON mismatch:\n got: %s\nwant: %s", b, publicGolden)
	}
}

func TestProfileMetaSerializationGolden(t *testing.T) {
	remote := ProfileMeta{
		ID: "abc123", Name: "测试订阅", Type: "remote",
		URL:       strPtr("https://example.com/sub?token=x<y>"),
		CreatedAt: 1700000000000, UpdatedAt: 1700000001000,
		UserInfo: &util.SubscriptionUserInfo{Upload: f64(100), Download: f64(200), Total: f64(300), Expire: f64(1800000000)},
		Settings: ProfileSettings{IntervalHours: 6, TestUrl: "https://cp.cloudflare.com/generate_204", TestTimeout: 5000, CustomRules: []string{}},
	}
	b, err := util.MarshalJSONIndent(remote)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != metaGolden {
		t.Errorf("remote meta JSON mismatch:\n got: %s\nwant: %s", b, metaGolden)
	}

	local := ProfileMeta{
		ID: "def456", Name: "本地", Type: "local",
		CreatedAt: 1700000000000, UpdatedAt: 1700000000000,
		Settings: DefaultProfileSettings(),
	}
	b, err = util.MarshalJSONIndent(local)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != localGolden {
		t.Errorf("local meta JSON mismatch:\n got: %s\nwant: %s", b, localGolden)
	}
}

func TestNormalizeSettingsRoundTripStable(t *testing.T) {
	var input any
	if err := json.Unmarshal([]byte(settingsGolden), &input); err != nil {
		t.Fatal(err)
	}
	n := NormalizeSettings(input)
	b, err := util.MarshalJSONIndent(n)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != settingsGolden {
		t.Errorf("normalize round-trip not stable:\n got: %s\nwant: %s", b, settingsGolden)
	}
}

func TestNormalizeSettingsDefaults(t *testing.T) {
	n := NormalizeSettings(nil)
	if n.Server.Port != 7788 || n.Server.Host != "0.0.0.0" {
		t.Errorf("server defaults: %+v", n.Server)
	}
	if n.Auth.PasswordHash != nil {
		t.Error("passwordHash should default nil")
	}
	if len(n.Auth.Secret) != 64 {
		t.Errorf("auth.secret should be 64 hex, got %d", len(n.Auth.Secret))
	}
	if len(n.Controller.Secret) != 48 {
		t.Errorf("controller.secret should be 48 hex, got %d", len(n.Controller.Secret))
	}
	if n.General.Mode != "rule" || n.General.LogLevel != "info" || n.General.BindAddress != "*" {
		t.Errorf("general defaults: %+v", n.General)
	}
	if n.Ports.Mixed != 7890 || n.Ports.HTTP != 0 || n.Ports.SOCKS != 0 {
		t.Errorf("ports defaults: %+v", n.Ports)
	}
	if n.Authentication == nil || len(n.Authentication) != 0 {
		t.Errorf("authentication default = %#v, want empty non-nil", n.Authentication)
	}
	if n.Kernel.Version != "latest" || n.Kernel.BinaryPath != "" || !n.Kernel.AutoStart {
		t.Errorf("kernel defaults: %+v", n.Kernel)
	}
	if n.GeoIP.Enable || n.GeoIP.Source != "ip2region" {
		t.Errorf("geoip defaults: %+v", n.GeoIP)
	}
	if len(n.Dns.Nameservers) != 2 || len(n.Tun.DnsHijack) != 1 || n.Tun.DnsHijack[0] != "any:53" {
		t.Errorf("dns/tun defaults: %+v %+v", n.Dns, n.Tun)
	}
	if n.CustomRules == nil || len(n.CustomRules) != 0 {
		t.Errorf("customRules default = %#v, want empty non-nil", n.CustomRules)
	}
}

func TestNormalizeSettingsBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		check func(*testing.T, Settings)
	}{
		{"port from string", map[string]any{"server": map[string]any{"port": "8080"}},
			func(t *testing.T, s Settings) { eqInt(t, "server.port", s.Server.Port, 8080) }},
		{"port zero falls back", map[string]any{"server": map[string]any{"port": 0}},
			func(t *testing.T, s Settings) { eqInt(t, "server.port", s.Server.Port, 7788) }},
		{"port too big", map[string]any{"server": map[string]any{"port": 70000}},
			func(t *testing.T, s Settings) { eqInt(t, "server.port", s.Server.Port, 7788) }},
		{"port fractional", map[string]any{"server": map[string]any{"port": 12.5}},
			func(t *testing.T, s Settings) { eqInt(t, "server.port", s.Server.Port, 7788) }},
		{"standalone ports from strings", map[string]any{"ports": map[string]any{"http": "7891", "socks": 7892}},
			func(t *testing.T, s Settings) {
				if s.Ports.HTTP != 7891 || s.Ports.SOCKS != 7892 {
					t.Errorf("ports = %+v, want http 7891 socks 7892", s.Ports)
				}
			}},
		{"standalone ports zero disables", map[string]any{"ports": map[string]any{"http": 0, "socks": 0}},
			func(t *testing.T, s Settings) {
				if s.Ports.HTTP != 0 || s.Ports.SOCKS != 0 {
					t.Errorf("ports = %+v, want both disabled", s.Ports)
				}
			}},
		{"standalone ports out of range", map[string]any{"ports": map[string]any{"http": 70000, "socks": -1}},
			func(t *testing.T, s Settings) {
				if s.Ports.HTTP != 0 || s.Ports.SOCKS != 0 {
					t.Errorf("ports = %+v, want fallback disabled", s.Ports)
				}
			}},
		{"authentication users", map[string]any{"authentication": []any{
			map[string]any{"username": " alice ", "password": " a b "},
			map[string]any{"username": " ", "password": "x"},
		}}, func(t *testing.T, s Settings) {
			if len(s.Authentication) != 1 || s.Authentication[0].Username != "alice" || s.Authentication[0].Password != " a b " {
				t.Errorf("authentication = %+v", s.Authentication)
			}
		}},
		{"authentication non-array", map[string]any{"authentication": "x"},
			func(t *testing.T, s Settings) {
				if s.Authentication == nil || len(s.Authentication) != 0 {
					t.Errorf("authentication = %#v, want empty non-nil", s.Authentication)
				}
			}},
		{"mode pick", map[string]any{"general": map[string]any{"mode": "global"}},
			func(t *testing.T, s Settings) { eqStr(t, "general.mode", s.General.Mode, "global") }},
		{"mode invalid", map[string]any{"general": map[string]any{"mode": "bogus"}},
			func(t *testing.T, s Settings) { eqStr(t, "general.mode", s.General.Mode, "rule") }},
		{"mode non-string", map[string]any{"general": map[string]any{"mode": 5}},
			func(t *testing.T, s Settings) { eqStr(t, "general.mode", s.General.Mode, "rule") }},
		{"bindAddress empty", map[string]any{"general": map[string]any{"bindAddress": "  "}},
			func(t *testing.T, s Settings) { eqStr(t, "general.bindAddress", s.General.BindAddress, "*") }},
		{"passwordHash untrimmed", map[string]any{"auth": map[string]any{"passwordHash": "  scrypt$x$y  "}},
			func(t *testing.T, s Settings) {
				if s.Auth.PasswordHash == nil || *s.Auth.PasswordHash != "  scrypt$x$y  " {
					t.Errorf("passwordHash not preserved untrimmed: %v", s.Auth.PasswordHash)
				}
			}},
		{"passwordHash non-string", map[string]any{"auth": map[string]any{"passwordHash": 5}},
			func(t *testing.T, s Settings) {
				if s.Auth.PasswordHash != nil {
					t.Error("non-string passwordHash should be nil")
				}
			}},
		{"secret empty regenerates", map[string]any{"auth": map[string]any{"secret": "  "}},
			func(t *testing.T, s Settings) { eqInt(t, "auth.secret len", len(s.Auth.Secret), 64) }},
		{"secret kept", map[string]any{"auth": map[string]any{"secret": "abc"}},
			func(t *testing.T, s Settings) { eqStr(t, "auth.secret", s.Auth.Secret, "abc") }},
		{"strList filters", map[string]any{"dns": map[string]any{"nameservers": []any{"a", " ", "b", 5}}},
			func(t *testing.T, s Settings) { eqStrs(t, "dns.nameservers", s.Dns.Nameservers, []string{"a", "b"}) }},
		{"strList empty array stays empty", map[string]any{"dns": map[string]any{"nameservers": []any{}}},
			func(t *testing.T, s Settings) { eqInt(t, "dns.nameservers len", len(s.Dns.Nameservers), 0) }},
		{"strList non-array falls back", map[string]any{"dns": map[string]any{"nameservers": "x"}},
			func(t *testing.T, s Settings) { eqInt(t, "dns.nameservers len", len(s.Dns.Nameservers), 2) }},
		{"binaryPath trimmed", map[string]any{"kernel": map[string]any{"binaryPath": "  /x  "}},
			func(t *testing.T, s Settings) { eqStr(t, "kernel.binaryPath", s.Kernel.BinaryPath, "/x") }},
		{"version empty latest", map[string]any{"kernel": map[string]any{"version": ""}},
			func(t *testing.T, s Settings) { eqStr(t, "kernel.version", s.Kernel.Version, "latest") }},
		{"geoip source pick", map[string]any{"geoip": map[string]any{"source": "mmdb"}},
			func(t *testing.T, s Settings) { eqStr(t, "geoip.source", s.GeoIP.Source, "mmdb") }},
		{"geoip source invalid", map[string]any{"geoip": map[string]any{"source": "bogus"}},
			func(t *testing.T, s Settings) { eqStr(t, "geoip.source", s.GeoIP.Source, "ip2region") }},
		{"geoip enable true", map[string]any{"geoip": map[string]any{"enable": true}},
			func(t *testing.T, s Settings) {
				if !s.GeoIP.Enable {
					t.Error("geoip.enable should be true")
				}
			}},
		{"geoip absent defaults off", map[string]any{},
			func(t *testing.T, s Settings) {
				if s.GeoIP.Enable || s.GeoIP.Source != "ip2region" {
					t.Errorf("geoip defaults: %+v", s.GeoIP)
				}
			}},
		{"customRules filters blanks", map[string]any{"customRules": []any{" DOMAIN,a,DIRECT ", " ", 5}},
			func(t *testing.T, s Settings) { eqStrs(t, "customRules", s.CustomRules, []string{"DOMAIN,a,DIRECT"}) }},
		{"customRules empty array stays empty", map[string]any{"customRules": []any{}},
			func(t *testing.T, s Settings) {
				if s.CustomRules == nil || len(s.CustomRules) != 0 {
					t.Errorf("customRules = %#v, want empty non-nil", s.CustomRules)
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.check(t, NormalizeSettings(c.input)) })
	}
}

func TestNormalizeProfileSettings(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  ProfileSettings
	}{
		{"defaults", nil, DefaultProfileSettings()},
		{"interval from string", map[string]any{"intervalHours": "6"},
			ProfileSettings{6, "https://cp.cloudflare.com/generate_204", 5000, []string{}}},
		{"interval clamp high", map[string]any{"intervalHours": 1000},
			ProfileSettings{720, "https://cp.cloudflare.com/generate_204", 5000, []string{}}},
		{"interval clamp low", map[string]any{"intervalHours": -5},
			ProfileSettings{0, "https://cp.cloudflare.com/generate_204", 5000, []string{}}},
		{"timeout clamp low", map[string]any{"testTimeout": 500},
			ProfileSettings{0, "https://cp.cloudflare.com/generate_204", 1000, []string{}}},
		{"timeout clamp high", map[string]any{"testTimeout": 999999},
			ProfileSettings{0, "https://cp.cloudflare.com/generate_204", 60000, []string{}}},
		{"timeout non-number", map[string]any{"testTimeout": "abc"},
			ProfileSettings{0, "https://cp.cloudflare.com/generate_204", 5000, []string{}}},
		{"testUrl empty falls back", map[string]any{"testUrl": "  "},
			ProfileSettings{0, "https://cp.cloudflare.com/generate_204", 5000, []string{}}},
		{"testUrl kept", map[string]any{"testUrl": "https://x.test"},
			ProfileSettings{0, "https://x.test", 5000, []string{}}},
		{"customRules trimmed", map[string]any{"customRules": []any{" DOMAIN,a,DIRECT ", "", 5}},
			ProfileSettings{0, "https://cp.cloudflare.com/generate_204", 5000, []string{"DOMAIN,a,DIRECT"}}},
		{"customRules empty", map[string]any{"customRules": []any{}},
			ProfileSettings{0, "https://cp.cloudflare.com/generate_204", 5000, []string{}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizeProfileSettings(c.input)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestPublicSettingsProjection(t *testing.T) {
	s := fixedSettings()
	s.Auth.PasswordHash = strPtr("scrypt$x$y")
	pub := ToPublic(s)
	if !pub.Auth.HasPassword {
		t.Error("hasPassword should be true when passwordHash set")
	}
	if pub.Controller.Secret != s.Controller.Secret {
		t.Error("controller.secret must remain exposed")
	}
	b, _ := util.MarshalJSON(pub)
	if strings.Contains(string(b), "passwordHash") || strings.Contains(string(b), "\"secret\":\""+s.Auth.Secret) {
		t.Errorf("public leaked auth secrets: %s", b)
	}
	// Empty-string hash counts as no password (Boolean("")).
	s.Auth.PasswordHash = strPtr("")
	if ToPublic(s).Auth.HasPassword {
		t.Error("empty passwordHash should yield hasPassword=false")
	}
}

func eqInt(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", name, got, want)
	}
}
func eqStr(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}
func eqStrs(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", name, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s = %v, want %v", name, got, want)
			return
		}
	}
}
