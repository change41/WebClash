package service

import (
	"encoding/json"
	"os"
	"testing"
)

var goldenCases = map[string]ServiceOptions{
	"systemd-root":       {Name: "webclash", Bin: "/usr/local/bin/webclash", DataDir: "/var/lib/webclash", Port: 7788, Host: "0.0.0.0", User: "root", Tun: false, Manager: "systemd"},
	"systemd-user-tun":   {Name: "wc2", Bin: "/opt/wc2/bin/wc2", DataDir: "/srv/wc2", Port: 9090, Host: "127.0.0.1", User: "nobody", Tun: true, Manager: "systemd"},
	"systemd-user-notun": {Name: "wc3", Bin: "/usr/local/bin/wc3", DataDir: "/var/lib/wc3", Port: 7788, Host: "0.0.0.0", User: "proxy", Tun: false, Manager: "systemd"},
	"openrc-root":        {Name: "webclash", Bin: "/usr/local/bin/webclash", DataDir: "/var/lib/webclash", Port: 7788, Host: "0.0.0.0", User: "root", Tun: false, Manager: "openrc"},
	"openrc-user-tun":    {Name: "wc2", Bin: "/opt/wc2/bin/wc2", DataDir: "/srv/wc2", Port: 9090, Host: "127.0.0.1", User: "nobody", Tun: true, Manager: "openrc"},
}

var parseInputs = map[string]string{
	"empty":       "",
	"basic":       "A=1\nB=2\n",
	"comments":    "# comment\n\nA=1\n  # indented comment\nB = 2 \n",
	"quotes":      "A=\"hello world\"\nB='single'\nC=\"mixed'\nD=unquoted\n",
	"dup":         "A=1\nB=2\nA=3\n",
	"noeq":        "NOEQUALS\n=leadingeq\nA=1\n",
	"emptyVal":    "A=\nB=\"\"\nC=''\n",
	"singleQuote": "A=\"\nB='\n",
	"spaces":      "  KEY  =  value  \n",
}

type templateGolden struct {
	Units map[string]string     `json:"units"`
	Init  map[string]string     `json:"init"`
	Env   map[string]string     `json:"env"`
	Conf  map[string]string     `json:"conf"`
	Parse map[string][][]string `json:"parse"`
}

func loadGolden(t *testing.T) templateGolden {
	t.Helper()
	raw, err := os.ReadFile("testdata/templates.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var g templateGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	return g
}

func TestRenderSystemdUnitGolden(t *testing.T) {
	g := loadGolden(t)
	for k, want := range g.Units {
		o, ok := goldenCases[k]
		if !ok {
			t.Fatalf("golden unit case %q has no Go fixture", k)
		}
		if got := RenderSystemdUnit(o); got != want {
			t.Errorf("unit %q byte mismatch\n--- got ---\n%s\n--- want ---\n%s", k, got, want)
		}
	}
}

func TestRenderOpenRcInitGolden(t *testing.T) {
	g := loadGolden(t)
	for k, want := range g.Init {
		o, ok := goldenCases[k]
		if !ok {
			t.Fatalf("golden init case %q has no Go fixture", k)
		}
		if got := RenderOpenRcInit(o); got != want {
			t.Errorf("init %q byte mismatch\n--- got ---\n%s\n--- want ---\n%s", k, got, want)
		}
	}
}

func TestRenderEnvFileGolden(t *testing.T) {
	g := loadGolden(t)
	base := goldenCases["systemd-root"]
	existingEnv := []EnvKV{{"CUSTOM_FLAG", "x"}, {"DATA_DIR", "/old/path"}, {"WEBUI_PORT", "1234"}, {"EXTRA", "y"}}
	existingOrder := []EnvKV{{"WEBUI_HOST", "h"}, {"NODE_ENV", "dev"}, {"DATA_DIR", "d"}}
	cases := map[string]string{
		"fresh":         RenderEnvFile(base, nil),
		"merged":        RenderEnvFile(base, existingEnv),
		"existingOrder": RenderEnvFile(goldenCases["systemd-user-tun"], existingOrder),
	}
	for k, got := range cases {
		if want := g.Env[k]; got != want {
			t.Errorf("env %q byte mismatch\n--- got ---\n%s\n--- want ---\n%s", k, got, want)
		}
	}
}

func TestRenderOpenRcConfGolden(t *testing.T) {
	g := loadGolden(t)
	obase := goldenCases["openrc-root"]
	existingConf := []EnvKV{{"WEBCLASH_USER", "old"}, {"CUSTOM", "z"}, {"WEBCLASH_PORT", "1"}}
	cases := map[string]string{
		"fresh":  RenderOpenRcConf(obase, nil),
		"merged": RenderOpenRcConf(obase, existingConf),
	}
	for k, got := range cases {
		if want := g.Conf[k]; got != want {
			t.Errorf("conf %q byte mismatch\n--- got ---\n%s\n--- want ---\n%s", k, got, want)
		}
	}
}

func TestParseEnvFileGolden(t *testing.T) {
	g := loadGolden(t)
	for k, want := range g.Parse {
		input, ok := parseInputs[k]
		if !ok {
			t.Fatalf("golden parse case %q has no Go input", k)
		}
		got := ParseEnvFile(input)
		if len(got) != len(want) {
			t.Errorf("parse %q length: got %d want %d (%v)", k, len(got), len(want), got)
			continue
		}
		for i := range got {
			if got[i].Key != want[i][0] || got[i].Value != want[i][1] {
				t.Errorf("parse %q entry %d: got [%s=%s] want [%s=%s]", k, i, got[i].Key, got[i].Value, want[i][0], want[i][1])
			}
		}
	}
}

// TestEnvMergePreservesOrder asserts the JS spread semantics directly: existing
// keys keep their position, overrides update in place, new keys append.
func TestEnvMergePreservesOrder(t *testing.T) {
	o := ServiceOptions{Name: "webclash", DataDir: "/d", Host: "h", Port: 1, Manager: "systemd"}
	existing := []EnvKV{{"Z", "1"}, {"DATA_DIR", "old"}, {"A", "2"}}
	got := RenderEnvFile(o, existing)
	want := "# WebClash 服务环境变量（由 webclash install 生成，可手动编辑）\n" +
		"# 优先级高于 settings.json；修改后执行 systemctl restart webclash\n" +
		"Z=1\nDATA_DIR=/d\nA=2\nWEBUI_HOST=h\nWEBUI_PORT=1\nNODE_ENV=production\n" +
		"# 首次启动管理密码（仅首启生效，留空则自动生成）\n# WEBUI_PASSWORD=\n" +
		"# 内核下载镜像前缀（可选）\n# WEBCLASH_KERNEL_MIRROR=\n"
	if got != want {
		t.Errorf("merge order mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestCapabilities(t *testing.T) {
	full := "CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE"
	bind := "CAP_NET_BIND_SERVICE"
	if got := capabilities(ServiceOptions{User: "root"}); got != full {
		t.Errorf("root caps = %q", got)
	}
	if got := capabilities(ServiceOptions{User: "nobody", Tun: true}); got != full {
		t.Errorf("tun caps = %q", got)
	}
	if got := capabilities(ServiceOptions{User: "nobody"}); got != bind {
		t.Errorf("non-root caps = %q", got)
	}
}
