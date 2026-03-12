package metrics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"opentela/internal/common"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

type PeerInfo struct {
	ID      string
	Address string
	Labels  map[string]string
}

type PeerProvider interface {
	GetScrapablePeers() []PeerInfo
}

type cachedMetric struct {
	families []*dto.MetricFamily
	updated  time.Time
}

type ScraperConfig struct {
	ScrapeInterval time.Duration
	ScrapeTimeout  time.Duration
	MetricsPath    string
	MaxConcurrent  int
}

type MetricsScraper struct {
	provider    PeerProvider
	metricsPath string
	timeout     time.Duration
	maxConc     int
	httpClient  *http.Client
	cache       *sync.Map
	stopCh      chan struct{}

	scrapeErrors        *prometheus.CounterVec
	scrapeDuration      *prometheus.HistogramVec
	scrapeCycleDuration prometheus.Histogram
}

func NewMetricsScraper(cfg ScraperConfig, provider PeerProvider, transport http.RoundTripper) *MetricsScraper {
	client := &http.Client{
		Transport: transport,
	}
	return &MetricsScraper{
		provider:    provider,
		metricsPath: cfg.MetricsPath,
		timeout:     cfg.ScrapeTimeout,
		maxConc:     cfg.MaxConcurrent,
		httpClient:  client,
		cache:       &sync.Map{},
		stopCh:      make(chan struct{}),
		scrapeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "otela_scraper_errors_total",
			Help: "Total scrape failures per peer",
		}, []string{"peer_id"}),
		scrapeDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "otela_scraper_duration_seconds",
			Help:    "Per-peer scrape duration",
			Buckets: prometheus.DefBuckets,
		}, []string{"peer_id"}),
		scrapeCycleDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "otela_scraper_cycle_duration_seconds",
			Help:    "Total wall time for one full scrape cycle",
			Buckets: prometheus.DefBuckets,
		}),
	}
}

func (s *MetricsScraper) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		s.scrapeAll()
		for {
			select {
			case <-ticker.C:
				s.scrapeAll()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *MetricsScraper) Stop() {
	close(s.stopCh)
}

func (s *MetricsScraper) GetCachedMetrics() []*dto.MetricFamily {
	var all []*dto.MetricFamily
	s.cache.Range(func(key, value any) bool {
		if cm, ok := value.(*cachedMetric); ok {
			all = append(all, cm.families...)
		}
		return true
	})
	return all
}

func (s *MetricsScraper) GetSelfMetrics() []prometheus.Collector {
	return []prometheus.Collector{s.scrapeErrors, s.scrapeDuration, s.scrapeCycleDuration}
}

func (s *MetricsScraper) scrapeAll() {
	cycleStart := time.Now()
	defer func() { s.scrapeCycleDuration.Observe(time.Since(cycleStart).Seconds()) }()

	peers := s.provider.GetScrapablePeers()

	active := make(map[string]bool, len(peers))
	for _, p := range peers {
		active[p.ID] = true
	}
	evictStale(s.cache, active)

	sem := make(chan struct{}, s.maxConc)
	var wg sync.WaitGroup

	for _, peer := range peers {
		wg.Add(1)
		sem <- struct{}{}
		go func(p PeerInfo) {
			defer wg.Done()
			defer func() { <-sem }()
			s.scrapePeer(p)
		}(peer)
	}
	wg.Wait()
}

func (s *MetricsScraper) scrapePeer(p PeerInfo) {
	start := time.Now()
	url := p.Address + s.metricsPath
	families, err := s.scrapeTarget(url)
	elapsed := time.Since(start)

	s.scrapeDuration.WithLabelValues(p.ID).Observe(elapsed.Seconds())

	if err != nil {
		common.Logger.Warnf("Scrape failed for peer %s: %v", p.ID, err)
		s.scrapeErrors.WithLabelValues(p.ID).Inc()
		return
	}

	relabeled := Relabel(families, p.Labels)
	s.cache.Store(p.ID, &cachedMetric{
		families: relabeled,
		updated:  time.Now(),
	})
}

func (s *MetricsScraper) scrapeTarget(baseURL string) ([]*dto.MetricFamily, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", string(expfmt.NewFormat(expfmt.TypeTextPlain)))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scraping %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scraping %s: status %d", baseURL, resp.StatusCode)
	}

	return parseMetrics(resp.Body, resp.Header)
}

func parseMetrics(r io.Reader, header http.Header) ([]*dto.MetricFamily, error) {
	mediaType := expfmt.ResponseFormat(header)
	if mediaType == expfmt.NewFormat(expfmt.TypeUnknown) {
		mediaType = expfmt.NewFormat(expfmt.TypeTextPlain)
	}

	decoder := expfmt.NewDecoder(r, mediaType)
	var families []*dto.MetricFamily
	for {
		var mf dto.MetricFamily
		if err := decoder.Decode(&mf); err != nil {
			if err == io.EOF {
				break
			}
			return families, fmt.Errorf("decoding metrics: %w", err)
		}
		families = append(families, &mf)
	}
	return families, nil
}

func evictStale(cache *sync.Map, activePeers map[string]bool) {
	cache.Range(func(key, _ any) bool {
		peerID, ok := key.(string)
		if !ok {
			return true
		}
		if !activePeers[peerID] {
			cache.Delete(key)
		}
		return true
	})
}
