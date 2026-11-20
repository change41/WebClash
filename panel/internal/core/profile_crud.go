package core

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"webclash/internal/config"
	"webclash/internal/util"
)

// ProfileUpdateFailure is one failed subscription refresh (port of the shape).
type ProfileUpdateFailure struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

// AddProfileInput carries the optional fields of POST /api/profiles.
type AddProfileInput struct {
	Name     *string
	URL      *string
	Content  *string
	Settings any
}

// profileIndex returns the index of id, or -1. Caller must hold c.mu.
func (c *Core) profileIndex(id string) int {
	for i := range c.profilesState.Profiles {
		if c.profilesState.Profiles[i].ID == id {
			return i
		}
	}
	return -1
}

func (c *Core) saveProfiles() error {
	c.mu.RLock()
	state := config.ProfilesState{
		ActiveID: c.profilesState.ActiveID,
		Profiles: append([]config.ProfileMeta{}, c.profilesState.Profiles...),
	}
	c.mu.RUnlock()
	c.persistMu.Lock()
	defer c.persistMu.Unlock()
	return util.WriteJsonAtomic(c.ProfilesPath, state)
}

// applyRuntimeConfig writes the config and makes a running kernel take it.
func (c *Core) applyRuntimeConfig() (ApplyResult, error) {
	if err := c.writeRuntimeConfig(); err != nil {
		return "", err
	}

	state := c.Kernel.State()
	if state == "running" {
		if err := c.Kernel.ReloadConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "[webclash] 热重载失败，改为重启内核：%s\n", util.ErrorMessage(err))
			if err := c.Kernel.Restart(); err != nil {
				return "", err
			}
			return ResultRestarted, nil
		}
		return ResultReloaded, nil
	}
	if state == "starting" || state == "stopping" {
		if err := c.Kernel.Restart(); err != nil {
			return "", err
		}
		return ResultRestarted, nil
	}
	return ResultSaved, nil
}

// ListProfiles returns the profiles snapshot for GET /api/profiles.
func (c *Core) ListProfiles() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]any{
		"activeId":        c.profilesState.ActiveID,
		"profiles":        append([]config.ProfileMeta{}, c.profilesState.Profiles...),
		"lastConfigAt":    c.lastConfigAt,
		"lastConfigError": c.lastConfigError,
	}
}

// AddProfile creates a local or remote profile (port of addProfile).
func (c *Core) AddProfile(input AddProfileInput) (config.ProfileMeta, error) {
	var meta config.ProfileMeta
	urlStr := ""
	if input.URL != nil {
		urlStr = strings.TrimSpace(*input.URL)
	}
	content := ""
	if input.Content != nil {
		content = *input.Content
	}
	settings, err := profileSettingsFromInput(input.Settings)
	if err != nil {
		return meta, err
	}
	typ := "local"
	var userInfo *util.SubscriptionUserInfo

	if urlStr != "" {
		text, ui, err := fetchSubscription(urlStr)
		if err != nil {
			return meta, err
		}
		content, userInfo, typ = text, ui, "remote"
	}
	if strings.TrimSpace(content) == "" {
		return meta, util.NewHttpError(400, "请提供订阅链接或 YAML 内容")
	}
	if _, err := parseClashConfig(content); err != nil {
		return meta, err
	}

	id := util.RandomId()
	now := time.Now().UnixMilli()
	if err := os.WriteFile(c.ProfileFile(id), []byte(content), 0o644); err != nil {
		return meta, err
	}

	name := ""
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	if name == "" {
		name = nameFromSubscriptionUrl(urlStr)
	}

	c.mu.Lock()
	if name == "" {
		name = fmt.Sprintf("配置 %d", len(c.profilesState.Profiles)+1)
	}
	meta = config.ProfileMeta{
		ID:        id,
		Name:      name,
		Type:      typ,
		CreatedAt: now,
		UpdatedAt: now,
		UserInfo:  userInfo,
		Settings:  settings,
	}
	if urlStr != "" {
		u := urlStr
		meta.URL = &u
	}
	c.profilesState.Profiles = append(c.profilesState.Profiles, meta)
	shouldActivate := c.profilesState.ActiveID == nil
	if shouldActivate {
		aid := id
		c.profilesState.ActiveID = &aid
	}
	c.mu.Unlock()

	if err := c.saveProfiles(); err != nil {
		return meta, err
	}
	if shouldActivate {
		if _, err := c.applyRuntimeConfig(); err != nil {
			return meta, err
		}
	}
	return meta, nil
}

// refreshRemoteProfile re-downloads a remote subscription and updates its meta.
func (c *Core) refreshRemoteProfile(id, url string) error {
	text, ui, err := fetchSubscription(url)
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.ProfileFile(id), []byte(text), 0o644); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	c.mu.Lock()
	if idx := c.profileIndex(id); idx >= 0 {
		c.profilesState.Profiles[idx].UpdatedAt = now
		c.profilesState.Profiles[idx].UserInfo = ui
	}
	c.mu.Unlock()
	return nil
}

// UpdateProfile refreshes one remote profile (port of updateProfile).
func (c *Core) UpdateProfile(id string) (config.ProfileMeta, error) {
	c.mu.RLock()
	idx := c.profileIndex(id)
	var meta config.ProfileMeta
	if idx >= 0 {
		meta = c.profilesState.Profiles[idx]
	}
	c.mu.RUnlock()
	if idx < 0 {
		return meta, util.NewHttpError(404, "配置不存在")
	}
	if meta.Type != "remote" || meta.URL == nil || *meta.URL == "" {
		return meta, util.NewHttpError(400, "本地配置无法从订阅更新")
	}
	if err := c.refreshRemoteProfile(id, *meta.URL); err != nil {
		return meta, err
	}
	if err := c.saveProfiles(); err != nil {
		return meta, err
	}
	c.mu.RLock()
	active := c.profilesState.ActiveID != nil && *c.profilesState.ActiveID == id
	if i := c.profileIndex(id); i >= 0 {
		meta = c.profilesState.Profiles[i]
	}
	c.mu.RUnlock()
	if active {
		if _, err := c.applyRuntimeConfig(); err != nil {
			return meta, err
		}
	}
	return meta, nil
}

// UpdateAllProfiles refreshes every remote profile (port of updateAllProfiles).
func (c *Core) UpdateAllProfiles() (map[string]any, error) {
	if !c.profileUpdateMu.TryLock() {
		return nil, util.NewHttpError(409, "订阅更新正在进行中")
	}
	defer c.profileUpdateMu.Unlock()

	c.mu.RLock()
	type remote struct{ id, url string }
	var remotes []remote
	for _, p := range c.profilesState.Profiles {
		if p.Type == "remote" && p.URL != nil && *p.URL != "" {
			remotes = append(remotes, remote{p.ID, *p.URL})
		}
	}
	activeID := c.profilesState.ActiveID
	c.mu.RUnlock()

	updated := []string{}
	failed := []ProfileUpdateFailure{}
	activeTouched := false
	for _, r := range remotes {
		if err := c.refreshRemoteProfile(r.id, r.url); err != nil {
			c.mu.RLock()
			name := ""
			if idx := c.profileIndex(r.id); idx >= 0 {
				name = c.profilesState.Profiles[idx].Name
			}
			c.mu.RUnlock()
			failed = append(failed, ProfileUpdateFailure{ID: r.id, Name: name, Error: util.ErrorMessage(err)})
			continue
		}
		updated = append(updated, r.id)
		if activeID != nil && *activeID == r.id {
			activeTouched = true
		}
	}
	if err := c.saveProfiles(); err != nil {
		return nil, err
	}
	if activeTouched {
		if _, err := c.applyRuntimeConfig(); err != nil {
			return nil, err
		}
	}
	return map[string]any{"updated": updated, "failed": failed}, nil
}

// ActivateProfile sets the active profile and applies it.
func (c *Core) ActivateProfile(id string) (config.ProfileMeta, error) {
	c.mu.RLock()
	idx := c.profileIndex(id)
	var meta config.ProfileMeta
	if idx >= 0 {
		meta = c.profilesState.Profiles[idx]
	}
	c.mu.RUnlock()
	if idx < 0 {
		return meta, util.NewHttpError(404, "配置不存在")
	}
	if err := c.validateRulePolicyRefs(c.Settings(), &meta, meta.Settings.CustomRules); err != nil {
		return meta, err
	}

	c.mu.Lock()
	if c.profileIndex(id) < 0 {
		c.mu.Unlock()
		return meta, util.NewHttpError(404, "配置不存在")
	}
	aid := id
	c.profilesState.ActiveID = &aid
	meta = c.profilesState.Profiles[c.profileIndex(id)]
	c.mu.Unlock()

	if err := c.saveProfiles(); err != nil {
		return meta, err
	}
	if _, err := c.applyRuntimeConfig(); err != nil {
		return meta, err
	}
	return meta, nil
}

// DeleteProfile removes a profile and its file (port of deleteProfile).
func (c *Core) DeleteProfile(id string) error {
	c.mu.Lock()
	idx := c.profileIndex(id)
	if idx < 0 {
		c.mu.Unlock()
		return util.NewHttpError(404, "配置不存在")
	}
	wasActive := c.profilesState.ActiveID != nil && *c.profilesState.ActiveID == id
	c.profilesState.Profiles = append(c.profilesState.Profiles[:idx], c.profilesState.Profiles[idx+1:]...)
	if wasActive {
		if len(c.profilesState.Profiles) > 0 {
			aid := c.profilesState.Profiles[0].ID
			c.profilesState.ActiveID = &aid
		} else {
			c.profilesState.ActiveID = nil
		}
	}
	c.mu.Unlock()

	_ = os.Remove(c.ProfileFile(id))
	if err := c.saveProfiles(); err != nil {
		return err
	}
	if wasActive {
		if _, err := c.applyRuntimeConfig(); err != nil {
			return err
		}
	}
	return nil
}

// ReadProfileRaw returns a profile's meta and raw YAML text.
func (c *Core) ReadProfileRaw(id string) (map[string]any, error) {
	c.mu.RLock()
	idx := c.profileIndex(id)
	var meta config.ProfileMeta
	if idx >= 0 {
		meta = c.profilesState.Profiles[idx]
	}
	c.mu.RUnlock()
	if idx < 0 {
		return nil, util.NewHttpError(404, "配置不存在")
	}
	content, err := util.ReadFileString(c.ProfileFile(id))
	if err != nil {
		return nil, err
	}
	return map[string]any{"meta": meta, "content": content}, nil
}

// SaveProfileRaw validates and stores edited YAML for a local profile. Remote
// subscription content is owned by its URL and must stay read-only.
func (c *Core) SaveProfileRaw(id, content string) (config.ProfileMeta, error) {
	c.mu.RLock()
	idx := c.profileIndex(id)
	var current config.ProfileMeta
	if idx >= 0 {
		current = c.profilesState.Profiles[idx]
	}
	c.mu.RUnlock()
	if idx < 0 {
		return config.ProfileMeta{}, util.NewHttpError(404, "配置不存在")
	}
	if current.Type == "remote" {
		return config.ProfileMeta{}, util.NewHttpError(400, "订阅配置内容为只读，请使用订阅设置中的扩展规则")
	}
	if _, err := parseClashConfig(content); err != nil {
		return config.ProfileMeta{}, err
	}
	if err := os.WriteFile(c.ProfileFile(id), []byte(content), 0o644); err != nil {
		return config.ProfileMeta{}, err
	}
	now := time.Now().UnixMilli()
	c.mu.Lock()
	var meta config.ProfileMeta
	if i := c.profileIndex(id); i >= 0 {
		c.profilesState.Profiles[i].UpdatedAt = now
		meta = c.profilesState.Profiles[i]
	}
	active := c.profilesState.ActiveID != nil && *c.profilesState.ActiveID == id
	c.mu.Unlock()

	if err := c.saveProfiles(); err != nil {
		return meta, err
	}
	if active {
		if _, err := c.applyRuntimeConfig(); err != nil {
			return meta, err
		}
	}
	return meta, nil
}

// UpdateProfileInput carries the editable metadata of PUT /api/profiles/{id}.
type UpdateProfileInput struct {
	Name *string
	URL  *string
}

// UpdateProfileInfo edits a profile's name and, for remote profiles, its
// subscription URL. Changing the URL re-downloads the subscription immediately.
func (c *Core) UpdateProfileInfo(id string, input UpdateProfileInput) (config.ProfileMeta, error) {
	c.mu.RLock()
	idx := c.profileIndex(id)
	var meta config.ProfileMeta
	if idx >= 0 {
		meta = c.profilesState.Profiles[idx]
	}
	c.mu.RUnlock()
	if idx < 0 {
		return config.ProfileMeta{}, util.NewHttpError(404, "配置不存在")
	}
	if input.Name == nil && input.URL == nil {
		return config.ProfileMeta{}, util.NewHttpError(400, "没有可修改的订阅信息")
	}

	name := meta.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
		if name == "" {
			return config.ProfileMeta{}, util.NewHttpError(400, "名称不能为空")
		}
	}

	var newURL *string
	var userInfo *util.SubscriptionUserInfo
	urlChanged := false
	if input.URL != nil {
		if meta.Type != "remote" {
			return config.ProfileMeta{}, util.NewHttpError(400, "本地配置没有订阅链接")
		}
		trimmed := strings.TrimSpace(*input.URL)
		if trimmed == "" {
			return config.ProfileMeta{}, util.NewHttpError(400, "订阅链接不能为空")
		}
		if meta.URL == nil || *meta.URL != trimmed {
			if _, err := validateSubscriptionURL(trimmed); err != nil {
				return config.ProfileMeta{}, util.NewHttpError(400, err.Error())
			}
			text, ui, err := fetchSubscription(trimmed)
			if err != nil {
				return config.ProfileMeta{}, util.NewHttpError(502, util.ErrorMessage(err))
			}
			if err := os.WriteFile(c.ProfileFile(id), []byte(text), 0o644); err != nil {
				return config.ProfileMeta{}, err
			}
			newURL = &trimmed
			userInfo = ui
			urlChanged = true
		}
	}

	c.mu.Lock()
	if i := c.profileIndex(id); i >= 0 {
		p := &c.profilesState.Profiles[i]
		p.Name = name
		if newURL != nil {
			p.URL = newURL
			p.UserInfo = userInfo
			p.UpdatedAt = time.Now().UnixMilli()
		}
		meta = *p
	}
	active := c.profilesState.ActiveID != nil && *c.profilesState.ActiveID == id
	c.mu.Unlock()

	if err := c.saveProfiles(); err != nil {
		return config.ProfileMeta{}, err
	}
	if active {
		if urlChanged {
			if _, err := c.applyRuntimeConfig(); err != nil {
				return config.ProfileMeta{}, err
			}
		} else if err := c.writeRuntimeConfig(); err != nil {
			return config.ProfileMeta{}, err
		}
	}
	return meta, nil
}

// UpdateProfileSettings merges a per-subscription settings patch.
func (c *Core) UpdateProfileSettings(id string, patch any) (config.ProfileMeta, error) {
	c.mu.RLock()
	idx := c.profileIndex(id)
	var meta config.ProfileMeta
	if idx >= 0 {
		meta = c.profilesState.Profiles[idx]
	}
	c.mu.RUnlock()
	if idx < 0 {
		return config.ProfileMeta{}, util.NewHttpError(404, "配置不存在")
	}
	rules, hasRules, err := customRulesPatch(patch)
	if err != nil {
		return config.ProfileMeta{}, err
	}
	merged := profileSettingsToMap(meta.Settings)
	if pm, ok := patch.(map[string]any); ok {
		for k, v := range pm {
			merged[k] = v
		}
	}
	oldRules := meta.Settings.CustomRules
	ns := config.NormalizeProfileSettings(merged)
	if hasRules {
		ns.CustomRules = rules
	}
	c.mu.RLock()
	active := c.profilesState.ActiveID != nil && *c.profilesState.ActiveID == id
	globalSettings := c.settings
	c.mu.RUnlock()
	if !active {
		globalSettings.CustomRules = nil
	}
	if err := c.validateRulePolicyRefs(globalSettings, &meta, ns.CustomRules); err != nil {
		return config.ProfileMeta{}, err
	}
	c.mu.Lock()
	if i := c.profileIndex(id); i >= 0 {
		c.profilesState.Profiles[i].Settings = ns
		meta = c.profilesState.Profiles[i]
	}
	c.mu.Unlock()
	if err := c.saveProfiles(); err != nil {
		return meta, err
	}
	if active && hasRules && !slices.Equal(oldRules, ns.CustomRules) {
		if _, err := c.applyRuntimeConfig(); err != nil {
			return meta, err
		}
	}
	return meta, nil
}

// TestConfig returns the active subscription's speed-test params, or defaults.
func (c *Core) TestConfig() config.ProfileSettings {
	if p := c.ActiveProfile(); p != nil {
		return p.Settings
	}
	return config.DefaultProfileSettings()
}

// customRulesPatch extracts and strictly validates a customRules field from a
// settings patch. present=false means the patch did not touch custom rules.
func customRulesPatch(patch any) (rules []string, present bool, err error) {
	m, ok := patch.(map[string]any)
	if !ok {
		return nil, false, nil
	}
	raw, ok := m["customRules"]
	if !ok {
		return nil, false, nil
	}
	rules, err = config.ParseCustomRulesStrict(raw)
	if err != nil {
		return nil, true, err
	}
	return rules, true, nil
}

func profileSettingsFromInput(input any) (config.ProfileSettings, error) {
	rules, hasRules, err := customRulesPatch(input)
	if err != nil {
		return config.ProfileSettings{}, err
	}
	ns := config.NormalizeProfileSettings(input)
	if hasRules {
		ns.CustomRules = rules
	}
	return ns, nil
}

func profileSettingsToMap(s config.ProfileSettings) map[string]any {
	b, err := json.Marshal(s)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}
	return m
}
