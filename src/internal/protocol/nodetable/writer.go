package nodetable

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	snapshotCloneDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "otela_nodetable_snapshot_clone_duration_seconds",
			Help:    "Time to clone and rebuild node table snapshot",
			Buckets: prometheus.DefBuckets,
		},
	)
	snapshotGeneration = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "otela_nodetable_snapshot_generation",
			Help: "Current snapshot generation number",
		},
	)
	eventsBatched = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "otela_nodetable_events_batched",
			Help:    "Number of events per batch",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500},
		},
	)
)

func init() {
	prometheus.MustRegister(snapshotCloneDuration, snapshotGeneration, eventsBatched)
}

const (
	batchInterval = 100 * time.Millisecond
	batchMaxSize  = 50
	eventChanSize = 1024
)

// Writer receives NodeEvents and applies them to the NodeTable
// in batches, producing new snapshots atomically.
type Writer struct {
	nt     *NodeTable
	events chan NodeEvent
	stop   chan struct{}
	wg     sync.WaitGroup
}

func NewWriter(nt *NodeTable) *Writer {
	return &Writer{
		nt:     nt,
		events: make(chan NodeEvent, eventChanSize),
		stop:   make(chan struct{}),
	}
}

func (w *Writer) Start() {
	w.wg.Add(1)
	go w.run()
}

func (w *Writer) Stop() {
	close(w.stop)
	w.wg.Wait()
}

// Send enqueues an event for processing. Non-blocking if channel isn't full.
func (w *Writer) Send(e NodeEvent) {
	select {
	case w.events <- e:
	default:
		// Channel full — drop event (log in production)
	}
}

func (w *Writer) run() {
	defer w.wg.Done()
	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()

	var batch []NodeEvent

	for {
		select {
		case <-w.stop:
			// Drain remaining events
			w.drainAndApply(batch)
			return

		case e := <-w.events:
			batch = append(batch, e)
			if len(batch) >= batchMaxSize {
				w.applyBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				w.applyBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *Writer) drainAndApply(batch []NodeEvent) {
	for {
		select {
		case e := <-w.events:
			batch = append(batch, e)
		default:
			if len(batch) > 0 {
				w.applyBatch(batch)
			}
			return
		}
	}
}

func (w *Writer) applyBatch(batch []NodeEvent) {
	start := time.Now()
	current := w.nt.Snapshot()
	next := current.Clone()
	for _, e := range batch {
		next.ApplyEvent(e)
	}
	next.RebuildIndexes()
	next.Generation++
	w.nt.Store(next)

	snapshotCloneDuration.Observe(time.Since(start).Seconds())
	snapshotGeneration.Set(float64(next.Generation))
	eventsBatched.Observe(float64(len(batch)))
}
