package geo

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func datBytes(n int) []byte {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	return buf
}

// setBuiltins swaps the non-custom mirror chain for the duration of a test so
// Provision never touches the real internet (nil = offline).
func setBuiltins(t *testing.T, fn func(string) []string) {
	t.Helper()
	old := geoBuiltinURLs
	geoBuiltinURLs = fn
	t.Cleanup(func() { geoBuiltinURLs = old })
}

func offline(t *testing.T) { setBuiltins(t, func(string) []string { return nil }) }

func geoServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestFileURL(t *testing.T) {
	if got := fileURL("", "geoip.dat"); got != geoGithubBase+"geoip.dat" {
		t.Fatalf("no mirror: %q", got)
	}
	want := "https://m.example.com/" + geoGithubBase + "geoip.dat"
	if got := fileURL("https://m.example.com", "geoip.dat"); got != want {
		t.Fatalf("custom (no slash): %q want %q", got, want)
	}
	if got := fileURL("https://m.example.com/", "geoip.dat"); got != want {
		t.Fatalf("custom (slash): %q want %q", got, want)
	}
	if got := fileURL("  ", "geoip.dat"); got != geoGithubBase+"geoip.dat" {
		t.Fatalf("blank mirror: %q", got)
	}
}

func TestCandidateURLs(t *testing.T) {
	urls := candidateURLs("https://mirror.example.com", "geosite.dat")
	if len(urls) == 0 {
		t.Fatal("empty chain")
	}
	if !strings.HasPrefix(urls[0], "https://mirror.example.com/") {
		t.Fatalf("custom mirror must be first: %v", urls)
	}
	if urls[len(urls)-1] != geoGithubBase+"geosite.dat" {
		t.Fatalf("direct github must be last: %v", urls)
	}
	seen := map[string]bool{}
	for _, u := range urls {
		if seen[u] {
			t.Fatalf("duplicate url %q in %v", u, urls)
		}
		seen[u] = true
	}
	// custom equal to a builtin prefix => deduped (no extra entry).
	withDupe := candidateURLs("https://ghfast.top/", "geosite.dat")
	plain := candidateURLs("", "geosite.dat")
	if len(withDupe) != len(plain) {
		t.Fatalf("custom matching builtin should dedupe: %d vs %d", len(withDupe), len(plain))
	}
}

func TestProvisionDownloadsBoth(t *testing.T) {
	offline(t)
	var mu sync.Mutex
	var hits []string
	srv := geoServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, path.Base(r.URL.Path))
		mu.Unlock()
		_, _ = w.Write(datBytes(4096))
	})
	dir := t.TempDir()
	present := Provision(context.Background(), dir, Needed{GeoIP: true, GeoSite: true}, srv.URL+"/")
	if present != (Present{GeoIP: true, GeoSite: true}) {
		t.Fatalf("present = %+v", present)
	}
	for _, f := range []string{"geoip.dat", "geosite.dat"} {
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil || st.Size() < minDatSize {
			t.Fatalf("%s not installed: %v", f, err)
		}
		if st.Mode().Perm() != 0o644 {
			t.Fatalf("%s mode = %o want 644", f, st.Mode().Perm())
		}
	}
	// no leftover staging files
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover tmp file: %s", e.Name())
		}
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 fetches, got %v", hits)
	}
}

func TestProvisionSkipsPresent(t *testing.T) {
	offline(t)
	dir := t.TempDir()
	writeDat(t, filepath.Join(dir, "geoip.dat"), 4096)
	var mu sync.Mutex
	var hits []string
	srv := geoServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, path.Base(r.URL.Path))
		mu.Unlock()
		_, _ = w.Write(datBytes(4096))
	})
	present := Provision(context.Background(), dir, Needed{GeoIP: true, GeoSite: true}, srv.URL+"/")
	if present != (Present{GeoIP: true, GeoSite: true}) {
		t.Fatalf("present = %+v", present)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hits) != 1 || hits[0] != "geosite.dat" {
		t.Fatalf("only geosite.dat should be fetched, got %v", hits)
	}
}

func TestProvisionNotNeededNoNetwork(t *testing.T) {
	offline(t)
	srv := geoServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("network must not be used when nothing is needed")
	})
	dir := t.TempDir()
	present := Provision(context.Background(), dir, Needed{}, srv.URL+"/")
	if present != (Present{}) {
		t.Fatalf("present = %+v", present)
	}
}

func TestProvisionRejectsHTML(t *testing.T) {
	offline(t)
	html := bytes.Repeat([]byte("<html><body>404 not found</body></html>\n"), 200)
	srv := geoServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(html) })
	dir := t.TempDir()
	present := Provision(context.Background(), dir, Needed{GeoSite: true}, srv.URL+"/")
	if present.GeoSite {
		t.Fatal("HTML error page must not count as a valid dat")
	}
	if _, err := os.Stat(filepath.Join(dir, "geosite.dat")); !os.IsNotExist(err) {
		t.Fatalf("geosite.dat should be absent, err=%v", err)
	}
}

func TestProvisionRejectsTiny(t *testing.T) {
	offline(t)
	srv := geoServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(datBytes(16)) })
	dir := t.TempDir()
	present := Provision(context.Background(), dir, Needed{GeoSite: true}, srv.URL+"/")
	if present.GeoSite {
		t.Fatal("tiny file must not count as a valid dat")
	}
}

func TestProvisionMirrorFallback(t *testing.T) {
	bad := geoServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	good := geoServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(datBytes(4096)) })
	setBuiltins(t, func(f string) []string { return []string{good.URL + "/" + f} })
	dir := t.TempDir()
	present := Provision(context.Background(), dir, Needed{GeoSite: true}, bad.URL+"/")
	if !present.GeoSite {
		t.Fatal("should fall back to the builtin mirror after custom 500")
	}
}

func TestProvisionAllMirrorsFail(t *testing.T) {
	offline(t)
	srv := geoServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	dir := t.TempDir()
	present := Provision(context.Background(), dir, Needed{GeoIP: true, GeoSite: true}, srv.URL+"/")
	if present != (Present{}) {
		t.Fatalf("all mirrors fail => nothing present, got %+v", present)
	}
	// never fatal: dir still usable, no dat installed
	if _, err := os.Stat(filepath.Join(dir, "geosite.dat")); !os.IsNotExist(err) {
		t.Fatalf("geosite.dat should be absent, err=%v", err)
	}
}

func TestProvisionContextCancel(t *testing.T) {
	offline(t)
	srv := geoServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("cancelled context must not fetch")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	present := Provision(ctx, dir, Needed{GeoSite: true}, srv.URL+"/")
	if present.GeoSite {
		t.Fatal("cancelled provision should not install anything")
	}
}
