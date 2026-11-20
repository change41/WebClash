import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from './api';
import { usePolling } from './hooks';
import type { GeoIPLookupResponse, GeoIPRegion, GeoIPStatus } from '../types';

/** 轮询 IP 归属功能状态（enable/ready/downloading/updateAvailable）。下载中加密轮询。 */
export function useGeoIPStatus(enabled = true) {
  const [intervalMs, setIntervalMs] = useState(5000);
  const poll = usePolling<GeoIPStatus>(() => api<GeoIPStatus>('/geoip/status'), intervalMs, enabled);
  const downloading = poll.data?.downloading ?? false;
  useEffect(() => {
    setIntervalMs(downloading ? 1500 : 5000);
  }, [downloading]);
  return poll;
}

const LOOKUP_BATCH = 256;

/**
 * 按目标（IP 或域名）缓存 + 去重 + 防抖批量查询归属。active=enable&&ready；变为 false 时清空缓存，
 * 以便重新启用/就绪后重新拉取。返回 regionOf(key) 与 requestLookup(keys)。
 * 域名由后端解析成 IP 后再查归属。不拦截 /ws/connections，仅通过独立 REST 富化。
 */
export function useGeoLookup(active: boolean) {
  const cacheRef = useRef<Map<string, GeoIPRegion | null>>(new Map());
  const pendingRef = useRef<Set<string>>(new Set());
  const timerRef = useRef<number | undefined>(undefined);
  const activeRef = useRef(active);
  activeRef.current = active;
  const [version, force] = useState(0);

  const flush = useCallback(async () => {
    timerRef.current = undefined;
    if (!activeRef.current) {
      pendingRef.current.clear();
      return;
    }
    const ips = Array.from(pendingRef.current)
      .filter((ip) => !cacheRef.current.has(ip))
      .slice(0, LOOKUP_BATCH);
    pendingRef.current.clear();
    if (ips.length === 0) return;
    try {
      const res = await api<GeoIPLookupResponse>('/geoip/lookup', { method: 'POST', body: { ips } });
      for (const ip of ips) cacheRef.current.set(ip, res.results[ip] ?? null);
    } catch {
      // 未就绪/网络失败：标记未知，避免每秒连接刷新时重复请求
      for (const ip of ips) cacheRef.current.set(ip, null);
    }
    force((n) => n + 1);
  }, []);

  const requestLookup = useCallback(
    (ips: string[]) => {
      if (!activeRef.current) return;
      let added = false;
      for (const ip of ips) {
        if (!ip || cacheRef.current.has(ip) || pendingRef.current.has(ip)) continue;
        pendingRef.current.add(ip);
        added = true;
      }
      if (added && timerRef.current === undefined) {
        timerRef.current = window.setTimeout(() => void flush(), 300);
      }
    },
    [flush],
  );

  const regionOf = useCallback((ip: string): GeoIPRegion | null => cacheRef.current.get(ip) ?? null, []);

  useEffect(() => {
    if (!active) {
      cacheRef.current.clear();
      pendingRef.current.clear();
      if (timerRef.current !== undefined) {
        window.clearTimeout(timerRef.current);
        timerRef.current = undefined;
      }
      force((n) => n + 1);
    }
  }, [active]);

  useEffect(
    () => () => {
      if (timerRef.current !== undefined) window.clearTimeout(timerRef.current);
    },
    [],
  );

  return { regionOf, requestLookup, version };
}

/** 把归属拼成简洁展示串（国家 省 市 ISP），空段跳过；无结果返回空串。 */
export function formatRegion(r: GeoIPRegion | null): string {
  if (!r) return '';
  if (r.country === 'Reserved') return '保留地址';
  return [r.country, r.province, r.city, r.isp].filter(Boolean).join(' ');
}

const COUNTRY_CODE_ALIASES: Record<string, string> = {
  UK: 'GB',
  EL: 'GR',
};

/** ISO 3166-1 alpha-2 国家码转 flag-icons 的 SVG 国旗类名；无效或空码返回空串。 */
export function countryFlagClass(code?: string | null): string {
  const normalized = (code ?? '').trim().toUpperCase();
  const iso = COUNTRY_CODE_ALIASES[normalized] ?? normalized;
  if (!/^[A-Z]{2}$/.test(iso)) return '';
  return `fi fi-${iso.toLowerCase()}`;
}