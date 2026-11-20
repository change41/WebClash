import { useCallback, useEffect, useRef, useState } from 'react';

/** 定时轮询，enabled 为 false 时停止请求 */
export function usePolling<T>(
  fetcher: () => Promise<T>,
  intervalMs: number,
  enabled = true,
  deps: unknown[] = [],
) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(enabled);
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  const refresh = useCallback(async () => {
    try {
      const next = await fetcherRef.current();
      setData(next);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) {
      setLoading(false);
      return;
    }
    setLoading(true);
    void refresh();
    const timer = window.setInterval(() => void refresh(), Math.max(250, intervalMs));
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, intervalMs, refresh, ...deps]);

  return { data, error, loading, refresh, setData };
}

interface WsOptions {
  /** 断线重连间隔基数（毫秒） */
  retryBase?: number;
  enabled?: boolean;
}

/** 订阅 WebSocket JSON 数据流，自动断线重连 */
export function useWsJson<T>(
  path: string | null,
  onMessage: (data: T) => void,
  options: WsOptions = {},
) {
  const { retryBase = 800, enabled = true } = options;
  const handlerRef = useRef(onMessage);
  handlerRef.current = onMessage;
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (!path || !enabled) {
      setConnected(false);
      return;
    }
    let socket: WebSocket | null = null;
    let closed = false;
    let retry = 0;
    let timer: number | undefined;

    const connect = () => {
      const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
      socket = new WebSocket(`${protocol}://${window.location.host}${path}`);
      socket.onopen = () => {
        retry = 0;
        setConnected(true);
      };
      socket.onmessage = (event) => {
        try {
          const parsed = JSON.parse(String(event.data)) as T;
          handlerRef.current(parsed);
        } catch {
          // 忽略非 JSON 消息
        }
      };
      socket.onclose = () => {
        setConnected(false);
        if (closed) return;
        const delay = Math.min(15_000, retryBase * 2 ** retry);
        retry = Math.min(retry + 1, 6);
        timer = window.setTimeout(connect, delay);
      };
      socket.onerror = () => {
        socket?.close();
      };
    };

    connect();
    return () => {
      closed = true;
      if (timer) window.clearTimeout(timer);
      socket?.close();
      setConnected(false);
    };
  }, [path, enabled, retryBase]);

  return connected;
}

/** 每秒触发一次重绘，用于展示时长等实时数据 */
export function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(timer);
  }, [intervalMs]);
  return now;
}
