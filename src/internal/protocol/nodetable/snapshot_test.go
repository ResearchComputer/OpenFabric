package nodetable

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestSnapshotEmpty(t *testing.T) {
	s := NewSnapshot()
	if len(s.Peers) != 0 {
		t.Fatal("expected empty peers")
	}
	if s.Generation != 0 {
		t.Fatal("expected generation 0")
	}
}

func TestSnapshotApplyJoin(t *testing.T) {
	s := NewSnapshot()
	pid := peer.ID("test-peer-1")
	s = s.Clone()
	s.ApplyEvent(NodeEvent{
		Type:   EventSWIMJoin,
		PeerID: pid,
		PeerData: &PeerData{
			IdentityGroups: []string{"model=Qwen3-8B"},
			Role:           []string{"worker"},
		},
	})
	s.RebuildIndexes()
	s.Generation++

	if _, ok := s.Peers[pid]; !ok {
		t.Fatal("peer should exist after join")
	}
	if !s.Peers[pid].Connected {
		t.Fatal("peer should be connected after join")
	}
	peers := s.ByIdentity["model=Qwen3-8B"]
	if len(peers) != 1 || peers[0].ID != string(pid) {
		t.Fatal("identity index should contain the peer")
	}
}

func TestSnapshotApplyDead(t *testing.T) {
	s := NewSnapshot()
	pid := peer.ID("test-peer-1")
	s = s.Clone()
	s.ApplyEvent(NodeEvent{
		Type:   EventSWIMJoin,
		PeerID: pid,
		PeerData: &PeerData{
			Role: []string{"worker"},
		},
	})
	s.ApplyEvent(NodeEvent{
		Type:   EventSWIMDead,
		PeerID: pid,
	})
	s.RebuildIndexes()

	if _, ok := s.Peers[pid]; ok {
		t.Fatal("dead peer should be removed")
	}
}

func TestSnapshotCloneIsolation(t *testing.T) {
	s := NewSnapshot()
	pid := peer.ID("test-peer-1")
	s.Peers[pid] = &SnapshotPeer{ID: string(pid), Connected: true}
	s.RebuildIndexes()

	cloned := s.Clone()
	delete(cloned.Peers, pid)

	if _, ok := s.Peers[pid]; !ok {
		t.Fatal("original should not be affected by clone mutation")
	}
}

func TestSnapshotServiceIndex(t *testing.T) {
	s := NewSnapshot()
	pid := peer.ID("peer-1")
	s = s.Clone()
	s.ApplyEvent(NodeEvent{
		Type:   EventSWIMJoin,
		PeerID: pid,
		PeerData: &PeerData{
			IdentityGroups: []string{"model=Qwen3-8B"},
		},
	})
	s.ApplyEvent(NodeEvent{
		Type:   EventCRDTUpdate,
		PeerID: pid,
		PeerData: &PeerData{
			Services: []ServiceData{
				{Name: "vllm", IdentityGroup: []string{"model=Qwen3-8B"}},
			},
		},
	})
	s.RebuildIndexes()

	if len(s.ByService["vllm"]) != 1 {
		t.Fatalf("expected 1 peer for vllm, got %d", len(s.ByService["vllm"]))
	}
}
