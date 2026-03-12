package metrics

import (
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
)

type MetricsSource interface {
	GetCachedMetrics() []*dto.MetricFamily
}

type AggregatedCollector struct {
	source MetricsSource

	peersConnected prometheus.Gauge
	peersTotal     prometheus.Gauge
	scraperTargets prometheus.Gauge
}

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

func (c *AggregatedCollector) Describe(ch chan<- *prometheus.Desc) {
	// Intentionally empty: unchecked collector pattern.
}

func (c *AggregatedCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- c.peersConnected
	ch <- c.peersTotal
	ch <- c.scraperTargets

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

func (c *AggregatedCollector) SetNetworkStats(connected, total int) {
	c.peersConnected.Set(float64(connected))
	c.peersTotal.Set(float64(total))
}

func (c *AggregatedCollector) SetScraperTargets(n int) {
	c.scraperTargets.Set(float64(n))
}

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
			return prometheus.MustNewConstHistogram(desc, m.Histogram.GetSampleCount(), m.Histogram.GetSampleSum(), buckets, labelValues...), nil
		}
	case dto.MetricType_SUMMARY:
		if m.Summary != nil {
			quantiles := make(map[float64]float64, len(m.Summary.Quantile))
			for _, q := range m.Summary.Quantile {
				quantiles[q.GetQuantile()] = q.GetValue()
			}
			return prometheus.MustNewConstSummary(desc, m.Summary.GetSampleCount(), m.Summary.GetSampleSum(), quantiles, labelValues...), nil
		}
	}

	return prometheus.MustNewConstMetric(desc, prometheus.UntypedValue, 0, labelValues...), nil
}
