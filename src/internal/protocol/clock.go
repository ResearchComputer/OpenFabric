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

	// Add resource monitoring every 2 minutes
	err = gocron.Every(2).Minutes().Do(func() {
		GetResourceManagerStats()

		// Also log current connection count for easy monitoring
		connectedPeers := ConnectedPeers()
		allPeers := AllPeers()
		common.Logger.Debugf("Connection Summary: %d connected peers, %d total known peers",
			len(connectedPeers), len(allPeers))

		// Log if we have very few connections (potential issue)
		if len(connectedPeers) == 0 {
			common.Logger.Warnf("Low connection count detected: only %d connected peers", len(connectedPeers))
			Reconnect()
		}

		// Always re-announce services so that after DAG sync the
		// service data gets a high enough CRDT priority to propagate.
		// Without this, a fresh node's initial low-priority Put is
		// never superseded once the DAG catches up.
		ReannounceLocalServices()

		// Cleanup: remove peers that have been disconnected for a long time
		// Define staleness threshold
		staleAfter := 10 * time.Minute
		table := *GetAllPeers()
		now := time.Now().Unix()
		for id, p := range table {
			if !p.Connected && p.LastSeen > 0 {
				if time.Unix(p.LastSeen, 0).Add(staleAfter).Before(time.Now()) {
					common.Logger.Warnf("Removing stale peer %s (last seen %v)", id, time.Unix(p.LastSeen, 0))
					DeleteNodeTableHook(ds.NewKey(id))
				}
			}
			// Also mark peers with very old LastSeen as disconnected
			if p.Connected && p.LastSeen > 0 && time.Unix(p.LastSeen, 0).Add(2*time.Minute).Before(time.Now()) {
				p.Connected = false
				value, err := json.Marshal(p)
				if err == nil {
					UpdateNodeTableHook(ds.NewKey(id), value)
				}
			}
			// If LastSeen is zero, initialize it now
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
