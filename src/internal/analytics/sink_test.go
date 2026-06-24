package analytics

import "testing"

func TestFakeSinkRecords(t *testing.T) {
	fs := &fakeSink{}
	fs.Enqueue(Event{DistinctID: "head1", Event: "node_perf_rollup", GroupType: "model", Group: "m1"})
	fs.Enqueue(Event{DistinctID: "head1", Event: "node_perf_rollup", GroupType: "model", Group: "m2"})
	got := fs.Snapshot()
	if len(got) != 2 || got[0].Group != "m1" || got[1].Group != "m2" {
		t.Fatalf("unexpected events: %+v", got)
	}
	fs.Close() // must not panic
}

func TestNewPostHogSinkEmptyKey(t *testing.T) {
	// Empty key short-circuits to a silent no-op sink; constructor must not error.
	s, err := NewPostHogSink(Config{PostHogHost: "https://us.i.posthog.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s.Enqueue(Event{Event: "node_perf_rollup", Properties: map[string]interface{}{"x": 1}})
	s.Close()
}
