package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"webclash/internal/util"
)

// ApplyMode persists the proxy mode, rewrites config and patches the running
// kernel (port of applyMode).
func (c *Core) ApplyMode(mode string) error {
	c.mu.Lock()
	c.settings.General.Mode = mode
	c.mu.Unlock()
	if err := c.persistSettings(); err != nil {
		return err
	}
	if err := c.writeRuntimeConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "[webclash] 写入配置失败：%s\n", util.ErrorMessage(err))
	}
	if c.Kernel.IsRunning() {
		_, err := c.Kernel.Client().Request("PATCH", "/configs", map[string]any{"mode": mode}, 15000)
		return err
	}
	return nil
}

// RuleProviderContent resolves a rule-provider's rules: inline payload → kernel
// local cache → subscription URL (port of ruleProviderContent).
func (c *Core) RuleProviderContent(name string) (map[string]any, error) {
	if !c.Kernel.IsRunning() {
		return nil, util.NewHttpError(409, "内核未运行，请先启动内核")
	}
	res, err := c.Kernel.Client().Request("GET", "/providers/rules", nil, 15000)
	if err != nil {
		return nil, err
	}
	providers := map[string]any{}
	if m, ok := res.(map[string]any); ok {
		if p, ok := m["providers"].(map[string]any); ok {
			providers = p
		}
	}
	metaVal, ok := providers[name]
	if !ok {
		return nil, util.NewHttpError(404, "规则集不存在")
	}
	meta, _ := metaVal.(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}

	def := c.ruleProviderDefinition(name)
	format := normalizeRuleFormat(firstNonNil(defGet(def, "format"), meta["format"]))
	if format == "mrs" {
		return nil, util.NewHttpError(400, "mrs 是二进制规则集，暂不支持预览内容")
	}

	out := cloneMap(meta)
	out["format"] = format

	if payload, ok := defGet(def, "payload").([]any); ok {
		rules := make([]string, len(payload))
		for i, item := range payload {
			rules[i] = jsString(item)
		}
		out["source"] = "inline"
		out["rules"] = rules
		return out, nil
	}

	cached, err := c.readRuleSetCache(def)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		rules, perr := parseRuleSet(*cached, format)
		if perr != nil {
			return nil, perr
		}
		out["source"] = "cache"
		out["rules"] = rules
		return out, nil
	}

	url := ""
	if u, ok := defGet(def, "url").(string); ok {
		url = strings.TrimSpace(u)
	}
	if url == "" {
		return nil, util.NewHttpError(404, "未找到规则集内容：缺少内联规则、本地缓存与订阅地址")
	}
	text, ferr := fetchRuleSet(url)
	if ferr != nil {
		return nil, ferr
	}
	rules, perr := parseRuleSet(text, format)
	if perr != nil {
		return nil, perr
	}
	out["source"] = "remote"
	out["rules"] = rules
	return out, nil
}

// ruleProviderDefinition reads the provider definition from the active profile
// (then the runtime config); the kernel API only returns metadata.
func (c *Core) ruleProviderDefinition(name string) map[string]any {
	active := c.ActiveProfile()
	var files []string
	if active != nil {
		files = append(files, c.ProfileFile(active.ID))
	}
	files = append(files, c.ConfigPath)
	for _, file := range files {
		raw, err := util.ReadFileString(file)
		if err != nil {
			continue
		}
		var cfg map[string]any
		if yaml.Unmarshal([]byte(raw), &cfg) != nil {
			continue
		}
		providers, ok := cfg["rule-providers"].(map[string]any)
		if !ok {
			continue
		}
		def, ok := providers[name]
		if !ok {
			continue
		}
		if d, ok := def.(map[string]any); ok {
			return d
		}
	}
	return nil
}

func (c *Core) readRuleSetCache(def map[string]any) (*string, error) {
	rel := ""
	if p, ok := defGet(def, "path").(string); ok {
		rel = strings.TrimSpace(p)
	}
	if rel == "" {
		return nil, nil
	}
	// path comes from subscription config; confine reads to the kernel dir.
	resolved := rel
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(c.KernelDir, rel)
	}
	safe := util.SanitizePath(resolved, c.KernelDir)
	if safe == "" {
		return nil, util.NewHttpError(403, "规则集缓存路径超出内核目录，已拒绝读取")
	}
	content, err := util.ReadFileString(safe)
	if err != nil {
		return nil, nil
	}
	return &content, nil
}

func normalizeRuleFormat(value any) string {
	s := "yaml"
	if value != nil {
		s = jsString(value)
	}
	raw := strings.ToLower(s)
	if strings.HasPrefix(raw, "text") {
		return "text"
	}
	if strings.HasPrefix(raw, "mrs") {
		return "mrs"
	}
	return "yaml"
}

func defGet(def map[string]any, key string) any {
	if def == nil {
		return nil
	}
	return def[key]
}

func firstNonNil(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+3)
	for k, v := range m {
		out[k] = v
	}
	return out
}
