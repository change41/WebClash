package sysproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webclash/internal/config"
)

func TestDetectShellFromEnvAndConfigPaths(t *testing.T) {
	home := t.TempDir()

	cases := []struct {
		name     string
		env      Env
		wantType string
		wantFile string
	}{
		{
			name:     "linux bash",
			env:      Env{GOOS: "linux", HomeDir: home, ShellEnv: "/bin/bash", ShellEnvSet: true},
			wantType: "bash",
			wantFile: filepath.Join(home, ".bashrc"),
		},
		{
			name:     "darwin bash defaults to bash_profile",
			env:      Env{GOOS: "darwin", HomeDir: home, ShellEnv: "/bin/bash", ShellEnvSet: true},
			wantType: "bash",
			wantFile: filepath.Join(home, ".bash_profile"),
		},
		{
			name:     "zsh default",
			env:      Env{GOOS: "linux", HomeDir: home, ShellEnv: "/usr/bin/zsh", ShellEnvSet: true, ZdotdirSet: true},
			wantType: "zsh",
			wantFile: filepath.Join(home, ".zshrc"),
		},
		{
			name:     "zsh respects ZDOTDIR",
			env:      Env{GOOS: "linux", HomeDir: home, ShellEnv: "/bin/zsh", ShellEnvSet: true, Zdotdir: filepath.Join(home, ".zsh"), ZdotdirSet: true},
			wantType: "zsh",
			wantFile: filepath.Join(home, ".zsh", ".zshrc"),
		},
		{
			name:     "fish respects XDG_CONFIG_HOME",
			env:      Env{GOOS: "linux", HomeDir: home, ShellEnv: "/usr/bin/fish", ShellEnvSet: true, XDGConfig: filepath.Join(home, ".xdg"), XDGSet: true},
			wantType: "fish",
			wantFile: filepath.Join(home, ".xdg", "fish", "config.fish"),
		},
		{
			name:     "csh",
			env:      Env{GOOS: "linux", HomeDir: home, ShellEnv: "/bin/csh", ShellEnvSet: true},
			wantType: "csh",
			wantFile: filepath.Join(home, ".cshrc"),
		},
		{
			name:     "dash maps to sh profile",
			env:      Env{GOOS: "linux", HomeDir: home, ShellEnv: "/bin/dash", ShellEnvSet: true},
			wantType: "sh",
			wantFile: filepath.Join(home, ".profile"),
		},
		{
			name:     "nushell",
			env:      Env{GOOS: "linux", HomeDir: home, ShellEnv: "/usr/local/bin/nu", ShellEnvSet: true, XDGSet: true},
			wantType: "nushell",
			wantFile: filepath.Join(home, ".config", "nushell", "env.nu"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := DetectShell(tc.env)
			if info.Shell != tc.wantType {
				t.Errorf("Shell = %q, want %q", info.Shell, tc.wantType)
			}
			if info.ConfigPath != tc.wantFile {
				t.Errorf("ConfigPath = %q, want %q", info.ConfigPath, tc.wantFile)
			}
		})
	}
}

func TestDetectShellPasswdFallback(t *testing.T) {
	home := t.TempDir()
	passwd := filepath.Join(home, "passwd")
	content := "root:x:0:0:root:/root:/bin/bash\nalice:x:1000:1000:Alice:/home/alice:/usr/bin/fish\nsvc:x:999:999::/nonexistent:/usr/sbin/nologin\n"
	if err := os.WriteFile(passwd, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	info := DetectShell(Env{
		GOOS:        "linux",
		HomeDir:     home,
		ShellEnv:    "",
		ShellEnvSet: true,
		PasswdPath:  passwd,
		UID:         1000,
		UIDSet:      true,
		XDGSet:      true,
	})
	if info.Shell != "fish" {
		t.Fatalf("Shell = %q, want fish", info.Shell)
	}
	if info.ConfigPath != filepath.Join(home, ".config", "fish", "config.fish") {
		t.Fatalf("ConfigPath = %q", info.ConfigPath)
	}

	// nologin falls back cleanly instead of returning nologin.
	nologin := DetectShell(Env{
		GOOS:        "darwin",
		HomeDir:     home,
		ShellEnv:    "/usr/sbin/nologin",
		ShellEnvSet: true,
		PasswdPath:  passwd,
		UID:         999,
		UIDSet:      true,
		DarwinShell: func(string) string { return "" },
		CurrentUser: "svc",
	})
	if nologin.Shell != "zsh" {
		t.Fatalf("darwin nologin fallback = %q, want zsh", nologin.Shell)
	}
}

func TestRenderBlockShellsAndAuthEscaping(t *testing.T) {
	specNoAuth := ProxySpec{HTTPPort: 7890, SOCKSPort: 7890}
	bashBlock := RenderBlock("bash", specNoAuth, false)
	for _, want := range []string{
		BlockStart,
		"export http_proxy='http://127.0.0.1:7890'",
		"export HTTPS_PROXY='http://127.0.0.1:7890'",
		"export all_proxy='socks5://127.0.0.1:7890'",
		"export NO_PROXY='localhost,127.0.0.1,::1'",
		BlockEnd,
	} {
		if !strings.Contains(bashBlock, want) {
			t.Errorf("bash block missing %q:\n%s", want, bashBlock)
		}
	}

	// Special characters in password are RFC 3986 percent-encoded by net/url
	// (with $ preserved unescaped in userinfo) and shell-quoted safely.
	specAuth := ProxySpec{
		HTTPPort:  7891,
		SOCKSPort: 7892,
		Username:  "alice",
		Password:  "p@ss:w'o$r!d/1",
	}
	wantEncoded := "http://alice:p%40ss%3Aw%27o$r%21d%2F1@127.0.0.1:7891"
	wantSocks := "socks5://alice:p%40ss%3Aw%27o$r%21d%2F1@127.0.0.1:7892"

	bashAuth := RenderBlock("bash", specAuth, false)
	if !strings.Contains(bashAuth, "export http_proxy='"+wantEncoded+"'") {
		t.Errorf("bash auth escaping unexpected:\n%s", bashAuth)
	}
	if !strings.Contains(bashAuth, "export all_proxy='"+wantSocks+"'") {
		t.Errorf("bash socks auth unexpected:\n%s", bashAuth)
	}

	masked := RenderBlock("bash", specAuth, true)
	if strings.Contains(masked, "p%40ss") || !strings.Contains(masked, "http://alice:******@127.0.0.1:7891") {
		t.Errorf("masked block leaked or malformed:\n%s", masked)
	}

	fishAuth := RenderBlock("fish", specAuth, false)
	if !strings.Contains(fishAuth, "set -gx http_proxy '"+wantEncoded+"'") {
		t.Errorf("fish auth escaping unexpected:\n%s", fishAuth)
	}

	cshAuth := RenderBlock("tcsh", specAuth, false)
	if !strings.Contains(cshAuth, "setenv http_proxy '"+wantEncoded+"'") {
		t.Errorf("csh auth escaping unexpected:\n%s", cshAuth)
	}

	nuAuth := RenderBlock("nushell", specAuth, false)
	if !strings.Contains(nuAuth, "$env.http_proxy = '"+wantEncoded+"'") {
		t.Errorf("nushell auth escaping unexpected:\n%s", nuAuth)
	}
}

func TestShellQuotingHelpers(t *testing.T) {
	if got := quotePOSIX("a'b$c"); got != `'a'\''b$c'` {
		t.Errorf("quotePOSIX = %q", got)
	}
	if got := quoteFish(`a'b\c`); got != `'a\'b\\c'` {
		t.Errorf("quoteFish = %q", got)
	}
	if got := quoteCsh("a'b!c"); got != `'a'\''b\!c'` {
		t.Errorf("quoteCsh = %q", got)
	}
	if got := quoteNushell(`a'b"c`); got != `"a'b\"c"` {
		t.Errorf("quoteNushell = %q", got)
	}
	if got := quoteSingleDoubled("a'b"); got != "'a''b'" {
		t.Errorf("quoteSingleDoubled = %q", got)
	}
}

func TestSpecFromSettings(t *testing.T) {
	s := config.DefaultSettings()
	s.Ports.Mixed = 7890
	spec := SpecFromSettings(s)
	if spec.HTTPPort != 7890 || spec.SOCKSPort != 7890 || spec.Username != "" {
		t.Fatalf("default spec = %+v", spec)
	}

	s.Ports.HTTP = 7891
	s.Ports.SOCKS = 7892
	s.Authentication = []config.InboundUser{
		{Username: "alice", Password: "secret"},
		{Username: "bob", Password: "other"},
	}
	spec = SpecFromSettings(s)
	if spec.HTTPPort != 7891 || spec.SOCKSPort != 7892 || spec.Username != "alice" || spec.Password != "secret" {
		t.Fatalf("custom spec = %+v", spec)
	}
}

func TestScanExistingDetectsExternalAndManaged(t *testing.T) {
	rc := strings.Join([]string{
		"# user comments",
		"# export http_proxy=http://ignored:1234",
		"export PATH=$HOME/bin:$PATH",
		"export http_proxy=http://127.0.0.1:1080",
		"HTTPS_PROXY='http://127.0.0.1:1080'",
		"set -gx all_proxy socks5://127.0.0.1:1081",
		"setenv NO_PROXY localhost",
		BlockStart,
		"export http_proxy='http://127.0.0.1:7890'",
		BlockEnd,
	}, "\n") + "\n"

	entries, hasManaged, managed := ScanExisting(rc)
	if !hasManaged {
		t.Fatal("expected hasManaged=true")
	}
	if !strings.Contains(managed, "export http_proxy='http://127.0.0.1:7890'") {
		t.Fatalf("managed block = %q", managed)
	}
	if len(entries) != 5 {
		t.Fatalf("got %d entries, want 5: %+v", len(entries), entries)
	}
	if entries[0].Line != 4 || entries[0].Managed {
		t.Errorf("entry[0] = %+v, want line 4 unmanaged", entries[0])
	}
	if entries[4].Line != 9 || !entries[4].Managed {
		t.Errorf("entry[4] = %+v, want line 9 managed", entries[4])
	}
}

func TestUpsertAndRemoveRoundTrip(t *testing.T) {
	original := "export EDITOR=vim\nexport http_proxy=http://old:1080\n"
	block1 := RenderBlock("bash", ProxySpec{HTTPPort: 7890, SOCKSPort: 7890}, false)
	block2 := RenderBlock("bash", ProxySpec{HTTPPort: 7890, SOCKSPort: 7890, Username: "alice", Password: "pw"}, false)

	withBlock1 := UpsertBlock(original, block1)
	if !strings.Contains(withBlock1, "export http_proxy=http://old:1080") {
		t.Fatal("UpsertBlock must preserve pre-existing user lines")
	}
	if strings.Count(withBlock1, BlockStart) != 1 {
		t.Fatalf("expected 1 start marker:\n%s", withBlock1)
	}

	// Updating replaces in place without duplicating markers.
	withBlock2 := UpsertBlock(withBlock1, block2)
	if strings.Count(withBlock2, BlockStart) != 1 || !strings.Contains(withBlock2, "alice:pw@") {
		t.Fatalf("second upsert failed:\n%s", withBlock2)
	}

	// Removing restores the exact original file content.
	restored, removed := RemoveBlock(withBlock2)
	if !removed {
		t.Fatal("expected removed=true")
	}
	if restored != original {
		t.Fatalf("restored mismatch:\n got: %q\nwant: %q", restored, original)
	}
}

func TestApplyAndRemovePreservesSymlinkAndCreatesDirs(t *testing.T) {
	dir := t.TempDir()

	// 1. Nested non-existent file (e.g. ~/.config/fish/config.fish)
	fishPath := filepath.Join(dir, ".config", "fish", "config.fish")
	fishShell := ShellInfo{Shell: "fish", ShellPath: "/usr/bin/fish", ConfigPath: fishPath}
	changed, err := Apply(fishShell, ProxySpec{HTTPPort: 7890, SOCKSPort: 7890})
	if err != nil || !changed {
		t.Fatalf("Apply fish: changed=%v err=%v", changed, err)
	}
	// Second apply is a no-op.
	changed, err = Apply(fishShell, ProxySpec{HTTPPort: 7890, SOCKSPort: 7890})
	if err != nil || changed {
		t.Fatalf("second Apply fish: changed=%v err=%v", changed, err)
	}

	// 2. Symlink preservation
	target := filepath.Join(dir, "real_bashrc")
	if err := os.WriteFile(target, []byte("export FOO=bar\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".bashrc")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	bashShell := ShellInfo{Shell: "bash", ShellPath: "/bin/bash", ConfigPath: link}
	if _, err := Apply(bashShell, ProxySpec{HTTPPort: 7890, SOCKSPort: 7890, Username: "u", Password: "p"}); err != nil {
		t.Fatalf("Apply symlink: %v", err)
	}
	linfo, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat symlink: %v", err)
	}
	if linfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was destroyed: mode=%v", linfo.Mode())
	}
	tinfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat target: %v", err)
	}
	if tinfo.Mode().Perm() != 0o640 {
		t.Fatalf("target perm = %o, want 640", tinfo.Mode().Perm())
	}
	raw, _ := os.ReadFile(target)
	if !strings.Contains(string(raw), "http://u:p@127.0.0.1:7890") {
		t.Fatalf("target content missing auth proxy:\n%s", raw)
	}

	removed, err := Remove(link)
	if err != nil || !removed {
		t.Fatalf("Remove symlink: removed=%v err=%v", removed, err)
	}
	raw, _ = os.ReadFile(target)
	if string(raw) != "export FOO=bar\n" {
		t.Fatalf("after Remove, content = %q", raw)
	}
}

func TestDetectShellContainerAndLoginOnlyFlags(t *testing.T) {
	home := t.TempDir()
	profilePath := filepath.Join(home, ".profile")
	yes := true
	no := false

	// POSIX sh without $ENV pointing to ~/.profile is login-shell only.
	shNoEnv := DetectShell(Env{
		GOOS:        "linux",
		HomeDir:     home,
		ShellEnv:    "/bin/sh",
		ShellEnvSet: true,
		ShEnv:       "",
		ShEnvSet:    true,
		InContainer: &yes,
	})
	if !shNoEnv.InContainer || !shNoEnv.LoginShellOnly {
		t.Fatalf("expected InContainer=true LoginShellOnly=true, got %+v", shNoEnv)
	}

	// POSIX sh with $ENV=~/.profile loads on non-login interactive shells too.
	shWithEnv := DetectShell(Env{
		GOOS:        "linux",
		HomeDir:     home,
		ShellEnv:    "/bin/sh",
		ShellEnvSet: true,
		ShEnv:       profilePath,
		ShEnvSet:    true,
		InContainer: &yes,
	})
	if !shWithEnv.InContainer || shWithEnv.LoginShellOnly {
		t.Fatalf("expected InContainer=true LoginShellOnly=false, got %+v", shWithEnv)
	}

	// bash (~/.bashrc) is not login-shell only.
	bashHost := DetectShell(Env{
		GOOS:        "linux",
		HomeDir:     home,
		ShellEnv:    "/bin/bash",
		ShellEnvSet: true,
		InContainer: &no,
	})
	if bashHost.InContainer || bashHost.LoginShellOnly {
		t.Fatalf("expected InContainer=false LoginShellOnly=false, got %+v", bashHost)
	}
}

func TestBuildPreviewMultiFileAndEnvProxies(t *testing.T) {
	home := t.TempDir()
	// Current shell is bash (~/.bashrc has no proxy), but ~/.zshrc and ~/.profile
	// and ~/.config/fish/config.fish already have proxy entries, plus active env vars.
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("export PATH=$HOME/bin:$PATH\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export http_proxy=http://127.0.0.1:1080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("https_proxy=http://127.0.0.1:1081\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fishDir := filepath.Join(home, ".config", "fish")
	if err := os.MkdirAll(fishDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fishDir, "config.fish"), []byte("set -gx all_proxy socks5://127.0.0.1:1082\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := Env{
		GOOS:            "linux",
		HomeDir:         home,
		ShellEnv:        "/bin/bash",
		ShellEnvSet:     true,
		ZdotdirSet:      true,
		XDGSet:          true,
		SkipSystemFiles: true,
		EnvironSet:      true,
		Environ: []string{
			"PATH=/usr/bin",
			"http_proxy=http://127.0.0.1:7897",
			"HTTPS_PROXY=http://127.0.0.1:7897",
			"all_proxy=",
		},
	}
	shellInfo := DetectShell(env)
	preview, err := BuildPreview(env, shellInfo, ProxySpec{HTTPPort: 7890, SOCKSPort: 7890})
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}
	if preview.Shell.ConfigPath != filepath.Join(home, ".bashrc") {
		t.Fatalf("target ConfigPath = %q, want .bashrc", preview.Shell.ConfigPath)
	}
	if !preview.HasExternalProxy || len(preview.Existing) != 3 {
		t.Fatalf("expected 3 external entries across zshrc/profile/fish, got %+v", preview.Existing)
	}
	filesFound := map[string]bool{}
	for _, entry := range preview.Existing {
		filesFound[filepath.Base(entry.File)] = true
	}
	for _, want := range []string{".zshrc", ".profile", "config.fish"} {
		if !filesFound[want] {
			t.Errorf("missing existing proxy from %s in %+v", want, preview.Existing)
		}
	}
	if !preview.HasEnvProxy || len(preview.EnvProxies) != 2 {
		t.Fatalf("expected 2 env proxies, got %+v", preview.EnvProxies)
	}
	if preview.EnvProxies[0].Name != "http_proxy" || preview.EnvProxies[1].Name != "HTTPS_PROXY" {
		t.Fatalf("unexpected env proxies order/content: %+v", preview.EnvProxies)
	}
}
