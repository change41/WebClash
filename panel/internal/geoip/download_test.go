package geoip

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func blob(n int) []byte {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	return buf
}

// setBuiltin swaps the non-custom mirror chain for the duration of a test so the
// downloader never touches the real internet (nil = offline).
func setBuiltin(t *testing.T, fn func(source, name string) []string) {
	t.Helper()
	old := geoipBuiltinURLs
	geoipBuiltinURLs = fn
	t.Cleanup(func() { geoipBuiltinURLs = old })
}

func offline(t *testing.T) { setBuiltin(t, func(string, string) []string { return nil }) }

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestSourceFiles(t *testing.T) {
	if got := sourceFiles("ip2region"); len(got) != 2 || got[0] != fileV4 || got[1] != fileV6 {
		t.Fatalf("ip2region must need v4+v6, got %v", got)
	}
	if got := sourceFiles("mmdb"); len(got) != 1 || got[0] != fileMMDB {
		t.Fatalf("mmdb must need metadb only, got %v", got)
	}
}

func TestCandidateURLs(t *testing.T) {
	urls := candidateURLs("ip2region", fileV4, "https://mirror.example.com")
	if len(urls) == 0 {
		t.Fatal("empty chain")
	}
	if !strings.HasPrefix(urls[0], "https://mirror.example.com/") {
		t.Fatalf("custom mirror must be first: %v", urls)
	}
	if urls[len(urls)-1] != ip2regionRawBase+fileV4 {
		t.Fatalf("direct upstream must be last: %v", urls)
	}
	seen := map[string]bool{}
	for _, u := range urls {
		if seen[u] {
			t.Fatalf("duplicate url %q in %v", u, urls)
		}
		seen[u] = true
		if strings.Contains(u, "jsdelivr") {
			t.Fatalf("ip2region must not use jsdelivr (v6 exceeds its cap): %q", u)
		}
	}

	murls := candidateURLs("mmdb", fileMMDB, "")
	hasJsdelivr := false
	for _, u := range murls {
		if strings.Contains(u, "jsdelivr") {
			hasJsdelivr = true
		}
	}
	if !hasJsdelivr {
		t.Fatalf("mmdb chain should include jsdelivr: %v", murls)
	}
	if murls[len(murls)-1] != mmdbGhBase+fileMMDB {
		t.Fatalf("mmdb direct upstream must be last: %v", murls)
	}

	// custom equal to a builtin prefix => deduped (mmdb custom prepends canonical,
	// which is exactly ghfast's builtin entry).
	withDupe := candidateURLs("mmdb", fileMMDB, "https://ghfast.top/")
	plain := candidateURLs("mmdb", fileMMDB, "")
	if len(withDupe) != len(plain) {
		t.Fatalf("custom matching builtin should dedupe: %d vs %d", len(withDupe), len(plain))
	}
}

func TestSpecs(t *testing.T) {
	sp := specs("ip2region", "")
	if len(sp) != 2 {
		t.Fatalf("ip2region specs = %d, want 2", len(sp))
	}
	for _, s := range sp {
		if s.minSize != minFileSize {
			t.Fatalf("%s minSize = %d", s.name, s.minSize)
		}
		if len(s.urls) == 0 {
			t.Fatalf("%s has no urls", s.name)
		}
	}
}

func TestValidateFile(t *testing.T) {
	dir := t.TempDir()
	tiny := filepath.Join(dir, "tiny")
	_ = os.WriteFile(tiny, blob(16), 0o644)
	if validateFile(tiny, minFileSize) {
		t.Fatal("tiny file must fail")
	}
	html := filepath.Join(dir, "html")
	_ = os.WriteFile(html, bytes.Repeat([]byte("<html><body>404</body></html>\n"), 200), 0o644)
	if validateFile(html, minFileSize) {
		t.Fatal("HTML error page must fail")
	}
	ok := filepath.Join(dir, "ok")
	_ = os.WriteFile(ok, blob(4096), 0o644)
	if !validateFile(ok, minFileSize) {
		t.Fatal("valid binary must pass")
	}
	if validateFile(filepath.Join(dir, "missing"), minFileSize) {
		t.Fatal("missing file must fail")
	}
}

func TestMetaChanged(t *testing.T) {
	cases := []struct {
		name          string
		remote, local FileMeta
		want          bool
	}{
		{"etag differ", FileMeta{ETag: "a"}, FileMeta{ETag: "b"}, true},
		{"etag same", FileMeta{ETag: "a"}, FileMeta{ETag: "a"}, false},
		{"remote unknown", FileMeta{}, FileMeta{ETag: "b", Size: 10}, false},
		{"local unknown", FileMeta{ETag: "a"}, FileMeta{}, false},
		{"lastmod differ", FileMeta{LastModified: "x"}, FileMeta{LastModified: "y"}, true},
		{"size differ", FileMeta{Size: 10}, FileMeta{Size: 20}, true},
		{"remote size zero", FileMeta{Size: 0}, FileMeta{Size: 20}, false},
		{"all same", FileMeta{ETag: "a", Size: 10}, FileMeta{ETag: "a", Size: 10}, false},
		// 镜像与直连 ETag 不同但为同一文件：尺寸一致即同版本，不得误报。
		{"same size diff etag", FileMeta{ETag: "mirror", Size: 10}, FileMeta{ETag: "direct", Size: 10}, false},
		{"size differ wins", FileMeta{ETag: "same", Size: 20}, FileMeta{ETag: "same", Size: 10}, true},
	}
	for _, c := range cases {
		if got := metaChanged(c.remote, c.local); got != c.want {
			t.Fatalf("%s: metaChanged = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDownloadFileAtomic(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
		_, _ = w.Write(blob(4096))
	})
	dir := t.TempDir()
	spec := fileSpec{name: fileV4, urls: []string{srv.URL + "/" + fileV4}, minSize: minFileSize}
	meta, ok := downloadFile(context.Background(), newHTTPClient(), dir, spec)
	if !ok {
		t.Fatal("download failed")
	}
	if meta.ETag != `"abc"` || meta.LastModified == "" {
		t.Fatalf("fingerprint not captured: %+v", meta)
	}
	if meta.Size != 4096 || meta.UpdatedAt == 0 {
		t.Fatalf("size/updatedAt not set: %+v", meta)
	}
	st, err := os.Stat(filepath.Join(dir, fileV4))
	if err != nil {
		t.Fatalf("file not installed: %v", err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o, want 644", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover tmp file: %s", e.Name())
		}
	}
}

func TestDownloadFileFallback(t *testing.T) {
	bad := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	good := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(blob(4096)) })
	dir := t.TempDir()
	spec := fileSpec{name: fileV4, urls: []string{bad.URL + "/" + fileV4, good.URL + "/" + fileV4}, minSize: minFileSize}
	if _, ok := downloadFile(context.Background(), newHTTPClient(), dir, spec); !ok {
		t.Fatal("should fall back to the second mirror")
	}
	if _, err := os.Stat(filepath.Join(dir, fileV4)); err != nil {
		t.Fatalf("file not installed after fallback: %v", err)
	}
}

func TestDownloadFileRejectsBadBody(t *testing.T) {
	for _, body := range [][]byte{
		bytes.Repeat([]byte("<html>404</html>\n"), 200),
		blob(16),
	} {
		srv := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) })
		dir := t.TempDir()
		spec := fileSpec{name: fileV4, urls: []string{srv.URL + "/" + fileV4}, minSize: minFileSize}
		if _, ok := downloadFile(context.Background(), newHTTPClient(), dir, spec); ok {
			t.Fatal("invalid body must not install")
		}
		if _, err := os.Stat(filepath.Join(dir, fileV4)); !os.IsNotExist(err) {
			t.Fatalf("file should be absent, err=%v", err)
		}
	}
}

func TestDownloadFileCancel(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("cancelled context must not fetch")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	spec := fileSpec{name: fileV4, urls: []string{srv.URL + "/" + fileV4}, minSize: minFileSize}
	if _, ok := downloadFile(ctx, newHTTPClient(), dir, spec); ok {
		t.Fatal("cancelled download must not install")
	}
}

func TestHeadFile(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
	})
	spec := fileSpec{name: fileV4, urls: []string{srv.URL + "/" + fileV4}}
	meta, ok := headFile(context.Background(), newHTTPClient(), spec)
	if !ok {
		t.Fatal("head failed")
	}
	if meta.ETag != `"v1"` || meta.LastModified == "" || meta.Size != 4096 {
		t.Fatalf("fingerprint not captured: %+v", meta)
	}
}

func TestHeadFilePrefersDirect(t *testing.T) {
	mirror := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"mirror"`)
		w.WriteHeader(http.StatusOK)
	})
	direct := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"direct"`)
		w.WriteHeader(http.StatusOK)
	})
	// direct is last; headFile iterates in reverse so it must be preferred.
	spec := fileSpec{name: fileV4, urls: []string{mirror.URL + "/" + fileV4, direct.URL + "/" + fileV4}}
	meta, ok := headFile(context.Background(), newHTTPClient(), spec)
	if !ok || meta.ETag != `"direct"` {
		t.Fatalf("headFile must prefer the direct upstream: ok=%v meta=%+v", ok, meta)
	}
}

func TestHeadFileStrippedHeaders(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	spec := fileSpec{name: fileV4, urls: []string{srv.URL + "/" + fileV4}}
	meta, ok := headFile(context.Background(), newHTTPClient(), spec)
	if !ok {
		t.Fatal("a 200 with stripped headers is still a successful probe")
	}
	if meta.ETag != "" || meta.LastModified != "" || meta.Size != 0 {
		t.Fatalf("stripped headers must yield unknown meta: %+v", meta)
	}
}

func TestHeadFileAllFail(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	spec := fileSpec{name: fileV4, urls: []string{srv.URL + "/" + fileV4}}
	if _, ok := headFile(context.Background(), newHTTPClient(), spec); ok {
		t.Fatal("all-404 probe must fail")
	}
}
