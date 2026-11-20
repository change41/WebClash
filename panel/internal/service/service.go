// Package service ports server/src/service.ts: service-manager detection and
// systemd/OpenRC installation. Templates live in templates.go.
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"webclash/internal/paths"
)

// ServiceManagerKind is "systemd", "openrc" or "none".
type ServiceManagerKind = string

// ServiceOptions is a fully-resolved service configuration.
type ServiceOptions struct {
	Name    string
	Bin     string
	DataDir string
	Port    int
	Host    string
	User    string
	Tun     bool
	Manager ServiceManagerKind
}

// StepResult is one install/uninstall action (port of StepResult).
type StepResult struct {
	Step   string `json:"step"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}

// ServiceInput carries optional resolveService inputs (zero value = unset).
type ServiceInput struct {
	Name    string
	Bin     string
	DataDir string
	Port    int
	Host    string
	User    string
	Tun     bool
	Manager ServiceManagerKind
}

// IsRoot reports whether the process runs as uid 0.
func IsRoot() bool { return os.Getuid() == 0 }

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func whichSync(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// DetectServiceManager probes systemd then OpenRC (port of detectServiceManager).
func DetectServiceManager() ServiceManagerKind {
	if runtime.GOOS != "linux" {
		return "none"
	}
	proc1 := ""
	if b, err := os.ReadFile("/proc/1/comm"); err == nil {
		proc1 = strings.TrimSpace(string(b))
	}
	if exists("/run/systemd/system") || proc1 == "systemd" {
		return "systemd"
	}
	if exists("/sbin/openrc") || exists("/run/openrc/softlevel") || whichSync("rc-service") {
		return "openrc"
	}
	return "none"
}

// UnitPath / EnvPath / InitPath / ConfPath / LogPath mirror service.ts.
func UnitPath(o ServiceOptions) string { return "/etc/systemd/system/" + o.Name + ".service" }
func EnvPath(o ServiceOptions) string  { return "/etc/webclash/" + o.Name + ".env" }
func InitPath(o ServiceOptions) string { return "/etc/init.d/" + o.Name }
func ConfPath(o ServiceOptions) string { return "/etc/conf.d/" + o.Name }
func LogPath(o ServiceOptions) string  { return "/var/log/" + o.Name + ".log" }

func capabilities(o ServiceOptions) string {
	if o.User == "root" || o.Tun {
		return "CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE"
	}
	return "CAP_NET_BIND_SERVICE"
}

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func absResolve(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// ResolveService fills defaults and detects the manager (port of resolveService).
func ResolveService(in ServiceInput) (ServiceOptions, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "webclash"
	}
	if !validNameRe.MatchString(name) {
		return ServiceOptions{}, fmt.Errorf("非法服务名: %s", name)
	}
	bin := strings.TrimSpace(in.Bin)
	if bin == "" {
		bin = "/usr/local/bin/" + name
	}
	dataDir := strings.TrimSpace(in.DataDir)
	if dataDir == "" {
		dataDir = "/var/lib/webclash"
	}
	port := in.Port
	if port == 0 {
		port = 7788
	}
	host := strings.TrimSpace(in.Host)
	if host == "" {
		host = "0.0.0.0"
	}
	user := strings.TrimSpace(in.User)
	if user == "" {
		user = "root"
	}
	manager := in.Manager
	if manager == "" || manager == "none" {
		manager = DetectServiceManager()
	}
	return ServiceOptions{
		Name:    name,
		Bin:     absResolve(paths.ExpandHome(bin)),
		DataDir: absResolve(paths.ExpandHome(dataDir)),
		Port:    port,
		Host:    host,
		User:    user,
		Tun:     in.Tun,
		Manager: manager,
	}, nil
}
