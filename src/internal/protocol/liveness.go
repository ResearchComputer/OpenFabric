package protocol

import (
	"context"
	"encoding/json"
	"opentela/internal/common"
	"strings"
	"sync"
	"time"

	ds "github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/viper"
)

const (
	// probeDialTimeout bounds a single liveness probe. A live relay answers
	// NO_RESERVATION immediately, so this only bounds the pathological cases.
	probeDialTimeout = 8 * time.Second
	// probeAddrTTL is how long the synthesised circuit address stays in the
	// peerstore. Short on purpose: it exists for this dial, not as routing
	// state, and a stale circuit address would outlive the relay reservation.
	probeAddrTTL = 30 * time.Second
)

// probeVerdict is the outcome of a liveness probe against a peer we can no
// longer reach directly.
type probeVerdict int

const (
	// probeAlive: the peer answered. It is up.
	probeAlive probeVerdict = iota
	// probeDeadAuthoritative: the relay told us it holds no reservation for
	// this peer. The relay is the authority on whether a worker is still
	// attached to it, so this is a positive statement of absence — not a
	// transient failure — and it is the only verdict that may evict.
	probeDeadAuthoritative
	// probeUnknown: the probe failed for a reason that says nothing about the
	// peer (relay unreachable, dial timeout, relay refusing us). Evicting on
	// this would wipe every worker behind a restarting relay, so it must never
	// evict.
	probeUnknown
)

func (v probeVerdict) String() string {
	switch v {
	case probeAlive:
		return "alive"
	case probeDeadAuthoritative:
		return "dead(authoritative)"
	default:
		return "unknown"
	}
}

// relayNoReservation is the status name go-libp2p formats into the dial error
// when a relay has no reservation for the destination
// (circuitv2/pb.Status_NO_RESERVATION, 204).
//
// This is matched as a string on purpose: circuitv2/client.relayError is
// unexported, so there is no type to assert on and no sentinel to compare with
// errors.Is. If go-libp2p ever changes the wording, this stops matching and
// every probe degrades to probeUnknown — which fails safe (ghosts linger rather
// than healthy peers being evicted). TestClassifyProbeError pins the current
// wording so the change surfaces as a test failure rather than silent drift.
const relayNoReservation = "NO_RESERVATION"

// classifyProbeError maps a probe error onto the eviction decision. See
// probeVerdict for why only NO_RESERVATION is allowed to evict.
func classifyProbeError(err error) probeVerdict {
	if err == nil {
		return probeAlive
	}
	if strings.Contains(err.Error(), relayNoReservation) {
		return probeDeadAuthoritative
	}
	return probeUnknown
}

// ProbePeerLiveness asks a peer's relay whether that peer is still attached.
//
// It exists because a worker behind a relay can vanish without the head ever
// noticing: the head may hold no direct connection to it (so DisconnectedF
// never fires) and libp2p eventually drops it from the peerstore (so the ping
// ticker, which walks the peerstore, stops covering it). The entry then freezes
// as connected:true and routing keeps selecting a dead endpoint.
//
// The verdict is deliberately asymmetric. Only the relay answering
// NO_RESERVATION counts as proof of absence. Every other failure — including
// being unable to reach the relay at all — returns probeUnknown, because it is
// evidence about the relay rather than the peer, and evicting on it would
// retire every worker behind a relay that is merely restarting.
func ProbePeerLiveness(ctx context.Context, peerID string) probeVerdict {
	h, _ := GetP2PNode(nil)
	if h == nil {
		return probeUnknown
	}
	entry, err := GetPeerFromTable(peerID)
	if err != nil || entry.RelayPeer == "" {
		// No advertised relay means there is nothing authoritative to ask.
		return probeUnknown
	}
	pid, err := peer.Decode(peerID)
	if err != nil {
		return probeUnknown
	}
	relayPID, err := peer.Decode(entry.RelayPeer)
	if err != nil {
		return probeUnknown
	}
	// If the relay itself is unreachable, any dial failure below would say
	// nothing about the worker. Bail out early rather than manufacture a
	// verdict from an unanswerable question.
	if h.Network().Connectedness(relayPID) != network.Connected {
		return probeUnknown
	}

	circuit, err := multiaddr.NewMultiaddr("/p2p/" + entry.RelayPeer + "/p2p-circuit")
	if err != nil {
		return probeUnknown
	}
	h.Peerstore().AddAddr(pid, circuit, probeAddrTTL)

	// Relay circuits are Limited connections; libp2p refuses to use them unless
	// we opt in explicitly (same as ping does internally).
	dialCtx, cancel := context.WithTimeout(
		network.WithAllowLimitedConn(ctx, "liveness-probe"), probeDialTimeout)
	defer cancel()

	if err := h.Connect(dialCtx, peer.AddrInfo{
		ID:    pid,
		Addrs: []multiaddr.Multiaddr{circuit},
	}); err != nil {
		verdict := classifyProbeError(err)
		common.Logger.Debugf("Liveness probe for %s via relay %s: %v (%v)",
			peerID[:12], entry.RelayPeer[:12], verdict, err)
		return verdict
	}

	// The circuit opened, so the relay still holds a reservation. Confirm the
	// peer itself answers rather than trusting the reservation alone — a
	// reservation can briefly outlive the process that made it.
	select {
	case res, ok := <-ping.Ping(dialCtx, h, pid):
		if ok && res.Error == nil {
			return probeAlive
		}
		if ok && res.Error != nil {
			return classifyProbeError(res.Error)
		}
		return probeUnknown
	case <-dialCtx.Done():
		return probeUnknown
	}
}

// Probes run concurrently under a cap, and the whole pass is time-boxed. Run
// serially, a mesh with many unreachable workers would spend
// len(peers) * probeDialTimeout in one pass and overrun the 30s tick that calls
// it, stacking passes on top of each other.
const (
	probeMaxConcurrent = 8
	probeSweepBudget   = 20 * time.Second
)

// probeUnreachableServicePeers retires service-carrying peers that their relay
// says are gone.
//
// It covers the blind spot that let dead workers linger: the 30s ping loop only
// walks the peerstore, and a worker whose job died is dropped from the peerstore
// entirely, so nothing ever re-examined it. Only peers we hold no direct
// connection to are probed — the rest are already covered by that ping loop.
func probeUnreachableServicePeers() {
	ctx, cancel := context.WithTimeout(context.Background(), probeSweepBudget)
	defer cancel()
	now := time.Now()

	var wg sync.WaitGroup
	sem := make(chan struct{}, probeMaxConcurrent)

	for id, p := range *GetAllPeers() {
		if !p.Connected || len(p.Service) == 0 {
			continue
		}
		peerID := strings.TrimPrefix(id, "/")
		if IsDirectlyConnected(peerID) {
			continue
		}

		wg.Add(1)
		go func(key, peerID string, p Peer) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				// Out of budget: leave this peer for the next tick rather than
				// letting the pass run long. Skipping is the safe direction —
				// it delays an eviction, it never causes a wrong one.
				return
			}

			if decideSweepAction(p, ProbePeerLiveness(ctx, peerID), now) != sweepMarkDisconnected {
				return
			}

			common.Logger.Infof("Retiring peer %s: relay holds no reservation (last seen %v)",
				peerID, time.Unix(p.LastSeen, 0))
			p.Connected = false
			if value, err := json.Marshal(p); err == nil {
				UpdateNodeTableHook(ds.NewKey(key), value)
			}
			publishEviction(ds.NewKey(peerID), p)
		}(id, peerID, p)
	}
	wg.Wait()
}

// publishEviction tells the rest of the mesh that we proved a peer is gone.
//
// Routine liveness is deliberately kept in-memory — writing Connected/LastSeen
// to the CRDT on every tick was the original source of DAG bloat (see
// clock.go). An authoritative eviction is different: it is rare, structural,
// and needs to converge. Without it a head that cannot reach the peer's relay
// can never reach a verdict of its own and would serve the ghost indefinitely,
// which is exactly the split observed between ocf-1 and ocf-2.
//
// Only heads publish. A worker has no business retiring its peers, and
// receivers drop records from evictors they cannot confirm to be heads.
func publishEviction(key ds.Key, p Peer) {
	if viper.GetString("role") != "head" {
		return
	}
	store, _ := GetCRDTStore()
	if store == nil {
		return
	}

	p.Status = LEFT
	p.Connected = false
	p.EvictedBy = MyID

	value, err := json.Marshal(p)
	if err != nil {
		common.Logger.Errorf("Could not marshal eviction record for %s: %v", p.ID, err)
		return
	}
	if err := store.Put(context.Background(), key, value); err != nil {
		common.Logger.Errorf("Could not publish eviction record for %s: %v", p.ID, err)
		return
	}
	common.Logger.Infof("Published eviction record for %s", p.ID)
}
