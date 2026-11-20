package mihomo

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// builtinPolicies are mihomo's built-in rule targets; any other target must be
// a proxy or proxy-group name present in the generated config.
var builtinPolicies = map[string]struct{}{
	"DIRECT":      {},
	"REJECT":      {},
	"REJECT-DROP": {},
	"PASS":        {},
	"PASS-RULE":   {},
	"COMPATIBLE":  {},
	"GLOBAL":      {},
}

// RulePolicyError reports a custom rule whose target policy does not exist in
// the generated config. Index is 1-based within the rule list of its scope.
type RulePolicyError struct {
	Index  int
	Rule   string
	Policy string
	Groups []string
}

func (e *RulePolicyError) Error() string {
	msg := fmt.Sprintf("第 %d 条规则「%s」的策略「%s」不存在", e.Index, e.Rule, e.Policy)
	if len(e.Groups) > 0 {
		msg += fmt.Sprintf("；当前可用的代理组：%s", strings.Join(e.Groups, "、"))
	}
	return msg + "。请使用 DIRECT、REJECT 等内置策略，或配置中实际存在的代理组 / 节点名称"
}

// ValidateRulePolicies checks that every custom rule's policy is a built-in
// policy or a proxy / proxy-group name defined in cfg. It mirrors the kernel's
// "proxy [X] not found" check but runs before the config is written, so the
// panel can report the exact offending rule instead of letting mihomo exit.
func ValidateRulePolicies(cfg *yaml.Node, rules []string) error {
	if cfg == nil || len(rules) == 0 {
		return nil
	}
	names, groups := availablePolicies(cfg)
	for i, rule := range rules {
		_, policy, ok := rulePolicy(rule)
		if !ok || policy == "" {
			continue
		}
		if _, builtin := builtinPolicies[policy]; builtin {
			continue
		}
		if _, exists := names[policy]; exists {
			continue
		}
		return &RulePolicyError{
			Index:  i + 1,
			Rule:   strings.TrimSpace(rule),
			Policy: policy,
			Groups: groups,
		}
	}
	return nil
}

func availablePolicies(cfg *yaml.Node) (map[string]struct{}, []string) {
	names := map[string]struct{}{}
	var groups []string

	if proxies := resolvedNode(MapGet(cfg, "proxies")); proxies != nil && proxies.Kind == yaml.SequenceNode {
		for _, proxy := range proxies.Content {
			if name := mappingName(proxy); name != "" {
				names[name] = struct{}{}
			}
		}
	}
	if proxyGroups := resolvedNode(MapGet(cfg, "proxy-groups")); proxyGroups != nil && proxyGroups.Kind == yaml.SequenceNode {
		for _, group := range proxyGroups.Content {
			if name := mappingName(group); name != "" {
				names[name] = struct{}{}
				groups = append(groups, name)
			}
		}
	}
	if len(groups) > 8 {
		groups = append(groups[:8], "…")
	}
	return names, groups
}

func resolvedNode(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	return node
}

func mappingName(node *yaml.Node) string {
	node = resolvedNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return ""
	}
	name := resolvedNode(MapGet(node, "name"))
	if name == nil || name.Kind != yaml.ScalarNode {
		return ""
	}
	return name.Value
}

// rulePolicy extracts the policy field from a rule using mihomo's parsing rules:
// MATCH puts it second; regex/logical types may contain commas in the payload so
// their target is the last field; SUB-RULE's target is a sub-rule reference.
func rulePolicy(rule string) (typ, policy string, ok bool) {
	fields := splitRuleFields(rule)
	if len(fields) < 2 {
		return "", "", false
	}
	typ = strings.ToUpper(fields[0])
	switch typ {
	case "MATCH":
		return typ, fields[1], true
	case "SUB-RULE":
		return typ, "", false
	case "NOT", "OR", "AND", "DOMAIN-REGEX", "PROCESS-NAME-REGEX", "PROCESS-PATH-REGEX":
		return typ, fields[len(fields)-1], true
	}
	if len(fields) < 3 {
		return typ, "", false
	}
	return typ, fields[2], true
}

// splitRuleFields mirrors mihomo's ParseRulePayload: a plain comma split with
// spaces trimmed. In particular, quotes are not treated specially.
func splitRuleFields(rule string) []string {
	fields := strings.Split(rule, ",")
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	return fields
}
