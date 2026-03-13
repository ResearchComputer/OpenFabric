package swim

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// mockTransport records all sent messages for test assertions.
type mockTransport struct {
	mu       sync.Mutex
	pings    []sentPing
	acks     []sentAck
	pingReqs []sentPingReq
}

type sentPing struct {
	to     peer.ID
	seq    uint64
	events []MemberEvent
}

type sentAck struct {
	to     peer.ID
	seq    uint64
	events []MemberEvent
}

type sentPingReq struct {
	to     peer.ID
	target peer.ID
	seq    uint64
	events []MemberEvent
}

func (m *mockTransport) SendPing(to peer.ID, seq uint64, events []MemberEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pings = append(m.pings, sentPing{to: to, seq: seq, events: events})
	return nil
}

func (m *mockTransport) SendAck(to peer.ID, seq uint64, events []MemberEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acks = append(m.acks, sentAck{to: to, seq: seq, events: events})
	return nil
}

func (m *mockTransport) SendPingReq(to peer.ID, target peer.ID, seq uint64, events []MemberEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pingReqs = append(m.pingReqs, sentPingReq{to: to, target: target, seq: seq, events: events})
	return nil
}

func (m *mockTransport) getPings() []sentPing {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]sentPing, len(m.pings))
	copy(cp, m.pings)
	return cp
}

func (m *mockTransport) getAcks() []sentAck {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]sentAck, len(m.acks))
	copy(cp, m.acks)
	return cp
}

func (m *mockTransport) getPingReqs() []sentPingReq {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]sentPingReq, len(m.pingReqs))
	copy(cp, m.pingReqs)
	return cp
}

func testConfig() Config {
	return Config{
		ProbeInterval:        100 * time.Millisecond,
		ProbeTimeout:         50 * time.Millisecond,
		IndirectProbeTimeout: 50 * time.Millisecond,
		IndirectProbes:       1,
		SuspectTimeout:       100 * time.Millisecond,
		RetransmitMult:       3,
	}
}

func TestSWIMAddRemoveMembers(t *testing.T) {
	transport := &mockTransport{}
	eventCh := make(chan MemberEvent, 10)
	s := NewSWIM(peer.ID("self"), testConfig(), transport, eventCh)
	defer s.Close()

	s.AddMember(peer.ID("p1"))
	s.AddMember(peer.ID("p2"))

	members := s.Members()
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	status := s.GetStatus(peer.ID("p1"))
	if status != StatusAlive {
		t.Fatalf("expected StatusAlive, got %d", status)
	}

	s.RemoveMember(peer.ID("p1"))
	members = s.Members()
	if len(members) != 1 {
		t.Fatalf("expected 1 member after remove, got %d", len(members))
	}

	status = s.GetStatus(peer.ID("p1"))
	if status != 0 {
		t.Fatalf("expected 0 for unknown member, got %d", status)
	}
}

func TestSWIMProbeAlive(t *testing.T) {
	transport := &mockTransport{}
	eventCh := make(chan MemberEvent, 10)
	s := NewSWIM(peer.ID("self"), testConfig(), transport, eventCh)
	defer s.Close()

	s.AddMember(peer.ID("p1"))

	// Trigger a probe
	s.probeOnce()

	pings := transport.getPings()
	if len(pings) != 1 {
		t.Fatalf("expected 1 ping, got %d", len(pings))
	}
	if pings[0].to != peer.ID("p1") {
		t.Fatalf("ping sent to wrong peer: %s", pings[0].to)
	}

	// Simulate receiving an Ack
	s.HandleMessage(peer.ID("p1"), &Message{
		Type: MsgAck,
		Seq:  pings[0].seq,
	})

	// Member should still be alive
	status := s.GetStatus(peer.ID("p1"))
	if status != StatusAlive {
		t.Fatalf("expected StatusAlive after ack, got %d", status)
	}

	// Pending probe should be resolved
	s.mu.RLock()
	pending := len(s.pendingProbes)
	s.mu.RUnlock()
	if pending != 0 {
		t.Fatalf("expected 0 pending probes after ack, got %d", pending)
	}
}

func TestSWIMSuspectOnTimeout(t *testing.T) {
	cfg := testConfig()
	cfg.IndirectProbes = 0 // No indirect probes — go straight to suspect

	transport := &mockTransport{}
	eventCh := make(chan MemberEvent, 10)
	s := NewSWIM(peer.ID("self"), cfg, transport, eventCh)
	defer s.Close()

	s.AddMember(peer.ID("p1"))

	// Trigger a probe
	s.probeOnce()

	pings := transport.getPings()
	if len(pings) != 1 {
		t.Fatalf("expected 1 ping, got %d", len(pings))
	}

	// Wait for probe to time out
	time.Sleep(cfg.ProbeTimeout + 20*time.Millisecond)

	// Process pending probes — should mark suspect since IndirectProbes=0
	s.processPendingProbes()

	status := s.GetStatus(peer.ID("p1"))
	if status != StatusSuspect {
		t.Fatalf("expected StatusSuspect after timeout, got %d", status)
	}
}

func TestSWIMSuspectOnTimeoutWithIndirect(t *testing.T) {
	cfg := testConfig()
	cfg.IndirectProbes = 1

	transport := &mockTransport{}
	eventCh := make(chan MemberEvent, 10)
	s := NewSWIM(peer.ID("self"), cfg, transport, eventCh)
	defer s.Close()

	s.AddMember(peer.ID("p1"))
	s.AddMember(peer.ID("p2"))

	// Force probe to target p1 by removing p2 first, probing, then adding p2 back.
	// Actually, let's just probe and handle whichever target gets picked.
	s.probeOnce()

	pings := transport.getPings()
	if len(pings) != 1 {
		t.Fatalf("expected 1 ping, got %d", len(pings))
	}
	target := pings[0].to
	seq := pings[0].seq

	// Wait for direct probe to time out
	time.Sleep(cfg.ProbeTimeout + 20*time.Millisecond)

	// Process pending probes — should send indirect probe
	s.processPendingProbes()

	pingReqs := transport.getPingReqs()
	if len(pingReqs) != 1 {
		t.Fatalf("expected 1 ping-req, got %d", len(pingReqs))
	}
	if pingReqs[0].target != target {
		t.Fatalf("ping-req target mismatch: got %s, want %s", pingReqs[0].target, target)
	}
	if pingReqs[0].seq != seq {
		t.Fatalf("ping-req seq mismatch: got %d, want %d", pingReqs[0].seq, seq)
	}

	// Wait for indirect probe to time out
	time.Sleep(cfg.IndirectProbeTimeout + 20*time.Millisecond)

	// Process pending probes again — should mark suspect
	s.processPendingProbes()

	status := s.GetStatus(target)
	if status != StatusSuspect {
		t.Fatalf("expected StatusSuspect after indirect timeout, got %d", status)
	}
}

func TestSWIMDeadAfterSuspectTimeout(t *testing.T) {
	cfg := testConfig()
	cfg.IndirectProbes = 0
	cfg.SuspectTimeout = 50 * time.Millisecond

	transport := &mockTransport{}
	eventCh := make(chan MemberEvent, 10)
	s := NewSWIM(peer.ID("self"), cfg, transport, eventCh)
	defer s.Close()

	s.AddMember(peer.ID("p1"))

	// Trigger probe and let it time out
	s.probeOnce()
	time.Sleep(cfg.ProbeTimeout + 20*time.Millisecond)
	s.processPendingProbes()

	// Verify suspect
	status := s.GetStatus(peer.ID("p1"))
	if status != StatusSuspect {
		t.Fatalf("expected StatusSuspect, got %d", status)
	}

	// Wait for suspect timeout
	time.Sleep(cfg.SuspectTimeout + 20*time.Millisecond)

	// Process suspects — should declare dead
	s.processSuspects()

	// Member should be removed
	status = s.GetStatus(peer.ID("p1"))
	if status != 0 {
		t.Fatalf("expected member to be removed (status 0) after dead, got %d", status)
	}

	// Should have received a Dead event
	select {
	case ev := <-eventCh:
		if ev.Peer != peer.ID("p1") {
			t.Fatalf("dead event for wrong peer: %s", ev.Peer)
		}
		if ev.Status != StatusDead {
			t.Fatalf("expected StatusDead event, got %d", ev.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Dead event")
	}
}

func TestSWIMSelfRefutation(t *testing.T) {
	transport := &mockTransport{}
	eventCh := make(chan MemberEvent, 10)
	s := NewSWIM(peer.ID("self"), testConfig(), transport, eventCh)
	defer s.Close()

	initialInc := s.GetIncarnation()

	// Receive a message with a Suspect event about self
	s.HandleMessage(peer.ID("p1"), &Message{
		Type: MsgPing,
		Seq:  1,
		Events: []MemberEvent{
			{
				Peer:        peer.ID("self"),
				Status:      StatusSuspect,
				Incarnation: initialInc,
			},
		},
	})

	newInc := s.GetIncarnation()
	if newInc <= initialInc {
		t.Fatalf("expected incarnation to increase from %d, got %d", initialInc, newInc)
	}

	// Also test Dead event about self triggers refutation
	currentInc := s.GetIncarnation()
	s.HandleMessage(peer.ID("p2"), &Message{
		Type: MsgPing,
		Seq:  2,
		Events: []MemberEvent{
			{
				Peer:        peer.ID("self"),
				Status:      StatusDead,
				Incarnation: currentInc,
			},
		},
	})

	newerInc := s.GetIncarnation()
	if newerInc <= currentInc {
		t.Fatalf("expected incarnation to increase on Dead refutation from %d, got %d", currentInc, newerInc)
	}
}

func TestSWIMHandlePingReq(t *testing.T) {
	transport := &mockTransport{}
	eventCh := make(chan MemberEvent, 10)
	s := NewSWIM(peer.ID("relay"), testConfig(), transport, eventCh)
	defer s.Close()

	s.AddMember(peer.ID("target"))

	// Receive a PingReq from "requester": asking relay to ping "target"
	s.HandleMessage(peer.ID("requester"), &Message{
		Type:   MsgPingReq,
		Seq:    42,
		Target: peer.ID("target"),
	})

	// Relay should have sent a Ping to "target" with a new local seq
	pings := transport.getPings()
	if len(pings) != 1 {
		t.Fatalf("expected 1 forwarded ping, got %d", len(pings))
	}
	if pings[0].to != peer.ID("target") {
		t.Fatalf("forwarded ping to wrong peer: %s", pings[0].to)
	}
	localSeq := pings[0].seq

	// Now simulate "target" responding with an Ack for the local seq
	s.HandleMessage(peer.ID("target"), &Message{
		Type: MsgAck,
		Seq:  localSeq,
	})

	// Relay should have forwarded an Ack back to the original requester
	// with the original sequence number (42)
	acks := transport.getAcks()
	if len(acks) != 1 {
		t.Fatalf("expected 1 forwarded ack, got %d", len(acks))
	}
	if acks[0].to != peer.ID("requester") {
		t.Fatalf("ack forwarded to wrong peer: got %s, want requester", acks[0].to)
	}
	if acks[0].seq != 42 {
		t.Fatalf("ack has wrong seq: got %d, want 42", acks[0].seq)
	}
}

func TestSWIMIndirectProbeSuccess(t *testing.T) {
	// Full indirect probe round-trip: A probes T, times out, sends PingReq
	// to B, B pings T, T acks, B forwards ack to A, A resolves the probe.
	cfg := testConfig()
	cfg.IndirectProbes = 1

	transportA := &mockTransport{}
	eventCh := make(chan MemberEvent, 10)
	nodeA := NewSWIM(peer.ID("A"), cfg, transportA, eventCh)
	defer nodeA.Close()

	transportB := &mockTransport{}
	nodeB := NewSWIM(peer.ID("B"), cfg, transportB, make(chan MemberEvent, 10))
	defer nodeB.Close()

	nodeA.AddMember(peer.ID("T"))
	nodeA.AddMember(peer.ID("B"))
	nodeB.AddMember(peer.ID("T"))

	// Force A to probe T: remove B, probe, add B back
	nodeA.RemoveMember(peer.ID("B"))
	nodeA.probeOnce()
	nodeA.AddMember(peer.ID("B"))

	pingsA := transportA.getPings()
	if len(pingsA) != 1 || pingsA[0].to != peer.ID("T") {
		t.Fatalf("expected A to ping T, got %v", pingsA)
	}
	originalSeq := pingsA[0].seq

	// Let A's direct probe time out
	time.Sleep(cfg.ProbeTimeout + 20*time.Millisecond)
	nodeA.processPendingProbes()

	// A should have sent a PingReq to B
	pingReqs := transportA.getPingReqs()
	if len(pingReqs) != 1 {
		t.Fatalf("expected 1 ping-req from A, got %d", len(pingReqs))
	}
	if pingReqs[0].to != peer.ID("B") {
		t.Fatalf("ping-req sent to wrong relay: %s", pingReqs[0].to)
	}

	// Simulate B receiving the PingReq
	nodeB.HandleMessage(peer.ID("A"), &Message{
		Type:   MsgPingReq,
		Seq:    pingReqs[0].seq,
		Target: peer.ID("T"),
	})

	// B should have pinged T with a new local seq
	pingsB := transportB.getPings()
	if len(pingsB) != 1 || pingsB[0].to != peer.ID("T") {
		t.Fatalf("expected B to ping T, got %v", pingsB)
	}

	// Simulate T acking B
	nodeB.HandleMessage(peer.ID("T"), &Message{
		Type: MsgAck,
		Seq:  pingsB[0].seq,
	})

	// B should have forwarded an Ack to A with the original seq
	acksB := transportB.getAcks()
	if len(acksB) != 1 {
		t.Fatalf("expected 1 forwarded ack from B, got %d", len(acksB))
	}
	if acksB[0].to != peer.ID("A") {
		t.Fatalf("ack forwarded to wrong node: got %s, want A", acksB[0].to)
	}
	if acksB[0].seq != originalSeq {
		t.Fatalf("forwarded ack has wrong seq: got %d, want %d", acksB[0].seq, originalSeq)
	}

	// Simulate A receiving the forwarded Ack
	nodeA.HandleMessage(peer.ID("B"), &Message{
		Type: MsgAck,
		Seq:  originalSeq,
	})

	// A should have resolved the pending probe — T is still alive
	nodeA.mu.RLock()
	pending := len(nodeA.pendingProbes)
	nodeA.mu.RUnlock()
	if pending != 0 {
		t.Fatalf("expected 0 pending probes after indirect ack, got %d", pending)
	}
	if status := nodeA.GetStatus(peer.ID("T")); status != StatusAlive {
		t.Fatalf("expected T to be alive after indirect probe success, got %d", status)
	}
}

func TestSWIMRunLoop(t *testing.T) {
	cfg := testConfig()
	cfg.ProbeInterval = 30 * time.Millisecond
	cfg.IndirectProbes = 0
	cfg.SuspectTimeout = 50 * time.Millisecond

	transport := &mockTransport{}
	eventCh := make(chan MemberEvent, 10)
	s := NewSWIM(peer.ID("self"), cfg, transport, eventCh)

	s.AddMember(peer.ID("p1"))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	// Wait for Run to finish via context cancellation
	<-done

	// Should have sent at least a few pings
	pings := transport.getPings()
	if len(pings) == 0 {
		t.Fatal("expected at least one ping from Run loop")
	}
}
