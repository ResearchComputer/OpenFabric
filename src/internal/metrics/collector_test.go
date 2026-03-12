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
