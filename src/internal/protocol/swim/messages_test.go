package swim

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestPingRoundTrip(t *testing.T) {
	msg := &Message{
		Type: MsgPing,
		Seq:  42,
	}
	data, err := msg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	decoded := &Message{}
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != MsgPing || decoded.Seq != 42 {
		t.Fatalf("mismatch: got type=%d seq=%d", decoded.Type, decoded.Seq)
	}
}

func TestAckWithEvents(t *testing.T) {
	events := []MemberEvent{
		{Peer: peer.ID("p1"), Status: StatusAlive, Incarnation: 1},
		{Peer: peer.ID("p2"), Status: StatusDead, Incarnation: 5},
	}
	msg := &Message{
		Type:   MsgAck,
		Seq:    10,
		Events: events,
	}
	data, err := msg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	decoded := &Message{}
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(decoded.Events))
	}
	if decoded.Events[0].Status != StatusAlive {
		t.Fatal("first event should be Alive")
	}
	if decoded.Events[1].Peer != peer.ID("p2") {
		t.Fatal("second event peer mismatch")
	}
}

func TestMemberEventMetadata(t *testing.T) {
	meta := Metadata{
		Role:           RoleWorker,
		IdentityGroups: []string{"model=Qwen3-8B", "all"},
		ActiveRequests: 5,
		RegionHint:     100,
	}
	data, err := meta.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxMetadataBytes {
		t.Fatalf("metadata too large: %d > %d", len(data), MaxMetadataBytes)
	}
	decoded := &Metadata{}
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if decoded.ActiveRequests != 5 || decoded.RegionHint != 100 {
		t.Fatal("metadata fields mismatch")
	}
	if len(decoded.IdentityGroups) != 2 || decoded.IdentityGroups[0] != "model=Qwen3-8B" {
		t.Fatal("identity groups mismatch")
	}
}
