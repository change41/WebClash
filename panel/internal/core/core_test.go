package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"webclash/internal/config"
	"webclash/internal/util"
)

func TestUpdateAllProfilesConflict(t *testing.T) {
	c := New(t.TempDir())
	c.profileUpdateMu.Lock()
	defer c.profileUpdateMu.Unlock()
	_, err := c.UpdateAllProfiles()
	he, ok := err.(*util.HttpError)
	if !ok || he.StatusCode != 409 || he.Message != "订阅更新正在进行中" {
		t.Fatalf("expected 409 conflict, got %v", err)
	}
}

func TestRunAutoUpdateNoDue(t *testing.T) {
	c := New(t.TempDir())
	c.runAutoUpdate()
	if !c.profileUpdateMu.TryLock() {
		t.Fatal("profileUpdateMu not released after no-due run")
	}
	c.profileUpdateMu.Unlock()
}

func TestRunAutoUpdateSkipsWhenBusy(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("proxies: []\nrules: [MATCH,DIRECT]\n"))
	}))
	defer srv.Close()

	c := newDueCore(t, srv.URL, 1, 2)
	c.profileUpdateMu.Lock()
	c.runAutoUpdate() // TryLock fails → returns without fetching
	c.profileUpdateMu.Unlock()
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("busy run should not fetch, hits=%d", hits)
	}
}

func TestRunAutoUpdateRefreshesDue(t *testing.T) {
	var hits int32
	const body = "proxies: []\nrules: [MATCH,DIRECT]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("subscription-userinfo", "upload=1; download=2; total=3; expire=1893456000")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := newDueCore(t, srv.URL, 1, 2) // intervalHours=1, updated 2h ago → due
	old := c.profilesState.Profiles[0].UpdatedAt
	c.runAutoUpdate()

	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("due profile was not refreshed")
	}
	c.mu.RLock()
	updated := c.profilesState.Profiles[0].UpdatedAt
	ui := c.profilesState.Profiles[0].UserInfo
	c.mu.RUnlock()
	if updated <= old {
		t.Fatalf("updatedAt not bumped: old=%d new=%d", old, updated)
	}
	if ui == nil || ui.Total == nil || *ui.Total != 3 {
		t.Fatalf("userInfo not refreshed: %+v", ui)
	}
	got, err := os.ReadFile(c.ProfileFile(c.profilesState.Profiles[0].ID))
	if err != nil || string(got) != body {
		t.Fatalf("profile file not rewritten: %q err=%v", got, err)
	}
	if !c.profileUpdateMu.TryLock() {
		t.Fatal("profileUpdateMu not released")
	}
	c.profileUpdateMu.Unlock()
}

func TestRunAutoUpdateNotDue(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer srv.Close()

	// intervalHours=0 disables auto-update; recent updatedAt also not due.
	for _, tc := range []struct {
		interval int
		hoursAgo int
	}{{0, 2}, {1, 0}} {
		c := newDueCore(t, srv.URL, tc.interval, tc.hoursAgo)
		c.runAutoUpdate()
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("not-due profiles should not fetch, hits=%d", hits)
	}
}

func TestShutdownStopsTicker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"kernel":{"autoStart":false}}`), 0o600); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	c := New(dir)
	if err := c.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := c.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case <-c.autoUpdateDone:
	case <-time.After(2 * time.Second):
		t.Fatal("autoUpdate ticker did not stop on Shutdown")
	}
	// stopOnce makes a second Shutdown safe.
	if err := c.Shutdown(); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

// newDueCore builds a Core (no Init) with one remote profile whose updatedAt is
// hoursAgo in the past and the given auto-update interval, plus the dirs needed
// for writeRuntimeConfig. The profile is active.
func newDueCore(t *testing.T, url string, intervalHours, hoursAgo int) *Core {
	t.Helper()
	c := New(t.TempDir())
	if err := os.MkdirAll(c.ProfilesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.KernelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "abcd1234"
	if err := os.WriteFile(c.ProfileFile(id), []byte("proxies: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().UnixMilli() - int64(hoursAgo)*3600_000
	u := url
	c.mu.Lock()
	c.profilesState = config.ProfilesState{
		ActiveID: &id,
		Profiles: []config.ProfileMeta{{
			ID: id, Name: "sub", Type: "remote", URL: &u,
			CreatedAt: ts, UpdatedAt: ts,
			Settings: config.ProfileSettings{IntervalHours: intervalHours},
		}},
	}
	c.mu.Unlock()
	return c
}
