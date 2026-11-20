import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Download, FileText, ListFilter, ListPlus, Play, RefreshCw, Search } from 'lucide-react';
import {
  Badge,
  Button,
  CopyButton,
  EmptyState,
  Input,
  Modal,
  Segmented,
  Spinner,
} from '../components/ui';
import { api } from '../lib/api';
import { formatDateTime } from '../lib/format';
import { usePolling } from '../lib/hooks';
import { useKernel } from '../lib/kernel';
import { useToast } from '../lib/toast';
import {
  classifyRuleSources,
  RULE_SOURCE_LABEL,
  ruleTypeLabel,
  type RuleSource,
} from '../lib/rules';
import { CustomRulesModal } from '../components/CustomRulesModal';
import type {
  AppSettings,
  ProfilesResponse,
  RuleProviderContent,
  RuleProvidersResponse,
  RulesResponse,
  SettingsResponse,
} from '../types';

type Tab = 'rules' | 'providers';

const SOURCE_LABEL: Record<RuleProviderContent['source'], string> = {
  inline: '订阅内联',
  cache: '本地缓存',
  remote: '远程订阅',
};

const RULE_TONE = (type: string) => {
  if (type === 'MATCH') return 'rose' as const;
  if (type.startsWith('GEO') || type === 'RULE-SET') return 'sky' as const;
  if (type.startsWith('IP')) return 'violet' as const;
  if (type.startsWith('DOMAIN')) return 'emerald' as const;
  if (type.startsWith('PROCESS')) return 'amber' as const;
  return 'slate' as const;
};

const SOURCE_TONE: Record<RuleSource, 'emerald' | 'amber' | 'sky' | 'slate'> = {
  profile: 'emerald',
  global: 'amber',
  subscription: 'sky',
  builtin: 'slate',
};

function parseIso(value?: string): number | null {
  if (!value) return null;
  const ts = Date.parse(value);
  if (!Number.isFinite(ts) || ts < 0) return null;
  return ts;
}

export function Rules() {
  const { status, start, busy } = useKernel();
  const running = Boolean(status?.running);
  const toast = useToast();
  const [tab, setTab] = useState<Tab>('rules');
  const [query, setQuery] = useState('');
  const [limit, setLimit] = useState(400);

  const rules = usePolling<RulesResponse>(() => api('/clash/rules'), 20000, running);
  const providers = usePolling<RuleProvidersResponse>(
    () => api('/clash/rule-providers'),
    30000,
    running && tab === 'providers',
  );

  const [scope, setScope] = useState<'global' | 'profile'>('global');
  const [appSettings, setAppSettings] = useState<AppSettings | null>(null);
  const [profilesData, setProfilesData] = useState<ProfilesResponse | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);

  const loadConfig = useCallback(async () => {
    try {
      const [settingsResult, profilesResult] = await Promise.all([
        api<SettingsResponse>('/settings'),
        api<ProfilesResponse>('/profiles'),
      ]);
      setAppSettings(settingsResult.settings);
      setProfilesData(profilesResult);
    } catch (err) {
      toast.error((err as Error).message);
    }
  }, [toast]);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  const activeProfile =
    profilesData?.profiles.find((profile) => profile.id === profilesData.activeId) ?? null;

  useEffect(() => {
    if (!activeProfile && scope === 'profile') setScope('global');
  }, [activeProfile, scope]);

  const currentRules =
    scope === 'global'
      ? (appSettings?.customRules ?? [])
      : (activeProfile?.settings.customRules ?? []);

  const scopeOptions = activeProfile
    ? [
        { value: 'global' as const, label: '全局' },
        { value: 'profile' as const, label: '当前订阅' },
      ]
    : [{ value: 'global' as const, label: '全局' }];

  const saveRules = async (nextRules: string[]) => {
    if (scope === 'global') {
      const result = await api<{ result: string; settings: AppSettings }>('/settings', {
        method: 'PUT',
        body: { customRules: nextRules },
      });
      setAppSettings(result.settings);
      toast.success(
        result.result === 'reloaded'
          ? '扩展规则已保存，内核已热重载'
          : result.result === 'restarted'
            ? '扩展规则已保存，内核已重启'
            : '扩展规则已保存（内核未运行）',
      );
    } else {
      if (!activeProfile) throw new Error('请先激活一个订阅');
      await api(`/profiles/${activeProfile.id}/settings`, {
        method: 'PUT',
        body: { customRules: nextRules },
      });
      toast.success(`「${activeProfile.name}」的扩展规则已保存`);
    }
    await loadConfig();
    if (running) void rules.refresh();
  };

  const keyword = query.trim().toLowerCase();

  const sourcedRules = useMemo(() => {
    const list = rules.data?.rules ?? [];
    const sources = classifyRuleSources(
      list,
      activeProfile?.settings.customRules ?? [],
      appSettings?.customRules ?? [],
      activeProfile !== null,
    );
    return list.map((rule, index) => ({ ...rule, source: sources[index] }));
  }, [rules.data, activeProfile, appSettings]);

  const filteredRules = useMemo(() => {
    if (!keyword) return sourcedRules;
    return sourcedRules.filter((rule) =>
      `${ruleTypeLabel(rule.type)} ${rule.type} ${rule.payload} ${rule.proxy} ${
        RULE_SOURCE_LABEL[rule.source]
      }`
        .toLowerCase()
        .includes(keyword),
    );
  }, [sourcedRules, keyword]);

  const providerList = useMemo(() => {
    const list = Object.values(providers.data?.providers ?? {});
    if (!keyword) return list;
    return list.filter((provider) =>
      `${provider.name} ${provider.behavior} ${provider.type}`.toLowerCase().includes(keyword),
    );
  }, [providers.data, keyword]);

  const [contentFor, setContentFor] = useState<string | null>(null);
  const [content, setContent] = useState<RuleProviderContent | null>(null);
  const [contentLoading, setContentLoading] = useState(false);
  const [contentError, setContentError] = useState<string | null>(null);
  const [contentQuery, setContentQuery] = useState('');
  const [contentLimit, setContentLimit] = useState(500);
  const contentToken = useRef(0);

  const loadContent = async (name: string) => {
    const token = ++contentToken.current;
    setContentFor(name);
    setContent(null);
    setContentError(null);
    setContentQuery('');
    setContentLimit(500);
    setContentLoading(true);
    try {
      const result = await api<RuleProviderContent>(
        `/clash/rule-provider-content?name=${encodeURIComponent(name)}`,
      );
      if (token !== contentToken.current) return;
      setContent(result);
    } catch (err) {
      if (token !== contentToken.current) return;
      setContentError((err as Error).message);
    } finally {
      if (token === contentToken.current) setContentLoading(false);
    }
  };

  const closeContent = () => {
    contentToken.current += 1;
    setContentFor(null);
    setContentLoading(false);
  };

  const contentKeyword = contentQuery.trim().toLowerCase();
  const contentRules = useMemo(() => {
    const list = content?.rules ?? [];
    if (!contentKeyword) return list;
    return list.filter((rule) => rule.toLowerCase().includes(contentKeyword));
  }, [content, contentKeyword]);

  const downloadContent = () => {
    if (!content) return;
    const blob = new Blob([`${content.rules.join('\n')}\n`], {
      type: 'text/plain;charset=utf-8',
    });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `${content.name}.txt`;
    link.click();
    URL.revokeObjectURL(url);
  };

  const loading = tab === 'rules' ? rules.loading : providers.loading;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        {running ? (
          <Segmented
            value={tab}
            options={[
              { value: 'rules', label: `规则 ${rules.data ? `(${rules.data.rules.length})` : ''}` },
              { value: 'providers', label: '规则集' },
            ]}
            onChange={(value) => setTab(value)}
          />
        ) : (
          <span className="text-xs text-slate-500">内核未运行，扩展规则仍可编辑</span>
        )}
        {running && (
          <div className="relative min-w-[200px] flex-1 sm:max-w-xs">
            <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-slate-600" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={tab === 'rules' ? '搜索规则类型、内容或策略…' : '搜索规则集…'}
              className="pl-9"
            />
          </div>
        )}
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <Segmented value={scope} options={scopeOptions} onChange={(value) => setScope(value)} />
          <Button size="sm" onClick={() => setEditorOpen(true)} disabled={!appSettings}>
            <ListPlus className="h-3.5 w-3.5" />
            扩展规则
          </Button>
          {running && (
            <Button
              size="sm"
              loading={loading}
              onClick={() => void (tab === 'rules' ? rules.refresh() : providers.refresh())}
            >
              <RefreshCw className="h-3.5 w-3.5" />
              刷新
            </Button>
          )}
        </div>
      </div>

      {!running ? (
        <div className="card">
          <EmptyState
            icon={<Play className="h-9 w-9" />}
            title="内核未运行"
            description="启动内核后可查看当前生效的规则与规则集；扩展规则可随时编辑，保存后在内核运行或下次启动时生效。"
            action={
              <Button variant="primary" loading={busy} onClick={() => void start()}>
                <Play className="h-3.5 w-3.5" />
                启动内核
              </Button>
            }
          />
        </div>
      ) : tab === 'rules' ? (
        <div className="card overflow-hidden">
          {!rules.data && rules.loading ? (
            <div className="flex items-center justify-center py-20 text-slate-500">
              <Spinner className="mr-2" />
              加载规则…
            </div>
          ) : filteredRules.length === 0 ? (
            <EmptyState
              icon={<ListFilter className="h-8 w-8" />}
              title="没有匹配的规则"
              description="当前配置未启用任何规则，或搜索关键词无匹配。"
            />
          ) : (
            <>
              <div className="border-b border-line bg-ink-950/30 px-4 py-2 text-[10px] leading-relaxed text-slate-500">
                <span className="text-slate-400">来源：</span>
                「订阅扩展」「全局扩展」为面板中添加的规则，「订阅」为订阅本身；未激活订阅时显示「内置」。
                <span className="ml-2 text-slate-400">状态：</span>
                内核返回的只读信息，显示是否被禁用或 GEOIP / GEOSITE 已加载的记录数，不是可编辑设置。
              </div>
              <div className="max-h-[calc(100vh-260px)] overflow-auto">
                <table className="w-full min-w-[860px] border-collapse text-left text-xs">
                  <thead className="sticky top-0 z-10 bg-ink-850 text-[10px] uppercase tracking-wider text-slate-500">
                    <tr>
                      <th className="w-14 px-4 py-2.5 font-medium">#</th>
                      <th className="w-36 px-3 py-2.5 font-medium">类型</th>
                      <th className="px-3 py-2.5 font-medium">内容</th>
                      <th className="w-40 px-3 py-2.5 font-medium">策略</th>
                      <th className="w-24 px-3 py-2.5 font-medium">来源</th>
                      <th
                        className="w-28 px-3 py-2.5 font-medium"
                        title="内核返回的只读状态，不是可编辑设置"
                      >
                        状态
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredRules.slice(0, limit).map((rule, index) => (
                      <tr
                        key={`${rule.type}-${rule.payload}-${index}`}
                        className="border-t border-line/70 hover:bg-white/[0.02]"
                      >
                        <td className="mono px-4 py-2 text-[10px] text-slate-600">{index + 1}</td>
                        <td className="px-3 py-2">
                          <Badge tone={RULE_TONE(ruleTypeLabel(rule.type))}>
                            {ruleTypeLabel(rule.type)}
                          </Badge>
                        </td>
                        <td className="mono max-w-[420px] truncate px-3 py-2 text-[11px] text-slate-300" title={rule.payload}>
                          {rule.payload || '—'}
                        </td>
                        <td className="px-3 py-2">
                          <span className="rounded bg-white/[0.04] px-1.5 py-0.5 text-[10px] text-slate-400">
                            {rule.proxy}
                          </span>
                        </td>
                        <td className="px-3 py-2">
                          <Badge tone={SOURCE_TONE[rule.source]}>{RULE_SOURCE_LABEL[rule.source]}</Badge>
                        </td>
                        <td className="px-3 py-2 text-[10px] text-slate-600">
                          {rule.extra?.disabled ? (
                            <span className="text-amber-300">已禁用</span>
                          ) : rule.size !== undefined && rule.size >= 0 ? (
                            <span title="GEOIP / GEOSITE 已加载的记录数">记录 {rule.size}</span>
                          ) : (
                            '—'
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {filteredRules.length > limit && (
                <div className="border-t border-line p-3 text-center">
                  <Button size="sm" variant="ghost" onClick={() => setLimit((value) => value + 400)}>
                    显示更多（剩余 {filteredRules.length - limit} 条）
                  </Button>
                </div>
              )}
            </>
          )}
        </div>
      ) : (
        <div className="card overflow-hidden">
          {!providers.data && providers.loading ? (
            <div className="flex items-center justify-center py-20 text-slate-500">
              <Spinner className="mr-2" />
              加载规则集…
            </div>
          ) : providerList.length === 0 ? (
            <EmptyState
              icon={<ListFilter className="h-8 w-8" />}
              title="没有规则集"
              description="当前配置未使用 rule-providers。"
            />
          ) : (
            <div className="overflow-auto">
              <table className="w-full min-w-[800px] border-collapse text-left text-xs">
                <thead className="bg-ink-850 text-[10px] uppercase tracking-wider text-slate-500">
                  <tr>
                    <th className="px-4 py-2.5 font-medium">名称</th>
                    <th className="px-3 py-2.5 font-medium">类型</th>
                    <th className="px-3 py-2.5 font-medium">行为</th>
                    <th className="px-3 py-2.5 text-right font-medium">规则数</th>
                    <th className="px-3 py-2.5 font-medium">来源</th>
                    <th className="px-3 py-2.5 font-medium">更新时间</th>
                    <th className="w-24 px-3 py-2.5 font-medium">内容</th>
                  </tr>
                </thead>
                <tbody>
                  {providerList.map((provider) => {
                    const updated = parseIso(provider.updatedAt);
                    return (
                      <tr
                        key={provider.name}
                        className="border-t border-line/70 hover:bg-white/[0.02]"
                      >
                        <td className="px-4 py-2.5 font-medium text-slate-200">{provider.name}</td>
                        <td className="px-3 py-2.5 text-slate-400">{provider.type}</td>
                        <td className="px-3 py-2.5">
                          <Badge tone="sky">{provider.behavior}</Badge>
                        </td>
                        <td className="mono px-3 py-2.5 text-right text-slate-300">
                          {provider.ruleCount}
                        </td>
                        <td className="px-3 py-2.5 text-slate-500">{provider.vehicleType}</td>
                        <td className="px-3 py-2.5 text-slate-500">
                          {updated ? formatDateTime(updated) : '—'}
                        </td>
                        <td className="px-3 py-2">
                          <Button
                            size="sm"
                            variant="ghost"
                            title={`查看 ${provider.name} 的规则内容`}
                            onClick={() => void loadContent(provider.name)}
                          >
                            <FileText className="h-3.5 w-3.5" />
                            查看
                          </Button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      <Modal
        open={contentFor !== null}
        onClose={closeContent}
        title={`规则集 · ${contentFor ?? ''}`}
        description={
          content
            ? `${content.behavior} · ${content.format} · ${content.vehicleType} · 来源：${SOURCE_LABEL[content.source]} · 共 ${content.rules.length} 条`
            : '正在读取规则集内容'
        }
        width="max-w-3xl"
        footer={
          <>
            <Button
              size="sm"
              variant="ghost"
              loading={contentLoading}
              onClick={() => {
                if (contentFor) void loadContent(contentFor);
              }}
            >
              <RefreshCw className="h-3.5 w-3.5" />
              重新加载
            </Button>
            <Button size="sm" variant="ghost" disabled={!content} onClick={downloadContent}>
              <Download className="h-3.5 w-3.5" />
              下载
            </Button>
            {content && <CopyButton value={content.rules.join('\n')} label="复制全部" />}
            <Button size="sm" onClick={closeContent}>
              关闭
            </Button>
          </>
        }
      >
        {contentLoading ? (
          <div className="flex items-center justify-center py-16 text-slate-500">
            <Spinner className="mr-2" />
            加载规则内容…
          </div>
        ) : contentError ? (
          <div className="rounded-lg border border-rose-500/30 bg-rose-500/5 px-4 py-8 text-center">
            <p className="text-sm leading-relaxed text-rose-300">{contentError}</p>
            <Button
              size="sm"
              className="mt-4"
              onClick={() => {
                if (contentFor) void loadContent(contentFor);
              }}
            >
              <RefreshCw className="h-3.5 w-3.5" />
              重试
            </Button>
          </div>
        ) : content ? (
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <div className="relative flex-1">
                <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-slate-600" />
                <Input
                  value={contentQuery}
                  onChange={(event) => {
                    setContentQuery(event.target.value);
                    setContentLimit(500);
                  }}
                  placeholder="过滤规则内容…"
                  className="pl-9"
                />
              </div>
              <span className="shrink-0 text-[11px] text-slate-500">
                {contentRules.length} / {content.rules.length} 条
              </span>
            </div>

            <div className="max-h-[52vh] overflow-auto rounded-lg border border-line bg-ink-950/70 px-3 py-2">
              {contentRules.length === 0 ? (
                <p className="py-10 text-center text-xs text-slate-600">
                  {content.rules.length === 0 ? '该规则集暂无规则' : '没有匹配的规则'}
                </p>
              ) : (
                <div className="space-y-0.5">
                  {contentRules.slice(0, contentLimit).map((rule, index) => {
                    const comma = rule.indexOf(',');
                    return (
                      <div
                        key={`${index}-${rule}`}
                        className="mono flex gap-2 rounded px-1 py-0.5 text-[11px] leading-relaxed hover:bg-white/[0.03]"
                      >
                        <span className="w-10 shrink-0 text-right text-slate-600">{index + 1}</span>
                        <span className="min-w-0 break-all whitespace-pre-wrap text-slate-300">
                          {comma > 0 ? (
                            <>
                              <span className="text-emerald-400">{rule.slice(0, comma)}</span>
                              <span className="text-slate-600">,</span>
                              {rule.slice(comma + 1)}
                            </>
                          ) : (
                            rule
                          )}
                        </span>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>

            {contentRules.length > contentLimit && (
              <div className="text-center">
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setContentLimit((value) => value + 500)}
                >
                  显示更多（剩余 {contentRules.length - contentLimit} 条）
                </Button>
              </div>
            )}
          </div>
        ) : null}
      </Modal>

      <CustomRulesModal
        open={editorOpen}
        onClose={() => setEditorOpen(false)}
        title={`扩展规则 · ${scope === 'global' ? '全局' : (activeProfile?.name ?? '当前订阅')}`}
        description={
          scope === 'global'
            ? '对所有订阅生效，插入在订阅规则之前；当前订阅的扩展规则仍更优先。'
            : `仅对「${activeProfile?.name ?? ''}」生效，插入在订阅规则与全局扩展规则之前。`
        }
        rules={currentRules}
        onSave={saveRules}
      />
    </div>
  );
}
