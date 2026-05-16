package crdt_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	crdt "opentela/internal/protocol/go-ds-crdt"

	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/boxo/exchange/offline"
	"github.com/ipfs/boxo/ipld/merkledag"
	cid "github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	dssync "github.com/ipfs/go-datastore/sync"
	ipld "github.com/ipfs/go-ipld-format"
	logging "github.com/ipfs/go-log/v2"
)

// pipeBroadcaster connects two crdt.Datastores by a pair of in-memory channels.
type pipeBroadcaster struct {
	in  <-chan []byte
	out chan<- []byte
}

func newPipeBroadcasters() (*pipeBroadcaster, *pipeBroadcaster) {
	forward := make(chan []byte, 64)
	reverse := make(chan []byte, 64)
	return &pipeBroadcaster{in: reverse, out: forward},
		&pipeBroadcaster{in: forward, out: reverse}
}

func (p *pipeBroadcaster) Broadcast(ctx context.Context, data []byte) error {
	cp := append([]byte(nil), data...)
	select {
	case p.out <- cp:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *pipeBroadcaster) Next(ctx context.Context) ([]byte, error) {
	select {
	case b, ok := <-p.in:
		if !ok {
			return nil, crdt.ErrNoMoreBroadcast
		}
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// firstBlockerDAG wraps an ipld.DAGService. While armed, the first CID it
// observes via Get/GetMany is latched and all subsequent fetches for that CID
// block on a gate channel. Other CIDs pass through. This simulates a bitswap
// fetch that hangs for a specific block (the original failure mode).
type firstBlockerDAG struct {
	ipld.DAGService
	gate     chan struct{}
	latched  chan struct{} // closed when the first armed CID is observed
	released sync.Once

	mu    sync.Mutex
	armed bool
	first cid.Cid
	seen  bool
}

func newFirstBlockerDAG(base ipld.DAGService) *firstBlockerDAG {
	return &firstBlockerDAG{
		DAGService: base,
		gate:       make(chan struct{}),
		latched:    make(chan struct{}),
	}
}

func (f *firstBlockerDAG) arm() {
	f.mu.Lock()
	f.armed = true
	f.mu.Unlock()
}

func (f *firstBlockerDAG) release() { f.released.Do(func() { close(f.gate) }) }

func (f *firstBlockerDAG) shouldBlock(c cid.Cid) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.armed {
		return false
	}
	if !f.seen {
		f.first = c
		f.seen = true
		close(f.latched)
		return true
	}
	return f.first.Equals(c)
}

func (f *firstBlockerDAG) wait(ctx context.Context, c cid.Cid) error {
	if !f.shouldBlock(c) {
		return nil
	}
	select {
	case <-f.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *firstBlockerDAG) Get(ctx context.Context, c cid.Cid) (ipld.Node, error) {
	if err := f.wait(ctx, c); err != nil {
		return nil, err
	}
	return f.DAGService.Get(ctx, c)
}

func (f *firstBlockerDAG) GetMany(ctx context.Context, cids []cid.Cid) <-chan *ipld.NodeOption {
	out := make(chan *ipld.NodeOption, len(cids))
	go func() {
		defer close(out)
		for _, c := range cids {
			if err := f.wait(ctx, c); err != nil {
				out <- &ipld.NodeOption{Err: err}
				continue
			}
			nd, err := f.DAGService.Get(ctx, c)
			out <- &ipld.NodeOption{Node: nd, Err: err}
		}
	}()
	return out
}

func newSharedDAG() ipld.DAGService {
	raw := dssync.MutexWrap(ds.NewMapDatastore())
	bs := blockstore.NewBlockstore(raw)
	return merkledag.NewDAGService(blockservice.New(bs, offline.Exchange(bs)))
}

func newTestPair(t *testing.T, multiHead bool) (*crdt.Datastore, *crdt.Datastore, *firstBlockerDAG, func()) {
	t.Helper()

	shared := newSharedDAG()
	readerDAG := newFirstBlockerDAG(shared)

	writerBC, readerBC := newPipeBroadcasters()
	writerStore := dssync.MutexWrap(ds.NewMapDatastore())
	readerStore := dssync.MutexWrap(ds.NewMapDatastore())

	mkOpts := func() *crdt.Options {
		o := crdt.DefaultOptions()
		o.Logger = logging.Logger("crdt-test")
		o.RebroadcastInterval = time.Second
		o.DAGSyncerTimeout = 30 * time.Second
		o.MultiHeadProcessing = multiHead
		return o
	}

	writer, err := crdt.New(writerStore, ds.NewKey("/test"), shared, writerBC, mkOpts())
	if err != nil {
		t.Fatalf("writer New: %v", err)
	}
	reader, err := crdt.New(readerStore, ds.NewKey("/test"), readerDAG, readerBC, mkOpts())
	if err != nil {
		_ = writer.Close()
		t.Fatalf("reader New: %v", err)
	}

	cleanup := func() {
		// Unblock any stuck fetches so Close() can complete its job drain.
		readerDAG.release()
		_ = writer.Close()
		_ = reader.Close()
	}
	return writer, reader, readerDAG, cleanup
}

func waitHas(ctx context.Context, store *crdt.Datastore, key ds.Key, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok, _ := store.Has(ctx, key); ok {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

// TestMultiHeadProcessing_AvoidsHeadOfLineBlocking is a regression test for the
// service-advertisement stall: when one DAG block fetch hangs (the bitswap
// failure mode), subsequent CRDT updates should still propagate. With
// MultiHeadProcessing=true the second value becomes visible while the first is
// stuck; with the option off, handleNext is serialized and the second value
// cannot reach the reader.
func TestMultiHeadProcessing_AvoidsHeadOfLineBlocking(t *testing.T) {
	cases := []struct {
		name              string
		multiHead         bool
		expectSecondValue bool
	}{
		{"WithMultiHead", true, true},
		{"WithoutMultiHead", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			writer, reader, readerDAG, cleanup := newTestPair(t, tc.multiHead)
			defer cleanup()

			// Bootstrap so the reader's heads set is non-empty; this avoids
			// the curHeadCount==0 path in handleNext, which calls GetPriority
			// inline and would block before we ever reach the processHead
			// spawn point.
			if err := writer.Put(ctx, ds.NewKey("bootstrap"), []byte("0")); err != nil {
				t.Fatalf("bootstrap put: %v", err)
			}
			if !waitHas(ctx, reader, ds.NewKey("bootstrap"), 3*time.Second) {
				t.Fatalf("reader never received bootstrap value")
			}

			// Arm the blocker so the NEXT new CID the reader fetches will
			// hang on the gate (simulates a stuck bitswap session).
			readerDAG.arm()

			if err := writer.Put(ctx, ds.NewKey("first"), []byte("v1")); err != nil {
				t.Fatalf("first put: %v", err)
			}
			// Wait deterministically for the reader to begin fetching the
			// first CID; otherwise on a slow runner the second put could
			// race ahead and become the latched/blocking CID instead.
			select {
			case <-readerDAG.latched:
			case <-time.After(5 * time.Second):
				t.Fatal("reader never began fetching the first CID")
			}

			if err := writer.Put(ctx, ds.NewKey("second"), []byte("v2")); err != nil {
				t.Fatalf("second put: %v", err)
			}

			got := waitHas(ctx, reader, ds.NewKey("second"), 3*time.Second)
			if got != tc.expectSecondValue {
				t.Fatalf("reader.Has(second) = %v, want %v (multiHead=%v) — "+
					"with multi-head processing the second value should be applied "+
					"while the first block fetch is still stuck on the gate.",
					got, tc.expectSecondValue, tc.multiHead)
			}

			// Release the gate and verify both values converge in both modes.
			readerDAG.release()
			if !waitHas(ctx, reader, ds.NewKey("first"), 3*time.Second) {
				t.Fatal("after release: reader never received 'first'")
			}
			if !waitHas(ctx, reader, ds.NewKey("second"), 3*time.Second) {
				t.Fatal("after release: reader never received 'second'")
			}
		})
	}
}

// alwaysFailDAG simulates a peer that holds no blocks and cannot fetch any.
// Used to drive the auto-ban code path: every fetch fails immediately, so
// failure counts accumulate quickly without waiting on bitswap timeouts.
type alwaysFailDAG struct{}

func (alwaysFailDAG) Get(ctx context.Context, c cid.Cid) (ipld.Node, error) {
	return nil, fmt.Errorf("simulated fetch failure for %s", c)
}

func (alwaysFailDAG) GetMany(ctx context.Context, cids []cid.Cid) <-chan *ipld.NodeOption {
	out := make(chan *ipld.NodeOption, len(cids))
	go func() {
		defer close(out)
		for _, c := range cids {
			out <- &ipld.NodeOption{Err: fmt.Errorf("simulated fetch failure for %s", c)}
		}
	}()
	return out
}

func (alwaysFailDAG) Add(ctx context.Context, _ ipld.Node) error          { return nil }
func (alwaysFailDAG) AddMany(ctx context.Context, _ []ipld.Node) error    { return nil }
func (alwaysFailDAG) Remove(ctx context.Context, _ cid.Cid) error         { return nil }
func (alwaysFailDAG) RemoveMany(ctx context.Context, _ []cid.Cid) error   { return nil }

// hasUnderPrefix reports whether the datastore contains at least one key with
// the given prefix.
func hasUnderPrefix(ctx context.Context, d ds.Datastore, prefix string) (bool, error) {
	res, err := d.Query(ctx, query.Query{Prefix: prefix, KeysOnly: true, Limit: 1})
	if err != nil {
		return false, err
	}
	defer res.Close()
	entry, ok := <-res.Next()
	if !ok {
		return false, nil
	}
	if entry.Error != nil {
		return false, entry.Error
	}
	return true, nil
}

// TestAutoBan_BansPersistentlyUnreachableCID drives a reader whose DAG service
// always errors. After the configured failure threshold and minimum age elapse,
// the reader should auto-ban the orphan head and persist that decision under
// the CRDT's /x/ namespace.
func TestAutoBan_BansPersistentlyUnreachableCID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	writerDAG := newSharedDAG()
	readerDAG := alwaysFailDAG{}

	writerBC, readerBC := newPipeBroadcasters()
	writerStore := dssync.MutexWrap(ds.NewMapDatastore())
	readerStore := dssync.MutexWrap(ds.NewMapDatastore())

	mkOpts := func() *crdt.Options {
		o := crdt.DefaultOptions()
		o.Logger = logging.Logger("crdt-test-autoban")
		o.RebroadcastInterval = 100 * time.Millisecond
		o.DAGSyncerTimeout = 50 * time.Millisecond
		o.MultiHeadProcessing = true
		// Aggressive thresholds so the ban fires within the test window.
		o.MaxFetchFailures = 3
		o.MinFetchFailureAge = 100 * time.Millisecond
		return o
	}

	writer, err := crdt.New(writerStore, ds.NewKey("/test"), writerDAG, writerBC, mkOpts())
	if err != nil {
		t.Fatalf("writer New: %v", err)
	}
	defer writer.Close()

	reader, err := crdt.New(readerStore, ds.NewKey("/test"), readerDAG, readerBC, mkOpts())
	if err != nil {
		t.Fatalf("reader New: %v", err)
	}
	defer reader.Close()

	if err := writer.Put(ctx, ds.NewKey("orphan"), []byte("v")); err != nil {
		t.Fatalf("writer.Put: %v", err)
	}

	// Wait for a persisted ban record to appear under /test/x.
	bannedPrefix := "/test/x"
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ok, err := hasUnderPrefix(ctx, readerStore, bannedPrefix)
		if err != nil {
			t.Fatalf("query reader store: %v", err)
		}
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected an entry under %s indicating auto-ban; none observed within 5s", bannedPrefix)
}
