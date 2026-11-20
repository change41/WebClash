package service

import (
	"bytes"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type cmdResult struct {
	status int
	stdout string
	stderr string
}

// runCmd ports cmd (spawnSync). A spawn failure (e.g. ENOENT) maps to status 1,
// matching Node's `res.status ?? 1`.
func runCmd(file string, args []string) cmdResult {
	c := exec.Command(file, args...)
	var out, errb bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errb
	err := c.Run()
	status := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			status = ee.ExitCode()
		} else {
			status = 1
		}
	}
	return cmdResult{status: status, stdout: out.String(), stderr: errb.String()}
}

// ServiceAction ports serviceAction (start/stop/restart).
func ServiceAction(o ServiceOptions, action string, dryRun bool) ([]StepResult, error) {
	var file string
	var args []string
	if o.Manager == "systemd" {
		file, args = "systemctl", []string{action, o.Name}
	} else {
		file, args = "rc-service", []string{o.Name, action}
	}
	joined := file + " " + strings.Join(args, " ")
	if dryRun {
		return []StepResult{{Step: "执行命令", Action: "would-run", Detail: joined}}, nil
	}
	res := runCmd(file, args)
	if res.status != 0 {
		detail := strings.TrimSpace(res.stderr)
		if detail == "" {
			detail = strings.TrimSpace(res.stdout)
		}
		return nil, fmt.Errorf("%s 失败: %s", joined, detail)
	}
	return []StepResult{{Step: "执行命令", Action: "run", Detail: joined}}, nil
}

// ServiceStatus ports ServiceStatus.
type ServiceStatus struct {
	Installed bool               `json:"installed"`
	Manager   ServiceManagerKind `json:"manager"`
	Active    bool               `json:"active"`
	Healthy   bool               `json:"healthy"`
}

// GetServiceStatus ports serviceStatus.
func GetServiceStatus(o ServiceOptions) ServiceStatus {
	installed := false
	switch o.Manager {
	case "systemd":
		installed = exists(UnitPath(o))
	case "openrc":
		installed = exists(InitPath(o))
	}
	active := false
	if installed && o.Manager == "systemd" {
		active = strings.TrimSpace(runCmd("systemctl", []string{"is-active", o.Name}).stdout) == "active"
	} else if installed && o.Manager == "openrc" {
		active = runCmd("rc-service", []string{o.Name, "status"}).status == 0
	}
	healthy := false
	if active {
		client := &http.Client{Timeout: 2 * time.Second}
		url := fmt.Sprintf("http://127.0.0.1:%d/api/health", o.Port)
		if res, err := client.Get(url); err == nil {
			healthy = res.StatusCode >= 200 && res.StatusCode < 300
			_ = res.Body.Close()
		}
	}
	return ServiceStatus{Installed: installed, Manager: o.Manager, Active: active, Healthy: healthy}
}
