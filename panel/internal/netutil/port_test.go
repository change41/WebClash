package netutil

import (
	"net"
	"testing"
)

func freePort(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

func TestTCPFreeDetectsListener(t *testing.T) {
	ln, port := freePort(t)
	defer ln.Close()
	if TCPFree("127.0.0.1", port) {
		t.Fatalf("port %d reported free while bound", port)
	}
}

func TestTCPFreeRejectsInvalidPort(t *testing.T) {
	for _, port := range []int{0, -1, 70000} {
		if TCPFree("127.0.0.1", port) {
			t.Errorf("port %d should not be usable", port)
		}
	}
}

func TestFindFreeSkipsBusyAndReserved(t *testing.T) {
	ln, busy := freePort(t)
	defer ln.Close()
	reserved := map[int]bool{busy + 1: true}
	got, ok := FindFree(busy, 50, reserved, func(port int) bool {
		return TCPFree("127.0.0.1", port)
	})
	if !ok {
		t.Fatal("no free port found")
	}
	if got <= busy+1 {
		t.Fatalf("FindFree = %d, want > %d (busy %d, reserved %d)", got, busy+1, busy, busy+1)
	}
	if !TCPFree("127.0.0.1", got) {
		t.Fatalf("returned port %d is not free", got)
	}
}

func TestListenTCPFallsBack(t *testing.T) {
	ln, busy := freePort(t)
	defer ln.Close()
	got, port, err := ListenTCP("127.0.0.1", busy, 10, nil)
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	defer got.Close()
	if port == busy {
		t.Fatalf("bound busy port %d", busy)
	}
	if port < busy {
		t.Fatalf("bound port %d < busy %d", port, busy)
	}
}

func TestListenTCPSkipsReserved(t *testing.T) {
	ln, busy := freePort(t)
	defer ln.Close()
	reserved := map[int]bool{busy + 1: true}
	got, port, err := ListenTCP("127.0.0.1", busy, 10, reserved)
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	defer got.Close()
	if port <= busy+1 {
		t.Fatalf("bound port %d, want > %d (reserved)", port, busy+1)
	}
}
