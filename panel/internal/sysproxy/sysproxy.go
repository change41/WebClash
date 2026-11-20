// Package sysproxy detects the current user's shell configuration file and
// manages a delimited WebClash proxy environment block inside it.
package sysproxy

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"webclash/internal/config"
	"webclash/internal/util"
)

const (
	// BlockStart and BlockEnd delimit the WebClash-managed proxy section in a
	// shell rc file so updates and removal never touch surrounding user config.
	BlockStart = "# >>> WebClash System Proxy >>>"
	BlockEnd   = "# <<< WebClash System Proxy <<<"

	defaultNoProxy = "localhost,127.0.0.1,::1"
)

// Env abstracts host environment lookups for deterministic testing. A zero Env
// reads from the live OS environment.
type Env struct {
	GOOS            string
	HomeDir         string
	ShellEnv        string
	ShellEnvSet     bool
	ShEnv           string
	ShEnvSet        bool
	Zdotdir         string
	ZdotdirSet      bool
	XDGConfig       string
	XDGSet          bool
	PasswdPath      string
	UID             int
	UIDSet          bool
	CurrentUser     string
	InContainer     *bool
	Environ         []string
	EnvironSet      bool
	SkipSystemFiles bool
	DarwinShell     func(username string) string
}

// ShellInfo describes the detected user shell and target config file.
type ShellInfo struct {
	Shell          string `json:"shell"`
	ShellPath      string `json:"shellPath"`
	ConfigPath     string `json:"configPath"`
	FileExists     bool   `json:"fileExists"`
	InContainer    bool   `json:"inContainer"`
	LoginShellOnly bool   `json:"loginShellOnly"`
}

// ExistingEntry represents one proxy-related line already present in a shell
// or system configuration file.
type ExistingEntry struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
	Managed bool   `json:"managed"`
}

// EnvProxyEntry represents an active proxy environment variable in the current
// process environment.
type EnvProxyEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ProxySpec holds the effective local proxy ports and optional credentials.
type ProxySpec struct {
	HTTPPort  int
	SOCKSPort int
	Username  string
	Password  string
}

// Preview describes the detected shell, rendered proxy configuration, and any
// pre-existing proxy entries already found in config files or environment vars.
type Preview struct {
	Shell              ShellInfo       `json:"shell"`
	HTTPProxy          string          `json:"httpProxy"`
	SOCKSProxy         string          `json:"socksProxy"`
	HasAuth            bool            `json:"hasAuth"`
	AuthUsername       string          `json:"authUsername"`
	Block              string          `json:"block"`
	MaskedBlock        string          `json:"maskedBlock"`
	Existing           []ExistingEntry `json:"existing"`
	EnvProxies         []EnvProxyEntry `json:"envProxies"`
	HasExternalProxy   bool            `json:"hasExternalProxy"`
	HasEnvProxy        bool            `json:"hasEnvProxy"`
	HasManagedBlock    bool            `json:"hasManagedBlock"`
	ManagedBlockSynced bool            `json:"managedBlockSynced"`
	SourceCommand      string          `json:"sourceCommand"`
}

// SpecFromSettings derives the proxy endpoints and credentials from panel
// settings. Standalone HTTP/SOCKS ports take precedence when configured;
// otherwise the mixed port is used. When port authentication is enabled, the
// first configured credential pair is embedded.
func SpecFromSettings(s config.Settings) ProxySpec {
	httpPort := s.Ports.Mixed
	if s.Ports.HTTP > 0 {
		httpPort = s.Ports.HTTP
	}
	socksPort := s.Ports.Mixed
	if s.Ports.SOCKS > 0 {
		socksPort = s.Ports.SOCKS
	}
	spec := ProxySpec{
		HTTPPort:  httpPort,
		SOCKSPort: socksPort,
	}
	if len(s.Authentication) > 0 {
		spec.Username = s.Authentication[0].Username
		spec.Password = s.Authentication[0].Password
	}
	return spec
}

// DetectShell identifies the user's configured shell and rc file path.
func DetectShell(env Env) ShellInfo {
	goos := env.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	home := strings.TrimSpace(env.HomeDir)
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			home = h
		} else {
			home = os.Getenv("HOME")
		}
	}

	rawShell := env.ShellEnv
	if !env.ShellEnvSet && rawShell == "" {
		rawShell = os.Getenv("SHELL")
	}
	rawShell = strings.TrimSpace(rawShell)

	if isUnusableShell(rawShell) {
		uid := env.UID
		if !env.UIDSet {
			uid = os.Getuid()
		}
		passwdPath := env.PasswdPath
		if passwdPath == "" {
			passwdPath = "/etc/passwd"
		}
		if s := lookupPasswdShell(passwdPath, uid); !isUnusableShell(s) {
			rawShell = s
		}
	}

	if isUnusableShell(rawShell) && goos == "darwin" {
		username := strings.TrimSpace(env.CurrentUser)
		if username == "" {
			username = strings.TrimSpace(os.Getenv("USER"))
		}
		if username != "" {
			lookup := env.DarwinShell
			if lookup == nil {
				lookup = lookupDarwinShell
			}
			if s := strings.TrimSpace(lookup(username)); !isUnusableShell(s) {
				rawShell = s
			}
		}
	}

	if isUnusableShell(rawShell) {
		if goos == "darwin" {
			rawShell = "/bin/zsh"
		} else if util.Exists("/bin/bash") {
			rawShell = "/bin/bash"
		} else {
			rawShell = "/bin/sh"
		}
	}

	shell := NormalizeShell(rawShell)

	zdotdir := env.Zdotdir
	if !env.ZdotdirSet && zdotdir == "" {
		zdotdir = os.Getenv("ZDOTDIR")
	}
	xdgConfig := env.XDGConfig
	if !env.XDGSet && xdgConfig == "" {
		xdgConfig = os.Getenv("XDG_CONFIG_HOME")
	}

	shEnv := env.ShEnv
	if !env.ShEnvSet && shEnv == "" {
		shEnv = os.Getenv("ENV")
	}
	shEnv = strings.TrimSpace(shEnv)

	inContainer := detectContainer()
	if env.InContainer != nil {
		inContainer = *env.InContainer
	}

	cfgPath := resolveConfigPath(shell, goos, home, strings.TrimSpace(zdotdir), strings.TrimSpace(xdgConfig))
	// POSIX sh (~/.profile) is only loaded automatically by login shells
	// (e.g. `sh -l` or SSH login) unless $ENV points to the same file.
	loginOnly := shell == "sh" && (shEnv == "" || filepath.Clean(shEnv) != filepath.Clean(cfgPath))

	return ShellInfo{
		Shell:          shell,
		ShellPath:      rawShell,
		ConfigPath:     cfgPath,
		FileExists:     util.Exists(cfgPath),
		InContainer:    inContainer,
		LoginShellOnly: loginOnly,
	}
}

func detectContainer() bool {
	return util.Exists("/.dockerenv") || util.Exists("/run/.containerenv")
}

// NormalizeShell maps a shell path or binary name to a canonical shell type.
func NormalizeShell(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return "sh"
	}
	// Handle both POSIX and Windows-style separators defensively.
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.ToLower(strings.TrimPrefix(base, "-"))
	base = strings.TrimSuffix(base, ".exe")

	switch base {
	case "bash", "rbash":
		return "bash"
	case "zsh":
		return "zsh"
	case "fish":
		return "fish"
	case "tcsh":
		return "tcsh"
	case "csh":
		return "csh"
	case "ksh", "ksh93", "mksh", "pdksh", "oksh":
		return "ksh"
	case "nu", "nushell":
		return "nushell"
	case "elvish":
		return "elvish"
	case "pwsh", "powershell":
		return "pwsh"
	case "sh", "dash", "ash":
		return "sh"
	default:
		return "sh"
	}
}

func isUnusableShell(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	base := strings.ToLower(filepath.Base(s))
	return base == "nologin" || base == "false" || base == "true"
}

func lookupPasswdShell(passwdPath string, uid int) string {
	f, err := os.Open(passwdPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	wantUID := strconv.Itoa(uid)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[2] == wantUID {
			return strings.TrimSpace(fields[6])
		}
	}
	return ""
}

func lookupDarwinShell(username string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "dscl", ".", "-read", "/Users/"+username, "UserShell").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "UserShell:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "UserShell:"))
		}
	}
	return ""
}

func resolveConfigPath(shell, goos, home, zdotdir, xdgConfig string) string {
	xdgBase := xdgConfig
	if xdgBase == "" {
		xdgBase = filepath.Join(home, ".config")
	}

	switch shell {
	case "bash":
		if goos == "darwin" {
			bashProfile := filepath.Join(home, ".bash_profile")
			bashrc := filepath.Join(home, ".bashrc")
			if util.Exists(bashProfile) {
				return bashProfile
			}
			if util.Exists(bashrc) {
				return bashrc
			}
			return bashProfile
		}
		return filepath.Join(home, ".bashrc")
	case "zsh":
		base := zdotdir
		if base == "" {
			base = home
		}
		return filepath.Join(base, ".zshrc")
	case "fish":
		return filepath.Join(xdgBase, "fish", "config.fish")
	case "tcsh":
		tcshrc := filepath.Join(home, ".tcshrc")
		if util.Exists(tcshrc) {
			return tcshrc
		}
		return filepath.Join(home, ".cshrc")
	case "csh":
		return filepath.Join(home, ".cshrc")
	case "ksh":
		kshrc := filepath.Join(home, ".kshrc")
		if util.Exists(kshrc) {
			return kshrc
		}
		return filepath.Join(home, ".profile")
	case "nushell":
		return filepath.Join(xdgBase, "nushell", "env.nu")
	case "elvish":
		return filepath.Join(xdgBase, "elvish", "rc.elv")
	case "pwsh":
		return filepath.Join(xdgBase, "powershell", "Microsoft.PowerShell_profile.ps1")
	default:
		return filepath.Join(home, ".profile")
	}
}

// SourceCommand returns the shell command a user can run to reload configPath
// in an already-open terminal.
func SourceCommand(shell, configPath string) string {
	quoted := quotePOSIX(configPath)
	switch shell {
	case "bash", "zsh", "csh", "tcsh":
		return "source " + quoted
	case "fish":
		return "source " + quoteFish(configPath)
	case "nushell":
		return "source " + quoteNushell(configPath)
	case "pwsh":
		return ". " + quoteSingleDoubled(configPath)
	case "elvish":
		return "重新打开终端生效"
	default:
		return ". " + quoted
	}
}

// BuildProxyURL formats a local proxy URL with optional RFC 3986 userinfo.
func BuildProxyURL(scheme string, port int, username, password string, maskPassword bool) string {
	host := fmt.Sprintf("127.0.0.1:%d", port)
	if username == "" {
		return fmt.Sprintf("%s://%s", scheme, host)
	}
	if maskPassword {
		encUser := url.User(username).String()
		return fmt.Sprintf("%s://%s:******@%s", scheme, encUser, host)
	}
	u := &url.URL{
		Scheme: scheme,
		User:   url.UserPassword(username, password),
		Host:   host,
	}
	return u.String()
}

// RenderBlock renders the complete WebClash-managed proxy block for the given
// shell type and proxy specification.
func RenderBlock(shell string, spec ProxySpec, maskPassword bool) string {
	httpURL := BuildProxyURL("http", spec.HTTPPort, spec.Username, spec.Password, maskPassword)
	socksURL := BuildProxyURL("socks5", spec.SOCKSPort, spec.Username, spec.Password, maskPassword)

	vars := []struct {
		key string
		val string
	}{
		{"http_proxy", httpURL},
		{"https_proxy", httpURL},
		{"HTTP_PROXY", httpURL},
		{"HTTPS_PROXY", httpURL},
		{"all_proxy", socksURL},
		{"ALL_PROXY", socksURL},
		{"no_proxy", defaultNoProxy},
		{"NO_PROXY", defaultNoProxy},
	}

	lines := make([]string, 0, len(vars)+2)
	lines = append(lines, BlockStart)
	for _, kv := range vars {
		lines = append(lines, formatEnvLine(shell, kv.key, kv.val))
	}
	lines = append(lines, BlockEnd)
	return strings.Join(lines, "\n")
}

func formatEnvLine(shell, key, val string) string {
	switch shell {
	case "fish":
		return fmt.Sprintf("set -gx %s %s", key, quoteFish(val))
	case "csh", "tcsh":
		return fmt.Sprintf("setenv %s %s", key, quoteCsh(val))
	case "nushell":
		return fmt.Sprintf("$env.%s = %s", key, quoteNushell(val))
	case "elvish":
		return fmt.Sprintf("set E:%s = %s", key, quoteSingleDoubled(val))
	case "pwsh":
		return fmt.Sprintf("$env:%s = %s", key, quoteSingleDoubled(val))
	default:
		return fmt.Sprintf("export %s=%s", key, quotePOSIX(val))
	}
}

func quotePOSIX(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func quoteFish(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, "'", `\'`)
	return "'" + escaped + "'"
}

func quoteCsh(s string) string {
	// In csh/tcsh, '!' is expanded for history even inside single quotes.
	escaped := strings.ReplaceAll(s, "'", `'\''`)
	escaped = strings.ReplaceAll(escaped, "!", `\!`)
	return "'" + escaped + "'"
}

func quoteNushell(s string) string {
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func quoteSingleDoubled(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

var proxyLinePatterns = []*regexp.Regexp{
	// POSIX: export/declare/typeset/bare assignment, including after ; & | then else do
	regexp.MustCompile(`(?i)(?:^|(?:then|else|do)\s+|[;&|]\s*)(?:(?:export|declare|typeset)\s+(?:-[a-zA-Z]+\s+)*)?(?:http|https|all|ftp|no|socks|rsync)_proxy\s*=`),
	// Fish: set -gx http_proxy ...
	regexp.MustCompile(`(?i)^\s*set\s+(?:(?:-[a-zA-Z]+|--[a-zA-Z-]+)\s+)*(?:http|https|all|ftp|no|socks|rsync)_proxy(?:\s|$)`),
	// Csh / Tcsh: setenv http_proxy ...
	regexp.MustCompile(`(?i)^\s*setenv\s+(?:http|https|all|ftp|no|socks|rsync)_proxy(?:\s|$)`),
	// Nushell: $env.http_proxy = ...
	regexp.MustCompile(`(?i)\$env\.(?:http|https|all|ftp|no|socks|rsync)_proxy\s*=`),
	// PowerShell: $env:http_proxy = ...
	regexp.MustCompile(`(?i)\$env:(?:http|https|all|ftp|no|socks|rsync)_proxy\s*=`),
	// Elvish: set E:http_proxy = ...
	regexp.MustCompile(`(?i)\bE:(?:http|https|all|ftp|no|socks|rsync)_proxy\s*=`),
}

func isProxyConfigLine(trimmed string) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	for _, re := range proxyLinePatterns {
		if re.MatchString(trimmed) {
			return true
		}
	}
	return false
}

// ScanExisting parses a shell config file's text and returns all active proxy
// configuration lines along with whether a WebClash managed block exists and
// its exact text.
func ScanExisting(content string) (entries []ExistingEntry, hasManaged bool, managedBlock string) {
	entries = []ExistingEntry{}
	if content == "" {
		return entries, false, ""
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	// Drop a single trailing empty element produced by a final newline so line
	// counts match editors.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	inManaged := false
	startIdx := -1
	endIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == BlockStart {
			if startIdx == -1 {
				startIdx = i
			}
			inManaged = true
			continue
		}
		if trimmed == BlockEnd && inManaged {
			endIdx = i
			inManaged = false
			continue
		}
		if isProxyConfigLine(trimmed) {
			entries = append(entries, ExistingEntry{
				Line:    i + 1,
				Content: line,
				Managed: inManaged,
			})
		}
	}
	if startIdx >= 0 && endIdx > startIdx {
		hasManaged = true
		managedBlock = strings.Join(lines[startIdx:endIdx+1], "\n")
	}
	return entries, hasManaged, managedBlock
}

var proxyEnvVarNames = []string{
	"http_proxy",
	"https_proxy",
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"all_proxy",
	"ALL_PROXY",
	"ftp_proxy",
	"FTP_PROXY",
	"socks_proxy",
	"SOCKS_PROXY",
	"no_proxy",
	"NO_PROXY",
}

// ScanEnvProxies inspects environment variables (or env.Environ when EnvironSet)
// for active proxy settings.
func ScanEnvProxies(env Env) []EnvProxyEntry {
	var pairs []string
	if env.EnvironSet {
		pairs = env.Environ
	} else {
		pairs = os.Environ()
	}

	values := make(map[string]string, len(pairs))
	for _, kv := range pairs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			values[k] = trimmed
		}
	}

	out := []EnvProxyEntry{}
	for _, name := range proxyEnvVarNames {
		if val, ok := values[name]; ok {
			out = append(out, EnvProxyEntry{Name: name, Value: val})
		}
	}
	return out
}

func resolveEnvDirs(env Env) (home, zdotdir, xdgBase string) {
	home = strings.TrimSpace(env.HomeDir)
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			home = h
		} else {
			home = os.Getenv("HOME")
		}
	}
	zdotdir = env.Zdotdir
	if !env.ZdotdirSet && zdotdir == "" {
		zdotdir = os.Getenv("ZDOTDIR")
	}
	zdotdir = strings.TrimSpace(zdotdir)

	xdgConfig := env.XDGConfig
	if !env.XDGSet && xdgConfig == "" {
		xdgConfig = os.Getenv("XDG_CONFIG_HOME")
	}
	xdgBase = strings.TrimSpace(xdgConfig)
	if xdgBase == "" && home != "" {
		xdgBase = filepath.Join(home, ".config")
	}
	return home, zdotdir, xdgBase
}

func candidateConfigFiles(env Env, targetPath string) []string {
	home, zdotdir, xdgBase := resolveEnvDirs(env)

	raw := make([]string, 0, 20)
	if targetPath != "" {
		raw = append(raw, targetPath)
	}
	if home != "" {
		raw = append(raw,
			filepath.Join(home, ".bashrc"),
			filepath.Join(home, ".bash_profile"),
			filepath.Join(home, ".bash_login"),
			filepath.Join(home, ".zshrc"),
			filepath.Join(home, ".zprofile"),
			filepath.Join(home, ".zshenv"),
			filepath.Join(home, ".profile"),
			filepath.Join(home, ".tcshrc"),
			filepath.Join(home, ".cshrc"),
			filepath.Join(home, ".kshrc"),
		)
	}
	if zdotdir != "" && zdotdir != home {
		raw = append(raw,
			filepath.Join(zdotdir, ".zshrc"),
			filepath.Join(zdotdir, ".zprofile"),
			filepath.Join(zdotdir, ".zshenv"),
		)
	}
	if xdgBase != "" {
		raw = append(raw,
			filepath.Join(xdgBase, "fish", "config.fish"),
			filepath.Join(xdgBase, "nushell", "env.nu"),
			filepath.Join(xdgBase, "nushell", "config.nu"),
			filepath.Join(xdgBase, "elvish", "rc.elv"),
			filepath.Join(xdgBase, "powershell", "Microsoft.PowerShell_profile.ps1"),
		)
	}
	if !env.SkipSystemFiles {
		raw = append(raw, "/etc/environment", "/etc/profile")
	}

	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		key := clean
		if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != "" {
			key = resolved
		}
		if seen[clean] || seen[key] {
			continue
		}
		seen[clean] = true
		seen[key] = true
		out = append(out, clean)
	}
	return out
}

// BuildPreview inspects shellInfo.ConfigPath, other common shell/system config
// files, and current environment variables, and builds a complete preview of
// what Apply would write alongside any pre-existing proxy entries.
func BuildPreview(env Env, shellInfo ShellInfo, spec ProxySpec) (Preview, error) {
	block := RenderBlock(shellInfo.Shell, spec, false)
	maskedBlock := RenderBlock(shellInfo.Shell, spec, true)
	shellInfo.FileExists = util.Exists(shellInfo.ConfigPath)

	envProxies := ScanEnvProxies(env)
	preview := Preview{
		Shell:         shellInfo,
		HTTPProxy:     BuildProxyURL("http", spec.HTTPPort, spec.Username, spec.Password, true),
		SOCKSProxy:    BuildProxyURL("socks5", spec.SOCKSPort, spec.Username, spec.Password, true),
		HasAuth:       spec.Username != "",
		AuthUsername:  spec.Username,
		Block:         block,
		MaskedBlock:   maskedBlock,
		Existing:      []ExistingEntry{},
		EnvProxies:    envProxies,
		HasEnvProxy:   len(envProxies) > 0,
		SourceCommand: SourceCommand(shellInfo.Shell, shellInfo.ConfigPath),
	}

	targetClean := filepath.Clean(shellInfo.ConfigPath)
	var targetErr error

	for _, path := range candidateConfigFiles(env, shellInfo.ConfigPath) {
		isTarget := path == targetClean
		raw, err := os.ReadFile(path)
		if err != nil {
			if isTarget && !os.IsNotExist(err) {
				targetErr = fmt.Errorf("无法读取 Shell 配置文件 %s：%w", shellInfo.ConfigPath, err)
			}
			continue
		}
		entries, hasManaged, managedBlock := ScanExisting(string(raw))
		for i := range entries {
			entries[i].File = path
		}
		preview.Existing = append(preview.Existing, entries...)
		if isTarget {
			preview.HasManagedBlock = hasManaged
			preview.ManagedBlockSynced = hasManaged && managedBlock == block
		}
	}

	for _, entry := range preview.Existing {
		if !entry.Managed {
			preview.HasExternalProxy = true
			break
		}
	}
	return preview, targetErr
}

// UpsertBlock replaces an existing WebClash managed block in place, or appends
// it cleanly at the end of content.
func UpsertBlock(content, block string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if strings.TrimSpace(normalized) == "" {
		return block + "\n"
	}

	lines := splitLinesTrimTrailing(normalized)
	startIdx, endIdx := findBlockRange(lines)
	blockLines := strings.Split(block, "\n")

	if startIdx >= 0 && endIdx > startIdx {
		out := make([]string, 0, len(lines)-(endIdx-startIdx+1)+len(blockLines))
		out = append(out, lines[:startIdx]...)
		out = append(out, blockLines...)
		out = append(out, lines[endIdx+1:]...)
		return strings.Join(out, "\n") + "\n"
	}

	// Trim excess trailing blank lines before appending with a single blank line separator.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	out := make([]string, 0, len(lines)+1+len(blockLines))
	out = append(out, lines...)
	out = append(out, "")
	out = append(out, blockLines...)
	return strings.Join(out, "\n") + "\n"
}

// RemoveBlock removes the WebClash managed block from content, returning the
// updated content and whether a block was actually removed.
func RemoveBlock(content string) (string, bool) {
	if content == "" {
		return "", false
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := splitLinesTrimTrailing(normalized)
	startIdx, endIdx := findBlockRange(lines)
	if startIdx < 0 || endIdx <= startIdx {
		return content, false
	}

	before := append([]string{}, lines[:startIdx]...)
	after := append([]string{}, lines[endIdx+1:]...)

	// Remove the single blank separator line that UpsertBlock inserts before the block.
	if len(before) > 0 && strings.TrimSpace(before[len(before)-1]) == "" {
		before = before[:len(before)-1]
	} else if len(before) == 0 && len(after) > 0 && strings.TrimSpace(after[0]) == "" {
		after = after[1:]
	}

	remaining := append(before, after...)
	allBlank := true
	for _, l := range remaining {
		if strings.TrimSpace(l) != "" {
			allBlank = false
			break
		}
	}
	if allBlank {
		return "", true
	}
	return strings.Join(remaining, "\n") + "\n", true
}

func splitLinesTrimTrailing(normalized string) []string {
	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func findBlockRange(lines []string) (int, int) {
	startIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == BlockStart && startIdx == -1 {
			startIdx = i
			continue
		}
		if trimmed == BlockEnd && startIdx >= 0 {
			return startIdx, i
		}
	}
	return -1, -1
}

// Apply writes or updates the WebClash managed proxy block in shellInfo.ConfigPath.
// It returns changed=false when the file already contains the exact block.
func Apply(shellInfo ShellInfo, spec ProxySpec) (changed bool, err error) {
	if strings.TrimSpace(shellInfo.ConfigPath) == "" {
		return false, fmt.Errorf("未解析到有效的 Shell 配置文件路径")
	}
	block := RenderBlock(shellInfo.Shell, spec, false)

	var current string
	raw, err := os.ReadFile(shellInfo.ConfigPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("无法读取 Shell 配置文件 %s：%w", shellInfo.ConfigPath, err)
		}
	} else {
		current = string(raw)
	}

	next := UpsertBlock(current, block)
	if err == nil && next == current {
		return false, nil
	}

	defaultMode := os.FileMode(0o644)
	if spec.Username != "" {
		defaultMode = 0o600
	}
	if err := writeFilePreservingSymlink(shellInfo.ConfigPath, next, defaultMode); err != nil {
		return false, fmt.Errorf("无法写入 Shell 配置文件 %s：%w", shellInfo.ConfigPath, err)
	}
	return true, nil
}

// Remove deletes the WebClash managed proxy block from configPath if present.
func Remove(configPath string) (removed bool, err error) {
	if strings.TrimSpace(configPath) == "" {
		return false, nil
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("无法读取 Shell 配置文件 %s：%w", configPath, err)
	}
	next, found := RemoveBlock(string(raw))
	if !found {
		return false, nil
	}
	if err := writeFilePreservingSymlink(configPath, next, 0o644); err != nil {
		return false, fmt.Errorf("无法更新 Shell 配置文件 %s：%w", configPath, err)
	}
	return true, nil
}

func writeFilePreservingSymlink(path, data string, defaultMode os.FileMode) error {
	target := path
	if linfo, err := os.Lstat(path); err == nil && linfo.Mode()&os.ModeSymlink != 0 {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			target = resolved
		} else if linkDest, rerr := os.Readlink(path); rerr == nil {
			if filepath.IsAbs(linkDest) {
				target = linkDest
			} else {
				target = filepath.Join(filepath.Dir(path), linkDest)
			}
		}
	}

	mode := defaultMode
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}

	if err := util.EnsureDir(filepath.Dir(target)); err != nil {
		return err
	}
	return util.WriteFileAtomic(target, data, util.FileMode(mode))
}
