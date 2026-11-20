package service

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"webclash/internal/paths"
	"webclash/internal/util"
)

// InstallFlags ports InstallFlags.
type InstallFlags struct {
	DryRun  bool
	Force   bool
	NoCopy  bool
	NoStart bool
}

// readIfPresent returns the file text and whether it exists (null vs string in
// the Node version).
func readIfPresent(file string) (string, bool) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// CopySelfAtomic ports copySelfAtomic: copy the running executable to dest via
// a .new temp file, chmod 0755, then rename.
func CopySelfAtomic(dest string) error {
	src := paths.ExecutablePath()
	tmp := dest + ".new"
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// InstallService ports installService.
func InstallService(o ServiceOptions, flags InstallFlags) ([]StepResult, error) {
	var steps []StepResult
	dry := flags.DryRun
	push := func(step, action, detail string) {
		steps = append(steps, StepResult{Step: step, Action: action, Detail: detail})
	}

	var dirs []string
	if o.Manager == "systemd" {
		dirs = []string{o.DataDir, filepath.Dir(EnvPath(o)), filepath.Dir(UnitPath(o))}
	} else {
		dirs = []string{o.DataDir, filepath.Dir(InitPath(o)), filepath.Dir(ConfPath(o)), filepath.Dir(LogPath(o))}
	}
	for _, dir := range dirs {
		if dry {
			push("创建目录", "would-run", "mkdir -p "+dir)
		} else {
			if err := util.EnsureDir(dir); err != nil {
				return nil, err
			}
			push("创建目录", "run", dir)
		}
	}

	self := absResolve(paths.ExecutablePath())
	dest := absResolve(o.Bin)
	if flags.NoCopy || self == dest {
		detail := "--no-copy"
		if self == dest {
			detail = "已在目标路径"
		}
		push("安装二进制", "skip", detail)
	} else if dry {
		push("安装二进制", "would-run", fmt.Sprintf("cp %s %s.new && chmod 0755 && mv -f %s.new %s", self, dest, dest, dest))
	} else {
		if err := CopySelfAtomic(dest); err != nil {
			return nil, err
		}
		push("安装二进制", "write", dest)
	}

	if o.Manager == "systemd" {
		envFile := EnvPath(o)
		envText, _ := readIfPresent(envFile)
		envContent := RenderEnvFile(o, ParseEnvFile(envText))
		if envText == envContent {
			push("环境变量文件", "skip", envFile)
		} else if dry {
			push("环境变量文件", "would-write", envFile+"\n"+envContent)
		} else {
			if err := util.WriteFileAtomic(envFile, envContent, util.FileMode(0o640)); err != nil {
				return nil, err
			}
			push("环境变量文件", "write", envFile)
		}

		unit := UnitPath(o)
		unitContent := RenderSystemdUnit(o)
		prevUnit, unitPresent := readIfPresent(unit)
		if unitPresent && prevUnit == unitContent {
			push("systemd unit", "skip", unit)
		} else {
			if unitPresent && !flags.Force {
				backup := fmt.Sprintf("%s.bak.%d", unit, time.Now().UnixMilli())
				if dry {
					push("systemd unit", "would-run", fmt.Sprintf("备份 %s → %s", unit, backup))
				} else {
					if err := copyFile(unit, backup); err != nil {
						return nil, err
					}
					push("systemd unit", "backup", backup)
				}
			}
			if dry {
				push("systemd unit", "would-write", unit+"\n"+unitContent)
			} else {
				if err := util.WriteFileAtomic(unit, unitContent, util.FileMode(0o644)); err != nil {
					return nil, err
				}
				push("systemd unit", "write", unit)
			}
		}

		commands := [][]string{
			{"systemctl", "daemon-reload"},
			{"systemctl", "enable", o.Name},
		}
		if !flags.NoStart {
			commands = append(commands, []string{"systemctl", "restart", o.Name})
		}
		if err := runCommands(commands, dry, push); err != nil {
			return nil, err
		}
	} else if o.Manager == "openrc" {
		conf := ConfPath(o)
		confText, _ := readIfPresent(conf)
		confContent := RenderOpenRcConf(o, ParseEnvFile(confText))
		if confText == confContent {
			push("OpenRC 配置", "skip", conf)
		} else if dry {
			push("OpenRC 配置", "would-write", conf+"\n"+confContent)
		} else {
			if err := util.WriteFileAtomic(conf, confContent, util.FileMode(0o644)); err != nil {
				return nil, err
			}
			push("OpenRC 配置", "write", conf)
		}

		init := InitPath(o)
		initContent := RenderOpenRcInit(o)
		prevInit, initPresent := readIfPresent(init)
		if initPresent && prevInit == initContent {
			push("OpenRC init", "skip", init)
		} else {
			if initPresent && !flags.Force {
				backup := fmt.Sprintf("%s.bak.%d", init, time.Now().UnixMilli())
				if dry {
					push("OpenRC init", "would-run", fmt.Sprintf("备份 %s → %s", init, backup))
				} else {
					if err := copyFile(init, backup); err != nil {
						return nil, err
					}
					push("OpenRC init", "backup", backup)
				}
			}
			if dry {
				push("OpenRC init", "would-write", init+"\n"+initContent)
			} else {
				if err := util.WriteFileAtomic(init, initContent, util.FileMode(0o755)); err != nil {
					return nil, err
				}
				push("OpenRC init", "write", init)
			}
		}

		commands := [][]string{{"rc-update", "add", o.Name, "default"}}
		if !flags.NoStart {
			commands = append(commands, []string{"rc-service", o.Name, "restart"})
		}
		if err := runCommands(commands, dry, push); err != nil {
			return nil, err
		}
	}

	if !flags.NoStart && !dry {
		healthy := PollHealth(o.Port, 30_000)
		if healthy {
			push("健康检查", "run", fmt.Sprintf("http://127.0.0.1:%d/api/health ok", o.Port))
		} else {
			push("健康检查", "skip", "30 秒内未就绪，请查看服务日志")
		}
	}
	return steps, nil
}

func runCommands(commands [][]string, dry bool, push func(step, action, detail string)) error {
	for _, c := range commands {
		file, args := c[0], c[1:]
		joined := file + " " + strings.Join(args, " ")
		if dry {
			push("执行命令", "would-run", joined)
			continue
		}
		res := runCmd(file, args)
		if res.status != 0 {
			detail := strings.TrimSpace(res.stderr)
			if detail == "" {
				detail = strings.TrimSpace(res.stdout)
			}
			return fmt.Errorf("%s 失败: %s", joined, detail)
		}
		push("执行命令", "run", joined)
	}
	return nil
}

func copyFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	mode := os.FileMode(0o644)
	if err == nil {
		mode = info.Mode()
	}
	return os.WriteFile(dest, data, mode)
}

// PollHealth ports pollHealth: GET /api/health every second until ok or timeout.
func PollHealth(port int, timeoutMs int) bool {
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/health", port)
	for time.Now().Before(deadline) {
		res, err := client.Get(url)
		if err == nil {
			ok := res.StatusCode >= 200 && res.StatusCode < 300
			_ = res.Body.Close()
			if ok {
				return true
			}
		}
		time.Sleep(1 * time.Second)
	}
	return false
}
