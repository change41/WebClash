package util

import (
	"math"
	"strconv"
	"strings"
)

// SubscriptionUserInfo mirrors types.ts SubscriptionUserInfo (optional numeric
// fields parsed from the subscription-userinfo HTTP header).
type SubscriptionUserInfo struct {
	Upload   *float64 `json:"upload,omitempty"`
	Download *float64 `json:"download,omitempty"`
	Total    *float64 `json:"total,omitempty"`
	Expire   *float64 `json:"expire,omitempty"`
}

// ParseUserInfo parses "upload=..; download=..; total=..; expire=.." headers.
// Returns nil when no finite numeric fields are present (port of parseUserInfo).
func ParseUserInfo(header string) *SubscriptionUserInfo {
	if header == "" {
		return nil
	}
	out := &SubscriptionUserInfo{}
	found := false
	for _, part := range strings.Split(header, ";") {
		segs := strings.Split(part, "=")
		if len(segs) < 2 {
			continue
		}
		key := strings.TrimSpace(segs[0])
		if key == "" {
			continue
		}
		raw := strings.TrimSpace(segs[1])
		var val float64
		if raw == "" {
			val = 0 // JS Number("") === 0
		} else {
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			val = v
		}
		switch key {
		case "upload":
			out.Upload = &val
			found = true
		case "download":
			out.Download = &val
			found = true
		case "total":
			out.Total = &val
			found = true
		case "expire":
			out.Expire = &val
			found = true
		}
	}
	if !found {
		return nil
	}
	return out
}
