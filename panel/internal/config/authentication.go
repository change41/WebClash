package config

import (
	"fmt"
	"strings"
)

// ValidateAuthentication rejects credentials that mihomo cannot parse or that
// would silently collapse into duplicate accounts.
func ValidateAuthentication(users []InboundUser) error {
	seen := map[string]bool{}
	for _, u := range users {
		if strings.Contains(u.Username, ":") {
			return fmt.Errorf("端口认证用户名「%s」不能包含冒号（mihomo 认证使用“用户名:密码”格式）", u.Username)
		}
		if u.Password == "" {
			return fmt.Errorf("端口认证用户「%s」的密码不能为空", u.Username)
		}
		if seen[u.Username] {
			return fmt.Errorf("端口认证用户名「%s」重复，请合并为一条", u.Username)
		}
		seen[u.Username] = true
	}
	return nil
}
