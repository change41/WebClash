import { useState } from 'react';
import { Eye, EyeOff, KeyRound, Loader2, ShieldCheck } from 'lucide-react';
import { api } from '../lib/api';
import { ThemeToggle } from '../components/ThemeToggle';
import { Button, Input } from '../components/ui';

export function Login({ onSuccess }: { onSuccess: () => void }) {
  const [password, setPassword] = useState('');
  const [show, setShow] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!password) return;
    setLoading(true);
    setError(null);
    try {
      await api('/auth/login', { method: 'POST', body: { password } });
      onSuccess();
    } catch (err) {
      setError((err as Error).message);
      setLoading(false);
    }
  };

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden px-4">
      <ThemeToggle className="absolute top-4 right-4 z-10" />
      <div className="pointer-events-none absolute -top-40 left-1/2 h-[420px] w-[720px] -translate-x-1/2 rounded-full bg-emerald-500/10 blur-[120px]" />
      <div className="pointer-events-none absolute -bottom-40 right-10 h-[360px] w-[520px] rounded-full bg-cyan-500/10 blur-[120px]" />
      <div className="login-dots pointer-events-none absolute inset-0" />

      <div className="animate-in relative w-full max-w-[380px]">
        <div className="mb-7 flex flex-col items-center text-center">
          <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-emerald-400/25 to-cyan-400/10 ring-1 ring-emerald-400/30">
            <ShieldCheck className="h-7 w-7 text-emerald-400" />
          </div>
          <h1 className="text-xl font-semibold tracking-wide text-slate-100">WebClash</h1>
          <p className="mt-1 text-xs text-slate-500">mihomo 内核 · Web 代理控制台</p>
        </div>

        <form onSubmit={submit} className="card space-y-4 p-6 shadow-2xl">
          <div>
            <label className="label" htmlFor="password">
              管理密码
            </label>
            <div className="relative">
              <KeyRound className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-slate-600" />
              <Input
                id="password"
                type={show ? 'text' : 'password'}
                value={password}
                autoFocus
                autoComplete="current-password"
                placeholder="请输入管理密码"
                className="pr-10 pl-9"
                onChange={(event) => setPassword(event.target.value)}
              />
              <button
                type="button"
                onClick={() => setShow((value) => !value)}
                className="absolute top-1/2 right-2.5 -translate-y-1/2 rounded p-1 text-slate-500 hover:text-slate-300"
                aria-label={show ? '隐藏密码' : '显示密码'}
              >
                {show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
          </div>

          {error && (
            <p className="rounded-lg border border-rose-500/25 bg-rose-500/10 px-3 py-2 text-xs text-rose-300">
              {error}
            </p>
          )}

          <Button
            type="submit"
            variant="primary"
            className="w-full"
            disabled={!password}
            loading={loading}
          >
            {loading ? '正在登录…' : '登录控制台'}
          </Button>

          <p className="text-center text-[11px] leading-relaxed text-slate-600">
            首次启动的管理密码会输出在服务端日志中，
            <br />
            并保存在 <code className="text-slate-500">data/initial-password.txt</code>
          </p>
        </form>

        <p className="mt-5 flex items-center justify-center gap-1.5 text-[10px] text-slate-600">
          <Loader2 className="hidden" />
          WebClash · Powered by MetaCubeX/mihomo
        </p>
      </div>
    </div>
  );
}
