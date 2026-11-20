export type LogLevel = 'debug' | 'info' | 'warning' | 'error' | 'silent';
export type ProxyMode = 'rule' | 'global' | 'direct';
export type TunStack = 'gvisor' | 'system' | 'mixed';
export type DnsMode = 'fake-ip' | 'redir-host';
export type GeoIPSource = 'ip2region' | 'mmdb';

export interface InboundUser {
  username: string;
  password: string;
}

export interface AppSettings {
  server: { port: number; host: string };
  auth: { hasPassword: boolean };
  general: {
    mode: ProxyMode;
    logLevel: LogLevel;
    allowLan: boolean;
    bindAddress: string;
    ipv6: boolean;
  };
  ports: { mixed: number; http: number; socks: number };
  authentication: InboundUser[];
  controller: { port: number; secret: string };
  tun: {
    enable: boolean;
    stack: TunStack;
    autoRoute: boolean;
    autoDetectInterface: boolean;
    dnsHijack: string[];
  };
  dns: {
    enable: boolean;
    listen: string;
    enhancedMode: DnsMode;
    fakeIpRange: string;
    nameservers: string[];
    fallback: string[];
    defaultNameservers: string[];
  };
  kernel: {
    autoStart: boolean;
    autoDownload: boolean;
    binaryPath: string;
    version: string;
    mirror: string;
  };
  geoip: {
    enable: boolean;
    source: GeoIPSource;
  };
  connHistory: {
    limit: number;
  };
  customRules: string[];
}

export interface SettingsResponse {
  settings: AppSettings;
  config: {
    path: string;
    dataDir: string;
    lastConfigAt: number | null;
    lastConfigError: string | null;
  };
}

export interface GeoIPRegion {
  country: string;
  countryCode: string;
  province: string;
  city: string;
  isp: string;
  raw: string;
}

export interface GeoIPFileStatus {
  name: string;
  size: number;
  updatedAt: number;
  version?: string;
}

export interface GeoIPStatus {
  enable: boolean;
  source: GeoIPSource;
  ready: boolean;
  downloading: boolean;
  files: GeoIPFileStatus[];
  lastCheckAt: number | null;
  updateAvailable: boolean;
  error: string | null;
}

export interface GeoIPFileMeta {
  etag: string;
  lastModified: string;
  size: number;
  updatedAt: number;
  version?: string;
}

export interface GeoIPCheckFile {
  name: string;
  remote: GeoIPFileMeta;
  local: GeoIPFileMeta;
}

export interface GeoIPCheckResult {
  updateAvailable: boolean;
  files: GeoIPCheckFile[];
  lastCheckAt: number;
}

export interface GeoIPLookupResponse {
  results: Record<string, GeoIPRegion>;
}

export interface SubscriptionUserInfo {
  upload?: number;
  download?: number;
  total?: number;
  expire?: number;
}

export interface ProfileSettings {
  intervalHours: number;
  testUrl: string;
  testTimeout: number;
  customRules: string[];
}

export interface ProfileMeta {
  id: string;
  name: string;
  type: 'remote' | 'local';
  url?: string;
  createdAt: number;
  updatedAt: number;
  userInfo?: SubscriptionUserInfo;
  settings: ProfileSettings;
}

export interface ProfilesResponse {
  activeId: string | null;
  profiles: ProfileMeta[];
  lastConfigAt: number | null;
  lastConfigError: string | null;
}

export interface DownloadState {
  active: boolean;
  progress: number;
  received: number;
  total: number;
  stage: 'resolving' | 'downloading' | 'extracting' | null;
  version: string | null;
  error: string | null;
}

export interface KernelStatus {
  state: 'stopped' | 'starting' | 'running' | 'stopping' | 'error';
  running: boolean;
  pid: number | null;
  startedAt: number | null;
  lastError: string | null;
  binary: {
    exists: boolean;
    path: string | null;
    version: string | null;
    managed: boolean;
  };
  download: DownloadState;
}

export interface KernelUpdateCheck {
  current: string | null;
  latest: string;
  hasUpdate: boolean;
}

export interface AuthStatus {
  name: string;
  version: string;
  authenticated: boolean;
  hasPassword: boolean;
  kernelRunning: boolean;
}

export interface ClashProxy {
  name: string;
  type: string;
  now?: string;
  all?: string[];
  history?: { time: string; delay: number }[];
  alive?: boolean;
  udp?: boolean;
  xudp?: boolean;
  provider?: string;
  icon?: string;
  hidden?: boolean;
}

export interface ProxiesResponse {
  proxies: Record<string, ClashProxy>;
}

export interface ProxyProvider {
  name: string;
  type: string;
  vehicleType: string;
  proxies: ClashProxy[];
  updatedAt?: string;
  subscriptionInfo?: SubscriptionUserInfo;
}

export interface ProvidersResponse {
  providers: Record<string, ProxyProvider>;
}

export interface DelayResponse {
  delay: number;
}

export interface GroupDelayResponse {
  delay: Record<string, number>;
}

export interface ClashMetadata {
  network: string;
  type: string;
  sourceIP: string;
  destinationIP: string;
  sourcePort: string;
  destinationPort: string;
  host: string;
  dnsMode?: string;
  processPath?: string;
  specialProxy?: string;
  remoteDestination?: string;
}

export interface ClashConnection {
  id: string;
  upload: number;
  download: number;
  start: string;
  chains: string[];
  rule: string;
  rulePayload: string;
  metadata: ClashMetadata;
}

export interface ConnectionsPayload {
  downloadTotal: number;
  uploadTotal: number;
  connections: ClashConnection[] | null;
  memory?: number;
}

export interface HistoryConnection extends ClashConnection {
  closedAt: number;
  lastSeen: number;
}

export interface ConnectionHistoryResponse {
  connections: HistoryConnection[];
  limit: number;
}

export interface ClashRule {
  type: string;
  payload: string;
  proxy: string;
  size?: number;
  extra?: {
    disabled?: boolean;
    hitCount?: number;
    hitAt?: string;
    missCount?: number;
    missAt?: string;
  };
}

export interface RulesResponse {
  rules: ClashRule[];
}

export interface RuleProvider {
  name: string;
  type: string;
  behavior: string;
  ruleCount: number;
  vehicleType: string;
  updatedAt: string;
  format?: string;
}

export interface RuleProvidersResponse {
  providers: Record<string, RuleProvider>;
}

export interface RuleProviderContent extends RuleProvider {
  format: 'yaml' | 'text' | 'mrs';
  source: 'inline' | 'cache' | 'remote';
  rules: string[];
}

export interface ClashConfigsResponse {
  port: number;
  'socks-port': number;
  'mixed-port': number;
  'allow-lan': boolean;
  mode: ProxyMode;
  'log-level': LogLevel;
  ipv6: boolean;
  [key: string]: unknown;
}

export interface TrafficMessage {
  up: number;
  down: number;
}

export interface TrafficHistoryPoint {
  t: number;
  up: number;
  down: number;
}

export interface TrafficHistoryResponse {
  bucketSeconds: number;
  windowSeconds: number;
  points: TrafficHistoryPoint[];
}

export interface SystemStatus {
  ready: boolean;
  updatedAt: number | null;
  cpu: { percent: number; cores: number };
  memory: { total: number; used: number; percent: number };
  load: { load1: number; load5: number; load15: number };
}

export interface MemoryMessage {
  inuse: number;
  oslimit: number;
}

export interface LogMessage {
  type: string;
  payload: string;
}
