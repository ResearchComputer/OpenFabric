package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMergeWorkerTimingIntoServerTiming verifies that a worker-reported
// X-Otela-Worker-Timing header is parsed and re-emitted under Server-Timing
// with each stage prefixed by "worker_".
func TestMergeWorkerTimingIntoServerTiming(t *testing.T) {
	timingEnabled = true
	defer func() { timingEnabled = false }()

	st := newStageTimer()
	st.Mark("head_recv")

	// Simulate ModifyResponse running with an upstream response carrying
	// the worker timing header.
	upstream := httptest.NewRecorder()
	upstream.Header().Set("X-Otela-Worker-Timing", "local_proxy;dur=1.1, sglang_ttft;dur=220.0")

	mergeWorkerTiming(st, upstream.Result())

	h := st.Header()
	assert.Contains(t, h, "head_recv;dur=")
	assert.Contains(t, h, "worker_local_proxy;dur=1.100")
	assert.Contains(t, h, "worker_sglang_ttft;dur=220.000")
	// Verify the stage appears only with the worker_ prefix, never bare.
	assert.False(t, strings.Contains(h, ", local_proxy;") || strings.HasPrefix(h, "local_proxy;"),
		"unprefixed worker stage leaked into header: %q", h)
}

// TestMergeWorkerTimingDisabledNoOp confirms that with the flag off, no
// worker stages are merged (and the header remains empty).
func TestMergeWorkerTimingDisabledNoOp(t *testing.T) {
	timingEnabled = false
	st := newStageTimer()
	st.Mark("head_recv")

	upstream := httptest.NewRecorder()
	upstream.Header().Set("X-Otela-Worker-Timing", "local_proxy;dur=1.1")

	mergeWorkerTiming(st, upstream.Result())
	assert.Equal(t, "", st.Header())
}

// TestMergeWorkerTimingMissingHeader confirms graceful handling when
// the upstream response has no X-Otela-Worker-Timing header.
func TestMergeWorkerTimingMissingHeader(t *testing.T) {
	timingEnabled = true
	defer func() { timingEnabled = false }()
	st := newStageTimer()
	st.Mark("head_recv")
	upstream := httptest.NewRecorder()
	mergeWorkerTiming(st, upstream.Result())
	assert.Contains(t, st.Header(), "head_recv;dur=")
	assert.False(t, strings.Contains(st.Header(), "worker_"),
		"no worker_ prefix expected when upstream emitted nothing")
}

// TestSetWorkerTimingHeader verifies the helper that copies the timer's
// stages onto an outgoing response under X-Otela-Worker-Timing.
func TestSetWorkerTimingHeader_Enabled(t *testing.T) {
	timingEnabled = true
	defer func() { timingEnabled = false }()

	st := newStageTimer()
	st.Mark("local_proxy")
	st.Mark("sglang_ttft")

	resp := &http.Response{Header: http.Header{}}
	setWorkerTimingHeader(st, resp)

	got := resp.Header.Get("X-Otela-Worker-Timing")
	assert.Contains(t, got, "local_proxy;dur=")
	assert.Contains(t, got, "sglang_ttft;dur=")
}

func TestSetWorkerTimingHeader_DisabledNoOp(t *testing.T) {
	timingEnabled = false
	st := newStageTimer()
	st.Mark("local_proxy")

	resp := &http.Response{Header: http.Header{}}
	setWorkerTimingHeader(st, resp)
	assert.Equal(t, "", resp.Header.Get("X-Otela-Worker-Timing"))
}

// helper to keep the test compiling regardless of test ordering
var _ = http.MethodGet
