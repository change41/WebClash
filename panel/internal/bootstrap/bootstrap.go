// Package bootstrap ports server/src/bootstrap.ts: startup banner, HTTP listen,
// console logs and graceful SIGINT/SIGTERM shutdown.
package bootstrap

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"webclash/internal/api"
	"webclash/internal/core"
	"webclash/internal/netutil"
	"webclash/internal/paths"
	"webclash/internal/util"
)

const portFallbackTries = 50

// StartServer runs the panel until SIGINT/SIGTERM, then shuts down cleanly.
// It returns nil on graceful exit (process exit code 0).
func StartServer() error {
	fmt.Printf("\n%s v%s 正在启动...\n", util.AppName, util.AppVersion())

	c := core.New(paths.DefaultDataDir())
	if err := c.Init(); err != nil {
		return err
	}
	settings := c.Settings()

	port := settings.Server.Port
	if v := os.Getenv("WEBUI_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			port = n
		}
	}
	host := settings.Server.Host
	if v := os.Getenv("WEBUI_HOST"); v != "" {
		host = v
	}

	apiSrv := api.New(c)
	ln, actualPort, err := netutil.ListenTCP(host, port, portFallbackTries, c.ReservedKernelPorts())
	if err != nil {
		return err
	}
	if actualPort != port {
		fmt.Printf("[webclash] WebUI 端口 %d 已被占用，改用 %d\n", port, actualPort)
		if os.Getenv("WEBUI_PORT") == "" {
			if err := c.SetServerPort(actualPort); err != nil {
				fmt.Fprintf(os.Stderr, "[webclash] 保存 WebUI 端口失败：%s\n", util.ErrorMessage(err))
			}
		}
		port = actualPort
	}
	srv := &http.Server{
		Addr:        net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:     apiSrv,
		ReadTimeout: 180 * time.Second,
	}

	shownHost := host
	if host == "0.0.0.0" || host == "::" {
		shownHost = "localhost"
	}
	fmt.Printf("%s 已启动: http://%s:%d\n", util.AppName, shownHost, port)
	fmt.Printf("数据目录: %s\n", c.DataDir)
	fmt.Printf("内核配置: %s\n", c.ConfigPath)
	fmt.Printf("控制器: 127.0.0.1:%d（仅本机可访问）\n", settings.Controller.Port)
	if paths.Embedded() {
		fmt.Printf("可执行文件: %s\n", paths.ExecutablePath())
	}

	var overrides []string
	if v := os.Getenv("WEBUI_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n != settings.Server.Port {
			overrides = append(overrides, "WEBUI_PORT="+v)
		}
	}
	if v := os.Getenv("WEBUI_HOST"); v != "" && v != settings.Server.Host {
		overrides = append(overrides, "WEBUI_HOST="+v)
	}
	if len(overrides) > 0 {
		fmt.Printf("环境变量覆盖设置: %s\n", strings.Join(overrides, ", "))
	}

	if pw := c.InitialPassword(); pw != nil {
		fmt.Println("\n============================================================")
		fmt.Println(" 首次启动，已生成 WebUI 管理密码：")
		fmt.Printf("    %s\n", *pw)
		fmt.Printf(" 密码同时保存在: %s\n", c.InitialPasswordFile)
		fmt.Println(" 登录后请尽快在「设置 - 安全」中修改。")
		fmt.Print("============================================================\n\n")
	} else {
		fmt.Println("提示: 若忘记密码，可删除 data/settings.json 中的 auth.passwordHash 后重启以重置。")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	case sig := <-sigCh:
		name := "SIGTERM"
		if sig == syscall.SIGINT {
			name = "SIGINT"
		}
		fmt.Printf("\n收到 %s，正在停止...\n", name)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "停止时出错：%s\n", util.ErrorMessage(err))
		}
		apiSrv.CloseWebSockets()
		if err := c.Shutdown(); err != nil {
			fmt.Fprintf(os.Stderr, "停止时出错：%s\n", util.ErrorMessage(err))
		}
		return nil
	}
}
