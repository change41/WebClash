package geoip

import (
	"net"
	"strings"
	"time"

	maxminddb "github.com/oschwald/maxminddb-golang"
)

// mmdbReader looks up country-level geolocation from the MetaCubeX geoip.metadb.
// That file is the compact "Meta-geoip0" schema (not GeoLite2): each record is a
// bare lowercase country code ("cn"), the reserved marker ("private"), or a
// [code, organization] pair (["us","google"]). It carries no country names and
// no province/city, so only countryCode (+isp when present) can be populated.
type mmdbReader struct {
	db       *maxminddb.Reader
	versions map[string]string
}

func openMMDB(path string) (*mmdbReader, error) {
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	if db.Metadata.BuildEpoch > 0 {
		versions[fileMMDB] = time.Unix(int64(db.Metadata.BuildEpoch), 0).UTC().Format("2006-01-02")
	}
	return &mmdbReader{db: db, versions: versions}, nil
}

func (r *mmdbReader) Lookup(ip string) (Region, bool) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return Region{}, false
	}
	var rec any
	if err := r.db.Lookup(parsed, &rec); err != nil {
		return Region{}, false
	}
	return metaRegion(rec)
}

// metaRegion decodes one Meta-geoip0 record into the normalized Region. The
// compact DB has no country names, so a known country yields only its uppercase
// ISO code (+organization when the record is a [code, org] pair). "private"
// maps to Reserved to match the ip2region reader; empty/unknown yields false.
func metaRegion(rec any) (Region, bool) {
	var code, org string
	switch v := rec.(type) {
	case string:
		code = v
	case []any:
		if len(v) > 0 {
			code, _ = v[0].(string)
		}
		if len(v) > 1 {
			org, _ = v[1].(string)
		}
	default:
		return Region{}, false
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return Region{}, false
	}
	if strings.EqualFold(code, "private") {
		return Region{Country: "Reserved", Raw: code}, true
	}
	raw := code
	if org != "" {
		raw = code + "|" + org
	}
	return Region{CountryCode: strings.ToUpper(code), ISP: org, Raw: raw}, true
}

func (r *mmdbReader) Versions() map[string]string { return r.versions }

func (r *mmdbReader) Close() { _ = r.db.Close() }
