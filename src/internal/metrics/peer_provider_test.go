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
