package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	probeGroup := r.Group("/v1/probe")
	probeGroup.GET("/echo", echoHandler)
	return r
}

func TestEcho_ReturnsRequestedByteCount(t *testing.T) {
	r := newTestEngine()
	for _, n := range []int64{0, 1, 1024, 1 << 20} {
		req := httptest.NewRequest(http.MethodGet, "/v1/probe/echo?bytes="+strconv.FormatInt(n, 10), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "n=%d", n)
		body, _ := io.ReadAll(w.Body)
		assert.Equal(t, int(n), len(body), "n=%d", n)
		assert.Equal(t, strconv.FormatInt(n, 10), w.Header().Get("Content-Length"), "n=%d", n)
		if n > 0 && n <= 1024 {
			expected := make([]byte, n)
			assert.Equal(t, expected, body, "body should be all-zero, n=%d", n)
		}
	}
}

func TestEcho_RejectsMissingBytes(t *testing.T) {
	r := newTestEngine()
	req := httptest.NewRequest(http.MethodGet, "/v1/probe/echo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEcho_RejectsInvalidBytes(t *testing.T) {
	r := newTestEngine()
	req := httptest.NewRequest(http.MethodGet, "/v1/probe/echo?bytes=notanumber", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEcho_RejectsExcessiveBytes(t *testing.T) {
	r := newTestEngine()
	req := httptest.NewRequest(http.MethodGet, "/v1/probe/echo?bytes=1073741825", nil) // 1 GiB + 1
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEcho_RejectsNegativeBytes(t *testing.T) {
	r := newTestEngine()
	req := httptest.NewRequest(http.MethodGet, "/v1/probe/echo?bytes=-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRun_RejectsInvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/probe/run", runHandler)

	req := httptest.NewRequest(http.MethodPost, "/v1/probe/run", bytes.NewReader([]byte("{")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRun_RejectsUnknownKind(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/probe/run", runHandler)

	body, _ := json.Marshal(map[string]any{
		"target": "12D3KooWPHmsoT1AdLbLUzVDYTk3xx3jSPfFy3Y3FdPzYpbPLyrV",
		"kind":   "telepathy",
		"count":  1,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/probe/run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRun_RejectsMissingTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/probe/run", runHandler)

	body, _ := json.Marshal(map[string]any{
		"kind":  "latency",
		"count": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/probe/run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSummariseDurations_ComputesPercentiles(t *testing.T) {
	samples := []int64{1_000_000, 2_000_000, 3_000_000, 4_000_000, 5_000_000}
	m := summariseDurations(samples, 0)
	assert.Equal(t, int64(1_000_000), m["min_ns"])
	assert.Equal(t, int64(5_000_000), m["max_ns"])
	assert.Equal(t, int64(3_000_000), m["avg_ns"])
	assert.Equal(t, int64(3_000_000), m["p50_ns"])
	assert.Equal(t, int64(5_000_000), m["p95_ns"])
	assert.Equal(t, 0, m["failed_samples"])
	assert.Equal(t, 1.0, m["min_ms"])
	assert.Equal(t, 3.0, m["avg_ms"])
}
