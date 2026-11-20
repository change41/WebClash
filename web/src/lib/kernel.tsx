import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { api } from './api';
import { useToast } from './toast';
import type { KernelStatus } from '../types';

interface KernelApi {
  status: KernelStatus | null;
  error: string | null;
  busy: boolean;
  refresh: () => Promise<void>;
  start: () => Promise<void>;
  stop: () => Promise<void>;
  restart: () => Promise<void>;
}

const KernelContext = createContext<KernelApi | null>(null);

export function KernelProvider({ children }: { children: ReactNode }) {
  const toast = useToast();
  const [status, setStatus] = useState<KernelStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const next = await api<KernelStatus>('/kernel/status');
      setStatus(next);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    }
  }, []);

  const downloadActive = status?.download.active ?? false;

  useEffect(() => {
    let cancelled = false;
    const tick = async () => {
      try {
        const next = await api<KernelStatus>('/kernel/status');
        if (!cancelled) {
          setStatus(next);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) setError((err as Error).message);
      }
    };
    void tick();
    const timer = window.setInterval(tick, downloadActive ? 800 : 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [downloadActive]);

  const run = useCallback(
    async (action: 'start' | 'stop' | 'restart') => {
      setBusy(true);
      try {
        const next = await api<KernelStatus>(`/kernel/${action}`, { method: 'POST' });
        setStatus(next);
        setError(null);
        toast.success(
          action === 'start' ? '内核已启动' : action === 'stop' ? '内核已停止' : '内核已重启',
        );
      } catch (err) {
        toast.error((err as Error).message);
        await refresh();
      } finally {
        setBusy(false);
      }
    },
    [refresh, toast],
  );

  const value = useMemo<KernelApi>(
    () => ({
      status,
      error,
      busy,
      refresh,
      start: () => run('start'),
      stop: () => run('stop'),
      restart: () => run('restart'),
    }),
    [status, error, busy, refresh, run],
  );

  return <KernelContext.Provider value={value}>{children}</KernelContext.Provider>;
}

export function useKernel(): KernelApi {
  const ctx = useContext(KernelContext);
  if (!ctx) throw new Error('useKernel 必须在 KernelProvider 内使用');
  return ctx;
}
