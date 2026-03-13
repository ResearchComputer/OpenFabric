package nodetable

import (
	"sync/atomic"

	"github.com/libp2p/go-libp2p/core/peer"
)

// SnapshotPeer is the node table's view of a peer. It combines data
// from SWIM membership events and CRDT state updates.
type SnapshotPeer struct {
	ID        string
	PeerID    peer.ID
	Connected bool     // from SWIM: alive/suspect
	Suspect   bool     // from SWIM: in suspect state
	LastSeen  int64    // unix timestamp
	Role      []string

	// From SWIM metadata (available immediately on join)
	IdentityGroups []string
	ActiveRequests uint16
	RegionHint     uint16

	// From CRDT (available after sync, may be nil initially)
	Services            []ServiceData
	Owner               string
	ProviderID          string
	PublicAddress       string
	Version             string
	Latency             int
	Privileged          bool
	Hardware            interface{}
	BuildAttestation    interface{}
	IdentityAttestation interface{}
	TrustLevel          int
	SignedBuild         bool
}

// NodeTableSnapshot is an immutable point-in-time view of the node table.
// Readers get zero-contention access via atomic.Pointer.
type NodeTableSnapshot struct {
	Peers      map[peer.ID]*SnapshotPeer
	ByService  map[string][]*SnapshotPeer // service name → connected peers
	ByIdentity map[string][]*SnapshotPeer // identity group → connected peers
	ByRole     map[string][]*SnapshotPeer // role → connected peers
	Generation uint64
}

// NodeTable is the scalable node table with lock-free reads.
type NodeTable struct {
	snapshot atomic.Pointer[NodeTableSnapshot]
}

// NewNodeTable creates a new node table with an empty snapshot.
func NewNodeTable() *NodeTable {
	nt := &NodeTable{}
	nt.snapshot.Store(NewSnapshot())
	return nt
}

// Snapshot returns the current immutable snapshot (lock-free).
func (nt *NodeTable) Snapshot() *NodeTableSnapshot {
	return nt.snapshot.Load()
}

// Store atomically publishes a new snapshot.
func (nt *NodeTable) Store(s *NodeTableSnapshot) {
	nt.snapshot.Store(s)
}

// NewSnapshot returns a new empty snapshot.
func NewSnapshot() *NodeTableSnapshot {
	return &NodeTableSnapshot{
		Peers:      make(map[peer.ID]*SnapshotPeer),
		ByService:  make(map[string][]*SnapshotPeer),
		ByIdentity: make(map[string][]*SnapshotPeer),
		ByRole:     make(map[string][]*SnapshotPeer),
	}
}

// Clone creates a shallow copy of the snapshot. Peer structs are
// copied by value (pointer to new SnapshotPeer) so mutations to the
// clone don't affect the original.
func (s *NodeTableSnapshot) Clone() *NodeTableSnapshot {
	c := &NodeTableSnapshot{
		Peers:      make(map[peer.ID]*SnapshotPeer, len(s.Peers)),
		ByService:  make(map[string][]*SnapshotPeer),
		ByIdentity: make(map[string][]*SnapshotPeer),
		ByRole:     make(map[string][]*SnapshotPeer),
		Generation: s.Generation,
	}
	for k, v := range s.Peers {
		cp := *v // value copy
		c.Peers[k] = &cp
	}
	return c
}

// ApplyEvent mutates the snapshot in place (call on a Clone, not the live snapshot).
func (s *NodeTableSnapshot) ApplyEvent(e NodeEvent) {
	switch e.Type {
	case EventSWIMJoin, EventSWIMAlive:
		p, ok := s.Peers[e.PeerID]
		if !ok {
			p = &SnapshotPeer{
				ID:       string(e.PeerID),
				PeerID:   e.PeerID,
				LastSeen: e.Timestamp,
			}
			s.Peers[e.PeerID] = p
		}
		p.Connected = true
		p.Suspect = false
		p.LastSeen = e.Timestamp
		if e.PeerData != nil {
			if len(e.PeerData.Role) > 0 {
				p.Role = e.PeerData.Role
			}
			if len(e.PeerData.IdentityGroups) > 0 {
				p.IdentityGroups = e.PeerData.IdentityGroups
			}
			p.ActiveRequests = e.PeerData.ActiveRequests
			p.RegionHint = e.PeerData.RegionHint
		}

	case EventSWIMSuspect:
		p, ok := s.Peers[e.PeerID]
		if !ok {
			// Suspect before Join: create peer in suspect state so that
			// a later Join event correctly finds it already suspected.
			p = &SnapshotPeer{
				ID:        string(e.PeerID),
				PeerID:    e.PeerID,
				Connected: true,
				LastSeen:  e.Timestamp,
			}
			s.Peers[e.PeerID] = p
		}
		p.Suspect = true
		p.LastSeen = e.Timestamp

	case EventSWIMDead, EventCRDTDelete:
		delete(s.Peers, e.PeerID)

	case EventCRDTUpdate:
		p, ok := s.Peers[e.PeerID]
		if !ok {
			p = &SnapshotPeer{
				ID:       string(e.PeerID),
				PeerID:   e.PeerID,
				LastSeen: e.Timestamp,
			}
			s.Peers[e.PeerID] = p
		}
		if e.PeerData != nil {
			if len(e.PeerData.Services) > 0 {
				p.Services = e.PeerData.Services
			}
			if e.PeerData.Owner != "" {
				p.Owner = e.PeerData.Owner
			}
			if e.PeerData.ProviderID != "" {
				p.ProviderID = e.PeerData.ProviderID
			}
			if e.PeerData.PublicAddress != "" {
				p.PublicAddress = e.PeerData.PublicAddress
			}
			if e.PeerData.Version != "" {
				p.Version = e.PeerData.Version
			}
			p.Hardware = e.PeerData.Hardware
			p.BuildAttestation = e.PeerData.BuildAttestation
			p.IdentityAttestation = e.PeerData.IdentityAttestation
			p.TrustLevel = e.PeerData.TrustLevel
			p.SignedBuild = e.PeerData.SignedBuild
			p.Latency = e.PeerData.Latency
			p.Privileged = e.PeerData.Privileged
			if len(e.PeerData.IdentityGroups) > 0 {
				p.IdentityGroups = e.PeerData.IdentityGroups
			}
		}
	}
}

// RebuildIndexes reconstructs ByService, ByIdentity, ByRole from Peers.
// Called after applying a batch of events, before publishing the snapshot.
func (s *NodeTableSnapshot) RebuildIndexes() {
	s.ByService = make(map[string][]*SnapshotPeer)
	s.ByIdentity = make(map[string][]*SnapshotPeer)
	s.ByRole = make(map[string][]*SnapshotPeer)

	for _, p := range s.Peers {
		if !p.Connected || p.Suspect {
			continue
		}
		for _, svc := range p.Services {
			s.ByService[svc.Name] = append(s.ByService[svc.Name], p)
		}
		for _, ig := range p.IdentityGroups {
			s.ByIdentity[ig] = append(s.ByIdentity[ig], p)
		}
		for _, r := range p.Role {
			s.ByRole[r] = append(s.ByRole[r], p)
		}
	}
}
