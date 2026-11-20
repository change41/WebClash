import { useEffect, useRef, useState } from 'react';
import { Download, Pause, Play, ScrollText, Search, Trash2 } from 'lucide-react';
import { Badge, Button, EmptyState, Input, Select } from '../components/ui';
import { api } from '../lib/api';
import { cx } from '../lib/format';
import { useWsJson } from '../lib/hooks';
import { useKernel } from '../lib/kernel';
import type { LogMessage } from '../types';

type Level = 'debug' | 'info' | 'warning' | 'error';

interface Entry {
  id: number;
  time: string;
  level: string;
  message: string;
}

const LEVEL_ORDER: Record<string, number> = {
  debug: 0,
  info: 1,
  warning: 2,
  error: 3,
};

const LEVEL_STYLE: Record<string, string> = {
  debug: 'text-slate-500',
  info: 'text-sky-400',
  warning: 'text-amber-400',
  error: 'text-rose-400',
};

const LEVEL_LABEL: Record<string, string> = {
  debug: 'DEBUG',
  info: 'INFO',
  warning: 'WARN',
  error: 'ERROR',
};

const MAX_ENTRIES = 2000;

/** 解析后端捕获的内核 stdout 行：`HH:MM:SS time="…" level=info msg="…"` */
function parseKernelLine(line: string): Omit<Entry, 'id'> | null {
  const structured = /^(\d{2}:\d{2}:\d{2})\s+time="[^"]*"\s+level=(\w+)\s+msg="([\s\S]*)"$/.exec(
    line,
  );
  if (structured) {
    return {
      time: structured[1],
      level: structured[2],
      message: structured[3].replace(/\\"/g, '"').replace(/\\\\/g, '\\'),
    };
  }
  const plain = /^(\d{2}:\d{2}:\d{2})\s+(.+)$/.exec(line);
  return plain ? { time: plain[1], level: 'info', message: plain[2] } : null;
}

export function Logs() {
  const { status, start, busy } = useKernel();
  const running = Boolean(status?.running);
  const [level, setLevel] = useState<Level>('info');
  const [entries, setEntries] = useState<Entry[]>([]);
  const [query, setQuery] = useState('');
  const [paused, setPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const containerRef = useRef<HTMLDivElement>(null);
  const idRef = useRef(0);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  const connected = useWsJson<LogMessage>(
    running ? `/ws/logs?level=${level}` : null,
    (message) => {
      if (pausedRef.current) return;
      if (typeof message?.payload !== 'string' || !message.payload) return;
      const entry: Entry = {
        id: ++idRef.current,
        time: new Date().toLocaleTimeString('zh-CN', { hour12: false }),
        level: message.type || 'info',
        message: message.payload,
      };
      setEntries((current) => {
        const next = [...current, entry];
        return next.length > MAX_ENTRIES ? next.slice(next.length - MAX_ENTRIES) : next;
      });
    },
  );

  useEffect(() => {
    if (!running) return;
    let cancelled = false;
    api<{ logs: string[] }>('/kernel/logs?lines=200')
      .then((result) => {
        if (cancelled) return;
        const seeded: Entry[] = [];
        for (const line of result.logs) {
          const parsed = parseKernelLine(line);
          if (parsed) seeded.push({ id: ++idRef.current, ...parsed });
        }
        setEntries((current) => (current.length ? current : seeded));
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [running]);

  const keyword = query.trim().toLowerCase();
  const minLevel = LEVEL_ORDER[level] ?? 0;
  const filtered = entries.filter(
    (entry) =>
      (LEVEL_ORDER[entry.level] ?? 1) >= minLevel &&
      (!keyword ||
        entry.message.toLowerCase().includes(keyword) ||
        entry.level.toLowerCase().includes(keyword)),
  );

  useEffect(() => {
    if (!autoScroll) return;
    const node = containerRef.current;
    if (!node) return;
    node.scrollTop = node.scrollHeight;
  }, [filtered.length, autoScroll, paused]);

  const clear = () => setEntries([]);

  const download = () => {
    const content = entries
      .map((entry) => `[${entry.time}] [${entry.level.toUpperCase()}] ${entry.message}`)
      .join('\n');
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `webclash-logs-${new Date().toISOString().replace(/[:.]/g, '-')}.txt`;
    link.click();
    URL.revokeObjectURL(url);
  };

  if (!running) {
    return (
      <div className="card">
        <EmptyState
          icon={<Play className="h-9 w-9" />}
          title="内核未运行"
          description="启动内核后可实时查看 mihomo 运行日志。"
          action={
            <Button variant="primary" loading={busy} onClick={() => void start()}>
              <Play className="h-3.5 w-3.5" />
              启动内核
            </Button>
          }
        />
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2.5">
        <Select
          value={level}
          onChange={(event) => setLevel(event.target.value as Level)}
          className="w-32"
        >
          <option value="debug">全部级别</option>
          <option value="info">信息</option>
          <option value="warning">警告</option>
          <option value="error">错误</option>
        </Select>
        <div className="relative min-w-[180px] flex-1 sm:max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-slate-600" />
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="过滤日志内容…"
            className="pl-9"
          />
        </div>
        <div className="flex items-center gap-1.5 text-[11px] text-slate-500">
          <span
            className={cx(
              'h-1.5 w-1.5 rounded-full',
              connected ? 'bg-emerald-400' : 'bg-amber-400 animate-pulse',
            )}
          />
          {connected ? '已连接' : '连接中'}
          <span className="ml-1 text-slate-600">{filtered.length} 条</span>
        </div>
        <div className="ml-auto flex items-center gap-1.5">
          <Button
            size="sm"
            variant={paused ? 'primary' : 'outline'}
            onClick={() => setPaused((value) => !value)}
          >
            {paused ? <Play className="h-3.5 w-3.5" /> : <Pause className="h-3.5 w-3.5" />}
            {paused ? '继续' : '暂停'}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={entries.length === 0}
            onClick={download}
            title="导出日志"
          >
            <Download className="h-3.5 w-3.5" />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={entries.length === 0}
            onClick={clear}
            title="清空日志"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      <div className="card overflow-hidden">
        <div className="flex items-center justify-between border-b border-line px-4 py-2">
          <div className="flex items-center gap-2 text-[11px] text-slate-500">
            <ScrollText className="h-3.5 w-3.5" />
            mihomo 运行日志
            {paused && <Badge tone="amber">已暂停</Badge>}
          </div>
          <label className="flex cursor-pointer items-center gap-1.5 text-[11px] text-slate-500">
            <input
              type="checkbox"
              checked={autoScroll}
              onChange={(event) => setAutoScroll(event.target.checked)}
              className="h-3 w-3 accent-emerald-500"
            />
            自动滚动
          </label>
        </div>

        <div
          ref={containerRef}
          className="h-[calc(100vh-250px)] min-h-[320px] overflow-auto bg-ink-950/70 px-4 py-3"
        >
          {filtered.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-2 text-slate-600">
              <ScrollText className="h-7 w-7" />
              <p className="text-xs">
                {keyword ? '没有匹配的日志' : '暂无日志：内核空闲时不产生新日志，可发起请求或切换级别查看'}
              </p>
            </div>
          ) : (
            <div className="space-y-0.5">
              {filtered.map((entry) => (
                <div
                  key={entry.id}
                  className="mono flex gap-2 rounded px-1 py-0.5 text-[11px] leading-relaxed hover:bg-white/[0.03]"
                >
                  <span className="shrink-0 text-slate-600">{entry.time.slice(0, 8)}</span>
                  <span
                    className={cx(
                      'w-12 shrink-0 text-right font-semibold',
                      LEVEL_STYLE[entry.level] ?? 'text-slate-500',
                    )}
                  >
                    {LEVEL_LABEL[entry.level] ?? entry.level.toUpperCase()}
                  </span>
                  <span className="min-w-0 break-all whitespace-pre-wrap text-slate-300">
                    {entry.message}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
