package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// newTestStore creates a rateLimiterStore without starting the cleanup goroutine.
func newTestStore(rps float64, burst int) *rateLimiterStore {
	return &rateLimiterStore{
		clients: make(map[string]*client),
		rps:     rate.Limit(rps),
		burst:   burst,
	}
}

func TestGetLimiter_CreatesNewLimiter(t *testing.T) {
	store := newTestStore(10, 20)

	limiter := store.getLimiter("192.168.1.1")
	require.NotNil(t, limiter)
	assert.Len(t, store.clients, 1)
}

func TestGetLimiter_ReturnsSameLimiter(t *testing.T) {
	store := newTestStore(10, 20)

	limiter1 := store.getLimiter("192.168.1.1")
	limiter2 := store.getLimiter("192.168.1.1")

	assert.Same(t, limiter1, limiter2, "same IP should return the same limiter instance")
	assert.Len(t, store.clients, 1)
}

func TestGetLimiter_DifferentIPsDifferentLimiters(t *testing.T) {
	store := newTestStore(10, 20)

	limiterA := store.getLimiter("10.0.0.1")
	limiterB := store.getLimiter("10.0.0.2")

	assert.NotSame(t, limiterA, limiterB, "different IPs should get different limiters")
	assert.Len(t, store.clients, 2)
}

func TestRateLimitMiddleware_DisabledByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	viper.Set("security.rate_limit.enabled", false)
	defer viper.Set("security.rate_limit.enabled", false)

	router := gin.New()
	router.Use(rateLimitMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Send several requests; all should pass through since rate limiting is disabled.
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/test", nil)
		require.NoError(t, err)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should pass through when rate limiting is disabled", i)
	}
}

func TestRateLimitMiddleware_Enabled_AllowsWithinLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	viper.Set("security.rate_limit.enabled", true)
	viper.Set("security.rate_limit.requests_per_second", 1000.0)
	viper.Set("security.rate_limit.burst", 1000)
	defer func() {
		viper.Set("security.rate_limit.enabled", false)
		viper.Set("security.rate_limit.requests_per_second", 0.0)
		viper.Set("security.rate_limit.burst", 0)
	}()

	router := gin.New()
	router.Use(rateLimitMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// With a high limit, a handful of requests should all succeed.
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/test", nil)
		require.NoError(t, err)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should be allowed within high limit", i)
	}
}

func TestRateLimitMiddleware_Enabled_RejectsOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	viper.Set("security.rate_limit.enabled", true)
	viper.Set("security.rate_limit.requests_per_second", 1.0)
	viper.Set("security.rate_limit.burst", 1)
	defer func() {
		viper.Set("security.rate_limit.enabled", false)
		viper.Set("security.rate_limit.requests_per_second", 0.0)
		viper.Set("security.rate_limit.burst", 0)
	}()

	router := gin.New()
	router.Use(rateLimitMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// First request should succeed (uses the burst token).
	w1 := httptest.NewRecorder()
	req1, err := http.NewRequest("GET", "/test", nil)
	require.NoError(t, err)
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code, "first request should be allowed")

	// Immediately following requests should be rejected since burst=1 and rps=1.
	got429 := false
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/test", nil)
		require.NoError(t, err)
		router.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			got429 = true
			assert.Equal(t, "1", w.Header().Get("Retry-After"))
			assert.Contains(t, w.Body.String(), "rate limit exceeded")
			break
		}
	}
	assert.True(t, got429, "expected at least one 429 response when exceeding rate limit")
}
