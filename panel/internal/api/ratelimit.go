package api

import (
	"fmt"
	"math"
	"sync"
	"time"

	"webclash/internal/util"
)

type loginRecord struct {
	count        int
	blockedUntil int64
}

// rateLimiter throttles login attempts per IP (port of authRoutes attempts map):
// 5 failures → 60s block → 429 with ceil seconds; success clears the record.
type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginRecord
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{attempts: map[string]*loginRecord{}}
}

// login runs the rate-limit + verify sequence for one attempt. verify reports
// whether the password matches. Returns nil on success, else an HttpError.
func (rl *rateLimiter) login(ip, password string, verify func(string) bool) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now().UnixMilli()
	rec := rl.attempts[ip]
	if rec == nil {
		rec = &loginRecord{}
		rl.attempts[ip] = rec
	}
	if rec.blockedUntil > now {
		seconds := int(math.Ceil(float64(rec.blockedUntil-now) / 1000))
		return util.NewHttpError(429, fmt.Sprintf("尝试次数过多，请 %d 秒后再试", seconds))
	}
	if rec.blockedUntil != 0 {
		rec.count = 0
		rec.blockedUntil = 0
	}
	if !verify(password) {
		rec.count++
		if rec.count >= 5 {
			rec.count = 0
			rec.blockedUntil = now + 60_000
		}
		return util.NewHttpError(401, "密码错误")
	}
	delete(rl.attempts, ip)
	return nil
}
