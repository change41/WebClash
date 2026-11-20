package api

import (
	"net/http"
	"testing"
)

func TestIsWebSocketUpgrade(t *testing.T) {
	cases := []struct {
		name       string
		upgrade    string
		connection string
		want       bool
	}{
		{"plain", "websocket", "upgrade", true},
		{"case-insensitive", "WebSocket", "Upgrade", true},
		{"multi-token", "websocket", "keep-alive, Upgrade", true},
		{"multi-token-spaces", "websocket", "keep-alive ,  upgrade ", true},
		{"no-upgrade-token", "websocket", "keep-alive", false},
		{"missing-upgrade-header", "", "upgrade", false},
		{"missing-connection-header", "websocket", "", false},
		{"wrong-protocol", "h2c", "upgrade", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &http.Request{Header: http.Header{}}
			if c.upgrade != "" {
				r.Header.Set("Upgrade", c.upgrade)
			}
			if c.connection != "" {
				r.Header.Set("Connection", c.connection)
			}
			if got := isWebSocketUpgrade(r); got != c.want {
				t.Fatalf("isWebSocketUpgrade(%q,%q)=%v want %v", c.upgrade, c.connection, got, c.want)
			}
		})
	}
}

func TestWsClampInt(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", 500},        // Number("")=0 → clamp min
		{"abc", 1000},    // NaN → fallback
		{"2000", 2000},   // in range
		{"100", 500},     // below min
		{"99999", 5000},  // above max
		{"1500.6", 1501}, // Math.round up
		{"1500.4", 1500}, // Math.round down
		{"  2500  ", 2500},
		{"-5", 500},
		{"Infinity", 1000}, // Inf → fallback
		{"1e3", 1000},
		{"500", 500},   // exactly min
		{"5000", 5000}, // exactly max
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			if got := wsClampInt(c.raw, 1000, 500, 5000); got != c.want {
				t.Fatalf("wsClampInt(%q)=%d want %d", c.raw, got, c.want)
			}
		})
	}
}

func TestWsUpstreamPath(t *testing.T) {
	q := func(kv map[string]string) map[string][]string {
		out := map[string][]string{}
		for k, v := range kv {
			out[k] = []string{v}
		}
		return out
	}
	cases := []struct {
		name string
		ch   string
		q    map[string][]string
		want string
	}{
		{"traffic", "traffic", nil, "/traffic"},
		{"memory", "memory", nil, "/memory"},
		{"connections-default", "connections", nil, "/connections?interval=500"},
		{"connections-interval", "connections", q(map[string]string{"interval": "2000"}), "/connections?interval=2000"},
		{"connections-garbage", "connections", q(map[string]string{"interval": "abc"}), "/connections?interval=1000"},
		{"connections-high", "connections", q(map[string]string{"interval": "99999"}), "/connections?interval=5000"},
		{"connections-empty", "connections", q(map[string]string{"interval": ""}), "/connections?interval=500"},
		{"logs-default", "logs", nil, "/logs?level=info"},
		{"logs-debug", "logs", q(map[string]string{"level": "debug"}), "/logs?level=debug"},
		{"logs-warning", "logs", q(map[string]string{"level": "warning"}), "/logs?level=warning"},
		{"logs-error", "logs", q(map[string]string{"level": "error"}), "/logs?level=error"},
		{"logs-bogus", "logs", q(map[string]string{"level": "bogus"}), "/logs?level=info"},
		{"logs-empty", "logs", q(map[string]string{"level": ""}), "/logs?level=info"},
		{"unknown", "foo", nil, ""},
		{"empty-name", "", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wsUpstreamPath(c.ch, c.q); got != c.want {
				t.Fatalf("wsUpstreamPath(%q,%v)=%q want %q", c.ch, c.q, got, c.want)
			}
		})
	}
}
