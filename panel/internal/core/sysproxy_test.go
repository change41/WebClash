package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webclash/internal/sysproxy"
)

func newTestCoreWithShell(t *testing.T, shellPath string) (*Core, string) {
	t.Helper()
	dataDir := t.TempDir()
	homeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "settings.json"), []byte(`{"kernel":{"autoStart":false}}`), 0o600); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	c := New(dataDir)
	c.SetSystemProxyEnv(sysproxy.Env{
		GOOS:            "linux",
		HomeDir:         homeDir,
		ShellEnv:        shellPath,
		ShellEnvSet:     true,
		ZdotdirSet:      true,
		XDGSet:          true,
		EnvironSet:      true,
		SkipSystemFiles: true,
	})
	if err := c.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = c.Shutdown() })
	return c, homeDir
}

func TestSystemProxyPreviewEnableAuthSyncAndDisable(t *testing.T) {
	c, home := newTestCoreWithShell(t, "/bin/bash")
	bashrc := filepath.Join(home, ".bashrc")
	initialRC := "# my bashrc\nexport http_proxy=http://127.0.0.1:9999\nexport FOO=bar\n"
	if err := os.WriteFile(bashrc, []byte(initialRC), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Preview exposes pre-existing proxy line.
	st := c.SystemProxyStatus()
	if st.Enable || st.Synced {
		t.Fatalf("expected disabled initially: %+v", st)
	}
	if !st.Preview.HasExternalProxy || len(st.Preview.Existing) != 1 || st.Preview.Existing[0].Line != 2 {
		t.Fatalf("existing proxy detection failed: %+v", st.Preview.Existing)
	}

	// 2. Enable writes managed block without auth while keeping user lines.
	st, err := c.EnableSystemProxy()
	if err != nil {
		t.Fatalf("EnableSystemProxy: %v", err)
	}
	if !st.Enable || !st.Synced || st.Preview.HasAuth {
		t.Fatalf("after enable: %+v", st)
	}
	raw, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "export http_proxy=http://127.0.0.1:9999") {
		t.Fatal("user existing proxy line was lost")
	}
	mixedPort := c.Settings().Ports.Mixed
	if !strings.Contains(string(raw), fmt.Sprintf("export http_proxy='http://127.0.0.1:%d'", mixedPort)) {
		t.Fatalf("managed proxy not written:\n%s", raw)
	}

	// 3. Adding authentication later proactively updates the shell rc file.
	outcome, err := c.SaveSettingsDetail(map[string]any{
		"authentication": []any{
			map[string]any{"username": "alice", "password": "p@ss:word"},
		},
	})
	if err != nil {
		t.Fatalf("SaveSettingsDetail add auth: %v", err)
	}
	if outcome.SystemProxySync == nil || !outcome.SystemProxySync.Updated || outcome.SystemProxySync.Error != nil {
		t.Fatalf("expected systemProxySync.Updated=true, got %+v", outcome.SystemProxySync)
	}
	raw, _ = os.ReadFile(bashrc)
	if !strings.Contains(string(raw), fmt.Sprintf("export http_proxy='http://alice:p%%40ss%%3Aword@127.0.0.1:%d'", mixedPort)) {
		t.Fatalf("auth not synced to bashrc:\n%s", raw)
	}
	if !strings.Contains(string(raw), fmt.Sprintf("export all_proxy='socks5://alice:p%%40ss%%3Aword@127.0.0.1:%d'", mixedPort)) {
		t.Fatalf("socks auth not synced to bashrc:\n%s", raw)
	}

	// 4. Changing ports & removing authentication proactively updates again.
	outcome, err = c.SaveSettingsDetail(map[string]any{
		"ports":          map[string]any{"mixed": 7895, "http": 7896, "socks": 7897},
		"authentication": []any{},
	})
	if err != nil {
		t.Fatalf("SaveSettingsDetail update ports & clear auth: %v", err)
	}
	if outcome.SystemProxySync == nil || !outcome.SystemProxySync.Updated {
		t.Fatalf("expected systemProxySync.Updated=true, got %+v", outcome.SystemProxySync)
	}
	raw, _ = os.ReadFile(bashrc)
	if strings.Contains(string(raw), "alice") {
		t.Fatalf("removed auth still present:\n%s", raw)
	}
	if !strings.Contains(string(raw), "export http_proxy='http://127.0.0.1:7896'") ||
		!strings.Contains(string(raw), "export all_proxy='socks5://127.0.0.1:7897'") {
		t.Fatalf("updated ports not synced:\n%s", raw)
	}

	// 5. SaveSettings cannot overwrite systemProxy state directly.
	if _, err := c.SaveSettings(map[string]any{
		"systemProxy": map[string]any{"enable": false},
	}); err != nil {
		t.Fatalf("SaveSettings with systemProxy patch: %v", err)
	}
	if !c.Settings().SystemProxy.Enable {
		t.Fatal("SaveSettings must ignore systemProxy field")
	}

	// 6. Disable removes managed block and restores original bashrc exactly.
	st, err = c.DisableSystemProxy()
	if err != nil {
		t.Fatalf("DisableSystemProxy: %v", err)
	}
	if st.Enable || st.Synced {
		t.Fatalf("after disable: %+v", st)
	}
	raw, _ = os.ReadFile(bashrc)
	if string(raw) != initialRC {
		t.Fatalf("bashrc not restored cleanly:\n got: %q\nwant: %q", string(raw), initialRC)
	}
}
