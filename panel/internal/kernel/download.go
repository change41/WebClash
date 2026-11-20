package kernel

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Port of server/src/kernel/download.ts: mirror chain, latest-tag resolution,
// streaming download with a 1MiB sanity guard, gzip/zip/raw extraction and
// atomic install. Error strings are 1:1 contracts.

const (
	downloadUserAgent = "WebClash/0.1.0 (mihomo-downloader)"
	releasePage       = "https://github.com/MetaCubeX/mihomo/releases/latest"
	downloadRoot      = "https://github.com/MetaCubeX/mihomo/releases/download"
)

var (
	tagFromURLRe   = regexp.MustCompile(`/tag/(v[\w.-]+)`)
	tagFromHTMLRe  = regexp.MustCompile(`/releases/tag/(v\d[\w.-]*)`)
	assertTagRe    = regexp.MustCompile(`^v[\w.-]+$`)
	leadingDigitRe = regexp.MustCompile(`^\d`)
	assetSanitize  = regexp.MustCompile(`[^\w.-]`)
	zipEntryRe     = regexp.MustCompile(`(?i)(^|/)mihomo(\.exe)?$`)
)

// builtinMirrorPrefixes is the public fallback chain (direct + gh proxies). It
// is a var so tests can substitute local servers; production uses these values.
var builtinMirrorPrefixes = []string{"", "https://ghfast.top/", "https://gh-proxy.com/", "https://ghproxy.net/"}

// mirrorPrefixes builds the ordered, de-duplicated mirror list (port of
// mirrorPrefixes): custom mirror first (trailing slash ensured), then direct,
// then the public gh proxies.
func mirrorPrefixes(mirror string) []string {
	var list []string
	custom := strings.TrimSpace(mirror)
	if custom != "" {
		if strings.HasSuffix(custom, "/") {
			list = append(list, custom)
		} else {
			list = append(list, custom+"/")
		}
	}
	list = append(list, builtinMirrorPrefixes...)
	seen := map[string]bool{}
	out := list[:0]
	for _, p := range list {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// normalizeTag returns nil for ""/"latest", else prefixes a leading digit with
// "v" (port of normalizeTag).
func normalizeTag(version string) *string {
	value := strings.TrimSpace(version)
	if value == "" || value == "latest" {
		return nil
	}
	if leadingDigitRe.MatchString(value) {
		v := "v" + value
		return &v
	}
	return &value
}

// releaseAsset maps the current OS/arch to the mihomo release asset name (port
// of releaseAsset). Go runtime values are mapped to the same asset tokens Node
// derives from process.platform/process.arch.
func releaseAsset(tag string) (string, error) {
	osMap := map[string]string{
		"darwin": "darwin", "linux": "linux", "windows": "windows",
		"freebsd": "freebsd", "openbsd": "openbsd",
	}
	archMap := map[string]string{
		"amd64": "amd64", "arm64": "arm64", "arm": "armv7", "386": "386",
		"ppc64le": "ppc64le", "ppc64": "ppc64le", "s390x": "s390x", "riscv64": "riscv64",
	}
	osName, ok := osMap[runtime.GOOS]
	if !ok {
		return "", fmt.Errorf("暂不支持当前操作系统：%s", runtime.GOOS)
	}
	archName, ok := archMap[runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("暂不支持当前架构：%s", runtime.GOARCH)
	}
	ext := "gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("mihomo-%s-%s-%s.%s", osName, archName, tag, ext), nil
}

func assertTag(tag string) error {
	if !assertTagRe.MatchString(tag) {
		return fmt.Errorf("非法的版本号：%s", tag)
	}
	return nil
}

// resolveLatestTag follows releases/latest redirects (HEAD then GET fallback)
// across the mirror chain (port of resolveLatestTag).
func resolveLatestTag(ctx context.Context, client *http.Client, mirror string) (string, error) {
	for _, prefix := range mirrorPrefixes(mirror) {
		url := prefix + releasePage
		headCtx, cancelHead := context.WithTimeout(ctx, 15000*time.Millisecond)
		req, err := http.NewRequestWithContext(headCtx, http.MethodHead, url, nil)
		if err == nil {
			req.Header.Set("User-Agent", downloadUserAgent)
			if res, derr := client.Do(req); derr == nil {
				final := res.Request.URL.String()
				_ = res.Body.Close()
				if m := tagFromURLRe.FindStringSubmatch(final); m != nil {
					cancelHead()
					return m[1], nil
				}
			}
		}
		cancelHead()

		getCtx, cancelGet := context.WithTimeout(ctx, 20000*time.Millisecond)
		greq, gerr := http.NewRequestWithContext(getCtx, http.MethodGet, url, nil)
		if gerr == nil {
			greq.Header.Set("User-Agent", downloadUserAgent)
			if res, derr := client.Do(greq); derr == nil {
				html, _ := io.ReadAll(res.Body)
				_ = res.Body.Close()
				if m := tagFromHTMLRe.FindStringSubmatch(string(html)); m != nil {
					cancelGet()
					return m[1], nil
				}
			}
		}
		cancelGet()
	}
	return "", errors.New("无法获取 mihomo 最新版本，请在设置中填写具体版本号（如 v1.19.34）或配置下载镜像")
}

// UpdateCheck compares the installed binary with the configured target version.
type UpdateCheck struct {
	Current   *string `json:"current"`
	Latest    string  `json:"latest"`
	HasUpdate bool    `json:"hasUpdate"`
}

// CheckUpdate resolves the target tag without downloading: a concrete
// configured version is used as-is, ""/"latest" goes through the mirror chain.
// HasUpdate is false only when the installed binary reports the same tag.
func (m *Manager) CheckUpdate(version string) (UpdateCheck, error) {
	m.mu.Lock()
	busy := m.downloadState.Active
	m.mu.Unlock()
	if busy {
		return UpdateCheck{}, errors.New("已有内核下载任务正在进行")
	}

	target := normalizeTag(version)
	if target == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		tag, err := resolveLatestTag(ctx, &http.Client{}, m.opts.GetSettings().Kernel.Mirror)
		if err != nil {
			return UpdateCheck{}, err
		}
		target = &tag
	}
	if err := assertTag(*target); err != nil {
		return UpdateCheck{}, err
	}

	var current *string
	if bin := m.binaryPath(); bin != "" {
		current = m.readVersion(bin)
	}
	return UpdateCheck{
		Current:   current,
		Latest:    *target,
		HasUpdate: current == nil || *current != *target,
	}, nil
}

// fetchToFile streams url to tmpFile, reporting progress, and rejects payloads
// under 1MiB (port of fetchToFile).
func fetchToFile(ctx context.Context, client *http.Client, url, tmpFile string, onProgress func(int64, int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", downloadUserAgent)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var total int64
	if cl := res.Header.Get("Content-Length"); cl != "" {
		if n, perr := strconv.ParseInt(cl, 10, 64); perr == nil {
			total = n
		}
	}
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	var received int64
	var streamErr error
	buf := make([]byte, 64*1024)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				streamErr = werr
				break
			}
			received += int64(n)
			if onProgress != nil {
				onProgress(received, total)
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				streamErr = rerr
			}
			break
		}
	}
	closeErr := f.Close()
	if streamErr != nil {
		return streamErr
	}
	if closeErr != nil {
		return closeErr
	}
	if received < 1024*1024 {
		return errors.New("下载内容异常（文件过小），可能被网络拦截")
	}
	return nil
}

// extractBinary gunzips, unzips (picking the mihomo entry) or passes through a
// raw binary (port of extractBinary).
func extractBinary(archive string) ([]byte, error) {
	data, err := os.ReadFile(archive)
	if err != nil {
		return nil, err
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, zerr := gzip.NewReader(bytes.NewReader(data))
		if zerr != nil {
			return nil, zerr
		}
		defer zr.Close()
		return io.ReadAll(zr)
	}
	if len(data) >= 2 && data[0] == 0x50 && data[1] == 0x4b {
		zr, zerr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if zerr != nil {
			return nil, zerr
		}
		var files []*zip.File
		for _, f := range zr.File {
			if !f.FileInfo().IsDir() {
				files = append(files, f)
			}
		}
		var entry *zip.File
		for _, f := range files {
			if zipEntryRe.MatchString(f.Name) {
				entry = f
				break
			}
		}
		if entry == nil && len(files) > 0 {
			entry = files[0]
		}
		if entry == nil {
			return nil, errors.New("压缩包中没有找到 mihomo 可执行文件")
		}
		rc, oerr := entry.Open()
		if oerr != nil {
			return nil, oerr
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return data, nil
}

type downloadOptions struct {
	version    string
	mirror     string
	dest       string
	tmpDir     string
	onProgress func(received, total int64)
	onStage    func(stage string)
}

// downloadKernel resolves the tag, downloads via the mirror chain, extracts and
// atomically installs to dest, returning the actual tag (port of downloadKernel).
func downloadKernel(ctx context.Context, o downloadOptions) (string, error) {
	client := &http.Client{}
	if o.onStage != nil {
		o.onStage("resolving")
	}
	var tag string
	if nt := normalizeTag(o.version); nt != nil {
		tag = *nt
	} else {
		t, err := resolveLatestTag(ctx, client, o.mirror)
		if err != nil {
			return "", err
		}
		tag = t
	}
	if err := assertTag(tag); err != nil {
		return "", err
	}
	asset, err := releaseAsset(tag)
	if err != nil {
		return "", err
	}
	source := downloadRoot + "/" + tag + "/" + asset
	if err := os.MkdirAll(o.tmpDir, 0o755); err != nil {
		return "", err
	}
	archive := filepath.Join(o.tmpDir, assetSanitize.ReplaceAllString(asset, "_"))

	if o.onStage != nil {
		o.onStage("downloading")
	}
	var lastError error
	for _, prefix := range mirrorPrefixes(o.mirror) {
		url := prefix + source
		ferr := fetchToFile(ctx, client, url, archive, o.onProgress)
		if ferr == nil {
			lastError = nil
			break
		}
		lastError = ferr
		if ctx.Err() != nil {
			return "", ferr
		}
		_ = os.Remove(archive)
	}
	if lastError != nil {
		return "", fmt.Errorf("下载内核失败：%s", lastError.Error())
	}

	if o.onStage != nil {
		o.onStage("extracting")
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return "", err
	}
	if len(binary) < 1024*1024 {
		return "", errors.New("解压后的内核文件异常")
	}
	if err := os.MkdirAll(filepath.Dir(o.dest), 0o755); err != nil {
		return "", err
	}
	staging := fmt.Sprintf("%s.%d.new", o.dest, os.Getpid())
	if err := os.WriteFile(staging, binary, 0o755); err != nil {
		return "", err
	}
	_ = os.Chmod(staging, 0o755)
	if err := os.Rename(staging, o.dest); err != nil {
		return "", err
	}
	_ = os.Remove(archive)
	return tag, nil
}
