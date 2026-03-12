package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

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
	require.NotEmpty(t, collected)

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

func TestFullPipeline_ScrapeRelabelCollect(t *testing.T) {
	metricsBody := `# TYPE test_counter counter
test_counter{env="prod"} 99
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, metricsBody)
	}))
	defer srv.Close()

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

	scraper.scrapeAll()

	collector := NewAggregatedCollector(scraper)
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(collector))

	families, err := reg.Gather()
	require.NoError(t, err)

	found := false
	for _, mf := range families {
		if mf.GetName() == "otela_node_test_counter" {
			found = true
			require.Len(t, mf.Metric, 1)
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

func TestAggregatedCollector_SetNetworkStats(t *testing.T) {
	c := NewAggregatedCollector(&mockScraper{})
	c.SetNetworkStats(5, 10)

	ch := make(chan prometheus.Metric, 100)
	c.Collect(ch)
	close(ch)

	var foundConnected, foundTotal bool
	for m := range ch {
		d := &dto.Metric{}
		require.NoError(t, m.Write(d))

		desc := m.Desc().String()
		if d.Gauge != nil {
			switch {
			case contains(desc, "otela_network_peers_connected"):
				assert.Equal(t, 5.0, d.Gauge.GetValue())
				foundConnected = true
			case contains(desc, "otela_network_peers_total"):
				assert.Equal(t, 10.0, d.Gauge.GetValue())
				foundTotal = true
			}
		}
	}
	assert.True(t, foundConnected, "should find peersConnected gauge with value 5")
	assert.True(t, foundTotal, "should find peersTotal gauge with value 10")
}

func TestAggregatedCollector_SetScraperTargets(t *testing.T) {
	c := NewAggregatedCollector(&mockScraper{})
	c.SetScraperTargets(3)

	ch := make(chan prometheus.Metric, 100)
	c.Collect(ch)
	close(ch)

	var found bool
	for m := range ch {
		d := &dto.Metric{}
		require.NoError(t, m.Write(d))

		desc := m.Desc().String()
		if d.Gauge != nil && contains(desc, "otela_scraper_targets") {
			assert.Equal(t, 3.0, d.Gauge.GetValue())
			found = true
		}
	}
	assert.True(t, found, "should find scraperTargets gauge with value 3")
}

// contains checks if substr is present in s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestMetricFromDTO_Counter(t *testing.T) {
	val := 42.0
	mf := &dto.MetricFamily{
		Name: proto.String("test_counter"),
		Help: proto.String("A test counter"),
		Type: dto.MetricType_COUNTER.Enum(),
		Metric: []*dto.Metric{
			{
				Counter: &dto.Counter{Value: &val},
				Label: []*dto.LabelPair{
					{Name: proto.String("env"), Value: proto.String("test")},
				},
			},
		},
	}

	pm, err := metricFromDTO(mf, mf.Metric[0])
	require.NoError(t, err)
	require.NotNil(t, pm)

	d := &dto.Metric{}
	require.NoError(t, pm.Write(d))
	assert.Equal(t, 42.0, d.Counter.GetValue())
	assert.Equal(t, "env", d.Label[0].GetName())
	assert.Equal(t, "test", d.Label[0].GetValue())
}

func TestMetricFromDTO_Histogram(t *testing.T) {
	sampleCount := uint64(10)
	sampleSum := 55.5
	upperBound := 1.0
	cumulativeCount := uint64(7)

	mf := &dto.MetricFamily{
		Name: proto.String("test_histogram"),
		Help: proto.String("A test histogram"),
		Type: dto.MetricType_HISTOGRAM.Enum(),
		Metric: []*dto.Metric{
			{
				Histogram: &dto.Histogram{
					SampleCount: &sampleCount,
					SampleSum:   &sampleSum,
					Bucket: []*dto.Bucket{
						{
							UpperBound:      &upperBound,
							CumulativeCount: &cumulativeCount,
						},
					},
				},
			},
		},
	}

	pm, err := metricFromDTO(mf, mf.Metric[0])
	require.NoError(t, err)
	require.NotNil(t, pm)

	d := &dto.Metric{}
	require.NoError(t, pm.Write(d))
	assert.Equal(t, uint64(10), d.Histogram.GetSampleCount())
	assert.Equal(t, 55.5, d.Histogram.GetSampleSum())
	require.Len(t, d.Histogram.Bucket, 1)
	assert.Equal(t, 1.0, d.Histogram.Bucket[0].GetUpperBound())
	assert.Equal(t, uint64(7), d.Histogram.Bucket[0].GetCumulativeCount())
}

func TestMetricFromDTO_Summary(t *testing.T) {
	sampleCount := uint64(20)
	sampleSum := 100.0
	quantile := 0.99
	quantileValue := 5.5

	mf := &dto.MetricFamily{
		Name: proto.String("test_summary"),
		Help: proto.String("A test summary"),
		Type: dto.MetricType_SUMMARY.Enum(),
		Metric: []*dto.Metric{
			{
				Summary: &dto.Summary{
					SampleCount: &sampleCount,
					SampleSum:   &sampleSum,
					Quantile: []*dto.Quantile{
						{
							Quantile: &quantile,
							Value:    &quantileValue,
						},
					},
				},
			},
		},
	}

	pm, err := metricFromDTO(mf, mf.Metric[0])
	require.NoError(t, err)
	require.NotNil(t, pm)

	d := &dto.Metric{}
	require.NoError(t, pm.Write(d))
	assert.Equal(t, uint64(20), d.Summary.GetSampleCount())
	assert.Equal(t, 100.0, d.Summary.GetSampleSum())
	require.Len(t, d.Summary.Quantile, 1)
	assert.Equal(t, 0.99, d.Summary.Quantile[0].GetQuantile())
	assert.Equal(t, 5.5, d.Summary.Quantile[0].GetValue())
}

func TestMetricFromDTO_NilMetricType(t *testing.T) {
	// A COUNTER family where the metric has nil Counter field:
	// metricFromDTO should fall through to the untyped fallback with value 0.
	mf := &dto.MetricFamily{
		Name: proto.String("test_nil_counter"),
		Help: proto.String("A counter with nil value"),
		Type: dto.MetricType_COUNTER.Enum(),
		Metric: []*dto.Metric{
			{
				Counter: nil,
				Label: []*dto.LabelPair{
					{Name: proto.String("k"), Value: proto.String("v")},
				},
			},
		},
	}

	pm, err := metricFromDTO(mf, mf.Metric[0])
	require.NoError(t, err)
	require.NotNil(t, pm)

	d := &dto.Metric{}
	require.NoError(t, pm.Write(d))
	// Falls through to untyped with value 0
	assert.Equal(t, 0.0, d.Untyped.GetValue())

	// Also test a GAUGE family with nil Gauge field
	mfGauge := &dto.MetricFamily{
		Name: proto.String("test_nil_gauge"),
		Help: proto.String("A gauge with nil value"),
		Type: dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{
			{
				Gauge: nil,
			},
		},
	}

	pm2, err := metricFromDTO(mfGauge, mfGauge.Metric[0])
	require.NoError(t, err)
	require.NotNil(t, pm2)

	d2 := &dto.Metric{}
	require.NoError(t, pm2.Write(d2))
	assert.Equal(t, 0.0, d2.Untyped.GetValue())
}
