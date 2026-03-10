package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"golang.org/x/time/rate"
)

// client tracks a per-IP rate limiter and the last time it was seen
// so stale entries can be evicted.
type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type rateLimiterStore struct {
	mu      sync.Mutex
	clients map[string]*client
	rps     rate.Limit
	burst   int
}

func newRateLimiterStore(rps float64, burst int) *rateLimiterStore {
	s := &rateLimiterStore{
		clients: make(map[string]*client),
		rps:     rate.Limit(rps),
		burst:   burst,
	}
	go s.cleanup()
	return s
}

func (s *rateLimiterStore) getLimiter(ip string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[ip]
	if !ok {
		limiter := rate.NewLimiter(s.rps, s.burst)
		s.clients[ip] = &client{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}
	c.lastSeen = time.Now()
	return c.limiter
}

// cleanup removes clients that haven't been seen in 3 minutes.
func (s *rateLimiterStore) cleanup() {
	for {
		time.Sleep(time.Minute)
		s.mu.Lock()
		for ip, c := range s.clients {
			if time.Since(c.lastSeen) > 3*time.Minute {
				delete(s.clients, ip)
			}
		}
		s.mu.Unlock()
	}
}

// rateLimitMiddleware returns a Gin middleware that rate-limits by client IP.
// If security.rate_limit.enabled is false (default), returns a no-op.
func rateLimitMiddleware() gin.HandlerFunc {
	if !viper.GetBool("security.rate_limit.enabled") {
		return func(c *gin.Context) { c.Next() }
	}

	rps := viper.GetFloat64("security.rate_limit.requests_per_second")
	if rps <= 0 {
		rps = 100
	}
	burst := viper.GetInt("security.rate_limit.burst")
	if burst <= 0 {
		burst = 200
	}

	store := newRateLimiterStore(rps, burst)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := store.getLimiter(ip)
		if !limiter.Allow() {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}
		c.Next()
	}
}
