export function cx(...values: Array<string | false | null | undefined>): string {
  return values.filter(Boolean).join(' ');
}

export function formatBytes(bytes: number | null | undefined, digits = 1): string {
  if (bytes == null || !Number.isFinite(bytes)) return '—';
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let value = Math.abs(bytes);
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  const fixed = value >= 100 || index === 0 ? value.toFixed(0) : value.toFixed(digits);
  return `${bytes < 0 ? '-' : ''}${fixed} ${units[index]}`;
}

export function formatSpeed(bytesPerSecond: number | null | undefined): string {
  if (bytesPerSecond == null || !Number.isFinite(bytesPerSecond)) return '—';
  return `${formatBytes(bytesPerSecond)}/s`;
}

export function formatCount(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—';
  return value.toLocaleString('zh-CN');
}

export function formatDuration(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms) || ms < 0) return '—';
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} 分 ${seconds % 60} 秒`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时 ${minutes % 60} 分`;
  const days = Math.floor(hours / 24);
  return `${days} 天 ${hours % 24} 小时`;
}

export function formatRelative(ts: number | null | undefined): string {
  if (ts == null || !Number.isFinite(ts)) return '—';
  const diff = Date.now() - ts;
  if (diff < 0) return formatDateTime(ts);
  if (diff < 60_000) return '刚刚';
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
  if (diff < 30 * 86_400_000) return `${Math.floor(diff / 86_400_000)} 天前`;
  return formatDateTime(ts);
}

export function formatDateTime(ts: number | null | undefined): string {
  if (ts == null || !Number.isFinite(ts)) return '—';
  return new Date(ts).toLocaleString('zh-CN', { hour12: false });
}

export function formatTimestamp(ts: number | null | undefined): string {
  if (ts == null || !Number.isFinite(ts)) return '—';
  return new Date(ts).toLocaleTimeString('zh-CN', { hour12: false });
}

export function formatExpire(seconds: number | null | undefined): string {
  if (!seconds || !Number.isFinite(seconds)) return '—';
  return new Date(seconds * 1000).toLocaleDateString('zh-CN');
}

export function latencyTone(delay: number | null | undefined): string {
  if (delay == null || delay <= 0) return 'text-slate-500';
  if (delay < 150) return 'latency-good';
  if (delay < 400) return 'latency-mid';
  return 'latency-bad';
}

export function latencyText(delay: number | null | undefined): string {
  if (delay == null) return '—';
  if (delay <= 0) return '超时';
  return `${delay} ms`;
}

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}
