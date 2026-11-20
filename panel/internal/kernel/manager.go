// Package kernel ports server/src/kernel: mihomo process lifecycle, log ring,
// status and crash breaker. G4 implements spawn/logs/restart; G6 the download
// pipeline. The Node EventEmitter has no subscribers (the WS bridge talks to the
// controller directly), so it is intentionally not ported.
package kernel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"webclash/internal/config"
	"webclash/internal/mihomo"
	"webclash/internal/util"
)

// Options wires the manager to panel paths and settings.
type Options struct {
	DataDir     string
	KernelDir   string
	ConfigPath  string
	GetSettings func() config.Settings
	// BeforeStart runs after the binary is resolved and before the child spawns
	// (bounded by a 30s context). Core uses it to provision geo .dat files and
	// rewrite the config if new files appeared, so mihomo never self-downloads.
	BeforeStart func(context.Context)
}

type versionEntry struct {
	path    string
	mtimeMs int64
	version *string
}

var versionRe = regexp.MustCompile(`v(\d+\.\d+\.\d+)`)

// Manager owns the mihomo child process and its runtime state.
type Manager struct {
	opts Options

	// opMu serializes lifecycle operations (Start/Stop). Restart calls them in
	// sequence without holding opMu so the non-reentrant lock never deadlocks.
	opMu sync.Mutex

	mu             sync.Mutex
	state          string
	lastError      *string
	startedAt      *int64
	logs           []string
	child          *exec.Cmd
	childExited    bool
	exitCh         chan struct{}
	intentional    bool
	restartTimes   []int64
	restartTimer   *time.Timer
	downloadState  config.DownloadState
	downloadCancel context.CancelFunc
	versionCache   *versionEntry
}

// New creates a manager in the stopped state.
func New(o Options) *Manager {
	return &Manager{opts: o, state: "stopped"}
}

// State returns the current runtime state.
func (m *Manager) State() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// IsRunning reports whether the kernel is in the running state.
func (m *Manager) IsRunning() bool { return m.State() == "running" }

// ManagedPath is the panel-managed binary location.
func (m *Manager) ManagedPath() string {
	return filepath.Join(m.opts.DataDir, "bin", "mihomo")
}

// Client builds a controller client from the current settings.
func (m *Manager) Client() *mihomo.Client {
	s := m.opts.GetSettings()
	return mihomo.NewClient(s.Controller.Port, s.Controller.Secret)
}

func (m *Manager) binaryPath() string {
	s := m.opts.GetSettings()
	custom := strings.TrimSpace(s.Kernel.BinaryPath)
	if custom != "" {
		if util.Exists(custom) {
			return custom
		}
		return ""
	}
	if util.Exists(m.ManagedPath()) {
		return m.ManagedPath()
	}
	return util.FindInPath("mihomo")
}

func (m *Manager) readVersion(bin string) *string {
	stat, err := os.Stat(bin)
	if err != nil {
		return nil
	}
	mtime := stat.ModTime().UnixMilli()
	m.mu.Lock()
	if m.versionCache != nil && m.versionCache.path == bin && m.versionCache.mtimeMs == mtime {
		v := m.versionCache.version
		m.mu.Unlock()
		return v
	}
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-v").CombinedOutput()
	if err != nil {
		return nil
	}
	var version *string
	if matched := versionRe.FindSubmatch(out); matched != nil {
		v := "v" + string(matched[1])
		version = &v
	}
	m.mu.Lock()
	m.versionCache = &versionEntry{path: bin, mtimeMs: mtime, version: version}
	m.mu.Unlock()
	return version
}

// Status returns the kernel runtime snapshot (port of status).
func (m *Manager) Status() config.KernelStatus {
	bin := m.binaryPath()
	managed := bin != "" && filepath.Clean(bin) == filepath.Clean(m.ManagedPath())
	var version *string
	if bin != "" {
		version = m.readVersion(bin)
	}
	var binPath *string
	if bin != "" {
		binPath = &bin
	}

	m.mu.Lock()
	state := m.state
	var pid *int
	if m.child != nil && !m.childExited && m.child.Process != nil {
		p := m.child.Process.Pid
		pid = &p
	}
	startedAt := m.startedAt
	lastError := m.lastError
	download := m.downloadState
	m.mu.Unlock()

	return config.KernelStatus{
		State:     state,
		Running:   state == "running",
		PID:       pid,
		StartedAt: startedAt,
		LastError: lastError,
		Binary:    config.BinaryStatus{Exists: bin != "", Path: binPath, Version: version, Managed: managed},
		Download:  download,
	}
}

// log timestamps and rings a line (port of log). Multi-line input is split.
func (m *Manager) log(line string) {
	stamp := time.Now().Format("15:04:05")
	for _, part := range strings.Split(line, "\n") {
		trimmed := strings.TrimRight(part, " \t\r\n\v\f")
		if trimmed == "" {
			continue
		}
		entry := stamp + " " + trimmed
		m.mu.Lock()
		m.logs = append(m.logs, entry)
		if len(m.logs) > 400 {
			m.logs = m.logs[len(m.logs)-400:]
		}
		m.mu.Unlock()
		fmt.Printf("[mihomo] %s\n", trimmed)
	}
}

// RecentLogs returns the last n lines, clamped to [1,400] (port of recentLogs).
func (m *Manager) RecentLogs(lines int) []string {
	n := lines
	if n < 1 {
		n = 1
	}
	if n > 400 {
		n = 400
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.logs) <= n {
		return append([]string{}, m.logs...)
	}
	return append([]string{}, m.logs[len(m.logs)-n:]...)
}

// Start launches the kernel and waits for the controller (port of start).
func (m *Manager) Start() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.startLocked()
}

// startLocked runs Start with opMu held. The kernel auto-download is the only
// unbounded step, so opMu is released around it: otherwise Stop (shutdown,
// Restart, UI) would block behind minutes of network I/O — a SIGTERM shutdown
// would hang until the fetch finished. Stop sets intentional, which is
// re-checked after re-acquiring opMu, so a stop that lands during the download
// wins and the kernel is not spawned afterwards.
func (m *Manager) startLocked() error {
	m.mu.Lock()
	if m.state == "running" || m.state == "starting" {
		m.mu.Unlock()
		return nil
	}
	m.intentional = false
	m.lastError = nil
	m.mu.Unlock()

	bin := m.binaryPath()
	if bin == "" {
		s := m.opts.GetSettings()
		if !s.Kernel.AutoDownload {
			return errors.New("未找到 mihomo 内核，请先在“设置 - 内核”中下载")
		}
		m.opMu.Unlock()
		downloadErr := m.Download(s.Kernel.Version)
		m.opMu.Lock()
		m.mu.Lock()
		stopped := m.intentional
		m.mu.Unlock()
		if stopped {
			return nil
		}
		if downloadErr != nil {
			return downloadErr
		}
		bin = m.binaryPath()
		if bin == "" {
			return errors.New("内核下载完成后仍未找到可执行文件")
		}
	}

	m.ensureKernelDir()
	m.mu.Lock()
	m.state = "starting"
	m.startedAt = nil
	m.mu.Unlock()

	if m.opts.BeforeStart != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		m.opts.BeforeStart(ctx)
		cancel()
	}

	cmd := exec.Command(bin, "-d", m.opts.KernelDir)
	cmd.Dir = m.opts.KernelDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	lw := &lineWriter{m: m}
	cmd.Stdout = lw
	cmd.Stderr = lw
	if err := cmd.Start(); err != nil {
		msg := "内核进程错误：" + err.Error()
		m.mu.Lock()
		m.lastError = &msg
		m.state = "error"
		m.mu.Unlock()
		m.log(msg)
		return errors.New(msg)
	}

	exitCh := make(chan struct{})
	m.mu.Lock()
	m.child = cmd
	m.childExited = false
	m.exitCh = exitCh
	m.mu.Unlock()
	m.log(fmt.Sprintf("启动内核：%s -d %s", bin, m.opts.KernelDir))

	go func() {
		err := cmd.Wait()
		lw.flush()
		m.handleExit(cmd, err)
		close(exitCh)
	}()

	if !m.waitForController(30000) {
		m.mu.Lock()
		m.intentional = true
		if m.lastError == nil {
			msg := "内核启动超时，请检查端口占用、配置是否合法，或查看内核日志"
			m.lastError = &msg
		}
		last := *m.lastError
		m.state = "error"
		m.mu.Unlock()
		m.log(last)
		_ = cmd.Process.Kill()
		return errors.New(last)
	}

	m.mu.Lock()
	m.state = "running"
	now := time.Now().UnixMilli()
	m.startedAt = &now
	m.restartTimes = nil
	m.mu.Unlock()
	m.log("内核已就绪")
	return nil
}

// Stop terminates the kernel (SIGTERM, then SIGKILL after 5s) (port of stop).
func (m *Manager) Stop() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	m.intentional = true
	if m.restartTimer != nil {
		m.restartTimer.Stop()
		m.restartTimer = nil
	}
	child := m.child
	exited := m.childExited
	exitCh := m.exitCh
	if child == nil || exited {
		m.child = nil
		m.state = "stopped"
		m.startedAt = nil
		m.mu.Unlock()
		return nil
	}
	m.state = "stopping"
	m.mu.Unlock()

	_ = child.Process.Signal(syscall.SIGTERM)
	if exitCh != nil {
		select {
		case <-exitCh:
		case <-time.After(5 * time.Second):
			_ = child.Process.Kill()
			<-exitCh
		}
	}

	m.mu.Lock()
	m.child = nil
	m.startedAt = nil
	m.state = "stopped"
	m.mu.Unlock()
	m.log("内核已停止")
	return nil
}

// Restart stops then starts the kernel.
func (m *Manager) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	return m.Start()
}

// ReloadConfig hot-reloads the running kernel's config file.
func (m *Manager) ReloadConfig() error {
	_, err := m.Client().Request("PUT", "/configs?force=true", map[string]any{"path": m.opts.ConfigPath}, 15000)
	return err
}

// Download fetches/updates the kernel binary (port of download). It does not
// hold opMu: the active flag under mu rejects concurrent downloads, and the
// post-download Restart only re-enters opMu when wasRunning (never the case for
// the Start→autoDownload path, which runs while stopped), so there is no
// re-entrant deadlock.
func (m *Manager) Download(version string) error {
	settings := m.opts.GetSettings()
	m.mu.Lock()
	if m.downloadState.Active {
		m.mu.Unlock()
		return errors.New("已有内核下载任务正在进行")
	}
	wasRunning := m.state == "running"
	m.mu.Unlock()

	if wasRunning && runtime.GOOS == "windows" {
		if err := m.Stop(); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	resolving := "resolving"
	m.mu.Lock()
	m.downloadCancel = cancel
	m.downloadState = config.DownloadState{Active: true, Stage: &resolving}
	m.mu.Unlock()

	label := version
	if label == "" {
		label = "latest"
	}
	m.log(fmt.Sprintf("开始下载 mihomo 内核（%s）", label))

	tag, err := downloadKernel(ctx, downloadOptions{
		version: version,
		mirror:  settings.Kernel.Mirror,
		dest:    m.ManagedPath(),
		tmpDir:  filepath.Join(m.opts.DataDir, "tmp"),
		onStage: func(stage string) {
			m.mu.Lock()
			s := stage
			m.downloadState.Stage = &s
			m.mu.Unlock()
		},
		onProgress: func(received, total int64) {
			m.mu.Lock()
			m.downloadState.Received = received
			m.downloadState.Total = total
			if total > 0 {
				m.downloadState.Progress = int(math.Min(100, util.JsRound(float64(received)/float64(total)*100)))
			} else {
				m.downloadState.Progress = 0
			}
			m.mu.Unlock()
		},
	})

	var downloadErr error
	if err == nil {
		m.mu.Lock()
		m.downloadState.Progress = 100
		m.downloadState.Stage = nil
		m.downloadState.Version = &tag
		m.versionCache = nil
		m.mu.Unlock()
		m.log(fmt.Sprintf("内核下载完成：%s -> %s", tag, m.ManagedPath()))
		if wasRunning {
			if rerr := m.Restart(); rerr != nil {
				em := "内核重启失败：" + util.ErrorMessage(rerr)
				m.mu.Lock()
				m.lastError = &em
				m.mu.Unlock()
				m.log(em)
			}
		}
	} else {
		downloadErr = err
		em := util.ErrorMessage(err)
		le := "内核下载失败：" + em
		m.mu.Lock()
		m.downloadState.Error = &em
		m.lastError = &le
		m.mu.Unlock()
		m.log(le)
		if wasRunning && m.State() != "running" {
			_ = m.Start()
		}
	}

	m.mu.Lock()
	m.downloadState.Active = false
	m.downloadState.Stage = nil
	m.downloadCancel = nil
	m.mu.Unlock()
	return downloadErr
}

// CancelDownload aborts an in-flight download (port of cancelDownload).
func (m *Manager) CancelDownload() error {
	m.mu.Lock()
	cancel := m.downloadCancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (m *Manager) handleExit(cmd *exec.Cmd, _ error) {
	m.mu.Lock()
	if m.child != cmd {
		m.mu.Unlock()
		return
	}
	intentional := m.intentional
	m.child = nil
	m.childExited = true
	m.startedAt = nil
	if intentional {
		m.state = "stopped"
		m.mu.Unlock()
		return
	}

	m.state = "error"
	code, signal := exitCodeSignal(cmd)
	msg := fmt.Sprintf("内核异常退出（code=%s，signal=%s）", code, signal)
	m.lastError = &msg

	now := time.Now().UnixMilli()
	kept := m.restartTimes[:0]
	for _, t := range m.restartTimes {
		if now-t < 60000 {
			kept = append(kept, t)
		}
	}
	m.restartTimes = kept
	if len(m.restartTimes) >= 5 {
		breakMsg := "内核在短时间内多次退出，已停止自动拉起，请检查配置"
		m.lastError = &breakMsg
		m.mu.Unlock()
		m.log(msg)
		m.log(breakMsg)
		return
	}
	m.restartTimes = append(m.restartTimes, now)
	m.mu.Unlock()

	m.log(msg)
	m.log("3 秒后尝试自动重启内核")

	m.mu.Lock()
	m.restartTimer = time.AfterFunc(3*time.Second, func() {
		m.mu.Lock()
		m.restartTimer = nil
		m.mu.Unlock()
		if err := m.Start(); err != nil {
			em := util.ErrorMessage(err)
			m.mu.Lock()
			m.state = "error"
			m.lastError = &em
			m.mu.Unlock()
			m.log("自动重启失败：" + em)
		}
	})
	m.mu.Unlock()
}

func (m *Manager) waitForController(timeoutMs int) bool {
	started := time.Now()
	for time.Since(started) < time.Duration(timeoutMs)*time.Millisecond {
		m.mu.Lock()
		gone := m.child == nil || m.childExited
		m.mu.Unlock()
		if gone {
			return false
		}
		if _, err := m.Client().Request("GET", "/version", nil, 1000); err == nil {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

func (m *Manager) ensureKernelDir() {
	_ = os.MkdirAll(m.opts.KernelDir, 0o755)
}

func exitCodeSignal(cmd *exec.Cmd) (string, string) {
	if cmd.ProcessState == nil {
		return "-", "-"
	}
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		if ws.Signaled() {
			return "-", signalName(ws.Signal())
		}
		return strconv.Itoa(ws.ExitStatus()), "-"
	}
	return strconv.Itoa(cmd.ProcessState.ExitCode()), "-"
}

func signalName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGQUIT:
		return "SIGQUIT"
	case syscall.SIGABRT:
		return "SIGABRT"
	case syscall.SIGSEGV:
		return "SIGSEGV"
	case syscall.SIGHUP:
		return "SIGHUP"
	default:
		return "SIG" + strconv.Itoa(int(sig))
	}
}

// lineWriter buffers child stdout/stderr into whole lines (shared buffer, like
// the Node onData). exec runs one copy goroutine per stream, so Write is guarded.
type lineWriter struct {
	mu  sync.Mutex
	m   *Manager
	buf string
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf += string(p)
	for {
		i := strings.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := w.buf[:i]
		w.buf = w.buf[i+1:]
		w.m.log(line)
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.TrimSpace(w.buf) != "" {
		w.m.log(w.buf)
	}
	w.buf = ""
}
