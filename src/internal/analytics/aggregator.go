package analytics

import "math/rand"

// Sample is one observed request handed to the recorder by the proxy.
type Sample struct {
	Service      string
	Model        string
	WorkerPeerID string
	ProviderID   string
	GPUModel     string
	GPUCount     int
	Status       int
	LatencyMs    float64
	TTFTMs       float64
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
	TotalTokens  int64
	GPUMs        int64
	Concurrency  int
}

// Rollup is the aggregated result for one (service, model, worker) over a window.
type Rollup struct {
	Service       string
	Model         string
	WorkerPeerID  string
	ProviderID    string
	GPUModel      string
	GPUCount      int
	WindowSeconds int

	RequestCount    int64
	ErrorCount      int64
	InputTokensSum  int64
	OutputTokensSum int64
	CachedTokensSum int64
	TotalTokensSum  int64
	GPUMsSum        int64

	ConcurrencyMax int
	ConcurrencyAvg float64

	LatencyP50 float64
	LatencyP95 float64
	LatencyAvg float64
	TTFTP50    float64
	TTFTP95    float64
}

type bucketKey struct {
	service string
	model   string
	worker  string
}

type bucket struct {
	providerID string
	gpuModel   string
	gpuCount   int

	requestCount    int64
	errorCount      int64
	inputTokensSum  int64
	outputTokensSum int64
	cachedTokensSum int64
	totalTokensSum  int64
	gpuMsSum        int64

	concurrencyMax int
	concurrencySum int64

	latency *reservoir
	ttft    *reservoir
}

// aggregator accumulates per-(service,model,worker) request stats into buckets
// and snapshots them on flush. It is NOT safe for concurrent use: add and flush
// (and the shared *rand.Rand they use) must be serialized by the caller — the
// Recorder holds a mutex around every add/flush call.
type aggregator struct {
	maxSamples    int
	windowSeconds int
	rng           *rand.Rand
	buckets       map[bucketKey]*bucket
}

func newAggregator(maxSamples, windowSeconds int, rng *rand.Rand) *aggregator {
	return &aggregator{
		maxSamples:    maxSamples,
		windowSeconds: windowSeconds,
		rng:           rng,
		buckets:       make(map[bucketKey]*bucket),
	}
}

func (a *aggregator) add(s Sample) {
	k := bucketKey{service: s.Service, model: s.Model, worker: s.WorkerPeerID}
	b := a.buckets[k]
	if b == nil {
		// Dimension metadata (provider, GPU) is captured from the first sample
		// for this key and not updated thereafter; a worker (the bucket key) is
		// expected to have stable provider/GPU config for the window.
		b = &bucket{
			providerID: s.ProviderID,
			gpuModel:   s.GPUModel,
			gpuCount:   s.GPUCount,
			latency:    newReservoir(a.maxSamples, a.rng),
			ttft:       newReservoir(a.maxSamples, a.rng),
		}
		a.buckets[k] = b
	}
	b.requestCount++
	if s.Status >= 400 {
		b.errorCount++
	}
	b.inputTokensSum += s.InputTokens
	b.outputTokensSum += s.OutputTokens
	b.cachedTokensSum += s.CachedTokens
	b.totalTokensSum += s.TotalTokens
	b.gpuMsSum += s.GPUMs
	b.concurrencySum += int64(s.Concurrency)
	if s.Concurrency > b.concurrencyMax {
		b.concurrencyMax = s.Concurrency
	}
	b.latency.add(s.LatencyMs)
	if s.TTFTMs > 0 {
		b.ttft.add(s.TTFTMs)
	}
}

// flush snapshots every bucket into a Rollup and clears the buckets.
func (a *aggregator) flush() []Rollup {
	rollups := make([]Rollup, 0, len(a.buckets))
	for k, b := range a.buckets {
		var concAvg float64
		if b.requestCount > 0 {
			concAvg = float64(b.concurrencySum) / float64(b.requestCount)
		}
		rollups = append(rollups, Rollup{
			Service:         k.service,
			Model:           k.model,
			WorkerPeerID:    k.worker,
			ProviderID:      b.providerID,
			GPUModel:        b.gpuModel,
			GPUCount:        b.gpuCount,
			WindowSeconds:   a.windowSeconds,
			RequestCount:    b.requestCount,
			ErrorCount:      b.errorCount,
			InputTokensSum:  b.inputTokensSum,
			OutputTokensSum: b.outputTokensSum,
			CachedTokensSum: b.cachedTokensSum,
			TotalTokensSum:  b.totalTokensSum,
			GPUMsSum:        b.gpuMsSum,
			ConcurrencyMax:  b.concurrencyMax,
			ConcurrencyAvg:  concAvg,
			LatencyP50:      b.latency.percentile(50),
			LatencyP95:      b.latency.percentile(95),
			LatencyAvg:      b.latency.mean(),
			TTFTP50:         b.ttft.percentile(50),
			TTFTP95:         b.ttft.percentile(95),
		})
	}
	a.buckets = make(map[bucketKey]*bucket)
	return rollups
}
