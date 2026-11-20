package core

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"webclash/internal/util"
)

func TestLookupTargetsDedupAndBlank(t *testing.T) {
	got := lookupTargets([]string{"1.1.1.1", "1.1.1.1", "2.2.2.2", "", "3.3.3.3", "2.2.2.2"})
	want := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order not preserved: got %v, want %v", got, want)
		}
	}
}

func TestLookupTargetsCap(t *testing.T) {
	targets := make([]string, 0, geoipLookupCap+50)
	for i := 0; i < geoipLookupCap+50; i++ {
		targets = append(targets, fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}
	got := lookupTargets(targets)
	if len(got) != geoipLookupCap {
		t.Fatalf("cap = %d, want %d", len(got), geoipLookupCap)
	}
	if got[0] != targets[0] || got[geoipLookupCap-1] != targets[geoipLookupCap-1] {
		t.Fatal("cap must keep the first N unique targets in order")
	}
}

func TestValidHostname(t *testing.T) {
	cases := map[string]bool{
		"example.com":                    true,
		"a-b.c":                          true,
		"localhost":                      true,
		"example.com.":                   true,
		"xn--fiq228c.com":                true,
		"":                               false,
		"bad host":                       false,
		"bad/host":                       false,
		"host:443":                       false,
		"-bad.com":                       false,
		"bad-.com":                       false,
		"..":                             false,
		strings.Repeat("a", 64) + ".com": false,
	}
	for in, want := range cases {
		if got := validHostname(in); got != want {
			t.Fatalf("validHostname(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestResolveTargetIPs(t *testing.T) {
	c := New(t.TempDir())
	defer c.GeoIP.Close()
	got := c.resolveTargetIPs(context.Background(), lookupTargets([]string{"1.2.3.4", "::1", "localhost", "bad host", ""}))
	if got["1.2.3.4"] != "1.2.3.4" || got["::1"] != "::1" {
		t.Fatalf("IP literals must map to themselves: %v", got)
	}
	// localhost comes from /etc/hosts, so this stays offline-safe.
	if ip := got["localhost"]; net.ParseIP(ip) == nil {
		t.Fatalf("localhost must resolve, got %q in %v", ip, got)
	}
	if _, ok := got["bad host"]; ok {
		t.Fatal("invalid hostname must be dropped")
	}
	if _, ok := got[""]; ok {
		t.Fatal("blank target must be dropped")
	}
}

func TestResolveHostCacheHit(t *testing.T) {
	c := New(t.TempDir())
	defer c.GeoIP.Close()
	c.dnsMu.Lock()
	c.dnsCache = map[string]dnsCacheEntry{
		"cached.example": {ip: "9.9.9.9", expires: time.Now().Add(time.Hour)},
	}
	c.dnsMu.Unlock()
	// Cache keys are case-insensitive and must not hit the network.
	if ip := c.resolveHost(context.Background(), "CACHED.Example"); ip != "9.9.9.9" {
		t.Fatalf("cached resolve = %q, want 9.9.9.9", ip)
	}
}

func httpErr(t *testing.T, err error, code int, msg string) {
	t.Helper()
	he, ok := err.(*util.HttpError)
	if !ok || he.StatusCode != code || he.Message != msg {
		t.Fatalf("expected %d %q, got %v", code, msg, err)
	}
}

func TestGeoIPDisabled409(t *testing.T) {
	c := New(t.TempDir())
	defer c.GeoIP.Close()
	if _, err := c.GeoIPLookup(context.Background(), []string{"1.2.3.4"}); err == nil {
		t.Fatal("disabled lookup must error")
	} else {
		httpErr(t, err, 409, "IP 归属未启用")
	}
	if _, err := c.GeoIPCheck(context.Background()); err == nil {
		t.Fatal("disabled check must error")
	} else {
		httpErr(t, err, 409, "IP 归属未启用")
	}
	if err := c.GeoIPUpdate(); err == nil {
		t.Fatal("disabled update must error")
	} else {
		httpErr(t, err, 409, "IP 归属未启用")
	}
}

func TestGeoIPLookupNotReady409(t *testing.T) {
	c := New(t.TempDir())
	defer c.GeoIP.Close()
	c.mu.Lock()
	c.settings.GeoIP.Enable = true
	c.mu.Unlock()
	// Enabled but no database downloaded/opened → not ready.
	_, err := c.GeoIPLookup(context.Background(), []string{"1.2.3.4"})
	httpErr(t, err, 409, "IP 归属数据库未就绪，请先更新")
}

func TestGeoIPStatusDefaults(t *testing.T) {
	c := New(t.TempDir())
	defer c.GeoIP.Close()
	st := c.GeoIPStatus()
	if st.Source != "ip2region" {
		t.Fatalf("default source = %q, want ip2region", st.Source)
	}
	if st.Ready || st.Downloading {
		t.Fatalf("fresh manager must be idle/not-ready: %+v", st)
	}
}
