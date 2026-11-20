package config

import (
	"errors"
	"strings"
	"testing"

	"webclash/internal/util"
)

func TestParseCustomRulesStrictValid(t *testing.T) {
	got, err := ParseCustomRulesStrict([]any{
		" DOMAIN, example.com , DIRECT ",
		"",
		"# comment",
		"IP-CIDR,10.0.0.0/8,PROXY,no-resolve",
		"AND,((DOMAIN,a.com),(NETWORK,tcp)),Proxy",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"DOMAIN,example.com,DIRECT",
		"IP-CIDR,10.0.0.0/8,PROXY,no-resolve",
		"AND,((DOMAIN,a.com),(NETWORK,tcp)),Proxy",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseCustomRulesStrictDedupes(t *testing.T) {
	got, err := ParseCustomRulesStrict([]any{
		" DOMAIN, example.com , DIRECT ",
		"DOMAIN,example.com,DIRECT",
		"DOMAIN,other.com,DIRECT",
		"# comment",
		"",
		" DOMAIN , example.com , DIRECT ",
		"DOMAIN,other.com,DIRECT",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"DOMAIN,example.com,DIRECT",
		"DOMAIN,other.com,DIRECT",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseCustomRulesStrictEmpty(t *testing.T) {
	got, err := ParseCustomRulesStrict([]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("empty array should return empty non-nil slice, got %#v", got)
	}
}

func TestParseCustomRulesStrictRejects(t *testing.T) {
	cases := []struct {
		name string
		raw  any
		msg  string
	}{
		{"nil", nil, "必须是字符串数组"},
		{"non-array", "DOMAIN,a,DIRECT", "必须是字符串数组"},
		{"non-string item", []any{5}, "必须是字符串"},
		{"upper MATCH", []any{"MATCH,DIRECT"}, "不能是 MATCH"},
		{"lower MATCH", []any{"match,DIRECT"}, "不能是 MATCH"},
		{"too few tokens", []any{"DOMAIN,a"}, "格式不正确"},
		{"empty type", []any{",a,DIRECT"}, "规则类型"},
		{"lowercase type", []any{"domain,a,DIRECT"}, "大写"},
		{"empty payload", []any{"DOMAIN, ,DIRECT"}, "缺少内容"},
		{"empty policy", []any{"DOMAIN,a,DIRECT,"}, "缺少策略"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseCustomRulesStrict(c.raw)
			if err == nil {
				t.Fatal("expected error")
			}
			var he *util.HttpError
			if !errors.As(err, &he) || he.StatusCode != 400 {
				t.Fatalf("expected 400 HttpError, got %T %v", err, err)
			}
			if !strings.Contains(err.Error(), c.msg) {
				t.Fatalf("error %q does not contain %q", err.Error(), c.msg)
			}
		})
	}
}
