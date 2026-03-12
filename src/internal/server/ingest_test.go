package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetIngestStats_NoURLConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	viper.Set("ingest.url", "")
	defer viper.Set("ingest.url", "")

	router := gin.New()
	router.GET("/ingest/stats", getIngestStats)

	w := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/ingest/stats", nil)
	require.NoError(t, err)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "ingest.url not configured")
}

func TestGetIngestStats_UpstreamReturnsStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	expectedStats := SystemStats{
		CPU: CPUStats{
			NumCPU:       8,
			NumGoroutine: 42,
		},
		Memory: MemoryStats{
			Alloc:      1024000,
			TotalAlloc: 2048000,
			Sys:        4096000,
			NumGC:      10,
		},
		GPU: []GPUStats{
			{
				Index:       0,
				Name:        "NVIDIA A100",
				Temperature: 65,
				MemoryUsage: 8192000000,
			},
		},
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/status", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(expectedStats)
	}))
	defer upstream.Close()

	viper.Set("ingest.url", upstream.URL)
	defer viper.Set("ingest.url", "")

	router := gin.New()
	router.GET("/ingest/stats", getIngestStats)

	w := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/ingest/stats", nil)
	require.NoError(t, err)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result SystemStats
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, expectedStats.CPU.NumCPU, result.CPU.NumCPU)
	assert.Equal(t, expectedStats.CPU.NumGoroutine, result.CPU.NumGoroutine)
	assert.Equal(t, expectedStats.Memory.Alloc, result.Memory.Alloc)
	assert.Equal(t, expectedStats.Memory.NumGC, result.Memory.NumGC)
	assert.Len(t, result.GPU, 1)
	assert.Equal(t, "NVIDIA A100", result.GPU[0].Name)
	assert.Equal(t, 65, result.GPU[0].Temperature)
}

func TestGetIngestStats_UpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	viper.Set("ingest.url", upstream.URL)
	defer viper.Set("ingest.url", "")

	router := gin.New()
	router.GET("/ingest/stats", getIngestStats)

	w := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/ingest/stats", nil)
	require.NoError(t, err)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "ingest service returned non-200 status")
}

func TestGetIngestStats_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("this is not valid json{{{"))
	}))
	defer upstream.Close()

	viper.Set("ingest.url", upstream.URL)
	defer viper.Set("ingest.url", "")

	router := gin.New()
	router.GET("/ingest/stats", getIngestStats)

	w := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/ingest/stats", nil)
	require.NoError(t, err)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to decode ingest response")
}
