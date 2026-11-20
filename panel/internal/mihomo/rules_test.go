package mihomo

import (
	"errors"
	"strings"
	"testing"

	"webclash/internal/config"
)

func TestValidateRulePoliciesAllowed(t *testing.T) {
	cfg := BuildConfig(parseBase(t, subscriptionYAML), config.DefaultSettings())
	rules := []string{
		"DOMAIN,www.example.com,DIRECT",
		"IP-CIDR,1.1.1.1/32,REJECT,no-resolve",
		"DST-PORT,443,PASS-RULE",
		"DST-PORT,443,PROXY",
		"DOMAIN,www.example.com,节点A",
		"AND,((DOMAIN-SUFFIX,example.com),(NETWORK,TCP)),PROXY",
		"SUB-RULE,(NETWORK,TCP),sub-rule",
	}
	if err := ValidateRulePolicies(cfg, rules); err != nil {
		t.Fatalf("valid policies rejected: %v", err)
	}
}

func TestValidateRulePoliciesMissingPolicy(t *testing.T) {
	cfg := BuildConfig(parseBase(t, subscriptionYAML), config.DefaultSettings())
	err := ValidateRulePolicies(cfg, []string{
		"DOMAIN,ok.test,DIRECT",
		"DST-PORT,443,PROXY2",
	})
	if err == nil {
		t.Fatal("missing policy accepted")
	}
	var pe *RulePolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("error type = %T, want *RulePolicyError", err)
	}
	if pe.Index != 2 || pe.Policy != "PROXY2" || pe.Rule != "DST-PORT,443,PROXY2" {
		t.Fatalf("unexpected error fields: %+v", pe)
	}
	if !strings.Contains(pe.Error(), "「DST-PORT,443,PROXY2」的策略「PROXY2」不存在") {
		t.Fatalf("error message = %q", pe.Error())
	}
	if !strings.Contains(pe.Error(), "PROXY") {
		t.Fatalf("available proxy groups missing from message: %q", pe.Error())
	}
}

func TestValidateRulePoliciesRegexPayloadWithComma(t *testing.T) {
	cfg := BuildConfig(parseBase(t, subscriptionYAML), config.DefaultSettings())
	rules := []string{
		"DOMAIN-REGEX,^a{1,3}$,PROXY",
		"PROCESS-NAME-REGEX,^foo(bar,baz)?$,PROXY",
	}
	if err := ValidateRulePolicies(cfg, rules); err != nil {
		t.Fatalf("regex payload with comma rejected: %v", err)
	}
}

func TestValidateRulePoliciesAliasedGroups(t *testing.T) {
	cfg := BuildConfig(parseBase(t, `
base: &groups
  - name: PROXY
    type: select
    proxies: [DIRECT]
proxy-groups: *groups
rules: [MATCH,DIRECT]
`), config.DefaultSettings())
	if err := ValidateRulePolicies(cfg, []string{"DST-PORT,443,PROXY"}); err != nil {
		t.Fatalf("aliased proxy-groups rejected: %v", err)
	}
}

func TestValidateRulePoliciesMatch(t *testing.T) {
	cfg := BuildConfig(parseBase(t, "proxies: []\nrules: [MATCH,DIRECT]\n"), config.DefaultSettings())
	if err := ValidateRulePolicies(cfg, []string{"MATCH,DIRECT"}); err != nil {
		t.Fatalf("MATCH,DIRECT rejected: %v", err)
	}
	if err := ValidateRulePolicies(cfg, []string{"MATCH,PROXY"}); err == nil {
		t.Fatal("MATCH,PROXY accepted without a PROXY target")
	}
}
