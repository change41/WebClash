import { useEffect, useState } from 'react';
import {
  AlertTriangle,
  CheckCircle2,
  Eye,
  EyeOff,
  RefreshCw,
  ShieldAlert,
  Terminal,
} from 'lucide-react';
import { Badge, Button, CopyButton, Modal } from './ui';
import type { SystemProxyStatus } from '../types';

interface SystemProxyModalProps {
  open: boolean;
  mode: 'enable' | 'inspect';
  status: SystemProxyStatus | null;
  hasUnsavedProxyChanges?: boolean;
  busy?: boolean;
  onClose: () => void;
  onConfirmEnable: () => void;
  onSync?: () => void;
}

export function SystemProxyModal({
  open,
  mode,
  status,
  hasUnsavedProxyChanges = false,
  busy = false,
  onClose,
  onConfirmEnable,
  onSync,
}: SystemProxyModalProps) {
  const [showPlaintext, setShowPlaintext] = useState(false);

  useEffect(() => {
    if (open) {
      setShowPlaintext(false);
    }
  }, [open, mode]);

  if (!status) return null;

  const { preview } = status;
  const envProxies = preview.envProxies ?? [];
  const hasAnyDetectedProxy = preview.existing.length > 0 || envProxies.length > 0;
  const isEnable = mode === 'enable';
  const renderedBlock = showPlaintext ? preview.block : preview.maskedBlock;

  const title = isEnable ? '确认开启系统代理' : '系统代理配置详情';

  const description = isEnable
    ? '请核对检测到的 Shell 类型、已有代理配置与即将写入的环境变量，确认后将写入目标配置文件'
    : `当前托管在 ${preview.shell.configPath} 的代理环境变量`;

  const footer = isEnable ? (
    <>
      <Button variant="ghost" onClick={onClose} disabled={busy}>
        取消
      </Button>
      <Button
        variant="primary"
        loading={busy}
        disabled={Boolean(status.error)}
        onClick={onConfirmEnable}
      >
        确认
      </Button>
    </>
  ) : (
    <>
      {status.enable && !status.synced && onSync && (
        <Button variant="primary" loading={busy} onClick={onSync}>
          <RefreshCw className="h-3.5 w-3.5" />
          立即同步
        </Button>
      )}
      <Button variant="outline" onClick={onClose}>
        关闭
      </Button>
    </>
  );

  return (
    <Modal
      open={open}
      onClose={busy ? () => undefined : onClose}
      title={title}
      description={description}
      width="max-w-2xl"
      footer={footer}
    >
      <div className="space-y-4">
        {status.error && (
          <div className="flex items-start gap-2 rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-2.5 text-xs text-rose-300">
            <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
            <p>{status.error}</p>
          </div>
        )}

        {preview.shell.inContainer && (
          <div className="flex items-start gap-2 rounded-lg border border-sky-500/30 bg-sky-500/10 px-3 py-2.5 text-xs leading-relaxed text-sky-300">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <p>
              当前检测到 WebClash 运行在容器环境中，此开关仅修改容器内的 Shell 配置文件（
              <code className="mono">{preview.shell.configPath}</code>
              ），不会修改宿主机配置。
            </p>
          </div>
        )}

        {preview.shell.loginShellOnly && (
          <div className="flex items-start gap-2 rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2.5 text-xs leading-relaxed text-amber-200">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <p>
              当前检测为 <code className="mono">sh</code>（写入{' '}
              <code className="mono">{preview.shell.configPath}</code>），该文件仅在登录 Shell（如{' '}
              <code className="mono">sh -l</code> 或 SSH 登录）中自动加载；非登录交互式{' '}
              <code className="mono">sh</code> 需手动执行{' '}
              <code className="mono">{preview.sourceCommand}</code> 生效。
            </p>
          </div>
        )}

        {hasUnsavedProxyChanges && (
          <div className="flex items-start gap-2 rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2.5 text-xs leading-relaxed text-amber-200">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <p>
              检测到当前页面有未保存的代理端口或端口认证修改，本次将按已保存配置写入；保存设置后系统代理会自动同步更新。
            </p>
          </div>
        )}

        <div className="space-y-2 rounded-lg border border-line bg-ink-950/50 p-3 text-xs">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-slate-500">检测到的 Shell</span>
            <div className="flex flex-wrap items-center gap-1.5">
              <Badge tone="sky">
                <Terminal className="h-3 w-3" />
                {preview.shell.shell}
              </Badge>
              {preview.shell.shellPath && (
                <span className="mono text-[11px] text-slate-400">
                  {preview.shell.shellPath}
                </span>
              )}
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line/60 pt-2">
            <span className="shrink-0 text-slate-500">目标配置文件</span>
            <div className="flex min-w-0 items-center gap-2">
              <span
                className="mono truncate text-[11px] text-slate-200"
                title={preview.shell.configPath}
              >
                {preview.shell.configPath}
              </span>
              <Badge tone={preview.shell.fileExists ? 'slate' : 'amber'}>
                {preview.shell.fileExists ? '已存在' : '将新建'}
              </Badge>
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line/60 pt-2">
            <span className="text-slate-500">HTTP / HTTPS 代理</span>
            <span className="mono text-[11px] text-slate-300">{preview.httpProxy}</span>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line/60 pt-2">
            <span className="text-slate-500">ALL / SOCKS 代理</span>
            <span className="mono text-[11px] text-slate-300">{preview.socksProxy}</span>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line/60 pt-2">
            <span className="text-slate-500">端口认证</span>
            {preview.hasAuth ? (
              <Badge tone="emerald">已附加认证（用户：{preview.authUsername}）</Badge>
            ) : (
              <span className="text-[11px] text-slate-400">
                未配置（后续添加并保存后将自动更新）
              </span>
            )}
          </div>
        </div>

        {hasAnyDetectedProxy ? (
          <div className="space-y-2.5 rounded-lg border-2 border-amber-500/45 bg-amber-500/15 p-3.5">
            <div className="flex items-start gap-2 text-xs text-amber-200">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-400" />
              <div className="space-y-1">
                <div className="flex flex-wrap items-center gap-1.5 font-semibold">
                  <span>检查结果：检测到已有代理配置</span>
                  {preview.existing.length > 0 && (
                    <Badge tone="amber">配置文件 {preview.existing.length} 条</Badge>
                  )}
                  {envProxies.length > 0 && (
                    <Badge tone="amber">环境变量 {envProxies.length} 项</Badge>
                  )}
                </div>
                <p className="text-[11px] leading-relaxed text-amber-200/85">
                  {preview.hasExternalProxy || envProxies.length > 0
                    ? `本次仅会修改目标文件 ${preview.shell.configPath}（在末尾写入或更新独立托管块），不会删除原有配置。请核对以下已检测到的代理设置：`
                    : `目标文件 ${preview.shell.configPath} 中已存在 WebClash 托管配置块，本次确认将原地更新：`}
                </p>
              </div>
            </div>

            {preview.existing.length > 0 && (
              <div className="max-h-44 overflow-auto rounded-lg border border-amber-500/25 bg-ink-950/85 p-2.5">
                <div className="space-y-1.5">
                  {preview.existing.map((entry, idx) => (
                    <div
                      key={`${entry.file}-${entry.line}-${idx}`}
                      className="flex flex-wrap items-start gap-2 text-[11px] leading-relaxed"
                    >
                      <Badge
                        tone={entry.managed ? 'emerald' : 'amber'}
                        className="shrink-0 py-0 text-[10px]"
                      >
                        {entry.managed ? '托管' : '配置'}
                      </Badge>
                      <span
                        className="mono shrink-0 text-slate-400"
                        title={`${entry.file}:${entry.line}`}
                      >
                        {entry.file === preview.shell.configPath
                          ? `L${entry.line}`
                          : `${entry.file}:${entry.line}`}
                      </span>
                      <code className="mono min-w-0 flex-1 break-all text-slate-200">
                        {entry.content}
                      </code>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {envProxies.length > 0 && (
              <div className="max-h-36 overflow-auto rounded-lg border border-amber-500/25 bg-ink-950/85 p-2.5">
                <div className="space-y-1">
                  {envProxies.map((item) => (
                    <div
                      key={item.name}
                      className="flex items-start gap-2 text-[11px] leading-relaxed"
                    >
                      <Badge tone="sky" className="shrink-0 py-0 text-[10px]">
                        环境变量
                      </Badge>
                      <code className="mono break-all text-slate-200">
                        {item.name}={item.value}
                      </code>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        ) : (
          <div className="flex items-start gap-2.5 rounded-lg border-2 border-emerald-500/40 bg-emerald-500/15 px-3.5 py-3 text-xs text-emerald-200">
            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-400" />
            <div className="space-y-0.5">
              <p className="font-semibold">检查结果：未检测到已有代理配置</p>
              <p className="text-[11px] leading-relaxed text-emerald-200/80">
                Shell 配置文件与当前环境变量中均未发现已有代理设置，可直接写入目标文件。
              </p>
            </div>
          </div>
        )}

        <div className="space-y-2">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-medium text-slate-300">
              {isEnable ? '即将写入的代理配置' : '当前托管的代理配置'}
            </span>
            <div className="flex items-center gap-1.5">
              {preview.hasAuth && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setShowPlaintext((value) => !value)}
                >
                  {showPlaintext ? (
                    <>
                      <EyeOff className="h-3.5 w-3.5" />
                      隐藏密码
                    </>
                  ) : (
                    <>
                      <Eye className="h-3.5 w-3.5" />
                      显示明文
                    </>
                  )}
                </Button>
              )}
              <CopyButton value={renderedBlock} label="复制配置" />
            </div>
          </div>
          <pre className="mono max-h-52 overflow-auto rounded-lg border border-line bg-ink-950/80 p-3 text-[11px] leading-relaxed text-slate-300">
            {renderedBlock}
          </pre>
        </div>

        {isEnable ? (
          <ul className="list-disc space-y-1 pl-5 text-[11px] leading-relaxed text-slate-500">
            <li>
              {preview.shell.loginShellOnly ? (
                <>
                  写入后需启动登录 Shell（如 <code className="mono text-slate-300">sh -l</code>
                  ）或执行 <code className="mono text-slate-300">{preview.sourceCommand}</code> 生效。
                </>
              ) : (
                <>
                  写入后新开终端自动生效，当前终端可执行{' '}
                  <code className="mono text-slate-300">{preview.sourceCommand}</code> 立即生效。
                </>
              )}
            </li>
            <li>
              后续新增、修改或删除端口认证，或调整代理端口并保存时，WebClash 会主动更新此托管块；关闭开关时仅移除托管块。
            </li>
          </ul>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-line bg-ink-950/40 px-3 py-2 text-[11px]">
            <span className="text-slate-400">
              当前终端立即生效：<code className="mono text-slate-200">{preview.sourceCommand}</code>
            </span>
            <CopyButton value={preview.sourceCommand} label="复制命令" />
          </div>
        )}
      </div>
    </Modal>
  );
}
