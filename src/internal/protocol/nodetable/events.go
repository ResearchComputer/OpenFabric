package nodetable

import "github.com/libp2p/go-libp2p/core/peer"

// EventType distinguishes the source of a node table update.
type EventType int

const (
	EventSWIMJoin    EventType = iota // Peer joined (SWIM membership)
	EventSWIMAlive                    // Peer confirmed alive
	EventSWIMSuspect                  // Peer suspected dead
	EventSWIMDead                     // Peer confirmed dead
	EventCRDTUpdate                   // Service/attestation data from CRDT
	EventCRDTDelete                   // Peer deleted from CRDT
)

// NodeEvent is the unified event type fed into the node table writer.
type NodeEvent struct {
	Type      EventType
	PeerID    peer.ID
	Timestamp int64     // Unix timestamp; set at event creation, used instead of time.Now() in Apply
	PeerData  *PeerData // nil for Dead/Delete events
}

// PeerData carries the mutable fields that an event can update.
// Fields set to their zero value are treated as "no change" (except
// for explicit clears like Dead which resets Connected).
type PeerData struct {
	// From SWIM metadata (fast path)
	Role           []string
	IdentityGroups []string // e.g. ["model=Qwen3-8B"]
	ActiveRequests uint16
	RegionHint     uint16

	// From CRDT (slow path)
	Services            []ServiceData
	Owner               string
	ProviderID          string
	Hardware            interface{} // common.HardwareSpec — use interface to avoid import cycle
	BuildAttestation    interface{} // *attestation.BuildInfo
	IdentityAttestation interface{} // *wallet.IdentityAttestation
	TrustLevel          int
	SignedBuild         bool
	PublicAddress       string
	Version             string
	Latency             int
	Privileged          bool
}

// ServiceData is the snapshot-local representation of a service.
type ServiceData struct {
	Name          string
	Host          string
	Port          string
	Status        string
	IdentityGroup []string
	Hardware      interface{} // common.HardwareSpec
}
