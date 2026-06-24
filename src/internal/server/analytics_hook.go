package server

import (
	"strconv"
	"time"

	"opentela/internal/analytics"
	"opentela/internal/common"
	"opentela/internal/protocol"

	"github.com/spf13/viper"
)

// analyticsRecorder is the process-wide head-node analytics recorder. nil when
// analytics is disabled; all *analytics.Recorder methods are nil-safe.
var analyticsRecorder *analytics.Recorder

// analyticsParseUsageBody caches analytics.parse_usage_body so the request hot
// path does not read Viper per request. Set in initAnalytics.
var analyticsParseUsageBody bool

func getAnalytics() *analytics.Recorder { return analyticsRecorder }

// initAnalytics constructs the recorder from Viper config when enabled. A nil
// recorder (disabled, or missing key) makes every hook a no-op.
func initAnalytics() {
	cfg := analytics.Config{
		Enabled:              viper.GetBool("analytics.enabled"),
		PostHogAPIKey:        viper.GetString("analytics.posthog_api_key"),
		PostHogHost:          viper.GetString("analytics.posthog_host"),
		FlushIntervalSeconds: viper.GetInt("analytics.flush_interval_seconds"),
		ParseUsageBody:       viper.GetBool("analytics.parse_usage_body"),
		MaxLatencySamples:    viper.GetInt("analytics.max_latency_samples"),
	}
	analyticsParseUsageBody = cfg.ParseUsageBody
	if !cfg.Enabled {
		return
	}
	if cfg.PostHogAPIKey == "" {
		common.Logger.Warn("analytics.enabled is set but analytics.posthog_api_key is empty; analytics disabled")
		return
	}
	sink, err := analytics.NewPostHogSink(cfg)
	if err != nil {
		common.Logger.Errorf("analytics: failed to initialise PostHog sink: %v", err)
		return
	}
	analyticsRecorder = analytics.New(cfg, protocol.MyID, sink)
	common.Logger.Infof("Analytics enabled: per-model rollups every %ds to %s", cfg.FlushIntervalSeconds, cfg.PostHogHost)
	if !viper.GetBool("observability.timing_headers") {
		common.Logger.Warn("analytics: ttft_ms_* will be 0 because observability.timing_headers is disabled")
	}
}

// stopAnalytics flushes and shuts down the recorder (nil-safe).
func stopAnalytics() { analyticsRecorder.Stop() }

// buildProxySample assembles an analytics.Sample from the buffered response.
// Token splits are read from the buffered body only for non-streaming,
// non-truncated responses (see retryableResponseWriter).
func buildProxySample(service, model, worker string, rw *retryableResponseWriter, st *stageTimer, conc int, latency time.Duration) analytics.Sample {
	status := rw.statusCode
	if status == 0 {
		status = 200
	}
	s := analytics.Sample{
		Service:      service,
		Model:        model,
		WorkerPeerID: worker,
		Status:       status,
		LatencyMs:    float64(latency.Microseconds()) / 1000.0,
		TTFTMs:       stageDuration(st, "head_p2p_to_worker_first_byte"),
		Concurrency:  conc,
	}
	if p, err := protocol.GetPeerFromTable(worker); err == nil {
		s.ProviderID = p.ProviderID
		s.GPUCount = len(p.Hardware.GPUs)
		if len(p.Hardware.GPUs) > 0 {
			s.GPUModel = p.Hardware.GPUs[0].Name
		}
	}
	if v := rw.Header().Get("X-Usage-GPU-Ms"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			s.GPUMs = ms
		}
	}
	if analyticsParseUsageBody && !rw.streaming && !rw.bufferExceeded {
		if u, ok := analytics.ParseUsage(rw.body.Bytes()); ok {
			s.InputTokens = u.InputTokens
			s.OutputTokens = u.OutputTokens
			s.TotalTokens = u.TotalTokens
			s.CachedTokens = u.CachedTokens
		}
	}
	return s
}

// stageDuration returns the recorded ms for a named stage, or 0 if absent
// (e.g. when observability.timing_headers is disabled).
func stageDuration(st *stageTimer, name string) float64 {
	for _, s := range st.stages {
		if s.name == name {
			return s.ms
		}
	}
	return 0
}
