# OpenTela Scalability Redesign: 1000-Node Support

**Date:** 2026-03-13
**Status:** Draft
**Target:** Scale from current limits (~50-100 nodes) to 1000+ nodes with high churn

## 1. Context & Problem

OpenTela is a decentralized distributed computing platform using libp2p, CRDT-based state management, and gossip protocols. The current architecture conflates liveness detection (heartbeats) with state propagation (CRDT), causing all messages to flow at the same high frequency through the same gossip mechanism. This creates several bottlenecks that prevent scaling beyond ~100 nodes:

**Critical bottlenecks:**
- Node table protected by a single-slot semaphore (`tableUpdateSem = make(chan struct{}, 1)`), serializing all reads and writes including crypto verification
- `NullResourceManager` for libp2p — no connection, stream, or memory limits
- GossipSub D=128 with 5s CRDT rebroadcast — O(N*D) traffic scaling
- 20s ping broadcast over PubSub to all mesh peers

**Additional concerns:**
- CRDT worker pool fixed at 5 with 5-minute timeout
- Tombstone compaction (512/hour) cannot keep up with high churn
- O(N) maintenance ticker every 30s with nested semaphore acquisitions
- HTTP connection pool (100 max) undersized for 1000 targets
- Random load balancing with no health/load awareness
- Badger DB using default options, no tuning for write-heavy CRDT workload

## 2. Deployment Assumptions

- **Topology:** Flat mesh. Most nodes are workers, a few serve as head/dispatcher nodes. Any node can take either role.
- **Consistency:** Sub-5s convergence (ideal), graceful degradation to 10-30s under extreme load.
- **Backward compatibility:** Clean break. All nodes upgrade together.
- **Churn:** Highly dynamic — nodes join and leave frequently (spot instances, volunteer computing).

## 3. Architecture Overview

Three-layer separation of concerns:

```
+--------------------------------------------------+
|              Application Layer                    |
|   HTTP routing, load balancing, proxy             |
|   Reads from: Node Table (materialized view)      |
+--------------------------------------------------+
|           Node Table (materialized view)           |
|   atomic.Pointer + copy-on-write snapshots         |
|   Built from membership events + CRDT state        |
+-------------------------+------------------------+
|   Membership Layer      |   State Layer           |
|   (fast path)           |   (slow path)           |
|                         |                         |
|   SWIM protocol         |   CRDT (tuned)          |
|   Liveness probes       |   Service registrations |
|   Join/leave events     |   Usage records         |
|   O(log N) per node     |   Infrequent updates    |
|   Sub-second detection  |   Batched, anti-entropy |
+-------------------------+------------------------+
```

### What changes vs. today

| Concern | Current | Proposed |
|---------|---------|----------|
| Liveness detection | 20s ping broadcast over PubSub to all mesh peers | SWIM protocol: probe random peer, indirect probe via k peers. O(log N) messages |
| State propagation | Every peer update -> CRDT delta -> 5s rebroadcast to 128 peers | CRDT only for service metadata changes. Rebroadcast 60s |
| Node table access | Semaphore(1), scan on every request | atomic.Pointer, copy-on-write snapshot. Readers never block |
| Failure detection | 30s ticker iterates all peers | SWIM suspicion: suspect -> confirm dead in ~3-5s |
| Churn handling | Full CRDT delta per join/leave + 24h tombstone | Membership event (tiny) + CRDT put/delete only for service metadata |

### What stays the same

- libp2p as transport (SWIM messages sent over libp2p streams)
- CRDT for durable state (service registrations, usage records, attestations)
- GossipSub for CRDT delta broadcast (with tuned parameters)
- HTTP/Gin routing layer (with improved node table access)
- Badger DB backing store

## 4. Membership Layer (SWIM Protocol)

SWIM achieves O(log N) convergence with constant per-node message load. Each node sends the same number of messages whether there are 10 or 10,000 peers.

### Protocol mechanics

**Probe cycle** (runs every `T` interval, default 500ms):

1. Pick a random peer from the known member list
2. Send `ping` directly over a libp2p stream
3. If ACK within timeout (200ms): peer is alive, done
4. If no ACK: send `ping-req` to `k` random peers (default k=3), asking them to probe the target
5. If no indirect ACK within timeout (500ms): mark peer as **suspect**
6. Suspect state persists for configurable window (default 3s). If no refutation: declare **dead**, broadcast leave event

**Dissemination via piggybacking:**

- Every probe message carries a small buffer of recent membership events (join/leave/suspect)
- Events have a per-node broadcast counter — retransmit until counter hits `lambda * log(N)` (lambda=3)
- No separate broadcast channel needed — events ride on probes for free

### Messages

All sent over libp2p streams (`/opentela/swim/1.0.0`), not PubSub:

```
Ping        { seq uint64 }
Ack         { seq uint64, events []MemberEvent }
PingReq     { seq uint64, target PeerID }
MemberEvent { peer PeerID, status Join|Alive|Suspect|Dead, incarnation uint64, metadata []byte }
```

- `incarnation`: monotonically increasing per peer. A peer refutes `Suspect` by incrementing incarnation and broadcasting `Alive`. Prevents flapping.
- `metadata`: carries lightweight info (role, identity group names) — enough for basic routing without waiting for full CRDT sync.

### Parameters

| Parameter | Default | Rationale |
|-----------|---------|-----------|
| Probe interval (`T`) | 500ms | 1000 nodes * 1 probe/500ms = 2000 probes/sec network-wide |
| Probe timeout | 200ms | Generous for datacenter; adjust for WAN |
| Indirect probes (`k`) | 3 | Balances false-positive rate vs. message overhead |
| Suspect timeout | 3s | Time before declaring dead. Allows refutation |
| Retransmit limit | `3 * log(N)` | ~30 retransmits at 1000 nodes |

### What this removes

- 20s ping broadcast over PubSub (`crdt.go:80-92`)
- 30s maintenance ticker scanning all peers (`clock.go:28-75`)
- Per-peer reconnect attempts with 5s timeout in ticker
- 1-minute reconnection scheduler (`clock.go:22-26`)

### Failure detection comparison

| Metric | Current | SWIM |
|--------|---------|------|
| Detection time | 30-60s | 3-5s |
| Messages per node per second | O(D) ~ 128 | O(1) = 2 probes/sec |
| Total network messages/sec at 1000 nodes | ~6,400 pings + gossip | ~2,000 probes + piggyback |

## 5. State Layer (Tuned CRDT)

With SWIM handling liveness, CRDT becomes focused on durable state only.

### Data ownership

| Data | In CRDT | Rationale |
|------|---------|-----------|
| Service registrations (name, identity group, port) | Yes | Durable, changes rarely |
| Usage/billing aggregates | Yes | Needs persistence and reconciliation |
| Attestations (build, identity) | Yes | Cryptographic proofs, set once |
| Hardware info (GPUs, memory) | Yes | Set on join, rarely changes |
| Liveness/connected status | No (SWIM) | High frequency |
| LastSeen timestamps | No (SWIM) | Derived from probe responses |
| Load metrics | No (SWIM metadata or scraper) | Changes too frequently for CRDT |

### Parameter changes

| Parameter | Current | Proposed | Why |
|-----------|---------|----------|-----|
| GossipSub D | 128 | 8-12 | CRDT updates are rare; don't need massive fanout |
| GossipSub Dlo/Dhi | 16/256 | 4/16 | Proportional reduction |
| Rebroadcast interval | 5s | 60s | Service registrations change slowly |
| CRDT workers | 5 | 16-32 | Handle burst on node join |
| DAGSyncer timeout | 5min | 30s | Fail fast, retry via anti-entropy |
| MaxBatchDeltaSize | 1MB | 2MB | Larger batches, fewer commits |
| Compaction batch | 512 | 4096 | High churn = many tombstones |
| Compaction interval | 1h | 10min | Faster cleanup |
| Tombstone retention | 24h | 2h | SWIM handles liveness; anti-entropy catches missed deletions |

### Anti-entropy sync (new)

Periodic consistency repair without gossip traffic increase:

1. Every 60s, pick a random peer
2. Exchange Bloom filter digests of current CRDT key set
3. Each side identifies keys the other is missing
4. Send missing deltas point-to-point

A Bloom filter for ~10,000 keys is ~12KB at 1% false positive rate.

### Badger DB tuning

```go
opts.ValueLogFileSize = 64 << 20    // 64MB (default 1GB too large)
opts.NumMemtables = 4                // More memtables for write-heavy bursts
opts.NumLevelZeroTables = 8          // Delay L0 compaction pressure
opts.NumCompactors = 4               // Parallel compaction
opts.BlockCacheSize = 64 << 20       // 64MB block cache
opts.IndexCacheSize = 32 << 20       // 32MB index cache
```

### PutHook optimization

- Batch verification: queue incoming puts, verify in batches of 10-50
- Cache verified attestations keyed by (peerID, attestation hash) with TTL
- Verify asynchronously: mark peer as "unverified" until complete. Routing can prefer verified peers.

## 6. Node Table (Materialized View)

The hot path — every HTTP request reads the node table to route.

### Copy-on-write snapshot design

```go
type NodeTable struct {
    snapshot atomic.Pointer[NodeTableSnapshot]  // Lock-free reads
    mu       sync.Mutex                         // Serializes writers only
}

type NodeTableSnapshot struct {
    Peers       map[peer.ID]*Peer
    ByService   map[string][]*Peer              // service name -> peers
    ByIdentity  map[string][]*Peer              // identity group -> peers
    ByRole      map[string][]*Peer              // role -> peers
    Generation  uint64                          // Monotonic version
}
```

**Read path:** `nt.snapshot.Load()` — atomic pointer load, zero contention.

**Write path:** Acquire mutex, clone current snapshot, apply events to clone, atomic store new pointer. Old snapshots GC'd when readers release them.

### Pre-built indexes

| Query | Current | Proposed |
|-------|---------|----------|
| Peers for service | O(N*M) scan under lock | `snapshot.ByService["vllm"]` — O(1) |
| Peers for identity group | O(N*M) scan under lock | `snapshot.ByIdentity["model=Qwen3-8B"]` — O(1) |
| All connected peers | O(N) scan under lock | Pre-filtered at write time |

### Event sources

```
SWIM membership event              CRDT state change
  (join/leave/suspect/dead)          (service reg, attestation, hardware)
         |                                    |
         v                                    v
    MemberEvent                          CRDTEvent
         |                                    |
         +----------------+------------------+
                          v
                NodeEvent (unified type)
                          |
                          v
                NodeTable.Apply(events...)
                          |
                          v
                New snapshot atomically published
```

**Event batching:** Writer goroutine drains the event channel and applies a batch every 100ms (or immediately if channel has >50 events). One clone + rebuild per batch.

### What this replaces

- `tableUpdateSem = make(chan struct{}, 1)` and all its acquisitions
- O(N*M) scan in `GetAllProviders`
- Per-request lock contention in routing
- Redundant `GetPeerFromTable` / `GetConnectedPeers` / `GetAllPeers`

## 7. Load Balancing & Routing

### Weighted selection

Replace random selection with weighted random based on multiple signals:

```
score(peer) = w1 * availabilityScore
            + w2 * latencyScore
            + w3 * loadScore
            + w4 * localityScore
```

| Signal | Source | Weight |
|--------|--------|--------|
| Availability | SWIM: alive=1.0, suspect=0.2 | 0.4 |
| Latency | libp2p peerstore RTT | 0.3 |
| Load | SWIM metadata piggyback (active request count) | 0.2 |
| Locality | Same datacenter/region hint in metadata | 0.1 |

Weighted random (not strict best) avoids thundering herd.

### Request retry with peer exclusion

1. Pick peer A via weighted selection, forward
2. If A fails (connection error, 502, 503): exclude A, pick peer B, retry once
3. If B fails: return error to client

No retry on 4xx or success. Max 1 retry.

### Request body streaming

Replace full body buffering with `io.TeeReader` — parse first few KB for model field, stream body to proxy simultaneously.

### Connection pool scaling

| Parameter | Current | Proposed |
|-----------|---------|----------|
| MaxIdleConns | 100 | 0 (unlimited) |
| MaxIdleConnsPerHost | 10 | 4 |
| IdleConnTimeout | 90s | 60s |

## 8. Resource Management & Backpressure

### libp2p Resource Manager

```go
limiter := rcmgr.NewFixedLimiter(rcmgr.ScalingLimitConfig{
    SystemBaseLimit: rcmgr.BaseLimit{
        Conns:           2048,
        ConnsInbound:    1024,
        ConnsOutbound:   1024,
        Streams:         8192,
        StreamsInbound:  4096,
        StreamsOutbound: 4096,
        Memory:          1 << 30,     // 1GB
    },
    PeerBaseLimit: rcmgr.BaseLimit{
        Conns:           8,
        ConnsInbound:    4,
        ConnsOutbound:   4,
        Streams:         64,
        StreamsInbound:  32,
        StreamsOutbound: 32,
        Memory:          16 << 20,    // 16MB per peer
    },
}.Scale(1024, 2 << 30))
```

### Connection pruning (tied to SWIM)

- Peer declared **dead**: close libp2p connection after 5s grace period
- Peer marked **suspect**: stop routing new requests, keep connection for probes
- Periodic sweep (60s): close connections to peers not in SWIM member list

### Head node admission control

```
if len(candidates) < minHealthyThreshold:
    acceptRate = len(candidates) / expectedWorkers
    probabilistically reject with 503 + Retry-After header
```

`expectedWorkers` derived from rolling max over 5 minutes.

### Goroutine budgets

| Task | Current | Proposed |
|------|---------|----------|
| CRDT PutHook processing | Unbounded | Worker pool, size 32 |
| SWIM probe handling | N/A | Worker pool, size 16 |
| Proxy request forwarding | 1 per request (Gin) | Keep as-is |
| Anti-entropy sync | N/A | Single goroutine, 60s ticker |
| Node table writer | N/A | Single goroutine draining channel |

### Production logging

```go
cfg := zap.NewProductionConfig()
cfg.Sampling = &zap.SamplingConfig{
    Initial:    100,
    Thereafter: 10,
}
```

## 9. Migration & Implementation Strategy

### Build sequence

```
Phase 1: Foundation (no behavior change)
  1a. Node table -> copy-on-write snapshot (replace semaphore)
  1b. libp2p ResourceManager (replace NullResourceManager)
  1c. Badger DB tuning, compaction parameter changes

Phase 2: Membership layer
  2a. SWIM protocol implementation (probe, ping-req, suspect/dead)
  2b. Event dissemination via piggyback
  2c. Integration: SWIM events -> NodeTable.Apply()
  2d. Remove old ping broadcaster + maintenance ticker

Phase 3: CRDT tuning
  3a. Reduce GossipSub parameters (D=8-12)
  3b. Increase rebroadcast interval to 60s
  3c. Strip liveness data out of CRDT (status, LastSeen)
  3d. Bloom filter anti-entropy sync
  3e. PutHook optimization (batch verify, cache attestations)

Phase 4: Routing improvements
  4a. Weighted load balancing
  4b. Request retry with peer exclusion
  4c. Request body streaming (TeeReader)
  4d. Connection pool scaling
  4e. Head node admission control

Phase 5: Hardening
  5a. Connection pruning tied to SWIM
  5b. Goroutine budgets / worker pools
  5c. Production logging config
  5d. Metrics additions
  5e. Load testing at 100 / 500 / 1000 nodes
```

**Phases 1 and 4 can be developed in parallel.** Same for Phases 3 and 4.

### Testing strategy

| Level | What | How |
|-------|------|-----|
| Unit | SWIM state machine, node table snapshot, weighted selection | Standard Go tests, table-driven |
| Integration | SWIM over libp2p streams, CRDT convergence with tuned params | Multi-node in-process tests using libp2p mock network |
| Scale simulation | 100/500/1000 virtual nodes | Lightweight simulator: goroutine per node with SWIM instance |
| Real deployment | Progressive rollout | 10 -> 50 -> 200 -> 1000 nodes |

### Expected outcomes at 1000 nodes (high churn)

| Metric | Current (projected) | After |
|--------|---------------------|-------|
| Liveness messages/sec (network) | ~6,400 (ping broadcast) | ~2,000 (SWIM probes) |
| Failure detection time | 30-60s | 3-5s |
| CRDT messages/sec | ~200K (5s rebroadcast * D=128) | ~500 (60s rebroadcast * D=10) |
| Node table read latency | Unbounded (semaphore) | <100ns (atomic pointer load) |
| Routing lookup | O(N*M) scan under lock | O(1) index lookup, lock-free |
| Memory per node | Unbounded | Capped at configured limits |
| Tombstone backlog | Grows unbounded under churn | Cleared every 10min, batch=4096 |
