package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"webclash/internal/util"
)

// wsUpgrader accepts any origin (the Node ws server performs no origin check).
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var wsLogLevels = map[string]bool{"debug": true, "info": true, "warning": true, "error": true}

func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, token := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
			return true
		}
	}
	return false
}

// wsUpstreamPath maps a channel name to the controller stream path (port of
// upstreamPath). Returns "" for unknown channels.
func wsUpstreamPath(name string, q map[string][]string) string {
	get := func(key string) (string, bool) {
		if v, ok := q[key]; ok && len(v) > 0 {
			return v[0], true
		}
		return "", false
	}
	switch name {
	case "traffic":
		return "/traffic"
	case "memory":
		return "/memory"
	case "connections":
		raw, _ := get("interval")
		return fmt.Sprintf("/connections?interval=%d", wsClampInt(raw, 1000, 500, 5000))
	case "logs":
		level, ok := get("level")
		if !ok {
			level = "info"
		}
		if !wsLogLevels[level] {
			level = "info"
		}
		return "/logs?level=" + level
	default:
		return ""
	}
}

func wsClampInt(raw string, fallback, min, max int) int {
	n := util.JsNumber(raw)
	if n != n || n > 1e308 || n < -1e308 { // NaN / Inf
		return fallback
	}
	r := int(util.JsRound(n))
	if r < min {
		r = min
	}
	if r > max {
		r = max
	}
	return r
}

// wsConn tracks a bridged pair so graceful shutdown can close it.
type wsConn struct {
	closeBoth func()
}

// handleWS authenticates via the session cookie (raw 401 on failure, matching
// ws.ts) then upgrades and bridges to the controller (port of attachWebSockets).
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthenticated(r) {
		wsUnauthorized(w)
		return
	}
	client, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/ws/")
	s.bridge(client, name, r.URL.Query())
}

func wsUnauthorized(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	_, _ = conn.Write([]byte("HTTP/1.1 401 Unauthorized\r\nConnection: close\r\n\r\n"))
	_ = conn.Close()
}

func wsSendError(client *websocket.Conn, msg string) {
	_ = client.WriteJSON(map[string]any{"error": msg})
}

// bridge is a dumb read-only pipe: controller→client forwards frames; client
// messages are discarded but its close/error tears down the upstream.
func (s *Server) bridge(client *websocket.Conn, name string, q map[string][]string) {
	path := wsUpstreamPath(name, q)
	if path == "" {
		wsSendError(client, "未知的数据通道："+name)
		_ = client.Close()
		return
	}
	if !s.core.Kernel.IsRunning() {
		wsSendError(client, "内核未运行")
		_ = client.Close()
		return
	}

	upstreamURL := s.core.Kernel.Client().WSURL(path)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	upstream, _, err := dialer.Dial(upstreamURL, nil)
	if err != nil {
		wsSendError(client, "内核数据流连接失败")
		_ = client.Close()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			cancel()
			_ = client.Close()
			_ = upstream.Close()
		})
	}
	conn := &wsConn{closeBoth: closeBoth}
	s.wsConns.Store(conn, struct{}{})
	defer func() {
		s.wsConns.Delete(conn)
		closeBoth()
	}()

	// controller → client
	go func() {
		for {
			mt, data, rerr := upstream.ReadMessage()
			if rerr != nil {
				var ce *websocket.CloseError
				if !errors.As(rerr, &ce) {
					wsSendError(client, "内核数据流连接失败")
				}
				closeBoth()
				return
			}
			if werr := client.WriteMessage(mt, data); werr != nil {
				closeBoth()
				return
			}
		}
	}()

	// client read pump: discard messages, detect close/error
	go func() {
		for {
			if _, _, rerr := client.ReadMessage(); rerr != nil {
				closeBoth()
				return
			}
		}
	}()

	<-ctx.Done()
}

// CloseWebSockets tears down every active bridge (called on graceful shutdown).
func (s *Server) CloseWebSockets() {
	s.wsConns.Range(func(key, _ any) bool {
		if conn, ok := key.(*wsConn); ok {
			conn.closeBoth()
		}
		return true
	})
}
