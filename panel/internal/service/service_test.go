package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveServiceDefaults(t *testing.T) {
	o, err := ResolveService(ServiceInput{Manager: "systemd"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Name != "webclash" {
		t.Errorf("name = %q", o.Name)
	}
	if o.Bin != "/usr/local/bin/webclash" {
		t.Errorf("bin = %q", o.Bin)
	}
	if o.DataDir != "/var/lib/webclash" {
		t.Errorf("dataDir = %q", o.DataDir)
	}
	if o.Port != 7788 {
		t.Errorf("port = %d", o.Port)
	}
	if o.Host != "0.0.0.0" {
		t.Errorf("host = %q", o.Host)
	}
	if o.User != "root" {
		t.Errorf("user = %q", o.User)
	}
	if o.Tun {
		t.Error("tun should default false")
	}
	if o.Manager != "systemd" {
		t.Errorf("manager = %q", o.Manager)
	}
}

func TestResolveServiceOverrides(t *testing.T) {
	o, err := ResolveService(ServiceInput{
		Name: "wc2", Bin: "/opt/wc2", DataDir: "/srv/wc2", Port: 9090,
		Host: "127.0.0.1", User: "nobody", Tun: true, Manager: "openrc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.Name != "wc2" || o.Bin != "/opt/wc2" || o.DataDir != "/srv/wc2" ||
		o.Port != 9090 || o.Host != "127.0.0.1" || o.User != "nobody" || !o.Tun || o.Manager != "openrc" {
		t.Errorf("overrides not applied: %+v", o)
	}
}

func TestResolveServiceInvalidName(t *testing.T) {
	for _, name := range []string{"bad name", "a/b", "a.b", "x;rm", ""} {
		// "" falls back to the default and is valid; only the others must fail.
		_, err := ResolveService(ServiceInput{Name: name, Manager: "systemd"})
		if name == "" {
			if err != nil {
				t.Errorf("empty name should default, got %v", err)
			}
			continue
		}
		if err == nil || err.Error() != "非法服务名: "+name {
			t.Errorf("name %q: got err %v", name, err)
		}
	}
}

func TestResolveServiceExpandHome(t *testing.T) {
	o, err := ResolveService(ServiceInput{Bin: "~/bin/wc", DataDir: "~/data", Manager: "systemd"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(o.Bin, "~") || !filepath.IsAbs(o.Bin) {
		t.Errorf("bin not expanded/abs: %q", o.Bin)
	}
	if strings.Contains(o.DataDir, "~") || !filepath.IsAbs(o.DataDir) {
		t.Errorf("dataDir not expanded/abs: %q", o.DataDir)
	}
}

func TestInstallDryRunZeroDisk(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		t.Run(manager, func(t *testing.T) {
			tmp := t.TempDir()
			dataDir := filepath.Join(tmp, "data")
			bin := filepath.Join(tmp, "bin", "webclash")
			o := ServiceOptions{Name: "webclash", Bin: bin, DataDir: dataDir, Port: 7788, Host: "0.0.0.0", User: "root", Manager: manager}
			steps, err := InstallService(o, InstallFlags{DryRun: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(steps) == 0 {
				t.Fatal("no steps")
			}
			for _, s := range steps {
				if !strings.HasPrefix(s.Action, "would-") {
					t.Errorf("dry-run produced real action %q (%s)", s.Action, s.Step)
				}
			}
			if exists(dataDir) {
				t.Error("dry-run created dataDir")
			}
			if exists(bin) {
				t.Error("dry-run created bin")
			}
		})
	}
}

func TestUninstallDryRunZeroDisk(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		t.Run(manager, func(t *testing.T) {
			tmp := t.TempDir()
			dataDir := filepath.Join(tmp, "data")
			if err := os.MkdirAll(dataDir, 0o755); err != nil {
				t.Fatal(err)
			}
			o := ServiceOptions{Name: "webclash", Bin: filepath.Join(tmp, "bin", "webclash"), DataDir: dataDir, Port: 7788, Host: "0.0.0.0", User: "root", Manager: manager}
			steps, err := UninstallService(o, UninstallFlags{DryRun: true, Purge: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range steps {
				if s.Action != "skip" && !strings.HasPrefix(s.Action, "would-") {
					t.Errorf("dry-run produced real action %q (%s)", s.Action, s.Step)
				}
			}
			if !exists(dataDir) {
				t.Error("dry-run removed dataDir")
			}
		})
	}
}

// TestInstallNoCopySkip asserts the binary step is skipped with --no-copy and
// that dry-run still emits only would-* actions.
func TestInstallNoCopySkip(t *testing.T) {
	tmp := t.TempDir()
	o := ServiceOptions{Name: "webclash", Bin: filepath.Join(tmp, "webclash"), DataDir: filepath.Join(tmp, "data"), Port: 7788, Host: "0.0.0.0", User: "root", Manager: "systemd"}
	steps, err := InstallService(o, InstallFlags{DryRun: true, NoCopy: true})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range steps {
		if s.Step == "安装二进制" {
			found = true
			if s.Action != "skip" || s.Detail != "--no-copy" {
				t.Errorf("no-copy binary step = %+v", s)
			}
		}
	}
	if !found {
		t.Error("no binary step")
	}
}
