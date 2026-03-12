# Metrics Aggregation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add federated Prometheus metrics aggregation so the head node's `/metrics` endpoint exposes both OpenTela operational metrics and scraped worker service metrics with per-peer labels.

**Architecture:** A new `internal/metrics` package with three components: a `MetricsScraper` that periodically pulls `/metrics` from connected workers via libp2p, a `Relabeler` that injects peer metadata labels and the `otela_node_` prefix, and an `AggregatedCollector` that implements `prometheus.Collector` (unchecked) to serve everything on the existing `/metrics` endpoint. Config defaults are wired through Viper.

**Tech Stack:** Go, `prometheus/client_golang`, `prometheus/common/expfmt`, `libp2p/go-libp2p-http`, Viper

**Spec:** `docs/superpowers/specs/2026-03-12-metrics-aggregation-design.md`

---

## Chunk 1: Core Metrics Package

### Task 1: Relabeler — Label Injection and Namespacing

**Files:**
- Create: `src/internal/metrics/relabeler.go`
- Test: `src/internal/metrics/relabeler_test.go`

The relabeler takes parsed Prometheus metric families and injects peer metadata labels + the `otela_node_` prefix. This is a pure function with no dependencies, so we build it first.

- [ ] **Step 1: Write the failing test for Relabel**

```go
// src/internal/metrics/relabeler_test.go
package metrics

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelabel_PrefixesMetricName(t *testing.T) {
	name := "vllm_requests_total"
	mf := &dto.MetricFamily{
		Name: &name,
		Metric: []*dto.Metric{
			{},
		},
	}

	peerLabels := map[string]string{
		"peer_id":     "12D3KooWTest",
		"provider_id": "otela-abc",
	}

	result := Relabel([]*dto.MetricFamily{mf}, peerLabels)

	require.Len(t, result, 1)
	assert.Equal(t, "otela_node_vllm_requests_total", result[0].GetName())
	// Check labels injected
	require.Len(t, result[0].Metric, 1)
	labelMap := make(map[string]string)
	for _, lp := range result[0].Metric[0].Label {
		labelMap[lp.GetName()] = lp.GetValue()
	}
	assert.Equal(t, "12D3KooWTest", labelMap["peer_id"])
	assert.Equal(t, "otela-abc", labelMap["provider_id"])
}

func TestRelabel_PreservesExistingLabels(t *testing.T) {
	name := "http_requests_total"
	labelName := "method"
	labelValue := "GET"
	mf := &dto.MetricFamily{
		Name: &name,
		Metric: []*dto.Metric{
			{
				Label: []*dto.LabelPair{
					{Name: &labelName, Value: &labelValue},
				},
			},
		},
	}

	peerLabels := map[string]string{"peer_id": "peer1"}
	result := Relabel([]*dto.MetricFamily{mf}, peerLabels)

	labels := result[0].Metric[0].Label
	assert.Len(t, labels, 2) // original + peer_id
	labelMap := make(map[string]string)
	for _, lp := range labels {
		labelMap[lp.GetName()] = lp.GetValue()
	}
	assert.Equal(t, "GET", labelMap["method"])
	assert.Equal(t, "peer1", labelMap["peer_id"])
}

func TestRelabel_EmptyInput(t *testing.T) {
	result := Relabel(nil, map[string]string{"peer_id": "p1"})
	assert.Empty(t, result)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src && go test ./internal/metrics/... -run TestRelabel -v`
Expected: FAIL — `Relabel` not defined

- [ ] **Step 3: Write minimal implementation**

```go
// src/internal/metrics/relabeler.go
package metrics

import (
	"strings"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

const metricPrefix = "otela_node_"

// Relabel takes parsed metric families and returns new copies with:
// - metric names prefixed with "otela_node_"
// - peer metadata labels injected into every metric
func Relabel(families []*dto.MetricFamily, peerLabels map[string]string) []*dto.MetricFamily {
	if len(families) == 0 {
		return nil
	}

	// Pre-build label pairs from peerLabels
	extraLabels := make([]*dto.LabelPair, 0, len(peerLabels))
	for k, v := range peerLabels {
		extraLabels = append(extraLabels, &dto.LabelPair{
			Name:  proto.String(k),
			Value: proto.String(v),
		})
	}

	result := make([]*dto.MetricFamily, 0, len(families))
	for _, mf := range families {
		prefixed := metricPrefix + sanitizeMetricName(mf.GetName())
		newMF := &dto.MetricFamily{
			Name: &prefixed,
			Help: mf.Help,
			Type: mf.Type,
		}
		newMF.Metric = make([]*dto.Metric, len(mf.Metric))
		for i, m := range mf.Metric {
			newLabels := make([]*dto.LabelPair, 0, len(m.Label)+len(extraLabels))
			newLabels = append(newLabels, m.Label...)
			newLabels = append(newLabels, extraLabels...)
			newMF.Metric[i] = &dto.Metric{
				Label:     newLabels,
				Gauge:     m.Gauge,
				Counter:   m.Counter,
				Summary:   m.Summary,
				Untyped:   m.Untyped,
				Histogram: m.Histogram,
				TimestampMs: m.TimestampMs,
			}
		}
		result = append(result, newMF)
	}
	return result
}

// sanitizeMetricName replaces characters not valid in Prometheus metric names.
// Colons are valid in metric names (used by recording rules / vLLM), so keep them.
func sanitizeMetricName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ':' {
			return r
		}
		return '_'
	}, name)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src && go test ./internal/metrics/... -run TestRelabel -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/metrics/relabeler.go internal/metrics/relabeler_test.go
git commit -m "feat(metrics): add relabeler for peer label injection and metric namespacing"
```

---

### Task 2: MetricsScraper — Periodic Worker Scraping

**Files:**
- Create: `src/internal/metrics/scraper.go`
- Test: `src/internal/metrics/scraper_test.go`

The scraper periodically fetches `/metrics` from connected workers, parses Prometheus text format, and caches results. It depends on the relabeler from Task 1.

- [ ] **Step 1: Write the failing test for scraper parsing and caching**

```go
// src/internal/metrics/scraper_test.go
package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPeerProvider implements PeerProvider for testing.
type mockPeerProvider struct {
	peers []PeerInfo
}

func (m *mockPeerProvider) GetScrapablePeers() []PeerInfo {
	return m.peers
}

func TestScrapeTarget_ParsesPrometheusFormat(t *testing.T) {
	// Start a test HTTP server that serves Prometheus metrics
	metricsBody := `# HELP http_requests_total Total requests
# TYPE http_requests_total counter
http_requests_total{method="GET"} 100
http_requests_total{method="POST"} 50
# HELP go_goroutines Number of goroutines
# TYPE go_goroutines gauge
go_goroutines 42
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, metricsBody)
	}))
	defer srv.Close()

	s := &MetricsScraper{
		metricsPath: "/metrics",
		timeout:     5 * time.Second,
		httpClient:  srv.Client(),
		cache:       &sync.Map{},
	}

	families, err := s.scrapeTarget(srv.URL)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(families), 2) // at least http_requests_total and go_goroutines
}

func TestScrapeTarget_ReturnsErrorOnTimeout(t *testing.T) {
	// Server that never responds
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer srv.Close()

	s := &MetricsScraper{
		metricsPath: "/metrics",
		timeout:     100 * time.Millisecond,
		httpClient:  srv.Client(),
		cache:       &sync.Map{},
	}

	_, err := s.scrapeTarget(srv.URL)
	assert.Error(t, err)
}

func TestCacheEviction_RemovesDisconnectedPeers(t *testing.T) {
	cache := &sync.Map{}
	cache.Store("peer-1", []*cachedMetric{})
	cache.Store("peer-2", []*cachedMetric{})

	activePeers := map[string]bool{"peer-1": true}
	evictStale(cache, activePeers)

	_, ok1 := cache.Load("peer-1")
	_, ok2 := cache.Load("peer-2")
	assert.True(t, ok1, "active peer should remain")
	assert.False(t, ok2, "disconnected peer should be evicted")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src && go test ./internal/metrics/... -run "TestScrapeTarget|TestCacheEviction" -v`
Expected: FAIL — types and functions not defined

- [ ] **Step 3: Write the scraper implementation**

```go
// src/internal/metrics/scraper.go
package metrics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"opentela/internal/common"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

// PeerInfo holds the metadata needed to scrape and relabel a peer's metrics.
type PeerInfo struct {
	ID         string            // libp2p peer ID
	Address    string            // scrape URL base (e.g. "libp2p://peerID")
	Labels     map[string]string // peer_id, provider_id, service, model, etc.
}

// PeerProvider abstracts discovery of scrape targets.
type PeerProvider interface {
	GetScrapablePeers() []PeerInfo
}

// cachedMetric is a timestamped snapshot of a peer's relabeled metric families.
type cachedMetric struct {
	families []*dto.MetricFamily
	updated  time.Time
}

// ScraperConfig holds scraper configuration.
type ScraperConfig struct {
	ScrapeInterval     time.Duration
	ScrapeTimeout      time.Duration
	MetricsPath        string
	MaxConcurrent      int
}

// MetricsScraper periodically scrapes /metrics from connected workers.
type MetricsScraper struct {
	provider    PeerProvider
	metricsPath string
	timeout     time.Duration
	maxConc     int
	httpClient  *http.Client
	cache       *sync.Map // map[peerID] -> *cachedMetric
	stopCh      chan struct{}

	// Prometheus metrics for self-monitoring (wired by SetSelfMetrics)
	scrapeErrors       *prometheus.CounterVec
	scrapeDuration     *prometheus.HistogramVec
	scrapeCycleDuration prometheus.Histogram
}

// NewMetricsScraper creates a new scraper.
func NewMetricsScraper(cfg ScraperConfig, provider PeerProvider, transport http.RoundTripper) *MetricsScraper {
	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.ScrapeTimeout,
	}
	return &MetricsScraper{
		provider:    provider,
		metricsPath: cfg.MetricsPath,
		timeout:     cfg.ScrapeTimeout,
		maxConc:     cfg.MaxConcurrent,
		httpClient:  client,
		cache:       &sync.Map{},
		stopCh:      make(chan struct{}),
		scrapeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "otela_scraper_errors_total",
			Help: "Total scrape failures per peer",
		}, []string{"peer_id"}),
		scrapeDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "otela_scraper_duration_seconds",
			Help:    "Per-peer scrape duration",
			Buckets: prometheus.DefBuckets,
		}, []string{"peer_id"}),
		scrapeCycleDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "otela_scraper_cycle_duration_seconds",
			Help:    "Total wall time for one full scrape cycle",
			Buckets: prometheus.DefBuckets,
		}),
	}
}

// Start begins the periodic scrape loop. Call Stop() to terminate.
func (s *MetricsScraper) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// Run once immediately
		s.scrapeAll()
		for {
			select {
			case <-ticker.C:
				s.scrapeAll()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop terminates the scrape loop.
func (s *MetricsScraper) Stop() {
	close(s.stopCh)
}

// GetCachedMetrics returns all cached, relabeled metric families.
func (s *MetricsScraper) GetCachedMetrics() []*dto.MetricFamily {
	var all []*dto.MetricFamily
	s.cache.Range(func(key, value any) bool {
		if cm, ok := value.(*cachedMetric); ok {
			all = append(all, cm.families...)
		}
		return true
	})
	return all
}

// GetSelfMetrics returns the scraper's own Prometheus collectors for registration.
func (s *MetricsScraper) GetSelfMetrics() []prometheus.Collector {
	return []prometheus.Collector{s.scrapeErrors, s.scrapeDuration, s.scrapeCycleDuration}
}

// scrapeAll runs one full scrape cycle across all peers.
func (s *MetricsScraper) scrapeAll() {
	cycleStart := time.Now()
	defer func() { s.scrapeCycleDuration.Observe(time.Since(cycleStart).Seconds()) }()

	peers := s.provider.GetScrapablePeers()

	// Build active set and evict stale entries
	active := make(map[string]bool, len(peers))
	for _, p := range peers {
		active[p.ID] = true
	}
	evictStale(s.cache, active)

	// Scrape concurrently with semaphore
	sem := make(chan struct{}, s.maxConc)
	var wg sync.WaitGroup

	for _, peer := range peers {
		wg.Add(1)
		sem <- struct{}{}
		go func(p PeerInfo) {
			defer wg.Done()
			defer func() { <-sem }()
			s.scrapePeer(p)
		}(peer)
	}
	wg.Wait()
}

func (s *MetricsScraper) scrapePeer(p PeerInfo) {
	start := time.Now()
	url := p.Address + s.metricsPath
	families, err := s.scrapeTarget(url)
	elapsed := time.Since(start)

	s.scrapeDuration.WithLabelValues(p.ID).Observe(elapsed.Seconds())

	if err != nil {
		common.Logger.Warnf("Scrape failed for peer %s: %v", p.ID, err)
		s.scrapeErrors.WithLabelValues(p.ID).Inc()
		// Keep stale cache entry if it exists
		return
	}

	// Relabel with peer metadata
	relabeled := Relabel(families, p.Labels)
	s.cache.Store(p.ID, &cachedMetric{
		families: relabeled,
		updated:  time.Now(),
	})
}

// scrapeTarget fetches and parses Prometheus metrics from a single URL.
func (s *MetricsScraper) scrapeTarget(baseURL string) ([]*dto.MetricFamily, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", string(expfmt.NewFormat(expfmt.TypeTextPlain)))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scraping %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scraping %s: status %d", baseURL, resp.StatusCode)
	}

	return parseMetrics(resp.Body, resp.Header)
}

// parseMetrics decodes Prometheus exposition format from a reader.
func parseMetrics(r io.Reader, header http.Header) ([]*dto.MetricFamily, error) {
	mediaType := expfmt.ResponseFormat(header)
	if mediaType == expfmt.FmtUnknown {
		mediaType = expfmt.NewFormat(expfmt.TypeTextPlain)
	}

	decoder := expfmt.NewDecoder(r, mediaType)
	var families []*dto.MetricFamily
	for {
		var mf dto.MetricFamily
		if err := decoder.Decode(&mf); err != nil {
			if err == io.EOF {
				break
			}
			return families, fmt.Errorf("decoding metrics: %w", err)
		}
		families = append(families, &mf)
	}
	return families, nil
}

// evictStale removes cache entries for peers not in the active set.
func evictStale(cache *sync.Map, activePeers map[string]bool) {
	cache.Range(func(key, _ any) bool {
		peerID, ok := key.(string)
		if !ok {
			return true
		}
		if !activePeers[peerID] {
			cache.Delete(key)
		}
		return true
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src && go test ./internal/metrics/... -run "TestScrapeTarget|TestCacheEviction" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/metrics/scraper.go internal/metrics/scraper_test.go
git commit -m "feat(metrics): add MetricsScraper for periodic worker metrics collection"
```

---

### Task 3: AggregatedCollector — Prometheus Collector Interface

**Files:**
- Create: `src/internal/metrics/collector.go`
- Test: `src/internal/metrics/collector_test.go`

The collector implements `prometheus.Collector` as an unchecked collector (empty `Describe`), combining scraped worker metrics with OpenTela operational metrics.

- [ ] **Step 1: Write the failing test**

```go
// src/internal/metrics/collector_test.go
package metrics

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// mockScraper implements MetricsSource for testing.
type mockScraper struct {
	families []*dto.MetricFamily
}

func (m *mockScraper) GetCachedMetrics() []*dto.MetricFamily {
	return m.families
}

func TestAggregatedCollector_DescribeSendsNothing(t *testing.T) {
	c := NewAggregatedCollector(&mockScraper{})
	ch := make(chan *prometheus.Desc, 10)
	c.Describe(ch)
	close(ch)
	assert.Empty(t, ch, "unchecked collector should send no descriptors")
}

func TestAggregatedCollector_CollectYieldsScrapedMetrics(t *testing.T) {
	name := "otela_node_test_gauge"
	gaugeValue := 42.0
	src := &mockScraper{
		families: []*dto.MetricFamily{
			{
				Name: &name,
				Type: dto.MetricType_GAUGE.Enum(),
				Metric: []*dto.Metric{
					{
						Gauge: &dto.Gauge{Value: &gaugeValue},
						Label: []*dto.LabelPair{
							{Name: proto.String("peer_id"), Value: proto.String("test-peer")},
						},
					},
				},
			},
		},
	}

	c := NewAggregatedCollector(src)
	ch := make(chan prometheus.Metric, 100)
	c.Collect(ch)
	close(ch)

	var collected []prometheus.Metric
	for m := range ch {
		collected = append(collected, m)
	}
	// Should have at least the scraped metric + operational metrics
	require.NotEmpty(t, collected)

	// Verify the scraped gauge is present
	found := false
	for _, m := range collected {
		dtoMetric := &dto.Metric{}
		_ = m.Write(dtoMetric)
		if dtoMetric.Gauge != nil && dtoMetric.Gauge.GetValue() == 42.0 {
			found = true
		}
	}
	assert.True(t, found, "should contain the scraped gauge metric")
}

func TestAggregatedCollector_RegistersWithoutError(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewAggregatedCollector(&mockScraper{})
	err := reg.Register(c)
	assert.NoError(t, err, "unchecked collector should register without error")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src && go test ./internal/metrics/... -run TestAggregatedCollector -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Write the collector implementation**

```go
// src/internal/metrics/collector.go
package metrics

import (
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
)

// MetricsSource provides cached metric families (implemented by MetricsScraper).
type MetricsSource interface {
	GetCachedMetrics() []*dto.MetricFamily
}

// AggregatedCollector implements prometheus.Collector as an unchecked collector.
// It yields scraped worker metrics (already relabeled) plus OpenTela operational metrics.
type AggregatedCollector struct {
	source MetricsSource

	// OpenTela operational metrics
	peersConnected prometheus.Gauge
	peersTotal     prometheus.Gauge
	scraperTargets prometheus.Gauge
}

// NewAggregatedCollector creates a new collector.
func NewAggregatedCollector(source MetricsSource) *AggregatedCollector {
	return &AggregatedCollector{
		source: source,
		peersConnected: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "otela_network_peers_connected",
			Help: "Number of currently connected peers",
		}),
		peersTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "otela_network_peers_total",
			Help: "Total known peers in the node table",
		}),
		scraperTargets: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "otela_scraper_targets",
			Help: "Number of peers being scraped for metrics",
		}),
	}
}

// Describe sends no descriptors — this is an unchecked collector
// because the set of metrics is dynamic (varies by which workers are connected).
func (c *AggregatedCollector) Describe(ch chan<- *prometheus.Desc) {
	// Intentionally empty: unchecked collector pattern.
	// See https://pkg.go.dev/github.com/prometheus/client_golang/prometheus#hdr-Custom_Collectors_and_constant_Metrics
}

// Collect yields all scraped worker metrics plus operational metrics.
func (c *AggregatedCollector) Collect(ch chan<- prometheus.Metric) {
	// Yield operational metrics
	ch <- c.peersConnected
	ch <- c.peersTotal
	ch <- c.scraperTargets

	// Yield scraped worker metrics
	for _, mf := range c.source.GetCachedMetrics() {
		for _, m := range mf.Metric {
			pm, err := metricFromDTO(mf, m)
			if err != nil {
				continue
			}
			ch <- pm
		}
	}
}

// SetNetworkStats updates the operational gauge values.
// Called periodically by the scraper or clock ticker.
func (c *AggregatedCollector) SetNetworkStats(connected, total int) {
	c.peersConnected.Set(float64(connected))
	c.peersTotal.Set(float64(total))
}

// SetScraperTargets updates the number of scrape targets.
func (c *AggregatedCollector) SetScraperTargets(n int) {
	c.scraperTargets.Set(float64(n))
}

// metricFromDTO converts a dto.MetricFamily + dto.Metric into a prometheus.Metric.
func metricFromDTO(mf *dto.MetricFamily, m *dto.Metric) (prometheus.Metric, error) {
	labelNames := make([]string, len(m.Label))
	labelValues := make([]string, len(m.Label))
	for i, lp := range m.Label {
		labelNames[i] = lp.GetName()
		labelValues[i] = lp.GetValue()
	}

	desc := prometheus.NewDesc(mf.GetName(), mf.GetHelp(), labelNames, nil)

	switch mf.GetType() {
	case dto.MetricType_COUNTER:
		if m.Counter != nil {
			return prometheus.MustNewConstMetric(desc, prometheus.CounterValue, m.Counter.GetValue(), labelValues...), nil
		}
	case dto.MetricType_GAUGE:
		if m.Gauge != nil {
			return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, m.Gauge.GetValue(), labelValues...), nil
		}
	case dto.MetricType_UNTYPED:
		if m.Untyped != nil {
			return prometheus.MustNewConstMetric(desc, prometheus.UntypedValue, m.Untyped.GetValue(), labelValues...), nil
		}
	case dto.MetricType_HISTOGRAM:
		if m.Histogram != nil {
			buckets := make(map[float64]uint64, len(m.Histogram.Bucket))
			for _, b := range m.Histogram.Bucket {
				buckets[b.GetUpperBound()] = b.GetCumulativeCount()
			}
			return prometheus.MustNewConstHistogram(
				desc,
				m.Histogram.GetSampleCount(),
				m.Histogram.GetSampleSum(),
				buckets,
				labelValues...,
			), nil
		}
	case dto.MetricType_SUMMARY:
		if m.Summary != nil {
			quantiles := make(map[float64]float64, len(m.Summary.Quantile))
			for _, q := range m.Summary.Quantile {
				quantiles[q.GetQuantile()] = q.GetValue()
			}
			return prometheus.MustNewConstSummary(
				desc,
				m.Summary.GetSampleCount(),
				m.Summary.GetSampleSum(),
				quantiles,
				labelValues...,
			), nil
		}
	}

	return prometheus.MustNewConstMetric(desc, prometheus.UntypedValue, 0, labelValues...), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src && go test ./internal/metrics/... -run TestAggregatedCollector -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/metrics/collector.go internal/metrics/collector_test.go
git commit -m "feat(metrics): add AggregatedCollector implementing unchecked prometheus.Collector"
```

---

## Chunk 2: Integration and Operational Metrics

### Task 4: Viper Config Defaults

**Files:**
- Modify: `src/entry/cmd/root.go:83-97` (inside `initConfig`)

Wire the new `metrics.*` config keys with defaults.

- [ ] **Step 1: Add config defaults to `initConfig` in `root.go`**

After the existing `viper.SetDefault("security.require_signed_binary", true)` block (line 99), add:

```go
	// Metrics aggregation configuration (opt-in, disabled by default)
	viper.SetDefault("metrics.aggregation_enabled", false)
	viper.SetDefault("metrics.scrape_interval_seconds", 30)
	viper.SetDefault("metrics.scrape_timeout_seconds", 5)
	viper.SetDefault("metrics.worker_metrics_path", "/metrics")
	viper.SetDefault("metrics.max_concurrent_scrapes", 10)
```

- [ ] **Step 2: Verify build succeeds**

Run: `cd src && make build`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
cd src && git add entry/cmd/root.go
git commit -m "feat(metrics): add Viper config defaults for metrics aggregation"
```

---

### Task 5: PeerProvider Adapter — Bridge Node Table to Scraper

**Files:**
- Create: `src/internal/metrics/peer_provider.go`
- Test: `src/internal/metrics/peer_provider_test.go`

This adapter implements `PeerProvider` by reading from the protocol node table. It translates `protocol.Peer` into `PeerInfo` with the correct libp2p address and peer labels.

- [ ] **Step 1: Write the failing test**

```go
// src/internal/metrics/peer_provider_test.go
package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildPeerLabels(t *testing.T) {
	labels := buildPeerLabels("peer123", "otela-abc", []ServiceInfo{
		{Name: "llm", Model: "Qwen/Qwen3-8B"},
	})

	assert.Equal(t, "peer123", labels["peer_id"])
	assert.Equal(t, "otela-abc", labels["provider_id"])
	assert.Equal(t, "llm", labels["service"])
	assert.Equal(t, "Qwen/Qwen3-8B", labels["model"])
}

func TestBuildPeerLabels_NoServices(t *testing.T) {
	labels := buildPeerLabels("peer123", "otela-abc", nil)
	assert.Equal(t, "peer123", labels["peer_id"])
	assert.Equal(t, "otela-abc", labels["provider_id"])
	_, hasService := labels["service"]
	assert.False(t, hasService)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src && go test ./internal/metrics/... -run TestBuildPeerLabels -v`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
// src/internal/metrics/peer_provider.go
package metrics

import (
	"opentela/internal/protocol"
	"strings"
)

// ServiceInfo holds extracted service metadata for labeling.
type ServiceInfo struct {
	Name  string
	Model string
}

// NodeTablePeerProvider implements PeerProvider using the protocol node table.
type NodeTablePeerProvider struct{}

// GetScrapablePeers returns PeerInfo for all connected peers with services.
func (p *NodeTablePeerProvider) GetScrapablePeers() []PeerInfo {
	table := protocol.GetConnectedPeers()
	if table == nil {
		return nil
	}

	var peers []PeerInfo
	for _, peer := range *table {
		// Skip self
		if peer.ID == protocol.MyID {
			continue
		}
		if len(peer.Service) == 0 {
			continue
		}

		services := extractServices(peer.Service)
		labels := buildPeerLabels(peer.ID, peer.ProviderID, services)

		peers = append(peers, PeerInfo{
			ID:      peer.ID,
			Address: "libp2p://" + peer.ID,
			Labels:  labels,
		})
	}
	return peers
}

// extractServices pulls service name and model from protocol.Service slices.
func extractServices(services []protocol.Service) []ServiceInfo {
	var infos []ServiceInfo
	for _, svc := range services {
		si := ServiceInfo{Name: svc.Name}
		for _, ig := range svc.IdentityGroup {
			parts := strings.SplitN(ig, "=", 2)
			if len(parts) == 2 && parts[0] == "model" {
				si.Model = parts[1]
			}
		}
		infos = append(infos, si)
	}
	return infos
}

// buildPeerLabels creates the label map for a peer's metrics.
func buildPeerLabels(peerID, providerID string, services []ServiceInfo) map[string]string {
	labels := map[string]string{
		"peer_id":     peerID,
		"provider_id": providerID,
	}
	// Use first service's name and model if available
	if len(services) > 0 {
		if services[0].Name != "" {
			labels["service"] = services[0].Name
		}
		if services[0].Model != "" {
			labels["model"] = services[0].Model
		}
	}
	return labels
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src && go test ./internal/metrics/... -run TestBuildPeerLabels -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/metrics/peer_provider.go internal/metrics/peer_provider_test.go
git commit -m "feat(metrics): add NodeTablePeerProvider bridging node table to scraper"
```

---

### Task 6: Server Integration — Wire Everything Together

**Files:**
- Modify: `src/internal/server/server.go:104-130`

Initialize the scraper, collector, and register on the Prometheus registry when `metrics.aggregation_enabled` is true.

- [ ] **Step 1: Add metrics initialization to `server.go`**

After the `protocol.GetCRDTStore()` call (line 105) and before the Gin router setup, add the scraper + collector initialization:

```go
	// Import at top: "opentela/internal/metrics" and p2phttp "github.com/libp2p/go-libp2p-http"

	// Metrics aggregation: scrape worker /metrics via libp2p and serve aggregated
	var metricsCollector *metrics.AggregatedCollector
	if viper.GetBool("metrics.aggregation_enabled") {
		node, _ := protocol.GetP2PNode(nil)
		scrapeTransport := &http.Transport{
			ResponseHeaderTimeout: time.Duration(viper.GetInt("metrics.scrape_timeout_seconds")) * time.Second,
			IdleConnTimeout:       30 * time.Second,
			MaxIdleConns:          50,
			MaxIdleConnsPerHost:   2,
		}
		scrapeTransport.RegisterProtocol("libp2p", p2phttp.NewTransport(node))

		cfg := metrics.ScraperConfig{
			ScrapeInterval: time.Duration(viper.GetInt("metrics.scrape_interval_seconds")) * time.Second,
			ScrapeTimeout:  time.Duration(viper.GetInt("metrics.scrape_timeout_seconds")) * time.Second,
			MetricsPath:    viper.GetString("metrics.worker_metrics_path"),
			MaxConcurrent:  viper.GetInt("metrics.max_concurrent_scrapes"),
		}
		provider := &metrics.NodeTablePeerProvider{}
		scraper := metrics.NewMetricsScraper(cfg, provider, scrapeTransport)
		metricsCollector = metrics.NewAggregatedCollector(scraper)
		prometheus.MustRegister(metricsCollector)
		for _, c := range scraper.GetSelfMetrics() {
			prometheus.MustRegister(c)
		}
		scraper.Start(cfg.ScrapeInterval)

		// Periodically update network stats gauges
		go func() {
			ticker := time.NewTicker(cfg.ScrapeInterval)
			defer ticker.Stop()
			for range ticker.C {
				connected := protocol.GetConnectedPeers()
				all := protocol.GetAllPeers()
				metricsCollector.SetNetworkStats(len(*connected), len(*all))
				metricsCollector.SetScraperTargets(len(provider.GetScrapablePeers()))
			}
		}()

		common.Logger.Infof("Metrics aggregation enabled: scraping workers every %ds", viper.GetInt("metrics.scrape_interval_seconds"))
	}
```

Also add the `"github.com/prometheus/client_golang/prometheus"` import to the import block.

- [ ] **Step 2: Verify build succeeds**

Run: `cd src && make build`
Expected: Build succeeds

- [ ] **Step 3: Verify tests still pass**

Run: `cd src && make test`
Expected: All existing tests pass

- [ ] **Step 4: Commit**

```bash
cd src && git add internal/server/server.go
git commit -m "feat(metrics): wire scraper and collector into server startup"
```

---

### Task 7: Routing Metrics Instrumentation

**Files:**
- Modify: `src/internal/server/proxy_handler.go`

Add Prometheus counters and histograms for request routing.

- [ ] **Step 1: Add routing metrics to `proxy_handler.go`**

Add these package-level variables after the existing `var` block:

```go
var (
	routingRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "otela_routing_requests_total",
			Help: "Total number of requests forwarded to workers",
		},
		[]string{"service", "status"},
	)
	routingRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "otela_routing_request_duration_seconds",
			Help:    "End-to-end forwarding latency",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service"},
	)
	routingFallbackTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "otela_routing_fallback_total",
			Help: "Number of times each fallback tier was used",
		},
		[]string{"service", "level"},
	)
)

func init() {
	prometheus.MustRegister(routingRequestsTotal, routingRequestDuration, routingFallbackTotal)
}
```

- [ ] **Step 2: Instrument `GlobalServiceForwardHandler`**

In `GlobalServiceForwardHandler`, after `serviceName` is set (line 272), add timing start:

```go
	routingStart := time.Now()
```

After the `selectCandidates` call (line 286), record fallback level:

```go
	routingFallbackTotal.WithLabelValues(serviceName, strconv.Itoa(fallbackLevel)).Inc()
```

After `proxy.ServeHTTP(streamWriter, c.Request)` (line 371), record request count and duration:

```go
	status := strconv.Itoa(c.Writer.Status())
	routingRequestsTotal.WithLabelValues(serviceName, status).Inc()
	routingRequestDuration.WithLabelValues(serviceName).Observe(time.Since(routingStart).Seconds())
```

- [ ] **Step 3: Verify build succeeds**

Run: `cd src && make build`
Expected: Build succeeds

- [ ] **Step 4: Run existing tests**

Run: `cd src && make test`
Expected: All tests pass

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/server/proxy_handler.go
git commit -m "feat(metrics): instrument request routing with Prometheus counters and histograms"
```

---

### Task 8: Full Integration Test

**Files:**
- Modify: `src/internal/metrics/collector_test.go` (add integration-style test)

- [ ] **Step 1: Add end-to-end test for the full pipeline**

Append to `collector_test.go`:

```go
func TestFullPipeline_ScrapeRelabelCollect(t *testing.T) {
	// Serve fake Prometheus metrics
	metricsBody := `# TYPE test_counter counter
test_counter{env="prod"} 99
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, metricsBody)
	}))
	defer srv.Close()

	// Create a scraper pointing at the test server
	provider := &mockPeerProvider{
		peers: []PeerInfo{
			{
				ID:      "test-peer-1",
				Address: srv.URL,
				Labels:  map[string]string{"peer_id": "test-peer-1", "provider_id": "otela-test"},
			},
		},
	}
	cfg := ScraperConfig{
		ScrapeInterval: time.Second,
		ScrapeTimeout:  5 * time.Second,
		MetricsPath:    "",
		MaxConcurrent:  5,
	}
	scraper := NewMetricsScraper(cfg, provider, http.DefaultTransport)

	// Run one scrape cycle directly
	scraper.scrapeAll()

	// Create collector and gather
	collector := NewAggregatedCollector(scraper)
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(collector))

	families, err := reg.Gather()
	require.NoError(t, err)

	// Find our relabeled metric
	found := false
	for _, mf := range families {
		if mf.GetName() == "otela_node_test_counter" {
			found = true
			require.Len(t, mf.Metric, 1)
			// Should have original "env" label + peer labels
			labelMap := make(map[string]string)
			for _, lp := range mf.Metric[0].Label {
				labelMap[lp.GetName()] = lp.GetValue()
			}
			assert.Equal(t, "prod", labelMap["env"])
			assert.Equal(t, "test-peer-1", labelMap["peer_id"])
			assert.Equal(t, "otela-test", labelMap["provider_id"])
		}
	}
	assert.True(t, found, "should find otela_node_test_counter in gathered metrics")
}
```

Add imports: `"fmt"`, `"net/http"`, `"net/http/httptest"`, `"time"`.

- [ ] **Step 2: Run the full test suite**

Run: `cd src && go test ./internal/metrics/... -v`
Expected: All tests PASS

- [ ] **Step 3: Run the full project test suite**

Run: `cd src && make test`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
cd src && git add internal/metrics/collector_test.go
git commit -m "test(metrics): add full pipeline integration test for scrape-relabel-collect"
```

---

### Task 9: Run Lint and Final Verification

- [ ] **Step 1: Run lint**

Run: `cd src && make lint`
Expected: No errors (fix any that appear)

- [ ] **Step 2: Run full check**

Run: `cd src && make check`
Expected: All tests pass, no lint errors

- [ ] **Step 3: Final commit if any lint fixes were needed**

```bash
cd src && git add -A && git commit -m "chore: fix lint issues in metrics package"
```
