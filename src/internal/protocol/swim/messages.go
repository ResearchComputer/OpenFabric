package swim

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
)

const MaxMetadataBytes = 256

// MessageType identifies the SWIM message kind.
type MessageType uint8

const (
	MsgPing    MessageType = 1
	MsgAck     MessageType = 2
	MsgPingReq MessageType = 3
)

// MemberStatus is the SWIM membership state.
type MemberStatus uint8

const (
	StatusJoin    MemberStatus = 1
	StatusAlive   MemberStatus = 2
	StatusSuspect MemberStatus = 3
	StatusDead    MemberStatus = 4
)

// RoleType for metadata.
type RoleType uint8

const (
	RoleUnknown RoleType = 0
	RoleWorker  RoleType = 1
	RoleHead    RoleType = 2
	RoleRelay   RoleType = 3
)

// MemberEvent is a membership state change piggy-backed on probes.
type MemberEvent struct {
	Peer        peer.ID      `json:"-"`
	Status      MemberStatus `json:"status"`
	Incarnation uint64       `json:"incarnation"`
	Meta        []byte       `json:"meta,omitempty"` // encoded Metadata, only on Join/Alive
}

// memberEventWire is the JSON wire format for MemberEvent, storing peer ID as raw bytes.
type memberEventWire struct {
	Peer        []byte       `json:"peer"`
	Status      MemberStatus `json:"status"`
	Incarnation uint64       `json:"incarnation"`
	Meta        []byte       `json:"meta,omitempty"`
}

func (e MemberEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(memberEventWire{
		Peer:        []byte(e.Peer),
		Status:      e.Status,
		Incarnation: e.Incarnation,
		Meta:        e.Meta,
	})
}

func (e *MemberEvent) UnmarshalJSON(data []byte) error {
	var w memberEventWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	e.Peer = peer.ID(w.Peer)
	e.Status = w.Status
	e.Incarnation = w.Incarnation
	e.Meta = w.Meta
	return nil
}

// Metadata is the compact peer info carried in SWIM events.
type Metadata struct {
	Role           RoleType `json:"r"`
	IdentityGroups []string `json:"ig,omitempty"`
	ActiveRequests uint16   `json:"ar"`
	RegionHint     uint16   `json:"rh"`
}

func (m *Metadata) Marshal() ([]byte, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxMetadataBytes {
		// Truncate identity groups on a copy to avoid mutating the receiver.
		igs := make([]string, len(m.IdentityGroups))
		copy(igs, m.IdentityGroups)
		tmp := *m
		tmp.IdentityGroups = igs
		for len(data) > MaxMetadataBytes && len(tmp.IdentityGroups) > 1 {
			tmp.IdentityGroups = tmp.IdentityGroups[:len(tmp.IdentityGroups)-1]
			data, err = json.Marshal(&tmp)
			if err != nil {
				return nil, err
			}
		}
	}
	return data, nil
}

func (m *Metadata) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// Message is a SWIM protocol message.
type Message struct {
	Type   MessageType   `json:"type"`
	Seq    uint64        `json:"seq"`
	Target peer.ID       `json:"-"`        // for PingReq, serialized via custom marshal
	Events []MemberEvent `json:"events,omitempty"` // piggybacked
}

// messageWire is the JSON wire format for Message, storing Target peer ID as raw bytes.
type messageWire struct {
	Type   MessageType   `json:"type"`
	Seq    uint64        `json:"seq"`
	Target []byte        `json:"target,omitempty"`
	Events []MemberEvent `json:"events,omitempty"`
}

func (m *Message) Marshal() ([]byte, error) {
	wire := messageWire{
		Type:   m.Type,
		Seq:    m.Seq,
		Events: m.Events,
	}
	if len(m.Target) > 0 {
		wire.Target = []byte(m.Target)
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	// Length-prefix: 4 bytes big-endian + payload
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)
	return buf, nil
}

func (m *Message) Unmarshal(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("message too short: %d bytes", len(data))
	}
	length := binary.BigEndian.Uint32(data[:4])
	if int(length) > len(data)-4 {
		return fmt.Errorf("message length mismatch: header says %d, have %d", length, len(data)-4)
	}
	var wire messageWire
	if err := json.Unmarshal(data[4:4+length], &wire); err != nil {
		return err
	}
	m.Type = wire.Type
	m.Seq = wire.Seq
	m.Target = peer.ID(wire.Target)
	m.Events = wire.Events
	return nil
}
