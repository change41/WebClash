package core

import (
	"webclash/internal/config"
	"webclash/internal/sysproxy"
	"webclash/internal/util"
)

// SystemProxyStatus combines persisted enable state with a live inspection of
// the target shell configuration file.
type SystemProxyStatus struct {
	Enable  bool             `json:"enable"`
	Synced  bool             `json:"synced"`
	Preview sysproxy.Preview `json:"preview"`
	Error   *string          `json:"error"`
}

// SystemProxySyncResult reports whether SaveSettings proactively updated the
// shell proxy configuration after a port or authentication change.
type SystemProxySyncResult struct {
	Updated bool    `json:"updated"`
	Error   *string `json:"error,omitempty"`
}

// SaveSettingsOutcome wraps the kernel apply result and any automatic system
// proxy sync outcome triggered by the settings change.
type SaveSettingsOutcome struct {
	Result          ApplyResult
	SystemProxySync *SystemProxySyncResult
}

// SetSystemProxyEnv overrides shell/home detection (used by tests).
func (c *Core) SetSystemProxyEnv(env sysproxy.Env) {
	c.sysProxyMu.Lock()
	c.sysProxyEnv = env
	c.sysProxyMu.Unlock()
}

func (c *Core) resolveSystemProxyShellLocked(s config.Settings) sysproxy.ShellInfo {
	detected := sysproxy.DetectShell(c.sysProxyEnv)
	if s.SystemProxy.Enable && s.SystemProxy.ConfigPath != "" && s.SystemProxy.Shell != "" {
		return sysproxy.ShellInfo{
			Shell:          s.SystemProxy.Shell,
			ShellPath:      detected.ShellPath,
			ConfigPath:     s.SystemProxy.ConfigPath,
			FileExists:     util.Exists(s.SystemProxy.ConfigPath),
			InContainer:    detected.InContainer,
			LoginShellOnly: detected.LoginShellOnly,
		}
	}
	return detected
}

func (c *Core) buildSystemProxyStatusLocked(s config.Settings) SystemProxyStatus {
	shellInfo := c.resolveSystemProxyShellLocked(s)
	spec := sysproxy.SpecFromSettings(s)
	preview, err := sysproxy.BuildPreview(c.sysProxyEnv, shellInfo, spec)
	var errPtr *string
	if err != nil {
		msg := util.ErrorMessage(err)
		errPtr = &msg
	}
	synced := s.SystemProxy.Enable && err == nil && preview.ManagedBlockSynced
	return SystemProxyStatus{
		Enable:  s.SystemProxy.Enable,
		Synced:  synced,
		Preview: preview,
		Error:   errPtr,
	}
}

// SystemProxyStatus inspects the target shell configuration file and returns
// the current enable state, existing proxy entries, and rendered preview.
func (c *Core) SystemProxyStatus() SystemProxyStatus {
	c.sysProxyMu.Lock()
	defer c.sysProxyMu.Unlock()
	return c.buildSystemProxyStatusLocked(c.Settings())
}

// EnableSystemProxy writes the WebClash managed proxy block into the detected
// shell configuration file and persists systemProxy.enable=true.
func (c *Core) EnableSystemProxy() (SystemProxyStatus, error) {
	c.sysProxyMu.Lock()
	defer c.sysProxyMu.Unlock()

	s := c.Settings()
	shellInfo := sysproxy.DetectShell(c.sysProxyEnv)
	spec := sysproxy.SpecFromSettings(s)

	// If previously enabled on a different rc file, clean up the old managed block.
	if s.SystemProxy.Enable && s.SystemProxy.ConfigPath != "" && s.SystemProxy.ConfigPath != shellInfo.ConfigPath {
		_, _ = sysproxy.Remove(s.SystemProxy.ConfigPath)
	}

	if _, err := sysproxy.Apply(shellInfo, spec); err != nil {
		return SystemProxyStatus{}, util.NewHttpError(500, util.ErrorMessage(err))
	}

	c.mu.Lock()
	c.settings.SystemProxy = config.SystemProxySettings{
		Enable:     true,
		Shell:      shellInfo.Shell,
		ConfigPath: shellInfo.ConfigPath,
	}
	updatedSettings := c.settings
	c.mu.Unlock()

	if err := c.persistSettings(); err != nil {
		return SystemProxyStatus{}, err
	}
	return c.buildSystemProxyStatusLocked(updatedSettings), nil
}

// DisableSystemProxy removes the WebClash managed proxy block from the shell
// configuration file and persists systemProxy.enable=false.
func (c *Core) DisableSystemProxy() (SystemProxyStatus, error) {
	c.sysProxyMu.Lock()
	defer c.sysProxyMu.Unlock()

	s := c.Settings()
	targetPath := s.SystemProxy.ConfigPath
	if targetPath == "" {
		targetPath = sysproxy.DetectShell(c.sysProxyEnv).ConfigPath
	}
	if _, err := sysproxy.Remove(targetPath); err != nil {
		return SystemProxyStatus{}, util.NewHttpError(500, util.ErrorMessage(err))
	}

	c.mu.Lock()
	c.settings.SystemProxy = config.SystemProxySettings{
		Enable:     false,
		Shell:      "",
		ConfigPath: "",
	}
	updatedSettings := c.settings
	c.mu.Unlock()

	if err := c.persistSettings(); err != nil {
		return SystemProxyStatus{}, err
	}
	return c.buildSystemProxyStatusLocked(updatedSettings), nil
}

// SyncSystemProxy updates the shell configuration file to match the latest
// ports and authentication credentials when system proxy is enabled.
func (c *Core) SyncSystemProxy() (bool, error) {
	c.sysProxyMu.Lock()
	defer c.sysProxyMu.Unlock()

	s := c.Settings()
	if !s.SystemProxy.Enable {
		return false, nil
	}
	shellInfo := c.resolveSystemProxyShellLocked(s)
	spec := sysproxy.SpecFromSettings(s)

	changed, err := sysproxy.Apply(shellInfo, spec)
	if err != nil {
		return false, err
	}

	if s.SystemProxy.Shell != shellInfo.Shell || s.SystemProxy.ConfigPath != shellInfo.ConfigPath {
		c.mu.Lock()
		c.settings.SystemProxy.Shell = shellInfo.Shell
		c.settings.SystemProxy.ConfigPath = shellInfo.ConfigPath
		c.mu.Unlock()
		if err := c.persistSettings(); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func inboundUsersEqual(a, b []config.InboundUser) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
