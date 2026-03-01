package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// SystemStats represents the current state of the machine as returned by the ingest service
type SystemStats struct {
	CPU    CPUStats    `json:"cpu"`
	Memory MemoryStats `json:"memory"`
	GPU    []GPUStats  `json:"gpu"`
}

type CPUStats struct {
	NumCPU       int `json:"num_cpu"`
	NumGoroutine int `json:"num_goroutine"`
}

type MemoryStats struct {
	Alloc      uint64 `json:"alloc_bytes"`
	TotalAlloc uint64 `json:"total_alloc_bytes"`
	Sys        uint64 `json:"sys_bytes"`
	NumGC      uint32 `json:"num_gc"`
}

type GPUStats struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Temperature int    `json:"temperature"`
	MemoryUsage uint64 `json:"memory_usage"`
}

func getIngestStats(c *gin.Context) {
	ingestURL := viper.GetString("ingest.url")
	if ingestURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ingest.url not configured"})
		return
	}

	client := http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get(ingestURL + "/status")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to contact ingest service", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ingest service returned non-200 status", "status": resp.StatusCode})
		return
	}

	var stats SystemStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decode ingest response", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}
