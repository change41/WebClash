// Package api ports server/src/app.ts + routes: the HTTP server, auth gate,
// error envelope, JSON responses and static/SPA serving.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"webclash/internal/mihomo"
	"webclash/internal/util"
)

const jsonContentType = "application/json; charset=utf-8"

// writeJSON sends a compact JSON body (no HTML escaping) with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := util.MarshalJSON(v)
	if err != nil {
		http.Error(w, "序列化失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// writeError maps an error to the Fastify-style {error} envelope (port of
// app.ts setErrorHandler): HttpError→its status, body-too-large→413, else 500.
func writeError(w http.ResponseWriter, err error) {
	var he *util.HttpError
	if errors.As(err, &he) {
		writeJSON(w, he.StatusCode, map[string]any{"error": he.Message})
		return
	}
	var ce *mihomo.ClashError
	if errors.As(err, &ce) {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": ce.Message})
		return
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "请求体过大"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": util.ErrorMessage(err)})
}

// readJSONBody parses the request body as JSON. An empty body yields (nil,nil);
// invalid JSON yields an error. The body is already size-limited by middleware.
func readJSONBody(r *http.Request) (any, error) {
	if r.Body == nil {
		return nil, nil
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, util.NewHttpError(http.StatusBadRequest, "请求体不是合法 JSON")
	}
	return v, nil
}

// clientIP resolves the caller IP, honoring X-Forwarded-For leftmost (trustProxy).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		if first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isHTTPS reports whether the request is HTTPS (direct TLS or trusted proxy).
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
