import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Cpu,
  ExternalLink,
  Eye,
  EyeOff,
  HardDriveDownload,
  KeyRound,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Save,
  ShieldAlert,
  Square,
  Terminal,
  Trash2,
  Undo2,
} from 'lucide-react';
import { SystemProxyModal } from '../components/SystemProxyModal';
import {
  Badge,
  Button,
  ConfirmDialog,
  ConfirmPopover,
  CopyButton,
  Field,
  Input,
  ProgressBar,
  Section,
  Select,
  SwitchRow,
  Textarea,
  Toggle,
} from '../components/ui';
import { api } from '../lib/api';
import { cx, formatBytes, formatDateTime, formatDuration } from '../lib/format';
import { useKernel } from '../lib/kernel';
import { useGeoIPStatus } from '../lib/geoip';
import { useToast } from '../lib/toast';
import type {
  AppSettings,
  AuthStatus,
  GeoIPCheckResult,
  InboundUser,
  KernelUpdateCheck,
  SettingsResponse,
  SystemProxyActionResponse,
  SystemProxyStatus,
  SystemProxySyncResult,
} from '../types';

const STAGE_LABEL: Record<string, string> = {
  resolving: '解析版本…',
  downloading: '下载中…',
  extracting: '解压中…',
};

const MIRROR_PRESETS = ['', 'https://ghfast.top/', 'https://gh-proxy.com/', 'https://ghproxy.net/'];

const isCustomMirror = (mirror: string) => !MIRROR_PRESETS.includes(mirror);

function validateAuthUsers(users: InboundUser[]): string | null {
  const seen = new Set<string>();
  for (const user of users) {
    const username = user.username.trim();
    if (!username) return '端口认证用户名不能为空';
    if (username.includes(':')) {
      return `端口认证用户名「${username}」不能包含冒号（mihomo 认证使用“用户名:密码”格式）`;
    }
    if (!user.password) return `端口认证用户「${username}」的密码不能为空`;
    if (seen.has(username)) return `端口认证用户名「${username}」重复，请合并为一条`;
    seen.add(username);
  }
  return null;
}

export function Settings() {
  const toast = useToast();
  const { status, start, stop, restart, busy } = useKernel();
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [meta, setMeta] = useState<SettingsResponse['config'] | null>(null);
  const [form, setForm] = useState<AppSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [regenerating, setRegenerating] = useState(false);
  const [confirmRegenerate, setConfirmRegenerate] = useState(false);
  const [confirmKernelAction, setConfirmKernelAction] = useState<'stop' | 'restart' | null>(null);
  const [checkingKernel, setCheckingKernel] = useState(false);
  const [updateCheck, setUpdateCheck] = useState<KernelUpdateCheck | null>(null);
  const [customMirror, setCustomMirror] = useState(false);
  const [version, setVersion] = useState('');

  const [passwordForm, setPasswordForm] = useState({ current: '', next: '', confirm: '' });
  const [passwordBusy, setPasswordBusy] = useState(false);
  const [showSecret, setShowSecret] = useState(false);

  const [sysProxy, setSysProxy] = useState<SystemProxyStatus | null>(null);
  const [sysProxyBusy, setSysProxyBusy] = useState(false);
  const [sysProxyModal, setSysProxyModal] = useState<'enable' | 'inspect' | null>(null);
  const [confirmDisableSysProxy, setConfirmDisableSysProxy] = useState(false);

  const loadSystemProxy = useCallback(async () => {
    try {
      const result = await api<SystemProxyStatus>('/system-proxy/status');
      setSysProxy(result);
      return result;
    } catch {
      return null;
    }
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [result, proxyStatus] = await Promise.all([
        api<SettingsResponse>('/settings'),
        api<SystemProxyStatus>('/system-proxy/status').catch(() => null),
      ]);
      setSettings(result.settings);
      setForm(structuredClone(result.settings));
      setMeta(result.config);
      setCustomMirror(isCustomMirror(result.settings.kernel.mirror));
      if (proxyStatus) setSysProxy(proxyStatus);
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [toast]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    api<AuthStatus>('/auth/status')
      .then((result) => setVersion(result.version))
      .catch(() => undefined);
  }, []);

  const dirty = useMemo(
    () => Boolean(form && settings && JSON.stringify(form) !== JSON.stringify(settings)),
    [form, settings],
  );

  const update = <K extends keyof AppSettings>(key: K, patch: Partial<AppSettings[K]>) => {
    setForm((current) =>
      current ? { ...current, [key]: { ...current[key], ...patch } } : current,
    );
  };

  const setAuthUsers = (users: InboundUser[]) => {
    setForm((current) => (current ? { ...current, authentication: users } : current));
  };

  const save = async () => {
    if (!form) return;
    const invalid = validateAuthUsers(form.authentication);
    if (invalid) {
      toast.error(invalid);
      return;
    }
    setSaving(true);
    try {
      const body: Record<string, unknown> = { ...form };
      delete body.customRules;
      delete body.systemProxy;
      const result = await api<{
        result: string;
        settings: AppSettings;
        systemProxySync?: SystemProxySyncResult;
      }>('/settings', {
        method: 'PUT',
        body,
      });
      setSettings(structuredClone(result.settings));
      setForm(structuredClone(result.settings));
      setCustomMirror(isCustomMirror(result.settings.kernel.mirror));
      const baseMessage =
        result.result === 'reloaded'
          ? '设置已保存，内核已热重载'
          : result.result === 'restarted'
            ? '设置已保存，内核已重启'
            : '设置已保存（内核未运行）';
      if (result.systemProxySync?.error) {
        toast.error(`${baseMessage}，但同步系统代理失败：${result.systemProxySync.error}`);
      } else if (result.systemProxySync?.updated) {
        toast.success(`${baseMessage}，系统代理配置已自动更新`);
      } else {
        toast.success(baseMessage);
      }
      void loadSystemProxy();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const handleToggleSystemProxy = async (next: boolean) => {
    if (next) {
      setSysProxyBusy(true);
      try {
        const fresh = await api<SystemProxyStatus>('/system-proxy/status');
        setSysProxy(fresh);
        setSysProxyModal('enable');
      } catch (err) {
        toast.error((err as Error).message);
      } finally {
        setSysProxyBusy(false);
      }
      return;
    }
    setConfirmDisableSysProxy(true);
  };

  const confirmEnableSystemProxy = async () => {
    setSysProxyBusy(true);
    try {
      const result = await api<SystemProxyActionResponse>('/system-proxy/enable', {
        method: 'POST',
        body: { confirmed: true },
      });
      setSysProxy(result.status);
      setSettings((cur) =>
        cur ? { ...cur, systemProxy: result.settings.systemProxy } : result.settings,
      );
      setForm((cur) =>
        cur ? { ...cur, systemProxy: result.settings.systemProxy } : result.settings,
      );
      setSysProxyModal(null);
      toast.success(
        `系统代理已写入 ${result.status.preview.shell.configPath}，新开终端或执行 source 后生效`,
      );
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSysProxyBusy(false);
    }
  };

  const confirmDisableSystemProxyAction = async () => {
    setSysProxyBusy(true);
    try {
      const result = await api<SystemProxyActionResponse>('/system-proxy/disable', {
        method: 'POST',
      });
      setSysProxy(result.status);
      setSettings((cur) =>
        cur ? { ...cur, systemProxy: result.settings.systemProxy } : result.settings,
      );
      setForm((cur) =>
        cur ? { ...cur, systemProxy: result.settings.systemProxy } : result.settings,
      );
      setConfirmDisableSysProxy(false);
      toast.success('已关闭系统代理并移除 WebClash 托管配置');
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSysProxyBusy(false);
    }
  };

  const syncSystemProxyNow = async () => {
    setSysProxyBusy(true);
    try {
      const result = await api<SystemProxyActionResponse>('/system-proxy/sync', {
        method: 'POST',
      });
      setSysProxy(result.status);
      setSettings((cur) =>
        cur ? { ...cur, systemProxy: result.settings.systemProxy } : result.settings,
      );
      setForm((cur) =>
        cur ? { ...cur, systemProxy: result.settings.systemProxy } : result.settings,
      );
      toast.success('系统代理配置已同步');
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSysProxyBusy(false);
    }
  };

  const downloadKernel = async () => {
    if (!form) return;
    try {
      await api('/kernel/download', {
        method: 'POST',
        body: { version: form.kernel.version },
      });
      toast.info('已开始下载内核，进度可在内核管理面板查看');
    } catch (err) {
      toast.error((err as Error).message);
    }
  };

  const checkKernelUpdate = async () => {
    if (!form) return;
    if (!status?.binary.exists) {
      await downloadKernel();
      return;
    }
    setCheckingKernel(true);
    try {
      const result = await api<KernelUpdateCheck>('/kernel/check-update', {
        method: 'POST',
        body: { version: form.kernel.version },
      });
      if (!result.hasUpdate) {
        toast.success(`已是最新版本（${result.latest}）`);
        return;
      }
      setUpdateCheck(result);
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCheckingKernel(false);
    }
  };

  const regenerateSecret = async () => {
    setRegenerating(true);
    try {
      const result = await api<{ secret: string; settings: AppSettings }>(
        '/settings/controller-secret',
        { method: 'POST' },
      );
      setSettings(structuredClone(result.settings));
      setForm(structuredClone(result.settings));
      setConfirmRegenerate(false);
      toast.success('控制器密钥已重置，内核已重启');
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setRegenerating(false);
    }
  };

  const changePassword = async () => {
    if (passwordForm.next !== passwordForm.confirm) {
      toast.error('两次输入的新密码不一致');
      return;
    }
    if (passwordForm.next.length < 6) {
      toast.error('新密码长度至少 6 位');
      return;
    }
    setPasswordBusy(true);
    try {
      await api('/auth/password', {
        method: 'POST',
        body: { current: passwordForm.current, next: passwordForm.next },
      });
      setPasswordForm({ current: '', next: '', confirm: '' });
      toast.success('密码已更新');
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setPasswordBusy(false);
    }
  };

  const geoPoll = useGeoIPStatus();
  const geoStatus = geoPoll.data;
  const setGeoStatus = geoPoll.setData;
  const refreshGeoStatus = geoPoll.refresh;
  const [geoChecking, setGeoChecking] = useState(false);
  const [geoUpdating, setGeoUpdating] = useState(false);
  const geoCheckedRef = useRef(false);

  const checkGeoUpdate = useCallback(
    async (silent = false) => {
      setGeoChecking(true);
      try {
        const result = await api<GeoIPCheckResult>('/geoip/check', { method: 'POST' });
        setGeoStatus((current) =>
          current
            ? { ...current, updateAvailable: result.updateAvailable, lastCheckAt: result.lastCheckAt }
            : current,
        );
        if (!silent) {
          toast.success(result.updateAvailable ? '发现新版本，可点击「更新数据库」' : '已是最新版本');
        }
      } catch (err) {
        if (!silent) toast.error((err as Error).message);
      } finally {
        setGeoChecking(false);
      }
    },
    [toast, setGeoStatus],
  );

  const updateGeoDB = useCallback(async () => {
    setGeoUpdating(true);
    try {
      await api('/geoip/update', { method: 'POST' });
      toast.info('已开始更新 IP 归属数据库，进度见下方状态');
      void refreshGeoStatus();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setGeoUpdating(false);
    }
  }, [toast, refreshGeoStatus]);

  useEffect(() => {
    // 仅当本地库已就绪且不在下载时自动检测一次：首次开启时数据库还在下载，
    // 此刻检测会把“缺文件”误判为“有新版本”。就绪后再拿真实库比对。
    if (geoStatus?.enable && geoStatus.ready && !geoStatus.downloading) {
      if (!geoCheckedRef.current) {
        geoCheckedRef.current = true;
        void checkGeoUpdate(true);
      }
    } else if (!geoStatus?.enable) {
      geoCheckedRef.current = false;
    }
  }, [geoStatus?.enable, geoStatus?.ready, geoStatus?.downloading, checkGeoUpdate]);

  const runConfirmedKernelAction = async () => {
    if (!confirmKernelAction) return;
    if (confirmKernelAction === 'stop') {
      await stop();
    } else {
      await restart();
    }
    setConfirmKernelAction(null);
  };

  if (loading || !form || !settings) {
    return (
      <div className="flex items-center justify-center py-24 text-sm text-slate-500">
        <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
        加载设置…
      </div>
    );
  }

  const kernelStateLabel: Record<string, string> = {
    running: '运行中',
    starting: '启动中',
    stopping: '停止中',
    stopped: '已停止',
    error: '异常',
  };
  const download = status?.download;
  const geoEnabled = Boolean(geoStatus?.enable);
  const geoDownloading = Boolean(geoStatus?.downloading);
  const geoDirty =
    form.geoip.enable !== settings.geoip.enable || form.geoip.source !== settings.geoip.source;
  const sysProxyEnabled = Boolean(sysProxy?.enable ?? settings.systemProxy.enable);
  const proxyConfigDirty =
    JSON.stringify(form.ports) !== JSON.stringify(settings.ports) ||
    JSON.stringify(form.authentication) !== JSON.stringify(settings.authentication);

  return (
    <div className="max-w-5xl space-y-4 pb-20">
      <Section
        title="内核管理"
        description="mihomo 二进制、版本与自动启动策略"
        actions={
          <div className="relative">
            <div className="flex flex-wrap items-center gap-3">
              <span
                className="flex items-center gap-1.5 text-[11px] text-slate-400"
                title="WebClash 启动时自动拉起内核"
              >
                自动启动
                <Toggle
                  checked={form.kernel.autoStart}
                  onChange={(value) => update('kernel', { autoStart: value })}
                  label="随面板自动启动"
                />
              </span>
              <span
                className="flex items-center gap-1.5 text-[11px] text-slate-400"
                title="启动内核时若未找到二进制则自动下载"
              >
                自动下载
                <Toggle
                  checked={form.kernel.autoDownload}
                  onChange={(value) => update('kernel', { autoDownload: value })}
                  label="缺少内核时自动下载"
                />
              </span>
              <span className="h-5 w-px bg-line" />
              <div className="flex items-center gap-1.5">
                {status?.running ? (
                  <Button size="sm" loading={busy} onClick={() => setConfirmKernelAction('stop')}>
                    <Square className="h-3.5 w-3.5" />
                    停止
                  </Button>
                ) : (
                  <Button size="sm" variant="primary" loading={busy} onClick={() => void start()}>
                    <Play className="h-3.5 w-3.5" />
                    启动
                  </Button>
                )}
                <Button size="sm" loading={busy} onClick={() => setConfirmKernelAction('restart')}>
                  <RotateCw className="h-3.5 w-3.5" />
                  重启
                </Button>
                <span className="mx-0.5 h-5 w-px bg-line" />
                <Button
                  size="sm"
                  loading={checkingKernel}
                  disabled={Boolean(download?.active)}
                  onClick={() => void checkKernelUpdate()}
                  title="更新前先检测最新版本"
                >
                  <HardDriveDownload className="h-3.5 w-3.5" />
                  {status?.binary.exists ? '更新内核' : '下载内核'}
                </Button>
              </div>
            </div>
            <ConfirmPopover
              open={confirmKernelAction !== null}
              message={confirmKernelAction === 'restart' ? '重启内核？' : '停止内核？'}
              detail={
                confirmKernelAction === 'restart' ? '代理连接将短暂中断' : '代理连接将全部中断'
              }
              confirmText={confirmKernelAction === 'restart' ? '重启' : '停止'}
              danger={confirmKernelAction === 'stop'}
              busy={busy}
              onCancel={() => setConfirmKernelAction(null)}
              onConfirm={() => void runConfirmedKernelAction()}
            />
            <ConfirmPopover
              open={updateCheck !== null}
              message={updateCheck ? `更新内核到 ${updateCheck.latest}？` : ''}
              detail={
                updateCheck
                  ? `当前版本 ${updateCheck.current ?? '未知'}，更新后内核将重启，代理会短暂中断`
                  : undefined
              }
              confirmText="更新"
              onCancel={() => setUpdateCheck(null)}
              onConfirm={() => {
                setUpdateCheck(null);
                void downloadKernel();
              }}
            />
          </div>
        }
      >
        <div className="space-y-5">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <div className="rounded-lg border border-line bg-ink-950/50 p-3">
              <p className="text-[10px] text-slate-500">运行状态</p>
              <p className="mt-1 flex items-center gap-1.5 text-sm text-slate-200">
                <span
                  className={cx(
                    'h-1.5 w-1.5 rounded-full',
                    status?.running ? 'bg-emerald-400' : 'bg-slate-500',
                  )}
                />
                {kernelStateLabel[status?.state ?? 'stopped'] ?? '已停止'}
              </p>
            </div>
            <div className="rounded-lg border border-line bg-ink-950/50 p-3">
              <p className="text-[10px] text-slate-500">内核版本</p>
              <p className="mono mt-1 text-sm text-slate-200">{status?.binary.version ?? '未安装'}</p>
            </div>
            <div className="rounded-lg border border-line bg-ink-950/50 p-3">
              <p className="text-[10px] text-slate-500">运行时长</p>
              <p className="mt-1 text-sm text-slate-200">
                {status?.startedAt ? formatDuration(Date.now() - status.startedAt) : '—'}
              </p>
            </div>
            <div className="rounded-lg border border-line bg-ink-950/50 p-3">
              <p className="text-[10px] text-slate-500">二进制来源</p>
              <p className="mt-1 text-sm text-slate-200">
                {status?.binary.managed ? '内置下载' : status?.binary.exists ? '外部路径' : '未找到'}
              </p>
            </div>
          </div>

          {!status?.binary.exists && (
            <div className="flex items-start gap-2 rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2.5 text-xs text-amber-200">
              <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
              <p>
                尚未找到 mihomo 内核。点击右上方「下载内核」自动获取，或填写本机已有的二进制路径。
              </p>
            </div>
          )}

          <div className="space-y-4 rounded-lg border border-line bg-ink-950/40 p-3">
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_12rem]">
              <Field
                label="下载加速站"
                hint="加速 GitHub 内核下载与版本检测；默认不设置，直连失败后自动尝试公共加速站"
              >
                <Select
                  value={customMirror ? '__custom__' : form.kernel.mirror}
                  onChange={(event) => {
                    const value = event.target.value;
                    if (value === '__custom__') {
                      setCustomMirror(true);
                      return;
                    }
                    setCustomMirror(false);
                    update('kernel', { mirror: value });
                  }}
                >
                  <option value="">未设置</option>
                  <option value="https://ghfast.top/">https://ghfast.top/</option>
                  <option value="https://gh-proxy.com/">https://gh-proxy.com/</option>
                  <option value="https://ghproxy.net/">https://ghproxy.net/</option>
                  <option value="__custom__">自定义前缀…</option>
                </Select>
                {customMirror && (
                  <Input
                    className="mt-2"
                    value={form.kernel.mirror}
                    onChange={(event) => update('kernel', { mirror: event.target.value })}
                    placeholder="https://your-proxy.example/"
                  />
                )}
              </Field>
              <Field label="内核版本" hint="latest 或 v1.19.34">
                <Input
                  value={form.kernel.version}
                  onChange={(event) => update('kernel', { version: event.target.value })}
                  placeholder="latest"
                />
              </Field>
            </div>

            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_12rem]">
              <Field label="自定义内核路径" hint="留空则使用 data/bin/mihomo 或系统 PATH 中的 mihomo">
                <Input
                  value={form.kernel.binaryPath}
                  onChange={(event) => update('kernel', { binaryPath: event.target.value })}
                  placeholder="/usr/local/bin/mihomo"
                />
              </Field>
              <Field label="日志级别" hint="mihomo 运行日志的输出级别，保存后重新生成配置生效">
                <Select
                  value={form.general.logLevel}
                  onChange={(event) =>
                    update('general', {
                      logLevel: event.target.value as AppSettings['general']['logLevel'],
                    })
                  }
                >
                  <option value="debug">debug</option>
                  <option value="info">info</option>
                  <option value="warning">warning</option>
                  <option value="error">error</option>
                  <option value="silent">silent</option>
                </Select>
              </Field>
            </div>

            {download?.active && (
              <div className="space-y-1.5">
                <div className="flex items-center justify-between text-[11px] text-slate-400">
                  <span>{STAGE_LABEL[download.stage ?? ''] ?? '处理中…'}</span>
                  <span className="mono">
                    {download.total > 0
                      ? `${formatBytes(download.received)} / ${formatBytes(download.total)}`
                      : formatBytes(download.received)}
                  </span>
                </div>
                <ProgressBar value={download.progress} />
                <div className="flex justify-end">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void api('/kernel/cancel-download', { method: 'POST' })}
                  >
                    取消下载
                  </Button>
                </div>
              </div>
            )}

            {download?.error && (
              <p className="text-[11px] text-rose-400">下载失败：{download.error}</p>
            )}
          </div>

          <div className="space-y-1.5 rounded-lg border border-line bg-ink-950/50 p-3 text-[11px]">
            <div className="flex items-center justify-between gap-3">
              <span className="shrink-0 text-slate-500">配置文件</span>
              <span className="mono truncate text-slate-400" title={meta?.path}>
                {meta?.path}
              </span>
              {meta?.path && <CopyButton value={meta.path} />}
            </div>
            <div className="flex items-center justify-between gap-3">
              <span className="shrink-0 text-slate-500">数据目录</span>
              <span className="mono truncate text-slate-400" title={meta?.dataDir}>
                {meta?.dataDir}
              </span>
              {meta?.dataDir && <CopyButton value={meta.dataDir} />}
            </div>
            {meta?.lastConfigAt && (
              <div className="flex justify-between gap-3">
                <span className="text-slate-500">最后生成</span>
                <span className="text-slate-400">{formatDateTime(meta.lastConfigAt)}</span>
              </div>
            )}
            {meta?.lastConfigError && (
              <p className="pt-1 text-rose-400">配置错误：{meta.lastConfigError}</p>
            )}
          </div>
        </div>
      </Section>

      <Section
        title="网络与端口"
        description="代理监听端口、局域网访问与运行模式"
        actions={
          <div className="flex flex-wrap items-center gap-3">
            <span
              className="flex items-center gap-1.5 text-[11px] text-slate-400"
              title="根据系统 Shell 类型自动在配置文件中写入或移除代理环境变量"
            >
              设置系统代理
              <Toggle
                checked={sysProxyEnabled}
                disabled={sysProxyBusy}
                onChange={(value) => void handleToggleSystemProxy(value)}
                label="设置系统代理"
              />
            </span>
            <span className="h-5 w-px bg-line" />
            <span
              className="flex items-center gap-1.5 text-[11px] text-slate-400"
              title="同一网络内的其他设备可通过本机代理端口访问"
            >
              允许局域网连接
              <Toggle
                checked={form.general.allowLan}
                onChange={(value) => update('general', { allowLan: value })}
                label="允许局域网连接"
              />
            </span>
          </div>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Field label="运行模式">
            <Select
              value={form.general.mode}
              onChange={(event) =>
                update('general', { mode: event.target.value as AppSettings['general']['mode'] })
              }
            >
              <option value="rule">规则（Rule）</option>
              <option value="global">全局（Global）</option>
              <option value="direct">直连（Direct）</option>
            </Select>
          </Field>
          <Field label="混合端口" hint="HTTP/SOCKS 混合代理端口">
            <Input
              type="number"
              value={form.ports.mixed}
              onChange={(event) => update('ports', { mixed: Number(event.target.value) })}
            />
          </Field>
          <Field label="HTTP 端口" hint="0 表示不单独监听">
            <Input
              type="number"
              value={form.ports.http}
              onChange={(event) => update('ports', { http: Number(event.target.value) })}
            />
          </Field>
          <Field label="SOCKS 端口" hint="0 表示不单独监听">
            <Input
              type="number"
              value={form.ports.socks}
              onChange={(event) => update('ports', { socks: Number(event.target.value) })}
            />
          </Field>
          <Field label="IPv6" hint="是否启用 IPv6 支持">
            <Select
              value={form.general.ipv6 ? 'on' : 'off'}
              onChange={(event) => update('general', { ipv6: event.target.value === 'on' })}
            >
              <option value="off">关闭</option>
              <option value="on">开启</option>
            </Select>
          </Field>
          {form.general.allowLan && (
            <Field label="绑定地址" hint="* 表示所有网卡">
              <Input
                value={form.general.bindAddress}
                onChange={(event) => update('general', { bindAddress: event.target.value })}
              />
            </Field>
          )}
        </div>

        {sysProxyEnabled && sysProxy && (
          <div className="mt-4 space-y-2 rounded-lg border border-line bg-ink-950/50 p-3 text-[11px]">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="font-medium text-slate-300">系统代理</span>
                <Badge tone={sysProxy.synced ? 'emerald' : 'amber'}>
                  {sysProxy.synced ? '已同步' : '待同步'}
                </Badge>
                <Badge tone="sky">
                  <Terminal className="h-3 w-3" />
                  {sysProxy.preview.shell.shell}
                </Badge>
                <Badge tone={sysProxy.preview.hasAuth ? 'emerald' : 'slate'}>
                  {sysProxy.preview.hasAuth
                    ? `含认证：${sysProxy.preview.authUsername}`
                    : '免认证'}
                </Badge>
              </div>
              <div className="flex items-center gap-1.5">
                {!sysProxy.synced && (
                  <Button
                    size="sm"
                    variant="primary"
                    loading={sysProxyBusy}
                    onClick={() => void syncSystemProxyNow()}
                  >
                    <RefreshCw className="h-3 w-3" />
                    立即同步
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={async () => {
                    const fresh = await loadSystemProxy();
                    if (fresh) setSysProxyModal('inspect');
                  }}
                >
                  查看配置
                </Button>
              </div>
            </div>
            <div className="flex items-center justify-between gap-2 border-t border-line/60 pt-2">
              <span className="shrink-0 text-slate-500">Shell 配置</span>
              <span
                className="mono truncate text-slate-300"
                title={sysProxy.preview.shell.configPath}
              >
                {sysProxy.preview.shell.configPath}
              </span>
              <CopyButton value={sysProxy.preview.shell.configPath} />
            </div>
            <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line/60 pt-2">
              <span className="text-slate-500">
                {sysProxy.preview.shell.loginShellOnly ? (
                  <>
                    登录 Shell（<code className="mono text-slate-300">sh -l</code>）生效，或执行{' '}
                    <code className="mono text-slate-300">{sysProxy.preview.sourceCommand}</code>
                  </>
                ) : (
                  <>
                    新开终端生效，或执行{' '}
                    <code className="mono text-slate-300">{sysProxy.preview.sourceCommand}</code>
                  </>
                )}
              </span>
              <CopyButton value={sysProxy.preview.sourceCommand} label="复制命令" />
            </div>
            {sysProxy.preview.shell.inContainer && (
              <p className="text-sky-400">
                当前运行在容器环境中，配置仅作用于容器内部，不影响宿主机。
              </p>
            )}
            {sysProxy.error && <p className="text-rose-400">读取异常：{sysProxy.error}</p>}
            {proxyConfigDirty && (
              <p className="text-amber-300">
                检测到端口或认证已修改，点击底部「保存并应用」后系统代理配置将自动同步更新。
              </p>
            )}
          </div>
        )}

        <div className="mt-5 border-t border-line pt-5">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <p className="flex flex-wrap items-center gap-2 text-sm font-medium text-slate-300">
                端口认证
                <Badge tone={form.authentication.length > 0 ? 'emerald' : 'slate'}>
                  {form.authentication.length > 0
                    ? `${form.authentication.length} 个用户`
                    : '未启用'}
                </Badge>
              </p>
              <p className="mt-0.5 text-xs leading-relaxed text-slate-500">
                为混合端口与 HTTP / SOCKS 端口统一设置认证；留空表示所有端口免认证，客户端连接时使用「用户名:密码」。
              </p>
            </div>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                setAuthUsers([...form.authentication, { username: '', password: '' }])
              }
            >
              <Plus className="h-3.5 w-3.5" />
              添加用户
            </Button>
          </div>
          {form.authentication.length === 0 ? (
            <p className="mt-3 rounded-lg border border-line bg-ink-950/50 px-3 py-2 text-[11px] leading-relaxed text-slate-500">
              当前无认证用户，任何能访问混合端口或已开启的 HTTP / SOCKS 端口的人都可以使用代理。
              {form.general.allowLan && ' 已开启「允许局域网连接」，建议至少添加一个用户。'}
            </p>
          ) : (
            <div className="mt-3 space-y-2">
              {form.authentication.map((user, index) => (
                <div key={index} className="flex items-start gap-2">
                  <Input
                    value={user.username}
                    onChange={(event) =>
                      setAuthUsers(
                        form.authentication.map((item, i) =>
                          i === index ? { ...item, username: event.target.value } : item,
                        ),
                      )
                    }
                    placeholder="用户名"
                    className="min-w-0 flex-1"
                  />
                  <Input
                    type="password"
                    value={user.password}
                    onChange={(event) =>
                      setAuthUsers(
                        form.authentication.map((item, i) =>
                          i === index ? { ...item, password: event.target.value } : item,
                        ),
                      )
                    }
                    placeholder="密码"
                    autoComplete="new-password"
                    className="min-w-0 flex-1"
                  />
                  <Button
                    size="sm"
                    variant="ghost"
                    className="shrink-0 px-2"
                    title="删除用户"
                    onClick={() =>
                      setAuthUsers(form.authentication.filter((_, i) => i !== index))
                    }
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="mt-5 grid gap-4 border-t border-line pt-5 sm:grid-cols-2">
          <Field label="WebUI 端口" hint="修改后需重启 WebClash 进程才会生效">
            <Input
              type="number"
              value={form.server.port}
              onChange={(event) => update('server', { port: Number(event.target.value) })}
            />
          </Field>
          <Field label="WebUI 监听地址" hint="0.0.0.0 表示监听所有网卡">
            <Input
              value={form.server.host}
              onChange={(event) => update('server', { host: event.target.value })}
            />
          </Field>
        </div>
      </Section>

      <Section title="控制器" description="mihomo external-controller，仅监听 127.0.0.1，由面板转发">
        <div className="grid gap-4 sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)]">
          <Field label="控制器端口">
            <Input
              type="number"
              value={form.controller.port}
              onChange={(event) => update('controller', { port: Number(event.target.value) })}
            />
          </Field>
          <Field label="控制器密钥" hint="用于面板与内核通信，修改会导致内核重启">
            <div className="flex items-center gap-2">
              <div className="relative min-w-0 flex-1">
                <Input
                  type={showSecret ? 'text' : 'password'}
                  value={form.controller.secret}
                  onChange={(event) => update('controller', { secret: event.target.value })}
                  className="mono pr-9 text-xs"
                  autoComplete="off"
                  spellCheck={false}
                />
                <button
                  type="button"
                  onClick={() => setShowSecret((value) => !value)}
                  className="absolute top-1/2 right-1.5 -translate-y-1/2 rounded-md p-1 text-slate-500 transition hover:bg-white/5 hover:text-slate-300"
                  title={showSecret ? '隐藏密钥' : '显示密钥'}
                  aria-label={showSecret ? '隐藏密钥' : '显示密钥'}
                >
                  {showSecret ? (
                    <EyeOff className="h-3.5 w-3.5" />
                  ) : (
                    <Eye className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
              <CopyButton value={form.controller.secret} className="shrink-0 whitespace-nowrap" />
              <Button
                size="sm"
                variant="outline"
                className="shrink-0"
                onClick={() => setConfirmRegenerate(true)}
                title="重新生成密钥"
              >
                <RefreshCw className="h-3.5 w-3.5" />
              </Button>
            </div>
          </Field>
        </div>
      </Section>

      <Section title="连接历史" description="面板内存中保留的已关闭连接（用于在线排查，重启面板后清空）">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="保留条数" hint="100–20000，默认 3000；超出后淘汰最旧的记录">
            <Input
              type="number"
              value={form.connHistory.limit}
              onChange={(event) => update('connHistory', { limit: Number(event.target.value) })}
            />
          </Field>
        </div>
      </Section>

      <Section
        title="TUN 模式"
        description="透明代理需要管理员 / root 权限或相应的系统能力"
        actions={
          <span
            className="flex items-center gap-1.5 text-[11px] text-slate-400"
            title="通过虚拟网卡接管全部流量（需 root / NET_ADMIN）"
          >
            启用 TUN
            <Toggle
              checked={form.tun.enable}
              onChange={(value) => update('tun', { enable: value })}
              label="启用 TUN"
            />
          </span>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <SwitchRow
            title="自动路由"
            description="自动配置系统路由表指向 TUN 设备"
            checked={form.tun.autoRoute}
            onChange={(value) => update('tun', { autoRoute: value })}
          />
          <Field label="协议栈">
            <Select
              value={form.tun.stack}
              onChange={(event) =>
                update('tun', { stack: event.target.value as AppSettings['tun']['stack'] })
              }
            >
              <option value="mixed">mixed（推荐）</option>
              <option value="gvisor">gvisor</option>
              <option value="system">system</option>
            </Select>
          </Field>
          <SwitchRow
            title="自动选择网卡"
            description="自动探测默认出口网卡，避免回环"
            checked={form.tun.autoDetectInterface}
            onChange={(value) => update('tun', { autoDetectInterface: value })}
          />
          <Field label="DNS 劫持" hint="逗号分隔，如 any:53, 198.18.0.2:53">
            <Input
              value={form.tun.dnsHijack.join(', ')}
              onChange={(event) =>
                update('tun', {
                  dnsHijack: event.target.value
                    .split(',')
                    .map((item) => item.trim())
                    .filter(Boolean),
                })
              }
            />
          </Field>
          {form.tun.enable && (
            <p className="rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-[11px] leading-relaxed text-amber-200 sm:col-span-2">
              提示：开启 TUN 后需要以 root 运行内核；Docker 部署需添加
              <code className="mx-1">NET_ADMIN</code>能力并使用 host 网络。
            </p>
          )}
        </div>
      </Section>

      <Section
        title="DNS"
        description="面板会覆盖订阅中的 DNS 配置"
        actions={
          <span
            className="flex items-center gap-1.5 text-[11px] text-slate-400"
            title="通过内核内置 DNS 解析域名"
          >
            启用 DNS
            <Toggle
              checked={form.dns.enable}
              onChange={(value) => update('dns', { enable: value })}
              label="启用 DNS"
            />
          </span>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Field label="监听地址">
            <Input
              value={form.dns.listen}
              onChange={(event) => update('dns', { listen: event.target.value })}
            />
          </Field>
          <Field label="增强模式">
            <Select
              value={form.dns.enhancedMode}
              onChange={(event) =>
                update('dns', {
                  enhancedMode: event.target.value as AppSettings['dns']['enhancedMode'],
                })
              }
            >
              <option value="fake-ip">fake-ip</option>
              <option value="redir-host">redir-host</option>
            </Select>
          </Field>
          <Field label="fake-ip 网段" className="sm:col-span-2 lg:col-span-1">
            <Input
              value={form.dns.fakeIpRange}
              onChange={(event) => update('dns', { fakeIpRange: event.target.value })}
            />
          </Field>
        </div>

        <div className="mt-4 grid gap-4 lg:grid-cols-3">
          <Field label="主 DNS（nameserver）" hint="每行一个，支持 DoH / DoT">
            <Textarea
              rows={5}
              value={form.dns.nameservers.join('\n')}
              onChange={(event) =>
                update('dns', {
                  nameservers: event.target.value
                    .split('\n')
                    .map((item) => item.trim())
                    .filter(Boolean),
                })
              }
            />
          </Field>
          <Field label="回退 DNS（fallback）" hint="主 DNS 结果不理想时使用">
            <Textarea
              rows={5}
              value={form.dns.fallback.join('\n')}
              onChange={(event) =>
                update('dns', {
                  fallback: event.target.value
                    .split('\n')
                    .map((item) => item.trim())
                    .filter(Boolean),
                })
              }
            />
          </Field>
          <Field label="默认 DNS（default-nameserver）" hint="用于解析 DoH/DoT 域名，需为 IP">
            <Textarea
              rows={5}
              value={form.dns.defaultNameservers.join('\n')}
              onChange={(event) =>
                update('dns', {
                  defaultNameservers: event.target.value
                    .split('\n')
                    .map((item) => item.trim())
                    .filter(Boolean),
                })
              }
            />
          </Field>
        </div>
      </Section>

      <Section
        title="IP 归属"
        description="为连接页显示目标 IP 的国家/省市/运营商，离线查询、IP 不出本机"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <span
              className="flex items-center gap-1.5 text-[11px] text-slate-400"
              title="开启后下载离线数据库，连接页展示目标 IP 归属"
            >
              启用 IP 归属
              <Toggle
                checked={form.geoip.enable}
                onChange={(value) => update('geoip', { enable: value })}
                label="启用 IP 归属"
              />
            </span>
            <span className="h-5 w-px bg-line" />
            {geoStatus?.updateAvailable && <Badge tone="sky">有新版本</Badge>}
            <Button
              size="sm"
              loading={geoChecking}
              disabled={!geoEnabled || geoDownloading}
              onClick={() => void checkGeoUpdate()}
            >
              <RefreshCw className="h-3.5 w-3.5" />
              检查新版本
            </Button>
            <Button
              size="sm"
              variant="primary"
              loading={geoUpdating}
              disabled={!geoEnabled || geoDownloading}
              onClick={() => void updateGeoDB()}
            >
              <HardDriveDownload className="h-3.5 w-3.5" />
              更新数据库
            </Button>
          </div>
        }
      >
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="数据源" hint="ip2region 含省市/运营商（v4+v6）；mmdb 仅国家/地区">
              <Select
                value={form.geoip.source}
                onChange={(event) =>
                  update('geoip', { source: event.target.value as AppSettings['geoip']['source'] })
                }
              >
                <option value="ip2region">ip2region（国家·省·市·ISP）</option>
                <option value="mmdb">mmdb（国家/地区）</option>
              </Select>
            </Field>
          </div>

          {geoDirty && (
            <p className="rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-[11px] text-amber-200">
              开关或数据源已修改，需点击下方「保存并应用」后才会生效。
            </p>
          )}

          <div className="space-y-2 rounded-lg border border-line bg-ink-950/50 p-3 text-[11px]">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-slate-500">状态</span>
              {!geoEnabled ? (
                <Badge tone="slate">未启用</Badge>
              ) : geoDownloading ? (
                <Badge tone="amber">下载中…</Badge>
              ) : geoStatus?.ready ? (
                <Badge tone="emerald">已就绪</Badge>
              ) : (
                <Badge tone="slate">未就绪</Badge>
              )}
              {geoStatus?.updateAvailable && <Badge tone="sky">有新版本</Badge>}
              {geoStatus?.lastCheckAt && (
                <span className="ml-auto text-slate-600">
                  最近检测 {formatDateTime(geoStatus.lastCheckAt)}
                </span>
              )}
            </div>

            {geoStatus?.error && <p className="text-rose-400">错误：{geoStatus.error}</p>}

            {geoEnabled && geoStatus && geoStatus.files.length > 0 && (
              <div className="space-y-1 pt-1">
                {geoStatus.files.map((file) => (
                  <div
                    key={file.name}
                    className="flex flex-wrap items-center justify-between gap-2 border-t border-line/60 pt-1"
                  >
                    <span className="mono text-slate-400">{file.name}</span>
                    <span className="text-slate-500">
                      {formatBytes(file.size)}
                      {file.version ? ` · ${file.version}` : ''}
                      {file.updatedAt ? ` · ${formatDateTime(file.updatedAt)}` : ''}
                    </span>
                  </div>
                ))}
              </div>
            )}

            {geoEnabled && !geoStatus?.ready && !geoDownloading && (
              <p className="text-slate-500">
                数据库尚未就绪，点击「更新数据库」下载；缺失时连接页归属显示为「未知」，不影响其他信息。
              </p>
            )}
          </div>

          <p className="text-[11px] leading-relaxed text-slate-500">
            数据库离线保存在本机 data/geoip/，查询不联网、IP 不外发；下载复用「内核管理」中的下载加速站。ip2region
            会同步下载 IPv4 与 IPv6 两个库。
          </p>
        </div>
      </Section>

      <Section title="安全" description="WebUI 管理密码">
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label="当前密码">
            <Input
              type="password"
              value={passwordForm.current}
              autoComplete="current-password"
              onChange={(event) =>
                setPasswordForm((current) => ({ ...current, current: event.target.value }))
              }
            />
          </Field>
          <Field label="新密码">
            <Input
              type="password"
              value={passwordForm.next}
              autoComplete="new-password"
              onChange={(event) =>
                setPasswordForm((current) => ({ ...current, next: event.target.value }))
              }
            />
          </Field>
          <Field label="确认新密码">
            <Input
              type="password"
              value={passwordForm.confirm}
              autoComplete="new-password"
              onChange={(event) =>
                setPasswordForm((current) => ({ ...current, confirm: event.target.value }))
              }
            />
          </Field>
        </div>
        <div className="mt-4">
          <Button
            loading={passwordBusy}
            disabled={!passwordForm.next || !passwordForm.confirm}
            onClick={() => void changePassword()}
          >
            <KeyRound className="h-3.5 w-3.5" />
            更新密码
          </Button>
        </div>
      </Section>

      <Section title="关于" description="项目信息与外部链接">
        <div className="grid gap-3 text-xs sm:grid-cols-2">
          <div className="flex justify-between rounded-lg border border-line bg-ink-950/40 px-3 py-2">
            <span className="text-slate-500">WebClash 版本</span>
            <span className="mono text-slate-300">{version || '—'}</span>
          </div>
          <div className="flex justify-between rounded-lg border border-line bg-ink-950/40 px-3 py-2">
            <span className="text-slate-500">内核</span>
            <span className="mono text-slate-300">mihomo {status?.binary.version ?? ''}</span>
          </div>
          <a
            href="https://github.com/MetaCubeX/mihomo"
            target="_blank"
            rel="noreferrer"
            className="flex items-center justify-between rounded-lg border border-line bg-ink-950/40 px-3 py-2 text-slate-400 transition hover:border-line-strong hover:text-slate-200"
          >
            <span>mihomo 项目主页</span>
            <ExternalLink className="h-3.5 w-3.5" />
          </a>
          <div className="flex items-center gap-2 rounded-lg border border-line bg-ink-950/40 px-3 py-2 text-slate-500">
            <Cpu className="h-3.5 w-3.5" />
            基于 mihomo external-controller RESTful API
          </div>
        </div>
      </Section>

      {dirty && (
        <div className="sticky bottom-4 z-20 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-emerald-500/30 bg-ink-850/95 px-4 py-3 shadow-2xl backdrop-blur">
          <div className="flex items-center gap-2 text-xs text-slate-400">
            <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
            有未保存的更改
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              onClick={() => {
                setForm(structuredClone(settings));
                setCustomMirror(isCustomMirror(settings.kernel.mirror));
              }}
              disabled={saving}
            >
              <Undo2 className="h-3.5 w-3.5" />
              重置
            </Button>
            <Button variant="primary" loading={saving} onClick={() => void save()}>
              <Save className="h-3.5 w-3.5" />
              保存并应用
            </Button>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={confirmRegenerate}
        title="重置控制器密钥"
        description="重置后所有与内核控制器相关的连接都会失效，内核将自动重启以加载新密钥。"
        confirmText="重置并重启"
        busy={regenerating}
        onCancel={() => setConfirmRegenerate(false)}
        onConfirm={() => void regenerateSecret()}
      />

      <SystemProxyModal
        open={sysProxyModal !== null}
        mode={sysProxyModal ?? 'enable'}
        status={sysProxy}
        hasUnsavedProxyChanges={proxyConfigDirty}
        busy={sysProxyBusy}
        onClose={() => setSysProxyModal(null)}
        onConfirmEnable={() => void confirmEnableSystemProxy()}
        onSync={() => void syncSystemProxyNow()}
      />

      <ConfirmDialog
        open={confirmDisableSysProxy}
        title="关闭系统代理"
        description={`将从 ${sysProxy?.preview.shell.configPath ?? settings.systemProxy.configPath ?? 'Shell 配置文件'} 中移除 WebClash 托管的代理配置块（不会影响文件中的其他配置）。已打开的终端需新开终端或手动取消环境变量后生效。`}
        confirmText="确认关闭并移除"
        busy={sysProxyBusy}
        onCancel={() => setConfirmDisableSysProxy(false)}
        onConfirm={() => void confirmDisableSystemProxyAction()}
      />
    </div>
  );
}
