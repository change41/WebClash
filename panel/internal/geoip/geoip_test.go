package geoip

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"webclash/internal/util"
)

func TestParseIP2Region(t *testing.T) {
	cases := []struct {
		in                               string
		country, province, city, isp, cc string
	}{
		{"中国|广东省|深圳市|电信|CN", "中国", "广东省", "深圳市", "电信", "CN"},
		{"United States|California|0|Google LLC|US", "United States", "California", "", "Google LLC", "US"},
		{"中国|0|0|电信|CN", "中国", "", "", "电信", "CN"},
		{"Reserved|Reserved|Reserved|0|0", "Reserved", "", "", "", ""},
		{"中国", "中国", "", "", "", ""},
		{"", "", "", "", "", ""},
		{" 中国 | 广东省 | 深圳市 | 电信 | CN ", "中国", "广东省", "深圳市", "电信", "CN"},
	}
	for _, c := range cases {
		got := parseIP2Region(c.in)
		if got.Country != c.country || got.Province != c.province || got.City != c.city || got.ISP != c.isp || got.CountryCode != c.cc {
			t.Fatalf("parse %q = %+v, want country=%q province=%q city=%q isp=%q cc=%q", c.in, got, c.country, c.province, c.city, c.isp, c.cc)
		}
		if got.Raw != c.in {
			t.Fatalf("parse %q raw = %q, want the original string", c.in, got.Raw)
		}
	}
}

type stubReader struct {
	regions  map[string]Region
	versions map[string]string
	closed   bool
}

func (s *stubReader) Lookup(ip string) (Region, bool) { r, ok := s.regions[ip]; return r, ok }
func (s *stubReader) Versions() map[string]string     { return s.versions }
func (s *stubReader) Close()                          { s.closed = true }

func TestManagerLookupNotReady(t *testing.T) {
	m := NewManager(t.TempDir())
	defer m.Close()
	if m.Ready() {
		t.Fatal("a fresh manager must not be ready")
	}
	if _, ok := m.Lookup("1.2.3.4"); ok {
		t.Fatal("not-ready lookup must return false")
	}
}

func TestManagerFinishStub(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	defer m.Close()
	m.state.Source = "ip2region"
	stub := &stubReader{
		regions:  map[string]Region{"1.2.3.4": {Country: "中国", CountryCode: "CN"}},
		versions: map[string]string{fileV4: "2024-01-01"},
	}
	m.finish(stub, nil)

	if !m.Ready() {
		t.Fatal("finish with a reader must mark ready")
	}
	if r, ok := m.Lookup("1.2.3.4"); !ok || r.Country != "中国" || r.CountryCode != "CN" {
		t.Fatalf("lookup = %+v ok=%v", r, ok)
	}
	if _, ok := m.Lookup("9.9.9.9"); ok {
		t.Fatal("unknown ip must not resolve")
	}
	if m.state.Files[fileV4].Version != "2024-01-01" {
		t.Fatalf("reader version not merged into state: %+v", m.state.Files[fileV4])
	}
	if !m.Status("ip2region").Ready {
		t.Fatal("status must be ready for the matching source")
	}
	if m.Status("mmdb").Ready {
		t.Fatal("status must not be ready for a mismatched source")
	}
	if got := loadState(dir); got.Files[fileV4].Version != "2024-01-01" {
		t.Fatalf("version not persisted: %+v", got.Files[fileV4])
	}
	m.Close()
	if !stub.closed {
		t.Fatal("Close must release the reader")
	}
	if m.Ready() {
		t.Fatal("Close must clear readiness")
	}
}

func TestManagerFinishError(t *testing.T) {
	m := NewManager(t.TempDir())
	defer m.Close()
	m.finish(nil, fmt.Errorf("boom"))
	if m.Ready() {
		t.Fatal("finish with error must not be ready")
	}
	if m.lastError == nil {
		t.Fatal("finish with error must record lastError")
	}
	if m.Status("ip2region").Error == nil {
		t.Fatal("status must surface the error")
	}
}

func TestManagerEnsureOfflineDegrades(t *testing.T) {
	offline(t)
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(blob(4096)) })
	dir := t.TempDir()
	m := NewManager(dir)
	defer m.Close()
	// Garbage bytes download fine but fail to open as a real mmdb: the manager
	// must degrade to not-ready with an error, never panic or block.
	m.Ensure("mmdb", srv.URL+"/", false)
	if !util.Exists(filepath.Join(dir, fileMMDB)) {
		t.Fatal("metadb not downloaded")
	}
	if m.Ready() {
		t.Fatal("a corrupt database must not be ready")
	}
	st := m.Status("mmdb")
	if st.Downloading {
		t.Fatal("downloading must be false after Ensure returns")
	}
	if st.Error == nil {
		t.Fatal("expected an open error to be recorded")
	}
}

func TestManagerEnsureSkipsExisting(t *testing.T) {
	offline(t)
	var hits int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write(blob(4096))
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileMMDB), blob(4096), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir)
	defer m.Close()
	m.Ensure("mmdb", srv.URL+"/", false)
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("existing file must not be re-downloaded, hits=%d", hits)
	}
}

func TestManagerEnsureForceRedownloads(t *testing.T) {
	offline(t)
	var hits int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write(blob(4096))
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileMMDB), blob(4096), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir)
	defer m.Close()
	m.Ensure("mmdb", srv.URL+"/", true)
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("force must re-download, hits=%d", hits)
	}
}

func TestManagerEnsureCoalesce(t *testing.T) {
	offline(t)
	release := make(chan struct{})
	var hits int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		<-release
		_, _ = w.Write(blob(4096))
	})
	dir := t.TempDir()
	m := NewManager(dir)
	defer m.Close()
	done := make(chan struct{})
	go func() { m.Ensure("mmdb", srv.URL+"/", false); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for !m.Status("mmdb").Downloading && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// A concurrent Ensure while downloading must coalesce (no extra fetch).
	m.Ensure("mmdb", srv.URL+"/", false)
	close(release)
	<-done
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("coalesced Ensure must download once, hits=%d", hits)
	}
}

func headServer(t *testing.T, etag string, size int64) *httptest.Server {
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		if etag != "" {
			w.Header().Set("ETag", etag)
		}
		if size > 0 {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestCheckUpdateEtagChanged(t *testing.T) {
	offline(t)
	// HEAD 未给 Content-Length（size 未知）时回退到 ETag 比较，ETag 变化即新版本。
	srv := headServer(t, `"remote-v2"`, 0)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileMMDB), blob(4096), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir)
	defer m.Close()
	m.state.Source = "mmdb"
	m.ready = true
	m.state.Files[fileMMDB] = FileMeta{ETag: "remote-v1", Size: 4096}

	res := m.CheckUpdate(context.Background(), "mmdb", srv.URL+"/")
	if !res.UpdateAvailable {
		t.Fatal("an ETag change must flag an update when size is unknown")
	}
	if res.LastCheckAt == 0 {
		t.Fatal("lastCheckAt must be set")
	}
	if len(res.Files) != 1 || res.Files[0].Name != fileMMDB {
		t.Fatalf("files = %+v", res.Files)
	}
	if res.Files[0].Remote.ETag != `"remote-v2"` || res.Files[0].Local.ETag != "remote-v1" {
		t.Fatalf("remote/local pairing wrong: %+v", res.Files[0])
	}
	if m.state.LastCheckAt == nil || !m.state.UpdateAvailable {
		t.Fatal("check result must persist to state")
	}
}

func TestCheckUpdateNotReadyNoFalsePositive(t *testing.T) {
	offline(t)
	srv := headServer(t, `"x"`, 4096)
	m := NewManager(t.TempDir())
	defer m.Close()
	// 首次开启、下载尚未完成（未就绪）时不得判“有新版本”——缺文件≠版本过旧。
	res := m.CheckUpdate(context.Background(), "mmdb", srv.URL+"/")
	if res.UpdateAvailable {
		t.Fatal("a not-ready manager must not claim an update")
	}
	if res.LastCheckAt == 0 {
		t.Fatal("lastCheckAt must be set")
	}
	if m.state.UpdateAvailable {
		t.Fatal("not-ready check must not persist an update flag")
	}
}

func TestCheckUpdateReadyMissingFile(t *testing.T) {
	offline(t)
	srv := headServer(t, `"x"`, 4096)
	m := NewManager(t.TempDir())
	defer m.Close()
	m.state.Source = "mmdb"
	m.ready = true // 就绪却被外部删除文件：提示需要重新下载
	if !m.CheckUpdate(context.Background(), "mmdb", srv.URL+"/").UpdateAvailable {
		t.Fatal("a ready manager with a missing file must flag an update")
	}
}

func TestCheckUpdateNoChange(t *testing.T) {
	offline(t)
	srv := headServer(t, `"same"`, 4096)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileMMDB), blob(4096), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir)
	defer m.Close()
	m.state.Source = "mmdb"
	m.ready = true
	m.state.Files[fileMMDB] = FileMeta{ETag: `"same"`, Size: 4096}
	res := m.CheckUpdate(context.Background(), "mmdb", srv.URL+"/")
	if res.UpdateAvailable {
		t.Fatal("identical fingerprint must not flag an update")
	}
}

func TestCheckUpdateStrippedHeaders(t *testing.T) {
	offline(t)
	srv := headServer(t, "", 0)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileMMDB), blob(4096), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir)
	defer m.Close()
	m.state.Source = "mmdb"
	m.ready = true
	m.state.Files[fileMMDB] = FileMeta{ETag: "local", Size: 4096}
	res := m.CheckUpdate(context.Background(), "mmdb", srv.URL+"/")
	if res.UpdateAvailable {
		t.Fatal("stripped remote headers (unknown) must not claim an update when local exists")
	}
}

func TestCheckUpdateSameSizeNoFalsePositive(t *testing.T) {
	offline(t)
	// 本地指纹来自镜像（ETag 与直连不同），但文件尺寸一致 —— 同一版本，不得误报。
	srv := headServer(t, `"direct-v1"`, 4096)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileMMDB), blob(4096), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir)
	defer m.Close()
	m.state.Source = "mmdb"
	m.ready = true
	m.state.Files[fileMMDB] = FileMeta{ETag: "mirror-v1", Size: 4096}
	if m.CheckUpdate(context.Background(), "mmdb", srv.URL+"/").UpdateAvailable {
		t.Fatal("same-size different-etag (different upstreams) must not flag an update")
	}
}

func TestCheckUpdateSizeChanged(t *testing.T) {
	offline(t)
	srv := headServer(t, "", 8192)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileMMDB), blob(4096), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir)
	defer m.Close()
	m.state.Source = "mmdb"
	m.ready = true
	m.state.Files[fileMMDB] = FileMeta{Size: 4096}
	if !m.CheckUpdate(context.Background(), "mmdb", srv.URL+"/").UpdateAvailable {
		t.Fatal("a size change must flag an update")
	}
}
