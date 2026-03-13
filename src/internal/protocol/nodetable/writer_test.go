package nodetable

import (
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestWriterAppliesEvents(t *testing.T) {
	nt := NewNodeTable()
	w := NewWriter(nt)
	w.Start()
	defer w.Stop()

	pid := peer.ID("peer-1")
	w.Send(NodeEvent{
		Type:   EventSWIMJoin,
		PeerID: pid,
		PeerData: &PeerData{
			IdentityGroups: []string{"model=Qwen3-8B"},
		},
	})

	// Wait for batch to be applied
	time.Sleep(200 * time.Millisecond)

	snap := nt.Snapshot()
	if _, ok := snap.Peers[pid]; !ok {
		t.Fatal("peer should exist after writer processes join event")
	}
	if snap.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", snap.Generation)
	}
}

func TestWriterBatchesMultipleEvents(t *testing.T) {
	nt := NewNodeTable()
	w := NewWriter(nt)
	w.Start()
	defer w.Stop()

	// Send many events rapidly — they should be batched into one generation
	for i := 0; i < 100; i++ {
		pid := peer.ID(fmt.Sprintf("peer-%d", i))
		w.Send(NodeEvent{
			Type:   EventSWIMJoin,
			PeerID: pid,
			PeerData: &PeerData{},
		})
	}

	time.Sleep(300 * time.Millisecond)

	snap := nt.Snapshot()
	if len(snap.Peers) != 100 {
		t.Fatalf("expected 100 peers, got %d", len(snap.Peers))
	}
	// Generation should be small (events batched), not 100
	if snap.Generation > 10 {
		t.Fatalf("expected batched writes (generation <= 10), got %d", snap.Generation)
	}
}
