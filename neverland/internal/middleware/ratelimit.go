package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// idleEvictionWindow is how long an IP can be silent before its limiter is evicted.
// Keeps the limiters map bounded under adversarial / NAT-rotation traffic.
const idleEvictionWindow = 30 * time.Minute

type ipLimiter struct {
	limiter *rate.Limiter
	seen    time.Time
}

// RateLimitPerIP returns a middleware that allows up to `burst` requests per `window`
// for each remote IP. Returns 429 when exceeded. Keys off the X-Forwarded-For
// header if present, falling back to RemoteAddr.
//
// A background goroutine evicts limiters idle for more than idleEvictionWindow.
func RateLimitPerIP(burst int, window time.Duration) func(http.Handler) http.Handler {
	per := rate.Every(window / time.Duration(burst))
	var mu sync.Mutex
	limiters := map[string]*ipLimiter{}

	getLimiter := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		l, ok := limiters[ip]
		if !ok {
			l = &ipLimiter{limiter: rate.NewLimiter(per, burst)}
			limiters[ip] = l
		}
		l.seen = time.Now()
		return l.limiter
	}

	// Evict idle limiters every idleEvictionWindow / 4 to bound memory growth.
	go func() {
		t := time.NewTicker(idleEvictionWindow / 4)
		defer t.Stop()
		for now := range t.C {
			mu.Lock()
			for ip, l := range limiters {
				if now.Sub(l.seen) > idleEvictionWindow {
					delete(limiters, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !getLimiter(ip).Allow() {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		if i := indexByte(h, ','); i >= 0 {
			return h[:i]
		}
		return h
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
