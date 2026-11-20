// Package mihomo ports server/src/mihomo: the runtime config builder and the
// Clash API client. The config builder merges panel settings into a subscription
// base so ports/controller/DNS/TUN are always panel-controlled.
//
// The builder operates on yaml.Node so subscription key order, scalar types and
// unknown fields (proxy-groups, rule-providers, DNS extensions, …) round-trip
// untouched; only panel-owned keys are injected or replaced.
package mihomo

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"webclash/internal/config"
)

func strNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func boolNode(b bool) *yaml.Node {
	v := "false"
	if b {
		v = "true"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: v}
}

func intNode(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(n)}
}

func strSeq(items []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, it := range items {
		n.Content = append(n.Content, strNode(it))
	}
	return n
}

func emptySeq() *yaml.Node { return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"} }

// Exported node helpers for the geo module (which post-processes the built config).
func StrNode(s string) *yaml.Node                  { return strNode(s) }
func BoolNode(b bool) *yaml.Node                   { return boolNode(b) }
func StrSeq(items []string) *yaml.Node             { return strSeq(items) }
func MappingNode(v *yaml.Node) *yaml.Node          { return mappingNode(v) }
func SetNode(m *yaml.Node, k string, v *yaml.Node) { setNode(m, k, v) }

// mappingNode returns v when it is a mapping, else a fresh empty mapping
// (port of asObject: non-plain-object → {}).
func mappingNode(v *yaml.Node) *yaml.Node {
	if v != nil && v.Kind == yaml.MappingNode {
		return v
	}
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func keyIndex(m *yaml.Node, key string) int {
	if m == nil {
		return -1
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// MapGet returns the value node for key, or nil when absent.
func MapGet(m *yaml.Node, key string) *yaml.Node {
	if i := keyIndex(m, key); i >= 0 {
		return m.Content[i+1]
	}
	return nil
}

func deleteKey(m *yaml.Node, key string) {
	if i := keyIndex(m, key); i >= 0 {
		m.Content = append(m.Content[:i], m.Content[i+2:]...)
	}
}

// setNode replaces key's value in place (preserving position) or appends it,
// matching JS object assignment / spread ordering.
func setNode(m *yaml.Node, key string, val *yaml.Node) {
	if i := keyIndex(m, key); i >= 0 {
		m.Content[i+1] = val
		return
	}
	m.Content = append(m.Content, strNode(key), val)
}

func authenticationNode(users []config.InboundUser) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, u := range users {
		n.Content = append(n.Content, strNode(u.Username+":"+u.Password))
	}
	return n
}

// BuildConfig merges settings into base (port of buildMihomoConfig). base is the
// parsed subscription mapping node (or nil for the builtin direct config). The
// returned mapping is ready to yaml.Marshal with original ordering preserved.
func BuildConfig(base *yaml.Node, s config.Settings) *yaml.Node {
	cfg := mappingNode(base)

	// Remove built-in console / controller entrypoints that bypass the panel.
	for _, k := range []string{
		"external-ui", "external-ui-name", "external-ui-url",
		"external-controller-unix", "external-controller-pipe", "external-controller-tls",
	} {
		deleteKey(cfg, k)
	}

	setNode(cfg, "mode", strNode(s.General.Mode))
	setNode(cfg, "log-level", strNode(s.General.LogLevel))
	setNode(cfg, "ipv6", boolNode(s.General.Ipv6))

	setNode(cfg, "mixed-port", intNode(s.Ports.Mixed))
	// Standalone HTTP/SOCKS ports are panel-controlled: emit them when set and
	// strip subscription leftovers when disabled, so the effective config
	// matches the UI.
	if s.Ports.HTTP > 0 {
		setNode(cfg, "port", intNode(s.Ports.HTTP))
	} else {
		deleteKey(cfg, "port")
	}
	if s.Ports.SOCKS > 0 {
		setNode(cfg, "socks-port", intNode(s.Ports.SOCKS))
	} else {
		deleteKey(cfg, "socks-port")
	}
	// The panel owns proxy authentication: it applies to the mixed, HTTP and
	// SOCKS ports. Remove subscription leftovers when the panel's list is empty
	// so the effective config matches the UI.
	if len(s.Authentication) > 0 {
		setNode(cfg, "authentication", authenticationNode(s.Authentication))
	} else {
		deleteKey(cfg, "authentication")
	}

	setNode(cfg, "allow-lan", boolNode(s.General.AllowLan))
	if s.General.AllowLan {
		bind := s.General.BindAddress
		if bind == "" {
			bind = "*"
		}
		setNode(cfg, "bind-address", strNode(bind))
	} else {
		deleteKey(cfg, "bind-address")
	}

	// Controller listens on loopback only; the panel reverse-proxies it.
	setNode(cfg, "external-controller", strNode(fmt.Sprintf("127.0.0.1:%d", s.Controller.Port)))
	setNode(cfg, "secret", strNode(s.Controller.Secret))

	if MapGet(cfg, "unified-delay") == nil {
		setNode(cfg, "unified-delay", boolNode(true))
	}
	if MapGet(cfg, "tcp-concurrent") == nil {
		setNode(cfg, "tcp-concurrent", boolNode(true))
	}
	if MapGet(cfg, "find-process-mode") == nil {
		setNode(cfg, "find-process-mode", strNode("strict"))
	}

	profile := mappingNode(MapGet(cfg, "profile"))
	setNode(profile, "store-selected", boolNode(true))
	setNode(profile, "store-fake-ip", boolNode(true))
	setNode(cfg, "profile", profile)

	dns := mappingNode(MapGet(cfg, "dns"))
	setNode(dns, "enable", boolNode(s.Dns.Enable))
	setNode(dns, "ipv6", boolNode(s.General.Ipv6))
	setNode(dns, "listen", strNode(s.Dns.Listen))
	setNode(dns, "enhanced-mode", strNode(s.Dns.EnhancedMode))
	setNode(dns, "fake-ip-range", strNode(s.Dns.FakeIpRange))
	setNode(dns, "nameserver", strSeq(s.Dns.Nameservers))
	setNode(dns, "fallback", strSeq(s.Dns.Fallback))
	setNode(dns, "default-nameserver", strSeq(s.Dns.DefaultNameservers))
	setNode(cfg, "dns", dns)

	tun := mappingNode(MapGet(cfg, "tun"))
	setNode(tun, "enable", boolNode(s.Tun.Enable))
	setNode(tun, "stack", strNode(s.Tun.Stack))
	setNode(tun, "auto-route", boolNode(s.Tun.AutoRoute))
	setNode(tun, "auto-detect-interface", boolNode(s.Tun.AutoDetectInterface))
	setNode(tun, "dns-hijack", strSeq(s.Tun.DnsHijack))
	setNode(cfg, "tun", tun)

	if pp := MapGet(cfg, "proxy-providers"); pp != nil && pp.Tag == "!!null" {
		deleteKey(cfg, "proxy-providers")
	}
	if pr := MapGet(cfg, "proxies"); pr == nil || pr.Kind != yaml.SequenceNode {
		setNode(cfg, "proxies", emptySeq())
	}
	if rules := MapGet(cfg, "rules"); rules == nil || rules.Kind != yaml.SequenceNode || len(rules.Content) == 0 {
		setNode(cfg, "rules", strSeq([]string{"MATCH,DIRECT"}))
	}
	PrependRules(cfg, s.CustomRules)

	return cfg
}

// PrependRules inserts rule lines before the existing rules sequence, keeping
// the "rules" key position. Used for globally-scoped and per-profile custom
// rules; subscription rules keep their original order after them.
func PrependRules(cfg *yaml.Node, rules []string) *yaml.Node {
	if cfg == nil || cfg.Kind != yaml.MappingNode || len(rules) == 0 {
		return cfg
	}
	existing := MapGet(cfg, "rules")
	var tail []*yaml.Node
	if existing != nil && existing.Kind == yaml.SequenceNode {
		tail = existing.Content
	}
	merged := make([]*yaml.Node, 0, len(rules)+len(tail))
	for _, rule := range rules {
		if s := strings.TrimSpace(rule); s != "" {
			merged = append(merged, strNode(s))
		}
	}
	merged = append(merged, tail...)
	setNode(cfg, "rules", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: merged})
	return cfg
}
