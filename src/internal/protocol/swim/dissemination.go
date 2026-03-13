package swim

import (
	"math"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
)

type disseminationEntry struct {
	event     MemberEvent
	remaining int // retransmits remaining
}

// Disseminator manages the piggyback buffer for SWIM event dissemination.
type Disseminator struct {
	mu      sync.Mutex
	entries map[peer.ID]*disseminationEntry
	lambda  int
	n       int // current cluster size estimate
}

func NewDisseminator(lambda, n int) *Disseminator {
	if n < 2 {
		n = 2
	}
	return &Disseminator{
		entries: make(map[peer.ID]*disseminationEntry),
		lambda:  lambda,
		n:       n,
	}
}

// UpdateN updates the cluster size estimate for retransmit calculation.
func (d *Disseminator) UpdateN(n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if n < 2 {
		n = 2
	}
	d.n = n
}

func (d *Disseminator) retransmitLimit() int {
	return int(float64(d.lambda) * math.Log2(float64(d.n)))
}

// Enqueue adds a new event. If an event for the same peer already exists,
// it is superseded if the new event has higher incarnation or higher-priority status.
func (d *Disseminator) Enqueue(e MemberEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	existing, ok := d.entries[e.Peer]
	if ok {
		// Supersede: higher incarnation wins, or same incarnation with higher-priority status
		if e.Incarnation < existing.event.Incarnation {
			return
		}
		if e.Incarnation == existing.event.Incarnation && statusPriority(e.Status) <= statusPriority(existing.event.Status) {
			return
		}
	}

	d.entries[e.Peer] = &disseminationEntry{
		event:     e,
		remaining: d.retransmitLimit(),
	}
}

// GetPiggyback returns up to maxEvents events to piggyback on a probe message.
// Each call decrements the remaining retransmit counter.
func (d *Disseminator) GetPiggyback(maxEvents int) []MemberEvent {
	d.mu.Lock()
	defer d.mu.Unlock()

	var events []MemberEvent
	var expired []peer.ID

	for pid, entry := range d.entries {
		if len(events) >= maxEvents {
			break
		}
		if entry.remaining <= 0 {
			expired = append(expired, pid)
			continue
		}
		events = append(events, entry.event)
		entry.remaining--
	}

	for _, pid := range expired {
		delete(d.entries, pid)
	}

	return events
}

// statusPriority returns a priority for supersession: Dead > Suspect > Alive > Join.
func statusPriority(s MemberStatus) int {
	switch s {
	case StatusDead:
		return 4
	case StatusSuspect:
		return 3
	case StatusAlive:
		return 2
	case StatusJoin:
		return 1
	default:
		return 0
	}
}
