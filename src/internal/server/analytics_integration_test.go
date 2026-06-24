package server

import (
	"sync"
	"testing"
	"time"

	"opentela/internal/analytics"
)

// captureSink implements analytics.Sink for assertions.
type captureSink struct {
	mu     sync.Mutex
	events []analytics.Event
}

func (c *captureSink) Enqueue(e analytics.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}
func (c *captureSink) Close() {}
func (c *captureSink) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func TestRecorderEmitsRollupForObservedRequest(t *testing.T) {
	analyticsParseUsageBody = true
	t.Cleanup(func() { analyticsParseUsageBody = false })

	sink := &captureSink{}
	analyticsRecorder = analytics.New(analytics.Config{}.Normalize(), "head-test", sink)
	t.Cleanup(func() {
		analyticsRecorder.Stop()
		analyticsRecorder = nil
	})

	rw := newRetryableResponseWriter(nil, false, 1<<20)
	rw.statusCode = 200
	rw.body.WriteString(`{"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}`)

	conc := getAnalytics().Begin("llm", "m1", "w1")
	getAnalytics().End("llm", "m1", "w1")
	getAnalytics().Observe(buildProxySample("llm", "m1", "w1", rw, &stageTimer{}, conc, 100*time.Millisecond))

	analyticsRecorder.FlushNow()
	analyticsRecorder.Stop()

	if sink.count() != 1 {
		t.Fatalf("got %d events, want 1", sink.count())
	}
	if sink.events[0].Group != "m1" || sink.events[0].Event != analytics.EventName {
		t.Fatalf("unexpected event: %+v", sink.events[0])
	}
	if sink.events[0].Properties["io_ratio"].(float64) != 0.5 {
		t.Fatalf("io_ratio = %v, want 0.5", sink.events[0].Properties["io_ratio"])
	}
}
