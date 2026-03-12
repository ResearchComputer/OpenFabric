package metrics

import (
	"strings"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

const metricPrefix = "otela_node_"

func Relabel(families []*dto.MetricFamily, peerLabels map[string]string) []*dto.MetricFamily {
	if len(families) == 0 {
		return nil
	}

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
				Label:       newLabels,
				Gauge:       m.Gauge,
				Counter:     m.Counter,
				Summary:     m.Summary,
				Untyped:     m.Untyped,
				Histogram:   m.Histogram,
				TimestampMs: m.TimestampMs,
			}
		}
		result = append(result, newMF)
	}
	return result
}

func sanitizeMetricName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ':' {
			return r
		}
		return '_'
	}, name)
}
