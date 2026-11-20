package core

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"webclash/internal/config"
	"webclash/internal/geoip"
	"webclash/internal/util"
)

const (
	// geoipLookupCap bounds the number of unique targets per batch request.
	geoipLookupCap = 256
	// geoipResolveWorkers bounds concurrent DNS resolutions per batch.
	geoipResolveWorkers = 8
	// geoipResolveTimeout caps a single hostname resolution.
	geoipResolveTimeout = 2 * time.Second
	// geoipDNSCacheMax bounds the resolution cache; expired entries are swept
	// first and the map is reset if it is still full.
	geoipDNSCacheMax = 4096
)

const (
	geoipDNSSuccessTTL = 10 * time.Minute
	geoipDNSFailureTTL = time.Minute
)

// dnsCacheEntry is one memoized hostname resolution (ip == "" for failures).
type dnsCacheEntry struct {
	ip      string
	expires time.Time
}

// GeoIPStatus returns the manager snapshot for the current settings source.
func (c *Core) GeoIPStatus() geoip.Status {
	return c.GeoIP.Status(c.Settings().GeoIP.Source)
}

// GeoIPCheck probes remote DB versions (best-effort). 409 when disabled.
func (c *Core) GeoIPCheck(ctx context.Context) (geoip.CheckResult, error) {
	s := c.Settings()
	if !s.GeoIP.Enable {
		return geoip.CheckResult{}, util.NewHttpError(409, "IP 归属未启用")
	}
	return c.GeoIP.CheckUpdate(ctx, s.GeoIP.Source, s.Kernel.Mirror), nil
}

// GeoIPUpdate force-refreshes the current source DB asynchronously. 409 when
// disabled; progress is reflected via GeoIPStatus().downloading/error.
func (c *Core) GeoIPUpdate() error {
	s := c.Settings()
	if !s.GeoIP.Enable {
		return util.NewHttpError(409, "IP 归属未启用")
	}
	go c.GeoIP.Ensure(s.GeoIP.Source, s.Kernel.Mirror, true)
	return nil
}

// GeoIPLookup resolves a batch of targets — IPv4/IPv6 literals or hostnames,
// deduped and capped at 256. Hostnames are resolved through the panel's DNS
// (cached, bounded concurrency) before the geo lookup, so connections whose
// metadata only carries a domain still get attribution. Results are keyed by
// the target as requested. 409 when disabled or the database is not ready.
// Unresolvable targets are omitted from the result map.
func (c *Core) GeoIPLookup(ctx context.Context, targets []string) (map[string]geoip.Region, error) {
	s := c.Settings()
	if !s.GeoIP.Enable {
		return nil, util.NewHttpError(409, "IP 归属未启用")
	}
	if !c.GeoIP.Status(s.GeoIP.Source).Ready {
		return nil, util.NewHttpError(409, "IP 归属数据库未就绪，请先更新")
	}
	results := map[string]geoip.Region{}
	for target, ip := range c.resolveTargetIPs(ctx, lookupTargets(targets)) {
		if region, ok := c.GeoIP.Lookup(ip); ok {
			results[target] = region
		}
	}
	return results, nil
}

// resolveTargetIPs maps each target to the IP used for the geo lookup: IP
// literals map to themselves, valid hostnames resolve via the cached resolver,
// and blank/invalid/unresolvable targets are omitted.
func (c *Core) resolveTargetIPs(ctx context.Context, targets []string) map[string]string {
	out := make(map[string]string, len(targets))
	var hosts []string
	for _, t := range targets {
		if net.ParseIP(t) != nil {
			out[t] = t
			continue
		}
		if validHostname(t) {
			hosts = append(hosts, t)
		}
	}
	if len(hosts) == 0 {
		return out
	}

	type pair struct{ host, ip string }
	done := make(chan pair, len(hosts))
	sem := make(chan struct{}, geoipResolveWorkers)
	var wg sync.WaitGroup
	for _, host := range hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				done <- pair{host, ""}
				return
			}
			defer func() { <-sem }()
			done <- pair{host, c.resolveHost(ctx, host)}
		}(host)
	}
	wg.Wait()
	close(done)
	for p := range done {
		if p.ip != "" {
			out[p.host] = p.ip
		}
	}
	return out
}

// resolveHost resolves a hostname to one IP, preferring IPv4 for stable
// attribution. Success is cached for 10 minutes; failures briefly, so dead
// hosts are not re-queried on every connections refresh.
func (c *Core) resolveHost(ctx context.Context, host string) string {
	key := strings.ToLower(host)
	now := time.Now()
	c.dnsMu.Lock()
	if e, ok := c.dnsCache[key]; ok && now.Before(e.expires) {
		c.dnsMu.Unlock()
		return e.ip
	}
	c.dnsMu.Unlock()

	rctx, cancel := context.WithTimeout(ctx, geoipResolveTimeout)
	ips, err := net.DefaultResolver.LookupHost(rctx, host)
	cancel()
	ip := ""
	if err == nil {
		for _, a := range ips {
			if p := net.ParseIP(a); p != nil && p.To4() != nil {
				ip = a
				break
			}
		}
		if ip == "" {
			for _, a := range ips {
				if net.ParseIP(a) != nil {
					ip = a
					break
				}
			}
		}
	}

	ttl := geoipDNSSuccessTTL
	if ip == "" {
		ttl = geoipDNSFailureTTL
	}
	c.dnsMu.Lock()
	if c.dnsCache == nil {
		c.dnsCache = make(map[string]dnsCacheEntry)
	}
	if len(c.dnsCache) >= geoipDNSCacheMax {
		for k, e := range c.dnsCache {
			if now.After(e.expires) {
				delete(c.dnsCache, k)
			}
		}
		if len(c.dnsCache) >= geoipDNSCacheMax {
			c.dnsCache = make(map[string]dnsCacheEntry)
		}
	}
	c.dnsCache[key] = dnsCacheEntry{ip: ip, expires: now.Add(ttl)}
	c.dnsMu.Unlock()
	return ip
}

// validHostname accepts a syntactically plausible DNS name (single trailing
// dot allowed). IP literals are handled earlier; colons and other junk are
// rejected so only domain-like strings reach the resolver.
func validHostname(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i := 0; i < len(label); i++ {
			ch := label[i]
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' {
				continue
			}
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}

// lookupTargets returns the unique, non-empty targets to resolve, capped at
// geoipLookupCap. Order is preserved; duplicates and blanks are dropped.
func lookupTargets(targets []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		if t == "" || seen[t] {
			continue
		}
		if len(out) >= geoipLookupCap {
			break
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// applyGeoIPChange reacts to a settings save: enabling or switching source kicks
// off an async Ensure; disabling releases the reader while keeping the files.
func (c *Core) applyGeoIPChange(before, next config.GeoIPSettings, mirror string) {
	if !next.Enable {
		if before.Enable {
			c.GeoIP.Close()
		}
		return
	}
	if !before.Enable || before.Source != next.Source {
		go c.GeoIP.Ensure(next.Source, mirror, false)
	}
}
