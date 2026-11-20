package core

import (
	"testing"
)

const minuteMs = int64(60_000)

func TestTrafficAggMinuteBuckets(t *testing.T) {
	agg := newTrafficAgg()
	base := int64(10) * minuteMs

	agg.Feed(base, 0, 0)
	agg.Feed(base+30_000, 60_000, 120_000)
	agg.Feed(base+60_000, 120_000, 240_000)
	agg.Feed(base+90_000, 180_000, 360_000)

	points := agg.Snapshot(base + 90_000)
	if len(points) != 2 {
		t.Fatalf("points = %+v, want 2 buckets", points)
	}
	if points[0].T != base || points[1].T != base+minuteMs {
		t.Errorf("bucket starts = %d/%d, want %d/%d", points[0].T, points[1].T, base, base+minuteMs)
	}
	for _, p := range points {
		if p.Up != 2000 || p.Down != 4000 {
			t.Errorf("rates at %d = up %v down %v, want 2000/4000", p.T, p.Up, p.Down)
		}
	}
}

func TestTrafficAggSplitsAtMinuteBoundary(t *testing.T) {
	agg := newTrafficAgg()
	base := int64(20) * minuteMs

	agg.Feed(base, 0, 0)
	agg.Feed(base+30_500, 61_000, 0)
	agg.Feed(base+61_000, 122_000, 0)

	points := agg.Snapshot(base + 61_000)
	if len(points) != 2 {
		t.Fatalf("points = %+v, want 2 buckets", points)
	}
	// 2000 B/s for 60s in the first bucket, 2000 B/s for 1s in the second.
	if points[0].T != base || points[0].Up != 2000 {
		t.Errorf("first bucket = %+v, want t=%d up=2000", points[0], base)
	}
	if points[1].T != base+minuteMs || points[1].Up != 2000 {
		t.Errorf("second bucket = %+v, want t=%d up=2000", points[1], base+minuteMs)
	}
}

func TestTrafficAggCounterResetDropsDelta(t *testing.T) {
	agg := newTrafficAgg()

	agg.Feed(0, 5000, 9000)
	agg.Feed(minuteMs, 100, 200) // kernel restart: counters went backwards
	if points := agg.Snapshot(minuteMs); len(points) != 0 {
		t.Fatalf("reset produced points: %+v", points)
	}

	agg.Feed(2*minuteMs, 100+60_000, 200+60_000)
	points := agg.Snapshot(2 * minuteMs)
	if len(points) != 1 {
		t.Fatalf("points = %+v, want 1 bucket", points)
	}
	if points[0].T != minuteMs || points[0].Up != 1000 || points[0].Down != 1000 {
		t.Errorf("bucket = %+v, want t=%d up/down=1000", points[0], minuteMs)
	}
}

func TestTrafficAggGapDropsWindow(t *testing.T) {
	agg := newTrafficAgg()

	agg.Feed(0, 0, 0)
	agg.Feed(200_000, 100_000, 200_000) // > 90s: untrustworthy, dropped
	if points := agg.Snapshot(200_000); len(points) != 0 {
		t.Fatalf("gap produced points: %+v", points)
	}

	agg.Feed(260_000, 160_000, 320_000) // 60s window from the re-baseline
	points := agg.Snapshot(260_000)
	if len(points) != 2 {
		t.Fatalf("points = %+v, want 2 buckets", points)
	}
	for _, p := range points {
		if p.Up != 1000 || p.Down != 2000 {
			t.Errorf("bucket %d = up %v down %v, want 1000/2000", p.T, p.Up, p.Down)
		}
	}
}

func TestTrafficAggClockRollbackIgnored(t *testing.T) {
	agg := newTrafficAgg()
	agg.Feed(10*minuteMs, 0, 0)
	agg.Feed(9*minuteMs, 5000, 5000) // clock went backwards
	if points := agg.Snapshot(10 * minuteMs); len(points) != 0 {
		t.Fatalf("rollback produced points: %+v", points)
	}
}

func TestTrafficAggSnapshotWindowAndRing(t *testing.T) {
	agg := newTrafficAgg()
	now := int64(1_800_000_000_000) / minuteMs * minuteMs
	current := now

	agg.addLocked(current-1441*minuteMs, 100, 100, 60)
	agg.addLocked(current-1439*minuteMs, 100, 100, 60)
	agg.addLocked(current, 50, 50, 30)

	points := agg.Snapshot(current)
	if len(points) != 2 {
		t.Fatalf("points = %+v, want 2 (oldest trimmed)", points)
	}
	if points[0].T != current-1439*minuteMs || points[1].T != current {
		t.Errorf("starts = %d/%d, want %d/%d", points[0].T, points[1].T, current-1439*minuteMs, current)
	}
	if points[1].Up != 50.0/30 || points[1].Down != 50.0/30 {
		t.Errorf("partial bucket rate = %v/%v, want 50/30", points[1].Up, points[1].Down)
	}

	// The same slot reused a full window later must not resurrect the old value.
	later := current + 1440*minuteMs
	agg.addLocked(later, 60, 120, 60)
	points = agg.Snapshot(later)
	if len(points) != 1 || points[0].T != later || points[0].Up != 1 {
		t.Errorf("ring reuse = %+v, want only t=%d up=1", points, later)
	}
}

func TestTrafficAggEmptySnapshot(t *testing.T) {
	agg := newTrafficAgg()
	points := agg.Snapshot(minuteMs)
	if points == nil || len(points) != 0 {
		t.Fatalf("empty snapshot = %#v, want non-nil empty", points)
	}
}
