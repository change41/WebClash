package core

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"webclash/internal/config"
	"webclash/internal/util"
)

const ruleProfileYAML = `proxies: []
rules:
  - DOMAIN,subscription.test,DIRECT
  - MATCH,PROXY
`

const policyProfileYAML = `proxies:
  - name: "节点A"
    type: ss
    server: 1.2.3.4
    port: 8388
    cipher: aes-256-gcm
    password: "123456"
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - "节点A"
rules:
  - DOMAIN,subscription.test,DIRECT
  - MATCH,PROXY
`

func newCustomRulesCore(t *testing.T) *Core {
	t.Helper()
	c := New(t.TempDir())
	if err := os.MkdirAll(c.ProfilesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.KernelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "abcd1234"
	if err := os.WriteFile(c.ProfileFile(id), []byte(ruleProfileYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.profilesState = config.ProfilesState{
		ActiveID: &id,
		Profiles: []config.ProfileMeta{{
			ID: id, Name: "sub", Type: "local", Settings: config.DefaultProfileSettings(),
		}},
	}
	c.mu.Unlock()
	return c
}

func newPolicyRulesCore(t *testing.T) *Core {
	t.Helper()
	c := newCustomRulesCore(t)
	if err := os.WriteFile(c.ProfileFile("abcd1234"), []byte(policyProfileYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return c
}

func runtimeRules(t *testing.T, c *Core) []string {
	t.Helper()
	raw, err := os.ReadFile(c.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var parsed struct {
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return parsed.Rules
}

func wantRules(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
}

func TestSaveSettingsValidatesCustomRulesBeforeMutation(t *testing.T) {
	c := newCustomRulesCore(t)
	before := c.Settings()
	_, err := c.SaveSettings(map[string]any{
		"customRules": []any{"DOMAIN,ok.test,DIRECT", "MATCH,DIRECT"},
	})
	he, ok := err.(*util.HttpError)
	if !ok || he.StatusCode != 400 {
		t.Fatalf("expected 400, got %v", err)
	}
	if !slices.Equal(c.Settings().CustomRules, before.CustomRules) {
		t.Fatalf("settings mutated on invalid rules: %v", c.Settings().CustomRules)
	}
}

func TestSaveSettingsGlobalCustomRules(t *testing.T) {
	c := newCustomRulesCore(t)
	result, err := c.SaveSettings(map[string]any{
		"customRules": []any{" DOMAIN,global.test,DIRECT ", "# comment"},
	})
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if result != ResultSaved {
		t.Fatalf("result = %v, want saved", result)
	}
	if !slices.Equal(c.Settings().CustomRules, []string{"DOMAIN,global.test,DIRECT"}) {
		t.Fatalf("persisted rules = %v", c.Settings().CustomRules)
	}
	wantRules(t, runtimeRules(t, c), []string{
		"DOMAIN,global.test,DIRECT",
		"DOMAIN,subscription.test,DIRECT",
		"MATCH,PROXY",
	})
}

func TestUpdateProfileSettingsCustomRulesOrder(t *testing.T) {
	c := newCustomRulesCore(t)
	if _, err := c.SaveSettings(map[string]any{
		"customRules": []any{"DOMAIN,global.test,DIRECT"},
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	meta, err := c.UpdateProfileSettings("abcd1234", map[string]any{
		"customRules": []any{"DOMAIN,profile.test,DIRECT"},
	})
	if err != nil {
		t.Fatalf("UpdateProfileSettings: %v", err)
	}
	if !slices.Equal(meta.Settings.CustomRules, []string{"DOMAIN,profile.test,DIRECT"}) {
		t.Fatalf("profile rules = %v", meta.Settings.CustomRules)
	}
	wantRules(t, runtimeRules(t, c), []string{
		"DOMAIN,profile.test,DIRECT",
		"DOMAIN,global.test,DIRECT",
		"DOMAIN,subscription.test,DIRECT",
		"MATCH,PROXY",
	})
}

func TestUpdateProfileSettingsInvalidRulesKeepsOtherFields(t *testing.T) {
	c := newCustomRulesCore(t)
	_, err := c.UpdateProfileSettings("abcd1234", map[string]any{
		"testUrl":     "https://changed.test",
		"customRules": []any{"MATCH,DIRECT"},
	})
	he, ok := err.(*util.HttpError)
	if !ok || he.StatusCode != 400 {
		t.Fatalf("expected 400, got %v", err)
	}
	p := c.ActiveProfile()
	if p == nil || p.Settings.TestUrl != config.DefaultProfileSettings().TestUrl {
		t.Fatalf("profile settings mutated on invalid rules: %+v", p)
	}
}

func TestSaveSettingsRejectsMissingRulePolicyBeforeMutation(t *testing.T) {
	c := newPolicyRulesCore(t)
	before := c.Settings()
	_, err := c.SaveSettings(map[string]any{
		"customRules": []any{"DST-PORT,443,MISSING"},
	})
	he, ok := err.(*util.HttpError)
	if !ok || he.StatusCode != 400 {
		t.Fatalf("expected 400, got %v", err)
	}
	for _, want := range []string{"第 1 条规则", "DST-PORT,443,MISSING", "MISSING", "DIRECT", "REJECT"} {
		if !strings.Contains(he.Message, want) {
			t.Fatalf("error %q missing %q", he.Message, want)
		}
	}
	if !slices.Equal(c.Settings().CustomRules, before.CustomRules) {
		t.Fatalf("settings mutated on invalid policy: %v", c.Settings().CustomRules)
	}
	if _, statErr := os.Stat(c.ConfigPath); statErr == nil {
		t.Fatal("runtime config was written for invalid policy")
	}
}

func TestSaveSettingsAcceptsExistingProxyGroupPolicy(t *testing.T) {
	c := newPolicyRulesCore(t)
	if _, err := c.SaveSettings(map[string]any{
		"customRules": []any{"DST-PORT,443,PROXY"},
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if !slices.Equal(c.Settings().CustomRules, []string{"DST-PORT,443,PROXY"}) {
		t.Fatalf("persisted rules = %v", c.Settings().CustomRules)
	}
	wantRules(t, runtimeRules(t, c), []string{
		"DST-PORT,443,PROXY",
		"DOMAIN,subscription.test,DIRECT",
		"MATCH,PROXY",
	})
}

func TestUpdateProfileSettingsRejectsMissingRulePolicy(t *testing.T) {
	c := newPolicyRulesCore(t)
	before := c.ActiveProfile()
	_, err := c.UpdateProfileSettings("abcd1234", map[string]any{
		"testUrl":     "https://changed.test",
		"customRules": []any{"DST-PORT,443,MISSING"},
	})
	he, ok := err.(*util.HttpError)
	if !ok || he.StatusCode != 400 {
		t.Fatalf("expected 400, got %v", err)
	}
	if !strings.Contains(he.Message, "MISSING") || !strings.Contains(he.Message, "订阅「sub」") {
		t.Fatalf("error message = %q", he.Message)
	}
	after := c.ActiveProfile()
	if after == nil || !slices.Equal(after.Settings.CustomRules, before.Settings.CustomRules) {
		t.Fatalf("profile rules mutated on invalid policy: %+v", after)
	}
	if after.Settings.TestUrl != before.Settings.TestUrl {
		t.Fatalf("profile settings mutated on invalid policy: %+v", after)
	}
}

func TestBuildCurrentConfigRejectsPersistedMissingPolicy(t *testing.T) {
	c := newPolicyRulesCore(t)
	c.mu.Lock()
	c.profilesState.Profiles[0].Settings.CustomRules = []string{"DST-PORT,443,MISSING"}
	c.mu.Unlock()

	err := c.writeRuntimeConfig()
	he, ok := err.(*util.HttpError)
	if !ok || he.StatusCode != 400 || !strings.Contains(he.Message, "MISSING") {
		t.Fatalf("writeRuntimeConfig error = %v", err)
	}
	if _, statErr := os.Stat(c.ConfigPath); statErr == nil {
		t.Fatal("runtime config was written for invalid policy")
	}
}

func TestActivateProfileRejectsInvalidGlobalRules(t *testing.T) {
	c := newPolicyRulesCore(t)
	id := "deadbeef"
	if err := os.WriteFile(c.ProfileFile(id), []byte("proxies: []\nrules: [MATCH,DIRECT]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.settings.CustomRules = []string{"DST-PORT,443,PROXY"}
	c.profilesState.Profiles = append(c.profilesState.Profiles, config.ProfileMeta{
		ID: id, Name: "other", Type: "local", Settings: config.DefaultProfileSettings(),
	})
	activeBefore := *c.profilesState.ActiveID
	c.mu.Unlock()

	_, err := c.ActivateProfile(id)
	he, ok := err.(*util.HttpError)
	if !ok || he.StatusCode != 400 || !strings.Contains(he.Message, "全局扩展规则") {
		t.Fatalf("expected 400 global rule error, got %v", err)
	}
	c.mu.RLock()
	activeAfter := *c.profilesState.ActiveID
	c.mu.RUnlock()
	if activeAfter != activeBefore {
		t.Fatalf("active profile changed to %q on invalid rules", activeAfter)
	}
}

func TestWriteBuiltinConfigIgnoresInvalidGlobalRules(t *testing.T) {
	c := New(t.TempDir())
	if err := os.MkdirAll(c.KernelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.settings.CustomRules = []string{"DST-PORT,443,MISSING"}
	c.mu.Unlock()

	if err := c.writeBuiltinConfig(); err != nil {
		t.Fatalf("writeBuiltinConfig: %v", err)
	}
	raw, err := os.ReadFile(c.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "DST-PORT,443,MISSING") {
		t.Fatalf("invalid custom rule written to fallback config:\n%s", raw)
	}
	if !strings.Contains(string(raw), "MATCH,DIRECT") {
		t.Fatalf("fallback config missing direct rule:\n%s", raw)
	}
}
