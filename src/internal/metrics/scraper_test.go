package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPeerProvider struct {
	peers []PeerInfo
}

func (m *mockPeerProvider) GetScrapablePeers() []PeerInfo {
	return m.peers
}

func TestScrapeTarget_ParsesPrometheusFormat(t *testing.T) {
	metricsBody := `# HELP http_requests_total Total requests
# TYPE http_requests_total counter
http_requests_total{method="GET"} 100
http_requests_total{method="POST"} 50
# HELP go_goroutines Number of goroutines
# TYPE go_goroutines gauge
go_goroutines 42
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, metricsBody)
	}))
	defer srv.Close()

	s := &MetricsScraper{
		metricsPath: "/metrics",
		timeout:     5 * time.Second,
		httpClient:  srv.Client(),
		cache:       &sync.Map{},
	}

	families, err := s.scrapeTarget(srv.URL)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(families), 2)
}

func TestScrapeTarget_ReturnsErrorOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // exits when client cancels
	}))
	defer srv.Close()

	s := &MetricsScraper{
		metricsPath: "/metrics",
		timeout:     100 * time.Millisecond,
		httpClient:  srv.Client(),
		cache:       &sync.Map{},
	}

	_, err := s.scrapeTarget(srv.URL)
	assert.Error(t, err)
}

func TestCacheEviction_RemovesDisconnectedPeers(t *testing.T) {
	cache := &sync.Map{}
	cache.Store("peer-1", []*cachedMetric{})
	cache.Store("peer-2", []*cachedMetric{})

	activePeers := map[string]bool{"peer-1": true}
	evictStale(cache, activePeers)

	_, ok1 := cache.Load("peer-1")
	_, ok2 := cache.Load("peer-2")
	assert.True(t, ok1, "active peer should remain")
	assert.False(t, ok2, "disconnected peer should be evicted")
}
