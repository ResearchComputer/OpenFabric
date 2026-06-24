package analytics

import "testing"

func TestBuildEventPropertiesAndGroup(t *testing.T) {
	r := Rollup{
		Service: "llm", Model: "Qwen/Qwen3-8B", WorkerPeerID: "w1", ProviderID: "prov1",
		GPUModel: "NVIDIA H100", GPUCount: 2, WindowSeconds: 60,
		RequestCount: 10, ErrorCount: 1,
		InputTokensSum: 1000, OutputTokensSum: 500, CachedTokensSum: 250, TotalTokensSum: 1500,
		GPUMsSum: 7000, ConcurrencyMax: 5, ConcurrencyAvg: 2.5,
		LatencyP50: 12.0, LatencyP95: 30.0, LatencyAvg: 15.0, TTFTP50: 4.0, TTFTP95: 9.0,
	}
	e := buildEvent("head-xyz", "model", r)

	if e.Event != "node_perf_rollup" || e.DistinctID != "head-xyz" {
		t.Fatalf("event/distinct wrong: %+v", e)
	}
	if e.GroupType != "model" || e.Group != "Qwen/Qwen3-8B" {
		t.Fatalf("group wrong: type=%q group=%q", e.GroupType, e.Group)
	}
	if e.Properties["io_ratio"].(float64) != 0.5 {
		t.Fatalf("io_ratio = %v, want 0.5", e.Properties["io_ratio"])
	}
	if e.Properties["cache_hit_ratio"].(float64) != 0.25 {
		t.Fatalf("cache_hit_ratio = %v, want 0.25", e.Properties["cache_hit_ratio"])
	}
	if e.Properties["requests_per_second"].(float64) != 10.0/60.0 {
		t.Fatalf("rps = %v", e.Properties["requests_per_second"])
	}
	if e.Properties["gpu_model"].(string) != "NVIDIA H100" || e.Properties["gpu_count"].(int) != 2 {
		t.Fatalf("gpu props wrong: %+v", e.Properties)
	}
	if e.Properties["concurrency_max"].(int) != 5 {
		t.Fatalf("concurrency_max wrong: %v", e.Properties["concurrency_max"])
	}
}

func TestBuildEventZeroInputNoDivideByZero(t *testing.T) {
	e := buildEvent("h", "model", Rollup{Model: "m", RequestCount: 0, WindowSeconds: 60})
	if e.Properties["io_ratio"].(float64) != 0 || e.Properties["cache_hit_ratio"].(float64) != 0 {
		t.Fatal("ratios with zero input must be 0, not NaN")
	}
	if e.Properties["requests_per_second"].(float64) != 0 {
		t.Fatal("rps with zero requests must be 0")
	}
}
