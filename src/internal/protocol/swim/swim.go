package swim

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// Config holds the tunable parameters for the SWIM failure detector.
type Config struct {
	ProbeInterval        time.Duration
	ProbeTimeout         time.Duration
	IndirectProbeTimeout time.Duration
	IndirectProbes       int
	SuspectTimeout       time.Duration
	RetransmitMult       int
}

// Transport is the interface for sending SWIM protocol messages over the network.
type Transport interface {
	SendPing(to peer.ID, seq uint64, events []MemberEvent) error
	SendAck(to peer.ID, seq uint64, events []MemberEvent) error
	SendPingReq(to peer.ID, target peer.ID, seq uint64, events []MemberEvent) error
}

// memberState tracks the current status and incarnation of a member.
type memberState struct {
	status      MemberStatus
	incarnation uint64
	suspectTime time.Time
}

// pendingProbe tracks an outstanding probe awaiting acknowledgement.
type pendingProbe struct {
	target           peer.ID
	deadline         time.Time
	indirectSent     bool
	indirectDeadline time.Time
	startTime        time.Time // for probe duration metrics
}

// pendingRelay tracks a ping sent on behalf of another node (indirect probe).
// When the Ack arrives for localSeq, we forward it back to the requester
// using their original sequence number.
type pendingRelay struct {
	requester   peer.ID
	originalSeq uint64
}

// SWIM implements the SWIM failure detection state machine.
type SWIM struct {
	self         peer.ID
	config       Config
	transport    Transport
	eventCh      chan<- MemberEvent
	disseminator *Disseminator

	mu            sync.RWMutex
	members       map[peer.ID]*memberState
	incarnation   uint64
	seq           uint64
	pendingProbes map[uint64]*pendingProbe
	pendingRelays map[uint64]*pendingRelay // localSeq → relay info

	cancel context.CancelFunc
}

// NewSWIM creates a new SWIM instance.
func NewSWIM(self peer.ID, config Config, transport Transport, eventCh chan<- MemberEvent) *SWIM {
	return &SWIM{
		self:          self,
		config:        config,
		transport:     transport,
		eventCh:       eventCh,
		disseminator:  NewDisseminator(config.RetransmitMult, 2),
		members:       make(map[peer.ID]*memberState),
		pendingProbes: make(map[uint64]*pendingProbe),
		pendingRelays: make(map[uint64]*pendingRelay),
	}
}

// AddMember adds a peer to the membership list with StatusAlive.
func (s *SWIM) AddMember(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.members[pid] = &memberState{
		status: StatusAlive,
	}
	s.disseminator.UpdateN(len(s.members) + 1) // +1 for self
	s.updateMemberGauge()
}

// RemoveMember removes a peer from the membership list.
func (s *SWIM) RemoveMember(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.members, pid)
	n := len(s.members) + 1
	s.disseminator.UpdateN(n)
	s.updateMemberGauge()
}

// GetStatus returns the current membership status of a peer, or 0 if unknown.
func (s *SWIM) GetStatus(pid peer.ID) MemberStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ms, ok := s.members[pid]; ok {
		return ms.status
	}
	return 0
}

// GetIncarnation returns the current incarnation number for this node.
func (s *SWIM) GetIncarnation() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.incarnation
}

// Members returns a list of all known member peer IDs.
func (s *SWIM) Members() []peer.ID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pids := make([]peer.ID, 0, len(s.members))
	for pid := range s.members {
		pids = append(pids, pid)
	}
	return pids
}

// probeOnce selects a random member, sends a ping with piggybacked events,
// and registers a pending probe with a deadline.
func (s *SWIM) probeOnce() {
	s.mu.Lock()

	if len(s.members) == 0 {
		s.mu.Unlock()
		return
	}

	// Pick a random member
	pids := make([]peer.ID, 0, len(s.members))
	for pid := range s.members {
		pids = append(pids, pid)
	}
	target := pids[rand.Intn(len(pids))]

	s.seq++
	seq := s.seq
	now := time.Now()
	s.pendingProbes[seq] = &pendingProbe{
		target:    target,
		deadline:  now.Add(s.config.ProbeTimeout),
		startTime: now,
	}

	s.mu.Unlock()

	events := s.disseminator.GetPiggyback(5)
	_ = s.transport.SendPing(target, seq, events)
}

// processPendingProbes checks for expired probes and either sends indirect
// probes or marks the target as suspect.
func (s *SWIM) processPendingProbes() {
	now := time.Now()
	s.mu.Lock()

	var toSuspect []uint64
	var toIndirect []uint64

	for seq, pp := range s.pendingProbes {
		if pp.indirectSent {
			// Waiting for indirect probe response
			if now.After(pp.indirectDeadline) {
				toSuspect = append(toSuspect, seq)
			}
		} else if now.After(pp.deadline) {
			// Direct probe timed out
			if s.config.IndirectProbes > 0 {
				// Collect other members for indirect probing
				others := make([]peer.ID, 0)
				for pid := range s.members {
					if pid != pp.target {
						others = append(others, pid)
					}
				}
				if len(others) > 0 {
					toIndirect = append(toIndirect, seq)
				} else {
					// No other members to relay through
					toSuspect = append(toSuspect, seq)
				}
			} else {
				toSuspect = append(toSuspect, seq)
			}
		}
	}

	// Mark suspects
	for _, seq := range toSuspect {
		pp := s.pendingProbes[seq]
		if ms, ok := s.members[pp.target]; ok {
			ms.status = StatusSuspect
			ms.suspectTime = now
		}
		s.disseminator.Enqueue(MemberEvent{
			Peer:   pp.target,
			Status: StatusSuspect,
		})
		if !pp.startTime.IsZero() {
			swimProbeDuration.WithLabelValues("suspect").Observe(time.Since(pp.startTime).Seconds())
		}
		swimProbeTotal.WithLabelValues("suspect").Inc()
		delete(s.pendingProbes, seq)
	}
	if len(toSuspect) > 0 {
		s.updateMemberGauge()
	}

	// Prepare indirect probes
	type indirectWork struct {
		target peer.ID
		seq    uint64
		relays []peer.ID
	}
	var work []indirectWork
	for _, seq := range toIndirect {
		pp := s.pendingProbes[seq]
		others := make([]peer.ID, 0)
		for pid := range s.members {
			if pid != pp.target {
				others = append(others, pid)
			}
		}
		// Shuffle and pick up to IndirectProbes relays
		rand.Shuffle(len(others), func(i, j int) {
			others[i], others[j] = others[j], others[i]
		})
		k := s.config.IndirectProbes
		if k > len(others) {
			k = len(others)
		}
		relays := others[:k]

		pp.indirectSent = true
		pp.indirectDeadline = now.Add(s.config.IndirectProbeTimeout)

		work = append(work, indirectWork{
			target: pp.target,
			seq:    seq,
			relays: relays,
		})
	}

	s.mu.Unlock()

	// Send indirect probes outside lock
	events := s.disseminator.GetPiggyback(5)
	for _, w := range work {
		for _, relay := range w.relays {
			_ = s.transport.SendPingReq(relay, w.target, w.seq, events)
		}
	}
}

// processSuspects checks all suspected members and declares them dead if the
// suspect timeout has elapsed. Dead members are removed and a Dead event is
// emitted on eventCh.
func (s *SWIM) processSuspects() {
	now := time.Now()
	s.mu.Lock()

	var dead []MemberEvent
	for pid, ms := range s.members {
		if ms.status == StatusSuspect && now.After(ms.suspectTime.Add(s.config.SuspectTimeout)) {
			dead = append(dead, MemberEvent{
				Peer:        pid,
				Status:      StatusDead,
				Incarnation: ms.incarnation,
			})
		}
	}

	for _, ev := range dead {
		delete(s.members, ev.Peer)
		s.disseminator.Enqueue(ev)
		swimProbeTotal.WithLabelValues("dead").Inc()
	}

	n := len(s.members) + 1
	if len(dead) > 0 {
		s.updateMemberGauge()
	}
	s.mu.Unlock()

	if len(dead) > 0 {
		s.disseminator.UpdateN(n)
	}

	// Emit dead events outside lock
	for _, ev := range dead {
		select {
		case s.eventCh <- ev:
		default:
		}
	}
}

// HandleMessage processes an incoming SWIM message.
func (s *SWIM) HandleMessage(from peer.ID, msg *Message) {
	switch msg.Type {
	case MsgPing:
		// Respond with Ack
		events := s.disseminator.GetPiggyback(5)
		_ = s.transport.SendAck(from, msg.Seq, events)

	case MsgAck:
		s.mu.Lock()
		// Check if this Ack is for a relayed indirect probe.
		if relay, ok := s.pendingRelays[msg.Seq]; ok {
			delete(s.pendingRelays, msg.Seq)
			s.mu.Unlock()
			// Forward the Ack back to the original requester with their seq.
			events := s.disseminator.GetPiggyback(5)
			_ = s.transport.SendAck(relay.requester, relay.originalSeq, events)
		} else {
			// Resolve our own pending probe and record metrics.
			pp := s.pendingProbes[msg.Seq]
			delete(s.pendingProbes, msg.Seq)
			s.mu.Unlock()
			if pp != nil && !pp.startTime.IsZero() {
				swimProbeDuration.WithLabelValues("ack").Observe(time.Since(pp.startTime).Seconds())
			}
			swimProbeTotal.WithLabelValues("ack").Inc()
		}

	case MsgPingReq:
		// Forward a ping to the target on behalf of the requester, using a
		// new local sequence number so that the Ack can be distinguished
		// from our own probes and forwarded back.
		s.mu.Lock()
		s.seq++
		localSeq := s.seq
		s.pendingRelays[localSeq] = &pendingRelay{
			requester:   from,
			originalSeq: msg.Seq,
		}
		s.mu.Unlock()
		events := s.disseminator.GetPiggyback(5)
		_ = s.transport.SendPing(msg.Target, localSeq, events)
	}

	// Process piggybacked membership events
	if len(msg.Events) > 0 {
		s.processEvents(msg.Events)
	}
}

// processEvents applies piggybacked membership events to the local state.
// Implements self-refutation: if an event says this node is Suspect or Dead,
// the incarnation number is incremented and an Alive event is enqueued.
func (s *SWIM) processEvents(events []MemberEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for _, ev := range events {
		if ev.Peer == s.self {
			// Self-refutation: if someone suspects or declares us dead
			if ev.Status == StatusSuspect || ev.Status == StatusDead {
				s.incarnation++
				s.disseminator.Enqueue(MemberEvent{
					Peer:        s.self,
					Status:      StatusAlive,
					Incarnation: s.incarnation,
				})
			}
			continue
		}

		ms, ok := s.members[ev.Peer]
		if !ok {
			// Unknown member — add if alive/join
			if ev.Status == StatusAlive || ev.Status == StatusJoin {
				s.members[ev.Peer] = &memberState{
					status:      StatusAlive,
					incarnation: ev.Incarnation,
				}
				s.disseminator.UpdateN(len(s.members) + 1)
				changed = true
			}
			continue
		}

		// Apply event based on incarnation and status priority
		if ev.Incarnation < ms.incarnation {
			continue
		}
		if ev.Incarnation == ms.incarnation && statusPriority(ev.Status) <= statusPriority(ms.status) {
			continue
		}

		switch ev.Status {
		case StatusAlive, StatusJoin:
			ms.status = StatusAlive
			ms.incarnation = ev.Incarnation
			ms.suspectTime = time.Time{}
			changed = true
		case StatusSuspect:
			ms.status = StatusSuspect
			ms.incarnation = ev.Incarnation
			ms.suspectTime = time.Now()
			changed = true
		case StatusDead:
			delete(s.members, ev.Peer)
			s.disseminator.UpdateN(len(s.members) + 1)
			s.disseminator.Enqueue(ev)
			changed = true
			// Emit dead event
			select {
			case s.eventCh <- ev:
			default:
			}
		}
	}
	if changed {
		s.updateMemberGauge()
	}
}

// Run starts the SWIM protocol loop, periodically probing members and
// processing timeouts. It blocks until ctx is cancelled or Close is called.
func (s *SWIM) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	ticker := time.NewTicker(s.config.ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.probeOnce()
			s.processPendingProbes()
			s.processSuspects()
		}
	}
}

// updateMemberGauge sets the Prometheus gauge for membership counts.
// Must be called with s.mu held.
func (s *SWIM) updateMemberGauge() {
	var alive, suspect float64
	for _, ms := range s.members {
		switch ms.status {
		case StatusAlive:
			alive++
		case StatusSuspect:
			suspect++
		}
	}
	swimMemberCount.WithLabelValues("alive").Set(alive)
	swimMemberCount.WithLabelValues("suspect").Set(suspect)
}

// Close stops the SWIM protocol loop.
func (s *SWIM) Close() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
}
