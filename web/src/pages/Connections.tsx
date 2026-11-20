import { useEffect, useMemo, useState } from 'react';
import { Cable, History, Play, Search, Trash2, X } from 'lucide-react';
import {
  Badge,
  Button,
  ConfirmDialog,
  EmptyState,
  Input,
} from '../components/ui';
import { api } from '../lib/api';
import {
  cx,
  formatBytes,
  formatDateTime,
  formatDuration,
  formatRelative,
} from '../lib/format';
import { countryFlagClass, formatRegion, useGeoIPStatus, useGeoLookup } from '../lib/geoip';
import { useNow, usePolling, useWsJson } from '../lib/hooks';
import { useKernel } from '../lib/kernel';
import { useToast } from '../lib/toast';
import type {
  ClashConnection,
  ConnectionHistoryResponse,
  ConnectionsPayload,
  GeoIPRegion,
  HistoryConnection,
} from '../types';

const NO_CONNECTIONS: ClashConnection[] = [];

function isFakeIP(connection: ClashConnection): boolean {
  return connection.metadata.dnsMode === 'fake-ip';
}

/**
 * 真实拨号地址：fake-ip 模式下内核上报的 destinationIP 是伪造地址
 * （198.18.x.x，归属库会查成 Reserved），真实目标在 remoteDestination。
 */
function realAddressOf(connection: ClashConnection): string {
  const metadata = connection.metadata;
  if (metadata.remoteDestination) return metadata.remoteDestination;
  if (!isFakeIP(connection) && metadata.destinationIP) return metadata.destinationIP;
  return '';
}

function targetOf(connection: ClashConnection): string {
  return connection.metadata.host || realAddressOf(connection) || '未知目标';
}

/**
 * 归属查询键：优先真实拨号地址；仅有域名时退回 host，
 * 由后端把域名解析成 IP 后再查归属。
 */
function geoKeyOf(connection: ClashConnection): string {
  return realAddressOf(connection) || connection.metadata.host || '';
}

/** 源地址归属查询键；源地址总是 IP，不需要域名解析。 */
function sourceGeoKeyOf(connection: ClashConnection): string {
  return connection.metadata.sourceIP?.trim() ?? '';
}

function isLoopbackAddress(ip: string): boolean {
  const lower = ip.toLowerCase();
  return lower === '::1' || lower.startsWith('127.') || lower.startsWith('::ffff:127.');
}

function isPrivateAddress(ip: string): boolean {
  const lower = ip.toLowerCase();
  if (lower.startsWith('::ffff:')) return isPrivateAddress(lower.slice(7));
  if (lower.startsWith('10.') || lower.startsWith('192.168.') || lower.startsWith('169.254.')) {
    return true;
  }
  if (lower.startsWith('172.')) {
    const second = Number(lower.split('.')[1]);
    if (second >= 16 && second <= 31) return true;
  }
  return lower.startsWith('fc') || lower.startsWith('fd') || /^fe[89ab]/.test(lower);
}

/** 源地址通常是回环 / 局域网地址，归属库会返回 Reserved，用更直观的标签代替。 */
function formatSourceRegion(region: GeoIPRegion | null, ip: string): string {
  if (!region) return '';
  if (region.country === 'Reserved') {
    if (isLoopbackAddress(ip)) return '本机';
    if (isPrivateAddress(ip)) return '局域网';
  }
  return formatRegion(region);
}

type ViewMode = 'live' | 'closed';

export function Connections() {
  const { status, start, busy } = useKernel();
  const toast = useToast();
  const running = Boolean(status?.running);
  const now = useNow(1000);
  const [view, setView] = useState<ViewMode>('live');
  const [snapshot, setSnapshot] = useState<ConnectionsPayload | null>(null);
  const [query, setQuery] = useState('');
  const [confirmClear, setConfirmClear] = useState(false);
  const [clearing, setClearing] = useState(false);

  const geoPoll = useGeoIPStatus(running || view === 'closed');
  const geoActive = Boolean(geoPoll.data?.enable && geoPoll.data?.ready);
  const { regionOf, requestLookup, version: geoVersion } = useGeoLookup(geoActive);

  const connected = useWsJson<ConnectionsPayload>(running ? '/ws/connections?interval=1000' : null, (message) => {
    if (message && Array.isArray(message.connections)) {
      setSnapshot(message);
    } else if (message && message.connections === null) {
      setSnapshot({ ...message, connections: [] });
    }
  });

  const history = usePolling<ConnectionHistoryResponse>(
    () => api('/connections/history?limit=500'),
    5000,
  );

  const connections = snapshot?.connections ?? NO_CONNECTIONS;
  const closed = history.data?.connections ?? NO_CONNECTIONS;
  const items: ClashConnection[] = view === 'live' ? connections : closed;
  const keyword = query.trim().toLowerCase();

  const filtered = useMemo(() => {
    if (!keyword) return items;
    return items.filter((connection) => {
      const region = regionOf(geoKeyOf(connection));
      const sourceRegion = regionOf(sourceGeoKeyOf(connection));
      const sourceLabel = formatSourceRegion(sourceRegion, connection.metadata.sourceIP);
      const haystack = [
        connection.metadata.host,
        connection.metadata.destinationIP,
        connection.metadata.remoteDestination,
        connection.metadata.destinationPort,
        connection.metadata.sourceIP,
        connection.metadata.sourcePort,
        connection.metadata.type,
        connection.metadata.network,
        connection.metadata.processPath,
        connection.rule,
        connection.rulePayload,
        connection.chains.join(' '),
        region
          ? `${region.country} ${region.countryCode} ${region.province} ${region.city} ${region.isp}`
          : '',
        sourceRegion
          ? `${sourceRegion.country} ${sourceRegion.countryCode} ${sourceRegion.province} ${sourceRegion.city} ${sourceRegion.isp}`
          : '',
        sourceLabel,
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
      return haystack.includes(keyword);
    });
  }, [items, keyword, regionOf, geoVersion]);

  useEffect(() => {
    if (!geoActive) return;
    const keys = items
      .flatMap((connection) => [geoKeyOf(connection), sourceGeoKeyOf(connection)])
      .filter(Boolean);
    if (keys.length > 0) requestLookup(keys);
  }, [items, geoActive, requestLookup]);

  const closeOne = async (id: string) => {
    try {
      await api(`/clash/connections/${encodeURIComponent(id)}`, { method: 'DELETE' });
      setSnapshot((current) =>
        current
          ? {
              ...current,
              connections: (current.connections ?? []).filter((item) => item.id !== id),
            }
          : current,
      );
    } catch (err) {
      toast.error((err as Error).message);
    }
  };

  const closeAll = async () => {
    setClearing(true);
    try {
      await api('/clash/connections', { method: 'DELETE' });
      setSnapshot((current) => (current ? { ...current, connections: [] } : current));
      toast.success('已关闭全部连接');
      setConfirmClear(false);
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setClearing(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="inline-flex rounded-lg border border-line bg-ink-950/60 p-0.5 text-[11px]">
          <button
            type="button"
            onClick={() => setView('live')}
            className={cx(
              'rounded-md px-3 py-1.5 transition',
              view === 'live'
                ? 'bg-white/[0.08] text-slate-200'
                : 'text-slate-500 hover:text-slate-300',
            )}
          >
            活跃 {connections.length}
          </button>
          <button
            type="button"
            onClick={() => setView('closed')}
            className={cx(
              'rounded-md px-3 py-1.5 transition',
              view === 'closed'
                ? 'bg-white/[0.08] text-slate-200'
                : 'text-slate-500 hover:text-slate-300',
            )}
          >
            已关闭 {closed.length}
          </button>
        </div>
        <div className="relative min-w-[200px] flex-1 sm:max-w-sm">
          <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-slate-600" />
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索主机、IP、归属、规则或链路…"
            className="pl-9"
          />
        </div>
        <div className="flex items-center gap-3 text-[11px] text-slate-500">
          {view === 'live' ? (
            <>
              <span className="inline-flex items-center gap-1.5">
                <Cable className="h-3.5 w-3.5" />
                {connections.length} 条连接
              </span>
              <span className="mono hidden sm:inline">
                ↓ {formatBytes(snapshot?.downloadTotal ?? 0)} · ↑ {formatBytes(snapshot?.uploadTotal ?? 0)}
              </span>
              {!connected && (
                <Badge tone="amber">重连中</Badge>
              )}
            </>
          ) : (
            <span className="inline-flex items-center gap-1.5">
              <History className="h-3.5 w-3.5" />
              {closed.length} 条历史
              {history.data?.limit ? (
                <span className="text-slate-600">· 最多保留 {history.data.limit} 条</span>
              ) : null}
            </span>
          )}
        </div>
        {view === 'live' && (
          <Button
            size="sm"
            variant="danger"
            className="ml-auto"
            disabled={connections.length === 0}
            onClick={() => setConfirmClear(true)}
          >
            <Trash2 className="h-3.5 w-3.5" />
            关闭全部
          </Button>
        )}
      </div>

      {view === 'live' && !running ? (
        <div className="card">
          <EmptyState
            icon={<Play className="h-9 w-9" />}
            title="内核未运行"
            description="启动内核后可查看实时连接、关闭连接并观察流量。"
            action={
              <Button variant="primary" loading={busy} onClick={() => void start()}>
                <Play className="h-3.5 w-3.5" />
                启动内核
              </Button>
            }
          />
        </div>
      ) : (
        <div className="card overflow-hidden">
          <div className="max-h-[calc(100vh-220px)] overflow-auto">
            <table className="w-full min-w-[1100px] border-collapse text-left text-xs">
              <thead className="sticky top-0 z-10 bg-ink-850 text-[10px] uppercase tracking-wider text-slate-500">
                <tr>
                  <th className="px-4 py-2.5 font-medium">目标</th>
                  <th className="px-3 py-2.5 font-medium">来源</th>
                  <th className="px-3 py-2.5 font-medium">归属</th>
                  <th className="px-3 py-2.5 font-medium">规则</th>
                  <th className="px-3 py-2.5 font-medium">链路</th>
                  <th className="px-3 py-2.5 font-medium">网络</th>
                  <th className="px-3 py-2.5 text-right font-medium">流量</th>
                  <th className="px-3 py-2.5 text-right font-medium">时长</th>
                  <th className="px-3 py-2.5">{view === 'closed' ? '关闭于' : null}</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((connection) => {
                  const started = Date.parse(connection.start);
                  const closedAt =
                    view === 'closed' ? (connection as HistoryConnection).closedAt : 0;
                  const duration = Number.isFinite(started)
                    ? (closedAt || now) - started
                    : 0;
                  const region = geoActive ? regionOf(geoKeyOf(connection)) : null;
                  const regionText = formatRegion(region);
                  const sourceRegion = geoActive ? regionOf(sourceGeoKeyOf(connection)) : null;
                  const sourceLabel =
                    formatSourceRegion(sourceRegion, connection.metadata.sourceIP) ||
                    sourceRegion?.countryCode ||
                    '';
                  const sourceFlagClass = countryFlagClass(sourceRegion?.countryCode);
                  const targetFlagClass = countryFlagClass(region?.countryCode);
                  const targetKnown = Boolean(regionText || region?.countryCode);
                  const address =
                    realAddressOf(connection) || connection.metadata.destinationIP;
                  const regionTitle = [
                    sourceLabel
                      ? `${connection.metadata.sourceIP || '源'} ${sourceRegion?.raw || sourceLabel}`
                      : '',
                    targetKnown
                      ? `${address || '目标'} ${region?.raw || regionText || region?.countryCode}`
                      : '未知',
                  ]
                    .filter(Boolean)
                    .join(' → ');
                  return (
                    <tr
                      key={connection.id}
                      className="group border-t border-line/70 transition hover:bg-white/[0.02]"
                    >
                      <td className="max-w-[240px] px-4 py-2.5">
                        <div className="truncate font-medium text-slate-200" title={targetOf(connection)}>
                          {targetOf(connection)}
                        </div>
                        <div className="mono mt-0.5 truncate text-[10px] text-slate-600">
                          {address
                            ? `${address}:${connection.metadata.destinationPort}`
                            : '—'}
                        </div>
                      </td>
                      <td className="max-w-[200px] px-3 py-2.5">
                        <div
                          className="mono truncate text-[11px] text-slate-300"
                          title={`${connection.metadata.sourceIP}:${connection.metadata.sourcePort}`}
                        >
                          {connection.metadata.sourceIP || '—'}:{connection.metadata.sourcePort}
                        </div>
                        {connection.metadata.processPath && (
                          <div
                            className="mt-0.5 truncate text-[10px] text-slate-600"
                            title={connection.metadata.processPath}
                          >
                            {connection.metadata.processPath.split('/').pop()}
                          </div>
                        )}
                      </td>
                      <td className="max-w-[240px] px-3 py-2.5">
                        {!geoActive ? (
                          <span className="text-slate-700" title="IP 归属未启用或数据库未就绪">
                            —
                          </span>
                        ) : sourceLabel || targetKnown ? (
                          <div className="truncate text-slate-300" title={regionTitle}>
                            {sourceLabel && (
                              <>
                                <span className="text-slate-400">
                                  {sourceFlagClass && (
                                    <span
                                      aria-hidden="true"
                                      className={cx('mr-1 align-[-0.125em]', sourceFlagClass)}
                                    />
                                  )}
                                  {sourceLabel}
                                </span>
                                <span className="text-slate-600"> → </span>
                              </>
                            )}
                            {regionText ? (
                              <>
                                {region?.countryCode && (
                                  <>
                                    {targetFlagClass && (
                                      <span
                                        aria-hidden="true"
                                        className={cx('mr-1 align-[-0.125em]', targetFlagClass)}
                                      />
                                    )}
                                    <span className="mono text-[10px] text-slate-500">
                                      {region.countryCode}
                                    </span>
                                  </>
                                )}
                                {' '}
                                {regionText}
                              </>
                            ) : region?.countryCode ? (
                              <span className="mono text-[10px] text-slate-500">
                                {targetFlagClass && (
                                  <span
                                    aria-hidden="true"
                                    className={cx('mr-1 align-[-0.125em]', targetFlagClass)}
                                  />
                                )}
                                {region.countryCode}
                              </span>
                            ) : (
                              <span className="text-slate-600">未知</span>
                            )}
                          </div>
                        ) : (
                          <span className="text-slate-600">未知</span>
                        )}
                      </td>
                      <td className="max-w-[200px] px-3 py-2.5">
                        <div className="flex items-center gap-1.5">
                          <span className="rounded bg-white/[0.05] px-1.5 py-0.5 text-[10px] text-slate-400">
                            {connection.rule}
                          </span>
                        </div>
                        <div className="mt-0.5 truncate text-[10px] text-slate-600" title={connection.rulePayload}>
                          {connection.rulePayload}
                        </div>
                      </td>
                      <td className="max-w-[220px] px-3 py-2.5">
                        <div className="flex flex-wrap items-center gap-1">
                          {connection.chains.slice().reverse().map((chain, index) => (
                            <span key={`${chain}-${index}`} className="flex items-center gap-1">
                              {index > 0 && <span className="text-slate-600">→</span>}
                              <span className="rounded bg-white/[0.04] px-1.5 py-0.5 text-[10px] text-slate-400">
                                {chain}
                              </span>
                            </span>
                          ))}
                        </div>
                      </td>
                      <td className="px-3 py-2.5">
                        <span className="text-slate-400">{connection.metadata.network.toUpperCase()}</span>
                        <span className="ml-1 text-[10px] text-slate-600">{connection.metadata.type}</span>
                      </td>
                      <td className="mono px-3 py-2.5 text-right text-[10px]">
                        <div className="text-emerald-400/90">↓ {formatBytes(connection.download)}</div>
                        <div className="text-sky-400/90">↑ {formatBytes(connection.upload)}</div>
                      </td>
                      <td className="px-3 py-2.5 text-right text-[11px] text-slate-400">
                        {formatDuration(duration)}
                      </td>
                      <td className="px-3 py-2.5 text-right">
                        {view === 'live' ? (
                          <button
                            type="button"
                            title="关闭连接"
                            onClick={() => void closeOne(connection.id)}
                            className="rounded p-1 text-slate-600 transition hover:bg-rose-500/10 hover:text-rose-400"
                          >
                            <X className="h-3.5 w-3.5" />
                          </button>
                        ) : (
                          <span
                            className="text-[10px] whitespace-nowrap text-slate-500"
                            title={formatDateTime(closedAt)}
                          >
                            {formatRelative(closedAt)}
                          </span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>

            {filtered.length === 0 &&
              (view === 'live' ? (
                <EmptyState
                  icon={<Cable className="h-8 w-8" />}
                  title={keyword ? '没有匹配的连接' : '暂无活动连接'}
                  description={
                    keyword
                      ? '尝试更换搜索关键词。'
                      : '流量经过代理后，连接会实时显示在这里。'
                  }
                />
              ) : (
                <EmptyState
                  icon={<History className="h-8 w-8" />}
                  title={keyword ? '没有匹配的历史连接' : '暂无已关闭连接'}
                  description={
                    keyword
                      ? '尝试更换搜索关键词。'
                      : '连接关闭后会保留在面板内存中，便于排查；重启面板后清空。'
                  }
                />
              ))}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={confirmClear}
        title="关闭全部连接"
        description={`将强制关闭当前 ${connections.length} 条连接，相关应用可能会重新建立连接。`}
        confirmText="全部关闭"
        danger
        busy={clearing}
        onCancel={() => setConfirmClear(false)}
        onConfirm={() => void closeAll()}
      />
    </div>
  );
}
