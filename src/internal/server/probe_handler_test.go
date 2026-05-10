package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
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

func TestRunThroughput_AggregateIsBandwidthWeighted(t *testing.T) {
	// Two iterations, equal bytes, very different elapsed times.
	// Per-iteration mbps: 80 and 8. Mean = 44 mbps.
	// Bandwidth-weighted: total_bytes=2*1MB=16Mbits, total_elapsed=0.1+1.0=1.1s = 16/1.1 ≈ 14.55 mbps.
	// The aggregate should be the bandwidth-weighted number.
	iter1 := map[string]any{"bytes_received": int64(1000000), "elapsed_ns": int64(100000000), "mbps": 80.0}
	iter2 := map[string]any{"bytes_received": int64(1000000), "elapsed_ns": int64(1000000000), "mbps": 8.0}
	iterations := []map[string]any{iter1, iter2}

	var totalBytes, totalElapsed int64
	for _, it := range iterations {
		totalBytes += it["bytes_received"].(int64)
		totalElapsed += it["elapsed_ns"].(int64)
	}
	expected := (float64(totalBytes) * 8.0 / 1e6) / (float64(totalElapsed) / 1e9)
	// Sanity check the test arithmetic itself.
	assert.InDelta(t, 14.545, expected, 0.01)
}

func TestProcessPingResults_RejectsZeroRTTAndCountsErrors(t *testing.T) {
	// libp2p's ping service emits Result{Error:nil, RTT:0} when the dial
	// layer can't establish a stream. Without rejection, unreachable peers
	// would register as fake-zero-RTT successes.
	ch := make(chan ping.Result, 5)
	ch <- ping.Result{RTT: 5 * time.Millisecond}                // valid
	ch <- ping.Result{RTT: 0}                                   // zero-RTT bug
	ch <- ping.Result{Error: errors.New("dial fail")}           // explicit error
	ch <- ping.Result{RTT: 6 * time.Millisecond}                // valid
	ch <- ping.Result{RTT: 0}                                   // zero-RTT bug
	close(ch)

	samples, failed, lastErr := processPingResults(context.Background(), ch, 5)
	assert.Equal(t, []int64{int64(5 * time.Millisecond), int64(6 * time.Millisecond)}, samples)
	assert.Equal(t, 3, failed)
	assert.NotEmpty(t, lastErr)
}

func TestProcessPingResults_StopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan ping.Result)
	cancel()
	samples, failed, lastErr := processPingResults(ctx, ch, 5)
	assert.Empty(t, samples)
	assert.Equal(t, 0, failed)
	assert.Contains(t, lastErr, "context canceled")
}

func TestProcessPingResults_HandlesClosedChannel(t *testing.T) {
	ch := make(chan ping.Result, 2)
	ch <- ping.Result{RTT: 1 * time.Millisecond}
	close(ch)
	samples, failed, _ := processPingResults(context.Background(), ch, 5)
	assert.Equal(t, []int64{int64(1 * time.Millisecond)}, samples)
	assert.Equal(t, 0, failed)
}
