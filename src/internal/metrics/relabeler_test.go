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
	assert.Len(t, labels, 2)
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
