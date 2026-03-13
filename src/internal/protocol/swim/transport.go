package swim

import (
	"context"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pprotocol "github.com/libp2p/go-libp2p/core/protocol"
)

const ProtocolID = libp2pprotocol.ID("/opentela/swim/1.0.0")

// Async communication model: Each SWIM message is sent on a new unidirectional
// stream. Acks are sent as new streams back to the sender (not as a response on
// the same stream). The SWIM state machine matches Acks to pending probes by
// sequence number.

// LibP2PTransport sends SWIM messages over libp2p streams.
type LibP2PTransport struct {
	host    host.Host
	timeout time.Duration
}

func NewLibP2PTransport(h host.Host, timeout time.Duration) *LibP2PTransport {
	return &LibP2PTransport{host: h, timeout: timeout}
}

func (t *LibP2PTransport) send(to peer.ID, msg *Message) error {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	s, err := t.host.NewStream(ctx, to, ProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()

	data, err := msg.Marshal()
	if err != nil {
		return err
	}
	_, err = s.Write(data)
	return err
}

func (t *LibP2PTransport) SendPing(to peer.ID, seq uint64, events []MemberEvent) error {
	return t.send(to, &Message{Type: MsgPing, Seq: seq, Events: events})
}

func (t *LibP2PTransport) SendAck(to peer.ID, seq uint64, events []MemberEvent) error {
	return t.send(to, &Message{Type: MsgAck, Seq: seq, Events: events})
}

func (t *LibP2PTransport) SendPingReq(to peer.ID, target peer.ID, seq uint64, events []MemberEvent) error {
	return t.send(to, &Message{Type: MsgPingReq, Seq: seq, Target: target, Events: events})
}

// RegisterHandler sets up the libp2p stream handler for incoming SWIM messages.
func RegisterHandler(h host.Host, swim *SWIM) {
	h.SetStreamHandler(ProtocolID, func(s network.Stream) {
		defer s.Close()
		data, err := io.ReadAll(io.LimitReader(s, 64*1024)) // 64KB max
		if err != nil {
			return
		}
		msg := &Message{}
		if err := msg.Unmarshal(data); err != nil {
			return
		}
		swim.HandleMessage(s.Conn().RemotePeer(), msg)
	})
}
