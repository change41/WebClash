// Package geo fixes the GeoSite.dat startup deadlock: mihomo is never allowed to
// download geo data itself (geodata-mode + geo-auto-update:false + geox-url),
// the panel pre-provisions the .dat files from a mirror chain before start, and
// when a file is missing the config degrades (geo rules/DNS refs stripped,
// MATCH,DIRECT fallback) so the kernel always starts. Nothing here is fatal.
package geo

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"webclash/internal/mihomo"
)

// Needed reports which geo data files the config references.
type Needed struct {
	GeoIP   bool
	GeoSite bool
}

// Present reports which geo data files exist and are plausibly valid (>=1KB).
type Present struct {
	GeoIP   bool
	GeoSite bool
}

const minDatSize = 1024

// CheckFiles stats the kernel dir for usable geoip.dat/geosite.dat (no network).
func CheckFiles(kernelDir string) Present {
	return Present{
		GeoIP:   validDat(filepath.Join(kernelDir, "geoip.dat")),
		GeoSite: validDat(filepath.Join(kernelDir, "geosite.dat")),
	}
}

func validDat(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() >= minDatSize
}

// Analyze scans rules (GEOSITE/GEOIP tokens) and the DNS subtree (geosite:/geoip:
// references) to decide which files are needed. Pure, in-memory.
func Analyze(cfg *yaml.Node) Needed {
	var n Needed
	if rules := mihomo.MapGet(cfg, "rules"); rules != nil && rules.Kind == yaml.SequenceNode {
		for _, r := range rules.Content {
			if r.Kind != yaml.ScalarNode {
				continue
			}
			switch ruleToken(r.Value) {
			case "GEOSITE":
				n.GeoSite = true
			case "GEOIP":
				n.GeoIP = true
			}
		}
	}
	if dns := mihomo.MapGet(cfg, "dns"); dns != nil {
		scanDNS(dns, &n)
	}
	return n
}

func ruleToken(rule string) string {
	first := rule
	if i := strings.IndexByte(rule, ','); i >= 0 {
		first = rule[:i]
	}
	return strings.ToUpper(strings.TrimSpace(first))
}

func scanDNS(node *yaml.Node, n *Needed) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.ScalarNode:
		v := strings.ToLower(node.Value)
		if strings.Contains(v, "geosite:") {
			n.GeoSite = true
		}
		if strings.Contains(v, "geoip:") {
			n.GeoIP = true
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			scanDNS(node.Content[i], n)
			scanDNS(node.Content[i+1], n)
		}
	case yaml.SequenceNode:
		for _, c := range node.Content {
			scanDNS(c, n)
		}
	}
}

// Apply forces panel-owned geo settings and degrades the config when files are
// missing, returning human-readable warnings (port of the geo design). Pure.
func Apply(cfg *yaml.Node, present Present, mirror string) []string {
	mihomo.SetNode(cfg, "geodata-mode", mihomo.BoolNode(true))
	mihomo.SetNode(cfg, "geo-auto-update", mihomo.BoolNode(false))
	geox := mihomo.MappingNode(nil)
	mihomo.SetNode(geox, "geoip", mihomo.StrNode(fileURL(mirror, "geoip.dat")))
	mihomo.SetNode(geox, "geosite", mihomo.StrNode(fileURL(mirror, "geosite.dat")))
	mihomo.SetNode(geox, "mmdb", mihomo.StrNode(fileURL(mirror, "geoip.metadb")))
	mihomo.SetNode(geox, "asn", mihomo.StrNode(fileURL(mirror, "asn.dat")))
	mihomo.SetNode(cfg, "geox-url", geox)

	var warnings []string
	if !present.GeoSite {
		r := stripRules(cfg, "GEOSITE")
		d := stripDNSRefs(mihomo.MapGet(cfg, "dns"), "geosite:")
		if r > 0 || d > 0 {
			warnings = append(warnings, "geosite.dat 缺失，已移除 GEOSITE 规则与相关 DNS 项（降级直连），可在内核目录补齐后重启恢复")
		}
	}
	if !present.GeoIP {
		r := stripRules(cfg, "GEOIP")
		d := stripDNSRefs(mihomo.MapGet(cfg, "dns"), "geoip:")
		if r > 0 || d > 0 {
			warnings = append(warnings, "geoip.dat 缺失，已移除 GEOIP 规则与相关 DNS 项（降级直连），可在内核目录补齐后重启恢复")
		}
	}
	// mihomo's DNS fallback-filter defaults to geoip:true, which loads (and, when
	// missing, downloads) GeoIP.dat at init even with zero GEOIP rules.
	// geo-auto-update:false does NOT suppress that initial load, so neutralize the
	// fallback-filter's geo dependencies whenever a file is absent.
	neutralizeFallbackFilter(cfg, present)
	if rules := mihomo.MapGet(cfg, "rules"); rules == nil || rules.Kind != yaml.SequenceNode || len(rules.Content) == 0 {
		mihomo.SetNode(cfg, "rules", mihomo.StrSeq([]string{"MATCH,DIRECT"}))
	}
	return warnings
}

// neutralizeFallbackFilter disables the DNS fallback-filter's geoip matcher and
// clears its geosite list when the corresponding .dat file is missing, so mihomo
// has no remaining reason to load or download geo data at startup.
func neutralizeFallbackFilter(cfg *yaml.Node, present Present) {
	if present.GeoIP && present.GeoSite {
		return
	}
	dns := mihomo.MapGet(cfg, "dns")
	if dns == nil || dns.Kind != yaml.MappingNode {
		return
	}
	ff := mihomo.MapGet(dns, "fallback-filter")
	if ff == nil || ff.Kind != yaml.MappingNode {
		ff = mihomo.MappingNode(nil)
		mihomo.SetNode(dns, "fallback-filter", ff)
	}
	if !present.GeoIP {
		mihomo.SetNode(ff, "geoip", mihomo.BoolNode(false))
	}
	if !present.GeoSite {
		mihomo.SetNode(ff, "geosite", mihomo.StrSeq(nil))
	}
}

// stripRules removes rules whose leading token equals token, returning the count.
func stripRules(cfg *yaml.Node, token string) int {
	rules := mihomo.MapGet(cfg, "rules")
	if rules == nil || rules.Kind != yaml.SequenceNode {
		return 0
	}
	kept := make([]*yaml.Node, 0, len(rules.Content))
	removed := 0
	for _, r := range rules.Content {
		if r.Kind == yaml.ScalarNode && ruleToken(r.Value) == token {
			removed++
			continue
		}
		kept = append(kept, r)
	}
	rules.Content = kept
	return removed
}

// stripDNSRefs recursively removes mapping keys / sequence items referencing
// prefix (e.g. "geosite:"), returning the number of removals.
func stripDNSRefs(node *yaml.Node, prefix string) int {
	if node == nil {
		return 0
	}
	prefix = strings.ToLower(prefix)
	removed := 0
	switch node.Kind {
	case yaml.MappingNode:
		kept := make([]*yaml.Node, 0, len(node.Content))
		for i := 0; i+1 < len(node.Content); i += 2 {
			k, v := node.Content[i], node.Content[i+1]
			if k.Kind == yaml.ScalarNode && strings.Contains(strings.ToLower(k.Value), prefix) {
				removed++
				continue
			}
			removed += stripDNSRefs(v, prefix)
			kept = append(kept, k, v)
		}
		node.Content = kept
	case yaml.SequenceNode:
		kept := make([]*yaml.Node, 0, len(node.Content))
		for _, c := range node.Content {
			if c.Kind == yaml.ScalarNode && strings.Contains(strings.ToLower(c.Value), prefix) {
				removed++
				continue
			}
			removed += stripDNSRefs(c, prefix)
			kept = append(kept, c)
		}
		node.Content = kept
	}
	return removed
}
