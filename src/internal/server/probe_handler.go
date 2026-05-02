package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"opentela/internal/common"
	"opentela/internal/protocol"

	"github.com/gin-gonic/gin"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
)

const maxEchoBytes = 1 << 30 // 1 GiB hard cap

func echoHandler(c *gin.Context) {
	raw := c.Query("bytes")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing required query parameter: bytes"})
		return
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 || n > maxEchoBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bytes must be an integer in [0, 1073741824]"})
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(n, 10))
	c.Status(http.StatusOK)
	if n == 0 {
		return
	}
	const chunk = 64 * 1024
	remaining := n
	for remaining > 0 {
		write := int64(chunk)
		if remaining < write {
			write = remaining
		}
		if _, err := io.CopyN(c.Writer, zeroReader{}, write); err != nil {
			common.Logger.Debugf("echo: write aborted after %d/%d bytes: %v", n-remaining, n, err)
			return
		}
		c.Writer.Flush()
		remaining -= write
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

type probeRequest struct {
	Target    string `json:"target"`
	Kind      string `json:"kind"`
	Bytes     int64  `json:"bytes"`
	Count     int    `json:"count"`
	TimeoutMS int    `json:"timeout_ms"`
}

type probeResponse struct {
	OK      bool           `json:"ok"`
	Kind    string         `json:"kind"`
	Target  string         `json:"target"`
	Config  map[string]any `json:"config"`
	Metrics map[string]any `json:"metrics"`
	Error   string         `json:"error,omitempty"`
}

func runHandler(c *gin.Context) {
	var req probeRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: " + err.Error()})
		return
	}
	if req.Target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target is required"})
		return
	}
	pid, err := libp2ppeer.Decode(req.Target)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid target peer id: " + err.Error()})
		return
	}
	if req.Count <= 0 {
		req.Count = 20
	}
	if req.TimeoutMS <= 0 {
		req.TimeoutMS = 60000
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()

	var resp probeResponse
	resp.Target = req.Target
	resp.Kind = req.Kind
	resp.Config = map[string]any{
		"count":      req.Count,
		"bytes":      req.Bytes,
		"timeout_ms": req.TimeoutMS,
	}
	switch req.Kind {
	case "latency":
		resp.OK, resp.Metrics, resp.Error = runLatency(ctx, pid, req.Count)
	case "throughput":
		resp.OK, resp.Metrics, resp.Error = runThroughput(ctx, pid, req.Bytes, req.Count)
	case "libp2p_ping":
		resp.OK, resp.Metrics, resp.Error = runLibp2pPing(ctx, pid, req.Count)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown kind: " + req.Kind})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func runLatency(ctx context.Context, pid libp2ppeer.ID, count int) (bool, map[string]any, string) {
	transport := getGlobalTransport()
	client := &http.Client{Transport: transport}
	samples := make([]int64, 0, count)
	var failed int
	var lastErr string
	url := "libp2p://" + pid.String() + "/v1/health"
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			failed++
			lastErr = err.Error()
			continue
		}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			failed++
			lastErr = err.Error()
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		samples = append(samples, time.Since(start).Nanoseconds())
	}
	if len(samples) == 0 {
		return false, map[string]any{"failed_samples": failed}, "no successful samples: " + lastErr
	}
	return true, summariseDurations(samples, failed), ""
}

func summariseDurations(samples []int64, failed int) map[string]any {
	sorted := append([]int64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum int64
	for _, v := range sorted {
		sum += v
	}
	pct := func(p float64) int64 {
		if len(sorted) == 0 {
			return 0
		}
		idx := int(math.Round(p * float64(len(sorted)-1)))
		return sorted[idx]
	}
	avg := sum / int64(len(sorted))
	return map[string]any{
		"min_ns":         sorted[0],
		"avg_ns":         avg,
		"p50_ns":         pct(0.5),
		"p95_ns":         pct(0.95),
		"max_ns":         sorted[len(sorted)-1],
		"min_ms":         float64(sorted[0]) / 1e6,
		"avg_ms":         float64(avg) / 1e6,
		"p50_ms":         float64(pct(0.5)) / 1e6,
		"p95_ms":         float64(pct(0.95)) / 1e6,
		"max_ms":         float64(sorted[len(sorted)-1]) / 1e6,
		"samples_ns":     samples,
		"failed_samples": failed,
	}
}

func runThroughput(ctx context.Context, pid libp2ppeer.ID, n int64, count int) (bool, map[string]any, string) {
	if n <= 0 {
		n = 10 * 1024 * 1024 // 10 MiB
	}
	transport := getGlobalTransport()
	client := &http.Client{Transport: transport}
	iterations := make([]map[string]any, 0, count)
	var failed int
	var lastErr string
	url := fmt.Sprintf("libp2p://%s/v1/probe/echo?bytes=%d", pid.String(), n)
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			failed++
			lastErr = err.Error()
			continue
		}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			failed++
			lastErr = err.Error()
			continue
		}
		bytesRead, copyErr := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		elapsed := time.Since(start).Nanoseconds()
		if copyErr != nil {
			failed++
			lastErr = copyErr.Error()
			continue
		}
		mbps := 0.0
		if elapsed > 0 {
			mbps = (float64(bytesRead) * 8.0 / 1e6) / (float64(elapsed) / 1e9)
		}
		iterations = append(iterations, map[string]any{
			"bytes_received": bytesRead,
			"elapsed_ns":     elapsed,
			"mbps":           mbps,
		})
	}
	if len(iterations) == 0 {
		return false, map[string]any{"failed_samples": failed}, "no successful iterations: " + lastErr
	}
	var totalBytes, totalElapsed int64
	var avgMbps float64
	for _, it := range iterations {
		totalBytes += it["bytes_received"].(int64)
		totalElapsed += it["elapsed_ns"].(int64)
		avgMbps += it["mbps"].(float64)
	}
	avgMbps /= float64(len(iterations))
	return true, map[string]any{
		"bytes_received": totalBytes,
		"elapsed_ns":     totalElapsed,
		"mbps":           avgMbps,
		"iterations":     iterations,
		"failed_samples": failed,
	}, ""
}

func runLibp2pPing(ctx context.Context, pid libp2ppeer.ID, count int) (bool, map[string]any, string) {
	h, _ := protocol.GetP2PNode(nil)
	if h == nil {
		return false, nil, "libp2p host not initialized"
	}
	svc := ping.NewPingService(h)
	resCh := svc.Ping(ctx, pid)
	samples := make([]int64, 0, count)
	var failed int
	var lastErr string
	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return len(samples) > 0, summariseDurations(samples, failed), ctx.Err().Error()
		case res := <-resCh:
			if res.Error != nil {
				failed++
				lastErr = res.Error.Error()
				continue
			}
			samples = append(samples, res.RTT.Nanoseconds())
		}
	}
	if len(samples) == 0 {
		return false, map[string]any{"failed_samples": failed}, "no successful pings: " + lastErr
	}
	return true, summariseDurations(samples, failed), ""
}

