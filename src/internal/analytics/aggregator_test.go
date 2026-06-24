package analytics

import (
	"math/rand"
	"testing"
)

func sample(model, worker string, status int, in, out, cached int64, lat float64, conc int) Sample {
	return Sample{
		Service: "llm", Model: model, WorkerPeerID: worker, ProviderID: "prov-" + worker,
		GPUModel: "NVIDIA H100", GPUCount: 1, Status: status,
		LatencyMs: lat, InputTokens: in, OutputTokens: out, CachedTokens: cached,
		TotalTokens: in + out, GPUMs: 7, Concurrency: conc,
	}
}

func TestAggregatorAccumulatesPerKey(t *testing.T) {
	a := newAggregator(512, 60, rand.New(rand.NewSource(1)))
	a.add(sample("m1", "w1", 200, 100, 50, 10, 10.0, 2))
	a.add(sample("m1", "w1", 500, 200, 0, 0, 20.0, 4))
	a.add(sample("m2", "w1", 200, 10, 10, 0, 5.0, 1))

	rollups := a.flush()
	if len(rollups) != 2 {
		t.Fatalf("got %d rollups, want 2", len(rollups))
	}
	var r1 *Rollup
	for i := range rollups {
		if rollups[i].Model == "m1" {
			r1 = &rollups[i]
		}
	}
	if r1 == nil {
		t.Fatal("missing m1 rollup")
	}
	if r1.RequestCount != 2 || r1.ErrorCount != 1 {
		t.Fatalf("m1 counts: req=%d err=%d", r1.RequestCount, r1.ErrorCount)
	}
	if r1.InputTokensSum != 300 || r1.OutputTokensSum != 50 || r1.CachedTokensSum != 10 {
		t.Fatalf("m1 token sums wrong: %+v", r1)
	}
	if r1.ConcurrencyMax != 4 || r1.ConcurrencyAvg != 3 {
		t.Fatalf("m1 concurrency: max=%d avg=%v", r1.ConcurrencyMax, r1.ConcurrencyAvg)
	}
	if r1.WindowSeconds != 60 || r1.ProviderID != "prov-w1" || r1.GPUModel != "NVIDIA H100" {
		t.Fatalf("m1 dims wrong: %+v", r1)
	}
}

func TestAggregatorFlushResets(t *testing.T) {
	a := newAggregator(512, 60, rand.New(rand.NewSource(1)))
	a.add(sample("m1", "w1", 200, 1, 1, 0, 1.0, 1))
	if len(a.flush()) != 1 {
		t.Fatal("first flush should have 1 rollup")
	}
	if len(a.flush()) != 0 {
		t.Fatal("second flush should be empty after reset")
	}
}
