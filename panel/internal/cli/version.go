package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"webclash/internal/paths"
	"webclash/internal/service"
	"webclash/internal/util"
)

// versionInfo is the JSON shape of `webclash version --json`. The Go release
// reports runtime (Go toolchain) and static (always true) in place of the Node
// build's node/sea keys; embedded stays true.
type versionInfo struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	Runtime        string `json:"runtime"`
	Platform       string `json:"platform"`
	Arch           string `json:"arch"`
	Libc           string `json:"libc"`
	Embedded       bool   `json:"embedded"`
	Static         bool   `json:"static"`
	Executable     string `json:"executable"`
	DefaultDataDir string `json:"defaultDataDir"`
	ServiceManager string `json:"serviceManager"`
}

func archName() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

func detectLibc() string {
	if runtime.GOOS != "linux" {
		return "n/a"
	}
	if m, _ := filepath.Glob("/lib/ld-musl-*.so.1"); len(m) > 0 {
		return "musl"
	}
	if out, err := exec.Command("getconf", "GNU_LIBC_VERSION").Output(); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return s
		}
	}
	for _, p := range []string{
		"/lib/x86_64-linux-gnu/libc.so.6",
		"/lib/aarch64-linux-gnu/libc.so.6",
		"/lib64/libc.so.6",
		"/lib/libc.so.6",
	} {
		if _, err := os.Stat(p); err == nil {
			return "glibc"
		}
	}
	return "musl"
}

func buildVersionInfo() versionInfo {
	return versionInfo{
		Name:           "webclash",
		Version:        util.AppVersion(),
		Runtime:        runtime.Version(),
		Platform:       runtime.GOOS,
		Arch:           archName(),
		Libc:           detectLibc(),
		Embedded:       paths.Embedded(),
		Static:         true,
		Executable:     paths.ExecutablePath(),
		DefaultDataDir: paths.DefaultDataDir(),
		ServiceManager: service.DetectServiceManager(),
	}
}

func printVersion(jsonOut bool) {
	info := buildVersionInfo()
	if jsonOut {
		b, _ := util.MarshalJSONIndent(info)
		fmt.Println(string(b))
		return
	}
	form := "源码"
	if info.Embedded {
		form = "内嵌单文件"
	}
	fmt.Printf("%s v%s（%s, %s/%s, %s, %s）\n",
		util.AppName, info.Version, info.Runtime, info.Platform, info.Arch, info.Libc, form)
}

func formatHelp() string {
	return util.AppName + " v" + util.AppVersion() + ` — 基于 mihomo 内核的 Web 代理管理面板

用法:
  webclash [run] [选项]          前台运行（默认命令）
  webclash <命令> [选项]

命令:
  run                          前台运行面板与内核守护
  install                      安装为系统服务（systemd / OpenRC，需 root）
  uninstall                    卸载服务（默认保留数据）
  start | stop | restart       控制已安装的服务
  status                       查看服务状态（0 运行 / 3 未运行 / 4 未安装）
  password                     显示首次启动生成的管理密码
  version                      显示版本与运行时信息
  help                         显示本帮助

run 选项:
  -p, --port <端口>            WebUI 监听端口（默认取设置值 7788）
  -H, --host <地址>            WebUI 监听地址（默认取设置值 0.0.0.0）
  -d, --data-dir <目录>        数据目录（内嵌形态默认 ~/.webclash）
      --password <密码>        首次启动时使用的管理密码（仅首启生效）
      --web-dist <目录>        强制使用磁盘前端产物（调试用）

install 选项:
  -n, --name <名称>            服务名（默认 webclash）
      --service-manager <m>    auto | systemd | openrc（默认 auto 自动检测）
  -d, --data-dir <目录>        数据目录（默认 /var/lib/webclash）
  -p, --port <端口>            WebUI 端口（默认 7788）
  -H, --host <地址>            监听地址（默认 0.0.0.0）
      --bin <路径>             二进制安装路径（默认 /usr/local/bin/webclash）
      --no-copy                不复制二进制（直接使用当前路径）
      --user <用户>            运行用户（默认 root）
      --tun                    为非 root 用户保留 TUN 所需能力
      --force                  覆盖已有 unit/init 时不备份
      --no-start               安装后不立即启动

uninstall 选项:
      --remove-bin             同时删除已安装的二进制
      --purge                  同时删除数据目录与配置（必须配合 --yes）

全局选项:
      --json                   以 JSON 输出
      --dry-run                只打印将执行的动作，不落盘
  -y, --yes                    跳过确认（配合 --purge）
  -h, --help                   显示帮助
  -v, --version                显示版本

优先级: 命令行参数 > 环境变量 > settings.json > 内置默认
环境变量: WEBUI_PORT / WEBUI_HOST / DATA_DIR / WEBUI_PASSWORD

示例:
  webclash -p 7788 -d /var/lib/webclash
  sudo webclash install
  sudo webclash install --service-manager openrc --port 7788
  webclash status --json
  sudo webclash uninstall --purge --yes
`
}
