package ingest

import (
	"net/http"
	"opentela/internal/common"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// SystemStats represents the current state of the machine
type SystemStats struct {
	CPU    CPUStats    `json:"cpu"`
	Memory MemoryStats `json:"memory"`
	GPU    []GPUStats  `json:"gpu"` // Placeholder for future GPU implementation
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

// Run launches the ingestion server on the configured port
func Run() {
	port := viper.GetString("ingest.port")
	if port == "" {
		port = "8081" // Default to 8081 to avoid conflict with main server (usually 8080)
	}

	// Use release mode if not debugging to reduce log noise
	if !viper.GetBool("debug") {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/status", func(c *gin.Context) {
		stats := collectStats()
		c.JSON(http.StatusOK, stats)
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "timestamp": time.Now()})
	})

	common.Logger.Infof("Starting Ingest Server component on port %s...", port)

	if err := r.Run(":" + port); err != nil {
		common.Logger.Errorf("Failed to start ingest server: %v", err)
	}
}

func collectStats() SystemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return SystemStats{
		CPU: CPUStats{
			NumCPU:       runtime.NumCPU(),
			NumGoroutine: runtime.NumGoroutine(),
		},
		Memory: MemoryStats{
			Alloc:      m.Alloc,
			TotalAlloc: m.TotalAlloc,
			Sys:        m.Sys,
			NumGC:      m.NumGC,
		},
		GPU: []GPUStats{}, // TODO: Implement NVIDIA/AMD GPU collection logic
	}
}
