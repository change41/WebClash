import { useCallback, useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { ShieldCheck } from 'lucide-react';
import { Layout } from './components/Layout';
import { api, setUnauthorizedHandler } from './lib/api';
import { KernelProvider } from './lib/kernel';
import { ToastProvider } from './lib/toast';
import { Connections } from './pages/Connections';
import { Dashboard } from './pages/Dashboard';
import { Login } from './pages/Login';
import { Logs } from './pages/Logs';
import { Profiles } from './pages/Profiles';
import { Proxies } from './pages/Proxies';
import { Rules } from './pages/Rules';
import { Settings } from './pages/Settings';
import type { AuthStatus } from './types';

function Splash() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4">
      <div className="flex h-12 w-12 animate-pulse items-center justify-center rounded-2xl bg-gradient-to-br from-emerald-400/25 to-cyan-400/10 ring-1 ring-emerald-400/30">
        <ShieldCheck className="h-6 w-6 text-emerald-400" />
      </div>
      <p className="text-xs text-slate-500">正在连接 WebClash…</p>
    </div>
  );
}

export default function App() {
  const [auth, setAuth] = useState<AuthStatus | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      const status = await api<AuthStatus>('/auth/status');
      setAuth(status);
    } catch {
      setAuth(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    setUnauthorizedHandler(() => {
      setAuth((previous) =>
        previous && previous.authenticated ? { ...previous, authenticated: false } : previous,
      );
    });
    return () => setUnauthorizedHandler(null);
  }, []);

  if (loading) return <Splash />;
  if (!auth?.authenticated) return <Login onSuccess={load} />;

  const logout = async () => {
    try {
      await api('/auth/logout', { method: 'POST' });
    } catch {
      // 无论服务端是否成功，前端都退出
    }
    setAuth((previous) => (previous ? { ...previous, authenticated: false } : previous));
  };

  return (
    <ToastProvider>
      <KernelProvider>
        <BrowserRouter>
          <Layout version={auth.version} onLogout={() => void logout()}>
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/proxies" element={<Proxies />} />
              <Route path="/profiles" element={<Profiles />} />
              <Route path="/connections" element={<Connections />} />
              <Route path="/rules" element={<Rules />} />
              <Route path="/logs" element={<Logs />} />
              <Route path="/settings" element={<Settings />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Layout>
        </BrowserRouter>
      </KernelProvider>
    </ToastProvider>
  );
}
