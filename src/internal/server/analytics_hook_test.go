package server

import (
	"testing"
	"time"
)

func TestBuildProxySampleParsesUsageAndStatus(t *testing.T) {
	analyticsParseUsageBody = true
	t.Cleanup(func() { analyticsParseUsageBody = false })
	rw := newRetryableResponseWriter(nil, false, 1<<20)
	rw.statusCode = 200
	rw.Header().Set("X-Usage-GPU-Ms", "4200")
	rw.body.WriteString(`{"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":40}}}`)

	s := buildProxySample("llm", "m1", "w1", rw, &stageTimer{}, 3, 250*time.Millisecond)

	if s.Service != "llm" || s.Model != "m1" || s.WorkerPeerID != "w1" {
		t.Fatalf("dims wrong: %+v", s)
	}
	if s.Status != 200 || s.Concurrency != 3 {
		t.Fatalf("status/conc wrong: %+v", s)
	}
	if s.LatencyMs != 250 {
		t.Fatalf("latency = %v, want 250", s.LatencyMs)
	}
	if s.InputTokens != 120 || s.OutputTokens != 30 || s.CachedTokens != 40 {
		t.Fatalf("tokens wrong: %+v", s)
	}
	if s.GPUMs != 4200 {
		t.Fatalf("gpu_ms = %d, want 4200", s.GPUMs)
	}
}

func TestBuildProxySampleSkipsBodyWhenStreaming(t *testing.T) {
	analyticsParseUsageBody = true
	t.Cleanup(func() { analyticsParseUsageBody = false })
	rw := newRetryableResponseWriter(nil, true, 1<<20) // streaming
	rw.statusCode = 200
	s := buildProxySample("llm", "m1", "w1", rw, &stageTimer{}, 1, time.Second)
	if s.InputTokens != 0 || s.OutputTokens != 0 {
		t.Fatalf("streaming must not parse token body: %+v", s)
	}
}

func TestGetAnalyticsNilSafe(t *testing.T) {
	analyticsRecorder = nil
	if getAnalytics() != nil {
		t.Fatal("expected nil recorder")
	}
	// nil-receiver calls must not panic
	getAnalytics().Begin("llm", "m", "w")
	getAnalytics().End("llm", "m", "w")
}
