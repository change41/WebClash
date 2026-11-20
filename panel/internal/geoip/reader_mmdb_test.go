package geoip

import "testing"

// TestMetaRegion locks the real MetaCubeX "Meta-geoip0" record shapes observed
// from geoip.metadb: a bare lowercase code ("cn"), the reserved marker
// ("private"), or a [code, organization] pair (["us","google"]). The compact DB
// carries no country names, so Country stays empty for a known code.
func TestMetaRegion(t *testing.T) {
	cases := []struct {
		name                           string
		rec                            any
		ok                             bool
		country, countryCode, isp, raw string
	}{
		{"cn code", "cn", true, "", "CN", "", "cn"},
		{"us code", "us", true, "", "US", "", "us"},
		{"code with org", []any{"us", "google"}, true, "", "US", "google", "us|google"},
		{"au cloudflare", []any{"au", "cloudflare"}, true, "", "AU", "cloudflare", "au|cloudflare"},
		{"single element array", []any{"cn"}, true, "", "CN", "", "cn"},
		{"private", "private", true, "Reserved", "", "", "private"},
		{"private uppercase", "PRIVATE", true, "Reserved", "", "", "PRIVATE"},
		{"trimmed code", "  cn  ", true, "", "CN", "", "cn"},
		{"non-string org", []any{"cn", 456}, true, "", "CN", "", "cn"},
		{"empty string", "", false, "", "", "", ""},
		{"blank string", "   ", false, "", "", "", ""},
		{"nil record", nil, false, "", "", "", ""},
		{"numeric record", 123, false, "", "", "", ""},
		{"empty array", []any{}, false, "", "", "", ""},
		{"non-string code", []any{123, "google"}, false, "", "", "", ""},
	}
	for _, c := range cases {
		got, ok := metaRegion(c.rec)
		if ok != c.ok {
			t.Fatalf("%s: ok=%v want %v (got %+v)", c.name, ok, c.ok, got)
		}
		if !c.ok {
			continue
		}
		if got.Country != c.country || got.CountryCode != c.countryCode || got.ISP != c.isp || got.Raw != c.raw {
			t.Fatalf("%s: got %+v want country=%q cc=%q isp=%q raw=%q", c.name, got, c.country, c.countryCode, c.isp, c.raw)
		}
		if got.Province != "" || got.City != "" {
			t.Fatalf("%s: mmdb tier must not populate province/city: %+v", c.name, got)
		}
	}
}
