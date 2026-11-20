package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func sampleConn(id, host string) HistoryConnection {
	return HistoryConnection{
		ID:          id,
		Upload:      100,
		Download:    200,
		Start:       "2026-01-01T00:00:00.000Z",
		Chains:      []string{"DIRECT"},
		Rule:        "DomainSuffix",
		RulePayload: host,
		Metadata: HistoryMetadata{
			Network:           "tcp",
			Type:              "HTTP",
			SourceIP:          "127.0.0.1",
			DestinationIP:     "198.18.0.5",
			SourcePort:        "5000",
			DestinationPort:   "443",
			Host:              host,
			DNSMode:           "fake-ip",
			RemoteDestination: "1.1.1.1",
		},
	}
}

func snapshotJSON(conns []HistoryConnection) string {
	b, _ := json.Marshal(map[string]any{"connections": conns})
	return string(b)
}

func TestConnHistorySnapshotNewestFirstAndLimit(t *testing.T) {
	limit := 3
	h := newConnHistory(func() int { return limit })
	h.Observe([]HistoryConnection{
		sampleConn("a", "a.com"), sampleConn("b", "b.com"), sampleConn("c", "c.com"),
	}, 1000)
	h.Observe([]HistoryConnection{sampleConn("a", "a.com"), sampleConn("c", "c.com")}, 2000)
	h.Observe([]HistoryConnection{sampleConn("a", "a.com")}, 3000)

	got := h.Snapshot(0)
	if len(got) != 2 || got[0].ID != "c" || got[1].ID != "b" {
		t.Fatalf("closed = %+v, want [c b] newest first", got)
	}
	if got[0].ClosedAt != 3000 || got[1].ClosedAt != 2000 {
		t.Errorf("closedAt = %d/%d, want 3000/2000", got[0].ClosedAt, got[1].ClosedAt)
	}
	if got[1].LastSeen != 1000 {
		t.Errorf("lastSeen = %d, want 1000", got[1].LastSeen)
	}
	if got[1].Metadata.Host != "b.com" || got[1].Rule != "DomainSuffix" {
		t.Errorf("metadata/rule not preserved: %+v", got[1])
	}

	if page := h.Snapshot(1); len(page) != 1 || page[0].ID != "c" {
		t.Errorf("page cap = %+v, want only c", page)
	}

	limit = 1
	if got := h.Snapshot(0); len(got) != 1 || got[0].ID != "c" {
		t.Errorf("after limit shrink = %+v, want only c", got)
	}
}

func TestConnHistoryRingEvictsOldest(t *testing.T) {
	h := newConnHistory(func() int { return 2 })
	h.Observe([]HistoryConnection{sampleConn("a", "a"), sampleConn("b", "b"), sampleConn("c", "c")}, 1000)
	h.Observe([]HistoryConnection{sampleConn("a", "a")}, 2000)
	h.Observe([]HistoryConnection{}, 3000)

	got := h.Snapshot(0)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	for _, conn := range got {
		if conn.ID == "b" {
			t.Errorf("oldest record b should be evicted: %+v", got)
		}
	}
}

func TestConnHistoryTickClosesVanished(t *testing.T) {
	var mu sync.Mutex
	body := snapshotJSON([]HistoryConnection{sampleConn("a", "a.com"), sampleConn("b", "b.com")})
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		b, f := body, fail
		mu.Unlock()
		if f {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(b))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	c := New(t.TempDir())
	c.mu.Lock()
	c.settings.Controller.Port = port
	c.settings.Controller.Secret = ""
	c.mu.Unlock()

	c.historyTick()
	if got := c.ConnHistory(10); len(got) != 0 {
		t.Fatalf("closed after first snapshot = %+v, want none", got)
	}

	mu.Lock()
	fail = true
	mu.Unlock()
	c.historyTick()
	if got := c.ConnHistory(10); len(got) != 0 {
		t.Fatalf("fetch failure closed connections: %+v", got)
	}

	mu.Lock()
	fail = false
	body = snapshotJSON([]HistoryConnection{sampleConn("a", "a.com")})
	mu.Unlock()
	c.historyTick()
	got := c.ConnHistory(10)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("closed = %+v, want [b]", got)
	}
	if got[0].ClosedAt == 0 || got[0].LastSeen == 0 {
		t.Errorf("markers not set: %+v", got[0])
	}
	if got[0].Metadata.Host != "b.com" || got[0].Metadata.DestinationPort != "443" {
		t.Errorf("metadata lost: %+v", got[0])
	}
	if got[0].Metadata.RemoteDestination != "1.1.1.1" || got[0].Metadata.DNSMode != "fake-ip" {
		t.Errorf("fake-ip metadata lost: %+v", got[0].Metadata)
	}
}

func TestHistoryTickFeedsTrafficTotals(t *testing.T) {
	var mu sync.Mutex
	upTotal, downTotal := int64(0), int64(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		b, err := json.Marshal(map[string]any{
			"uploadTotal":   upTotal,
			"downloadTotal": downTotal,
			"connections":   []HistoryConnection{},
		})
		mu.Unlock()
		if err != nil {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	c := New(t.TempDir())
	c.mu.Lock()
	c.settings.Controller.Port = port
	c.settings.Controller.Secret = ""
	c.mu.Unlock()

	c.historyTick() // baseline only
	mu.Lock()
	upTotal, downTotal = 60_000, 120_000
	mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	c.historyTick()

	hist := c.TrafficHistory()
	if hist.BucketSeconds != 60 || hist.WindowSeconds != 86400 {
		t.Errorf("window = %d/%d, want 60/86400", hist.BucketSeconds, hist.WindowSeconds)
	}
	if len(hist.Points) == 0 {
		t.Fatalf("no traffic buckets recorded from /connections totals")
	}
	var up, down float64
	for _, p := range hist.Points {
		if p.T%60_000 != 0 {
			t.Errorf("bucket t = %d, want minute-aligned", p.T)
		}
		up += p.Up
		down += p.Down
	}
	if up <= 0 || down <= 0 {
		t.Errorf("traffic totals not fed: up=%v down=%v", up, down)
	}
}

func TestConnHistoryStopWithoutStart(t *testing.T) {
	h := newConnHistory(func() int { return 1 })
	h.Stop()
	h.Stop()
}

func TestTruncateText(t *testing.T) {
	if got := truncateText(strings.Repeat("x", 300), connHistoryTextMax); len(got) != connHistoryTextMax {
		t.Errorf("len = %d, want %d", len(got), connHistoryTextMax)
	}
	if got := truncateText("短", connHistoryTextMax); got != "短" {
		t.Errorf("short string changed: %q", got)
	}
	if got := truncateText(strings.Repeat("中", 300), 10); got != strings.Repeat("中", 10) {
		t.Errorf("rune truncation = %q", got)
	}
}
