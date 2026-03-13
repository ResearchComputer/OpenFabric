package nodetable

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestScalableNodeTableConcurrentReadWrite(t *testing.T) {
	nt := NewNodeTable()
	w := NewWriter(nt)
	w.Start()
	defer w.Stop()

	// Concurrent writers
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pid := peer.ID(fmt.Sprintf("peer-%d", i))
			w.Send(NodeEvent{
				Type:   EventSWIMJoin,
				PeerID: pid,
				PeerData: &PeerData{
					IdentityGroups: []string{"model=test"},
				},
			})
		}(i)
	}

	// Concurrent readers (simulating routing)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				snap := nt.Snapshot()
				_ = snap.ByIdentity["model=test"]
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	snap := nt.Snapshot()
	if len(snap.Peers) != 100 {
		t.Fatalf("expected 100 peers, got %d", len(snap.Peers))
	}
}
