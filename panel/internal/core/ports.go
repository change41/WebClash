package core

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"webclash/internal/config"
	"webclash/internal/netutil"
	"webclash/internal/util"
)

const kernelPortRetries = 50

func splitListen(listen string) (string, int, bool) {
	host, rawPort, err := net.SplitHostPort(listen)
	if err != nil {
		return "", 0, false
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

func kernelPortSet(s config.Settings) map[int]bool {
	ports := map[int]bool{}
	add := func(p int) {
		if p > 0 {
			ports[p] = true
		}
	}
	add(s.Ports.Mixed)
	add(s.Ports.HTTP)
	add(s.Ports.SOCKS)
	add(s.Controller.Port)
	if s.Dns.Enable {
		if _, p, ok := splitListen(s.Dns.Listen); ok {
			add(p)
		}
	}
	return ports
}

// ReservedKernelPorts returns the ports mihomo will bind, so the WebUI listener
// can avoid picking one of them when it falls back.
func (c *Core) ReservedKernelPorts() map[int]bool {
	return kernelPortSet(c.Settings())
}

// SetServerPort records a WebUI port chosen at runtime and persists it.
func (c *Core) SetServerPort(port int) error {
	c.mu.Lock()
	c.settings.Server.Port = port
	c.mu.Unlock()
	return c.persistSettings()
}

// resolveKernelPorts probes kernel inbound ports before the runtime config is
// written. Any port already bound by another process is moved to the next free
// one and persisted, so the kernel, UI and clients agree on the new port.
func (c *Core) resolveKernelPorts() {
	before := c.Settings()
	after, changes := planKernelPorts(before, netutil.TCPFree, netutil.UDPFree)
	if len(changes) == 0 {
		return
	}
	c.mu.Lock()
	c.settings.Ports = after.Ports
	c.settings.Controller.Port = after.Controller.Port
	c.settings.Dns.Listen = after.Dns.Listen
	c.mu.Unlock()
	for _, change := range changes {
		fmt.Printf("[webclash] %s\n", change)
	}
	if err := c.persistSettings(); err != nil {
		fmt.Fprintf(os.Stderr, "[webclash] 保存端口调整失败：%s\n", util.ErrorMessage(err))
	}
}

func planKernelPorts(s config.Settings, tcpFree, udpFree func(host string, port int) bool) (config.Settings, []string) {
	out := s
	reserved := kernelPortSet(s)
	used := map[int]string{}
	if s.Server.Port > 0 {
		reserved[s.Server.Port] = true
		used[s.Server.Port] = "WebUI"
	}
	var changes []string

	move := func(label, host string, current int, free func(string, int) bool, reason string) int {
		next, ok := netutil.FindFree(current+1, kernelPortRetries, reserved, func(port int) bool {
			if _, taken := used[port]; taken {
				return false
			}
			return free(host, port)
		})
		if !ok {
			changes = append(changes, fmt.Sprintf("%s端口 %d %s，且未找到可用替代端口", label, current, reason))
			if _, taken := used[current]; !taken {
				used[current] = label
			}
			return current
		}
		reserved[next] = true
		used[next] = label
		changes = append(changes, fmt.Sprintf("%s端口 %d %s，改用 %d", label, current, reason, next))
		return next
	}

	pick := func(label, host string, current int, enabled bool, free func(string, int) bool) int {
		if !enabled || current <= 0 {
			return current
		}
		if owner, taken := used[current]; taken {
			return move(label, host, current, free, fmt.Sprintf("与 %s 端口冲突", owner))
		}
		if free(host, current) {
			used[current] = label
			return current
		}
		return move(label, host, current, free, "已被占用")
	}

	out.Ports.Mixed = pick("内核 mixed", "0.0.0.0", s.Ports.Mixed, true, tcpFree)
	out.Ports.HTTP = pick("内核 HTTP", "0.0.0.0", s.Ports.HTTP, s.Ports.HTTP > 0, tcpFree)
	out.Ports.SOCKS = pick("内核 SOCKS", "0.0.0.0", s.Ports.SOCKS, s.Ports.SOCKS > 0, tcpFree)
	out.Controller.Port = pick("内核控制器", "127.0.0.1", s.Controller.Port, true, tcpFree)
	if s.Dns.Enable {
		if host, port, ok := splitListen(s.Dns.Listen); ok {
			next := pick("DNS", host, port, true, func(h string, p int) bool {
				return tcpFree(h, p) && udpFree(h, p)
			})
			if next != port {
				out.Dns.Listen = net.JoinHostPort(host, strconv.Itoa(next))
			}
		}
	}
	return out, changes
}
