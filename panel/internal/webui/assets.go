package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"webclash/internal/buildinfo"
)

// File is one embedded static asset.
type File struct {
	Path      string
	Mime      string
	Size      int64
	ETag      string
	Immutable bool
	Data      []byte
}

// Table is the resolved embedded asset manifest.
type Table struct {
	Files     map[string]*File
	Index     *File
	BuildTime int64
}

var mimeTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json; charset=utf-8",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".gif":         "image/gif",
	".webp":        "image/webp",
	".avif":        "image/avif",
	".ico":         "image/x-icon",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".ttf":         "font/ttf",
	".otf":         "font/otf",
	".txt":         "text/plain; charset=utf-8",
	".xml":         "application/xml; charset=utf-8",
	".wasm":        "application/wasm",
	".map":         "application/json; charset=utf-8",
	".webmanifest": "application/manifest+json; charset=utf-8",
}

// MimeFor maps a filename extension to its Content-Type.
func MimeFor(file string) string {
	dot := strings.LastIndexByte(file, '.')
	if dot < 0 {
		return "application/octet-stream"
	}
	if m, ok := mimeTypes[strings.ToLower(file[dot:])]; ok {
		return m
	}
	return "application/octet-stream"
}

var immutableRe = regexp.MustCompile(`^assets/.+-[A-Za-z0-9_-]{8,}\.[a-z0-9]+$`)

// NormalizeAssetPath normalizes a raw URL path to an asset-relative path.
// Returns ok=false on traversal, bad encoding, NUL or backslash (port of
// normalizeAssetPath). An empty path resolves to index.html.
func NormalizeAssetPath(rawURL string) (string, bool) {
	pathname := rawURL
	if i := strings.IndexByte(pathname, '?'); i >= 0 {
		pathname = pathname[:i]
	}
	if i := strings.IndexByte(pathname, '#'); i >= 0 {
		pathname = pathname[:i]
	}
	decoded, err := url.PathUnescape(pathname)
	if err != nil {
		return "", false
	}
	if strings.ContainsRune(decoded, 0) || strings.ContainsRune(decoded, '\\') {
		return "", false
	}
	segments := make([]string, 0, 8)
	for _, seg := range strings.Split(decoded, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			return "", false
		}
		segments = append(segments, seg)
	}
	if len(segments) == 0 {
		return "index.html", true
	}
	return strings.Join(segments, "/"), true
}

// Load walks the embedded dist tree and builds the asset table. Returns nil if
// index.html is missing.
func Load() *Table {
	buildTime := time.Now().UnixMilli()
	if buildinfo.BuildTime != "" {
		if v, err := strconv.ParseInt(buildinfo.BuildTime, 10, 64); err == nil {
			buildTime = v
		}
	}
	files := make(map[string]*File)
	_ = fs.WalkDir(distFS, "dist", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, "dist/")
		norm, ok := NormalizeAssetPath("/" + rel)
		if !ok || norm != rel {
			return nil
		}
		data, err := distFS.ReadFile(p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		files[rel] = &File{
			Path:      rel,
			Mime:      MimeFor(rel),
			Size:      int64(len(data)),
			ETag:      hex.EncodeToString(sum[:])[:16],
			Immutable: immutableRe.MatchString(rel),
			Data:      data,
		}
		return nil
	})
	index := files["index.html"]
	if index == nil {
		return nil
	}
	return &Table{Files: files, Index: index, BuildTime: buildTime}
}
