package protocol

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	mrand "math/rand"
	"net"
	"opentela/internal/common"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ipfs/boxo/ipns"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	dualdht "github.com/libp2p/go-libp2p-kad-dht/dual"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/spf13/viper"
)

var P2PNode *host.Host
var ddht *dualdht.DHT
var hostOnce sync.Once
var autoReconnectOnce sync.Once
var MyID string

const (
	Version = "0.0.0-dev.0"
)

func GetP2PNode(ds datastore.Batching) (host.Host, dualdht.DHT) {
	hostOnce.Do(func() {
		ctx := context.Background()
		var err error
		seed := viper.GetString("seed")
		// try to parse the seed as int64
		seedInt, err := strconv.ParseInt(seed, 10, 64)
		if err != nil {
			common.Logger.Error("Seed is not a valid int64 value: ", seed, " parse error: ", err)
			os.Exit(1)
		}
		host, err := newHost(ctx, seedInt, ds)
		if err != nil {
			common.Logger.Error("Error while creating P2P node: ", err)
			os.Exit(1)
		}
		MyID = host.ID().String()
		P2PNode = &host
	})
	return *P2PNode, *ddht
}

func newHost(ctx context.Context, seed int64, ds datastore.Batching) (host.Host, error) {
	var err error
	if err != nil {
		common.Logger.Error("Error while creating connection manager: ", err)
	}
	var priv crypto.PrivKey
	if seed == 0 {
		// seed=0: always generate a fresh random identity (no persistence)
		common.Logger.Debug("seed=0: generating ephemeral random key")
		priv, _, err = crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
		if err != nil {
			return nil, err
		}
	} else {
		// seed!=0: use persisted key file for stable identity across restarts.
		// On first run, generate from seed and save; subsequent runs load from file.
		priv = loadKeyFromFile()
		if priv == nil {
			common.Logger.Debugf("No existing key file, generating from seed=%d", seed)
			r := mrand.New(mrand.NewSource(seed))
			priv, _, err = crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
			if err != nil {
				return nil, err
			}
			writeKeyToFile(priv)
		}
	}
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256([]byte(Version))
	keyHex := hex.EncodeToString(hash[:])

	var buf bytes.Buffer
	buf.WriteString("/key/swarm/psk/1.0.0/\n")
	buf.WriteString("/base16/\n")
	buf.WriteString(keyHex + "\n")

	// psk, err := pnet.DecodeV1PSK(bytes.NewReader(buf.Bytes()))
	// if err != nil {
	// 	panic(err)
	// }

	opts := []libp2p.Option{
		libp2p.DefaultTransports,
		libp2p.Identity(priv),
		// libp2p.PrivateNetwork(psk),
		libp2p.ResourceManager(newResourceManager()),
		// libp2p.ConnectionManager(connmgr),
		libp2p.NATPortMap(),
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/"+viper.GetString("tcpport"),
			"/ip4/0.0.0.0/tcp/"+viper.GetString("tcpport")+"/ws",
			"/ip4/0.0.0.0/udp/"+viper.GetString("udpport")+"/quic",
		),
		libp2p.Security(libp2ptls.ID, libp2ptls.New),
		libp2p.Security(noise.ID, noise.New),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.EnableAutoNATv2(),
		libp2p.EnableRelayService(),
		libp2p.ForceReachabilityPublic(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			ddht, err = newDHT(ctx, h, ds)
			return ddht, err
		}),
	}

	host, err := libp2p.New(opts...)
	if err != nil {
		return nil, err
	}

	// Log connection events for debugging
	host.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(n network.Network, c network.Conn) {
			// Log the negotiated security protocol for observability.
			secProto := "<unknown>"
			if cs := c.ConnState(); cs.Security != "" {
				secProto = string(cs.Security)
			}
			common.Logger.Debugf("Connected to peer: %s  security=%s  conns=%d", c.RemotePeer(), secProto, len(n.Conns()))
			// On (re)connections, re-announce local services
			go ReannounceLocalServices()

			// Mark peer as connected in node table — only update existing peers.
			// New peers are added via the CRDT PutHook which carries the full
			// record (including build attestation).
			go func(pid peer.ID) {
				if pid == host.ID() {
					return
				}
				p, err := GetPeerFromTable(pid.String())
				if err != nil {
					common.Logger.Debugf("Ignoring connect for unknown peer [%s]; waiting for CRDT sync", pid.String())
					return
				}
				common.Logger.Debugf("Updating peer [%s] on connect", pid.String())
				p.Connected = true
				p.LastSeen = time.Now().Unix()
				if b, e := json.Marshal(p); e == nil {
					UpdateNodeTableHook(datastore.NewKey(pid.String()), b)
				} else {
					common.Logger.Error("Failed to marshal peer on connect: ", e)
				}
			}(c.RemotePeer())
		},
		DisconnectedF: func(n network.Network, c network.Conn) {
			common.Logger.Debugf("Disconnected from peer: %s  conns=%d", c.RemotePeer(), len(n.Conns()))
			// Mark peer as disconnected — only update existing peers.
			go func(pid peer.ID) {
				if pid == host.ID() {
					return
				}
				p, err := GetPeerFromTable(pid.String())
				if err != nil {
					common.Logger.Debugf("Ignoring disconnect for unknown peer [%s]", pid.String())
					return
				}
				p.Connected = false
				common.Logger.Debugf("Marking peer [%s] disconnected", pid.String())
				// keep LastSeen as last known good; do not bump here
				if b, e := json.Marshal(p); e == nil {
					UpdateNodeTableHook(datastore.NewKey(pid.String()), b)
				} else {
					common.Logger.Error("Failed to marshal peer on disconnect: ", e)
				}
			}(c.RemotePeer())
		},
	})

	// NOTE: auto-reconnect is started later by StartAutoReconnect() after
	// bitswap/CRDT are initialized, so that connection notifications reach
	// bitswap's peer manager.

	return host, nil
}

// StartAutoReconnect launches the background auto-reconnector. Must be called
// AFTER bitswap/CRDT are initialized so that connection events reach bitswap.
// The provided context controls the lifetime of the auto-reconnect loop.
func StartAutoReconnect(ctx context.Context) {
	autoReconnectOnce.Do(func() {
		host, _ := GetP2PNode(nil)
		go startAutoReconnect(ctx, host)
	})
}

// startAutoReconnect periodically checks if we lost connectivity and attempts to reconnect to bootstraps with backoff.
func startAutoReconnect(ctx context.Context, h host.Host) {
	const (
		healthCheckInterval = 30 * time.Second
		minBackoff          = 5 * time.Second
		maxBackoff          = 2 * time.Minute
		dialTimeout         = 10 * time.Second
	)

	attempt := 0

	for {
		if ctx.Err() != nil {
			return
		}

		if len(h.Network().Conns()) == 0 {
			attempt++
			if attempt == 1 {
				common.Logger.Debug("No active P2P connections; attempting reconnect to bootstraps")
			} else {
				backoff := backoffDelay(attempt-1, minBackoff, maxBackoff)
				common.Logger.With("attempt", attempt).Debugf("Reconnect will retry after %s", backoff)
				if !waitFor(ctx, backoff) {
					return
				}
			}

			if tryReconnectToBootstraps(ctx, h, dialTimeout) {
				if attempt > 1 {
					common.Logger.Debugf("P2P connectivity restored after %d attempts", attempt)
				}
				attempt = 0
				if !waitFor(ctx, healthCheckInterval) {
					return
				}
				continue
			}

			// Failed attempt; loop and escalate backoff
			continue
		}

		if attempt > 0 {
			common.Logger.Debug("P2P connectivity restored")
			attempt = 0
		}

		if !waitFor(ctx, healthCheckInterval) {
			return
		}
	}
}

func tryReconnectToBootstraps(ctx context.Context, h host.Host, dialTimeout time.Duration) bool {
	mode := viper.GetString("mode")
	addrs := getDefaultBootstrapPeers(nil, mode)
	if len(addrs) == 0 {
		common.Logger.Debug("Reconnect attempt skipped: no bootstrap addresses configured")
		return false
	}

	peerInfos, err := peer.AddrInfosFromP2pAddrs(addrs...)
	if err != nil {
		common.Logger.Error("Failed to parse bootstrap peers during reconnect: ", err)
		return false
	}

	successes := 0
	for _, info := range peerInfos {
		if info.ID == h.ID() {
			continue
		}

		if h.Network().Connectedness(info.ID) == network.Connected {
			successes++
			continue
		}

		if len(info.Addrs) == 0 {
			common.Logger.With("peer", info.ID).Warn("Bootstrap peer has no address; skipping")
			continue
		}

		connectCtx, cancel := context.WithTimeout(ctx, dialTimeout)
		err := h.Connect(connectCtx, info)
		cancel()

		if err != nil {
			if isTransientNetworkError(err) {
				common.Logger.With("peer", info.ID).Debugf("Transient error connecting to bootstrap: %v", err)
			} else {
				common.Logger.With("peer", info.ID).Debugf("Failed to connect to bootstrap: %v", err)
			}
			continue
		}

		common.Logger.Debugf("Connected to bootstrap peer %s", info.ID)
		successes++
	}

	if successes > 0 {
		go Reconnect()
		return true
	}

	common.Logger.Warn("Reconnect attempt failed; no bootstrap peers reachable")
	return false
}

func waitFor(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func backoffDelay(attempt int, minDelay, maxDelay time.Duration) time.Duration {
	base := backoffBaseDelay(attempt, minDelay, maxDelay)
	if base <= 0 {
		return minDelay
	}

	jitterMax := base / 3
	if jitterMax <= 0 {
		return base
	}

	randSrc := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	jitter := time.Duration(randSrc.Int63n(int64(jitterMax)))
	return base + jitter
}

func backoffBaseDelay(attempt int, minDelay, maxDelay time.Duration) time.Duration {
	if attempt <= 1 {
		return minDelay
	}

	delay := minDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}

	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	return false
}

func newResourceManager() network.ResourceManager {
	limiter := rcmgr.NewFixedLimiter(rcmgr.DefaultLimits.AutoScale())
	rm, err := rcmgr.NewResourceManager(limiter)
	if err != nil {
		common.Logger.Errorf("Failed to create resource manager, falling back to null (NO RESOURCE LIMITS): %v", err)
		return &network.NullResourceManager{}
	}
	return rm
}

func newDHT(ctx context.Context, h host.Host, ds datastore.Batching) (*dualdht.DHT, error) {
	dhtOpts := []dualdht.Option{
		dualdht.DHTOption(dht.NamespacedValidator("pk", record.PublicKeyValidator{})),
		dualdht.DHTOption(dht.NamespacedValidator("ipns", ipns.Validator{KeyBook: h.Peerstore()})),
		dualdht.DHTOption(dht.Concurrency(512)),
		dualdht.DHTOption(dht.Mode(dht.ModeAuto)),
	}
	if ds != nil {
		dhtOpts = append(dhtOpts, dualdht.DHTOption(dht.Datastore(ds)))
	}
	return dualdht.New(ctx, h, dhtOpts...)
}

// GetConnectedPeers returns the list of connected peers
func ConnectedPeers() []*peer.AddrInfo {
	var pinfos = []*peer.AddrInfo{}
	host, _ := GetP2PNode(nil)
	for _, p := range host.Peerstore().Peers() {
		// check if the peer is connected
		if host.Network().Connectedness(p) == network.Connected {
			pinfos = append(pinfos, &peer.AddrInfo{
				ID:    p,
				Addrs: host.Peerstore().Addrs(p),
			})
		}
	}
	return pinfos
}

func AllPeers() []*PeerWithStatus {
	var pinfos = []*PeerWithStatus{}
	host, _ := GetP2PNode(nil)
	for _, p := range host.Peerstore().Peers() {
		pinfos = append(pinfos, &PeerWithStatus{
			ID:            p.String(),
			Connectedness: host.Network().Connectedness(p).String(),
		})
	}
	return pinfos
}

func ConnectedBootstraps() []string {
	var bootstraps = []string{}
	dnt := GetAllPeers()
	host, _ := GetP2PNode(nil)
	for _, p := range *dnt {
		if p.PublicAddress != "" {
			common.Logger.Debugf("Peer %s addr=%s connectedness=%s", p.ID, p.PublicAddress, host.Network().Connectedness(peer.ID(p.ID)))
			if host.Network().Connectedness(peer.ID(p.ID)) == network.Connected || host.ID().String() == p.ID {
				bootstrapAddr := "/ip4/" + p.PublicAddress + "/tcp/" + viper.GetString("tcpport") + "/p2p/" + p.ID
				bootstraps = append(bootstraps, bootstrapAddr)
			}
		}
	}
	bootstraps = common.DeduplicateStrings(bootstraps)
	return bootstraps
}

// GetResourceManagerStats returns current resource usage statistics
func GetResourceManagerStats() {
	host, _ := GetP2PNode(nil)
	if rm := host.Network().ResourceManager(); rm != nil {
		// Try to get stats if available
		if statsGetter, ok := rm.(interface {
			Stat() rcmgr.ResourceManagerStat
		}); ok {
			stats := statsGetter.Stat()
			common.Logger.Debugf("Resource Manager: conns=%d (in:%d out:%d) streams=%d (in:%d out:%d) mem=%d",
				stats.System.NumConnsInbound+stats.System.NumConnsOutbound,
				stats.System.NumConnsInbound,
				stats.System.NumConnsOutbound,
				stats.System.NumStreamsInbound+stats.System.NumStreamsOutbound,
				stats.System.NumStreamsInbound,
				stats.System.NumStreamsOutbound,
				stats.System.Memory,
			)
		} else {
			common.Logger.Debug("Resource Manager present but stats not available")
		}
	} else {
		common.Logger.Debug("No Resource Manager configured")
	}
}
