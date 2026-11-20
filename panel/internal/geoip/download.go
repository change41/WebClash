package geoip

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Genuine upstream sources (task-aligned, no temporary proxies). ip2region xdb
// lives in the official lionsoul2014/ip2region repo; geoip.metadb is the same
// MetaCubeX release asset the geo package already mirrors. Filenames are fixed
// constants — never derived from request input — so dir joins stay internal.
const (
	ip2regionRawBase = "https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/"
	ip2regionGhBase  = "https://github.com/lionsoul2014/ip2region/raw/master/data/"
	mmdbGhBase       = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/"
	downloadUA       = "WebClash/0.1.0 (geoip-downloader)"
	minFileSize      = 1024

	fileV4   = "ip2region_v4.xdb"
	fileV6   = "ip2region_v6.xdb"
	fileMMDB = "geoip.metadb"
)

// fileSpec is one database file plus its ordered, de-duplicated mirror chain.
type fileSpec struct {
	name    string
	urls    []string
	minSize int64
}

func ensureSlash(s string) string {
	if strings.HasSuffix(s, "/") {
		return s
	}
	return s + "/"
}

// sourceFiles returns the files a source needs. ip2region downloads BOTH v4 and
// v6 xdb (per requirement); mmdb downloads the single country-level metadb.
func sourceFiles(source string) []string {
	if source == "mmdb" {
		return []string{fileMMDB}
	}
	return []string{fileV4, fileV6}
}

// canonicalURL is the direct upstream URL for a file; custom mirrors prepend to
// it. Kept independent of the builtin chain so a custom mirror still resolves
// when the chain is swapped out in offline tests.
func canonicalURL(source, name string) string {
	if source == "mmdb" {
		return mmdbGhBase + name
	}
	return ip2regionRawBase + name
}

// geoipBuiltinURLs is the non-custom mirror chain for a file: gh proxies →
// (jsdelivr for mmdb only) → direct upstream. jsdelivr is skipped for ip2region
// because the v6 xdb (~37MB) exceeds its 20MB cap. The direct upstream is last so
// HEAD checks (which iterate in reverse) hit the reliable ETag source first.
// Swappable in tests to stay offline (mirrors geo.geoBuiltinURLs).
var geoipBuiltinURLs = func(source, name string) []string {
	canonical := canonicalURL(source, name)
	if source == "mmdb" {
		return []string{
			"https://ghfast.top/" + canonical,
			"https://gh-proxy.com/" + canonical,
			"https://ghproxy.net/" + canonical,
			"https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/" + name,
			canonical,
		}
	}
	gh := ip2regionGhBase + name
	return []string{
		"https://ghfast.top/" + gh,
		"https://gh-proxy.com/" + gh,
		"https://ghproxy.net/" + gh,
		canonical,
	}
}

// candidateURLs builds the mirror chain for a file: custom mirror → builtin
// chain, de-duplicated, custom first and direct upstream last.
func candidateURLs(source, name, mirror string) []string {
	var urls []string
	if custom := strings.TrimSpace(mirror); custom != "" {
		urls = append(urls, ensureSlash(custom)+canonicalURL(source, name))
	}
	urls = append(urls, geoipBuiltinURLs(source, name)...)
	seen := map[string]bool{}
	out := urls[:0]
	for _, u := range urls {
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

func specs(source, mirror string) []fileSpec {
	var out []fileSpec
	for _, name := range sourceFiles(source) {
		out = append(out, fileSpec{name: name, urls: candidateURLs(source, name, mirror), minSize: minFileSize})
	}
	return out
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 8 * time.Second}).DialContext},
		Timeout:   60 * time.Second,
	}
}

// downloadFile fetches a file via the mirror chain and installs it atomically,
// returning its fingerprint. It NEVER panics or blocks indefinitely: on any
// failure the existing file (if any) is left untouched and ok=false.
func downloadFile(ctx context.Context, client *http.Client, dir string, spec fileSpec) (FileMeta, bool) {
	tmp := filepath.Join(dir, fmt.Sprintf("%s.%d.tmp", spec.name, os.Getpid()))
	defer os.Remove(tmp)
	dest := filepath.Join(dir, spec.name)
	for _, url := range spec.urls {
		if ctx.Err() != nil {
			return FileMeta{}, false
		}
		meta, ok := fetchFile(ctx, client, url, tmp)
		if ok && validateFile(tmp, spec.minSize) {
			_ = os.Chmod(tmp, 0o644)
			if os.Rename(tmp, dest) == nil {
				if st, err := os.Stat(dest); err == nil {
					meta.Size = st.Size()
				}
				meta.UpdatedAt = time.Now().UnixMilli()
				return meta, true
			}
		}
		_ = os.Remove(tmp)
	}
	return FileMeta{}, false
}

func fetchFile(ctx context.Context, client *http.Client, url, tmp string) (FileMeta, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return FileMeta{}, false
	}
	req.Header.Set("User-Agent", downloadUA)
	res, err := client.Do(req)
	if err != nil {
		return FileMeta{}, false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return FileMeta{}, false
	}
	f, err := os.Create(tmp)
	if err != nil {
		return FileMeta{}, false
	}
	_, copyErr := io.Copy(f, res.Body)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return FileMeta{}, false
	}
	return FileMeta{
		ETag:         res.Header.Get("ETag"),
		LastModified: res.Header.Get("Last-Modified"),
	}, true
}

// validateFile rejects truncated downloads and HTML error pages (same guard the
// geo package uses). The reader does full format verification on open.
func validateFile(tmp string, minSize int64) bool {
	st, err := os.Stat(tmp)
	if err != nil || st.Size() < minSize {
		return false
	}
	f, err := os.Open(tmp)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 16)
	n, _ := f.Read(head)
	return !strings.HasPrefix(strings.TrimSpace(string(head[:n])), "<")
}

// headFile probes the remote fingerprint, preferring the direct upstream (last
// candidate) for a reliable ETag/Last-Modified. Mirrors that strip headers yield
// an empty meta (unknown), which the caller treats as "no change detected".
func headFile(ctx context.Context, client *http.Client, spec fileSpec) (FileMeta, bool) {
	for i := len(spec.urls) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			return FileMeta{}, false
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, spec.urls[i], nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", downloadUA)
		res, err := client.Do(req)
		if err != nil {
			continue
		}
		_ = res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			continue
		}
		size := res.ContentLength
		if size < 0 {
			size = 0
		}
		return FileMeta{
			ETag:         res.Header.Get("ETag"),
			LastModified: res.Header.Get("Last-Modified"),
			Size:         size,
		}, true
	}
	return FileMeta{}, false
}

// metaChanged reports whether a remote fingerprint differs from the local one.
// Size (Content-Length) is upstream-independent — the same file has the same
// length via any mirror — so when both sizes are known it is the reliable
// cross-mirror signal: equal size means the same version, and a mirror echoing a
// different ETag/Last-Modified than the direct upstream can no longer cause a
// false "new version" right after a fresh download. ETag/Last-Modified are only
// consulted when a size is unknown (a HEAD that omits Content-Length).
func metaChanged(remote, local FileMeta) bool {
	if remote.Size > 0 && local.Size > 0 {
		return remote.Size != local.Size
	}
	if remote.ETag != "" && local.ETag != "" && remote.ETag != local.ETag {
		return true
	}
	if remote.LastModified != "" && local.LastModified != "" && remote.LastModified != local.LastModified {
		return true
	}
	return false
}
