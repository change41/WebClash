package kernel

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"webclash/internal/config"
)

func testManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m := New(Options{
		DataDir:     dir,
		KernelDir:   filepath.Join(dir, "kernel"),
		ConfigPath:  filepath.Join(dir, "kernel", "config.yaml"),
		GetSettings: config.DefaultSettings,
	})
	return m, dir
}

func TestStoppedStatus(t *testing.T) {
	m, _ := testManager(t)
	if m.State() != "stopped" || m.IsRunning() {
		t.Fatalf("state = %q", m.State())
	}
	st := m.Status()
	if st.State != "stopped" || st.Running || st.PID != nil || st.StartedAt != nil {
		t.Fatalf("status = %+v", st)
	}
	if st.Binary.Exists || st.Binary.Managed || st.Binary.Path != nil {
		t.Fatalf("binary = %+v", st.Binary)
	}
	if st.Download.Active || st.Download.Progress != 0 {
		t.Fatalf("download = %+v", st.Download)
	}
}

func TestManagedPathAndBinaryPath(t *testing.T) {
	m, dir := testManager(t)
	if m.ManagedPath() != filepath.Join(dir, "bin", "mihomo") {
		t.Fatalf("managedPath = %q", m.ManagedPath())
	}
	// no binary anywhere => ""
	if m.binaryPath() != "" {
		t.Fatalf("binaryPath should be empty, got %q", m.binaryPath())
	}
	// managed binary present
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	managed := filepath.Join(dir, "bin", "mihomo")
	_ = os.WriteFile(managed, []byte("x"), 0o755)
	if m.binaryPath() != managed {
		t.Fatalf("binaryPath = %q want %q", m.binaryPath(), managed)
	}
	st := m.Status()
	if !st.Binary.Exists || !st.Binary.Managed || st.Binary.Path == nil || *st.Binary.Path != managed {
		t.Fatalf("managed status = %+v", st.Binary)
	}
}

func TestBinaryPathCustom(t *testing.T) {
	dir := t.TempDir()
	custom := filepath.Join(dir, "custom-mihomo")
	_ = os.WriteFile(custom, []byte("x"), 0o755)
	settings := config.DefaultSettings()
	settings.Kernel.BinaryPath = "  " + custom + "  "
	m := New(Options{DataDir: dir, KernelDir: dir, ConfigPath: dir, GetSettings: func() config.Settings { return settings }})
	if m.binaryPath() != custom {
		t.Fatalf("custom binaryPath = %q want %q", m.binaryPath(), custom)
	}
	if m.Status().Binary.Managed {
		t.Fatal("custom binary should not be managed")
	}
	settings.Kernel.BinaryPath = filepath.Join(dir, "missing")
	if m.binaryPath() != "" {
		t.Fatalf("missing custom should yield empty, got %q", m.binaryPath())
	}
}

var stampRe = regexp.MustCompile(`^\d{2}:\d{2}:\d{2} `)

func TestLogRingAndClamp(t *testing.T) {
	m, _ := testManager(t)
	for i := 0; i < 500; i++ {
		m.log("line")
	}
	if got := m.RecentLogs(400); len(got) != 400 {
		t.Fatalf("ring cap: len = %d want 400", len(got))
	}
	if got := m.RecentLogs(10); len(got) != 10 {
		t.Fatalf("recent 10: len = %d", len(got))
	}
	if got := m.RecentLogs(0); len(got) != 1 {
		t.Fatalf("clamp min: len = %d want 1", len(got))
	}
	if got := m.RecentLogs(1000); len(got) != 400 {
		t.Fatalf("clamp max: len = %d want 400", len(got))
	}
	for _, l := range m.RecentLogs(5) {
		if !stampRe.MatchString(l) {
			t.Fatalf("log missing HH:MM:SS stamp: %q", l)
		}
	}
}

func TestLogMultilineTrim(t *testing.T) {
	m, _ := testManager(t)
	m.log("a\nb\n\n  c  \n")
	got := m.RecentLogs(400)
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d: %#v", len(got), got)
	}
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if got[i][len(got[i])-len(w):] != w {
			t.Fatalf("entry %d = %q want suffix %q", i, got[i], w)
		}
	}
}

// Start's kernel auto-download must not hold opMu: before the fix a Stop
// (SIGTERM shutdown / UI stop / Restart) blocked until the whole fetch finished,
// which made service shutdown hang behind an unbounded download.
func TestStartAutoDownloadDoesNotBlockStop(t *testing.T) {
	setBuiltins(t)
	payload := fakePayload(8 * 1024 * 1024)
	srv := newMirrorServer(t, payload)
	srv.slow = true // 20ms per 64KiB => ~2.5s, long enough to observe Stop
	dir := t.TempDir()
	mgr := New(Options{
		DataDir: dir, KernelDir: filepath.Join(dir, "kernel"), ConfigPath: filepath.Join(dir, "kernel", "config.yaml"),
		GetSettings: func() config.Settings {
			s := config.DefaultSettings()
			s.Kernel.Version = "v1.2.3"
			s.Kernel.Mirror = srv.prefix()
			return s
		},
	})

	startDone := make(chan error, 1)
	go func() { startDone <- mgr.Start() }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !mgr.Status().Download.Active {
		time.Sleep(10 * time.Millisecond)
	}
	if !mgr.Status().Download.Active {
		t.Fatal("auto-download never started")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- mgr.Stop() }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop blocked behind Start's kernel auto-download")
	}
	if !mgr.Status().Download.Active {
		t.Fatal("download unexpectedly finished before Stop returned")
	}

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("Start after stop: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Start did not return after the download finished")
	}
	st := mgr.Status()
	if st.State != "stopped" || st.Running || st.PID != nil || st.StartedAt != nil {
		t.Fatalf("stop must win over the in-flight start: %+v", st)
	}
	if st.Download.Active {
		t.Fatalf("download state not finalized: %+v", st.Download)
	}
	if !st.Binary.Exists {
		t.Fatal("the finished download should still install the binary")
	}
}
