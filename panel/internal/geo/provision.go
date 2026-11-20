package geo

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	geoGithubBase = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/"
	geoUserAgent  = "WebClash/0.1.0 (geo-downloader)"
)

func ensureSlash(s string) string {
	if strings.HasSuffix(s, "/") {
		return s
	}
	return s + "/"
}

// fileURL is the (mirror-prefixed) canonical URL used for geox-url. With
// geo-auto-update:false mihomo never fetches it; it is informational.
func fileURL(mirror, file string) string {
	custom := strings.TrimSpace(mirror)
	if custom != "" {
		return ensureSlash(custom) + geoGithubBase + file
	}
	return geoGithubBase + file
}

// geoBuiltinURLs is the non-custom mirror chain for a file: gh proxies →
// jsdelivr → direct GitHub. Extracted as a var so tests can run fully offline
// (nil) or point at a local fallback server without touching the internet.
var geoBuiltinURLs = func(file string) []string {
	gh := geoGithubBase + file
	return []string{
		"https://ghfast.top/" + gh,
		"https://gh-proxy.com/" + gh,
		"https://ghproxy.net/" + gh,
		"https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/" + file,
		gh,
	}
}

// candidateURLs is the ordered, de-duplicated mirror chain for a geo file:
// custom → gh proxies → jsdelivr → direct GitHub.
func candidateURLs(mirror, file string) []string {
	var urls []string
	if custom := strings.TrimSpace(mirror); custom != "" {
		urls = append(urls, ensureSlash(custom)+geoGithubBase+file)
	}
	urls = append(urls, geoBuiltinURLs(file)...)
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

// Provision downloads the needed-but-missing geo files into kernelDir via the
// mirror chain, bounded by ctx. It NEVER returns a fatal error: on any failure
// the file simply stays absent and Apply degrades the config. Files are fetched
// concurrently and installed atomically.
func Provision(ctx context.Context, kernelDir string, needed Needed, mirror string) Present {
	present := CheckFiles(kernelDir)
	var files []string
	if needed.GeoSite && !present.GeoSite {
		files = append(files, "geosite.dat")
	}
	if needed.GeoIP && !present.GeoIP {
		files = append(files, "geoip.dat")
	}
	if len(files) == 0 {
		return present
	}
	client := &http.Client{
		Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 8 * time.Second}).DialContext},
		Timeout:   15 * time.Second,
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, f := range files {
		wg.Add(1)
		go func(file string) {
			defer wg.Done()
			if downloadDat(ctx, client, kernelDir, file, mirror) {
				mu.Lock()
				if file == "geoip.dat" {
					present.GeoIP = true
				} else {
					present.GeoSite = true
				}
				mu.Unlock()
			}
		}(f)
	}
	wg.Wait()
	return present
}

func downloadDat(ctx context.Context, client *http.Client, kernelDir, file, mirror string) bool {
	tmp := filepath.Join(kernelDir, fmt.Sprintf("%s.%d.tmp", file, os.Getpid()))
	defer os.Remove(tmp)
	dest := filepath.Join(kernelDir, file)
	for _, url := range candidateURLs(mirror, file) {
		if ctx.Err() != nil {
			return false
		}
		if fetchDat(ctx, client, url, tmp) && validateDat(tmp) {
			_ = os.Chmod(tmp, 0o644)
			if os.Rename(tmp, dest) == nil {
				return true
			}
		}
		_ = os.Remove(tmp)
	}
	return false
}

func fetchDat(ctx context.Context, client *http.Client, url, tmp string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", geoUserAgent)
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return false
	}
	f, err := os.Create(tmp)
	if err != nil {
		return false
	}
	_, copyErr := io.Copy(f, res.Body)
	closeErr := f.Close()
	return copyErr == nil && closeErr == nil
}

// validateDat rejects truncated downloads and HTML error pages.
func validateDat(tmp string) bool {
	st, err := os.Stat(tmp)
	if err != nil || st.Size() < minDatSize {
		return false
	}
	f, err := os.Open(tmp)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 16)
	n, _ := f.Read(head)
	if strings.HasPrefix(strings.TrimSpace(string(head[:n])), "<") {
		return false
	}
	return true
}
