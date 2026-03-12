package metrics

import (
	"opentela/internal/protocol"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestExtractServices_WithModel(t *testing.T) {
	services := []protocol.Service{
		{
			Name:          "llm",
			IdentityGroup: []string{"model=Qwen/Qwen3-8B"},
		},
	}
	infos := extractServices(services)
	require.Len(t, infos, 1)
	assert.Equal(t, "llm", infos[0].Name)
	assert.Equal(t, "Qwen/Qwen3-8B", infos[0].Model)
}

func TestExtractServices_WithoutModel(t *testing.T) {
	services := []protocol.Service{
		{
			Name:          "compute",
			IdentityGroup: []string{"role=worker"},
		},
	}
	infos := extractServices(services)
	require.Len(t, infos, 1)
	assert.Equal(t, "compute", infos[0].Name)
	assert.Equal(t, "", infos[0].Model)
}

func TestExtractServices_MultipleServices(t *testing.T) {
	services := []protocol.Service{
		{
			Name:          "llm",
			IdentityGroup: []string{"model=Qwen/Qwen3-8B"},
		},
		{
			Name:          "embedding",
			IdentityGroup: []string{"model=bge-small"},
		},
	}
	infos := extractServices(services)
	require.Len(t, infos, 2)
	assert.Equal(t, "llm", infos[0].Name)
	assert.Equal(t, "Qwen/Qwen3-8B", infos[0].Model)
	assert.Equal(t, "embedding", infos[1].Name)
	assert.Equal(t, "bge-small", infos[1].Model)
}

func TestExtractServices_EmptyServices(t *testing.T) {
	infos := extractServices([]protocol.Service{})
	assert.Nil(t, infos)
}

func TestExtractServices_MultipleIdentityGroups(t *testing.T) {
	services := []protocol.Service{
		{
			Name:          "llm",
			IdentityGroup: []string{"role=inference", "model=Llama-3-70B", "region=eu"},
		},
	}
	infos := extractServices(services)
	require.Len(t, infos, 1)
	assert.Equal(t, "llm", infos[0].Name)
	assert.Equal(t, "Llama-3-70B", infos[0].Model, "should extract only the model= identity group")
}
