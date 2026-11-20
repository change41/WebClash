import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  Cable,
  ExternalLink,
  FileStack,
  MemoryStick,
  Play,
  RefreshCw,
} from 'lucide-react';
import { TrafficChart } from '../components/TrafficChart';
import { Badge, Button, EmptyState, ProgressBar, Section } from '../components/ui';
import { api } from '../lib/api';
import { formatBytes, formatExpire, formatRelative, formatSpeed } from '../lib/format';
import { usePolling, useWsJson } from '../lib/hooks';
import { useKernel } from '../lib/kernel';
import { useToast } from '../lib/toast';
import type {
  ConnectionsPayload,
  MemoryMessage,
  ProfilesResponse,
  SystemStatus,
  TrafficHistoryResponse,
  TrafficMessage,
} from '../types';

function Stat({
  label,
  value,
  sub,
  icon,
  accent,
  hint,
}: {
  label: string;
  value: string;
  sub: string;
  icon: React.ReactNode;
  accent: string;
  hint?: string;
}) {
  return (
    <div className="card p-4" title={hint}>
      <div className="flex items-center justify-between">
        <span className="text-xs text-slate-500">{label}</span>
        <span className={`flex h-7 w-7 items-center justify-center rounded-lg ${accent}`}>
          {icon}
        </span>
      </div>
      <p className="mono mt-2 text-xl font-semibold tracking-tight text-slate-100">{value}</p>
      <p className="mt-0.5 truncate text-[11px] text-slate-500">{sub}</p>
    </div>
  );
}

export function Dashboard() {
  const { status, start, busy } = useKernel();
  const toast = useToast();
  const navigate = useNavigate();
  const running = Boolean(status?.running);

  const [traffic, setTraffic] = useState<TrafficMessage>({ up: 0, down: 0 });
  const [memory, setMemory] = useState<MemoryMessage | null>(null);
  const [updating, setUpdating] = useState(false);

  useWsJson<TrafficMessage>(running ? '/ws/traffic' : null, (message) => {
    if (typeof message.up !== 'number' || typeof message.down !== 'number') return;
    setTraffic({ up: message.up, down: message.down });
  });

  useWsJson<MemoryMessage>(running ? '/ws/memory' : null, (message) => {
    if (typeof message.inuse === 'number') setMemory(message);
  });

  const connections = usePolling<ConnectionsPayload>(
    () => api('/clash/connections'),
    5000,
    running,
  );
  const profiles = usePolling<ProfilesResponse>(() => api('/profiles'), 15000);
  const kernelLogs = usePolling<{ logs: string[] }>(() => api('/kernel/logs?lines=12'), 5000);
  const trafficHistory = usePolling<TrafficHistoryResponse>(
    () => api('/traffic/history'),
    5000,
  );
  const system = usePolling<SystemStatus>(() => api('/system/status'), 5000);

  const activeProfile = useMemo(
    () => profiles.data?.profiles.find((profile) => profile.id === profiles.data?.activeId) ?? null,
    [profiles.data],
  );

  const connectionList = connections.data?.connections ?? [];
  const memoryRatio =
    memory && memory.oslimit > 0 ? Math.min(100, (memory.inuse / memory.oslimit) * 100) : null;

  const trafficPoints = trafficHistory.data?.points ?? [];
  const systemSnapshot = system.data;
  const systemReady = Boolean(systemSnapshot?.ready);
  const cpuPercent = systemSnapshot?.cpu.percent ?? 0;
  const memoryPercent = systemSnapshot?.memory.percent ?? 0;
  const cores = systemSnapshot?.cpu.cores ?? 0;
  const loadRows = [
    { label: '1 分钟', value: systemSnapshot?.load.load1 },
    { label: '5 分钟', value: systemSnapshot?.load.load5 },
    { label: '15 分钟', value: systemSnapshot?.load.load15 },
  ];
  const loadPercent = (value?: number) =>
    systemReady && value != null && cores > 0 ? (value / cores) * 100 : null;
  const loadTone = (value?: number) => {
    const percent = loadPercent(value);
    if (percent == null) return 'emerald' as const;
    return percent >= 100 ? ('rose' as const) : percent >= 75 ? ('amber' as const) : ('emerald' as const);
  };

  const refreshActive = async () => {
    if (!activeProfile) return;
    setUpdating(true);
    try {
      await api(`/profiles/${activeProfile.id}/update`, { method: 'POST' });
      toast.success('订阅已更新');
      await profiles.refresh();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setUpdating(false);
    }
  };

  const usage = activeProfile?.userInfo;
  const used =
    usage && (usage.upload !== undefined || usage.download !== undefined)
      ? (usage.upload ?? 0) + (usage.download ?? 0)
      : null;
  const usagePercent = usage?.total && used !== null ? Math.min(100, (used / usage.total) * 100) : 0;

  return (
    <div className="space-y-4">
      <Section
        title="系统负载"
        description="主机 CPU、内存与按核心数归一化的负载均值（容器内为宿主机视角）"
        actions={
          <span className="text-[11px] text-slate-500">
            {systemSnapshot?.updatedAt
              ? `更新于 ${formatRelative(systemSnapshot.updatedAt)}`
              : '采样中…'}
          </span>
        }
      >
        <div className="grid gap-4 lg:grid-cols-2">
          <div className="flex flex-col gap-3">
            <div className="flex flex-1 flex-col justify-center rounded-lg border border-line/70 bg-ink-950/40 p-3">
              <div className="flex items-baseline justify-between">
                <span className="text-xs text-slate-500">CPU</span>
                <span className="mono text-sm font-medium text-slate-200">
                  {systemReady ? `${cpuPercent.toFixed(1)}%` : '—'}
                </span>
              </div>
              <ProgressBar
                className="mt-2"
                value={systemReady ? cpuPercent : 0}
                tone={cpuPercent > 90 ? 'rose' : cpuPercent > 70 ? 'amber' : 'emerald'}
              />
              <p className="mt-1 text-[10px] text-slate-600">
                {systemSnapshot ? `${systemSnapshot.cpu.cores} 核` : '等待采样'}
              </p>
            </div>

            <div className="flex flex-1 flex-col justify-center rounded-lg border border-line/70 bg-ink-950/40 p-3">
              <div className="flex items-baseline justify-between gap-2">
                <span className="text-xs text-slate-500">系统内存</span>
                <span className="mono text-sm font-medium text-slate-200">
                  {systemReady ? `${memoryPercent.toFixed(1)}%` : '—'}
                </span>
              </div>
              <ProgressBar
                className="mt-2"
                value={systemReady ? memoryPercent : 0}
                tone={memoryPercent > 90 ? 'rose' : memoryPercent > 75 ? 'amber' : 'emerald'}
              />
              <p className="mt-1 truncate text-[10px] text-slate-600">
                {systemSnapshot && systemSnapshot.memory.total > 0
                  ? `${formatBytes(systemSnapshot.memory.used)} / ${formatBytes(systemSnapshot.memory.total)}`
                  : '等待采样'}
              </p>
            </div>
          </div>

          <div className="flex h-full flex-col rounded-lg border border-line/70 bg-ink-950/40 p-3">
            <div className="flex items-baseline justify-between gap-2">
              <span className="text-xs text-slate-500">负载均值</span>
              <span className="text-[10px] text-slate-600">
                {systemSnapshot ? `${cores} 核 · 满负载 ${cores.toFixed(2)}` : '等待采样'}
              </span>
            </div>
            <div className="mt-3 flex flex-1 flex-col justify-between gap-2.5">
              {loadRows.map(({ label, value }) => {
                const percent = loadPercent(value);
                return (
                  <div key={label}>
                    <div className="flex items-baseline justify-between gap-2">
                      <span className="text-[11px] text-slate-500">{label}</span>
                      <span className="flex items-baseline gap-2">
                        <span className="mono text-xs font-medium text-slate-200">
                          {systemReady && value != null ? value.toFixed(2) : '—'}
                        </span>
                        <span className="mono w-9 text-right text-[10px] text-slate-600">
                          {percent != null ? `${Math.round(percent)}%` : ''}
                        </span>
                      </span>
                    </div>
                    <ProgressBar
                      className="mt-1.5 h-1"
                      value={percent ?? 0}
                      tone={loadTone(value)}
                    />
                  </div>
                );
              })}
            </div>
            <p className="mt-2.5 text-[10px] leading-relaxed text-slate-600">
              百分比 = 负载 ÷ 核心数，超过 100% 表示任务在排队
            </p>
          </div>
        </div>
      </Section>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Stat
          label="实时下载"
          value={formatSpeed(traffic.down)}
          sub={connections.data ? `会话累计 ${formatBytes(connections.data.downloadTotal)}` : '等待数据'}
          icon={<ArrowDownToLine className="h-4 w-4 text-emerald-400" />}
          accent="bg-emerald-500/10"
        />
        <Stat
          label="实时上传"
          value={formatSpeed(traffic.up)}
          sub={connections.data ? `会话累计 ${formatBytes(connections.data.uploadTotal)}` : '等待数据'}
          icon={<ArrowUpFromLine className="h-4 w-4 text-sky-400" />}
          accent="bg-sky-500/10"
        />
        <Stat
          label="活动连接"
          value={running ? String(connectionList.length) : '—'}
          sub={running ? '实时连接数' : '内核未运行'}
          icon={<Cable className="h-4 w-4 text-violet-400" />}
          accent="bg-violet-500/10"
        />
        <Stat
          label="内核内存"
          value={memory ? formatBytes(memory.inuse) : '—'}
          sub={
            memory
              ? memory.oslimit > 0
                ? `系统上限 ${formatBytes(memory.oslimit)} · ${memoryRatio?.toFixed(0)}%`
                : 'mihomo 自报 Go 堆占用'
              : '等待数据'
          }
          hint="由 mihomo /memory 接口自报的 inuse（Go 堆内存），面板仅透传展示"
          icon={<MemoryStick className="h-4 w-4 text-amber-400" />}
          accent="bg-amber-500/10"
        />
      </div>

      <Section
        title="实时流量"
        description="上下行速率 · 时间范围随数据自动缩放"
        actions={
          <div className="flex items-center gap-3 text-[11px] text-slate-500">
            <span className="flex items-center gap-1.5">
              <span className="h-1.5 w-3 rounded-full bg-emerald-400" />
              下载
            </span>
            <span className="flex items-center gap-1.5">
              <span className="h-1.5 w-3 rounded-full bg-sky-400" />
              上传
            </span>
          </div>
        }
      >
        {running || trafficPoints.length > 0 ? (
          <TrafficChart points={trafficPoints} height={300} />
        ) : (
          <EmptyState
            icon={<Play className="h-8 w-8" />}
            title="内核未运行"
            description="启动 mihomo 内核后即可查看实时流量、连接与内存占用。"
            action={
              <Button variant="primary" loading={busy} onClick={() => void start()}>
                <Play className="h-3.5 w-3.5" />
                启动内核
              </Button>
            }
          />
        )}
      </Section>

      <div className="grid gap-4 xl:grid-cols-3">
        <Section
          title="当前订阅"
          description="正在使用的配置文件"
          actions={
            <Button size="sm" variant="ghost" onClick={() => navigate('/profiles')}>
              管理
              <ExternalLink className="h-3 w-3" />
            </Button>
          }
        >
          {activeProfile ? (
            <div className="space-y-4">
              <div>
                <div className="flex items-center gap-2">
                  <p className="truncate text-sm font-medium text-slate-200">
                    {activeProfile.name}
                  </p>
                  <Badge tone={activeProfile.type === 'remote' ? 'sky' : 'slate'}>
                    {activeProfile.type === 'remote' ? '订阅' : '本地'}
                  </Badge>
                </div>
                <p className="mt-1 text-[11px] text-slate-500">
                  更新于 {formatRelative(activeProfile.updatedAt)}
                </p>
              </div>

              {usage && usage.total ? (
                <div className="space-y-1.5">
                  <div className="flex justify-between text-[11px]">
                    <span className="text-slate-500">流量</span>
                    <span className="mono text-slate-400">
                      {formatBytes(used ?? 0)} / {formatBytes(usage.total)}
                    </span>
                  </div>
                  <ProgressBar value={usagePercent} tone={usagePercent > 90 ? 'rose' : 'emerald'} />
                  <p className="text-[10px] text-slate-600">
                    到期时间：{formatExpire(usage.expire)}
                  </p>
                </div>
              ) : (
                <p className="text-[11px] text-slate-600">订阅未提供流量信息</p>
              )}

              <div className="flex gap-2">
                <Button
                  size="sm"
                  className="flex-1"
                  loading={updating}
                  disabled={activeProfile.type !== 'remote'}
                  onClick={() => void refreshActive()}
                >
                  <RefreshCw className="h-3.5 w-3.5" />
                  更新订阅
                </Button>
                <Button size="sm" variant="ghost" onClick={() => navigate('/settings')}>
                  配置
                </Button>
              </div>
            </div>
          ) : (
            <EmptyState
              icon={<FileStack className="h-7 w-7" />}
              title="尚未添加订阅"
              description="添加 Clash/Mihomo 订阅链接或本地 YAML 配置后即可开始使用。"
              action={
                <Button variant="primary" size="sm" onClick={() => navigate('/profiles')}>
                  添加订阅
                </Button>
              }
            />
          )}
        </Section>

        <Section
          className="xl:col-span-2"
          title="最近内核日志"
          description="mihomo 进程标准输出（最近 12 行）"
          actions={
            <Button size="sm" variant="ghost" onClick={() => navigate('/logs')}>
              查看全部
              <ExternalLink className="h-3 w-3" />
            </Button>
          }
          bodyClassName="p-0"
        >
          <div className="h-[188px] overflow-y-auto bg-ink-950/60 p-4">
            {kernelLogs.data?.logs.length ? (
              <pre className="mono whitespace-pre-wrap break-all text-[11px] leading-relaxed text-slate-500">
                {kernelLogs.data.logs.join('\n')}
              </pre>
            ) : (
              <p className="text-xs text-slate-600">暂无日志</p>
            )}
          </div>
        </Section>
      </div>
    </div>
  );
}
