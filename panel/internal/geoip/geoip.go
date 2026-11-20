// Package geoip is the opt-in connection IP-geolocation feature. It downloads a
// lookup database (ip2region v4+v6 xdb, or the MetaCubeX country-level mmdb) via
// the same mirror-chain/atomic/non-fatal philosophy as the geo package, and
// answers batch lookups over a separate REST path. It NEVER touches the kernel
// or the /ws/connections dumb pipe, and a missing/failed DB degrades to "unknown"
// rather than blocking startup.
package geoip

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"webclash/internal/util"
)

// Region is the source-normalized geolocation shape returned to the frontend.
type Region struct {
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	Province    string `json:"province"`
	City        string `json:"city"`
	ISP         string `json:"isp"`
	Raw         string `json:"raw"`
}

type reader interface {
	Lookup(ip string) (Region, bool)
	Versions() map[string]string
	Close()
}

// FileStatus is one database file's local snapshot for the status endpoint.
type FileStatus struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	UpdatedAt int64  `json:"updatedAt"`
	Version   string `json:"version,omitempty"`
}

// Status is the manager's internal snapshot; the API adds enable/source from
// settings on top of it.
type Status struct {
	Source          string       `json:"source"`
	Ready           bool         `json:"ready"`
	Downloading     bool         `json:"downloading"`
	Files           []FileStatus `json:"files"`
	LastCheckAt     *int64       `json:"lastCheckAt"`
	UpdateAvailable bool         `json:"updateAvailable"`
	Error           *string      `json:"error"`
}

// CheckFile pairs the remote and local fingerprint of one file.
type CheckFile struct {
	Name   string   `json:"name"`
	Remote FileMeta `json:"remote"`
	Local  FileMeta `json:"local"`
}

// CheckResult is the outcome of a best-effort remote version probe.
type CheckResult struct {
	UpdateAvailable bool        `json:"updateAvailable"`
	Files           []CheckFile `json:"files"`
	LastCheckAt     int64       `json:"lastCheckAt"`
}

// Manager owns the geoip dir, persisted state, the active reader and the
// background download lifecycle. All public methods are concurrency-safe.
type Manager struct {
	dir string

	ctx    context.Context
	cancel context.CancelFunc

	mu          sync.RWMutex
	state       State
	reader      reader
	ready       bool
	downloading bool
	lastError   *string
}

// NewManager creates a manager rooted at dir, loading any persisted state.
func NewManager(dir string) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{dir: dir, ctx: ctx, cancel: cancel, state: loadState(dir)}
}

// Ensure downloads any missing files for source (all files when force), then
// opens the reader. It is fire-and-forget: failures are recorded in lastError and
// readiness stays false, never fatal. A concurrent call is coalesced.
func (m *Manager) Ensure(source, mirror string, force bool) {
	m.mu.Lock()
	if m.downloading {
		m.mu.Unlock()
		return
	}
	m.downloading = true
	m.lastError = nil
	if m.state.Source != source || force {
		m.ready = false
		if m.reader != nil {
			m.reader.Close()
			m.reader = nil
		}
	}
	m.state.Source = source
	m.mu.Unlock()

	if err := util.EnsureDir(m.dir); err != nil {
		m.finish(nil, fmt.Errorf("创建 geoip 目录失败：%w", err))
		return
	}

	client := newHTTPClient()
	var wg sync.WaitGroup
	for _, spec := range specs(source, mirror) {
		wg.Add(1)
		go func(spec fileSpec) {
			defer wg.Done()
			dest := filepath.Join(m.dir, spec.name)
			if !force && util.Exists(dest) {
				m.mu.Lock()
				fm := m.state.Files[spec.name]
				if st, err := os.Stat(dest); err == nil {
					fm.Size = st.Size()
					if fm.UpdatedAt == 0 {
						fm.UpdatedAt = st.ModTime().UnixMilli()
					}
				}
				m.state.Files[spec.name] = fm
				m.mu.Unlock()
				return
			}
			if meta, ok := downloadFile(m.ctx, client, m.dir, spec); ok {
				m.mu.Lock()
				m.state.Files[spec.name] = meta
				m.mu.Unlock()
			}
		}(spec)
	}
	wg.Wait()

	rdr, err := m.openReader(source)
	m.finish(rdr, err)
}

func (m *Manager) openReader(source string) (reader, error) {
	if source == "mmdb" {
		path := filepath.Join(m.dir, fileMMDB)
		if !util.Exists(path) {
			return nil, fmt.Errorf("mmdb 数据库缺失")
		}
		return openMMDB(path)
	}
	v4 := filepath.Join(m.dir, fileV4)
	v6 := filepath.Join(m.dir, fileV6)
	if !util.Exists(v4) {
		v4 = ""
	}
	if !util.Exists(v6) {
		v6 = ""
	}
	if v4 == "" && v6 == "" {
		return nil, fmt.Errorf("ip2region 数据库缺失")
	}
	return openIP2Region(v4, v6)
}

func (m *Manager) finish(rdr reader, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.downloading = false
	if err != nil || rdr == nil {
		m.ready = false
		msg := "数据库打开失败"
		if err != nil {
			msg = util.ErrorMessage(err)
		}
		m.lastError = &msg
		_ = saveState(m.dir, m.state)
		return
	}
	m.reader = rdr
	m.ready = true
	m.lastError = nil
	// A completed (re)download is as current as we can be: drop any stale
	// "new version" flag so the badge clears once the DB is refreshed.
	m.state.UpdateAvailable = false
	for name, ver := range rdr.Versions() {
		fm := m.state.Files[name]
		fm.Version = ver
		m.state.Files[name] = fm
	}
	_ = saveState(m.dir, m.state)
}

// CheckUpdate probes the remote fingerprint of each file (best-effort HEAD) and
// compares it with the local copy. It only compares once a local DB is ready and
// no download is in flight: before the first download completes the files are
// absent, which is "not downloaded yet", NOT "a new version" — reporting an
// update there made the first enable (right as the auto download starts) falsely
// show 「有新版本」. A mirror that strips headers yields "unknown" (no change
// claimed), never an error.
func (m *Manager) CheckUpdate(ctx context.Context, source, mirror string) CheckResult {
	now := time.Now().UnixMilli()
	m.mu.RLock()
	ready := m.ready && m.state.Source == source
	downloading := m.downloading
	m.mu.RUnlock()
	if !ready || downloading {
		m.mu.Lock()
		m.state.LastCheckAt = &now
		m.state.UpdateAvailable = false
		_ = saveState(m.dir, m.state)
		m.mu.Unlock()
		return CheckResult{UpdateAvailable: false, LastCheckAt: now}
	}

	client := newHTTPClient()
	var files []CheckFile
	available := false
	for _, spec := range specs(source, mirror) {
		remote, _ := headFile(ctx, client, spec)
		m.mu.RLock()
		local := m.state.Files[spec.name]
		m.mu.RUnlock()
		if !util.Exists(filepath.Join(m.dir, spec.name)) {
			available = true
		} else if metaChanged(remote, local) {
			available = true
		}
		files = append(files, CheckFile{Name: spec.name, Remote: remote, Local: local})
	}
	m.mu.Lock()
	m.state.LastCheckAt = &now
	m.state.UpdateAvailable = available
	_ = saveState(m.dir, m.state)
	m.mu.Unlock()
	return CheckResult{UpdateAvailable: available, Files: files, LastCheckAt: now}
}

// Status snapshots readiness/files for the given (settings) source. Readiness is
// only true when the active reader matches the requested source.
func (m *Manager) Status(source string) Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := Status{
		Source:          source,
		Ready:           m.ready && m.state.Source == source,
		Downloading:     m.downloading,
		LastCheckAt:     m.state.LastCheckAt,
		UpdateAvailable: m.state.UpdateAvailable,
		Error:           m.lastError,
	}
	for _, name := range sourceFiles(source) {
		fm := m.state.Files[name]
		fs := FileStatus{Name: name, Size: fm.Size, UpdatedAt: fm.UpdatedAt, Version: fm.Version}
		if info, err := os.Stat(filepath.Join(m.dir, name)); err == nil {
			fs.Size = info.Size()
		}
		st.Files = append(st.Files, fs)
	}
	return st
}

// Ready reports whether a lookup can be served for the current reader.
func (m *Manager) Ready() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ready
}

// Lookup resolves one IP. The underlying readers are concurrency-safe, so shared
// RLock holders may search in parallel.
func (m *Manager) Lookup(ip string) (Region, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.ready || m.reader == nil {
		return Region{}, false
	}
	return m.reader.Lookup(ip)
}

// Close cancels any in-flight download and releases the reader (files are kept).
func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reader != nil {
		m.reader.Close()
		m.reader = nil
	}
	m.ready = false
}
