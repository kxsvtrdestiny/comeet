package httpapi

import (
	"comeet/server/internal/config"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Bounded per-login buckets; arbitrary meeting/path IDs cannot grow the map.
// Process-local protection complements auth/IP and per-WebSocket chat limits.
func apiLimit(c config.Config) func(http.Handler) http.Handler {
	type bucket struct {
		count int
		until time.Time
	}
	var mu sync.Mutex
	items := map[string]bucket{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/auth/logout" {
				next.ServeHTTP(w, r)
				return
			}
			kind, limit := "read", c.APIReads
			if limit == 0 {
				limit = 600
			}
			if r.Method != "GET" && r.Method != "HEAD" {
				kind, limit = "write", c.APIWrites
				if limit == 0 {
					limit = 120
				}
			}
			if r.Method == "POST" && r.URL.Path == "/api/v1/meetings" {
				kind, limit = "create", c.APICreates
				if limit == 0 {
					limit = 20
				}
			}
			key := identity(r).SID + ":" + kind
			now := time.Now()
			mu.Lock()
			b := items[key]
			if !now.Before(b.until) {
				b = bucket{until: now.Add(time.Minute)}
			}
			if len(items) >= 4096 {
				for k, v := range items {
					if !now.Before(v.until) {
						delete(items, k)
					}
				}
			}
			_, exists := items[key]
			allowed := b.count < limit && (exists || len(items) < 4096)
			if allowed {
				b.count++
				items[key] = b
			}
			mu.Unlock()
			if !allowed {
				seconds := max(1, int(time.Until(b.until).Seconds())+1)
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
				send(w, r, 429, "RATE_LIMITED", "操作过于频繁，请稍后重试", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
