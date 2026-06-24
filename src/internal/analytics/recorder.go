package analytics

import (
	"math/rand"
	"sync"
	"time"
)

// Recorder is the proxy-facing seam. All methods are safe to call on a nil
// *Recorder (the disabled path) so callers need no enabled-checks.
type Recorder struct {
	cfg    Config
	headID string
	sink   Sink

	mu       sync.Mutex
	agg      *aggregator
	inflight map[bucketKey]int

	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
}

// New constructs a running recorder and starts its flush goroutine.
func New(cfg Config, headID string, sink Sink) *Recorder {
	cfg = cfg.Normalize()
	r := &Recorder{
		cfg:      cfg,
		headID:   headID,
		sink:     sink,
		agg:      newAggregator(cfg.MaxLatencySamples, cfg.FlushIntervalSeconds, rand.New(rand.NewSource(time.Now().UnixNano()))),
		inflight: make(map[bucketKey]int),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go r.run()
	return r
}

func (r *Recorder) run() {
	defer close(r.doneCh)
	ticker := time.NewTicker(time.Duration(r.cfg.FlushIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.flushNow()
		case <-r.stopCh:
			r.flushNow()
			return
		}
	}
}

// Begin records a dispatched request and returns the in-flight count (including
// this one) for the (service, model, worker) key.
func (r *Recorder) Begin(service, model, worker string) int {
	if r == nil {
		return 0
	}
	k := bucketKey{service: service, model: model, worker: worker}
	r.mu.Lock()
	r.inflight[k]++
	n := r.inflight[k]
	r.mu.Unlock()
	return n
}

// End records that a dispatched request finished.
func (r *Recorder) End(service, model, worker string) {
	if r == nil {
		return
	}
	k := bucketKey{service: service, model: model, worker: worker}
	r.mu.Lock()
	if r.inflight[k] > 0 {
		r.inflight[k]--
	}
	r.mu.Unlock()
}

// Observe folds a completed request's sample into the current window.
func (r *Recorder) Observe(s Sample) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.agg.add(s)
	r.mu.Unlock()
}

// FlushNow synchronously flushes the current window, enqueuing one event per
// (model, worker) bucket. It is exported for use in integration tests; normal
// production code relies on the ticker-driven flush in run().
func (r *Recorder) FlushNow() {
	if r == nil {
		return
	}
	r.flushNow()
}

// flushNow snapshots the window and enqueues one event per (model, worker).
func (r *Recorder) flushNow() {
	if r == nil {
		return
	}
	r.mu.Lock()
	rollups := r.agg.flush()
	r.mu.Unlock()
	for _, ro := range rollups {
		r.sink.Enqueue(buildEvent(r.headID, r.cfg.GroupType, ro))
	}
}

// Stop flushes the final window, stops the ticker, and closes the sink.
// Safe to call multiple times and on a nil *Recorder.
func (r *Recorder) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		close(r.stopCh)
		<-r.doneCh
		r.sink.Close()
	})
}
