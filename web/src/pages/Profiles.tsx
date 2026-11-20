import { useState } from 'react';
import {
  CheckCircle2,
  Clock,
  Eye,
  FileCode2,
  FileStack,
  Link2,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Timer,
  Trash2,
} from 'lucide-react';
import {
  Badge,
  Button,
  ConfirmDialog,
  EmptyState,
  Field,
  Input,
  Modal,
  ProgressBar,
  Segmented,
  Spinner,
  Textarea,
} from '../components/ui';
import { api } from '../lib/api';
import { cx, formatBytes, formatDateTime, formatExpire, formatRelative } from '../lib/format';
import { usePolling } from '../lib/hooks';
import { parseRuleText, rulesToText } from '../lib/rules';
import { useToast } from '../lib/toast';
import type { ProfileMeta, ProfileSettings, ProfilesResponse } from '../types';

interface UpdateAllResult {
  updated: string[];
  failed: { id: string; name: string; error: string }[];
}

function hostOf(url?: string): string {
  if (!url) return '本地 YAML';
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

function UsageCell({ profile }: { profile: ProfileMeta }) {
  const info = profile.userInfo;
  if (!info?.total) {
    return <p className="text-[11px] text-slate-600">未提供流量信息</p>;
  }
  const used = (info.upload ?? 0) + (info.download ?? 0);
  const percent = Math.min(100, (used / info.total) * 100);
  const expired = Boolean(info.expire && info.expire * 1000 < Date.now());
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between gap-2 text-[10px]">
        <span className="mono text-slate-400">
          {formatBytes(used)} / {formatBytes(info.total)}
        </span>
        <span className={cx(expired ? 'text-rose-400' : 'text-slate-600')}>
          {formatExpire(info.expire)}
        </span>
      </div>
      <ProgressBar value={percent} tone={expired || percent > 90 ? 'rose' : 'emerald'} />
    </div>
  );
}

export function Profiles() {
  const toast = useToast();
  const profiles = usePolling<ProfilesResponse>(() => api('/profiles'), 10000);

  const [addOpen, setAddOpen] = useState(false);
  const [addTab, setAddTab] = useState<'url' | 'yaml'>('url');
  const [addName, setAddName] = useState('');
  const [addUrl, setAddUrl] = useState('');
  const [addContent, setAddContent] = useState('');
  const [adding, setAdding] = useState(false);

  const [editing, setEditing] = useState<ProfileMeta | null>(null);
  const [editName, setEditName] = useState('');
  const [editUrl, setEditUrl] = useState('');
  const [editContent, setEditContent] = useState('');
  const [editSettings, setEditSettings] = useState<ProfileSettings | null>(null);
  const [editRules, setEditRules] = useState('');
  const [editLoading, setEditLoading] = useState(false);
  const [editSaving, setEditSaving] = useState(false);

  const [viewing, setViewing] = useState<ProfileMeta | null>(null);
  const [viewContent, setViewContent] = useState('');
  const [viewLoading, setViewLoading] = useState(false);

  const [deleting, setDeleting] = useState<ProfileMeta | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);

  const [busyIds, setBusyIds] = useState<Set<string>>(new Set());
  const [updatingAll, setUpdatingAll] = useState(false);

  const closeEdit = () => {
    setEditing(null);
    setEditSettings(null);
    setEditRules('');
  };

  const data = profiles.data;
  const activeId = data?.activeId ?? null;

  const resetAdd = () => {
    setAddTab('url');
    setAddName('');
    setAddUrl('');
    setAddContent('');
    setAddOpen(false);
  };

  const submitAdd = async () => {
    setAdding(true);
    try {
      await api('/profiles', {
        method: 'POST',
        body:
          addTab === 'url'
            ? { name: addName || undefined, url: addUrl.trim() }
            : { name: addName || undefined, content: addContent },
      });
      toast.success('配置已添加');
      resetAdd();
      await profiles.refresh();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setAdding(false);
    }
  };

  const withBusy = async (id: string, task: () => Promise<void>) => {
    setBusyIds((current) => new Set(current).add(id));
    try {
      await task();
      await profiles.refresh();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setBusyIds((current) => {
        const next = new Set(current);
        next.delete(id);
        return next;
      });
    }
  };

  const updateOne = (profile: ProfileMeta) =>
    withBusy(profile.id, async () => {
      await api(`/profiles/${profile.id}/update`, { method: 'POST' });
      toast.success(`「${profile.name}」已更新`);
    });

  const activate = (profile: ProfileMeta) =>
    withBusy(profile.id, async () => {
      await api(`/profiles/${profile.id}/activate`, { method: 'POST' });
      toast.success(`已启用「${profile.name}」`);
    });

  const updateAll = async () => {
    setUpdatingAll(true);
    try {
      const result = await api<UpdateAllResult>('/profiles/update-all', { method: 'POST' });
      if (result.failed.length) {
        toast.error(
          `已更新 ${result.updated.length} 个，失败 ${result.failed.length} 个：${result.failed
            .map((item) => item.name)
            .join('、')}`,
        );
      } else {
        toast.success(`已更新 ${result.updated.length} 个订阅`);
      }
      await profiles.refresh();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setUpdatingAll(false);
    }
  };

  const openEdit = async (profile: ProfileMeta) => {
    setEditing(profile);
    setEditName(profile.name);
    setEditUrl(profile.url ?? '');
    setEditSettings(structuredClone(profile.settings));
    setEditRules(rulesToText(profile.settings.customRules));
    setEditContent('');
    if (profile.type === 'local') {
      setEditLoading(true);
      try {
        const result = await api<{ meta: ProfileMeta; content: string }>(
          `/profiles/${profile.id}/raw`,
        );
        setEditContent(result.content);
      } catch (err) {
        toast.error((err as Error).message);
        closeEdit();
      } finally {
        setEditLoading(false);
      }
    }
  };

  const openView = async (profile: ProfileMeta) => {
    setViewing(profile);
    setViewContent('');
    setViewLoading(true);
    try {
      const result = await api<{ meta: ProfileMeta; content: string }>(
        `/profiles/${profile.id}/raw`,
      );
      setViewContent(result.content);
    } catch (err) {
      toast.error((err as Error).message);
      setViewing(null);
    } finally {
      setViewLoading(false);
    }
  };

  const saveEdit = async () => {
    if (!editing || !editSettings) return;
    setEditSaving(true);
    try {
      if (editing.type === 'remote') {
        await api(`/profiles/${editing.id}`, {
          method: 'PUT',
          body: { name: editName.trim(), url: editUrl.trim() },
        });
      } else {
        await api(`/profiles/${editing.id}/raw`, {
          method: 'PUT',
          body: { content: editContent },
        });
        await api(`/profiles/${editing.id}`, {
          method: 'PUT',
          body: { name: editName.trim() },
        });
      }
      await api(`/profiles/${editing.id}/settings`, {
        method: 'PUT',
        body: { ...editSettings, customRules: parseRuleText(editRules) },
      });
      toast.success(`「${editName.trim()}」已保存`);
      closeEdit();
      await profiles.refresh();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setEditSaving(false);
    }
  };

  const confirmDelete = async () => {
    if (!deleting) return;
    setDeleteBusy(true);
    try {
      await api(`/profiles/${deleting.id}`, { method: 'DELETE' });
      toast.success('配置已删除');
      setDeleting(null);
      await profiles.refresh();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleteBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      {data?.lastConfigError && (
        <div className="rounded-lg border border-rose-500/25 bg-rose-500/10 px-4 py-2.5 text-xs text-rose-300">
          当前配置生成失败：{data.lastConfigError}
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="text-xs text-slate-500">
          {data?.lastConfigAt ? `配置最后应用：${formatDateTime(data.lastConfigAt)}` : '尚未应用配置'}
        </div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            loading={updatingAll}
            onClick={() => void updateAll()}
            disabled={!data?.profiles.some((profile) => profile.type === 'remote')}
          >
            <RefreshCw className="h-3.5 w-3.5" />
            全部更新
          </Button>
          <Button size="sm" variant="primary" onClick={() => setAddOpen(true)}>
            <Plus className="h-4 w-4" />
            添加配置
          </Button>
        </div>
      </div>

      <div className="card overflow-hidden">
        {!data && profiles.loading ? (
          <div className="flex items-center justify-center py-20 text-slate-500">
            <Spinner className="mr-2" />
            加载配置…
          </div>
        ) : data && data.profiles.length > 0 ? (
          <div className="divide-y divide-line">
            {data.profiles.map((profile) => {
              const active = profile.id === activeId;
              const busy = busyIds.has(profile.id);
              return (
                <div
                  key={profile.id}
                  className={cx(
                    'flex flex-col gap-3 px-4 py-4 transition sm:flex-row sm:items-center',
                    active ? 'bg-emerald-500/[0.03]' : 'hover:bg-white/[0.015]',
                  )}
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      {active ? (
                        <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400" />
                      ) : (
                        <FileStack className="h-4 w-4 shrink-0 text-slate-600" />
                      )}
                      <span className="truncate text-sm font-medium text-slate-200">
                        {profile.name}
                      </span>
                      <Badge tone={profile.type === 'remote' ? 'sky' : 'slate'}>
                        {profile.type === 'remote' ? '订阅' : '本地'}
                      </Badge>
                      {active && <Badge tone="emerald">使用中</Badge>}
                      {profile.settings.customRules.length > 0 && (
                        <Badge tone="violet">扩展规则 {profile.settings.customRules.length}</Badge>
                      )}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-slate-500">
                      <span className="inline-flex items-center gap-1 truncate">
                        {profile.type === 'remote' ? (
                          <Link2 className="h-3 w-3" />
                        ) : (
                          <FileCode2 className="h-3 w-3" />
                        )}
                        {hostOf(profile.url)}
                      </span>
                      <span className="inline-flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {formatRelative(profile.updatedAt)}
                      </span>
                      {profile.type === 'remote' && (
                        <span className="inline-flex items-center gap-1">
                          <Timer className="h-3 w-3" />
                          {profile.settings.intervalHours > 0
                            ? `每 ${profile.settings.intervalHours} 小时自动更新`
                            : '手动更新'}
                        </span>
                      )}
                    </div>
                  </div>

                  <div className="w-full shrink-0 sm:w-44">
                    <UsageCell profile={profile} />
                  </div>

                  <div className="flex shrink-0 items-center gap-1.5">
                    {!active && (
                      <Button
                        size="sm"
                        variant="primary"
                        loading={busy}
                        onClick={() => void activate(profile)}
                      >
                        启用
                      </Button>
                    )}
                    {profile.type === 'remote' && (
                      <>
                        <Button
                          size="sm"
                          variant="ghost"
                          title="从订阅更新"
                          loading={busy}
                          onClick={() => void updateOne(profile)}
                        >
                          <RefreshCw className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          title="查看订阅配置"
                          onClick={() => void openView(profile)}
                        >
                          <Eye className="h-3.5 w-3.5" />
                        </Button>
                      </>
                    )}
                    <Button
                      size="sm"
                      variant="ghost"
                      title={profile.type === 'remote' ? '编辑订阅' : '编辑配置'}
                      onClick={() => void openEdit(profile)}
                    >
                      <Pencil className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      title="删除"
                      className="text-slate-500 hover:text-rose-400"
                      onClick={() => setDeleting(profile)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        ) : (
          <EmptyState
            icon={<FileStack className="h-9 w-9" />}
            title="还没有任何配置"
            description="添加 Clash/Mihomo 订阅链接，或直接粘贴 YAML 配置。首个配置会自动启用。"
            action={
              <Button variant="primary" onClick={() => setAddOpen(true)}>
                <Plus className="h-4 w-4" />
                添加配置
              </Button>
            }
          />
        )}
      </div>

      <Modal
        open={addOpen}
        onClose={resetAdd}
        title="添加配置"
        description="支持 Clash / Mihomo 订阅链接或本地 YAML"
        width="max-w-2xl"
        footer={
          <>
            <Button variant="ghost" onClick={resetAdd} disabled={adding}>
              取消
            </Button>
            <Button
              variant="primary"
              loading={adding}
              disabled={addTab === 'url' ? !addUrl.trim() : !addContent.trim()}
              onClick={() => void submitAdd()}
            >
              添加
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="flex items-center justify-between gap-3">
            <Segmented
              value={addTab}
              options={[
                { value: 'url', label: '订阅链接' },
                { value: 'yaml', label: '本地 YAML' },
              ]}
              onChange={(value) => setAddTab(value)}
            />
            <span className="text-[10px] text-slate-600">
              {addTab === 'url' ? '请求会携带 clash.meta UA' : '内容将保存到本地'}
            </span>
          </div>

          <Field label="名称（可选）">
            <Input
              value={addName}
              onChange={(event) => setAddName(event.target.value)}
              placeholder="留空则使用订阅标题或域名"
            />
          </Field>

          {addTab === 'url' ? (
            <Field label="订阅链接" hint="支持 http/https，可带 #名称 片段。">
              <Input
                value={addUrl}
                onChange={(event) => setAddUrl(event.target.value)}
                placeholder="https://example.com/api/v1/client/subscribe?token=..."
                spellCheck={false}
              />
            </Field>
          ) : (
            <Field label="YAML 配置" hint="需包含 proxies / proxy-providers / rules 之一。">
              <Textarea
                value={addContent}
                onChange={(event) => setAddContent(event.target.value)}
                rows={14}
                placeholder={'mixed-port: 7890\nproxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT'}
                spellCheck={false}
              />
            </Field>
          )}
        </div>
      </Modal>

      <Modal
        open={Boolean(editing)}
        onClose={closeEdit}
        title={`${editing?.type === 'remote' ? '编辑订阅' : '编辑配置'}${
          editing ? ` · ${editing.name}` : ''
        }`}
        description={
          editing?.type === 'remote'
            ? '名称、订阅链接与订阅设置；修改链接后会立即重新下载并应用订阅内容'
            : '名称、YAML 内容与测速 / 扩展规则；保存后立即生效'
        }
        width="max-w-2xl"
        footer={
          <>
            <Button variant="ghost" onClick={closeEdit} disabled={editSaving}>
              取消
            </Button>
            <Button
              variant="primary"
              loading={editSaving}
              disabled={
                editLoading ||
                !editName.trim() ||
                (editing?.type === 'remote' ? !editUrl.trim() : !editContent.trim())
              }
              onClick={() => void saveEdit()}
            >
              <Save className="h-3.5 w-3.5" />
              保存并应用
            </Button>
          </>
        }
      >
        {editLoading || !editSettings ? (
          <div className="flex items-center justify-center py-16 text-sm text-slate-500">
            <Spinner className="mr-2" />
            读取配置…
          </div>
        ) : (
          <div className="space-y-4">
            <Field label="名称">
              <Input
                value={editName}
                onChange={(event) => setEditName(event.target.value)}
                placeholder={editing?.type === 'remote' ? '订阅名称' : '配置名称'}
              />
            </Field>
            {editing?.type === 'remote' ? (
              <>
                <Field label="订阅链接" hint="修改后会立即用新链接重新下载订阅内容">
                  <Input
                    value={editUrl}
                    onChange={(event) => setEditUrl(event.target.value)}
                    placeholder="https://example.com/api/v1/client/subscribe?token=..."
                    spellCheck={false}
                  />
                </Field>
                <Field label="自动更新间隔（小时）" hint="0 表示关闭自动更新，仅手动更新">
                  <Input
                    type="number"
                    min={0}
                    value={editSettings.intervalHours}
                    onChange={(event) =>
                      setEditSettings((current) =>
                        current
                          ? { ...current, intervalHours: Number(event.target.value) }
                          : current,
                      )
                    }
                  />
                </Field>
              </>
            ) : (
              <Field label="YAML 配置" hint="保存后会立即生效（当前配置会热重载内核）">
                <Textarea
                  value={editContent}
                  onChange={(event) => setEditContent(event.target.value)}
                  rows={16}
                  spellCheck={false}
                />
              </Field>
            )}
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="测速地址" hint="启用该订阅后，代理页测速默认使用此地址">
                <Input
                  value={editSettings.testUrl}
                  spellCheck={false}
                  onChange={(event) =>
                    setEditSettings((current) =>
                      current ? { ...current, testUrl: event.target.value } : current,
                    )
                  }
                />
              </Field>
              <Field label="测速超时（毫秒）">
                <Input
                  type="number"
                  min={1000}
                  value={editSettings.testTimeout}
                  onChange={(event) =>
                    setEditSettings((current) =>
                      current ? { ...current, testTimeout: Number(event.target.value) } : current,
                    )
                  }
                />
              </Field>
            </div>
            <Field
              label="扩展规则"
              hint="每行一条 mihomo 规则（类型,内容,策略），插入在订阅规则与全局扩展规则之前；不支持 MATCH，空行与 # 注释会被忽略。"
            >
              <Textarea
                value={editRules}
                onChange={(event) => setEditRules(event.target.value)}
                rows={8}
                spellCheck={false}
                placeholder={'DOMAIN-SUFFIX,example.com,DIRECT'}
              />
            </Field>
          </div>
        )}
      </Modal>

      <Modal
        open={Boolean(viewing)}
        onClose={() => setViewing(null)}
        title={`查看订阅配置${viewing ? ` · ${viewing.name}` : ''}`}
        description="订阅内容由链接提供，为只读；如需调整请使用「编辑」中的扩展规则"
        width="max-w-3xl"
        footer={<Button onClick={() => setViewing(null)}>关闭</Button>}
      >
        {viewLoading ? (
          <div className="flex items-center justify-center py-16 text-sm text-slate-500">
            <Spinner className="mr-2" />
            读取配置…
          </div>
        ) : (
          <Textarea
            value={viewContent}
            rows={22}
            spellCheck={false}
            readOnly
            className="cursor-default text-slate-400"
          />
        )}
      </Modal>

      <ConfirmDialog
        open={Boolean(deleting)}
        title="删除配置"
        description={
          <>
            确定删除「{deleting?.name}」吗？此操作不可撤销。
            {deleting?.id === activeId && ' 删除当前配置后会自动切换到其他配置。'}
          </>
        }
        confirmText="删除"
        danger
        busy={deleteBusy}
        onCancel={() => setDeleting(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  );
}
