package core

import (
	"sync"
	"time"
)

const (
	trafficBucketSeconds = 60
	trafficBuckets       = 24 * 60
	trafficBucketMs      = int64(trafficBucketSeconds) * 1000
	// trafficMaxGapMs bounds a trustworthy delta window; anything longer means
	// the ticker stalled (panel freeze, controller outage) and is dropped.
	trafficMaxGapMs = 90_000
)

// TrafficPoint is one bucket's average rate in bytes per second.
type TrafficPoint struct {
	T    int64   `json:"t"`
	Up   float64 `json:"up"`
	Down float64 `json:"down"`
}

// TrafficHistoryResponse is the /api/traffic/history payload.
type TrafficHistoryResponse struct {
	BucketSeconds int            `json:"bucketSeconds"`
	WindowSeconds int            `json:"windowSeconds"`
	Points        []TrafficPoint `json:"points"`
}

type trafficBucket struct {
	minute    int64 // bucket start (ms); 0 marks an empty slot
	upBytes   float64
	downBytes float64
	seconds   float64
}

// trafficAgg turns the kernel's cumulative uploadTotal/downloadTotal counters
// into 1-minute average-rate buckets covering the last 24h. It lives purely in
// process memory, so history resets on panel restart.
type trafficAgg struct {
	mu      sync.Mutex
	buckets [trafficBuckets]trafficBucket

	haveBase bool
	lastUp   int64
	lastDown int64
	lastAt   int64
}

func newTrafficAgg() *trafficAgg {
	return &trafficAgg{}
}

// Feed ingests one /connections totals sample taken at now (ms). The first
// sample only establishes a baseline. Counter resets (kernel restart), clock
// rollback and gaps longer than 90s re-baseline without fabricating traffic.
func (t *trafficAgg) Feed(now, upTotal, downTotal int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.haveBase {
		t.haveBase = true
		t.lastUp, t.lastDown, t.lastAt = upTotal, downTotal, now
		return
	}
	elapsed := now - t.lastAt
	if elapsed <= 0 || upTotal < t.lastUp || downTotal < t.lastDown {
		t.lastUp, t.lastDown, t.lastAt = upTotal, downTotal, now
		return
	}

	upDelta := upTotal - t.lastUp
	downDelta := downTotal - t.lastDown
	prevAt := t.lastAt
	t.lastUp, t.lastDown, t.lastAt = upTotal, downTotal, now
	if elapsed > trafficMaxGapMs {
		return
	}

	upRate := float64(upDelta) / float64(elapsed)
	downRate := float64(downDelta) / float64(elapsed)
	for start := prevAt; start < now; {
		bucketStart := start / trafficBucketMs * trafficBucketMs
		sliceEnd := bucketStart + trafficBucketMs
		if sliceEnd > now {
			sliceEnd = now
		}
		sliceMs := sliceEnd - start
		t.addLocked(bucketStart, upRate*float64(sliceMs), downRate*float64(sliceMs), float64(sliceMs)/1000)
		start = sliceEnd
	}
}

func trafficSlot(minute int64) int {
	idx := int(minute / trafficBucketMs % trafficBuckets)
	if idx < 0 {
		idx += trafficBuckets
	}
	return idx
}

func (t *trafficAgg) addLocked(minute int64, upBytes, downBytes, seconds float64) {
	b := &t.buckets[trafficSlot(minute)]
	if b.minute != minute {
		*b = trafficBucket{minute: minute}
	}
	b.upBytes += upBytes
	b.downBytes += downBytes
	b.seconds += seconds
}

// Snapshot returns observed buckets inside the last 24h in chronological order,
// including the current (still filling) minute.
func (t *trafficAgg) Snapshot(now int64) []TrafficPoint {
	t.mu.Lock()
	defer t.mu.Unlock()

	current := now / trafficBucketMs * trafficBucketMs
	first := current - int64(trafficBuckets-1)*trafficBucketMs
	points := make([]TrafficPoint, 0, trafficBuckets)
	for minute := first; minute <= current; minute += trafficBucketMs {
		b := t.buckets[trafficSlot(minute)]
		if b.minute != minute || b.seconds <= 0 {
			continue
		}
		points = append(points, TrafficPoint{
			T:    minute,
			Up:   b.upBytes / b.seconds,
			Down: b.downBytes / b.seconds,
		})
	}
	return points
}

// TrafficHistory returns the last 24h of 1-minute average rates.
func (c *Core) TrafficHistory() TrafficHistoryResponse {
	return TrafficHistoryResponse{
		BucketSeconds: trafficBucketSeconds,
		WindowSeconds: trafficBuckets * trafficBucketSeconds,
		Points:        c.traffic.Snapshot(time.Now().UnixMilli()),
	}
}
