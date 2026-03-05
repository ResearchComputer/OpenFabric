# OpenTela Scalability Review

This document analyses the current codebase for scalability bottlenecks and proposes concrete solutions for operating OpenTela at the scale of thousands to hundreds of thousands of machines.

---

## Table of Contents

1. [Architecture Summary](#architecture-summary)
2. [Identified Bottlenecks](#identified-bottlenecks)
3. [Solutions](#solutions)
4. [Scaling Roadmap by Target Size](#scaling-roadmap-by-target-size)
5. [Target Architecture at 100k Scale](#target-architecture-at-100k-scale)

---

## Architecture Summary

OpenTela is a flat, fully decentralised P2P fabric where every node runs the same binary. The key layers are:

| Layer | Technology | Role |
|---|---|---|
| P2P Networking | libp2p + Kademlia DHT | Peer discovery, transport, hole-punching |
| State Propagation | GossipSub PubSub | Broadcasts CRDT delta updates to all peers |
| Shared State | CRDT (go-ds-crdt over BadgerDB) | Distributed Node Table (DNT) — who is alive, what they serve |
| In-Memory View | `NodeTable` (`map[string]Peer`) | Hot cache for routing lookups |
| Routing / Proxy | Gin HTTP + `httputil.ReverseProxy` | Forwards user requests to worker nodes via libp2p transport |

The design is elegantly simple for small-to-medium clusters (tens to hundreds of nodes). However, several architectural and implementation choices create hard ceilings at larger scales. The bottlenecks and their corresponding solutions are described below.

---

## Identified Bottlenecks

### 🔴 Bottleneck 1 — The In-Memory Node Table is a Single-Threaded Serialisation Point

**File:** `src/internal/protocol/node_table.go`

Every operation on the node table — reads, writes, deletes, and full iterations — blocks behind a capacity-1 channel semaphore:

```go
var tableUpdateSem = make(chan struct{}, 1) // capacity 1 → max 1 goroutine at a time
```

This is effectively a global mutex. Critically, functions such as `GetAllProviders()`, `GetAllPeers()`, and `GetConnectedPeers()` hold this lock while iterating the **entire** map:

```go
func GetAllProviders(serviceName string) ([]Peer, error) {
    table := *getNodeTable()
    tableUpdateSem <- struct{}{}
    defer func() { <-tableUpdateSem }()
    for _, peer := range table {   // full scan while holding the global lock
        ...
    }
}
```

`GetAllProviders` is called on **every incoming user request**. At 100,000 nodes, this map holds 100k entries, and every routing call must lock the entire structure, iterate it completely, then release. Every concurrent request queues behind it, turning the routing layer into a single-threaded pipeline regardless of how many CPUs are available.

---

### 🔴 Bottleneck 2 — GossipSub Fanout is Configured for a Tiny Network

**File:** `src/internal/protocol/crdt.go`

```go
pubsubParams := pubsub.DefaultGossipSubParams()
pubsubParams.D   = 128
pubsubParams.Dlo = 16
pubsubParams.Dhi = 256
```

`D` is the gossip degree — the number of peers a node immediately forwards each message to. The GossipSub default recommended by the libp2p team is `D=6`. Setting it to `128` means every single CRDT update is immediately fanned out to 128 neighbours. Each of those neighbours processes the message and may forward it further. For a 100,000-node network this creates a self-amplifying message storm.

Compounding this, two background loops emit traffic even when nothing has changed:

- A **20-second ping loop** that publishes `"ping"` to the network topic indefinitely.
- A **5-second CRDT rebroadcast interval** that re-announces all current DAG heads.

Together these generate a constant baseline of gossip traffic that multiplies with the fanout factor as the network grows.

---

### 🔴 Bottleneck 3 — The Health-Check Ticker is O(N) Per Node Every 30 Seconds

**File:** `src/internal/protocol/clock.go`

Every 30 seconds, each node attempts to dial **every single peer it has ever known** in its libp2p peerstore:

```go
gocron.Every(30).Second().Do(func() {
    peers := host.Peerstore().Peers()
    for _, peer_id := range peers {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        cancel()
        host.Connect(ctx, addrInfo)
    }
})
```

The problem here:

1. **O(N) dial attempts.** The libp2p peerstore has no eviction by default, so it grows unboundedly over the lifetime of the node. At 100k known peers, this is 100,000 dial attempts every 30 seconds per node — the majority of which connect to peers that are perfectly healthy and need no intervention.

---

### 🔴 Bottleneck 4 — No Connection Limits: the NullResourceManager

**File:** `src/internal/protocol/host.go`

```go
libp2p.ResourceManager(&network.NullResourceManager{}),
// libp2p.ConnectionManager(connmgr),   ← commented out
```

The `NullResourceManager` imposes **zero limits** on connections, streams, memory, or file descriptors. As the network grows, a node accumulates an ever-increasing number of open connections. Linux systems typically default to 1,024 open file descriptors per process (maximum ~65,535). In a 100k-node network, a popular bootstrap or head node would hit OS file descriptor limits long before any application-level logic notices, causing silent failures.

---

### 🟡 Bottleneck 5 — Load Balancing Ignores All Available Load Data

**File:** `src/internal/server/proxy_handler.go`

```go
// randomly select one of the candidates
randomIndex := rand.Intn(len(candidates))
```

The `Peer` struct already carries real-time hardware utilisation:

```go
type Peer struct {
    Load     []int               `json:"load"`
    Hardware common.HardwareSpec `json:"hardware"` // GPUs[].TotalMemory, GPUs[].UsedMemory
    ...
}
```

This data is actively collected (`platform.GetGPUInfo()`) and published back into the CRDT on every re-announcement. However the routing layer ignores it entirely, using uniform random selection. A node with 100% GPU memory utilisation is exactly as likely to receive the next request as an idle node. At scale this leads to hot-spots, request queuing on saturated nodes, and underutilisation of idle capacity.

---

### 🟡 Bottleneck 6 — Flat Network Topology with No Hierarchical Structure

There is no concept of clusters, regions, or locality. Every node is an equal peer in a single global P2P network. At 100k nodes this creates three problems:

- **Geography-blind routing.** A request originating in Europe may be forwarded to a GPU node in Asia because `rand.Intn()` has no awareness of proximity. A nearby idle node may be skipped entirely.
- **Bootstrap congestion.** There is one shared bootstrap list. Every new node joining the network contacts the same small set of bootstrap addresses. At 100k nodes cycling through Slurm preemptions, these nodes become a critical bottleneck for join traffic.
- **Unbounded CRDT dataset.** Every node must store and process the full global CRDT state — 100k entries, each updated periodically. There is no partitioning.

---

### 🟡 Bottleneck 7 — CRDT DAG Heads Accumulate Under High Churn

**File:** `src/internal/protocol/go-ds-crdt/crdt.go`

In a high-churn Slurm environment where nodes join and leave every few minutes, every join, leave, service update, and heartbeat is an independent concurrent CRDT write. This generates a large number of concurrent DAG heads. The CRDT converges by merging them, but the defaults limit this pipeline:

```go
NumWorkers:          5,
MultiHeadProcessing: false,
```

With only 5 merge workers and sequential head processing disabled, the merge pipeline can fall behind the rate of incoming updates at scale. This delays the convergence of the distributed state, causing stale routing decisions where a freshly-joined node is not yet visible to the router, or a departed node continues to receive traffic.

---

### 🟡 Bottleneck 8 — A New `http.Client` is Allocated on Every `RemoteGET` Call

**File:** `src/internal/common/requests.go`

```go
func RemoteGET(url string) ([]byte, error) {
    client := &http.Client{Timeout: 5 * time.Second}  // new client every call
    res, err := client.Do(req)
```

A new `http.Client` is constructed on every invocation. Each client allocates its own transport, which means no TCP connection pooling between calls. This function is invoked during bootstrap source fetching and LLM model discovery. At high call rates this wastes ephemeral port allocations and prevents the kernel-level connection reuse that `http.DefaultTransport` provides for free.

---

### 🟡 Bottleneck 9 — RSA-2048 Key Generation is Slow During Mass Restarts

**File:** `src/internal/protocol/host.go`

```go
priv, _, err = crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
```

RSA-2048 key generation takes approximately 100–500 ms per key. When a Slurm preemption wave causes hundreds of nodes to restart simultaneously, every one of them blocks on key generation before it can rejoin the network. This creates a coordinated startup delay across the cluster precisely when fast recovery is most important. Ed25519 key generation completes in microseconds and provides equivalent security for this use case.

---

### 🟡 Bottleneck 10 — `healthCheckRemote` Can Retry Silently for Up to 16 Hours

**File:** `src/internal/protocol/registrar.go`

```go
func healthCheckRemote(port string, maxTries int) error {
    for err != nil {
        time.Sleep(10 * time.Second)
        tries++
        if tries > maxTries { // maxTries = 6000 → 60,000 seconds ≈ 16.7 hours
            return err
        }
    }
}
```

When called with `maxTries=6000`, this function can block the service registration goroutine for up to ~16.7 hours, silently consuming Slurm wall-time without contributing to the network. Typical Slurm job time limits are 24–48 hours, so this can consume the majority of a job's allocation before any error is surfaced.

---

## Solutions

### Solution 1 — Replace the Semaphore with a Sharded `sync.RWMutex` Map

**Addresses:** Bottleneck 1

Replace the capacity-1 channel with a sharded map structure that allows fully concurrent reads across independent buckets. Writes to different buckets never block each other, and reads never block other reads.

```go
const numShards = 256

type tableShard struct {
    sync.RWMutex
    data map[string]Peer
}

type ShardedNodeTable struct {
    shards [numShards]*tableShard
}

func (t *ShardedNodeTable) getShard(key string) *tableShard {
    h := fnv.New32a()
    h.Write([]byte(key))
    return t.shards[h.Sum32()%numShards]
}

// Concurrent read — multiple goroutines across different shards run in parallel.
func (t *ShardedNodeTable) GetPeer(id string) (Peer, bool) {
    s := t.getShard(id)
    s.RLock()
    defer s.RUnlock()
    p, ok := s.data[id]
    return p, ok
}

// GetAllProviders holds one shard's read lock at a time, not a global one.
func (t *ShardedNodeTable) GetAllProviders(service string) []Peer {
    var result []Peer
    for _, s := range t.shards {
        s.RLock()
        for _, p := range s.data {
            if p.Connected {
                for _, svc := range p.Service {
                    if svc.Name == service {
                        result = append(result, p)
                        break
                    }
                }
            }
        }
        s.RUnlock()
    }
    return result
}
```

This transforms the routing hot-path from fully serialised to concurrent. At 100k nodes, reads across different shards run fully in parallel, completely eliminating the single-threaded bottleneck on every incoming user request.

---

### Solution 2 — Materialised Service Index with Incremental Updates

**Addresses:** Bottleneck 1, Bottleneck 5 (routing latency)

Even with sharding, `GetAllProviders` still scans all shards. The correct fix for the routing hot-path is to maintain a **pre-computed inverted index** — a map from `serviceName → candidates` — that is updated incrementally inside the existing `PutHook` and `DeleteHook` CRDT callbacks. Routing lookups then become O(1) regardless of the total number of nodes.

```go
type ServiceIndex struct {
    mu     sync.RWMutex
    byName map[string]map[string]Peer // service name → peer ID → Peer
}

var globalServiceIndex = &ServiceIndex{
    byName: make(map[string]map[string]Peer),
}

// Called from UpdateNodeTableHook on every CRDT update.
func (idx *ServiceIndex) OnPeerUpdate(p Peer) {
    idx.mu.Lock()
    defer idx.mu.Unlock()
    // Evict stale entries for this peer across all service buckets.
    for _, bucket := range idx.byName {
        delete(bucket, p.ID)
    }
    // Re-insert into the correct buckets if the peer is live.
    if p.Connected {
        for _, svc := range p.Service {
            if idx.byName[svc.Name] == nil {
                idx.byName[svc.Name] = make(map[string]Peer)
            }
            idx.byName[svc.Name][p.ID] = p
        }
    }
}

// O(1) lookup — a single RLock and a map dereference.
func (idx *ServiceIndex) GetProviders(service string) []Peer {
    idx.mu.RLock()
    defer idx.mu.RUnlock()
    bucket := idx.byName[service]
    result := make([]Peer, 0, len(bucket))
    for _, p := range bucket {
        result = append(result, p)
    }
    return result
}
```

The index is updated lazily on CRDT events, so the routing path itself does zero iteration. Write contention on the index is low because CRDT updates arrive far less frequently than routing requests.

---

### Solution 3 — Adaptive GossipSub Parameters and Topic Sharding

**Addresses:** Bottleneck 2, Bottleneck 6

**3a — Tune GossipSub parameters for large networks:**

The GossipSub protocol achieves high message reliability with `D=6`. Values above 12–15 offer negligible reliability improvement while multiplying bandwidth consumption. The current values of 128/256 are counterproductive at any scale above a few dozen nodes.

```go
pubsubParams := pubsub.DefaultGossipSubParams()
pubsubParams.D                 = 8    // gossip degree (default 6)
pubsubParams.Dlo               = 4    // minimum degree before adding peers
pubsubParams.Dhi               = 12   // maximum degree before pruning peers
pubsubParams.HeartbeatInterval = 700 * time.Millisecond
// Peer scoring prevents misbehaving nodes from amplifying traffic.
pubsubParams.GossipThreshold   = -4000
pubsubParams.PublishThreshold  = -8000
pubsubParams.GraylistThreshold = -16000
```

**3b — Shard GossipSub topics by cluster:**

Instead of one global topic (`"ocf-crdt-net"`) for all 100k nodes, namespace topics by cluster ID. Each cluster manages its own convergent CRDT in isolation. A small set of aggregator nodes bridge between cluster topics by subscribing to all of them and re-publishing coarsened summaries at a low frequency.

```go
// Worker nodes subscribe only to their own cluster's topic.
clusterTopic := "ocf-crdt-" + viper.GetString("cluster.id")

// Aggregator nodes additionally subscribe to the global topic
// and re-publish cluster-level summaries (not full peer records).
globalTopic := "ocf-crdt-global"
```

This reduces per-node gossip traffic from O(global_N × D) to O(cluster_N × D), which is bounded by cluster size rather than total fleet size.

---

### Solution 4 — Fix the O(N) Health-Check Ticker and the `defer` Bug

**Addresses:** Bottleneck 3

Replace the full peerstore scan with a **sampled health check** that actively dials only a small random subset of known peers per cycle. Peer liveness for the rest of the network is inferred from received GossipSub messages and DHT routing table activity — both of which already update `LastSeen` in the node table.

Also fix the `defer cancel()` bug by moving cancellation to the end of each loop iteration instead of deferring it to function return:

```go
const (
    healthCheckInterval = 30 * time.Second
    activeCheckFraction = 0.05  // actively dial 5% of known peers per cycle
    activeCheckMax      = 20    // never more than 20 dials per cycle
)

gocron.Every(30).Second().Do(func() {
    host, _ := GetP2PNode(nil)
    allPeers := host.Peerstore().Peers()

    sampleSize := len(allPeers)
    if max := int(float64(len(allPeers)) * activeCheckFraction); max < sampleSize {
        sampleSize = max
    }
    if sampleSize > activeCheckMax {
        sampleSize = activeCheckMax
    }

    // Shuffle and take sampleSize peers.
    rand.Shuffle(len(allPeers), func(i, j int) { allPeers[i], allPeers[j] = allPeers[j], allPeers[i] })
    sample := allPeers[:sampleSize]

    for _, pid := range sample {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        // ✅ cancel is called at the END of each iteration, not deferred to
        //    function return, so contexts are not allowed to accumulate.
        err := host.Connect(ctx, peer.AddrInfo{ID: pid, Addrs: host.Peerstore().Addrs(pid)})
        cancel()
        // ... update node table based on err
        _ = err
    }
})
```

This converts the per-node work from O(N) dials to O(min(N×0.05, 20)) dials per cycle, independent of total fleet size.

---

### Solution 5 — Enable the Connection Manager and Resource Manager

**Addresses:** Bottleneck 4

Re-enable the connection manager and replace the `NullResourceManager` with one that enforces sensible per-node limits. The libp2p connection manager will automatically prune cold connections when the high watermark is exceeded, preserving file descriptors for active traffic.

```go
import (
    connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
    rcmgr   "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
)

// Connection manager: keep between 400 and 600 open connections.
// Peers tagged with "keep" (already done in crdt.go) are protected from pruning.
cm, _ := connmgr.NewConnManager(
    400,
    600,
    connmgr.WithGracePeriod(2*time.Minute),
)

// Resource manager: hard limits to prevent FD exhaustion.
limiter := rcmgr.NewFixedLimiter(rcmgr.InfiniteLimits)
limiter.SystemLimits.SetConns(1024, 512, 512)      // total, inbound, outbound
limiter.SystemLimits.SetStreams(4096, 2048, 2048)
limiter.SystemLimits.SetMemory(512 << 20)           // 512 MiB
rm, _ := rcmgr.NewResourceManager(limiter)

opts := []libp2p.Option{
    // ...
    libp2p.ConnectionManager(cm),
    libp2p.ResourceManager(rm),
    // ...
}
```

Limits should be tuned per deployment based on available file descriptors (`ulimit -n`) and expected cluster size.

---

### Solution 6 — Load-Aware Routing with Circuit Breakers

**Addresses:** Bottleneck 5

The hardware utilisation data already collected and published into the CRDT (`Hardware.GPUs[].UsedMemory`, `Hardware.GPUs[].TotalMemory`) should drive routing decisions. Replace uniform random selection with weighted random selection proportional to available GPU memory headroom, and add a circuit-breaker threshold to exclude saturated nodes.

```go
// scorePeer returns a value in [0.0, 1.0] where higher means more preferred.
// It uses available GPU memory headroom as the primary signal.
func scorePeer(p protocol.Peer) float64 {
    if len(p.Hardware.GPUs) == 0 {
        return 0.5 // unknown utilisation; treat as mid-range
    }
    var free, total int64
    for _, gpu := range p.Hardware.GPUs {
        free  += gpu.TotalMemory - gpu.UsedMemory
        total += gpu.TotalMemory
    }
    if total == 0 {
        return 0.5
    }
    return float64(free) / float64(total)
}

// weightedRandomSelect picks a candidate proportional to GPU headroom.
// Nodes with less than 5% headroom are excluded (circuit breaker).
func weightedRandomSelect(candidates []string, peerMap map[string]protocol.Peer) string {
    weights := make([]float64, len(candidates))
    total   := 0.0
    for i, id := range candidates {
        w := scorePeer(peerMap[id])
        if w < 0.05 { // circuit breaker: exclude near-saturated nodes
            w = 0
        }
        weights[i] = w
        total      += w
    }
    if total == 0 {
        // All nodes saturated; fall back to uniform random to spread the load.
        return candidates[rand.Intn(len(candidates))]
    }
    r := rand.Float64() * total
    cumulative := 0.0
    for i, w := range weights {
        cumulative += w
        if r <= cumulative {
            return candidates[i]
        }
    }
    return candidates[len(candidates)-1]
}
```

This requires no new data collection — the utilisation figures are already being gathered via `platform.GetGPUInfo()` and published into the CRDT on every re-announcement.

---

### Solution 7 — Three-Tier Hierarchical Cluster Architecture

**Addresses:** Bottleneck 6, Bottleneck 2, Bottleneck 7

Introduce a formal three-tier topology that limits each node's CRDT dataset and gossip traffic to its own cluster, rather than the entire global fleet.

**Tier 1 — Global Aggregators (2–5 nodes, always-on, e.g. Kubernetes)**
- Subscribe to all inter-cluster summary topics.
- Maintain a coarsened global directory: `{ "llm": { "model=Llama-3-70B": ["clariden", "bristen"] } }`.
- Accept all external user requests and route them to the correct cluster head.
- Run the public API (authentication, rate limiting, Prometheus metrics).

**Tier 2 — Cluster Head Nodes (1–3 per HPC sub-cluster)**
- Maintain the full CRDT for their own cluster (hundreds of nodes).
- Publish a coarsened cluster-level summary to Tier 1 every 30 seconds.
- Perform load-aware routing within their own cluster.

**Tier 3 — Worker Nodes (hundreds to thousands per cluster)**
- Participate only in their own cluster's CRDT and GossipSub topic.
- Never subscribe to global topics or see the full fleet state.
- Register their services with the cluster head.

This is a natural evolution of SwissAI's existing deployment (Kubernetes API frontend routing to Slurm workers). The change codifies that topology directly into the protocol, making it explicit, automatic, and independently scalable per cluster.

---

### Solution 8 — Sharded CRDT Namespaces for Horizontal Data Partitioning

**Addresses:** Bottleneck 7, Bottleneck 2

Split the single global CRDT namespace `"ocf-crdt"` into independent shards. Each shard has its own DAG, its own GossipSub topic, and its own convergence cycle. Nodes only participate in the shards relevant to their peers.

```go
// Derive the shard key from the first byte of the peer ID.
// This gives 256 balanced shards; at 100k nodes each shard holds ~390 peers.
func getCRDTShard(peerID string) string {
    return fmt.Sprintf("ocf-crdt-shard-%02x", peerID[0])
}

// A node subscribes only to its own shard's topic.
// A small number of index nodes maintain a cross-shard directory that maps
// service names to the shard IDs that contain providers for that service.
// This directory is tiny (services × shards) and fits comfortably in memory.
```

Each shard converges independently. DAG merge workers are no longer competing across 100k entries — each shard's merge pipeline handles only ~390 entries. This also makes tombstone compaction proportionally cheaper.

---

### Solution 9 — Hierarchical Bootstrap Discovery with DNS SRV

**Addresses:** Bottleneck 6

Replace the flat bootstrap list with a two-level DNS-based discovery hierarchy. The existing `fetchDNSAddrBootstraps()` function in `bootstrap.go` already supports `dnsaddr://` — this solution adds a regional second level.

```
Level 1 — Global DNS record (one entry, resolves to regional pointers)
  _dnsaddr.opentela.example.com
    → dnsaddr=/dns4/eu-bootstrap.opentela.example.com/tcp/4001/...
    → dnsaddr=/dns4/us-bootstrap.opentela.example.com/tcp/4001/...
    → dnsaddr=/dns4/as-bootstrap.opentela.example.com/tcp/4001/...

Level 2 — Regional DNS records (one per region, resolves to actual nodes)
  _dnsaddr.eu-bootstrap.opentela.example.com
    → dnsaddr=/ip4/1.2.3.4/tcp/4001/p2p/QmA...
    → dnsaddr=/ip4/5.6.7.8/tcp/4001/p2p/QmB...
```

A node joining from CSCS Switzerland resolves the global record, identifies the EU region, and connects only to EU bootstrap nodes. It never contacts US or Asia bootstraps unless EU is entirely unavailable. Bootstrap load is distributed geographically rather than concentrated on a single shared list.

---

### Solution 10 — Switch to Ed25519 for Node Identity Keys

**Addresses:** Bottleneck 9

This is a one-line change that dramatically speeds up node startup in high-churn environments where hundreds of Slurm nodes restart concurrently.

```go
// Before: RSA-2048 — 100–500 ms per key generation
priv, _, err = crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)

// After: Ed25519 — microseconds per key, equivalent security for this use case
priv, _, err = crypto.GenerateKeyPairWithReader(crypto.Ed25519, -1, r)
```

Note: changing the key type will change the peer ID format for any node that has not yet generated a key. Nodes with an existing key file on disk continue to use it unchanged. Only nodes performing a first-time key generation (new Slurm jobs with empty home directories) are affected.

---

### Solution 11 — Singleton Shared `http.Client` in `RemoteGET`

**Addresses:** Bottleneck 8

Replace the per-call client allocation with a package-level singleton that uses a pooled transport. This enables TCP connection reuse across all `RemoteGET` calls on the same node.

```go
var remoteHTTPClient = &http.Client{
    Timeout: 5 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        64,
        MaxIdleConnsPerHost: 8,
        IdleConnTimeout:     90 * time.Second,
    },
}

func RemoteGET(url string) ([]byte, error) {
    req, err := http.NewRequest(http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }
    res, err := remoteHTTPClient.Do(req)
    // ...
}
```

---

### Solution 12 — Cap `healthCheckRemote` Retries with a Sensible Timeout

**Addresses:** Bottleneck 10

Replace the 6,000-iteration retry ceiling (≈16.7 hours) with a configurable deadline that defaults to a fraction of the expected Slurm job wall time.

```go
// healthCheckRemote polls the given port until it responds or the deadline passes.
func healthCheckRemote(port string, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        _, err := remoteHTTPClient.Get("http://localhost:" + port + "/health")
        if err == nil {
            return nil
        }
        common.Logger.Infof("Waiting for service on port %s (%s remaining)...",
            port, time.Until(deadline).Round(time.Second))
        time.Sleep(10 * time.Second)
    }
    return fmt.Errorf("service on port %s did not become healthy within %s", port, timeout)
}

// Called with a sensible default, e.g. 30 minutes.
err := healthCheckRemote(servicePort, viper.GetDuration("service.startup_timeout"))
```

---

## Scaling Roadmap by Target Size

Rather than applying all solutions at once, the following phased roadmap applies changes in order of impact and risk.

### Stage 1 — Hundreds of Nodes (current → ~500)

Quick wins: correctness fixes and parameter tuning with minimal code change.

| Priority | Change | Files |
|---|---|---|
| 1 | Fix `defer cancel()` bug inside the health-check loop | `clock.go` |
| 2 | Switch to Ed25519 key generation | `host.go` |
| 3 | Tune GossipSub `D=8, Dlo=4, Dhi=12` | `crdt.go` |
| 4 | Replace channel semaphore with `sync.RWMutex` | `node_table.go` |
| 5 | Enable `connmgr` with low/high watermarks | `host.go` |
| 6 | Cap `healthCheckRemote` to 30 minutes | `registrar.go` |
| 7 | Singleton `http.Client` in `RemoteGET` | `common/requests.go` |

### Stage 2 — Thousands of Nodes (~500 → ~10,000)

Structural improvements to the routing and state layers that require moderate code changes.

| Priority | Change | Files |
|---|---|---|
| 8 | Materialised `ServiceIndex` — O(1) routing lookups | `node_table.go`, `crdt.go` |
| 9 | Load-aware weighted random routing using GPU utilisation | `proxy_handler.go` |
| 10 | Sampled health checks instead of O(N) dial loop | `clock.go` |
| 11 | Enable `MultiHeadProcessing: true`, increase `NumWorkers` to 16 | `go-ds-crdt/crdt.go` |
| 12 | Two-level regional DNS bootstrap hierarchy | `bootstrap.go` |

### Stage 3 — Tens to Hundreds of Thousands of Nodes (~10k → 100k+)

Architectural changes that cannot be deferred at this scale. These require coordinated deployment changes alongside code changes.

| Priority | Change | Scope |
|---|---|---|
| 13 | Three-tier cluster hierarchy (global aggregators → cluster heads → workers) | Architecture + config |
| 14 | Sharded CRDT namespaces partitioned by cluster or peer ID prefix | `crdt.go`, `protocol/` |
| 15 | Per-cluster GossipSub topics with inter-cluster bridge nodes | `crdt.go`, `host.go` |
| 16 | Cross-shard service directory maintained by aggregator nodes | New `index/` package |

---

## Target Architecture at 100k Scale

The following diagram shows what the fully-scaled deployment looks like end-to-end, combining Solutions 3, 7, 8, and 9 into a single coherent topology.

```
┌─────────────────────────────────────────────────────────────────────┐
│                   TIER 1 — Global Aggregators                       │
│           (3–5 always-on nodes, e.g. on Kubernetes)                 │
│                                                                     │
│  • Subscribe to all inter-cluster summary topics                    │
│  • Maintain a coarsened global directory:                           │
│      { "llm": { "model=Llama-3-70B": ["clariden", "bristen"] } }   │
│  • Accept all external user requests; route to the correct          │
│    cluster head based on the global directory                       │
│  • Run the public API (auth, rate limiting, Prometheus metrics)     │
└──────────────────────┬──────────────────────────────────────────────┘
                       │  inter-cluster GossipSub topics
                       │  "ocf-crdt-global"  (low-frequency summaries only)
            ┌──────────┴───────────┐
            │                      │
┌───────────▼──────────┐  ┌────────▼─────────────┐
│  TIER 2              │  │  TIER 2               │   (one per HPC sub-cluster)
│  Cluster Head        │  │  Cluster Head         │
│  "clariden"          │  │  "bristen"            │
│                      │  │                       │
│  • Full CRDT of its  │  │  • Full CRDT of its   │
│    own cluster       │  │    own cluster        │
│  • Publishes coarse  │  │  • Publishes coarse   │
│    summary to Tier 1 │  │    summary to Tier 1  │
│    every 30 seconds  │  │    every 30 seconds   │
│  • Load-aware        │  │  • Load-aware routing │
│    routing within    │  │    within bristen     │
│    clariden          │  │                       │
└──────────┬───────────┘  └──────────┬────────────┘
           │                         │
           │  intra-cluster GossipSub topics
           │  "ocf-crdt-clariden"    "ocf-crdt-bristen"
           │  (full-frequency, full-fidelity updates)
    ┌──────┴──────┐               ┌──────┴──────┐
    │             │               │             │
┌───▼───┐   ┌────▼──┐       ┌────▼──┐   ┌──────▼┐
│Worker │   │Worker │       │Worker │   │Worker │
│ Node  │   │ Node  │       │ Node  │   │ Node  │
│       │   │       │       │       │   │       │
│ vLLM  │   │ vLLM  │       │ vLLM  │   │ vLLM  │
└───────┘   └───────┘       └───────┘   └───────┘
  (thousands of Slurm nodes per cluster)
```

### Key properties of this architecture

- **Worker nodes** only ever participate in their own cluster's CRDT. The total gossip traffic they generate and consume is bounded by cluster size (hundreds of nodes), not global fleet size (100k nodes).

- **Cluster heads** (Tier 2) hold a complete picture of their own cluster and a coarse picture of other clusters. They are the routing decision-makers for all intra-cluster requests and the authoritative source for cluster-level health summaries.

- **Global aggregators** (Tier 1) never process the full 100k-node CRDT. They only process cluster-level summaries — a dataset of at most `num_clusters × num_services` entries, which is entirely manageable regardless of total fleet size.

- **Failure isolation** is improved. A GossipSub storm or a CRDT repair cycle in one cluster does not affect other clusters. A Tier 2 cluster head going offline degrades only that cluster; Tier 1 can temporarily route around it.

- **This is an evolution, not a rewrite.** SwissAI already deploys OpenTela with this logical structure (Kubernetes API frontend + per-cluster Slurm nodes). The change codifies that existing topology into the protocol itself — adding cluster-aware topic naming and a coarsened summary publication mechanism — rather than requiring a ground-up redesign.

---

## Bottleneck and Solution Summary

| # | Severity | File | Problem | Solution |
|---|---|---|---|---|
| 1 | 🔴 Critical | `node_table.go` | Global capacity-1 channel serialises all table operations | Sharded `sync.RWMutex` map (Solution 1) |
| 2 | 🔴 Critical | `crdt.go` | GossipSub `D=128` floods the network at scale | Tune to `D=8`, shard topics by cluster (Solution 3) |
| 3 | 🔴 Critical | `clock.go` | O(N) dial loop every 30 s + `defer cancel()` bug inside loop | Sampled health checks + move `cancel()` out of defer (Solution 4) |
| 4 | 🔴 Critical | `host.go` | `NullResourceManager` — no connection or FD limits | Enable `connmgr` + `rcmgr` with sensible limits (Solution 5) |
| 5 | 🟡 Medium | `proxy_handler.go` | Uniform random routing ignores GPU utilisation data | Weighted random via GPU memory headroom + circuit breaker (Solution 6) |
| 6 | 🟡 Medium | Architecture | Flat topology — no cluster hierarchy, geography-blind routing | Three-tier cluster hierarchy with aggregator nodes (Solution 7) |
| 7 | 🟡 Medium | `go-ds-crdt/crdt.go` | Single CRDT namespace, 5 workers, DAG accumulation under high churn | Sharded CRDT namespaces + increase `NumWorkers` + enable `MultiHeadProcessing` (Solution 8) |
| 8 | 🟡 Medium | `common/requests.go` | New `http.Client` allocated on every `RemoteGET` — no connection pooling | Singleton shared `http.Client` with pooled transport (Solution 11) |
| 9 | 🟡 Medium | `host.go` | RSA-2048 key generation takes 100–500 ms during mass cluster restarts | Switch to Ed25519 — microseconds, equivalent security (Solution 10) |
| 10 | 🟡 Medium | `registrar.go` | `healthCheckRemote` can retry silently for up to 16.7 hours | Cap with a configurable deadline defaulting to 30 minutes (Solution 12) |
| 11 | 🟢 Low | `bootstrap.go` | Flat bootstrap list with no regional awareness | Two-level DNS SRV hierarchy with regional bootstrap tiers (Solution 9) |
| 12 | 🟢 Low | `node_table.go`, `proxy_handler.go` | Full table scan on every routing request | Materialised, incrementally-updated service index — O(1) lookup (Solution 2) |
