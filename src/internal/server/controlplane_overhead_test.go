//go:build bandwidthbench

// Control-plane bandwidth measurement.
//
// This measures the *background* network traffic an OpenTela node generates
// just to stay in the mesh — gossip (GossipSub), CRDT head rebroadcast, the
// 20 s pubsub ping, ping protocol, DHT and identify — and, in the churn test,
// the cost of CRDT writes (peers joining / re-registering services).
//
// It boots the REAL stack used by internal/protocol/crdt.go on N connected
// nodes in-process:
//   - ipfslite.SetupLibp2p  -> libp2p host + dual DHT (with a BandwidthReporter)
//   - ipfslite.New          -> the DAG syncer (bitswap)
//   - pubsub.NewGossipSub    (DefaultGossipSubParams: D=6, Dlo=4, Dhi=12)
//   - join "ocf-crdt-net" + a 20 s "ping" publish loop   (crdt.go:67-121)
//   - go-ds-crdt with RebroadcastInterval = 5 s          (crdt.go:124-167)
//
// Each node has its own metrics.BandwidthCounter, so we get a per-protocol
// byte breakdown.
//
// Tests:
//
//	TestControlPlaneBandwidth   — idle, ALL-PAIRS topology, N=2,4,8 (dense upper bound)
//	TestControlPlaneScaling     — idle, SPARSE bootstrap-style topology, N=10,25,50
//	TestControlPlaneActiveChurn — CRDT writes vs idle at fixed N
//
// Run with:
//
//	cd src && go test -tags bandwidthbench -run TestControlPlane -v -timeout 1200s ./internal/server/
//
// Build-tagged so it never runs in `make test` / CI.
package server

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"opentela/internal/common"
	"opentela/internal/protocol"
	crdt "opentela/internal/protocol/go-ds-crdt"

	ipfslite "github.com/hsanjuan/ipfs-lite"
	ds "github.com/ipfs/go-datastore"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/metrics"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	protocolID "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/multiformats/go-multiaddr"

	libp2p "github.com/libp2p/go-libp2p"
)

// cpNode bundles one fully-wired control-plane node.
type cpNode struct {
	host  host.Host
	bwc   *metrics.BandwidthCounter
	store *crdt.Datastore
}

// cpOpts selects which control-plane configuration to exercise. The zero value
// is NOT valid — use defaultCPOpts() and override fields.
type cpOpts struct {
	rebroadcast time.Duration // CRDT rebroadcast interval (5s default, 60s tuned)
	gD, gDlo    int           // GossipSub D / Dlo (0 => library defaults 6/4)
	gDhi        int           // GossipSub Dhi (0 => library default 12)
	pubsubPing  bool          // run the 20s pubsub ping (true unless SWIM is on)
	enableSWIM  bool          // run the SWIM membership protocol (started post-connect)
}

// defaultCPOpts mirrors production defaults: 5s rebroadcast, default GossipSub
// params, 20s pubsub ping, no SWIM.
func defaultCPOpts() cpOpts {
	return cpOpts{rebroadcast: 5 * time.Second, pubsubPing: true}
}

// buildCPNode stands up one node with the same control-plane stack as crdt.go.
func buildCPNode(t *testing.T, ctx context.Context, opt cpOpts) *cpNode {
	t.Helper()

	priv, _, err := crypto.GenerateEd25519Key(crand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	listen, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/0")
	store := ipfslite.NewInMemoryDatastore()
	bwc := metrics.NewBandwidthCounter()

	h, dht, err := ipfslite.SetupLibp2p(ctx, priv, nil, []multiaddr.Multiaddr{listen}, store,
		libp2p.BandwidthReporter(bwc))
	if err != nil {
		t.Fatalf("SetupLibp2p: %v", err)
	}

	ipfs, err := ipfslite.New(ctx, store, nil, h, dht, nil)
	if err != nil {
		t.Fatalf("ipfslite.New: %v", err)
	}
	ping.NewPingService(h) // /ipfs/ping/1.0.0, as host.go registers

	params := pubsub.DefaultGossipSubParams() // D=6,Dlo=4,Dhi=12
	if opt.gD > 0 {
		params.D, params.Dlo, params.Dhi = opt.gD, opt.gDlo, opt.gDhi
	}
	psub, err := pubsub.NewGossipSub(ctx, h, pubsub.WithGossipSubParams(params))
	if err != nil {
		t.Fatalf("NewGossipSub: %v", err)
	}

	// Join the membership topic and drain it, then run the 20 s ping publish
	// loop — exactly crdt.go:67-121.
	netTopic, err := psub.Join("ocf-crdt-net")
	if err != nil {
		t.Fatalf("join net topic: %v", err)
	}
	netSub, err := netTopic.Subscribe()
	if err != nil {
		t.Fatalf("subscribe net topic: %v", err)
	}
	go func() {
		for {
			if _, err := netSub.Next(ctx); err != nil {
				return
			}
		}
	}()
	// The 20s pubsub ping runs only when SWIM is disabled (crdt.go:107).
	if opt.pubsubPing {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_ = netTopic.Publish(ctx, []byte("ping"))
					time.Sleep(20 * time.Second)
				}
			}
		}()
	}

	// CRDT datastore with the configured rebroadcast interval.
	bcast, err := crdt.NewPubSubBroadcaster(ctx, psub, "ocf-crdt")
	if err != nil {
		t.Fatalf("NewPubSubBroadcaster: %v", err)
	}
	opts := crdt.DefaultOptions()
	opts.Logger = common.Logger
	opts.RebroadcastInterval = opt.rebroadcast
	opts.DAGSyncerTimeout = 30 * time.Second
	cstore, err := crdt.New(store, ds.NewKey("ocf-crdt"), ipfs, bcast, opts)
	if err != nil {
		t.Fatalf("crdt.New: %v", err)
	}

	return &cpNode{host: h, bwc: bwc, store: cstore}
}

func addrInfo(n *cpNode) libp2ppeer.AddrInfo {
	return libp2ppeer.AddrInfo{ID: n.host.ID(), Addrs: n.host.Addrs()}
}

// connectAllPairs wires every node to every other — a fully connected graph.
// At small N (< the gossip fanout) this makes the GossipSub overlay nearly
// complete, so it OVER-states gossip traffic. Use it only as a dense upper bound.
func connectAllPairs(t *testing.T, ctx context.Context, nodes []*cpNode) {
	t.Helper()
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			if err := nodes[i].host.Connect(ctx, addrInfo(nodes[j])); err != nil {
				t.Fatalf("connect %d->%d: %v", i, j, err)
			}
		}
	}
}

// connectSparse models a realistic bootstrap+discovery result: each new node
// connects to k random already-joined nodes (>=1 guarantees a connected graph).
// Average libp2p degree stays ~2k regardless of N, so GossipSub's own D=6
// mesh bound — not the connection graph — governs gossip fan-out, the same
// regime a production mesh runs in.
func connectSparse(t *testing.T, ctx context.Context, nodes []*cpNode, k int, seed int64) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	for i := 1; i < len(nodes); i++ {
		m := k
		if i < m {
			m = i
		}
		picks := map[int]bool{}
		for len(picks) < m {
			picks[rng.Intn(i)] = true
		}
		for j := range picks {
			if err := nodes[i].host.Connect(ctx, addrInfo(nodes[j])); err != nil {
				t.Fatalf("connect %d->%d: %v", i, j, err)
			}
		}
	}
}

type cpSample struct {
	total metrics.Stats
	proto map[protocolID.ID]metrics.Stats
}

func snapshotNodes(nodes []*cpNode) []cpSample {
	out := make([]cpSample, len(nodes))
	for i, nd := range nodes {
		out[i] = cpSample{total: nd.bwc.GetBandwidthTotals(), proto: nd.bwc.GetBandwidthByProtocol()}
	}
	return out
}

// measureWindow idles for `window`, then reports per-node byte rate and the
// per-protocol breakdown (whole mesh). Returns the per-node B/s.
func measureWindow(t *testing.T, nodes []*cpNode, window time.Duration, label string) float64 {
	t.Helper()
	n := len(nodes)
	start := snapshotNodes(nodes)
	time.Sleep(window)
	end := snapshotNodes(nodes)
	secs := window.Seconds()

	var sumBytes float64
	protoDelta := map[protocolID.ID]float64{}
	for i := 0; i < n; i++ {
		sumBytes += float64((end[i].total.TotalIn - start[i].total.TotalIn) +
			(end[i].total.TotalOut - start[i].total.TotalOut))
		for pid, st := range end[i].proto {
			s0 := start[i].proto[pid]
			protoDelta[pid] += float64((st.TotalIn - s0.TotalIn) + (st.TotalOut - s0.TotalOut))
		}
	}
	perNode := sumBytes / float64(n) / secs

	t.Logf("")
	t.Logf("[%s] N=%d, %.0fs window  —  per-node %.0f B/s (in+out, app-level)", label, n, secs, perNode)
	type kv struct {
		id    protocolID.ID
		bytes float64
	}
	var ranked []kv
	for pid, b := range protoDelta {
		ranked = append(ranked, kv{pid, b})
	}
	sort.Slice(ranked, func(a, b int) bool { return ranked[a].bytes > ranked[b].bytes })
	for _, r := range ranked {
		rate := r.bytes / secs
		if rate < 1 {
			continue
		}
		name := string(r.id)
		if name == "" {
			name = "(connection/identify/unattributed)"
		}
		t.Logf("    %-34s %9.1f B/s", name, rate)
	}
	return perNode
}

func buildNodes(t *testing.T, ctx context.Context, n int, opt cpOpts) []*cpNode {
	nodes := make([]*cpNode, n)
	for i := range nodes {
		nodes[i] = buildCPNode(t, ctx, opt)
	}
	return nodes
}

// closeNodes shuts down all hosts. Called explicitly (not via t.Cleanup) so a
// test that runs several configs in sequence frees each batch before the next.
func closeNodes(nodes []*cpNode) {
	for _, nd := range nodes {
		_ = nd.host.Close()
	}
}

// TestControlPlaneBandwidth — idle, ALL-PAIRS (dense upper bound).
func TestControlPlaneBandwidth(t *testing.T) {
	for _, n := range []int{2, 4, 8} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			nodes := buildNodes(t, ctx, n, defaultCPOpts())
			defer closeNodes(nodes)
			connectAllPairs(t, ctx, nodes)
			time.Sleep(12 * time.Second) // warmup
			measureWindow(t, nodes, 40*time.Second, "idle/all-pairs")
		})
	}
}

// TestControlPlaneScaling — idle, SPARSE bootstrap-style topology. Measures how
// per-node gossip grows with N. (It does NOT flatten: every node is a periodic
// publisher, so the distinct-message count is O(N) and per-node traffic is ~O(N).)
func TestControlPlaneScaling(t *testing.T) {
	for _, n := range []int{10, 25, 50} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			nodes := buildNodes(t, ctx, n, defaultCPOpts())
			defer closeNodes(nodes)
			connectSparse(t, ctx, nodes, 8, 1) // ~bounded degree, fixed seed
			time.Sleep(25 * time.Second)       // warmup: let overlay + DHT settle
			measureWindow(t, nodes, 40*time.Second, "idle/sparse")
		})
	}
}

// buildPeerRecord constructs a realistic protocol.Peer record (the thing a node
// writes into the CRDT when it joins / re-registers a service).
func buildPeerRecord(id string, seq int64) []byte {
	p := protocol.Peer{
		ID:                id,
		Owner:             "7vfCXTUXx5WJV5JADk17DUJ4ksgau7utNKj4b963voxs",
		ProviderID:        "otela-7vfCXTUXx5WJV5JA",
		Role:              []string{"worker"},
		Status:            "online",
		CurrentOffering:   []string{"llm"},
		AvailableOffering: []string{"llm"},
		Service: []protocol.Service{{
			Name:          "llm",
			Status:        "online",
			Host:          "127.0.0.1",
			Port:          "8080",
			IdentityGroup: []string{"model=Qwen/Qwen3-8B", "model=*", "all"},
		}},
		LastSeen:      seq, // changes each write so a new DAG node is produced
		Version:       "0.0.0-dev.0",
		PublicAddress: "203.0.113.7",
		PublicPort:    "43905",
		Connected:     true,
		Load:          []int{12, 34, 56},
	}
	b, _ := json.Marshal(p)
	return b
}

// TestControlPlaneActiveChurn measures the bandwidth cost of CRDT writes
// (peers joining / re-registering) on top of the idle baseline, at fixed N.
func TestControlPlaneActiveChurn(t *testing.T) {
	const n = 10
	const churnInterval = 3 * time.Second // each node re-registers this often
	const window = 30 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodes := buildNodes(t, ctx, n, defaultCPOpts())
	defer closeNodes(nodes)
	connectSparse(t, ctx, nodes, 8, 1)
	time.Sleep(20 * time.Second) // warmup

	sample := buildPeerRecord(nodes[0].host.ID().String(), 0)
	t.Logf("Peer record size: %d B; churn = each of %d nodes writes every %s",
		len(sample), n, churnInterval)

	// Idle baseline.
	idleRate := measureWindow(t, nodes, window, "idle/sparse")

	// Start churn: every node periodically writes its (updated) Peer record.
	churnCtx, churnCancel := context.WithCancel(ctx)
	for _, nd := range nodes {
		go func(nd *cpNode) {
			key := ds.NewKey(nd.host.ID().String())
			var seq int64
			for {
				select {
				case <-churnCtx.Done():
					return
				default:
					_ = nd.store.Put(churnCtx, key, buildPeerRecord(nd.host.ID().String(), seq))
					seq++
					time.Sleep(churnInterval)
				}
			}
		}(nd)
	}
	time.Sleep(3 * time.Second) // let writes start propagating

	churnRate := measureWindow(t, nodes, window, "churn/sparse")
	churnCancel()

	writesPerSec := float64(n) / churnInterval.Seconds()
	deltaMeshPerSec := (churnRate - idleRate) * float64(n) // whole-mesh extra B/s
	t.Logf("")
	t.Logf("CHURN SUMMARY (N=%d):", n)
	t.Logf("  idle  per-node: %.0f B/s", idleRate)
	t.Logf("  churn per-node: %.0f B/s   (%.1fx idle)", churnRate, churnRate/idleRate)
	t.Logf("  write rate: %.2f writes/s (mesh-wide)", writesPerSec)
	if writesPerSec > 0 {
		t.Logf("  => ~%.0f B of mesh traffic per CRDT write (%d-node mesh)",
			deltaMeshPerSec/writesPerSec, n)
	}
}
