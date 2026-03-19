//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opentela/internal/solana"
	"opentela/internal/usage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestBillingPipeline_EndToEnd exercises the full billing accounting flow
// without any network or Solana dependencies:
//
//   HTTP response (mock) → extract usage headers → store records →
//   aggregate → reconcile (dual-attestation) → rate lookup → settlement amount
//
// Run: make test TEST_PKGS="./tests/integration/..." GOARGS="-tags=integration -count=1"
// ---------------------------------------------------------------------------

func TestBillingPipeline_EndToEnd(t *testing.T) {
	// --- 1. Extract usage from mock HTTP responses ---

	headResp := mockResponseWithUsageHeaders(map[string]string{
		"X-Usage-Tokens": "1500",
		"X-Usage-GPU-Ms": "4200",
	})
	headMetrics, err := usage.ExtractUsageMetrics(headResp)
	require.NoError(t, err)
	assert.Equal(t, int64(1500), headMetrics["tokens"])
	assert.Equal(t, int64(4200), headMetrics["gpu_ms"])

	workerResp := mockResponseWithUsageHeaders(map[string]string{
		"X-Usage-Tokens": "1480",
		"X-Usage-GPU-Ms": "4200",
	})
	workerMetrics, err := usage.ExtractUsageMetrics(workerResp)
	require.NoError(t, err)
	assert.Equal(t, int64(1480), workerMetrics["tokens"])

	// --- 2. Persist records to store ---

	storeDir := filepath.Join(t.TempDir(), "usage-store")
	store, err := usage.NewUsageStore(storeDir)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	now := time.Now().Unix()
	requestID := "test-req-001"

	headRecord := &usage.UsageRecord{
		RequestID:    requestID,
		Service:      "llm",
		ConsumerPeer: "head-node-1",
		ProviderPeer: "worker-node-1",
		MetricName:   "tokens",
		MetricValue:  headMetrics["tokens"],
		Timestamp:    now,
	}
	workerRecord := &usage.UsageRecord{
		RequestID:    requestID,
		Service:      "llm",
		ConsumerPeer: "head-node-1",
		ProviderPeer: "worker-node-1",
		MetricName:   "tokens",
		MetricValue:  workerMetrics["tokens"],
		Timestamp:    now,
	}

	require.NoError(t, store.SaveRecord(headRecord))

	got, err := store.GetRecord(requestID)
	require.NoError(t, err)
	assert.Equal(t, headRecord.MetricValue, got.MetricValue)

	// --- 3. Aggregate records ---

	config := &usage.BillingConfig{
		Enabled:             true,
		ValueThreshold:      10000,
		MaxIntervalMinutes:  60,
		DisputeThresholdPct: 10,
	}
	agg := usage.NewAggregator(config)

	// Simulate multiple requests accumulating
	for i := 0; i < 5; i++ {
		agg.AddRecord("worker-node-1", "llm", "tokens", 1500)
	}
	assert.Equal(t, int64(7500), agg.GetValue("worker-node-1", "llm", "tokens"))

	// Below threshold — should not flush yet
	assert.False(t, agg.ShouldFlush("worker-node-1", "llm", "tokens"))

	// Push over the threshold
	agg.AddRecord("worker-node-1", "llm", "tokens", 5000)
	assert.True(t, agg.ShouldFlush("worker-node-1", "llm", "tokens"))

	aggregate := agg.BuildAggregate("worker-node-1", "llm", "tokens")
	require.NotNil(t, aggregate)
	assert.Equal(t, int64(12500), aggregate.TotalValue)
	assert.Equal(t, int64(6), aggregate.RecordCount)

	// Bucket should be reset
	assert.Equal(t, int64(0), agg.GetValue("worker-node-1", "llm", "tokens"))

	// Save aggregate to store
	require.NoError(t, store.SaveAggregate(aggregate))

	// --- 4. Reconcile dual-attestation records ---

	resolved, err := usage.ReconcileRecords(headRecord, workerRecord, config.DisputeThresholdPct)
	require.NoError(t, err)
	assert.False(t, resolved.Disputed, "1.3%% difference should not be disputed at 10%% threshold")
	// Average of 1500 and 1480 = 1490
	assert.Equal(t, int64(1490), resolved.ResolvedValue)

	// --- 5. Rate lookup and settlement amount calculation ---

	rm := solana.NewRateManager(100, 2) // 100 per token, 2 per gpu_ms
	rm.SetRate(solana.Rate{
		ProviderAddress: "worker-node-1",
		Service:         "llm",
		MetricName:      "tokens",
		PricePerUnit:    50, // 50 OTELA base units per token
	})

	rate, err := rm.GetRate("worker-node-1", "llm", "tokens")
	require.NoError(t, err)
	assert.Equal(t, int64(50), rate.PricePerUnit)

	settlementAmount := resolved.ResolvedValue * rate.PricePerUnit
	assert.Equal(t, int64(74500), settlementAmount) // 1490 * 50

	// Verify unknown provider falls back to default
	defaultRate, err := rm.GetRate("unknown-provider", "llm", "tokens")
	require.NoError(t, err)
	assert.Equal(t, int64(100), defaultRate.PricePerUnit)

	t.Logf("Pipeline complete: extracted=%d tokens, reconciled=%d, settlement=%d base units",
		headMetrics["tokens"], resolved.ResolvedValue, settlementAmount)
}

// ---------------------------------------------------------------------------
// TestBillingPipeline_MultiProviderSettlement verifies that settlement
// amounts are correctly computed when multiple providers serve requests
// with different rates.
// ---------------------------------------------------------------------------

func TestBillingPipeline_MultiProviderSettlement(t *testing.T) {
	config := &usage.BillingConfig{
		Enabled:             true,
		ValueThreshold:      1000,
		MaxIntervalMinutes:  60,
		DisputeThresholdPct: 10,
	}

	providers := []struct {
		peerID       string
		service      string
		metric       string
		headValue    int64
		workerValue  int64
		pricePerUnit int64
	}{
		{"provider-A", "llm", "tokens", 2000, 2000, 50},   // exact match
		{"provider-B", "llm", "tokens", 1000, 1050, 80},   // small diff (5%)
		{"provider-C", "sandbox", "gpu_ms", 5000, 5000, 3}, // exact match, different metric
	}

	var totalSettlement int64

	for _, p := range providers {
		head := &usage.UsageRecord{
			RequestID:    fmt.Sprintf("req-%s", p.peerID),
			Service:      p.service,
			ConsumerPeer: "head-1",
			ProviderPeer: p.peerID,
			MetricName:   p.metric,
			MetricValue:  p.headValue,
			Timestamp:    time.Now().Unix(),
		}
		worker := &usage.UsageRecord{
			RequestID:    fmt.Sprintf("req-%s", p.peerID),
			Service:      p.service,
			ConsumerPeer: "head-1",
			ProviderPeer: p.peerID,
			MetricName:   p.metric,
			MetricValue:  p.workerValue,
			Timestamp:    time.Now().Unix(),
		}

		resolved, err := usage.ReconcileRecords(head, worker, config.DisputeThresholdPct)
		require.NoError(t, err)
		assert.False(t, resolved.Disputed, "provider %s should not be disputed", p.peerID)

		amount := resolved.ResolvedValue * p.pricePerUnit
		totalSettlement += amount

		t.Logf("Provider %s: resolved=%d * rate=%d = %d",
			p.peerID, resolved.ResolvedValue, p.pricePerUnit, amount)
	}

	// provider-A: 2000 * 50  = 100000
	// provider-B: 1025 * 80  = 82000
	// provider-C: 5000 * 3   = 15000
	assert.Equal(t, int64(197000), totalSettlement)
}

// ---------------------------------------------------------------------------
// TestBillingPipeline_DisputedRecordsExcluded verifies that disputed
// records are excluded from settlement totals.
// ---------------------------------------------------------------------------

func TestBillingPipeline_DisputedRecordsExcluded(t *testing.T) {
	config := &usage.BillingConfig{
		Enabled:             true,
		ValueThreshold:      1000,
		MaxIntervalMinutes:  60,
		DisputeThresholdPct: 10,
	}

	requests := []struct {
		id          string
		headValue   int64
		workerValue int64
		wantDispute bool
	}{
		{"req-ok-1", 1000, 1000, false},    // exact match
		{"req-ok-2", 2000, 2050, false},     // 2.5% diff
		{"req-dispute", 1000, 1500, true},   // 40% diff → disputed
		{"req-ok-3", 3000, 3000, false},     // exact match
	}

	rm := solana.NewRateManager(10, 1)
	var totalSettlement int64

	for _, r := range requests {
		head := &usage.UsageRecord{
			RequestID: r.id, Service: "llm", ConsumerPeer: "head-1",
			ProviderPeer: "worker-1", MetricName: "tokens",
			MetricValue: r.headValue, Timestamp: time.Now().Unix(),
		}
		worker := &usage.UsageRecord{
			RequestID: r.id, Service: "llm", ConsumerPeer: "head-1",
			ProviderPeer: "worker-1", MetricName: "tokens",
			MetricValue: r.workerValue, Timestamp: time.Now().Unix(),
		}

		resolved, err := usage.ReconcileRecords(head, worker, config.DisputeThresholdPct)
		require.NoError(t, err)
		assert.Equal(t, r.wantDispute, resolved.Disputed, "request %s dispute mismatch", r.id)

		if !resolved.Disputed {
			rate, err := rm.GetRate("worker-1", "llm", "tokens")
			require.NoError(t, err)
			totalSettlement += resolved.ResolvedValue * rate.PricePerUnit
		}
	}

	// req-ok-1:  1000 * 10 = 10000
	// req-ok-2:  2025 * 10 = 20250
	// req-dispute: skipped
	// req-ok-3:  3000 * 10 = 30000
	assert.Equal(t, int64(60250), totalSettlement)
}

// ---------------------------------------------------------------------------
// TestBillingPipeline_StoreRoundTrip verifies that records survive a
// store close/reopen cycle (persistence).
// ---------------------------------------------------------------------------

func TestBillingPipeline_StoreRoundTrip(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "roundtrip-store")

	// Write
	store, err := usage.NewUsageStore(storeDir)
	require.NoError(t, err)

	record := &usage.UsageRecord{
		RequestID:    "persist-req-1",
		Service:      "llm",
		ConsumerPeer: "head-1",
		ProviderPeer: "worker-1",
		MetricName:   "tokens",
		MetricValue:  42000,
		Timestamp:    time.Now().Unix(),
	}
	require.NoError(t, store.SaveRecord(record))
	require.NoError(t, store.Close())

	// Reopen and read
	store2, err := usage.NewUsageStore(storeDir)
	require.NoError(t, err)
	t.Cleanup(func() { store2.Close() })

	got, err := store2.GetRecord("persist-req-1")
	require.NoError(t, err)
	assert.Equal(t, int64(42000), got.MetricValue)
	assert.Equal(t, "llm", got.Service)
	assert.Equal(t, "worker-1", got.ProviderPeer)
}

// ---------------------------------------------------------------------------
// TestBillingPipeline_AggregationTimeFlush verifies the time-ceiling
// flush trigger works end-to-end with the store.
// ---------------------------------------------------------------------------

func TestBillingPipeline_AggregationTimeFlush(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "time-flush-store")
	store, err := usage.NewUsageStore(storeDir)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	config := &usage.BillingConfig{
		Enabled:            true,
		ValueThreshold:     1000000, // very high — won't trigger by value
		MaxIntervalMinutes: 5,
	}
	agg := usage.NewAggregator(config)

	// Add a small amount — won't reach value threshold
	agg.AddRecord("worker-1", "llm", "tokens", 100)
	assert.False(t, agg.ShouldFlush("worker-1", "llm", "tokens"))

	// Simulate window started 10 minutes ago
	agg.SetWindowStart("worker-1", "llm", "tokens", time.Now().Add(-10*time.Minute).Unix())
	assert.True(t, agg.ShouldFlush("worker-1", "llm", "tokens"), "time ceiling should trigger flush")

	aggregate := agg.BuildAggregate("worker-1", "llm", "tokens")
	require.NotNil(t, aggregate)
	assert.Equal(t, int64(100), aggregate.TotalValue)

	require.NoError(t, store.SaveAggregate(aggregate))
}

// ---------------------------------------------------------------------------
// TestBillingPipeline_RateConfigFromYAML verifies that provider-specific
// rates loaded from YAML override defaults in settlement calculations.
// ---------------------------------------------------------------------------

func TestBillingPipeline_RateConfigFromYAML(t *testing.T) {
	dir := t.TempDir()
	ratesYAML := filepath.Join(dir, "rates.yaml")
	require.NoError(t, os.WriteFile(ratesYAML, []byte(`providers:
  - address: "ProviderWallet1"
    services:
      - name: "llm"
        metrics:
          - name: "tokens"
            price_per_1000: 5000
  - address: "ProviderWallet2"
    services:
      - name: "llm"
        metrics:
          - name: "tokens"
            price_per_1000: 8000
`), 0o644))

	rm := solana.NewRateManager(1, 1) // low defaults
	require.NoError(t, rm.LoadFromConfig(ratesYAML))

	r1, err := rm.GetRate("ProviderWallet1", "llm", "tokens")
	require.NoError(t, err)
	assert.Equal(t, int64(5), r1.PricePerUnit) // 5000/1000

	r2, err := rm.GetRate("ProviderWallet2", "llm", "tokens")
	require.NoError(t, err)
	assert.Equal(t, int64(8), r2.PricePerUnit) // 8000/1000

	// Same reconciled usage, different providers → different amounts
	resolved := &usage.ResolvedUsage{ResolvedValue: 10000}
	assert.Equal(t, int64(50000), resolved.ResolvedValue*r1.PricePerUnit)
	assert.Equal(t, int64(80000), resolved.ResolvedValue*r2.PricePerUnit)
}

// ---------------------------------------------------------------------------
// TestBillingPipeline_PendingRecordFiltering verifies that the store
// correctly filters pending records by consumer/provider/service/metric.
// ---------------------------------------------------------------------------

func TestBillingPipeline_PendingRecordFiltering(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "filter-store")
	store, err := usage.NewUsageStore(storeDir)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	now := time.Now().Unix()
	records := []*usage.UsageRecord{
		{RequestID: "r1", Service: "llm", ConsumerPeer: "head-1", ProviderPeer: "w1", MetricName: "tokens", MetricValue: 100, Timestamp: now},
		{RequestID: "r2", Service: "llm", ConsumerPeer: "head-1", ProviderPeer: "w1", MetricName: "tokens", MetricValue: 200, Timestamp: now},
		{RequestID: "r3", Service: "llm", ConsumerPeer: "head-1", ProviderPeer: "w2", MetricName: "tokens", MetricValue: 300, Timestamp: now},
		{RequestID: "r4", Service: "sandbox", ConsumerPeer: "head-1", ProviderPeer: "w1", MetricName: "gpu_ms", MetricValue: 5000, Timestamp: now},
	}
	for _, r := range records {
		require.NoError(t, store.SaveRecord(r))
	}

	// Filter: head-1/w1/llm/tokens → should get r1 and r2
	pending, err := store.GetPendingRecords("head-1", "w1", "llm", "tokens")
	require.NoError(t, err)
	assert.Len(t, pending, 2)

	var total int64
	for _, p := range pending {
		total += p.MetricValue
	}
	assert.Equal(t, int64(300), total) // 100 + 200

	// Filter: head-1/w1/sandbox/gpu_ms → should get r4
	gpuPending, err := store.GetPendingRecords("head-1", "w1", "sandbox", "gpu_ms")
	require.NoError(t, err)
	assert.Len(t, gpuPending, 1)
	assert.Equal(t, int64(5000), gpuPending[0].MetricValue)

	// Mark r1, r2 as aggregated — they should disappear
	require.NoError(t, store.MarkAggregated([]string{"r1", "r2"}))

	remaining, err := store.GetPendingRecords("head-1", "w1", "llm", "tokens")
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

// ---------------------------------------------------------------------------
// TestBillingPipeline_HeaderExtractionEdgeCases checks that the extractor
// handles malformed headers gracefully.
// ---------------------------------------------------------------------------

func TestBillingPipeline_HeaderExtractionEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		wantLen int
	}{
		{
			name:    "valid headers",
			headers: map[string]string{"X-Usage-Tokens": "500", "X-Usage-GPU-Ms": "1200"},
			wantLen: 2,
		},
		{
			name:    "non-numeric value skipped",
			headers: map[string]string{"X-Usage-Tokens": "abc", "X-Usage-GPU-Ms": "1200"},
			wantLen: 1,
		},
		{
			name:    "empty value skipped",
			headers: map[string]string{"X-Usage-Tokens": "", "X-Usage-GPU-Ms": "99"},
			wantLen: 1,
		},
		{
			name:    "non-usage headers ignored",
			headers: map[string]string{"Content-Type": "application/json", "X-Request-ID": "abc"},
			wantLen: 0,
		},
		{
			name:    "zero is valid",
			headers: map[string]string{"X-Usage-Tokens": "0"},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := mockResponseWithUsageHeaders(tt.headers)
			metrics, err := usage.ExtractUsageMetrics(resp)
			require.NoError(t, err)
			assert.Len(t, metrics, tt.wantLen)
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mockResponseWithUsageHeaders(headers map[string]string) *http.Response {
	rec := httptest.NewRecorder()
	for k, v := range headers {
		rec.Header().Set(k, v)
	}
	rec.WriteHeader(http.StatusOK)
	return rec.Result()
}
