package core

import (
	"sort"
	"sync"
	"time"

	"webclash/internal/mihomo"
)

const (
	// connHistoryInterval is the /connections snapshot cadence. A connection
	// missing from a snapshot is treated as closed with a ~1s precision.
	connHistoryInterval = time.Second
	// connHistoryFetchTimeoutMs bounds one controller fetch; below the interval
	// so slow snapshots never pile up.
	connHistoryFetchTimeoutMs = 900
	// connHistoryTextMax caps long user-controlled strings kept per record.
	connHistoryTextMax = 256
)

// HistoryMetadata is the mihomo connection metadata subset kept for debugging.
type HistoryMetadata struct {
	Network           string `json:"network"`
	Type              string `json:"type"`
	SourceIP          string `json:"sourceIP"`
	DestinationIP     string `json:"destinationIP"`
	SourcePort        string `json:"sourcePort"`
	DestinationPort   string `json:"destinationPort"`
	Host              string `json:"host"`
	DNSMode           string `json:"dnsMode"`
	RemoteDestination string `json:"remoteDestination"`
	ProcessPath       string `json:"processPath"`
}

// HistoryConnection mirrors mihomo's connection shape plus closure markers, so
// the frontend reuses the live connections table for the closed view.
type HistoryConnection struct {
	ID          string          `json:"id"`
	Upload      int64           `json:"upload"`
	Download    int64           `json:"download"`
	Start       string          `json:"start"`
	Chains      []string        `json:"chains"`
	Rule        string          `json:"rule"`
	RulePayload string          `json:"rulePayload"`
	Metadata    HistoryMetadata `json:"metadata"`
	ClosedAt    int64           `json:"closedAt"`
	LastSeen    int64           `json:"lastSeen"`
}

// connHistory keeps closed connections in a bounded in-memory ring for the
// panel process lifetime. Every tick it diffs the mihomo /connections snapshot
// by id: ids that vanished are recorded as closed. The ring is capped at the
// configured limit, so memory stays constant regardless of traffic volume.
type connHistory struct {
	limit func() int

	mu       sync.Mutex
	started  bool
	seen     map[string]HistoryConnection
	closed   []HistoryConnection
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newConnHistory(limit func() int) *connHistory {
	return &connHistory{
		limit: limit,
		seen:  map[string]HistoryConnection{},
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

// Start launches the diff ticker; it is a no-op when already started.
func (h *connHistory) Start(tick func()) {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return
	}
	h.started = true
	h.mu.Unlock()
	go func() {
		defer close(h.done)
		t := time.NewTicker(connHistoryInterval)
		defer t.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-t.C:
				tick()
			}
		}
	}()
}

// Stop terminates the ticker and waits for it to exit. Safe when never started
// and on repeated calls.
func (h *connHistory) Stop() {
	h.mu.Lock()
	started := h.started
	h.started = false
	h.mu.Unlock()
	if !started {
		return
	}
	h.stopOnce.Do(func() {
		close(h.stop)
		<-h.done
	})
}

// Observe feeds one /connections snapshot (nil closes every tracked id).
// Callers must skip Observe on fetch failures so an unreachable kernel never
// mass-closes connections.
func (h *connHistory) Observe(current []HistoryConnection, now int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	active := make(map[string]struct{}, len(current))
	for _, conn := range current {
		if conn.ID == "" {
			continue
		}
		active[conn.ID] = struct{}{}
		conn.ClosedAt = 0
		conn.LastSeen = now
		conn.RulePayload = truncateText(conn.RulePayload, connHistoryTextMax)
		conn.Metadata.ProcessPath = truncateText(conn.Metadata.ProcessPath, connHistoryTextMax)
		h.seen[conn.ID] = conn
	}
	// Map iteration order is random; sort so connections that vanish in the same
	// tick evict deterministically, oldest first.
	var vanished []HistoryConnection
	for id, conn := range h.seen {
		if _, ok := active[id]; ok {
			continue
		}
		conn.ClosedAt = now
		vanished = append(vanished, conn)
		delete(h.seen, id)
	}
	sort.Slice(vanished, func(i, j int) bool {
		if vanished[i].LastSeen != vanished[j].LastSeen {
			return vanished[i].LastSeen < vanished[j].LastSeen
		}
		return vanished[i].ID < vanished[j].ID
	})
	h.closed = append(h.closed, vanished...)
	h.trimLocked(h.limit())
}

// Snapshot returns closed records newest-first, capped at max (<=0 means the
// configured retention size).
func (h *connHistory) Snapshot(max int) []HistoryConnection {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.trimLocked(h.limit())
	n := len(h.closed)
	if max > 0 && max < n {
		n = max
	}
	out := make([]HistoryConnection, 0, n)
	for i := len(h.closed) - 1; i >= len(h.closed)-n; i-- {
		out = append(out, h.closed[i])
	}
	return out
}

func (h *connHistory) trimLocked(limit int) {
	if limit < 1 {
		limit = 1
	}
	if over := len(h.closed) - limit; over > 0 {
		h.closed = append([]HistoryConnection(nil), h.closed[over:]...)
	}
}

// truncateText caps a string at max runes (byte length short-circuits ASCII).
func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}

// historyTick fetches one controller snapshot and feeds the differ. Failures
// (kernel stopped, controller unreachable) leave the previous snapshot intact.
func (c *Core) historyTick() {
	s := c.Settings()
	var payload struct {
		DownloadTotal int64               `json:"downloadTotal"`
		UploadTotal   int64               `json:"uploadTotal"`
		Connections   []HistoryConnection `json:"connections"`
	}
	err := mihomo.NewClient(s.Controller.Port, s.Controller.Secret).
		RequestInto("GET", "/connections", nil, connHistoryFetchTimeoutMs, &payload)
	if err != nil {
		return
	}
	now := time.Now().UnixMilli()
	c.history.Observe(payload.Connections, now)
	c.traffic.Feed(now, payload.UploadTotal, payload.DownloadTotal)
}

// ConnHistory returns recently closed connections (newest first), capped at
// max; max <= 0 returns up to the configured retention size.
func (c *Core) ConnHistory(max int) []HistoryConnection {
	return c.history.Snapshot(max)
}

// ConnHistoryLimit returns the configured retention size.
func (c *Core) ConnHistoryLimit() int {
	return c.Settings().ConnHistory.Limit
}
