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
- 20s ping published via GossipSub (D=128, effectively flooding the mesh)

**Additional concerns:**
- CRDT worker pool fixed at 5 with 5-minute timeout
- Tombstone compaction (512/hour) cannot keep up with high churn
- O(N) maintenance ticker every 30s with nested semaphore acquisitions
- HTTP connection pool (100 max) undersized for 1000 targets
- Random load balancing with no health/load awareness
- Badger DB using default options via `go-ds-badger` wrapper, no tuning for write-heavy CRDT workload

## 2. Deployment Assumptions

- **Topology:** Flat mesh. Most nodes are workers, a few serve as head/dispatcher nodes. Any node can take either role.
- **Consistency:** Sub-5s convergence (ideal), graceful degradation to 10-30s under extreme load.
- **Backward compatibility:** Clean break. All nodes upgrade together.
- **Churn:** Highly dynamic — nodes join and leave frequently (spot instances, volunteer computing).
- **Network conditions:** Heterogeneous — mix of datacenter (sub-ms RTT), WAN (10-50ms RTT), and volunteer/consumer networks (50-500ms RTT, occasional spikes to seconds).

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
| Liveness detection | 20s ping via GossipSub (D=128, flooding mesh) | SWIM protocol: probe random peer, indirect probe via k peers. O(log N) messages |
| State propagation | Every peer update -> CRDT delta -> 5s rebroadcast to 128 peers | CRDT only for service metadata changes. Rebroadcast 60s |
| Node table access | Semaphore(1), scan on every request | atomic.Pointer, copy-on-write snapshot. Readers never block |
| Failure detection | 30s ticker iterates all peers | SWIM suspicion: suspect -> confirm dead in ~5-10s |
| Churn handling | Full CRDT delta per join/leave + 24h tombstone | Membership event (tiny) + CRDT put/delete only for service metadata |

### What stays the same

- libp2p as transport (SWIM messages sent over libp2p streams)
- CRDT for durable state (service registrations, usage records, attestations)
- GossipSub for CRDT delta broadcast (with tuned parameters)
- HTTP/Gin routing layer (with improved node table access)
- Badger DB backing store

## 4. Membership Layer (SWIM Protocol)

SWIM achieves O(log N) convergence with constant per-node message load. Each node sends the same number of messages whether there are 10 or 10,000 peers.

### Bootstrap & Discovery

SWIM requires an initial member list. Integration with existing discovery:

1. **Initial seeding:** On startup, the node connects to bootstrap peers via DHT (existing mechanism). Once connected, it sends a SWIM `Join` message to bootstrap peers. They respond with a partial member list (random subset of known alive members).
2. **Ongoing discovery:** Kademlia DHT continues running for peer address resolution. SWIM handles membership state (alive/suspect/dead). DHT provides "how to reach a peer"; SWIM provides "is this peer alive."
3. **Bootstrap failure:** If all bootstrap peers are unreachable, the node retries with exponential backoff (existing `startAutoReconnect` logic). SWIM probing begins once at least one peer is known.
4. **Member list synchronization:** On join, a new node requests a full member list from its first SWIM contact. This is a one-time transfer, not ongoing — subsequent updates arrive via piggybacked events.

### Protocol mechanics

**Probe cycle** (runs every `T` interval, default 500ms):

1. Pick a random peer from the known member list
2. Send `ping` directly over a libp2p stream
3. If ACK within timeout (configurable, default 500ms): peer is alive, done
4. If no ACK: send `ping-req` to `k` random peers (default k=3), asking them to probe the target
5. If no indirect ACK within timeout (configurable, default 1s): mark peer as **suspect**
6. Suspect state persists for configurable window (default 5s). If no refutation: declare **dead**, broadcast leave event

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
- `metadata`: max 256 bytes. Fixed schema: `{role uint8, identityGroups []string (truncated to fit), activeRequests uint16, regionHint uint16}`. Carried on `Join` and `Alive` events only (not every probe). See Section 7 for how routing uses this data.

### Parameters

All SWIM parameters are configurable via `cfg.yaml` under `swim.*`:

| Parameter | Default | Rationale |
|-----------|---------|-----------|
| Probe interval (`T`) | 500ms | 1000 nodes * 1 probe/500ms = 2000 probes/sec network-wide |
| Probe timeout | 500ms | Safe for WAN; reduce to 200ms in datacenter-only deployments |
| Indirect probe timeout | 1s | Allows for two network hops |
| Indirect probes (`k`) | 3 | Balances false-positive rate vs. message overhead |
| Suspect timeout | 5s | Tolerates GC pauses and consumer network spikes. Configurable up to 30s for high-latency environments |
| Retransmit limit | `3 * log(N)` | ~30 retransmits at 1000 nodes |

**False positive analysis:** With probe timeout=500ms, indirect timeout=1s, suspect timeout=5s, a node must be unreachable for 6.5s continuously to be declared dead. In datacenter environments, false positives are near zero. On consumer networks with occasional 1-5s spikes, the indirect probe path (k=3 different network paths) provides resilience. Only sustained unreachability triggers dead status.

### What this removes

- 20s ping published via GossipSub (`crdt.go:80-92`)
- 30s maintenance ticker scanning all peers (`clock.go:28-75`)
- Per-peer reconnect attempts with 5s timeout in ticker
- 1-minute reconnection scheduler (`clock.go:22-26`)

### Failure detection comparison

| Metric | Current | SWIM |
|--------|---------|------|
| Detection time | 30-60s | 5-10s (configurable) |
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
| Load metrics | No (SWIM metadata) | Changes too frequently for CRDT |

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
| Tombstone retention | 24h | 6h | See tombstone safety analysis below |

### Tombstone retention analysis

Reducing tombstone retention from 24h carries a resurrection risk: a node offline longer than the retention window may miss a deletion and re-introduce stale data via anti-entropy sync.

**6h retention rationale:**
- With SWIM handling liveness (not CRDT), the primary tombstone concern is deleted service registrations and usage records, not peer liveness.
- A node offline for >6h in a high-churn volunteer environment is likely dead and will re-register fresh on rejoin.
- Anti-entropy sync (see below) includes tombstone awareness to prevent resurrection.
- Maximum safe offline duration: **6 hours**. Nodes offline longer must perform a full state reset on rejoin (request complete state from a peer, discarding local CRDT state).

**Forced reset on rejoin:** If SWIM detects a node that was previously declared dead rejoining after >6h (tracked via last-known-dead timestamp), the CRDT layer triggers a full state pull from a healthy peer rather than merging local state. This prevents tombstone-expired deletions from being resurrected.

### Anti-entropy sync (new)

Periodic consistency repair without gossip traffic increase:

1. Every 60s, pick a random peer
2. Exchange **Merkle tree digests** of current CRDT key set (more precise than Bloom filters for bidirectional sync)
3. For each differing subtree, exchange the actual keys
4. Each side categorizes differences:
   - Key present locally, absent remotely: check if key has a local tombstone → if yes, it was deleted, do not send. If no tombstone, send delta to remote.
   - Key absent locally, present remotely: request delta from remote.
   - Key present on both with different values: use CRDT merge semantics (higher priority wins).
5. Send missing/updated deltas point-to-point (libp2p stream, not PubSub)

**Key set enumeration:** Maintained incrementally — the Merkle tree is updated on each CRDT put/delete, not rebuilt by scanning Badger. Tree nodes are cached in memory (~32 bytes per leaf, ~32KB for 1000 keys).

**Deletion handling:** The Merkle tree includes tombstone entries (with expiry timestamp). During anti-entropy, a tombstone on one side prevents the other side from re-sending the deleted key. After tombstone expiry (6h), the forced-reset-on-rejoin mechanism (above) prevents resurrection.

**Wire protocol:** `/opentela/antientropy/1.0.0` over libp2p streams. Messages: `DigestExchange{treeLevel int, hashes [][]byte}`, `KeyExchange{keys []string, values [][]byte}`, `DeltaTransfer{deltas []CRDTDelta}`.

### Badger DB tuning

**Note:** The current codebase uses `github.com/ipfs/go-ds-badger` wrapper (`crdt.go:38`). This wrapper exposes a subset of raw Badger options. Implementation must verify which options are available through the wrapper API. If insufficient, migrate to `go-ds-badger4` or use raw Badger directly with a thin datastore adapter.

Target tuning (subject to wrapper API availability):

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

### Snapshot clone cost analysis

At 1000 peers with ~3 services each:
- `Peers` map: 1000 entries, each pointer (8 bytes) + key (38 bytes for PeerID) = ~46KB
- `ByService`, `ByIdentity`, `ByRole`: ~3000 total index entries, each pointer = ~24KB
- Peer structs themselves: ~1KB each = ~1MB (shared via pointers, not deep-copied)

**Total clone cost:** ~70KB of map metadata copied per write. The `Peer` structs are immutable — new writes create new `Peer` values, old snapshots retain pointers to old values. This is a shallow clone of maps, not a deep copy of all peer data.

**Clone duration:** Map cloning at 70KB is <100us on modern hardware. At 10 writes/sec (batched), that's <1ms/sec spent cloning — negligible.

**GC pressure:** At most 2-3 concurrent snapshots exist (current + 1-2 in-flight reader goroutines). Each snapshot's map overhead is ~70KB. Total overhead: <500KB. The `Peer` structs are shared across snapshots and only GC'd when no snapshot references them.

### Pre-built indexes

| Query | Current | Proposed |
|-------|---------|----------|
| Peers for service | O(N*M) scan under lock | `snapshot.ByService["vllm"]` — O(1) |
| Peers for identity group | O(N*M) scan under lock | `snapshot.ByIdentity["model=Qwen3-8B"]` — O(1) |
| All connected peers | O(N) scan under lock | Pre-filtered at write time |

### Event sources and join sequence

The node table receives updates from two sources:

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

**Node join sequence:**

1. **T=0:** SWIM detects new peer via `Join` event. Node table creates entry with `status=alive` and SWIM metadata (role, identity groups from metadata field). **Peer is now routable** using SWIM metadata if identity group matches.
2. **T=0 to T~5s:** CRDT sync begins. New peer publishes its full service registrations, attestations, hardware info via CRDT.
3. **T~5s+:** CRDT update arrives. Node table entry updated with full service metadata, attestation verification status, hardware info. **Full routing with all signals now available.**

During the gap (step 1-2), the routing layer uses SWIM metadata for identity group matching. This provides basic routing within seconds of join. The `ByIdentity` index is populated from SWIM metadata first, then enriched by CRDT data.

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
| Load | SWIM metadata: `activeRequests` field (uint16, updated on Alive events) | 0.2 |
| Locality | SWIM metadata: `regionHint` field | 0.1 |

Weighted random (not strict best) avoids thundering herd.

### Request retry with peer exclusion

1. Pick peer A via weighted selection, forward
2. If A fails (connection error, 502, 503): exclude A, pick peer B, retry once
3. If B fails: return error to client

No retry on 4xx or success. Max 1 retry.

### Identity group routing optimization

Current code reads the full request body to extract the `model` field for identity group matching (`proxy_handler.go:292`). Two improvements:

1. **Preferred: Routing hint header.** Clients set `X-Otela-Identity-Group: model=Qwen3-8B` header. If present, skip body parsing entirely. This is the recommended path for all API clients.
2. **Fallback: Bounded body parse.** If no header is present, read first 8KB of body to find the `model` field. If not found within 8KB, fall back to URL-based routing. Document that the `model` field must appear in the first 8KB of the JSON request body for automatic routing to work.

### Connection pool scaling

| Parameter | Current | Proposed |
|-----------|---------|----------|
| MaxIdleConns | 100 | 512 |
| MaxIdleConnsPerHost | 10 | 4 |
| IdleConnTimeout | 90s | 60s |

512 idle connections provides coverage for ~500 active targets while remaining bounded. Combined with the resource manager's 2048 total connection limit (Section 8), this prevents file descriptor exhaustion.

## 8. Resource Management & Backpressure

### libp2p Resource Manager

```go
scalingLimits := rcmgr.ScalingLimitConfig{
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
}
// Scale(memory int64, numFD int) — 2GB memory budget, 1024 file descriptors
limiter := rcmgr.NewFixedLimiter(scalingLimits.Scale(2 << 30, 1024))
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

## 9. Observability

### Metrics

All metrics prefixed with `otela_` and registered with Prometheus.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `otela_swim_probe_duration_seconds` | Histogram | `result` (ack/timeout/indirect) | Probe round-trip time |
| `otela_swim_probe_total` | Counter | `result` | Probe outcomes |
| `otela_swim_member_count` | Gauge | `status` (alive/suspect/dead) | Current membership view |
| `otela_swim_false_positive_total` | Counter | — | Peers declared dead that later rejoined within 60s |
| `otela_swim_event_dissemination_rounds` | Histogram | `event_type` | Rounds to fully disseminate an event |
| `otela_crdt_rebroadcast_size_bytes` | Histogram | — | Size of rebroadcast messages |
| `otela_crdt_antientropy_keys_synced` | Counter | `direction` (sent/received) | Keys exchanged during anti-entropy |
| `otela_crdt_antientropy_duration_seconds` | Histogram | — | Anti-entropy sync round-trip time |
| `otela_crdt_tombstone_count` | Gauge | — | Current tombstone count |
| `otela_crdt_compaction_duration_seconds` | Histogram | — | Compaction cycle duration |
| `otela_nodetable_snapshot_clone_duration_seconds` | Histogram | — | Time to clone + rebuild snapshot |
| `otela_nodetable_snapshot_generation` | Gauge | — | Current snapshot generation number |
| `otela_nodetable_events_batched` | Histogram | — | Events per batch |
| `otela_routing_peer_score` | Gauge | `peer`, `service` | Current routing score |
| `otela_routing_retry_total` | Counter | `service`, `reason` | Retry attempts |
| `otela_routing_candidate_pool_size` | Histogram | `service` | Candidates available per request |
| `otela_rcmgr_connections` | Gauge | `direction` (inbound/outbound) | Active libp2p connections |
| `otela_rcmgr_streams` | Gauge | `direction` | Active libp2p streams |
| `otela_rcmgr_memory_bytes` | Gauge | `scope` (system/peer) | Memory used by libp2p |

## 10. Migration & Implementation Strategy

### Build sequence

```
Phase 1: Foundation (no behavior change)
  1a. Node table -> copy-on-write snapshot (replace semaphore)
  1b. libp2p ResourceManager (replace NullResourceManager)
  1c. Badger DB tuning, compaction parameter changes

Phase 2: Membership layer
  2a. SWIM protocol implementation (probe, ping-req, suspect/dead)
  2b. Event dissemination via piggyback
  2c. Bootstrap integration (DHT seeding, member list sync)
  2d. Integration: SWIM events -> NodeTable.Apply()
  2e. Remove old ping broadcaster + maintenance ticker

Phase 3: CRDT tuning
  3a. Reduce GossipSub parameters (D=8-12)
  3b. Increase rebroadcast interval to 60s
  3c. Strip liveness data out of CRDT (status, LastSeen)
  3d. Merkle-tree anti-entropy sync with tombstone awareness
  3e. PutHook optimization (batch verify, cache attestations)
  3f. Forced state reset on rejoin after >6h offline

Phase 4: Routing improvements
  4a. Weighted load balancing
  4b. Request retry with peer exclusion
  4c. X-Otela-Identity-Group header + bounded body parse fallback
  4d. Connection pool scaling
  4e. Head node admission control

Phase 5: Hardening
  5a. Connection pruning tied to SWIM
  5b. Goroutine budgets / worker pools
  5c. Production logging config
  5d. Metrics instrumentation (see Section 9)
  5e. Load testing at 100 / 500 / 1000 nodes
```

**Phases 1 and 4 can be developed in parallel.** Same for Phases 3 and 4.

### Feature flags & rollback

Each phase is gated by a config flag under `scalability.*`:

| Flag | Default | Controls |
|------|---------|----------|
| `scalability.swim_enabled` | false | Phase 2: SWIM membership (when false, falls back to existing ping+ticker) |
| `scalability.crdt_tuned` | false | Phase 3: Tuned CRDT params (when false, uses current D=128, 5s rebroadcast) |
| `scalability.weighted_routing` | false | Phase 4: Weighted LB (when false, uses random selection) |
| `scalability.admission_control` | false | Phase 4e: Head node load shedding |

**Rollback procedure:** Set the relevant flag to `false` and restart. The old code paths remain until the next major release after scale testing confirms stability. Feature flags are removed once the phase is validated at target scale.

**Phase gate criteria:** Each phase must pass before enabling the next:
- Phase 1: All existing tests pass, no performance regression on 10-node testbed
- Phase 2: SWIM converges in <10s at 100 simulated nodes, false positive rate <0.1%
- Phase 3: CRDT state converges within 120s at 100 nodes, no data loss after 6h offline+rejoin
- Phase 4: P99 routing latency <5ms at 100 nodes under load
- Phase 5: Stable for 24h at 1000 simulated nodes

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
| Liveness messages/sec (network) | ~6,400 (N=1000 * D=128 / 20s) | ~2,000 (SWIM probes, 1 probe/500ms/node) |
| Failure detection time | 30-60s | 5-10s |
| CRDT messages/sec | ~25,600 (N=1000 * D=128 / 5s rebroadcast) | ~167 (N=1000 * D=10 / 60s rebroadcast) |
| Node table read latency | Unbounded (semaphore contention) | <100ns (atomic pointer load) |
| Routing lookup | O(N*M) scan under lock | O(1) index lookup, lock-free |
| Memory per node | Unbounded (no resource limits) | Capped at configured limits |
| Tombstone backlog | Grows unbounded under churn | Cleared every 10min, batch=4096 |
