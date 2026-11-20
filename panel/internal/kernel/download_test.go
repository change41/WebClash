package kernel

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"webclash/internal/config"
)

func fakePayload(n int) []byte {
	r := rand.New(rand.NewSource(7))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Intn(256))
	}
	return b
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipBytes(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: name, Method: zip.Store}
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// setBuiltins swaps the public mirror chain for local prefixes (default empty)
// so tests never touch the real internet, restoring it on cleanup.
func setBuiltins(t *testing.T, prefixes ...string) {
	t.Helper()
	old := builtinMirrorPrefixes
	builtinMirrorPrefixes = prefixes
	t.Cleanup(func() { builtinMirrorPrefixes = old })
}

func strptr(s string) *string { return &s }

func TestNormalizeTag(t *testing.T) {
	cases := []struct {
		in   string
		want *string
	}{
		{"", nil},
		{"latest", nil},
		{"  latest  ", nil},
		{"1.19.34", strptr("v1.19.34")},
		{"v1.19.34", strptr("v1.19.34")},
		{"  v1.2.3  ", strptr("v1.2.3")},
		{"alpha", strptr("alpha")},
	}
	for _, c := range cases {
		got := normalizeTag(c.in)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Fatalf("normalizeTag(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestReleaseAsset(t *testing.T) {
	name, err := releaseAsset("v1.19.34")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "mihomo-") || !strings.HasSuffix(name, "-v1.19.34.gz") {
		t.Fatalf("unexpected asset %q", name)
	}
	if runtime.GOOS == "linux" && !strings.Contains(name, "linux") {
		t.Fatalf("expected linux asset, got %q", name)
	}
}

func TestAssertTag(t *testing.T) {
	for _, ok := range []string{"v1.19.34", "v1.2.3-alpha.1", "v10"} {
		if err := assertTag(ok); err != nil {
			t.Fatalf("assertTag(%q) unexpected err %v", ok, err)
		}
	}
	for _, bad := range []string{"1.19.34", "latest", "v1/../x", ""} {
		if err := assertTag(bad); err == nil {
			t.Fatalf("assertTag(%q) expected error", bad)
		}
	}
}

func TestMirrorPrefixes(t *testing.T) {
	got := mirrorPrefixes("https://my.mirror/")
	want := []string{"https://my.mirror/", "", "https://ghfast.top/", "https://gh-proxy.com/", "https://ghproxy.net/"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v want %v", got, want)
	}
	got2 := mirrorPrefixes("https://ghfast.top")
	if got2[0] != "https://ghfast.top/" || len(got2) != 4 {
		t.Fatalf("dedupe failed: %v", got2)
	}
	got3 := mirrorPrefixes("  ")
	if len(got3) != 4 || got3[0] != "" {
		t.Fatalf("empty mirror: %v", got3)
	}
}

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	payload := fakePayload(2048)

	gz := filepath.Join(dir, "a.gz")
	os.WriteFile(gz, gzipBytes(t, payload), 0o644)
	if out, err := extractBinary(gz); err != nil || !bytes.Equal(out, payload) {
		t.Fatalf("gzip extract: err=%v equal=%v", err, bytes.Equal(out, payload))
	}

	zp := filepath.Join(dir, "a.zip")
	os.WriteFile(zp, zipBytes(t, "mihomo", payload), 0o644)
	if out, err := extractBinary(zp); err != nil || !bytes.Equal(out, payload) {
		t.Fatalf("zip extract: err=%v equal=%v", err, bytes.Equal(out, payload))
	}

	zp2 := filepath.Join(dir, "b.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w1, _ := zw.Create("readme.txt")
	w1.Write([]byte("decoy"))
	w2, _ := zw.Create("sub/mihomo.exe")
	w2.Write(payload)
	zw.Close()
	os.WriteFile(zp2, buf.Bytes(), 0o644)
	if out, err := extractBinary(zp2); err != nil || !bytes.Equal(out, payload) {
		t.Fatalf("zip nested select: err=%v equal=%v", err, bytes.Equal(out, payload))
	}

	raw := filepath.Join(dir, "raw")
	os.WriteFile(raw, payload, 0o644)
	if out, err := extractBinary(raw); err != nil || !bytes.Equal(out, payload) {
		t.Fatalf("raw passthrough: err=%v", err)
	}
}

type mirrorServer struct {
	archive   []byte
	failFirst bool
	slow      bool
	htmlTag   string
	headTag   string
	srv       *httptest.Server
	mu        sync.Mutex
	hits      int
}

func newMirrorServer(t *testing.T, archive []byte) *mirrorServer {
	m := &mirrorServer{archive: archive}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		m.mu.Lock()
		m.hits++
		n := m.hits
		m.mu.Unlock()
		switch {
		case strings.Contains(p, "/releases/latest"):
			if r.Method == http.MethodHead && m.headTag != "" {
				w.Header().Set("Location", m.srv.URL+"/https://github.com/MetaCubeX/mihomo/releases/tag/"+m.headTag)
				w.WriteHeader(http.StatusFound)
				return
			}
			if m.htmlTag != "" {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, `<a href="/MetaCubeX/mihomo/releases/tag/%s">x</a>`, m.htmlTag)
				return
			}
			http.Error(w, "no tag", http.StatusNotFound)
		case strings.Contains(p, "/releases/download/"):
			if m.failFirst && n == 1 {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			if m.slow {
				w.Header().Set("Content-Length", fmt.Sprint(len(m.archive)))
				w.WriteHeader(http.StatusOK)
				fl, _ := w.(http.Flusher)
				chunk := 64 * 1024
				for off := 0; off < len(m.archive); off += chunk {
					if r.Context().Err() != nil {
						return
					}
					end := off + chunk
					if end > len(m.archive) {
						end = len(m.archive)
					}
					w.Write(m.archive[off:end])
					if fl != nil {
						fl.Flush()
					}
					time.Sleep(20 * time.Millisecond)
				}
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(m.archive)))
			w.Write(m.archive)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mirrorServer) prefix() string { return m.srv.URL + "/" }

func TestFetchToFileGuards(t *testing.T) {
	payload := fakePayload(2 * 1024 * 1024)
	m := newMirrorServer(t, payload)
	dir := t.TempDir()
	client := &http.Client{}

	dst := filepath.Join(dir, "ok")
	url := m.prefix() + downloadRoot + "/v1.2.3/mihomo-linux-amd64-v1.2.3.gz"
	var gotTotal int64
	if err := fetchToFile(context.Background(), client, url, dst, func(r, tot int64) { gotTotal = tot }); err != nil {
		t.Fatalf("fetchToFile: %v", err)
	}
	if b, _ := os.ReadFile(dst); !bytes.Equal(b, payload) {
		t.Fatal("content mismatch")
	}
	if gotTotal != int64(len(payload)) {
		t.Fatalf("total=%d want %d", gotTotal, len(payload))
	}

	small := newMirrorServer(t, []byte("tiny"))
	dst2 := filepath.Join(dir, "small")
	err := fetchToFile(context.Background(), client, small.prefix()+downloadRoot+"/v1.2.3/a.gz", dst2, nil)
	if err == nil || err.Error() != "下载内容异常（文件过小），可能被网络拦截" {
		t.Fatalf("small guard: %v", err)
	}
}

func TestDownloadKernelGzip(t *testing.T) {
	setBuiltins(t)
	payload := fakePayload(2 * 1024 * 1024)
	m := newMirrorServer(t, gzipBytes(t, payload))
	dir := t.TempDir()
	dest := filepath.Join(dir, "bin", "mihomo")
	var stages []string
	tag, err := downloadKernel(context.Background(), downloadOptions{
		version: "v1.19.34", mirror: m.prefix(), dest: dest, tmpDir: filepath.Join(dir, "tmp"),
		onStage: func(s string) { stages = append(stages, s) },
	})
	if err != nil {
		t.Fatalf("downloadKernel: %v", err)
	}
	if tag != "v1.19.34" {
		t.Fatalf("tag=%q", tag)
	}
	if b, _ := os.ReadFile(dest); !bytes.Equal(b, payload) {
		t.Fatal("installed binary mismatch")
	}
	if st, _ := os.Stat(dest); st.Mode().Perm() != 0o755 {
		t.Fatalf("mode=%v", st.Mode().Perm())
	}
	if strings.Join(stages, ",") != "resolving,downloading,extracting" {
		t.Fatalf("stages=%v", stages)
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "tmp"))
	if len(ents) != 0 {
		t.Fatalf("tmp not cleaned: %v", ents)
	}
}

func TestDownloadKernelZipAndRaw(t *testing.T) {
	setBuiltins(t)
	payload := fakePayload(2 * 1024 * 1024)
	for _, kind := range []string{"zip", "raw"} {
		var archive []byte
		if kind == "zip" {
			archive = zipBytes(t, "mihomo", payload)
		} else {
			archive = payload
		}
		m := newMirrorServer(t, archive)
		dir := t.TempDir()
		dest := filepath.Join(dir, "mihomo")
		tag, err := downloadKernel(context.Background(), downloadOptions{version: "1.2.3", mirror: m.prefix(), dest: dest, tmpDir: filepath.Join(dir, "tmp")})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if tag != "v1.2.3" {
			t.Fatalf("%s tag=%q", kind, tag)
		}
		if b, _ := os.ReadFile(dest); !bytes.Equal(b, payload) {
			t.Fatalf("%s content mismatch", kind)
		}
	}
}

func TestDownloadKernelMirrorFallback(t *testing.T) {
	payload := fakePayload(2 * 1024 * 1024)
	bad := newMirrorServer(t, nil)
	bad.failFirst = true // always 500 on its single hit
	good := newMirrorServer(t, payload)
	setBuiltins(t, good.prefix()) // chain: bad custom -> good builtin
	dir := t.TempDir()
	dest := filepath.Join(dir, "mihomo")
	tag, err := downloadKernel(context.Background(), downloadOptions{version: "v1.2.3", mirror: bad.prefix(), dest: dest, tmpDir: filepath.Join(dir, "tmp")})
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if tag != "v1.2.3" {
		t.Fatalf("tag=%q", tag)
	}
	if b, _ := os.ReadFile(dest); !bytes.Equal(b, payload) {
		t.Fatal("content mismatch after fallback")
	}
}

func TestResolveLatestTag(t *testing.T) {
	setBuiltins(t)
	payload := fakePayload(2 * 1024 * 1024)
	mHead := newMirrorServer(t, payload)
	mHead.headTag = "v9.9.9"
	tag, err := resolveLatestTag(context.Background(), &http.Client{}, mHead.prefix())
	if err != nil || tag != "v9.9.9" {
		t.Fatalf("HEAD resolve: tag=%q err=%v", tag, err)
	}
	mHTML := newMirrorServer(t, payload)
	mHTML.htmlTag = "v8.8.8"
	tag2, err2 := resolveLatestTag(context.Background(), &http.Client{}, mHTML.prefix())
	if err2 != nil || tag2 != "v8.8.8" {
		t.Fatalf("GET resolve: tag=%q err=%v", tag2, err2)
	}
}

func TestDownloadKernelLatest(t *testing.T) {
	setBuiltins(t)
	payload := fakePayload(2 * 1024 * 1024)
	m := newMirrorServer(t, payload)
	m.headTag = "v7.7.7"
	dir := t.TempDir()
	dest := filepath.Join(dir, "mihomo")
	tag, err := downloadKernel(context.Background(), downloadOptions{version: "latest", mirror: m.prefix(), dest: dest, tmpDir: filepath.Join(dir, "tmp")})
	if err != nil || tag != "v7.7.7" {
		t.Fatalf("latest: tag=%q err=%v", tag, err)
	}
}

func TestDownloadKernelCancel(t *testing.T) {
	setBuiltins(t)
	payload := fakePayload(4 * 1024 * 1024)
	m := newMirrorServer(t, payload)
	m.slow = true
	dir := t.TempDir()
	dest := filepath.Join(dir, "mihomo")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(120 * time.Millisecond); cancel() }()
	_, err := downloadKernel(ctx, downloadOptions{version: "v1.2.3", mirror: m.prefix(), dest: dest, tmpDir: filepath.Join(dir, "tmp")})
	if err == nil {
		t.Fatal("expected cancel error")
	}
	if _, serr := os.Stat(dest); serr == nil {
		t.Fatal("dest should not exist after cancel")
	}
}

func downloadTestManager(t *testing.T, mirror string) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m := New(Options{
		DataDir: dir, KernelDir: filepath.Join(dir, "kernel"), ConfigPath: filepath.Join(dir, "kernel", "config.yaml"),
		GetSettings: func() config.Settings {
			s := config.DefaultSettings()
			s.Kernel.Mirror = mirror
			return s
		},
	})
	return m, dir
}

func TestManagerDownloadSuccess(t *testing.T) {
	setBuiltins(t)
	payload := fakePayload(2 * 1024 * 1024)
	m := newMirrorServer(t, payload)
	mgr, _ := downloadTestManager(t, m.prefix())
	if err := mgr.Download("v1.19.34"); err != nil {
		t.Fatalf("Download: %v", err)
	}
	st := mgr.Status()
	if st.Download.Active || st.Download.Progress != 100 || st.Download.Stage != nil {
		t.Fatalf("download state: %+v", st.Download)
	}
	if st.Download.Version == nil || *st.Download.Version != "v1.19.34" {
		t.Fatalf("version: %v", st.Download.Version)
	}
	if b, _ := os.ReadFile(mgr.ManagedPath()); !bytes.Equal(b, payload) {
		t.Fatal("binary mismatch")
	}
	if !st.Binary.Exists || !st.Binary.Managed {
		t.Fatalf("binary status: %+v", st.Binary)
	}
}

func TestManagerDownloadError(t *testing.T) {
	setBuiltins(t)
	m := newMirrorServer(t, []byte("tiny")) // <1MiB guard fails on the only mirror
	mgr, _ := downloadTestManager(t, m.prefix())
	err := mgr.Download("v1.2.3")
	if err == nil {
		t.Fatal("expected error")
	}
	st := mgr.Status()
	if st.Download.Active || st.Download.Error == nil {
		t.Fatalf("state: %+v", st.Download)
	}
	if st.LastError == nil || !strings.HasPrefix(*st.LastError, "内核下载失败：") {
		t.Fatalf("lastError: %v", st.LastError)
	}
}

func TestManagerDownloadActiveReject(t *testing.T) {
	setBuiltins(t)
	payload := fakePayload(4 * 1024 * 1024)
	m := newMirrorServer(t, payload)
	m.slow = true
	mgr, _ := downloadTestManager(t, m.prefix())
	done := make(chan error, 1)
	go func() { done <- mgr.Download("v1.2.3") }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.Status().Download.Active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !mgr.Status().Download.Active {
		t.Fatal("download never became active")
	}
	if err := mgr.Download("v1.2.3"); err == nil || err.Error() != "已有内核下载任务正在进行" {
		t.Fatalf("active reject: %v", err)
	}
	mgr.CancelDownload()
	<-done
}

func TestManagerCancelDownload(t *testing.T) {
	setBuiltins(t)
	payload := fakePayload(4 * 1024 * 1024)
	m := newMirrorServer(t, payload)
	m.slow = true
	mgr, _ := downloadTestManager(t, m.prefix())
	done := make(chan error, 1)
	go func() { done <- mgr.Download("v1.2.3") }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.Status().Download.Active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := mgr.CancelDownload(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	err := <-done
	if err == nil {
		t.Fatal("expected cancelled download to error")
	}
	st := mgr.Status()
	if st.Download.Active || st.Download.Stage != nil {
		t.Fatalf("post-cancel state: %+v", st.Download)
	}
}
