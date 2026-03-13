package nodetable

import (
	"sync"
	"time"
)

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
	current := w.nt.Snapshot()
	next := current.Clone()
	for _, e := range batch {
		next.ApplyEvent(e)
	}
	next.RebuildIndexes()
	next.Generation++
	w.nt.Store(next)
}
