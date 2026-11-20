package geoip

import (
	"strings"
	"time"

	"github.com/lionsoul2014/ip2region/binding/golang/service"
	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

// ip2regionReader wraps the official unified v4+v6 service. Search auto-routes
// 4-byte IPs to the v4 xdb and 16-byte IPs to the v6 xdb; a disabled version
// (empty path) yields "" rather than an error, so lookups degrade gracefully.
type ip2regionReader struct {
	svc      *service.Ip2Region
	versions map[string]string
}

func openIP2Region(v4Path, v6Path string) (*ip2regionReader, error) {
	svc, err := service.NewIp2RegionWithPath(v4Path, v6Path)
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	if v4Path != "" {
		if h, err := xdb.LoadHeaderFromFile(v4Path); err == nil {
			versions[fileV4] = xdbVersion(h)
		}
	}
	if v6Path != "" {
		if h, err := xdb.LoadHeaderFromFile(v6Path); err == nil {
			versions[fileV6] = xdbVersion(h)
		}
	}
	return &ip2regionReader{svc: svc, versions: versions}, nil
}

func xdbVersion(h *xdb.Header) string {
	if h.CreatedAt == 0 {
		return ""
	}
	return time.Unix(int64(h.CreatedAt), 0).UTC().Format("2006-01-02")
}

func (r *ip2regionReader) Lookup(ip string) (Region, bool) {
	s, err := r.svc.Search(ip)
	if err != nil || s == "" {
		return Region{}, false
	}
	return parseIP2Region(s), true
}

func (r *ip2regionReader) Versions() map[string]string { return r.versions }

func (r *ip2regionReader) Close() { r.svc.Close() }

// parseIP2Region maps the structure-3.0 region string, verified against the real
// upstream xdb: "country|province|city|isp|countryCode" (e.g.
// "中国|广东省|深圳市|电信|CN", "United States|California|0|Google LLC|US").
// "0"/empty segments are skipped. Reserved/private IPs ("Reserved|...") collapse
// to a single Reserved marker so the UI never shows triple placeholders.
func parseIP2Region(s string) Region {
	parts := strings.Split(s, "|")
	get := func(i int) string {
		if i < len(parts) {
			if v := strings.TrimSpace(parts[i]); v != "0" {
				return v
			}
		}
		return ""
	}
	region := Region{
		Country:     get(0),
		Province:    get(1),
		City:        get(2),
		ISP:         get(3),
		CountryCode: get(4),
		Raw:         s,
	}
	if region.Country == "Reserved" {
		region.Province, region.City, region.ISP, region.CountryCode = "", "", "", ""
	}
	return region
}
