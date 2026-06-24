package analytics

// EventName is the PostHog event name for a performance rollup.
const EventName = "node_perf_rollup"

func ratio(num, den int64) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func perSecond(count int64, windowSeconds int) float64 {
	if windowSeconds <= 0 {
		return 0
	}
	return float64(count) / float64(windowSeconds)
}

// buildEvent maps a Rollup into a sink-ready PostHog Event. distinct_id is the
// head node id; the model is attached both as the group value and a property.
func buildEvent(headID, groupType string, r Rollup) Event {
	props := map[string]interface{}{
		"model":               r.Model,
		"service":             r.Service,
		"worker_peer_id":      r.WorkerPeerID,
		"provider_id":         r.ProviderID,
		"gpu_model":           r.GPUModel,
		"gpu_count":           r.GPUCount,
		"head_peer_id":        headID,
		"window_seconds":      r.WindowSeconds,
		"request_count":       r.RequestCount,
		"error_count":         r.ErrorCount,
		"requests_per_second": perSecond(r.RequestCount, r.WindowSeconds),
		"concurrency_avg":     r.ConcurrencyAvg,
		"concurrency_max":     r.ConcurrencyMax,
		"input_tokens_sum":    r.InputTokensSum,
		"output_tokens_sum":   r.OutputTokensSum,
		"cached_tokens_sum":   r.CachedTokensSum,
		"total_tokens_sum":    r.TotalTokensSum,
		"gpu_ms_sum":          r.GPUMsSum,
		"io_ratio":            ratio(r.OutputTokensSum, r.InputTokensSum),
		"cache_hit_ratio":     ratio(r.CachedTokensSum, r.InputTokensSum),
		"latency_ms_p50":      r.LatencyP50,
		"latency_ms_p95":      r.LatencyP95,
		"latency_ms_avg":      r.LatencyAvg,
		"ttft_ms_p50":         r.TTFTP50,
		"ttft_ms_p95":         r.TTFTP95,
	}
	return Event{
		DistinctID: headID,
		Event:      EventName,
		GroupType:  groupType,
		Group:      r.Model,
		Properties: props,
	}
}
