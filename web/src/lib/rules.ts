import type { ClashRule } from '../types';

export interface RuleExample {
  description: string;
  rule: string;
}

export type RuleSource = 'profile' | 'global' | 'subscription' | 'builtin';

export const RULE_SOURCE_LABEL: Record<RuleSource, string> = {
  profile: '订阅扩展',
  global: '全局扩展',
  subscription: '订阅',
  builtin: '内置',
};

export const RULE_EXAMPLES: RuleExample[] = [
  { description: '完整域名匹配：只匹配指定的完整域名。', rule: 'DOMAIN,www.example.com,DIRECT' },
  {
    description: '域名后缀匹配：匹配该域名及其所有子域名。',
    rule: 'DOMAIN-SUFFIX,example.com,DIRECT',
  },
  {
    description: '域名关键字匹配：域名中包含关键字即命中。',
    rule: 'DOMAIN-KEYWORD,google,DIRECT',
  },
  {
    description: '域名正则匹配：按正则表达式匹配域名。',
    rule: 'DOMAIN-REGEX,^ad\\..*\\.com$,REJECT',
  },
  {
    description: 'IPv4 网段匹配：no-resolve 表示不解析域名，只匹配直连 IP。',
    rule: 'IP-CIDR,1.1.1.1/32,DIRECT,no-resolve',
  },
  {
    description: 'IPv6 网段匹配：按 IPv6 地址段匹配。',
    rule: 'IP-CIDR6,2001:db8::/32,DIRECT,no-resolve',
  },
  { description: 'IP 归属地匹配：按国家或地区代码匹配 IP。', rule: 'GEOIP,CN,DIRECT' },
  {
    description: 'GeoSite 分类匹配：按内置站点分类批量匹配域名。',
    rule: 'GEOSITE,category-ads-all,REJECT',
  },
  {
    description: '规则集匹配：引用配置中已定义的 rule-provider。',
    rule: 'RULE-SET,ads,REJECT',
  },
  {
    description:
      '目标端口匹配（代理）：按目标端口匹配并交给代理组；PROXY 需是当前订阅中实际存在的代理组或节点，否则保存时会指出具体规则。',
    rule: 'DST-PORT,443,PROXY',
  },
  { description: '网络类型匹配：按 TCP 或 UDP 协议匹配。', rule: 'NETWORK,UDP,DIRECT' },
  {
    description: '进程名匹配：按发起连接的进程名匹配，平台相关。',
    rule: 'PROCESS-NAME,Telegram,DIRECT',
  },
  {
    description: '逻辑与：多个条件同时满足时命中。',
    rule: 'AND,((DOMAIN-SUFFIX,example.com),(NETWORK,TCP)),DIRECT',
  },
];

export interface ParsedRuleText {
  rules: string[];
  duplicateCount: number;
}

/** 解析多行规则文本：空行与 # 注释行会被丢弃，重复规则只保留首次出现。 */
export function parseRuleTextWithStats(text: string): ParsedRuleText {
  const seen = new Set<string>();
  const rules: string[] = [];
  let duplicateCount = 0;
  for (const line of text.split('\n')) {
    const trimmed = line.trim();
    if (trimmed === '' || trimmed.startsWith('#')) continue;
    const rule = trimmed
      .split(',')
      .map((part) => part.trim())
      .join(',');
    if (seen.has(rule)) {
      duplicateCount += 1;
      continue;
    }
    seen.add(rule);
    rules.push(rule);
  }
  return { rules, duplicateCount };
}

/** 把多行文本转换为去重后的自定义规则数组。 */
export function parseRuleText(text: string): string[] {
  return parseRuleTextWithStats(text).rules;
}

export function rulesToText(rules?: string[] | null): string {
  return (rules ?? []).join('\n');
}

const RULE_TYPE_LABELS: Record<string, string> = {
  Domain: 'DOMAIN',
  DomainSuffix: 'DOMAIN-SUFFIX',
  DomainKeyword: 'DOMAIN-KEYWORD',
  DomainRegex: 'DOMAIN-REGEX',
  DomainWildcard: 'DOMAIN-WILDCARD',
  GeoSite: 'GEOSITE',
  GeoIP: 'GEOIP',
  SrcGeoIP: 'SRC-GEOIP',
  IPASN: 'IPASN',
  SrcIPASN: 'SRC-IPASN',
  IPCIDR: 'IP-CIDR',
  SrcIPCIDR: 'SRC-IP-CIDR',
  IPSuffix: 'IP-SUFFIX',
  SrcIPSuffix: 'SRC-IP-SUFFIX',
  SrcPort: 'SRC-PORT',
  DstPort: 'DST-PORT',
  InPort: 'IN-PORT',
  InUser: 'IN-USER',
  InName: 'IN-NAME',
  InType: 'IN-TYPE',
  ProcessName: 'PROCESS-NAME',
  ProcessPath: 'PROCESS-PATH',
  ProcessNameRegex: 'PROCESS-NAME-REGEX',
  ProcessPathRegex: 'PROCESS-PATH-REGEX',
  ProcessNameWildcard: 'PROCESS-NAME-WILDCARD',
  ProcessPathWildcard: 'PROCESS-PATH-WILDCARD',
  RuleSet: 'RULE-SET',
  Network: 'NETWORK',
  Uid: 'UID',
  SubRules: 'SUB-RULE',
  Match: 'MATCH',
};

/** 把内核返回的 CamelCase 规则类型转成配置里使用的写法。 */
export function ruleTypeLabel(type: string): string {
  return RULE_TYPE_LABELS[type] ?? type.toUpperCase().replace(/_/g, '-');
}

const POLICY_LAST_RULE_TYPES = new Set([
  'NOT',
  'OR',
  'AND',
  'DOMAIN-REGEX',
  'PROCESS-NAME-REGEX',
  'PROCESS-PATH-REGEX',
]);

function compactRuleType(type: string): string {
  const compact = type.trim().toUpperCase().replace(/[-_\s]/g, '');
  return compact === 'SUBRULES' ? 'SUBRULE' : compact;
}

function ruleSignature(type: string, payload: string, proxy: string): string {
  return `${compactRuleType(type)}\u0000${payload.trim()}\u0000${proxy.trim()}`;
}

function ruleSignatureFromLine(rule: string): string | null {
  const fields = rule.split(',').map((field) => field.trim());
  if (fields.length < 2) return null;
  const type = fields[0].toUpperCase();
  if (type === 'MATCH') {
    return ruleSignature(type, '', fields[1] ?? '');
  }
  if (POLICY_LAST_RULE_TYPES.has(type)) {
    return ruleSignature(type, fields.slice(1, -1).join(','), fields[fields.length - 1] ?? '');
  }
  if (fields.length < 3) return null;
  return ruleSignature(type, fields[1], fields[2]);
}

function runtimeRuleSignature(rule: Pick<ClashRule, 'type' | 'payload' | 'proxy'>): string {
  return ruleSignature(rule.type, rule.payload ?? '', rule.proxy ?? '');
}

/**
 * 内核按「当前订阅扩展 → 全局扩展 → 订阅规则」的顺序返回规则。
 * 只给连续且签名匹配的前缀打标，配置与运行时不一致时宁可不标，避免误导。
 */
export function classifyRuleSources(
  runtimeRules: Pick<ClashRule, 'type' | 'payload' | 'proxy'>[],
  profileRules: string[],
  globalRules: string[],
  hasActiveProfile: boolean,
): RuleSource[] {
  const sources: RuleSource[] = runtimeRules.map(() =>
    hasActiveProfile ? 'subscription' : 'builtin',
  );
  const expected: { rule: string; source: RuleSource }[] = [
    ...profileRules.map((rule) => ({ rule, source: 'profile' as const })),
    ...globalRules.map((rule) => ({ rule, source: 'global' as const })),
  ];
  let cursor = 0;
  for (const item of expected) {
    const signature = ruleSignatureFromLine(item.rule);
    if (!signature || cursor >= runtimeRules.length) break;
    if (runtimeRuleSignature(runtimeRules[cursor]) !== signature) break;
    sources[cursor] = item.source;
    cursor += 1;
  }
  return sources;
}

