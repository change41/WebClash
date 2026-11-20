package api

import "testing"

func TestGeoIPStatusRequiresAuth(t *testing.T) {
	s := newTestServer(t)
	if r := do(s, "GET", "/api/geoip/status", "", nil); r.code != 401 {
		t.Fatalf("unauth status code = %d, want 401", r.code)
	}
}

func TestGeoIPStatusDisabled(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	r := do(s, "GET", "/api/geoip/status", "", cookies)
	if r.code != 200 {
		t.Fatalf("status code = %d, want 200 (always, even disabled): %s", r.code, r.body)
	}
	if jsonField(t, r.body, "enable") != false {
		t.Fatalf("enable = %v, want false by default", jsonField(t, r.body, "enable"))
	}
	if jsonField(t, r.body, "source") != "ip2region" {
		t.Fatalf("source = %v, want ip2region", jsonField(t, r.body, "source"))
	}
	if jsonField(t, r.body, "ready") != false {
		t.Fatalf("ready = %v, want false", jsonField(t, r.body, "ready"))
	}
}

func TestGeoIPCheckDisabled409(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	r := do(s, "POST", "/api/geoip/check", "", cookies)
	if r.code != 409 || jsonField(t, r.body, "error") != "IP 归属未启用" {
		t.Fatalf("got %d %s", r.code, r.body)
	}
}

func TestGeoIPUpdateDisabled409(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	r := do(s, "POST", "/api/geoip/update", "", cookies)
	if r.code != 409 || jsonField(t, r.body, "error") != "IP 归属未启用" {
		t.Fatalf("got %d %s", r.code, r.body)
	}
}

func TestGeoIPLookupRequiresAuth(t *testing.T) {
	s := newTestServer(t)
	r := do(s, "POST", "/api/geoip/lookup", `{"ips":["1.2.3.4"]}`, nil)
	if r.code != 401 {
		t.Fatalf("unauth lookup code = %d, want 401", r.code)
	}
}

func TestGeoIPLookupDisabled409(t *testing.T) {
	s := newTestServer(t)
	cookies := loginCookies(t, s)
	r := do(s, "POST", "/api/geoip/lookup", `{"ips":["1.2.3.4"]}`, cookies)
	if r.code != 409 || jsonField(t, r.body, "error") != "IP 归属未启用" {
		t.Fatalf("got %d %s", r.code, r.body)
	}
}
