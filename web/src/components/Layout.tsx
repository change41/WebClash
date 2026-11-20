import { useEffect, useState, type ReactNode } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import type { LucideIcon } from 'lucide-react';
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  Cable,
  CircleStop,
  FileStack,
  LayoutDashboard,
  ListFilter,
  LogOut,
  MemoryStick,
  Menu,
  Play,
  RotateCw,
  ScrollText,
  Settings,
  ShieldCheck,
  Waypoints,
} from 'lucide-react';
import { api } from '../lib/api';
import { cx, formatBytes, formatDuration, formatSpeed } from '../lib/format';
import { usePolling, useWsJson } from '../lib/hooks';
import { useKernel } from '../lib/kernel';
import { useToast } from '../lib/toast';
import type {
  ClashConfigsResponse,
  ConnectionsPayload,
  MemoryMessage,
  ProxyMode,
  TrafficMessage,
} from '../types';
import { ThemeToggle } from './ThemeToggle';
import { Button, ConfirmPopover, Segmented } from './ui';

interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  end?: boolean;
}

const NAV: NavItem[] = [
  { to: '/', label: '仪表盘', icon: LayoutDashboard, end: true },
  { to: '/proxies', label: '代理', icon: Waypoints },
  { to: '/profiles', label: '订阅配置', icon: FileStack },
  { to: '/connections', label: '连接', icon: Cable },
  { to: '/rules', label: '规则', icon: ListFilter },
  { to: '/logs', label: '日志', icon: ScrollText },
  { to: '/settings', label: '设置', icon: Settings },
];

const MODE_LABEL: Record<ProxyMode, string> = {
  rule: '规则',
  global: '全局',
  direct: '直连',
};

const STATE_META: Record<
  string,
  { label: string; dot: string; text: string }
> = {
  running: { label: '运行中', dot: 'bg-emerald-400', text: 'text-emerald-300' },
  starting: { label: '启动中', dot: 'bg-amber-400 animate-pulse', text: 'text-amber-300' },
  stopping: { label: '停止中', dot: 'bg-amber-400 animate-pulse', text: 'text-amber-300' },
  stopped: { label: '已停止', dot: 'bg-slate-500', text: 'text-slate-400' },
  error: { label: '异常', dot: 'bg-rose-400', text: 'text-rose-300' },
};

export function Layout({
  version,
  onLogout,
  children,
}: {
  version: string;
  onLogout: () => void;
  children: ReactNode;
}) {
  const { status, start, stop, restart, busy } = useKernel();
  const toast = useToast();
  const location = useLocation();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [mode, setMode] = useState<ProxyMode>('rule');
  const [confirmAction, setConfirmAction] = useState<'stop' | 'restart' | null>(null);

  const running = Boolean(status?.running);
  const kernelState = status?.state ?? 'stopped';
  const meta = STATE_META[kernelState] ?? STATE_META.stopped!;
  const current = NAV.find((item) => (item.end ? location.pathname === item.to : location.pathname.startsWith(item.to)));

  const [traffic, setTraffic] = useState<TrafficMessage>({ up: 0, down: 0 });
  const [memory, setMemory] = useState<MemoryMessage | null>(null);

  useWsJson<TrafficMessage>(running ? '/ws/traffic' : null, (message) => {
    if (typeof message.up !== 'number' || typeof message.down !== 'number') return;
    setTraffic({ up: message.up, down: message.down });
  });

  useWsJson<MemoryMessage>(running ? '/ws/memory' : null, (message) => {
    if (typeof message.inuse === 'number') setMemory(message);
  });

  const connections = usePolling<ConnectionsPayload>(
    () => api('/clash/connections'),
    10000,
    running,
  );
  const connectionCount = connections.data?.connections?.length ?? 0;

  useEffect(() => {
    setMobileOpen(false);
    window.scrollTo({ top: 0 });
  }, [location.pathname]);

  useEffect(() => {
    if (!running) return;
    let cancelled = false;
    const load = async () => {
      try {
        const configs = await api<ClashConfigsResponse>('/clash/configs');
        if (!cancelled) setMode(configs.mode ?? 'rule');
      } catch {
        // 内核可能正在重载
      }
    };
    void load();
    const timer = window.setInterval(load, 10_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [running]);

  const changeMode = async (next: ProxyMode) => {
    if (!running) {
      toast.info('请先启动内核');
      return;
    }
    const previous = mode;
    setMode(next);
    try {
      await api('/clash/configs', { method: 'PATCH', body: { mode: next } });
      toast.success(`已切换到${MODE_LABEL[next]}模式`);
    } catch (err) {
      setMode(previous);
      toast.error((err as Error).message);
    }
  };

  const runConfirmedAction = async () => {
    if (!confirmAction) return;
    if (confirmAction === 'stop') {
      await stop();
    } else {
      await restart();
    }
    setConfirmAction(null);
  };

  return (
    <div className="min-h-screen">
      {mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/60 backdrop-blur-sm lg:hidden"
          onClick={() => setMobileOpen(false)}
        />
      )}

      <aside
        className={cx(
          'fixed inset-y-0 left-0 z-40 flex w-60 flex-col border-r border-line bg-ink-900/95 backdrop-blur transition-transform duration-200 lg:translate-x-0',
          mobileOpen ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <div className="flex h-14 items-center gap-2.5 border-b border-line px-4">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-emerald-400/20 to-cyan-400/10 ring-1 ring-emerald-400/30">
            <ShieldCheck className="h-4.5 w-4.5 text-emerald-400" />
          </div>
          <div className="min-w-0">
            <p className="text-sm font-semibold tracking-wide text-slate-100">WebClash</p>
            <p className="text-[10px] text-slate-500">v{version} · mihomo</p>
          </div>
        </div>

        <nav className="flex-1 space-y-0.5 overflow-y-auto px-2.5 py-3">
          {NAV.map((item) => {
            const Icon = item.icon;
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  cx(
                    'group flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-[13px] font-medium transition',
                    isActive
                      ? 'bg-emerald-500/10 text-emerald-300'
                      : 'text-slate-400 hover:bg-white/[0.04] hover:text-slate-200',
                  )
                }
              >
                <Icon className="h-4 w-4 shrink-0" />
                {item.label}
              </NavLink>
            );
          })}
        </nav>

        <div className="border-t border-line p-3">
          <div className="rounded-xl border border-line bg-white/[0.02] p-3">
            <div className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                <span className={cx('h-1.5 w-1.5 rounded-full', meta.dot)} />
                <span className={cx('text-xs font-medium', meta.text)}>{meta.label}</span>
              </div>
              <span className="mono text-[10px] text-slate-500">
                {status?.binary.version ?? '内核未安装'}
              </span>
            </div>
            <p className="mt-1 truncate text-[10px] text-slate-500">
              {running
                ? `PID ${status?.pid ?? '-'} · ${formatDuration(Date.now() - (status?.startedAt ?? Date.now()))}`
                : status?.lastError
                  ? status.lastError
                  : '内核未运行'}
            </p>
            <div className="mt-1 flex items-center justify-between gap-2 text-[10px]">
              <span className="shrink-0 text-slate-500">内核路径</span>
              <span
                className="mono min-w-0 truncate text-slate-400"
                title={status?.binary.path ?? '未找到内核二进制'}
              >
                {status?.binary.path ?? '未找到'}
              </span>
            </div>
            {running && (
              <div className="mt-2.5 space-y-1.5 rounded-lg border border-line/70 bg-ink-950/40 px-2.5 py-2">
                <div className="flex items-center justify-between gap-2">
                  <span className="flex items-center gap-1 text-[10px]" title="实时下载">
                    <ArrowDownToLine className="h-3 w-3 text-emerald-400" />
                    <span className="mono text-slate-300">{formatSpeed(traffic.down)}</span>
                  </span>
                  <span className="flex items-center gap-1 text-[10px]" title="实时上传">
                    <ArrowUpFromLine className="h-3 w-3 text-sky-400" />
                    <span className="mono text-slate-300">{formatSpeed(traffic.up)}</span>
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-[10px]">
                  <span className="flex items-center gap-1" title="活动连接">
                    <Cable className="h-3 w-3 text-violet-400" />
                    <span className="mono text-slate-300">
                      {connections.data ? connectionCount : '—'}
                    </span>
                    <span className="text-slate-500">连接</span>
                  </span>
                  <span className="flex items-center gap-1" title="内核内存（mihomo 自报）">
                    <MemoryStick className="h-3 w-3 text-amber-400" />
                    <span className="mono text-slate-300">
                      {memory ? formatBytes(memory.inuse) : '—'}
                    </span>
                    <span className="text-slate-500">内存</span>
                  </span>
                </div>
              </div>
            )}
            <div className="relative mt-2.5">
              <div className="flex gap-1.5">
                {running ? (
                  <Button
                    size="sm"
                    variant="outline"
                    className="flex-1"
                    loading={busy}
                    onClick={() => setConfirmAction('stop')}
                  >
                    <CircleStop className="h-3.5 w-3.5" />
                    停止
                  </Button>
                ) : (
                  <Button
                    size="sm"
                    variant="primary"
                    className="flex-1"
                    loading={busy}
                    onClick={() => void start()}
                  >
                    <Play className="h-3.5 w-3.5" />
                    启动
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  title="重启内核"
                  disabled={busy}
                  onClick={() => setConfirmAction('restart')}
                >
                  <RotateCw className="h-3.5 w-3.5" />
                </Button>
              </div>
              <ConfirmPopover
                open={confirmAction !== null}
                className="left-0 right-0 w-auto"
                placement="top"
                message={confirmAction === 'restart' ? '重启内核？' : '停止内核？'}
                detail={
                  confirmAction === 'restart' ? '代理连接将短暂中断' : '代理连接将全部中断'
                }
                confirmText={confirmAction === 'restart' ? '重启' : '停止'}
                danger={confirmAction === 'stop'}
                busy={busy}
                onCancel={() => setConfirmAction(null)}
                onConfirm={() => void runConfirmedAction()}
              />
            </div>
          </div>

          <button
            type="button"
            onClick={onLogout}
            className="mt-2 flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-xs text-slate-500 transition hover:bg-white/[0.04] hover:text-slate-300"
          >
            <LogOut className="h-3.5 w-3.5" />
            退出登录
          </button>
        </div>
      </aside>

      <div className="flex min-h-screen min-w-0 flex-col lg:pl-60">
        <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-line bg-ink-950/85 px-4 backdrop-blur-md lg:px-6">
          <button
            type="button"
            className="rounded-lg p-1.5 text-slate-400 hover:bg-white/5 hover:text-white lg:hidden"
            onClick={() => setMobileOpen(true)}
            aria-label="打开菜单"
          >
            <Menu className="h-5 w-5" />
          </button>
          <div className="min-w-0">
            <h1 className="truncate text-sm font-semibold text-slate-100">{current?.label ?? 'WebClash'}</h1>
          </div>
          <div className="ml-auto flex items-center gap-2.5">
            <Segmented
              className="hidden sm:inline-flex"
              value={mode}
              options={[
                { value: 'rule', label: '规则' },
                { value: 'global', label: '全局' },
                { value: 'direct', label: '直连' },
              ]}
              onChange={(next) => void changeMode(next)}
            />
            <div className="hidden h-6 w-px bg-line sm:block" />
            <div className="flex items-center gap-2 rounded-full border border-line bg-ink-900 px-2.5 py-1">
              <span className={cx('h-1.5 w-1.5 rounded-full', meta.dot)} />
              <span className={cx('text-[11px] font-medium', meta.text)}>{meta.label}</span>
            </div>
            <ThemeToggle />
          </div>
        </header>

        <main className="mx-auto w-full max-w-[1500px] flex-1 px-4 py-5 lg:px-6 lg:py-6">{children}</main>
      </div>
    </div>
  );
}
