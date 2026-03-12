package metrics

import (
	"testing"

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
