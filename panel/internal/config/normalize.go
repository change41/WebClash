package config

import (
	"math"
	"strings"

	"webclash/internal/util"
)

// get reads a map key, returning the undefined sentinel when absent so that
// JS Number(undefined)=NaN is distinguished from Number(null)=0.
func get(m map[string]any, key string) any {
	if v, ok := m[key]; ok {
		return v
	}
	return util.UndefinedValue
}

// obj coerces a value to a plain object map (empty when not an object).
func obj(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func str(v any, fallback string) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return fallback
}

// strOr is str(v, fallback) || fallback (empty trimmed string falls back).
func strOr(v any, fallback string) string {
	s := str(v, fallback)
	if s == "" {
		return fallback
	}
	return s
}

func boolOr(v any, fallback bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return fallback
}

func port(v any, fallback int, allowZero bool) int {
	n := util.JsNumber(v)
	if !util.JsIsInteger(n) || n < 0 || n > 65535 {
		return fallback
	}
	if n == 0 && !allowZero {
		return fallback
	}
	return int(n)
}

func num(v any, fallback, min, max int) int {
	n := util.JsNumber(v)
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return fallback
	}
	r := util.JsRound(n)
	if r < float64(min) {
		r = float64(min)
	}
	if r > float64(max) {
		r = float64(max)
	}
	return int(r)
}

func pick(v any, options []string, fallback string) string {
	if s, ok := v.(string); ok {
		for _, o := range options {
			if o == s {
				return s
			}
		}
	}
	return fallback
}

func strList(v any, fallback []string) []string {
	arr, ok := v.([]any)
	if !ok {
		return fallback
	}
	out := []string{}
	for _, item := range arr {
		if s, ok := item.(string); ok {
			if t := strings.TrimSpace(s); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// authUsers normalizes proxy authentication credentials, dropping malformed
// entries and blank usernames. Passwords are preserved verbatim.
func authUsers(v any) []InboundUser {
	arr, ok := v.([]any)
	if !ok {
		return []InboundUser{}
	}
	out := make([]InboundUser, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		username, _ := m["username"].(string)
		username = strings.TrimSpace(username)
		if username == "" {
			continue
		}
		password, _ := m["password"].(string)
		out = append(out, InboundUser{Username: username, Password: password})
	}
	return out
}
