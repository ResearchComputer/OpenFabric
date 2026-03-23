package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"opentela/internal/common"
	"strings"
	"sync"
	"time"

	crdt "opentela/internal/protocol/go-ds-crdt"

	ipfslite "github.com/hsanjuan/ipfs-lite"
	ds "github.com/ipfs/go-datastore"
	badger "github.com/ipfs/go-ds-badger"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spf13/viper"
)

var (
	pubsubTopic = "ocf-crdt"
	pubsubKey   = "ocf-crdt"
	pubsubNet   = "ocf-crdt-net"
)
var ipfs *ipfslite.Peer
var crdtStore *crdt.Datastore
var once sync.Once
var cancelSubscriptions context.CancelFunc

func GetCRDTStore() (*crdt.Datastore, context.CancelFunc) {
	once.Do(func() {
		mode := viper.GetString("mode")
		host, dht := GetP2PNode(nil)
		ctx := context.Background()
		common.Logger.Debug("Creating CRDT store, dbpath: " + common.GetDBPath(host.ID().String()))
		store, err := badger.NewDatastore(common.GetDBPath(host.ID().String()), &badger.DefaultOptions)
		common.ReportError(err, "Error while creating datastore")

		ipfs, err = ipfslite.New(ctx, store, nil, host, &dht, nil)
		common.ReportError(err, "Error while creating ipfs lite node")
		pubsubParams := pubsub.DefaultGossipSubParams()
		if viper.GetBool("scalability.crdt_tuned") {
			pubsubParams.D = viper.GetInt("crdt.tuned_gossipsub_d")     // default 10
			pubsubParams.Dlo = viper.GetInt("crdt.tuned_gossipsub_dlo") // default 4
			pubsubParams.Dhi = viper.GetInt("crdt.tuned_gossipsub_dhi") // default 16
		}
		// Default GossipSub params (D=6, Dlo=4, Dhi=12) work well for
		// networks of any size.
		psub, err := pubsub.NewGossipSub(ctx, host, pubsub.WithGossipSubParams(pubsubParams))
		common.ReportError(err, "Error while creating pubsub")

		// Bootstrap BEFORE joining the CRDT pubsub topic. GossipSub
		// exchanges topic subscriptions with already-connected peers.
		// If we join the topic before peers are connected, the relay
		// won't see cloud nodes as CRDT topic subscribers, causing
		// unidirectional mesh and CRDT data not propagating.
		addsInfo, err := peer.AddrInfosFromP2pAddrs(getDefaultBootstrapPeers(nil, mode)...)
		common.ReportError(err, "Error while getting bootstrap peers")
		ipfs.Bootstrap(addsInfo)
		// Give bootstrap connections time to establish before joining
		// topics, so GossipSub sees the peers immediately.
		time.Sleep(3 * time.Second)

		topic, err := psub.Join(pubsubNet)
		common.ReportError(err, "Error while joining pubsub topic")

		netSubs, err := topic.Subscribe()
		common.ReportError(err, "Error while subscribing to pubsub topic")

		go func() {
			for {
				msg, err := netSubs.Next(ctx)
				if err != nil {
					fmt.Println(err)
					break
				}
				host.ConnManager().TagPeer(msg.ReceivedFrom, "keep", 100)
				// Use message author (original publisher), not ReceivedFrom
				// (forwarding peer). In relay topologies GetFrom() is the worker.
				authorID := msg.GetFrom()
				p, gerr := GetPeerFromTable(authorID.String())
				if gerr != nil {
					// Peer not yet in table. Create a minimal entry so it is
					// tracked immediately. The full record (with build attestation)
					// will overwrite this when the CRDT PutHook fires.
					if host.Network().Connectedness(authorID) == network.Connected {
						common.Logger.Debugf("Adding minimal entry for new peer [%s] from gossip", authorID.String())
						p = Peer{ID: authorID.String(), Connected: true, LastSeen: time.Now().Unix()}
						if b, merr := json.Marshal(p); merr == nil {
							UpdateNodeTableHook(ds.NewKey(authorID.String()), b)
						}
					}
					continue
				}
				common.Logger.Debugf("Updating peer: [%s] triggered by msg received", authorID.String())
				p.LastSeen = time.Now().Unix()
				p.Connected = true
				if b, merr := json.Marshal(p); merr == nil {
					UpdateNodeTableHook(ds.NewKey(authorID.String()), b)
				}
			}
		}()

		if !viper.GetBool("scalability.swim_enabled") {
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					default:
						if err := topic.Publish(ctx, []byte("ping")); err != nil {
							common.Logger.Warn("Error while publishing ping: ", err)
						}
						time.Sleep(20 * time.Second)
					}
				}
			}()
		}
		psubCtx, pcancel := context.WithCancel(ctx)
		cancelSubscriptions = pcancel
		pubsubBC, err := crdt.NewPubSubBroadcaster(psubCtx, psub, pubsubTopic)
		common.ReportError(err, "Error while creating pubsub broadcaster")
		opts := crdt.DefaultOptions()
		opts.Logger = common.Logger
		opts.DAGSyncerTimeout = 30 * time.Second // reduced from 5min to prevent cascading blockage
		if viper.GetBool("scalability.crdt_tuned") {
			opts.RebroadcastInterval = viper.GetDuration("crdt.tuned_rebroadcast_interval") // default 60s
			opts.NumWorkers = viper.GetInt("crdt.tuned_workers")                            // default 16
		} else {
			opts.RebroadcastInterval = 5 * time.Second
		}
		opts.PutHook = func(k ds.Key, v []byte) {
			var peer Peer
			err := json.Unmarshal(v, &peer)
			common.ReportError(err, "Error while unmarshalling peer")
			// When a new peer is added to the table it is marked as disconnected by default.
			// Doing so allows to intercept ghost peers by the verification procedure.

			// Do not update itself
			host, _ := GetP2PNode(nil)
			if strings.Trim(k.String(), "/") == host.ID().String() {
				return
			}
			p, err := GetPeerFromTable(strings.Trim(k.String(), "/"))
			if err != nil {
				peer.Connected = false
				common.Logger.Debugf("Adding peer: [%s] triggered by p2p hook", strings.Trim(k.String(), "/"))
			} else {
				peer.Connected = p.Connected
				common.Logger.Debugf("Updating peer: [%s] triggered by p2p hook", strings.Trim(k.String(), "/"))
			}
			value, err := json.Marshal(peer)
			if err == nil {
				UpdateNodeTableHook(k, value)
			} else {
				common.Logger.Error("Error while marshalling peer", err)
			}
		}
		opts.DeleteHook = func(k ds.Key) {
			common.Logger.Debugf("Removed: [%s] triggered by p2p hook", strings.Trim(k.String(), "/"))
			DeleteNodeTableHook(k)
		}

		crdtStore, err = crdt.New(store, ds.NewKey(pubsubKey), ipfs, pubsubBC, opts)
		common.ReportError(err, "Error while creating crdt store")

		StartAutoReconnect(ctx)

		// Workers: reserve relay slots so head nodes can reach us via circuit.
		go func() {
			time.Sleep(10 * time.Second)
			MakeRelayReservations()
		}()

		startTombstoneCompactor(crdtStore)
	})
	return crdtStore, cancelSubscriptions
}

func Reconnect() {
	mode := viper.GetString("mode")
	if ipfs == nil {
		common.Logger.Debug("Reconnect skipped: CRDT/IPFS not initialized yet")
		return
	}
	addsInfo, err := peer.AddrInfosFromP2pAddrs(getDefaultBootstrapPeers(nil, mode)...)
	common.ReportError(err, "Error while getting bootstrap peers")
	ipfs.Bootstrap(addsInfo)
	// Re-establish relay reservations after reconnecting.
	go func() {
		time.Sleep(5 * time.Second)
		MakeRelayReservations()
	}()
}

func ClearCRDTStore() {
	// remove ~/.ocfcore directory
	host, _ := GetP2PNode(nil)
	err := common.RemoveDir(common.GetDBPath(host.ID().String()))
	if err != nil {
		common.Logger.Error("Error while removing directory: ", err)
	}
}
