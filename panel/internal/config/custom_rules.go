package config

import (
	"fmt"
	"strings"
	"unicode"

	"webclash/internal/util"
)

// ParseCustomRulesStrict validates a JSON-decoded custom rule list. Lines are
// trimmed, blank/comment lines are dropped, comma-separated tokens are
// normalized, and duplicate rules are removed while keeping the first
// occurrence. Errors are user-facing 400s.
func ParseCustomRulesStrict(raw any) ([]string, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, util.NewHttpError(400, "自定义规则必须是字符串数组")
	}
	out := make([]string, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, util.NewHttpError(400, fmt.Sprintf("第 %d 条自定义规则必须是字符串", i+1))
		}
		line := strings.TrimSpace(s)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tokens := strings.Split(line, ",")
		for j := range tokens {
			tokens[j] = strings.TrimSpace(tokens[j])
		}
		typ := tokens[0]
		if strings.EqualFold(typ, "MATCH") {
			return nil, util.NewHttpError(400, fmt.Sprintf("第 %d 条自定义规则不能是 MATCH：MATCH 会屏蔽订阅中的后续规则", i+1))
		}
		if len(tokens) < 3 {
			return nil, util.NewHttpError(400, fmt.Sprintf("第 %d 条自定义规则格式不正确，应为 类型,内容,策略", i+1))
		}
		if typ == "" || !isUpperRuleType(typ) {
			return nil, util.NewHttpError(400, fmt.Sprintf("第 %d 条自定义规则的规则类型必须使用大写字母", i+1))
		}
		if tokens[1] == "" {
			return nil, util.NewHttpError(400, fmt.Sprintf("第 %d 条自定义规则缺少内容", i+1))
		}
		if tokens[len(tokens)-1] == "" {
			return nil, util.NewHttpError(400, fmt.Sprintf("第 %d 条自定义规则缺少策略", i+1))
		}
		rule := strings.Join(tokens, ",")
		if _, duplicate := seen[rule]; duplicate {
			continue
		}
		seen[rule] = struct{}{}
		out = append(out, rule)
	}
	return out, nil
}

func isUpperRuleType(typ string) bool {
	for _, r := range typ {
		if unicode.IsLower(r) {
			return false
		}
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
