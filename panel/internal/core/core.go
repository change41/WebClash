// Package core ports server/src/core.ts: panel orchestration, settings/profile
// persistence and runtime config generation. G2 covers settings/auth/config;
// G3 adds subscription fetch/CRUD, G4 kernel lifecycle, G9 the auto-update tick.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"webclash/internal/config"
	"webclash/internal/geo"
	"webclash/internal/geoip"
	"webclash/internal/kernel"
	"webclash/internal/mihomo"
	"webclash/internal/util"
)

// ApplyResult is the outcome of applying settings/config (port of ApplyResult).
type ApplyResult string

const (
	ResultSaved     ApplyResult = "saved"
	ResultReloaded  ApplyResult = "reloaded"
	ResultRestarted ApplyResult = "restarted"
)

var profileIDRe = regexp.MustCompile(`^[a-f0-9]{8,32}$`)

// Core holds panel state and orchestrates persistence + kernel.
type Core struct {
	DataDir             string
	KernelDir           string
	ProfilesDir         string
	ConfigPath          string
	SettingsPath        string
	ProfilesPath        string
	InitialPasswordFile string

	Kernel *kernel.Manager
	GeoIP  *geoip.Manager

	// history tracks closed connections (1s /connections diff, bounded ring).
	history *connHistory
	// traffic aggregates the same 1s /connections totals into 24h minute buckets.
	traffic *trafficAgg
	// system samples host CPU/memory/load every 5s.
	system *systemSampler

	mu              sync.RWMutex
	settings        config.Settings
	profilesState   config.ProfilesState
	lastConfigAt    *int64
	lastConfigError *string
	initialPassword *string

	persistMu sync.Mutex

	// dnsMu/dnsCache memoize hostname→IP resolution for GeoIP attribution of
	// domain-only connections (nil map is created lazily on first resolve).
	dnsMu    sync.Mutex
	dnsCache map[string]dnsCacheEntry

	// profileUpdateMu serializes subscription refreshes; update-all and the
	// auto-update tick TryLock it so concurrent runs return 409 / skip.
	profileUpdateMu sync.Mutex

	// autoUpdateStop/Done bound the 60s refresh ticker; stopOnce guards Shutdown.
	autoUpdateStop chan struct{}
	autoUpdateDone chan struct{}
	stopOnce       sync.Once
}

// New creates a Core rooted at dataDir (resolved absolute).
func New(dataDir string) *Core {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	c := &Core{
		DataDir:             abs,
		KernelDir:           filepath.Join(abs, "kernel"),
		ProfilesDir:         filepath.Join(abs, "profiles"),
		ConfigPath:          filepath.Join(abs, "kernel", "config.yaml"),
		SettingsPath:        filepath.Join(abs, "settings.json"),
		ProfilesPath:        filepath.Join(abs, "profiles.json"),
		InitialPasswordFile: filepath.Join(abs, "initial-password.txt"),
		settings:            config.DefaultSettings(),
		profilesState:       config.ProfilesState{ActiveID: nil, Profiles: []config.ProfileMeta{}},
	}
	c.Kernel = kernel.New(kernel.Options{
		DataDir:     c.DataDir,
		KernelDir:   c.KernelDir,
		ConfigPath:  c.ConfigPath,
		GetSettings: c.Settings,
		BeforeStart: c.prepareGeo,
	})
	c.GeoIP = geoip.NewManager(filepath.Join(abs, "geoip"))
	c.history = newConnHistory(func() int { return c.Settings().ConnHistory.Limit })
	c.traffic = newTrafficAgg()
	c.system = newSystemSampler()
	return c
}

// Settings returns a snapshot of the current settings.
func (c *Core) Settings() config.Settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.settings
}

// LastConfigAt returns the last successful config write time (ms), or nil.
func (c *Core) LastConfigAt() *int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastConfigAt
}

// LastConfigError returns the last config generation error, or nil.
func (c *Core) LastConfigError() *string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastConfigError
}

// InitialPassword returns the first-boot generated password, or nil.
func (c *Core) InitialPassword() *string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialPassword
}

// Init loads/creates settings, profiles and the runtime config (port of init).
func (c *Core) Init() error {
	for _, dir := range []string{c.DataDir, c.KernelDir, c.ProfilesDir, filepath.Join(c.DataDir, "bin"), filepath.Join(c.DataDir, "tmp"), filepath.Join(c.DataDir, "geoip")} {
		if err := util.EnsureDir(dir); err != nil {
			return err
		}
	}

	raw := util.ReadJson[any](c.SettingsPath, map[string]any{})
	var legacySubscription any
	if m, ok := raw.(map[string]any); ok {
		legacySubscription = m["subscription"]
	}
	settings := config.NormalizeSettings(raw)

	if settings.Auth.PasswordHash == nil {
		envPassword := util.TrimSpaceEnv("WEBUI_PASSWORD")
		password := envPassword
		if password == "" {
			password = util.GeneratePassword(16)
		}
		hash := util.HashPassword(password)
		settings.Auth.PasswordHash = &hash
		c.mu.Lock()
		c.initialPassword = &password
		c.mu.Unlock()
		if envPassword == "" {
			if err := os.WriteFile(c.InitialPasswordFile, []byte(password+"\n"), 0o600); err != nil {
				return err
			}
		}
	}
	c.mu.Lock()
	c.settings = settings
	c.mu.Unlock()
	if err := c.persistSettings(); err != nil {
		return err
	}

	state, err := c.loadProfiles(legacySubscription)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.profilesState = state
	c.mu.Unlock()

	c.resolveKernelPorts()

	if err := c.writeRuntimeConfig(); err != nil {
		msg := util.ErrorMessage(err)
		c.mu.Lock()
		c.lastConfigError = &msg
		c.mu.Unlock()
		fmt.Fprintf(os.Stderr, "[webclash] 生成配置失败，将使用内置直连配置：%s\n", msg)
		if err := c.writeBuiltinConfig(); err != nil {
			return err
		}
	}

	if c.Settings().Kernel.AutoStart {
		go func() {
			if err := c.Kernel.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "[webclash] 自动启动内核失败：%s\n", util.ErrorMessage(err))
			}
		}()
	}

	if s := c.Settings(); s.GeoIP.Enable {
		go c.GeoIP.Ensure(s.GeoIP.Source, s.Kernel.Mirror, false)
	}

	c.autoUpdateStop = make(chan struct{})
	c.autoUpdateDone = make(chan struct{})
	go c.autoUpdateLoop()
	c.history.Start(c.historyTick)
	c.system.Start()
	return nil
}

// autoUpdateLoop refreshes due subscriptions every 60s until Shutdown.
func (c *Core) autoUpdateLoop() {
	defer close(c.autoUpdateDone)
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.autoUpdateStop:
			return
		case <-t.C:
			c.runAutoUpdate()
		}
	}
}

// runAutoUpdate refreshes remote profiles whose interval has elapsed (port of
// runAutoUpdate). It TryLocks profileUpdateMu so a concurrent update-all wins
// and the tick is skipped, mirroring the Node updatingProfiles guard.
func (c *Core) runAutoUpdate() {
	if !c.profileUpdateMu.TryLock() {
		return
	}
	defer c.profileUpdateMu.Unlock()

	now := time.Now().UnixMilli()
	type remote struct{ id, url, name string }
	var due []remote
	c.mu.RLock()
	for _, p := range c.profilesState.Profiles {
		if p.Type == "remote" && p.URL != nil && *p.URL != "" &&
			p.Settings.IntervalHours > 0 &&
			now-p.UpdatedAt >= int64(p.Settings.IntervalHours)*3600_000 {
			due = append(due, remote{p.ID, *p.URL, p.Name})
		}
	}
	activeID := c.profilesState.ActiveID
	c.mu.RUnlock()
	if len(due) == 0 {
		return
	}

	activeTouched := false
	for _, r := range due {
		if err := c.refreshRemoteProfile(r.id, r.url); err != nil {
			fmt.Fprintf(os.Stderr, "[webclash] 自动更新「%s」失败：%s\n", r.name, util.ErrorMessage(err))
			continue
		}
		if activeID != nil && *activeID == r.id {
			activeTouched = true
		}
	}
	if err := c.saveProfiles(); err != nil {
		fmt.Fprintf(os.Stderr, "[webclash] 自动更新保存失败：%s\n", util.ErrorMessage(err))
		return
	}
	if activeTouched {
		if _, err := c.applyRuntimeConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "[webclash] 自动更新应用配置失败：%s\n", util.ErrorMessage(err))
		}
	}
}

// Shutdown stops background work and the kernel (port of shutdown).
func (c *Core) Shutdown() error {
	c.stopOnce.Do(func() {
		if c.autoUpdateStop != nil {
			close(c.autoUpdateStop)
			<-c.autoUpdateDone
		}
		c.history.Stop()
		c.system.Stop()
	})
	c.GeoIP.Close()
	return c.Kernel.Stop()
}

// KernelStatus returns the kernel runtime snapshot (port of kernelStatus).
func (c *Core) KernelStatus() config.KernelStatus {
	return c.Kernel.Status()
}

func (c *Core) persistSettings() error {
	s := c.Settings()
	c.persistMu.Lock()
	defer c.persistMu.Unlock()
	return util.WriteJsonAtomic(c.SettingsPath, s)
}

// ActiveProfile returns the active profile meta, or nil.
func (c *Core) ActiveProfile() *config.ProfileMeta {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.profilesState.ActiveID == nil {
		return nil
	}
	for i := range c.profilesState.Profiles {
		if c.profilesState.Profiles[i].ID == *c.profilesState.ActiveID {
			p := c.profilesState.Profiles[i]
			return &p
		}
	}
	return nil
}

// ProfileFile returns the on-disk YAML path for a profile id.
func (c *Core) ProfileFile(id string) string {
	return filepath.Join(c.ProfilesDir, id+".yaml")
}

func (c *Core) loadProfiles(legacySubscription any) (config.ProfilesState, error) {
	raw := util.ReadJson[map[string]any](c.ProfilesPath, map[string]any{"activeId": nil, "profiles": []any{}})
	list, _ := raw["profiles"].([]any)
	now := time.Now().UnixMilli()
	profiles := make([]config.ProfileMeta, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, idOk := m["id"].(string)
		name, nameOk := m["name"].(string)
		if !idOk || !nameOk || !profileIDRe.MatchString(id) {
			continue
		}
		if !util.Exists(c.ProfileFile(id)) {
			continue
		}
		typ := "local"
		if m["type"] == "remote" {
			typ = "remote"
		}
		var url *string
		if s, ok := m["url"].(string); ok {
			url = &s
		}
		settingsSrc := legacySubscription
		if v, ok := m["settings"]; ok && v != nil {
			settingsSrc = v
		}
		profiles = append(profiles, config.ProfileMeta{
			ID:        id,
			Name:      name,
			Type:      typ,
			URL:       url,
			CreatedAt: timestampOr(m["createdAt"], now),
			UpdatedAt: timestampOr(m["updatedAt"], now),
			UserInfo:  toUserInfo(m["userInfo"]),
			Settings:  config.NormalizeProfileSettings(settingsSrc),
		})
	}
	var activeID *string
	if want, ok := raw["activeId"].(string); ok {
		for i := range profiles {
			if profiles[i].ID == want {
				activeID = &profiles[i].ID
				break
			}
		}
	}
	if activeID == nil && len(profiles) > 0 {
		activeID = &profiles[0].ID
	}
	return config.ProfilesState{ActiveID: activeID, Profiles: profiles}, nil
}

func (c *Core) profileConfigBase(profile *config.ProfileMeta) (*yaml.Node, error) {
	if profile == nil {
		return nil, nil
	}
	raw, err := util.ReadFileString(c.ProfileFile(profile.ID))
	if err != nil {
		return nil, err
	}
	parsed, perr := parseClashConfig(raw)
	if perr != nil {
		return nil, fmt.Errorf("配置「%s」无法解析：%s", profile.Name, util.ErrorMessage(perr))
	}
	return parsed, nil
}

// buildConfigFor builds the effective config for a profile and validates the
// custom-rule policies against it. Validation runs before the config is written
// so a missing target is reported instead of crashing the kernel.
func (c *Core) buildConfigFor(settings config.Settings, profile *config.ProfileMeta, profileRules []string) (*yaml.Node, error) {
	base, err := c.profileConfigBase(profile)
	if err != nil {
		return nil, err
	}
	cfg := mihomo.BuildConfig(base, settings)
	if profile != nil {
		cfg = mihomo.PrependRules(cfg, profileRules)
	}
	if err := mihomo.ValidateRulePolicies(cfg, settings.CustomRules); err != nil {
		return nil, fmt.Errorf("全局扩展规则：%w", err)
	}
	if profile != nil {
		if err := mihomo.ValidateRulePolicies(cfg, profileRules); err != nil {
			return nil, fmt.Errorf("订阅「%s」的扩展规则：%w", profile.Name, err)
		}
	}
	return cfg, nil
}

func (c *Core) buildCurrentConfig() (*yaml.Node, error) {
	profile := c.ActiveProfile()
	settings := c.Settings()
	var profileRules []string
	if profile != nil {
		profileRules = profile.Settings.CustomRules
	}
	return c.buildConfigFor(settings, profile, profileRules)
}

// validateRulePolicyRefs returns a user-facing 400 for an unknown rule target.
// Other config-generation errors are left to the normal apply path.
func (c *Core) validateRulePolicyRefs(settings config.Settings, profile *config.ProfileMeta, profileRules []string) error {
	_, err := c.buildConfigFor(settings, profile, profileRules)
	if err == nil {
		return nil
	}
	var policyErr *mihomo.RulePolicyError
	if errors.As(err, &policyErr) {
		return util.NewHttpError(400, err.Error())
	}
	return nil
}

func rulePolicyHttpError(err error) error {
	var policyErr *mihomo.RulePolicyError
	if errors.As(err, &policyErr) {
		return util.NewHttpError(400, err.Error())
	}
	return err
}

func configHeader(profile *config.ProfileMeta) string {
	label := "builtin-direct"
	if profile != nil {
		label = fmt.Sprintf("%s (%s)", profile.Name, profile.ID)
	}
	return fmt.Sprintf("# Generated by WebClash at %s\n# profile: %s\n", nowISO(), label)
}

// writeRuntimeConfig builds and atomically writes data/kernel/config.yaml.
// Geo is applied here with a stat-only check (zero network) so the config is
// always loadable; missing .dat files degrade rather than block. Kernel.Start
// provisions the files before spawn and rewrites via this path when they appear.
func (c *Core) writeRuntimeConfig() error {
	cfg, err := c.buildCurrentConfig()
	if err != nil {
		return rulePolicyHttpError(err)
	}
	warnings := geo.Apply(cfg, geo.CheckFiles(c.KernelDir), c.Settings().Kernel.Mirror)
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	header := configHeader(c.ActiveProfile()) +
		"# 此文件由面板自动生成，手动修改会在下次应用配置时被覆盖\n"
	if err := util.WriteFileAtomic(c.ConfigPath, header+string(body), nil); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	c.mu.Lock()
	c.lastConfigAt = &now
	c.lastConfigError = joinWarnings(warnings)
	c.mu.Unlock()
	return nil
}

func joinWarnings(warnings []string) *string {
	if len(warnings) == 0 {
		return nil
	}
	s := strings.Join(warnings, "；")
	return &s
}

func (c *Core) writeBuiltinConfig() error {
	settings := c.Settings()
	cfg := mihomo.BuildConfig(nil, settings)
	warnings := geo.Apply(cfg, geo.CheckFiles(c.KernelDir), settings.Kernel.Mirror)
	var ruleWarning string
	if err := mihomo.ValidateRulePolicies(cfg, settings.CustomRules); err != nil {
		ruleWarning = fmt.Sprintf("全局扩展规则无效，内置直连配置已忽略：%s", util.ErrorMessage(err))
		fmt.Fprintf(os.Stderr, "[webclash] %s\n", ruleWarning)
		settings.CustomRules = nil
		cfg = mihomo.BuildConfig(nil, settings)
		warnings = geo.Apply(cfg, geo.CheckFiles(c.KernelDir), settings.Kernel.Mirror)
	}
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("# Generated by WebClash at %s\n# profile: builtin-direct\n", nowISO())
	if err := util.WriteFileAtomic(c.ConfigPath, header+string(body), nil); err != nil {
		return err
	}
	if ruleWarning != "" {
		warnings = append(warnings, ruleWarning)
	}
	now := time.Now().UnixMilli()
	c.mu.Lock()
	c.lastConfigAt = &now
	c.lastConfigError = joinWarnings(warnings)
	c.mu.Unlock()
	return nil
}

// prepareGeo runs before the kernel spawns (bounded network): it provisions any
// needed-but-missing geo files and, if a file newly appeared, rewrites the config
// so the degraded geo rules are restored. Best-effort; never fatal.
func (c *Core) prepareGeo(ctx context.Context) {
	cfg, err := c.buildCurrentConfig()
	if err != nil {
		return
	}
	needed := geo.Analyze(cfg)
	if !needed.GeoIP && !needed.GeoSite {
		return
	}
	before := geo.CheckFiles(c.KernelDir)
	after := geo.Provision(ctx, c.KernelDir, needed, c.Settings().Kernel.Mirror)
	if before != after {
		if err := c.writeRuntimeConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "[webclash] geo 预置后重写配置失败：%s\n", util.ErrorMessage(err))
		}
	}
}

// SaveSettings merges a patch (auth excluded), persists and applies it.
func (c *Core) SaveSettings(patch any) (ApplyResult, error) {
	clean := map[string]any{}
	if m, ok := patch.(map[string]any); ok {
		for k, v := range m {
			clean[k] = v
		}
	}
	delete(clean, "auth")

	customRules, hasCustomRules, err := customRulesPatch(clean)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	before := c.settings
	next := config.NormalizeSettings(util.DeepMerge(settingsToMap(before), clean))
	if hasCustomRules {
		next.CustomRules = customRules
	}
	c.mu.Unlock()

	if err := config.ValidateAuthentication(next.Authentication); err != nil {
		return "", util.NewHttpError(400, err.Error())
	}

	profile := c.ActiveProfile()
	var profileRules []string
	if profile != nil {
		profileRules = profile.Settings.CustomRules
	}
	if err := c.validateRulePolicyRefs(next, profile, profileRules); err != nil {
		return "", err
	}

	c.mu.Lock()
	c.settings = next
	c.mu.Unlock()
	if err := c.persistSettings(); err != nil {
		return "", err
	}
	binaryChanged := before.Kernel.BinaryPath != next.Kernel.BinaryPath
	c.applyGeoIPChange(before.GeoIP, next.GeoIP, next.Kernel.Mirror)

	if err := c.writeRuntimeConfig(); err != nil {
		return "", err
	}

	if c.Kernel.State() == "running" {
		if binaryChanged {
			if err := c.Kernel.Restart(); err != nil {
				return "", err
			}
			return ResultRestarted, nil
		}
		if err := c.Kernel.ReloadConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "[webclash] 热重载失败，改为重启内核：%s\n", util.ErrorMessage(err))
			if err := c.Kernel.Restart(); err != nil {
				return "", err
			}
			return ResultRestarted, nil
		}
		return ResultReloaded, nil
	}
	return ResultSaved, nil
}

// RotateControllerSecret generates a new controller secret and applies it.
func (c *Core) RotateControllerSecret() (string, error) {
	secret := util.RandomToken(24)
	c.mu.Lock()
	c.settings.Controller.Secret = secret
	c.mu.Unlock()
	if err := c.persistSettings(); err != nil {
		return "", err
	}
	if err := c.writeRuntimeConfig(); err != nil {
		return "", err
	}
	if c.Kernel.IsRunning() {
		if err := c.Kernel.Restart(); err != nil {
			return "", err
		}
	}
	return secret, nil
}

// ChangePassword verifies the current password, sets a new one and rotates the
// session secret so all old sessions are invalidated (port of changePassword).
func (c *Core) ChangePassword(current *string, next string) error {
	c.mu.RLock()
	hash := c.settings.Auth.PasswordHash
	c.mu.RUnlock()
	if hash != nil {
		cur := ""
		if current != nil {
			cur = *current
		}
		if !util.VerifyPassword(cur, *hash) {
			return util.NewHttpError(400, "当前密码不正确")
		}
	}
	if next == "" || util.JsStringLength(next) < 6 {
		return util.NewHttpError(400, "新密码长度至少 6 位")
	}
	newHash := util.HashPassword(next)
	newSecret := util.RandomToken(32)
	c.mu.Lock()
	c.settings.Auth.PasswordHash = &newHash
	c.settings.Auth.Secret = newSecret
	c.mu.Unlock()
	if err := c.persistSettings(); err != nil {
		return err
	}
	_ = os.Remove(c.InitialPasswordFile)
	return nil
}

func settingsToMap(s config.Settings) map[string]any {
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

func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}
