package core

import (
	"encoding/json"
	"net"
	"os"
	"testing"

	"webclash/internal/config"
)

func portBusyProbe(busy map[int]bool) func(string, int) bool {
	return func(_ string, port int) bool { return !busy[port] }
}

func TestPlanKernelPortsMovesConflicts(t *testing.T) {
	s := config.DefaultSettings()
	busy := map[int]bool{7890: true, 9090: true}
	got, changes := planKernelPorts(s, portBusyProbe(busy), func(string, int) bool { return true })

	if got.Ports.Mixed != 7891 {
		t.Errorf("mixed = %d, want 7891", got.Ports.Mixed)
	}
	if got.Controller.Port != 9091 {
		t.Errorf("controller = %d, want 9091", got.Controller.Port)
	}
	if got.Dns.Listen != s.Dns.Listen {
		t.Errorf("dns listen = %q, want unchanged %q", got.Dns.Listen, s.Dns.Listen)
	}
	if len(changes) != 2 {
		t.Errorf("changes = %v, want 2 entries", changes)
	}
	if s.Ports.Mixed != 7890 || s.Controller.Port != 9090 {
		t.Errorf("input settings mutated: %+v", s.Ports)
	}
}

func TestPlanKernelPortsSkipsReserved(t *testing.T) {
	s := config.DefaultSettings()
	s.Server.Port = 7891
	busy := map[int]bool{7890: true, 7891: true, 7892: true}
	got, _ := planKernelPorts(s, portBusyProbe(busy), func(string, int) bool { return true })
	if got.Ports.Mixed != 7893 {
		t.Errorf("mixed = %d, want 7893 (server/busy ports reserved)", got.Ports.Mixed)
	}
}

func TestPlanKernelPortsMovesDns(t *testing.T) {
	s := config.DefaultSettings()
	tcpBusy := map[int]bool{1053: true}
	udpBusy := map[int]bool{}
	got, _ := planKernelPorts(s, portBusyProbe(tcpBusy), portBusyProbe(udpBusy))
	if got.Dns.Listen != "0.0.0.0:1054" {
		t.Errorf("dns listen = %q, want 0.0.0.0:1054", got.Dns.Listen)
	}

	udpBusy[1054] = true
	got, _ = planKernelPorts(s, portBusyProbe(tcpBusy), portBusyProbe(udpBusy))
	if got.Dns.Listen != "0.0.0.0:1055" {
		t.Errorf("dns listen = %q, want 0.0.0.0:1055", got.Dns.Listen)
	}
}

func TestPlanKernelPortsDisabledDnsUntouched(t *testing.T) {
	s := config.DefaultSettings()
	s.Dns.Enable = false
	got, _ := planKernelPorts(s, func(string, int) bool { return false }, func(string, int) bool { return false })
	if got.Dns.Listen != s.Dns.Listen {
		t.Errorf("dns listen = %q, want unchanged %q", got.Dns.Listen, s.Dns.Listen)
	}
}

func TestReservedKernelPorts(t *testing.T) {
	c := New(t.TempDir())
	reserved := c.ReservedKernelPorts()
	for _, port := range []int{7890, 9090, 1053} {
		if !reserved[port] {
			t.Errorf("port %d not reserved: %v", port, reserved)
		}
	}
	if reserved[7891] || reserved[7892] {
		t.Errorf("disabled inbound ports should not be reserved: %v", reserved)
	}

	c.mu.Lock()
	c.settings.Dns.Enable = false
	c.settings.Ports.HTTP = 7891
	c.settings.Ports.SOCKS = 7892
	c.mu.Unlock()
	reserved = c.ReservedKernelPorts()
	if reserved[1053] {
		t.Error("disabled DNS port should not be reserved")
	}
	if !reserved[7891] || !reserved[7892] {
		t.Errorf("enabled standalone ports should be reserved: %v", reserved)
	}
}

func TestPlanKernelPortsMovesHTTPPort(t *testing.T) {
	s := config.DefaultSettings()
	s.Ports.HTTP = 7891
	busy := map[int]bool{7891: true}
	got, changes := planKernelPorts(s, portBusyProbe(busy), func(string, int) bool { return true })
	if got.Ports.HTTP != 7892 {
		t.Errorf("http port = %d, want 7892", got.Ports.HTTP)
	}
	if len(changes) != 1 {
		t.Errorf("changes = %v, want 1 entry", changes)
	}
}

func TestPlanKernelPortsResolvesDuplicatePorts(t *testing.T) {
	s := config.DefaultSettings()
	s.Ports.HTTP = 7891
	s.Ports.SOCKS = 7891
	got, changes := planKernelPorts(s, func(string, int) bool { return true }, func(string, int) bool { return true })
	if got.Ports.HTTP != 7891 {
		t.Errorf("http port = %d, want 7891", got.Ports.HTTP)
	}
	if got.Ports.SOCKS != 7892 {
		t.Errorf("socks port = %d, want 7892", got.Ports.SOCKS)
	}
	if len(changes) != 1 {
		t.Errorf("changes = %v, want 1 duplicate entry", changes)
	}
}

func TestResolveKernelPortsShiftsAndPersists(t *testing.T) {
	// Bind the wildcard so the probe on 0.0.0.0 sees the port as busy on macOS too.
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	busy := ln.Addr().(*net.TCPAddr).Port
	defer ln.Close()

	c := New(t.TempDir())
	c.mu.Lock()
	c.settings.Ports.Mixed = busy
	c.settings.Dns.Enable = false
	c.mu.Unlock()

	c.resolveKernelPorts()

	if mixed := c.Settings().Ports.Mixed; mixed == busy || mixed <= busy {
		t.Fatalf("mixed = %d, want a free port above busy %d", mixed, busy)
	}

	raw, err := os.ReadFile(c.SettingsPath)
	if err != nil {
		t.Fatalf("settings.json: %v", err)
	}
	var persisted struct {
		Ports config.PortsSettings `json:"ports"`
	}
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("decode settings.json: %v", err)
	}
	if persisted.Ports.Mixed != c.Settings().Ports.Mixed {
		t.Errorf("persisted mixed = %d, want %d", persisted.Ports.Mixed, c.Settings().Ports.Mixed)
	}
}

func TestResolveKernelPortsPersistsStandalonePortShift(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	busy := ln.Addr().(*net.TCPAddr).Port
	defer ln.Close()

	c := New(t.TempDir())
	c.mu.Lock()
	c.settings.Dns.Enable = false
	c.settings.Ports.HTTP = busy
	c.mu.Unlock()

	c.resolveKernelPorts()

	got := c.Settings().Ports.HTTP
	if got == busy {
		t.Fatalf("http port = %d, want a free port", got)
	}
	raw, err := os.ReadFile(c.SettingsPath)
	if err != nil {
		t.Fatalf("settings.json: %v", err)
	}
	var persisted struct {
		Ports config.PortsSettings `json:"ports"`
	}
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("decode settings.json: %v", err)
	}
	if persisted.Ports.HTTP != got {
		t.Errorf("persisted http port = %d, want %d", persisted.Ports.HTTP, got)
	}
}

func TestSetServerPortPersists(t *testing.T) {
	c := New(t.TempDir())
	if err := c.SetServerPort(8123); err != nil {
		t.Fatalf("SetServerPort: %v", err)
	}
	raw, err := os.ReadFile(c.SettingsPath)
	if err != nil {
		t.Fatalf("settings.json: %v", err)
	}
	var persisted struct {
		Server config.ServerSettings `json:"server"`
	}
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("decode settings.json: %v", err)
	}
	if persisted.Server.Port != 8123 {
		t.Errorf("persisted port = %d, want 8123", persisted.Server.Port)
	}
}
