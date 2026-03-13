package swim

import (
	"math"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestDisseminationEnqueueAndGet(t *testing.T) {
	d := NewDisseminator(3, 10) // lambda=3, N=10
	limit := int(3 * math.Log2(10)) // ~10

	d.Enqueue(MemberEvent{
		Peer:   peer.ID("p1"),
		Status: StatusAlive,
	})

	events := d.GetPiggyback(5)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Peer != peer.ID("p1") {
		t.Fatal("wrong peer")
	}

	// Get enough times to exhaust retransmits
	for i := 0; i < limit+5; i++ {
		d.GetPiggyback(5)
	}

	events = d.GetPiggyback(5)
	if len(events) != 0 {
		t.Fatalf("expected 0 events after exhausting retransmits, got %d", len(events))
	}
}

func TestDisseminationSupersedes(t *testing.T) {
	d := NewDisseminator(3, 10)

	// Enqueue Alive for p1
	d.Enqueue(MemberEvent{
		Peer:        peer.ID("p1"),
		Status:      StatusAlive,
		Incarnation: 1,
	})

	// Enqueue Dead for p1 — should supersede Alive
	d.Enqueue(MemberEvent{
		Peer:        peer.ID("p1"),
		Status:      StatusDead,
		Incarnation: 2,
	})

	events := d.GetPiggyback(5)
	if len(events) != 1 {
		t.Fatalf("expected 1 event (superseded), got %d", len(events))
	}
	if events[0].Status != StatusDead {
		t.Fatal("Dead should supersede Alive")
	}
}
