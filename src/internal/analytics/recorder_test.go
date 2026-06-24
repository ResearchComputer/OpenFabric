package analytics

import "testing"

func TestRecorderNilSafe(t *testing.T) {
	var r *Recorder // disabled path
	if r.Begin("llm", "m1", "w1") != 0 {
		t.Fatal("nil Begin should return 0")
	}
	r.End("llm", "m1", "w1") // must not panic
	r.Observe(Sample{Model: "m1"})
	r.Stop()
}

func TestRecorderConcurrencyCounting(t *testing.T) {
	r := New(Config{}.Normalize(), "head1", &fakeSink{})
	defer r.Stop()
	if n := r.Begin("llm", "m1", "w1"); n != 1 {
		t.Fatalf("first Begin = %d, want 1", n)
	}
	if n := r.Begin("llm", "m1", "w1"); n != 2 {
		t.Fatalf("second Begin = %d, want 2", n)
	}
	r.End("llm", "m1", "w1")
	if n := r.Begin("llm", "m1", "w1"); n != 2 {
		t.Fatalf("Begin after one End = %d, want 2", n)
	}
}

func TestRecorderStopIdempotent(t *testing.T) {
	r := New(Config{}.Normalize(), "head1", &fakeSink{})
	r.Stop()
	r.Stop() // second call must not panic
}

func TestRecorderObserveFlushesEvent(t *testing.T) {
	fs := &fakeSink{}
	r := New(Config{}.Normalize(), "head1", fs)
	defer r.Stop()
	r.Observe(Sample{Service: "llm", Model: "m1", WorkerPeerID: "w1", Status: 200,
		InputTokens: 100, OutputTokens: 50, CachedTokens: 10, TotalTokens: 150, LatencyMs: 12, Concurrency: 1})
	r.flushNow()

	events := fs.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]
	if e.Event != EventName || e.Group != "m1" || e.GroupType != "model" {
		t.Fatalf("event shape wrong: %+v", e)
	}
	if e.Properties["io_ratio"].(float64) != 0.5 {
		t.Fatalf("io_ratio = %v, want 0.5", e.Properties["io_ratio"])
	}
}
