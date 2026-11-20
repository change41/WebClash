package api

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func configRules(t *testing.T, s *Server) []string {
	t.Helper()
	raw, err := os.ReadFile(s.core.ConfigPath)
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

func TestSettingsCustomRulesValidation(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)

	r := do(s, "PUT", "/api/settings", `{"customRules":["MATCH,DIRECT"]}`, cookies)
	if r.code != 400 || !strings.Contains(jsonField(t, r.body, "error").(string), "MATCH") {
		t.Fatalf("invalid rules: got %d %s", r.code, r.body)
	}
	if got := do(s, "GET", "/api/settings", "", cookies); !strings.Contains(got.body, `"customRules":[]`) {
		t.Fatalf("invalid rules should not persist: %s", got.body)
	}

	r = do(s, "PUT", "/api/settings", `{"customRules":[" DOMAIN,custom.test,DIRECT ","# comment"]}`, cookies)
	if r.code != 200 {
		t.Fatalf("valid rules: got %d %s", r.code, r.body)
	}
	settings, _ := jsonField(t, r.body, "settings").(map[string]any)
	rules, _ := settings["customRules"].([]any)
	if len(rules) != 1 || rules[0] != "DOMAIN,custom.test,DIRECT" {
		t.Fatalf("settings.customRules = %v", rules)
	}
	want := []string{"DOMAIN,custom.test,DIRECT", "MATCH,DIRECT"}
	if got := configRules(t, s); !slices.Equal(got, want) {
		t.Fatalf("runtime rules = %v, want %v", got, want)
	}
}

func TestProfileCustomRules(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)

	r := do(s, "POST", "/api/profiles", `{
		"name": "local",
		"content": "proxies: []\nrules:\n  - MATCH,DIRECT\n",
		"settings": {"customRules": ["DOMAIN,profile.test,DIRECT"]}
	}`, cookies)
	if r.code != 200 {
		t.Fatalf("add profile: got %d %s", r.code, r.body)
	}
	id, _ := jsonField(t, r.body, "id").(string)
	if id == "" {
		t.Fatalf("profile id missing: %s", r.body)
	}
	settings, _ := jsonField(t, r.body, "settings").(map[string]any)
	rules, _ := settings["customRules"].([]any)
	if len(rules) != 1 || rules[0] != "DOMAIN,profile.test,DIRECT" {
		t.Fatalf("added profile rules = %v", rules)
	}
	want := []string{"DOMAIN,profile.test,DIRECT", "MATCH,DIRECT"}
	if got := configRules(t, s); !slices.Equal(got, want) {
		t.Fatalf("runtime rules = %v, want %v", got, want)
	}

	r = do(s, "PUT", "/api/profiles/"+id+"/settings", `{"customRules":["MATCH,DIRECT"]}`, cookies)
	if r.code != 400 {
		t.Fatalf("invalid profile rules: got %d %s", r.code, r.body)
	}
	if got := configRules(t, s); !slices.Equal(got, want) {
		t.Fatalf("invalid rules changed config: %v", got)
	}

	r = do(s, "PUT", "/api/profiles/"+id+"/settings", `{"customRules":["DOMAIN,new.test,DIRECT"]}`, cookies)
	if r.code != 200 {
		t.Fatalf("update profile rules: got %d %s", r.code, r.body)
	}
	want = []string{"DOMAIN,new.test,DIRECT", "MATCH,DIRECT"}
	if got := configRules(t, s); !slices.Equal(got, want) {
		t.Fatalf("updated runtime rules = %v, want %v", got, want)
	}
}

func TestAddProfileRejectsInvalidCustomRules(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	r := do(s, "POST", "/api/profiles", `{
		"name": "bad",
		"content": "proxies: []\nrules:\n  - MATCH,DIRECT\n",
		"settings": {"customRules": ["MATCH,DIRECT"]}
	}`, cookies)
	if r.code != 400 {
		t.Fatalf("got %d %s", r.code, r.body)
	}
	list := do(s, "GET", "/api/profiles", "", cookies)
	if !strings.Contains(list.body, `"profiles":[]`) {
		t.Fatalf("invalid profile was persisted: %s", list.body)
	}
}
