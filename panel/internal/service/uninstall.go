package service

import (
	"os"
	"path/filepath"
	"strings"
)

// UninstallFlags ports UninstallFlags.
type UninstallFlags struct {
	DryRun    bool
	RemoveBin bool
	Purge     bool
}

// UninstallService ports uninstallService.
func UninstallService(o ServiceOptions, flags UninstallFlags) ([]StepResult, error) {
	var steps []StepResult
	dry := flags.DryRun
	push := func(step, action, detail string) {
		steps = append(steps, StepResult{Step: step, Action: action, Detail: detail})
	}
	remove := func(file, label string) error {
		if !exists(file) {
			push(label, "skip", file+" 不存在")
			return nil
		}
		if dry {
			push(label, "would-delete", file)
			return nil
		}
		if err := os.Remove(file); err != nil {
			return err
		}
		push(label, "delete", file)
		return nil
	}
	runIgnore := func(file string, args []string) {
		joined := file + " " + strings.Join(args, " ")
		if dry {
			push("执行命令", "would-run", joined)
			return
		}
		runCmd(file, args)
		push("执行命令", "run", joined+"（忽略失败）")
	}
	removeBackups := func(dir, prefix, label string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), prefix) {
				if err := remove(filepath.Join(dir, entry.Name()), label); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if o.Manager == "systemd" {
		runIgnore("systemctl", []string{"disable", "--now", o.Name})
		if err := remove(UnitPath(o), "systemd unit"); err != nil {
			return nil, err
		}
		if err := removeBackups(filepath.Dir(UnitPath(o)), o.Name+".service.bak.", "unit 备份"); err != nil {
			return nil, err
		}
		runIgnore("systemctl", []string{"daemon-reload"})
		runIgnore("systemctl", []string{"reset-failed", o.Name})
	} else if o.Manager == "openrc" {
		runIgnore("rc-service", []string{o.Name, "stop"})
		runIgnore("rc-update", []string{"del", o.Name})
		if err := remove(InitPath(o), "OpenRC init"); err != nil {
			return nil, err
		}
		if err := removeBackups(filepath.Dir(InitPath(o)), o.Name+".bak.", "init 备份"); err != nil {
			return nil, err
		}
		if err := remove(ConfPath(o), "OpenRC 配置"); err != nil {
			return nil, err
		}
	}

	if flags.RemoveBin || flags.Purge {
		if err := remove(o.Bin, "可执行文件"); err != nil {
			return nil, err
		}
	}
	if flags.Purge {
		if o.Manager == "systemd" {
			if err := remove(EnvPath(o), "环境变量文件"); err != nil {
				return nil, err
			}
		}
		if dry {
			push("数据目录", "would-delete", o.DataDir)
		} else {
			if err := os.RemoveAll(o.DataDir); err != nil {
				return nil, err
			}
			push("数据目录", "delete", o.DataDir)
		}
	} else {
		push("数据目录", "skip", "保留 "+o.DataDir)
		if o.Manager == "systemd" {
			push("环境变量文件", "skip", "保留 "+EnvPath(o))
		}
	}
	return steps, nil
}
