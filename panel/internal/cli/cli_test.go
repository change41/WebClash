package cli

import (
	"testing"

	"webclash/internal/service"
)

func TestManagerFlag(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		errCode int
	}{
		{"", "", 0},
		{"auto", "", 0},
		{"systemd", "systemd", 0},
		{"openrc", "openrc", 0},
		{"bogus", "", 2},
	}
	for _, c := range cases {
		vals := map[string]any{}
		if c.in != "" {
			vals["service-manager"] = c.in
		}
		got, err := managerFlag(vals)
		if c.errCode == 0 {
			if err != nil || got != c.want {
				t.Errorf("managerFlag(%q) = %q, %v; want %q", c.in, got, err, c.want)
			}
			continue
		}
		ce, ok := err.(*CliError)
		if !ok || ce.ExitCode != c.errCode {
			t.Errorf("managerFlag(%q) err = %v; want CliError %d", c.in, err, c.errCode)
			continue
		}
		want := "--service-manager 只接受 auto/systemd/openrc，收到: " + c.in
		if ce.Message != want {
			t.Errorf("managerFlag(%q) message = %q; want %q", c.in, ce.Message, want)
		}
	}
}

func TestParsePort(t *testing.T) {
	cases := []struct {
		raw     string
		want    int
		errCode int
	}{
		{"", 7788, 0},
		{"80", 80, 0},
		{"65535", 65535, 0},
		{" 80 ", 80, 0},
		{"0", 0, 2},
		{"65536", 0, 2},
		{"abc", 0, 2},
		{"-1", 0, 2},
		{"80.5", 0, 2},
	}
	for _, c := range cases {
		got, err := parsePort(c.raw, 7788)
		if c.errCode == 0 {
			if err != nil || got != c.want {
				t.Errorf("parsePort(%q) = %d, %v; want %d", c.raw, got, err, c.want)
			}
			continue
		}
		ce, ok := err.(*CliError)
		if !ok || ce.ExitCode != c.errCode {
			t.Errorf("parsePort(%q) err = %v; want CliError %d", c.raw, err, c.errCode)
		}
	}
}

func TestRequireManager(t *testing.T) {
	err := requireManager(service.ServiceOptions{Manager: "none"})
	ce, ok := err.(*CliError)
	if !ok || ce.ExitCode != 4 {
		t.Fatalf("requireManager(none) = %v; want CliError 4", err)
	}
	if ce.Message != "未检测到 systemd 或 OpenRC；可用 --service-manager 强制指定，或直接 webclash run 前台运行" {
		t.Errorf("requireManager message = %q", ce.Message)
	}
	if err := requireManager(service.ServiceOptions{Manager: "systemd"}); err != nil {
		t.Errorf("requireManager(systemd) = %v", err)
	}
}

func TestRequireRootDryRun(t *testing.T) {
	if err := requireRoot(true); err != nil {
		t.Errorf("requireRoot(dryRun) = %v; want nil", err)
	}
}

func TestParseArgsMessages(t *testing.T) {
	_, err := ParseArgs([]string{"--nope"}, allSpecs())
	ce, ok := err.(*CliError)
	if !ok || ce.ExitCode != 2 || ce.Message != "未知参数: --nope（使用 --help 查看用法）" {
		t.Errorf("unknown flag err = %v", err)
	}
	_, err = ParseArgs([]string{"--port"}, allSpecs())
	ce, ok = err.(*CliError)
	if !ok || ce.ExitCode != 2 || ce.Message != "参数 --port 需要一个值" {
		t.Errorf("missing value err = %v", err)
	}
	_, err = ParseArgs([]string{"--port="}, allSpecs())
	ce, ok = err.(*CliError)
	if !ok || ce.ExitCode != 2 || ce.Message != "参数 --port 的值不能为空" {
		t.Errorf("empty value err = %v", err)
	}
	_, err = ParseArgs([]string{"--json=maybe"}, allSpecs())
	ce, ok = err.(*CliError)
	if !ok || ce.ExitCode != 2 || ce.Message != "参数 --json 只接受 true/false" {
		t.Errorf("bool value err = %v", err)
	}
}

func TestRunExitCodes(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want int
	}{
		{"version flag", []string{"--version"}, 0},
		{"help", []string{"help"}, 0},
		{"install help", []string{"install", "--help"}, 0},
		{"unknown command", []string{"bogus"}, 2},
		{"unknown flag", []string{"install", "--nope"}, 2},
		{"flag not allowed", []string{"version", "--port", "80"}, 2},
		{"bad manager", []string{"install", "--service-manager", "bogus"}, 2},
		{"bad port", []string{"status", "--port", "abc"}, 2},
		{"purge without yes", []string{"uninstall", "--purge", "--service-manager", "systemd"}, 2},
		{"install dry-run", []string{"install", "--service-manager", "systemd", "--dry-run"}, 0},
		{"uninstall dry-run", []string{"uninstall", "--service-manager", "systemd", "--dry-run"}, 0},
		{"start dry-run", []string{"start", "--service-manager", "systemd", "--dry-run"}, 0},
		{"status not installed", []string{"status", "--service-manager", "systemd"}, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Run(c.argv); got != c.want {
				t.Errorf("Run(%v) = %d; want %d", c.argv, got, c.want)
			}
		})
	}
}
