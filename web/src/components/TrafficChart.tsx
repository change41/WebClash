import { useId, useMemo, useState } from 'react';
import { formatSpeed } from '../lib/format';

export interface TrafficPoint {
  t: number;
  up: number;
  down: number;
}

const WIDTH = 720;
const HEIGHT = 200;
const PAD_X = 6;
const PAD_TOP = 12;
const PAD_BOTTOM = 18;
const INNER_H = HEIGHT - PAD_TOP - PAD_BOTTOM;

const BUCKET_MS = 60_000;
const WINDOW_MS = 24 * 60 * 60 * 1000;
const MIN_WINDOW_MS = 10 * 60_000;
const MAX_POINTS = 96;

const AGG_STEPS = [1, 2, 5, 10, 15, 30, 60].map((minutes) => minutes * BUCKET_MS);
const TICK_STEPS = [1, 2, 5, 10, 15, 30, 60, 120, 180, 240, 360].map(
  (minutes) => minutes * BUCKET_MS,
);

const Y_RATIOS = [1, 0.75, 0.5, 0.25, 0];

function pickStep(span: number, maxSegments: number, steps: number[]): number {
  return steps.find((step) => span / step <= maxSegments) ?? steps[steps.length - 1]!;
}

function formatClock(ts: number): string {
  return new Date(ts).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
}

function formatHoverTime(ts: number, spanMs: number): string {
  const date = new Date(ts);
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  const prefix = `${month}-${day} `;
  if (spanMs <= BUCKET_MS) return prefix + formatClock(ts);
  const end = new Date(ts + spanMs);
  const endText = `${String(end.getHours()).padStart(2, '0')}:${String(
    end.getMinutes(),
  ).padStart(2, '0')}`;
  return `${prefix}${formatClock(ts)}–${endText}`;
}

function formatWindow(spanMs: number): string {
  const minutes = Math.round(spanMs / BUCKET_MS);
  if (minutes >= 24 * 60) return '24 小时';
  if (minutes >= 60) {
    const hours = minutes / 60;
    return `${Number.isInteger(hours) ? hours : hours.toFixed(1)} 小时`;
  }
  return `${minutes} 分钟`;
}

function formatGranularity(ms: number): string {
  if (ms === BUCKET_MS) return '1 分钟粒度';
  if (ms >= 3_600_000) return `${ms / 3_600_000} 小时均值`;
  return `${ms / BUCKET_MS} 分钟均值`;
}

interface HoverHit {
  t: number;
  up: number;
  down: number;
  xPct: number;
  downYPct: number;
  upYPct: number;
}

interface Tick {
  t: number;
  xPct: number;
}

export function TrafficChart({
  points,
  height = 220,
  className,
}: {
  points: TrafficPoint[];
  height?: number;
  className?: string;
}) {
  const rawId = useId();
  const id = rawId.replace(/[^a-zA-Z0-9]/g, '');
  const downId = `down-${id}`;
  const upId = `up-${id}`;
  const [hover, setHover] = useState<HoverHit | null>(null);

  // The window follows the observed data span (capped at 24h) instead of always
  // stretching a few minutes over a full day; granularity grows with the span so
  // the series stays around MAX_POINTS segments.
  const { max, segments, ticks, lookup, start, end, granularity } = useMemo(() => {
    const nowBucket = Math.floor(Date.now() / BUCKET_MS) * BUCKET_MS;
    const known = points
      .filter(
        (point) =>
          Number.isFinite(point.t) && Number.isFinite(point.up) && Number.isFinite(point.down),
      )
      .sort((a, b) => a.t - b.t);

    const end = nowBucket + BUCKET_MS;
    let start = end - MIN_WINDOW_MS;
    if (known.length > 0) {
      const firstT = Math.floor(known[0]!.t / BUCKET_MS) * BUCKET_MS;
      const earliest = end - WINDOW_MS;
      start = Math.max(earliest, Math.min(firstT - BUCKET_MS, end - MIN_WINDOW_MS));
    }

    const granularity = pickStep(end - start, MAX_POINTS, AGG_STEPS);
    start = Math.floor(start / granularity) * granularity;

    const buckets = new Map<number, { up: number; down: number; count: number }>();
    for (const point of known) {
      if (point.t < start || point.t >= end) continue;
      const key = Math.floor(point.t / granularity) * granularity;
      const bucket = buckets.get(key);
      if (bucket) {
        bucket.up += point.up;
        bucket.down += point.down;
        bucket.count += 1;
      } else {
        buckets.set(key, { up: point.up, down: point.down, count: 1 });
      }
    }
    const series = Array.from(buckets, ([t, bucket]) => ({
      t,
      up: bucket.up / bucket.count,
      down: bucket.down / bucket.count,
    })).sort((a, b) => a.t - b.t);

    const values = series.flatMap((point) => [point.up, point.down]);
    const peak = Math.max(2048, ...values);
    const maxValue = Math.ceil(peak / 1024) * 1024;
    const innerW = WIDTH - PAD_X * 2;

    const xAt = (t: number) => PAD_X + ((t - start) / (end - start)) * innerW;
    const yAt = (value: number) => PAD_TOP + INNER_H - (value / maxValue) * INNER_H;
    const baseline = PAD_TOP + INNER_H;

    // A gap wider than one and a half buckets (kernel stopped, panel restarted)
    // starts a new segment instead of drawing a fake slope across it.
    const build = (key: 'up' | 'down') => {
      const out: { line: string; area: string }[] = [];
      let line = '';
      let firstX = 0;
      let lastX = 0;
      let count = 0;
      let prevT: number | null = null;
      const flush = () => {
        if (line && count >= 2) {
          out.push({
            line,
            area: `${line} L${lastX.toFixed(2)},${baseline.toFixed(2)} L${firstX.toFixed(2)},${baseline.toFixed(2)} Z`,
          });
        }
        line = '';
        count = 0;
        prevT = null;
      };
      for (const point of series) {
        if (prevT !== null && point.t - prevT > granularity * 1.5) flush();
        const x = xAt(point.t);
        line += `${count === 0 ? 'M' : ' L'}${x.toFixed(2)},${yAt(point[key]).toFixed(2)}`;
        if (count === 0) firstX = x;
        lastX = x;
        count += 1;
        prevT = point.t;
      }
      flush();
      return out;
    };

    const hitMap = new Map<number, HoverHit>();
    for (const point of series) {
      hitMap.set(point.t, {
        t: point.t,
        up: point.up,
        down: point.down,
        xPct: (xAt(point.t) / WIDTH) * 100,
        downYPct: (yAt(point.down) / HEIGHT) * 100,
        upYPct: (yAt(point.up) / HEIGHT) * 100,
      });
    }

    const tickStep = pickStep(end - start, 6, TICK_STEPS);
    const firstTick = Math.ceil(start / tickStep) * tickStep;
    const ticks: Tick[] = [];
    for (let t = firstTick; t < end; t += tickStep) {
      ticks.push({ t, xPct: (xAt(t) / WIDTH) * 100 });
    }

    return {
      max: maxValue,
      segments: { down: build('down'), up: build('up') },
      ticks,
      lookup: hitMap,
      start,
      end,
      granularity,
    };
  }, [points]);

  const moveHover = (event: React.PointerEvent<HTMLDivElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return;
    const ratio = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width));
    const at = start + ratio * (end - start);
    const hit = lookup.get(Math.floor(at / granularity) * granularity);
    if (!hit) {
      setHover(null);
      return;
    }
    setHover((prev) => (prev && prev.t === hit.t ? prev : hit));
  };

  const windowText = formatWindow(end - start);
  const grid = [0.25, 0.5, 0.75].map((ratio) => PAD_TOP + INNER_H * ratio);
  const yLabels = Y_RATIOS.map((ratio) => ({
    ratio,
    text: formatSpeed(max * ratio),
    topPct: ((PAD_TOP + INNER_H * (1 - ratio)) / HEIGHT) * 100,
  }));

  return (
    <div className={className}>
      <div className="flex gap-2">
        <div className="relative w-14 shrink-0 select-none" style={{ height }}>
          {yLabels.map((label) => (
            <span
              key={label.ratio}
              className="mono absolute right-0 -translate-y-1/2 text-[10px] leading-none text-slate-600"
              style={{ top: `${label.topPct}%` }}
            >
              {label.text}
            </span>
          ))}
        </div>

        <div
          className="relative min-w-0 flex-1"
          style={{ height }}
          onPointerMove={moveHover}
          onPointerLeave={() => setHover(null)}
        >
          <svg
            viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
            preserveAspectRatio="none"
            style={{ height, width: '100%' }}
            role="img"
            aria-label={`最近 ${windowText}流量图`}
          >
            <defs>
              <linearGradient id={downId} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--chart-down)" stopOpacity="0.35" />
                <stop offset="100%" stopColor="var(--chart-down)" stopOpacity="0" />
              </linearGradient>
              <linearGradient id={upId} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--chart-up)" stopOpacity="0.28" />
                <stop offset="100%" stopColor="var(--chart-up)" stopOpacity="0" />
              </linearGradient>
            </defs>

            {grid.map((y) => (
              <line
                key={y}
                x1={0}
                x2={WIDTH}
                y1={y}
                y2={y}
                stroke="var(--chart-grid)"
                strokeDasharray="3 5"
                vectorEffect="non-scaling-stroke"
              />
            ))}

            {segments.up.map((segment, index) => (
              <g key={`up-${index}`}>
                <path d={segment.area} fill={`url(#${upId})`} />
                <path
                  d={segment.line}
                  fill="none"
                  stroke="var(--chart-up)"
                  strokeWidth="1.6"
                  strokeLinejoin="round"
                  strokeLinecap="round"
                  vectorEffect="non-scaling-stroke"
                />
              </g>
            ))}
            {segments.down.map((segment, index) => (
              <g key={`down-${index}`}>
                <path d={segment.area} fill={`url(#${downId})`} />
                <path
                  d={segment.line}
                  fill="none"
                  stroke="var(--chart-down)"
                  strokeWidth="1.8"
                  strokeLinejoin="round"
                  strokeLinecap="round"
                  vectorEffect="non-scaling-stroke"
                />
              </g>
            ))}
          </svg>

          {hover && (
            <>
              <div
                className="pointer-events-none absolute inset-y-0 w-px bg-white/20"
                style={{ left: `${hover.xPct}%` }}
              />
              <div
                className="pointer-events-none absolute h-1.5 w-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-emerald-400 ring-2 ring-emerald-400/25"
                style={{ left: `${hover.xPct}%`, top: `${hover.downYPct}%` }}
              />
              <div
                className="pointer-events-none absolute h-1.5 w-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-sky-400 ring-2 ring-sky-400/25"
                style={{ left: `${hover.xPct}%`, top: `${hover.upYPct}%` }}
              />
              <div
                className="pointer-events-none absolute top-2 w-[156px] -translate-x-1/2 rounded-lg border border-line bg-ink-900/95 px-2.5 py-2 text-[10px] shadow-lg"
                style={{ left: `min(max(${hover.xPct}%, 80px), calc(100% - 80px))` }}
              >
                <p className="mb-1.5 text-[10px] text-slate-400">
                  {formatHoverTime(hover.t, granularity)}
                </p>
                <p className="flex items-center justify-between gap-3">
                  <span className="flex items-center gap-1.5 text-slate-500">
                    <span className="h-1.5 w-3 rounded-full bg-emerald-400" />
                    下载
                  </span>
                  <span className="mono text-slate-200">{formatSpeed(hover.down)}</span>
                </p>
                <p className="mt-1 flex items-center justify-between gap-3">
                  <span className="flex items-center gap-1.5 text-slate-500">
                    <span className="h-1.5 w-3 rounded-full bg-sky-400" />
                    上传
                  </span>
                  <span className="mono text-slate-200">{formatSpeed(hover.up)}</span>
                </p>
              </div>
            </>
          )}
        </div>
      </div>

      <div className="mt-1 h-3 pl-16 text-[10px] text-slate-600">
        <div className="relative h-full">
          {ticks.map((tick) => (
            <span
              key={tick.t}
              className="absolute top-0 -translate-x-1/2 leading-none whitespace-nowrap"
              style={{ left: `min(max(${tick.xPct}%, 20px), calc(100% - 20px))` }}
            >
              {formatClock(tick.t)}
            </span>
          ))}
        </div>
      </div>
      <div className="mt-0.5 flex items-center justify-between pl-16 text-[10px] text-slate-600">
        <span>峰值 {formatSpeed(max)}</span>
        <span>
          最近 {windowText} · {formatGranularity(granularity)}
        </span>
      </div>
    </div>
  );
}
