package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"webclash/internal/util"
)

// subscriptionUA ensures subscription services return Clash/Mihomo format.
const subscriptionUA = "clash.meta"

const maxRuleSetChars = 8 * 1024 * 1024

func fetchHeaders() http.Header {
	h := http.Header{}
	h.Set("User-Agent", subscriptionUA)
	h.Set("Accept", "text/yaml, application/yaml, text/plain, */*")
	return h
}

// parseClashConfig parses and validates a Clash/Mihomo subscription document,
// returning the root mapping node (port of profiles.ts parseClashConfig). The
// node form preserves key order / scalar types for the config builder.
func parseClashConfig(text string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, fmt.Errorf("YAML 解析失败：%s", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("不是有效的 Clash/Mihomo 配置（顶层必须是对象）")
	}
	root := doc.Content[0]
	hasKey := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "proxies", "proxy-providers", "rules":
			hasKey = true
		}
	}
	if !hasKey {
		return nil, errors.New("配置中未找到 proxies / proxy-providers / rules，可能不是 Clash 订阅")
	}
	return root, nil
}

// validateSubscriptionURL mirrors new URL(url) + http/https protocol checks.
func validateSubscriptionURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("订阅链接格式不正确")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("订阅链接仅支持 http/https")
	}
	return u, nil
}

// fetchSubscription downloads and validates a subscription (port of
// fetchSubscription). Returns the raw text and parsed subscription-userinfo.
func fetchSubscription(rawURL string) (string, *util.SubscriptionUserInfo, error) {
	if _, err := validateSubscriptionURL(rawURL); err != nil {
		return "", nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("订阅请求失败：%s", err)
	}
	req.Header = fetchHeaders()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("订阅请求失败：%s", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", nil, fmt.Errorf("订阅请求失败：HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", nil, fmt.Errorf("订阅请求失败：%s", err)
	}
	text := string(body)
	if strings.TrimSpace(text) == "" {
		return "", nil, errors.New("订阅内容为空")
	}
	if _, err := parseClashConfig(text); err != nil {
		return "", nil, err
	}
	return text, util.ParseUserInfo(res.Header.Get("subscription-userinfo")), nil
}

// nameFromSubscriptionUrl derives a profile name from the URL fragment or host.
func nameFromSubscriptionUrl(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	if hash := strings.TrimSpace(u.Fragment); hash != "" {
		return hash
	}
	return u.Hostname()
}

// fetchRuleSet downloads rule-set text; the address comes from subscription
// config and only http/https is allowed (port of fetchRuleSet).
func fetchRuleSet(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", util.NewHttpError(400, "规则集地址格式不正确")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", util.NewHttpError(400, "规则集地址仅支持 http/https")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", util.NewHttpError(502, fmt.Sprintf("规则集下载失败：%s", err))
	}
	req.Header = fetchHeaders()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", util.NewHttpError(502, fmt.Sprintf("规则集下载失败：%s", err))
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", util.NewHttpError(502, fmt.Sprintf("规则集下载失败：HTTP %d", res.StatusCode))
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", util.NewHttpError(502, fmt.Sprintf("规则集下载失败：%s", err))
	}
	return string(body), nil
}

// parseRuleSet turns rule-set text into rule lines: yaml takes the payload list,
// text takes non-empty non-comment lines (port of parseRuleSet).
func parseRuleSet(text, format string) ([]string, error) {
	if util.JsStringLength(text) > maxRuleSetChars {
		return nil, util.NewHttpError(413, "规则集内容过大，无法预览")
	}
	if format == "text" {
		out := []string{}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
			if line != "" && !strings.HasPrefix(line, "#") {
				out = append(out, line)
			}
		}
		return out, nil
	}
	var data any
	if err := yaml.Unmarshal([]byte(text), &data); err != nil {
		return nil, util.NewHttpError(502, fmt.Sprintf("规则集解析失败：%s", err))
	}
	payload := data
	if m, ok := data.(map[string]any); ok {
		payload = m["payload"]
	}
	list, ok := payload.([]any)
	if !ok {
		return nil, util.NewHttpError(502, "规则集内容缺少 payload 列表")
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, jsString(item))
	}
	return out, nil
}

// jsString approximates JS String(item) for rule payload scalars.
func jsString(item any) string {
	if item == nil {
		return "null"
	}
	return fmt.Sprint(item)
}

// timestampOr replicates Number(value) || fallback (0/NaN fall back).
func timestampOr(v any, fallback int64) int64 {
	n := util.JsNumber(v)
	if math.IsNaN(n) || n == 0 {
		return fallback
	}
	return int64(n)
}

// toUserInfo converts a persisted userInfo object into the typed form, keeping
// only finite numeric fields (mirrors the SubscriptionUserInfo shape).
func toUserInfo(v any) *util.SubscriptionUserInfo {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := &util.SubscriptionUserInfo{}
	found := false
	set := func(dst **float64, raw any) {
		n := util.JsNumber(raw)
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return
		}
		val := n
		*dst = &val
		found = true
	}
	if _, ok := m["upload"]; ok {
		set(&out.Upload, m["upload"])
	}
	if _, ok := m["download"]; ok {
		set(&out.Download, m["download"])
	}
	if _, ok := m["total"]; ok {
		set(&out.Total, m["total"])
	}
	if _, ok := m["expire"]; ok {
		set(&out.Expire, m["expire"])
	}
	if !found {
		return nil
	}
	return out
}
