package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webclash/internal/sysproxy"
)

func TestSystemProxyAPIFlow(t *testing.T) {
	s := newTestServer(t)
	home := t.TempDir()
	zshrc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(zshrc, []byte("export https_proxy=http://127.0.0.1:8888\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.core.SetSystemProxyEnv(sysproxy.Env{
		GOOS:            "linux",
		HomeDir:         home,
		ShellEnv:        "/bin/zsh",
		ShellEnvSet:     true,
		ZdotdirSet:      true,
		XDGSet:          true,
		EnvironSet:      true,
		SkipSystemFiles: true,
	})

	// Requires auth.
	if r := do(s, "GET", "/api/system-proxy/status", "", nil); r.code != 401 {
		t.Fatalf("unauth status = %d, want 401", r.code)
	}

	cookies := loginCookies(t, s)

	// Status reports detected zsh and existing proxy line.
	r := do(s, "GET", "/api/system-proxy/status", "", cookies)
	if r.code != 200 {
		t.Fatalf("status = %d %s", r.code, r.body)
	}
	if jsonField(t, r.body, "enable") != false {
		t.Fatalf("expected enable=false: %s", r.body)
	}
	preview, _ := jsonField(t, r.body, "preview").(map[string]any)
	if preview == nil || preview["hasExternalProxy"] != true {
		t.Fatalf("expected hasExternalProxy=true: %s", r.body)
	}

	// Enable without confirmation is rejected.
	r = do(s, "POST", "/api/system-proxy/enable", `{}`, cookies)
	if r.code != 400 {
		t.Fatalf("unconfirmed enable = %d %s, want 400", r.code, r.body)
	}

	// Enable with confirmed=true succeeds and writes .zshrc.
	mixedPort := s.core.Settings().Ports.Mixed
	r = do(s, "POST", "/api/system-proxy/enable", `{"confirmed":true}`, cookies)
	if r.code != 200 {
		t.Fatalf("confirmed enable = %d %s", r.code, r.body)
	}
	raw, _ := os.ReadFile(zshrc)
	if !strings.Contains(string(raw), fmt.Sprintf("export http_proxy='http://127.0.0.1:%d'", mixedPort)) {
		t.Fatalf("zshrc missing proxy block:\n%s", raw)
	}

	// Later adding authentication via PUT /api/settings proactively updates .zshrc.
	r = do(s, "PUT", "/api/settings", `{"authentication":[{"username":"dev","password":"s3cret"}]}`, cookies)
	if r.code != 200 {
		t.Fatalf("PUT settings auth = %d %s", r.code, r.body)
	}
	syncRes, _ := jsonField(t, r.body, "systemProxySync").(map[string]any)
	if syncRes == nil || syncRes["updated"] != true {
		t.Fatalf("expected systemProxySync.updated=true: %s", r.body)
	}
	raw, _ = os.ReadFile(zshrc)
	if !strings.Contains(string(raw), fmt.Sprintf("export http_proxy='http://dev:s3cret@127.0.0.1:%d'", mixedPort)) {
		t.Fatalf("zshrc not updated with auth:\n%s", raw)
	}

	// Disable cleans up managed block while keeping user's pre-existing line.
	r = do(s, "POST", "/api/system-proxy/disable", "", cookies)
	if r.code != 200 {
		t.Fatalf("disable = %d %s", r.code, r.body)
	}
	raw, _ = os.ReadFile(zshrc)
	if string(raw) != "export https_proxy=http://127.0.0.1:8888\n" {
		t.Fatalf("zshrc after disable = %q", string(raw))
	}
}
