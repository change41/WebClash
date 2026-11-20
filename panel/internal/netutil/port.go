// Package netutil probes and binds local TCP/UDP ports so startup can move to
// the next free port instead of failing on a conflict.
package netutil

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
)

// TCPFree reports whether host:port can be bound right now. The probe socket is
// closed immediately, so another process may still grab the port afterwards.
func TCPFree(host string, port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// UDPFree reports whether host:port can be bound for UDP right now.
func UDPFree(host string, port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	pc, err := net.ListenPacket("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = pc.Close()
	return true
}

// FindFree returns the first port in [start, start+maxOffset] (capped at 65535)
// that is not reserved and for which free reports true.
func FindFree(start, maxOffset int, reserved map[int]bool, free func(port int) bool) (int, bool) {
	for port := start; port <= start+maxOffset && port <= 65535; port++ {
		if port <= 0 || reserved[port] {
			continue
		}
		if free(port) {
			return port, true
		}
	}
	return 0, false
}

// ListenTCP binds host:start and, while the address is already in use, advances
// one port at a time (up to maxOffset attempts), skipping reserved ports. It
// returns the live listener and the port actually bound.
func ListenTCP(host string, start, maxOffset int, reserved map[int]bool) (net.Listener, int, error) {
	var lastErr error
	for port := start; port <= start+maxOffset && port <= 65535; port++ {
		if port <= 0 || reserved[port] {
			continue
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			bound := port
			if addr, ok := ln.Addr().(*net.TCPAddr); ok {
				bound = addr.Port
			}
			return ln, bound, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, 0, err
		}
		lastErr = err
	}
	if lastErr == nil {
		return nil, 0, fmt.Errorf("端口 %d 不可用，且未找到可替代的监听端口", start)
	}
	return nil, 0, lastErr
}
