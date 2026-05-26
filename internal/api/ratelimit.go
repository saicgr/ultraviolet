package api

import (
	"net/http"
	"sync"
	"time"
)

// tokenBucket is a per-key token bucket: capacity = burst, refilled at rps tokens/sec.
type tokenBucket struct {
	tokens   float64
	last     time.Time
	capacity float64
	rps      float64
}

func (b *tokenBucket) take(now time.Time) bool {
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min64(b.capacity, b.tokens+elapsed*b.rps)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// Limiter is a simple in-memory rate limiter keyed by API key / remote IP.
// Phase 1: per-process; multi-instance deploys need a shared store (Redis token bucket
// or a sidecar like envoy local_ratelimit). Tracked in IMPROVEMENTS.md.
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rps      float64
	capacity float64
}

func NewLimiter(rps int) *Limiter {
	if rps <= 0 {
		rps = 100
	}
	return &Limiter{
		buckets:  map[string]*tokenBucket{},
		rps:      float64(rps),
		capacity: float64(rps) * 2, // 2 second burst window
	}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	now := time.Now()
	if !ok {
		b = &tokenBucket{tokens: l.capacity, last: now, capacity: l.capacity, rps: l.rps}
		l.buckets[key] = b
	}
	return b.take(now)
}

func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			key = r.RemoteAddr
		}
		if !l.Allow(key) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
