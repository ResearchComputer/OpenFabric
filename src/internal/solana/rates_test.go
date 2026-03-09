package solana

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateManager_SetAndGetRate(t *testing.T) {
	rm := NewRateManager(100, 1)

	r := Rate{
		ProviderAddress: "Provider1",
		Service:         "llm",
		MetricName:      "tokens",
		PricePerUnit:    500,
	}
	rm.SetRate(r)

	got, err := rm.GetRate("Provider1", "llm", "tokens")
	require.NoError(t, err)
	assert.Equal(t, int64(500), got.PricePerUnit)
}

func TestRateManager_FallbackDefaults(t *testing.T) {
	rm := NewRateManager(100, 2)

	got, err := rm.GetRate("UnknownProvider", "llm", "tokens")
	require.NoError(t, err)
	assert.Equal(t, int64(100), got.PricePerUnit)

	got, err = rm.GetRate("UnknownProvider", "sandbox", "gpu_ms")
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.PricePerUnit)
}

func TestRateManager_UnknownMetric(t *testing.T) {
	rm := NewRateManager(100, 1)

	_, err := rm.GetRate("Provider1", "llm", "unknown_metric")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no rate found")
}

func TestRateManager_LoadFromConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "rates.yaml")

	yamlContent := `providers:
  - address: "ProvAddr1"
    services:
      - name: "llm"
        metrics:
          - name: "tokens"
            price_per_1000: 5000
          - name: "gpu_ms"
            price_per_1000: 2000
`
	require.NoError(t, os.WriteFile(configPath, []byte(yamlContent), 0o644))

	rm := NewRateManager(1, 1)
	require.NoError(t, rm.LoadFromConfig(configPath))

	got, err := rm.GetRate("ProvAddr1", "llm", "tokens")
	require.NoError(t, err)
	assert.Equal(t, int64(5), got.PricePerUnit) // 5000/1000

	got, err = rm.GetRate("ProvAddr1", "llm", "gpu_ms")
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.PricePerUnit) // 2000/1000
}

func TestRateManager_LoadFromConfig_MissingFile(t *testing.T) {
	rm := NewRateManager(1, 1)
	err := rm.LoadFromConfig("/nonexistent/rates.yaml")
	assert.Error(t, err)
}
