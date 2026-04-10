package protocol

import (
	"context"
	"encoding/json"
	"math/rand"
	"opentela/internal/common"
	"opentela/internal/common/process"
	"os"
	"time"

	ds "github.com/ipfs/go-datastore"
	"github.com/jasonlvhit/gocron"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
)

// var verificationKey = "ocf-verification-key"
var verificationProb = 0.5

func StartTicker() {
	err := gocron.Every(1).Minute().Do(func() {
		if rand.Float64() < verificationProb {
			Reconnect()
		}
	})
	common.ReportError(err, "Error while creating verification ticker")
	err = gocron.Every(30).Second().Do(func() {
		host, _ := GetP2PNode(nil)
		peers := host.Peerstore().Peers()
		var alive = 0
		var disconnected = 0
		for _, peer_id := range peers {
			if peer_id == host.ID() {
				continue
			}
			p, err := GetPeerFromTable(peer_id.String())
			if err != nil {
				continue
			}
			// Active liveness check: ping the peer through whatever
			// transport is available (direct or relay circuit).
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			ch := ping.Ping(ctx, host, peer_id)
			var reachable bool
			select {
			case res, ok := <-ch:
				reachable = ok && res.Error == nil
				if !reachable && ok && res.Error != nil {
					common.Logger.Debugf("Ping failed for peer %s: %v", peer_id, res.Error)
				}
			case <-ctx.Done():
			}
			cancel()
			if !reachable {
				p.Connected = false
				disconnected++
			} else {
				p.Connected = true
				alive++
			}
			p.LastSeen = time.Now().Unix()
			// Liveness updates go to the in-memory node table only.
			// Writing Connected/LastSeen to CRDT on every tick was the
			// main source of DAG bloat (~12k writes/day), causing fresh
			// nodes to spend minutes walking history before they could
			// discover peers. Structural changes (join, leave, service
			// registration) still go through CRDT.
			value, err := json.Marshal(p)
			if err == nil {
				UpdateNodeTableHook(ds.NewKey(peer_id.String()), value)
			} else {
				common.Logger.Error("Error while marshalling peer: ", peer_id.String(), err)
			}
		}
		if !process.HealthCheck() {
			common.Logger.Error("Health check failed")
			os.Exit(1)
		}
		common.Logger.Debugf("Verification Summary: %d alive peers, %d unreachable peers", alive, disconnected)
	})
	common.ReportError(err, "Error while creating verification ticker")

	// Periodic maintenance: stale peer cleanup and resource monitoring.
	err = gocron.Every(2).Minutes().Do(func() {
		GetResourceManagerStats()

		connectedPeers := ConnectedPeers()
		allPeers := AllPeers()
		common.Logger.Debugf("Connection Summary: %d connected peers, %d total known peers",
			len(connectedPeers), len(allPeers))

		if len(connectedPeers) == 0 {
			common.Logger.Warnf("Low connection count detected: only %d connected peers", len(connectedPeers))
			Reconnect()
		}

		// Cleanup: remove peers that have been disconnected for a long time.
		// Skip peers with registered services — they're actively providing
		// workloads and may be reachable through a relay even if we can't
		// ping them directly.
		staleAfter := 10 * time.Minute
		table := *GetAllPeers()
		now := time.Now().Unix()
		for id, p := range table {
			hasServices := len(p.Service) > 0
			if !p.Connected && p.LastSeen > 0 && !hasServices {
				if time.Unix(p.LastSeen, 0).Add(staleAfter).Before(time.Now()) {
					common.Logger.Debugf("Removing stale peer %s (last seen %v)", id, time.Unix(p.LastSeen, 0))
					DeleteNodeTableHook(ds.NewKey(id))
				}
			}
			// Mark peers with very old LastSeen as disconnected (in-memory only).
			// Skip peers with services — they may be behind a relay.
			if p.Connected && p.LastSeen > 0 && !hasServices && time.Unix(p.LastSeen, 0).Add(2*time.Minute).Before(time.Now()) {
				p.Connected = false
				value, err := json.Marshal(p)
				if err == nil {
					UpdateNodeTableHook(ds.NewKey(id), value)
				}
			}
			// Initialize LastSeen if zero (in-memory only).
			if p.LastSeen == 0 {
				p.LastSeen = now
				value, err := json.Marshal(p)
				if err == nil {
					UpdateNodeTableHook(ds.NewKey(id), value)
				}
			}
		}
	})
	common.ReportError(err, "Error while creating resource monitoring and clean-up ticker")
	<-gocron.Start()
}
