package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"webclash/internal/webui"
)

// serveFile writes one embedded asset with the exact static-embedded.ts headers,
// honoring 304 negotiation and HEAD (port of registerEmbeddedStatic handler).
func serveFile(w http.ResponseWriter, r *http.Request, file *webui.File, buildTime int64) {
	h := w.Header()
	h.Set("Content-Type", file.Mime)
	h.Set("Content-Length", strconv.FormatInt(file.Size, 10))
	h.Set("ETag", "\""+file.ETag+"\"")
	h.Set("Last-Modified", time.UnixMilli(buildTime).UTC().Format(http.TimeFormat))
	if file.Immutable {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	h.Set("Accept-Ranges", "none")

	if notModified(r, file.ETag, buildTime) {
		h.Del("Content-Length")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(file.Data)
	}
}

func matchesEtag(header, etag string) bool {
	if strings.TrimSpace(header) == "*" {
		return true
	}
	target := strings.TrimPrefix(etag, "W/")
	for _, part := range strings.Split(header, ",") {
		p := strings.TrimSpace(part)
		p = strings.TrimPrefix(p, "W/")
		p = strings.TrimPrefix(p, "\"")
		p = strings.TrimSuffix(p, "\"")
		if p == target {
			return true
		}
	}
	return false
}

func notModified(r *http.Request, etag string, buildTime int64) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		return matchesEtag(inm, etag)
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if t, err := http.ParseTime(ims); err == nil {
			return buildTime/1000 <= t.UnixMilli()/1000
		}
	}
	return false
}

// serveStatic resolves the request path against the embedded table, falling back
// to a directory index.html. Returns false when no asset matched (→ notFound).
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) bool {
	if s.table == nil {
		return false
	}
	rawURL := r.RequestURI
	if rawURL == "" {
		rawURL = "/"
	}
	rel, ok := webui.NormalizeAssetPath(rawURL)
	if !ok {
		return false
	}
	file := s.table.Files[rel]
	if file == nil {
		pathname := strings.SplitN(rawURL, "?", 2)[0]
		if strings.HasSuffix(pathname, "/") {
			file = s.table.Files[rel+"/index.html"]
		}
	}
	if file == nil {
		return false
	}
	serveFile(w, r, file, s.table.BuildTime)
	return true
}

// notFound ports app.ts setNotFoundHandler: /api/* → 404 JSON, else SPA index.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.RequestURI, "/api/") {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "接口不存在"})
		return
	}
	if s.table != nil {
		serveFile(w, r, s.table.Index, s.table.BuildTime)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "前端未构建，请执行 npm run build"})
}
