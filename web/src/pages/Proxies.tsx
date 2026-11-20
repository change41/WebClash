import { useMemo, useState } from 'react';
import {
  ChevronDown,
  Gauge,
  Play,
  RefreshCw,
  Search,
  Waypoints,
} from 'lucide-react';
import { Badge, Button, EmptyState, Input, Spinner } from '../components/ui';
import { api } from '../lib/api';
import { cx, latencyText, latencyTone } from '../lib/format';
import { usePolling } from '../lib/hooks';
import { useKernel } from '../lib/kernel';
import { useToast } from '../lib/toast';
import type { ClashProxy, DelayResponse, GroupDelayResponse, ProxiesResponse } from '../types';

const SELECTABLE = new Set(['Selector']);

const TYPE_TONE: Record<string, 'emerald' | 'sky' | 'amber' | 'violet' | 'slate'> = {
  Selector: 'emerald',
  URLTest: 'sky',
  Fallback: 'amber',
  LoadBalance: 'violet',
  Relay: 'violet',
  Smart: 'sky',
};

function nodeDelay(proxy: ClashProxy | undefined, overrides: Record<string, number>): number | null {
  if (!proxy) return null;
  if (proxy.name in overrides) return overrides[proxy.name]!;
  const history = proxy.history;
  if (!history?.length) return null;
  return history[history.length - 1]?.delay ?? null;
}

export function Proxies() {
  const { status, start, busy } = useKernel();
  const toast = useToast();
  const running = Boolean(status?.running);
  const proxies = usePolling<ProxiesResponse>(() => api('/clash/proxies'), 4000, running);
  const [query, setQuery] = useState('');
  const [delays, setDelays] = useState<Record<string, number>>({});
  const [testingNodes, setTestingNodes] = useState<Set<string>>(new Set());
  const [testingGroups, setTestingGroups] = useState<Set<string>>(new Set());
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());

  const groups = useMemo(() => {
    const map = proxies.data?.proxies;
    if (!map) return [];
    return Object.values(map).filter((proxy) => Array.isArray(proxy.all));
  }, [proxies.data]);

  const keyword = query.trim().toLowerCase();

  const filteredGroups = useMemo(() => {
    if (!keyword) return groups;
    return groups
      .map((group) => ({
        ...group,
        all: (group.all ?? []).filter((name) => name.toLowerCase().includes(keyword)),
      }))
      .filter((group) => group.name.toLowerCase().includes(keyword) || group.all.length > 0);
  }, [groups, keyword]);

  const totalNodes = groups.reduce((sum, group) => sum + (group.all?.length ?? 0), 0);

  const toggleGroup = (name: string) => {
    setCollapsed((current) => {
      const next = new Set(current);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const select = async (group: string, name: string) => {
    try {
      await api('/clash/select', { method: 'POST', body: { group, name } });
      toast.success(`已切换到「${name}」`);
      await proxies.refresh();
    } catch (err) {
      toast.error((err as Error).message);
    }
  };

  const testNode = async (name: string) => {
    setTestingNodes((current) => new Set(current).add(name));
    try {
      const result = await api<DelayResponse>(
        `/clash/proxy-delay?name=${encodeURIComponent(name)}`,
      );
      setDelays((current) => ({ ...current, [name]: result.delay }));
    } catch {
      setDelays((current) => ({ ...current, [name]: 0 }));
    } finally {
      setTestingNodes((current) => {
        const next = new Set(current);
        next.delete(name);
        return next;
      });
    }
  };

  const testGroup = async (group: ClashProxy) => {
    if (!group.name || testingGroups.has(group.name)) return;
    setTestingGroups((current) => new Set(current).add(group.name));
    try {
      const result = await api<GroupDelayResponse>(
        `/clash/group-delay?name=${encodeURIComponent(group.name)}`,
      );
      setDelays((current) => ({ ...current, ...result.delay }));
      toast.success(`「${group.name}」测速完成`);
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setTestingGroups((current) => {
        const next = new Set(current);
        next.delete(group.name);
        return next;
      });
    }
  };

  if (!running) {
    return (
      <div className="card">
        <EmptyState
          icon={<Play className="h-9 w-9" />}
          title="内核未运行"
          description="启动内核后即可查看策略组、切换节点并测试延迟。"
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
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="relative min-w-[220px] flex-1 sm:max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-slate-600" />
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索策略组或节点…"
            className="pl-9"
          />
        </div>
        <div className="flex items-center gap-2 text-[11px] text-slate-500">
          <Waypoints className="h-3.5 w-3.5" />
          {groups.length} 个策略组 · {totalNodes} 个节点
        </div>
        <Button
          size="sm"
          className="ml-auto"
          loading={proxies.loading}
          onClick={() => void proxies.refresh()}
        >
          <RefreshCw className="h-3.5 w-3.5" />
          刷新
        </Button>
      </div>

      {proxies.error && (
        <div className="rounded-lg border border-rose-500/25 bg-rose-500/10 px-4 py-2.5 text-xs text-rose-300">
          {proxies.error}
        </div>
      )}

      {!proxies.data && proxies.loading ? (
        <div className="flex items-center justify-center py-20 text-slate-500">
          <Spinner className="mr-2" />
          加载策略组…
        </div>
      ) : filteredGroups.length === 0 ? (
        <div className="card">
          <EmptyState
            icon={<Waypoints className="h-9 w-9" />}
            title={keyword ? '没有匹配的策略组或节点' : '当前配置没有策略组'}
            description={
              keyword
                ? '尝试更换搜索关键词。'
                : '请确认当前订阅包含 proxy-groups 配置，或重新导入订阅。'
            }
          />
        </div>
      ) : (
        <div className="space-y-3">
          {filteredGroups.map((group) => {
            const isCollapsed = collapsed.has(group.name) && !keyword;
            const selectable = SELECTABLE.has(group.type);
            const nowProxy = group.now ? proxies.data?.proxies[group.now] : undefined;
            const groupTesting = testingGroups.has(group.name);

            return (
              <section key={group.name} className="card overflow-hidden">
                <header className="flex flex-wrap items-center gap-3 border-b border-line px-4 py-3">
                  <button
                    type="button"
                    onClick={() => toggleGroup(group.name)}
                    className="flex min-w-0 items-center gap-2 text-left"
                  >
                    <ChevronDown
                      className={cx(
                        'h-4 w-4 shrink-0 text-slate-500 transition-transform',
                        isCollapsed && '-rotate-90',
                      )}
                    />
                    <span className="truncate text-sm font-medium text-slate-200">
                      {group.name}
                    </span>
                  </button>
                  <Badge tone={TYPE_TONE[group.type] ?? 'slate'}>{group.type}</Badge>
                  <span className="text-[10px] text-slate-600">{group.all?.length ?? 0} 节点</span>

                  <div className="ml-auto flex items-center gap-2">
                    {group.now && (
                      <div className="hidden items-center gap-2 sm:flex">
                        <span className="max-w-[180px] truncate text-[11px] text-slate-400">
                          {group.now}
                        </span>
                        <span
                          className={cx(
                            'mono text-[11px]',
                            latencyTone(nodeDelay(nowProxy, delays)),
                          )}
                        >
                          {latencyText(nodeDelay(nowProxy, delays))}
                        </span>
                      </div>
                    )}
                    <Button
                      size="sm"
                      variant="ghost"
                      loading={groupTesting}
                      onClick={() => void testGroup(group)}
                      title="测试该策略组所有节点延迟"
                    >
                      <Gauge className="h-3.5 w-3.5" />
                      测速
                    </Button>
                  </div>
                </header>

                {!isCollapsed && (
                  <div className="grid grid-cols-1 gap-2 p-3 sm:grid-cols-2 xl:grid-cols-3">
                    {(group.all ?? []).map((name) => {
                      const proxy = proxies.data?.proxies[name];
                      const delay = nodeDelay(proxy, delays);
                      const selected = group.now === name;
                      const testing = testingNodes.has(name);

                      const content = (
                        <>
                          <div className="flex min-w-0 items-center gap-2">
                            <span
                              className={cx(
                                'h-1.5 w-1.5 shrink-0 rounded-full',
                                selected ? 'bg-emerald-400' : 'bg-slate-600',
                              )}
                            />
                            <span
                              className={cx(
                                'truncate text-xs',
                                selected ? 'text-emerald-200' : 'text-slate-300',
                              )}
                              title={name}
                            >
                              {name}
                            </span>
                          </div>
                          <div className="flex shrink-0 items-center gap-2">
                            <span className="hidden text-[10px] text-slate-600 sm:inline">
                              {proxy?.type ?? ''}
                            </span>
                            <button
                              type="button"
                              onClick={(event) => {
                                event.stopPropagation();
                                void testNode(name);
                              }}
                              className={cx(
                                'mono rounded px-1.5 py-0.5 text-[10px] transition hover:bg-white/5',
                                latencyTone(delay),
                              )}
                              title="测试延迟"
                            >
                              {testing ? '…' : latencyText(delay)}
                            </button>
                          </div>
                        </>
                      );

                      if (!selectable) {
                        return (
                          <div
                            key={name}
                            className={cx(
                              'flex items-center justify-between gap-2 rounded-lg border px-3 py-2',
                              selected
                                ? 'border-emerald-400/40 bg-emerald-400/[0.07]'
                                : 'border-line bg-white/[0.015]',
                            )}
                          >
                            {content}
                          </div>
                        );
                      }

                      return (
                        <div
                          key={name}
                          role="button"
                          tabIndex={0}
                          onClick={() => void select(group.name, name)}
                          onKeyDown={(event) => {
                            if (event.key === 'Enter' || event.key === ' ') {
                              event.preventDefault();
                              void select(group.name, name);
                            }
                          }}
                          className={cx(
                            'flex cursor-pointer items-center justify-between gap-2 rounded-lg border px-3 py-2 text-left transition',
                            selected
                              ? 'border-emerald-400/50 bg-emerald-400/10 ring-1 ring-emerald-400/20'
                              : 'border-line bg-white/[0.015] hover:border-line-strong hover:bg-white/[0.04]',
                          )}
                        >
                          {content}
                        </div>
                      );
                    })}
                  </div>
                )}
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}
