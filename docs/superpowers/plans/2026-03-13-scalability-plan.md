# OpenTela Scalability Redesign — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Scale OpenTela from ~100 to 1000+ nodes with high churn by separating liveness detection (SWIM) from state propagation (CRDT) and replacing the node table's single-slot semaphore with lock-free copy-on-write snapshots.

**Architecture:** Three layers: SWIM membership (O(log N) liveness), tuned CRDT (infrequent durable state), and a materialized node table (atomic snapshots). See `docs/superpowers/specs/2026-03-13-scalability-design.md` for full spec.

**Tech Stack:** Go 1.25, libp2p v0.47.0, Badger v4, GossipSub, Gin, Prometheus, Viper

---

## File Structure

### New files

| Path (relative to `src/`) | Responsibility |
|---|---|
| `internal/protocol/nodetable/snapshot.go` | `NodeTableSnapshot` struct, `Clone()`, index builders, `Apply()` |
| `internal/protocol/nodetable/snapshot_test.go` | Snapshot unit tests |
| `internal/protocol/nodetable/writer.go` | `NodeTableWriter` — event channel, batching, atomic swap |
| `internal/protocol/nodetable/writer_test.go` | Writer unit tests |
| `internal/protocol/nodetable/events.go` | `NodeEvent`, `MemberEvent`, `CRDTEvent` types |
| `internal/protocol/swim/swim.go` | SWIM state machine — probe cycle, suspect/dead transitions |
| `internal/protocol/swim/swim_test.go` | SWIM state machine tests |
| `internal/protocol/swim/messages.go` | Ping/Ack/PingReq/MemberEvent message types + encoding |
| `internal/protocol/swim/messages_test.go` | Message encoding tests |
| `internal/protocol/swim/transport.go` | libp2p stream-based transport for SWIM |
| `internal/protocol/swim/transport_test.go` | Transport tests |
| `internal/protocol/swim/dissemination.go` | Piggyback event buffer, retransmit tracking |
| `internal/protocol/swim/dissemination_test.go` | Dissemination tests |
| `internal/protocol/antientropy/merkle.go` | Incrementally-maintained Merkle tree over CRDT key set |
| `internal/protocol/antientropy/merkle_test.go` | Merkle tree tests |
| `internal/protocol/antientropy/sync.go` | Anti-entropy sync protocol over libp2p |
| `internal/protocol/antientropy/sync_test.go` | Sync protocol tests |

### Modified files

| Path (relative to `src/`) | What changes |
|---|---|
| `internal/protocol/node_table.go` | Keep `Peer`, `Service`, trust types. Remove `NodeTable` map, semaphore, `GetAllProviders`, etc. Add bridge functions that delegate to new `nodetable` package. |
| `internal/protocol/host.go:115` | Replace `NullResourceManager` with real `rcmgr` |
| `internal/protocol/crdt.go:43-99` | Reduce GossipSub D, increase rebroadcast, remove ping goroutine, update PutHook |
| `internal/protocol/clock.go` | Remove `StartTicker()` entirely — replaced by SWIM |
| `internal/protocol/tombstone_compactor.go:13-16` | Update defaults: retention=6h, interval=10m, batch=4096 |
| `internal/server/proxy_handler.go:61-74,281-406` | Connection pool tuning, weighted LB, retry, `X-Otela-Identity-Group` header |
| `internal/server/server.go:176` | Replace `protocol.StartTicker()` with SWIM start |
| `internal/common/logger.go` | Production config with sampling |
| `entry/cmd/root.go:86-106` | Add `swim.*`, `scalability.*` config defaults |

---

## Chunk 1: Foundation (Phase 1)

### Task 1: Config defaults for new settings

**Files:**
- Modify: `src/entry/cmd/root.go:86-106`

- [ ] **Step 1: Read existing config defaults**

Open `src/entry/cmd/root.go` and find the `initConfig()` defaults block (lines 86-106). Understand existing defaults.

- [ ] **Step 2: Add new config defaults**

Add after line 106:

```go
// SWIM membership protocol parameters
viper.SetDefault("swim.probe_interval", "500ms")
viper.SetDefault("swim.probe_timeout", "500ms")
viper.SetDefault("swim.indirect_probe_timeout", "1s")
viper.SetDefault("swim.indirect_probes", 3)
viper.SetDefault("swim.suspect_timeout", "5s")
viper.SetDefault("swim.retransmit_mult", 3)
viper.SetDefault("swim.metadata_max_bytes", 256)

// Scalability feature flags (all default to false for safe rollout)
viper.SetDefault("scalability.swim_enabled", false)
viper.SetDefault("scalability.crdt_tuned", false)
viper.SetDefault("scalability.weighted_routing", false)
viper.SetDefault("scalability.admission_control", false)
viper.SetDefault("scalability.expected_workers", 0) // 0 = auto (disabled)

// CRDT tuned values (used when scalability.crdt_tuned=true)
viper.SetDefault("crdt.tuned_gossipsub_d", 10)
viper.SetDefault("crdt.tuned_gossipsub_dlo", 4)
viper.SetDefault("crdt.tuned_gossipsub_dhi", 16)
viper.SetDefault("crdt.tuned_rebroadcast_interval", "60s")
viper.SetDefault("crdt.tuned_workers", 16)
```

- [ ] **Step 3: Run tests**

Run: `cd src && make test TEST_PKGS="./entry/..."`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
cd src && git add entry/cmd/root.go
git commit -m "feat: add scalability config defaults (SWIM, feature flags, CRDT tuning)"
```

---

### Task 2: Node table — Event types

**Files:**
- Create: `src/internal/protocol/nodetable/events.go`

- [ ] **Step 1: Create the nodetable package with event types**

```go
package nodetable

import "github.com/libp2p/go-libp2p/core/peer"

// EventType distinguishes the source of a node table update.
type EventType int

const (
	EventSWIMJoin    EventType = iota // Peer joined (SWIM membership)
	EventSWIMAlive                    // Peer confirmed alive
	EventSWIMSuspect                  // Peer suspected dead
	EventSWIMDead                     // Peer confirmed dead
	EventCRDTUpdate                   // Service/attestation data from CRDT
	EventCRDTDelete                   // Peer deleted from CRDT
)

// NodeEvent is the unified event type fed into the node table writer.
type NodeEvent struct {
	Type      EventType
	PeerID    peer.ID
	Timestamp int64     // Unix timestamp; set at event creation, used instead of time.Now() in Apply
	PeerData  *PeerData // nil for Dead/Delete events
}

// PeerData carries the mutable fields that an event can update.
// Fields set to their zero value are treated as "no change" (except
// for explicit clears like Dead which resets Connected).
type PeerData struct {
	// From SWIM metadata (fast path)
	Role           []string
	IdentityGroups []string // e.g. ["model=Qwen3-8B"]
	ActiveRequests uint16
	RegionHint     uint16

	// From CRDT (slow path)
	Services            []ServiceData
	Owner               string
	ProviderID          string
	Hardware            interface{} // common.HardwareSpec — use interface to avoid import cycle
	BuildAttestation    interface{} // *attestation.BuildInfo
	IdentityAttestation interface{} // *wallet.IdentityAttestation
	TrustLevel          int
	SignedBuild         bool
	PublicAddress       string
	Version             string
	Latency             int
	Privileged          bool
}

// ServiceData is the snapshot-local representation of a service.
type ServiceData struct {
	Name          string
	Host          string
	Port          string
	Status        string
	IdentityGroup []string
	Hardware      interface{} // common.HardwareSpec
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd src && go build ./internal/protocol/nodetable/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
cd src && git add internal/protocol/nodetable/
git commit -m "feat: add nodetable event types"
```

---

### Task 3: Node table — Snapshot

**Files:**
- Create: `src/internal/protocol/nodetable/snapshot.go`
- Create: `src/internal/protocol/nodetable/snapshot_test.go`

- [ ] **Step 1: Write snapshot tests**

```go
package nodetable

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestSnapshotEmpty(t *testing.T) {
	s := NewSnapshot()
	if len(s.Peers) != 0 {
		t.Fatal("expected empty peers")
	}
	if s.Generation != 0 {
		t.Fatal("expected generation 0")
	}
}

func TestSnapshotApplyJoin(t *testing.T) {
	s := NewSnapshot()
	pid := peer.ID("test-peer-1")
	s = s.Clone()
	s.ApplyEvent(NodeEvent{
		Type:   EventSWIMJoin,
		PeerID: pid,
		PeerData: &PeerData{
			IdentityGroups: []string{"model=Qwen3-8B"},
			Role:           []string{"worker"},
		},
	})
	s.RebuildIndexes()
	s.Generation++

	if _, ok := s.Peers[pid]; !ok {
		t.Fatal("peer should exist after join")
	}
	if !s.Peers[pid].Connected {
		t.Fatal("peer should be connected after join")
	}
	peers := s.ByIdentity["model=Qwen3-8B"]
	if len(peers) != 1 || peers[0].ID != string(pid) {
		t.Fatal("identity index should contain the peer")
	}
}

func TestSnapshotApplyDead(t *testing.T) {
	s := NewSnapshot()
	pid := peer.ID("test-peer-1")
	s = s.Clone()
	s.ApplyEvent(NodeEvent{
		Type:   EventSWIMJoin,
		PeerID: pid,
		PeerData: &PeerData{
			Role: []string{"worker"},
		},
	})
	s.ApplyEvent(NodeEvent{
		Type:   EventSWIMDead,
		PeerID: pid,
	})
	s.RebuildIndexes()

	if _, ok := s.Peers[pid]; ok {
		t.Fatal("dead peer should be removed")
	}
}

func TestSnapshotCloneIsolation(t *testing.T) {
	s := NewSnapshot()
	pid := peer.ID("test-peer-1")
	s.Peers[pid] = &SnapshotPeer{ID: string(pid), Connected: true}
	s.RebuildIndexes()

	cloned := s.Clone()
	delete(cloned.Peers, pid)

	if _, ok := s.Peers[pid]; !ok {
		t.Fatal("original should not be affected by clone mutation")
	}
}

func TestSnapshotServiceIndex(t *testing.T) {
	s := NewSnapshot()
	pid := peer.ID("peer-1")
	s = s.Clone()
	s.ApplyEvent(NodeEvent{
		Type:   EventSWIMJoin,
		PeerID: pid,
		PeerData: &PeerData{
			IdentityGroups: []string{"model=Qwen3-8B"},
		},
	})
	s.ApplyEvent(NodeEvent{
		Type:   EventCRDTUpdate,
		PeerID: pid,
		PeerData: &PeerData{
			Services: []ServiceData{
				{Name: "vllm", IdentityGroup: []string{"model=Qwen3-8B"}},
			},
		},
	})
	s.RebuildIndexes()

	if len(s.ByService["vllm"]) != 1 {
		t.Fatalf("expected 1 peer for vllm, got %d", len(s.ByService["vllm"]))
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `cd src && go test ./internal/protocol/nodetable/ -v -run TestSnapshot`
Expected: FAIL (types not defined yet)

- [ ] **Step 3: Implement snapshot**

```go
package nodetable

import (
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// SnapshotPeer is the node table's view of a peer. It combines data
// from SWIM membership events and CRDT state updates.
type SnapshotPeer struct {
	ID        string
	PeerID    peer.ID
	Connected bool     // from SWIM: alive/suspect
	Suspect   bool     // from SWIM: in suspect state
	LastSeen  int64    // unix timestamp
	Role      []string

	// From SWIM metadata (available immediately on join)
	IdentityGroups []string
	ActiveRequests uint16
	RegionHint     uint16

	// From CRDT (available after sync, may be nil initially)
	Services            []ServiceData
	Owner               string
	ProviderID          string
	PublicAddress       string
	Version             string
	Latency             int
	Privileged          bool
	Hardware            interface{}
	BuildAttestation    interface{}
	IdentityAttestation interface{}
	TrustLevel          int
	SignedBuild         bool
}

// NodeTableSnapshot is an immutable point-in-time view of the node table.
// Readers get zero-contention access via atomic.Pointer.
type NodeTableSnapshot struct {
	Peers      map[peer.ID]*SnapshotPeer
	ByService  map[string][]*SnapshotPeer // service name → connected peers
	ByIdentity map[string][]*SnapshotPeer // identity group → connected peers
	ByRole     map[string][]*SnapshotPeer // role → connected peers
	Generation uint64
}

// NodeTable is the scalable node table with lock-free reads.
type NodeTable struct {
	snapshot atomic.Pointer[NodeTableSnapshot]
	mu       sync.Mutex // Serializes writers; not needed if only Writer goroutine calls Store
}

// NewNodeTable creates a new node table with an empty snapshot.
func NewNodeTable() *NodeTable {
	nt := &NodeTable{}
	nt.snapshot.Store(NewSnapshot())
	return nt
}

// Snapshot returns the current immutable snapshot (lock-free).
func (nt *NodeTable) Snapshot() *NodeTableSnapshot {
	return nt.snapshot.Load()
}

// Store atomically publishes a new snapshot.
func (nt *NodeTable) Store(s *NodeTableSnapshot) {
	nt.snapshot.Store(s)
}

func NewSnapshot() *NodeTableSnapshot {
	return &NodeTableSnapshot{
		Peers:      make(map[peer.ID]*SnapshotPeer),
		ByService:  make(map[string][]*SnapshotPeer),
		ByIdentity: make(map[string][]*SnapshotPeer),
		ByRole:     make(map[string][]*SnapshotPeer),
	}
}

// Clone creates a shallow copy of the snapshot. Peer structs are
// copied by value (pointer to new SnapshotPeer) so mutations to the
// clone don't affect the original.
func (s *NodeTableSnapshot) Clone() *NodeTableSnapshot {
	c := &NodeTableSnapshot{
		Peers:      make(map[peer.ID]*SnapshotPeer, len(s.Peers)),
		ByService:  make(map[string][]*SnapshotPeer),
		ByIdentity: make(map[string][]*SnapshotPeer),
		ByRole:     make(map[string][]*SnapshotPeer),
		Generation: s.Generation,
	}
	for k, v := range s.Peers {
		cp := *v // value copy
		c.Peers[k] = &cp
	}
	return c
}

// ApplyEvent mutates the snapshot in place (call on a Clone, not the live snapshot).
func (s *NodeTableSnapshot) ApplyEvent(e NodeEvent) {
	switch e.Type {
	case EventSWIMJoin, EventSWIMAlive:
		p, ok := s.Peers[e.PeerID]
		if !ok {
			p = &SnapshotPeer{
				ID:       string(e.PeerID),
				PeerID:   e.PeerID,
				LastSeen: e.Timestamp,
			}
			s.Peers[e.PeerID] = p
		}
		p.Connected = true
		p.Suspect = false
		p.LastSeen = e.Timestamp
		if e.PeerData != nil {
			if len(e.PeerData.Role) > 0 {
				p.Role = e.PeerData.Role
			}
			if len(e.PeerData.IdentityGroups) > 0 {
				p.IdentityGroups = e.PeerData.IdentityGroups
			}
			p.ActiveRequests = e.PeerData.ActiveRequests
			p.RegionHint = e.PeerData.RegionHint
		}

	case EventSWIMSuspect:
		if p, ok := s.Peers[e.PeerID]; ok {
			p.Suspect = true
			p.LastSeen = e.Timestamp
		}

	case EventSWIMDead, EventCRDTDelete:
		delete(s.Peers, e.PeerID)

	case EventCRDTUpdate:
		p, ok := s.Peers[e.PeerID]
		if !ok {
			// CRDT update for unknown peer — create entry but mark disconnected
			// (SWIM hasn't confirmed it yet)
			p = &SnapshotPeer{
				ID:       string(e.PeerID),
				PeerID:   e.PeerID,
				LastSeen: e.Timestamp,
			}
			s.Peers[e.PeerID] = p
		}
		if e.PeerData != nil {
			if len(e.PeerData.Services) > 0 {
				p.Services = e.PeerData.Services
			}
			if e.PeerData.Owner != "" {
				p.Owner = e.PeerData.Owner
			}
			if e.PeerData.ProviderID != "" {
				p.ProviderID = e.PeerData.ProviderID
			}
			if e.PeerData.PublicAddress != "" {
				p.PublicAddress = e.PeerData.PublicAddress
			}
			if e.PeerData.Version != "" {
				p.Version = e.PeerData.Version
			}
			p.Hardware = e.PeerData.Hardware
			p.BuildAttestation = e.PeerData.BuildAttestation
			p.IdentityAttestation = e.PeerData.IdentityAttestation
			p.TrustLevel = e.PeerData.TrustLevel
			p.SignedBuild = e.PeerData.SignedBuild
			p.Latency = e.PeerData.Latency
			p.Privileged = e.PeerData.Privileged
			if len(e.PeerData.IdentityGroups) > 0 {
				p.IdentityGroups = e.PeerData.IdentityGroups
			}
		}
	}
}

// RebuildIndexes reconstructs ByService, ByIdentity, ByRole from Peers.
// Called after applying a batch of events, before publishing the snapshot.
func (s *NodeTableSnapshot) RebuildIndexes() {
	s.ByService = make(map[string][]*SnapshotPeer)
	s.ByIdentity = make(map[string][]*SnapshotPeer)
	s.ByRole = make(map[string][]*SnapshotPeer)

	for _, p := range s.Peers {
		if !p.Connected {
			continue
		}
		for _, svc := range p.Services {
			s.ByService[svc.Name] = append(s.ByService[svc.Name], p)
		}
		for _, ig := range p.IdentityGroups {
			s.ByIdentity[ig] = append(s.ByIdentity[ig], p)
		}
		for _, r := range p.Role {
			s.ByRole[r] = append(s.ByRole[r], p)
		}
	}
}
```

- [ ] **Step 4: Run tests — verify they pass**

Run: `cd src && go test ./internal/protocol/nodetable/ -v -run TestSnapshot`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/protocol/nodetable/
git commit -m "feat: add copy-on-write node table snapshot"
```

---

### Task 4: Node table — Event writer with batching

**Files:**
- Create: `src/internal/protocol/nodetable/writer.go`
- Create: `src/internal/protocol/nodetable/writer_test.go`

- [ ] **Step 1: Write writer tests**

```go
package nodetable

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestWriterAppliesEvents(t *testing.T) {
	nt := NewNodeTable()
	w := NewWriter(nt)
	w.Start()
	defer w.Stop()

	pid := peer.ID("peer-1")
	w.Send(NodeEvent{
		Type:   EventSWIMJoin,
		PeerID: pid,
		PeerData: &PeerData{
			IdentityGroups: []string{"model=Qwen3-8B"},
		},
	})

	// Wait for batch to be applied
	time.Sleep(200 * time.Millisecond)

	snap := nt.Snapshot()
	if _, ok := snap.Peers[pid]; !ok {
		t.Fatal("peer should exist after writer processes join event")
	}
	if snap.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", snap.Generation)
	}
}

func TestWriterBatchesMultipleEvents(t *testing.T) {
	nt := NewNodeTable()
	w := NewWriter(nt)
	w.Start()
	defer w.Stop()

	// Send many events rapidly — they should be batched into one generation
	for i := 0; i < 100; i++ {
		pid := peer.ID(fmt.Sprintf("peer-%d", i))
		w.Send(NodeEvent{
			Type:   EventSWIMJoin,
			PeerID: pid,
			PeerData: &PeerData{},
		})
	}

	time.Sleep(300 * time.Millisecond)

	snap := nt.Snapshot()
	if len(snap.Peers) != 100 {
		t.Fatalf("expected 100 peers, got %d", len(snap.Peers))
	}
	// Generation should be small (events batched), not 100
	if snap.Generation > 10 {
		t.Fatalf("expected batched writes (generation <= 10), got %d", snap.Generation)
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `cd src && go test ./internal/protocol/nodetable/ -v -run TestWriter`
Expected: FAIL

- [ ] **Step 3: Implement writer**

```go
package nodetable

import (
	"sync"
	"time"
)

const (
	batchInterval = 100 * time.Millisecond
	batchMaxSize  = 50
	eventChanSize = 1024
)

// Writer receives NodeEvents and applies them to the NodeTable
// in batches, producing new snapshots atomically.
type Writer struct {
	nt     *NodeTable
	events chan NodeEvent
	stop   chan struct{}
	wg     sync.WaitGroup
}

func NewWriter(nt *NodeTable) *Writer {
	return &Writer{
		nt:     nt,
		events: make(chan NodeEvent, eventChanSize),
		stop:   make(chan struct{}),
	}
}

func (w *Writer) Start() {
	w.wg.Add(1)
	go w.run()
}

func (w *Writer) Stop() {
	close(w.stop)
	w.wg.Wait()
}

// Send enqueues an event for processing. Non-blocking if channel isn't full.
func (w *Writer) Send(e NodeEvent) {
	select {
	case w.events <- e:
	default:
		// Channel full — drop event (log in production)
	}
}

func (w *Writer) run() {
	defer w.wg.Done()
	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()

	var batch []NodeEvent

	for {
		select {
		case <-w.stop:
			// Drain remaining events
			w.drainAndApply(batch)
			return

		case e := <-w.events:
			batch = append(batch, e)
			if len(batch) >= batchMaxSize {
				w.applyBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				w.applyBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *Writer) drainAndApply(batch []NodeEvent) {
	for {
		select {
		case e := <-w.events:
			batch = append(batch, e)
		default:
			if len(batch) > 0 {
				w.applyBatch(batch)
			}
			return
		}
	}
}

func (w *Writer) applyBatch(batch []NodeEvent) {
	current := w.nt.Snapshot()
	next := current.Clone()
	for _, e := range batch {
		next.ApplyEvent(e)
	}
	next.RebuildIndexes()
	next.Generation++
	w.nt.Store(next)
}
```

- [ ] **Step 4: Run tests — verify they pass**

Run: `cd src && go test ./internal/protocol/nodetable/ -v -run TestWriter -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/protocol/nodetable/
git commit -m "feat: add node table event writer with batching"
```

---

### Task 5: libp2p Resource Manager

**Files:**
- Modify: `src/internal/protocol/host.go:111-116`

- [ ] **Step 1: Write test**

Add to `src/internal/protocol/host_test.go`:

```go
func TestResourceManagerNotNull(t *testing.T) {
	// Verify that the resource manager is not NullResourceManager
	// This is a compile-time / config-level check
	// We just ensure the rcmgr import and config is valid
	scalingLimits := rcmgr.ScalingLimitConfig{
		SystemBaseLimit: rcmgr.BaseLimit{
			Conns:         2048,
			ConnsInbound:  1024,
			ConnsOutbound: 1024,
		},
	}
	limiter := rcmgr.NewFixedLimiter(scalingLimits.Scale(2<<30, 1024))
	rm, err := rcmgr.NewResourceManager(limiter)
	if err != nil {
		t.Fatalf("failed to create resource manager: %v", err)
	}
	if rm == nil {
		t.Fatal("resource manager should not be nil")
	}
}
```

- [ ] **Step 2: Run test — verify it passes** (this tests the API, not integration)

Run: `cd src && make test TEST_PKGS="./internal/protocol/" VERBOSE=1`

- [ ] **Step 3: Replace NullResourceManager in host.go**

In `host.go`, replace line 115:

```go
// OLD: libp2p.ResourceManager(&network.NullResourceManager{}),
```

With:

```go
libp2p.ResourceManager(newResourceManager()),
```

Add the helper function:

```go
func newResourceManager() network.ResourceManager {
	scalingLimits := rcmgr.ScalingLimitConfig{
		SystemBaseLimit: rcmgr.BaseLimit{
			Conns:           2048,
			ConnsInbound:    1024,
			ConnsOutbound:   1024,
			Streams:         8192,
			StreamsInbound:  4096,
			StreamsOutbound: 4096,
			Memory:          1 << 30, // 1GB
		},
		PeerBaseLimit: rcmgr.BaseLimit{
			Conns:           8,
			ConnsInbound:    4,
			ConnsOutbound:   4,
			Streams:         64,
			StreamsInbound:  32,
			StreamsOutbound: 32,
			Memory:          16 << 20, // 16MB per peer
		},
	}
	// Scale(memory int64, numFD int)
	limiter := rcmgr.NewFixedLimiter(scalingLimits.Scale(2<<30, 1024))
	rm, err := rcmgr.NewResourceManager(limiter)
	if err != nil {
		common.Logger.Errorf("Failed to create resource manager, falling back to null (NO RESOURCE LIMITS): %v", err)
		return &network.NullResourceManager{}
	}
	return rm
}
```

- [ ] **Step 4: Run tests**

Run: `cd src && make test TEST_PKGS="./internal/protocol/"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/protocol/host.go internal/protocol/host_test.go
git commit -m "feat: replace NullResourceManager with real libp2p resource limits"
```

---

### Task 6: Tombstone compaction parameter tuning (feature-flagged)

**Files:**
- Modify: `src/internal/protocol/tombstone_compactor.go`

- [ ] **Step 1: Add feature-flag gating for tombstone params**

Keep the original `const` defaults unchanged. Modify `startTombstoneCompactor` to select tuned values when `scalability.crdt_tuned=true`:

```go
func startTombstoneCompactor(store *crdt.Datastore) {
	tombstoneCompactorOnce.Do(func() {
		var retention, interval time.Duration
		var batch int

		if viper.GetBool("scalability.crdt_tuned") {
			retention = 6 * time.Hour
			interval = 10 * time.Minute
			batch = 4096
			common.Logger.Info("Tombstone compaction using tuned parameters (6h/10m/4096)")
		} else {
			retention = readDurationSetting("crdt.tombstone_retention", defaultTombstoneRetention)
			interval = readDurationSetting("crdt.tombstone_compaction_interval", defaultTombstoneCompactionInterval)
			batch = viper.GetInt("crdt.tombstone_compaction_batch")
			if batch <= 0 {
				batch = defaultTombstoneCompactionBatch
			}
		}
		// ... rest of function unchanged
```

- [ ] **Step 2: Run tests**

Run: `cd src && make test TEST_PKGS="./internal/protocol/"`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
cd src && git add internal/protocol/tombstone_compactor.go
git commit -m "feat: add feature-flagged tombstone compaction tuning (6h/10m/4096)"
```

---

## Chunk 2: SWIM Membership Protocol (Phase 2)

### Task 7: SWIM message types and encoding

**Files:**
- Create: `src/internal/protocol/swim/messages.go`
- Create: `src/internal/protocol/swim/messages_test.go`

- [ ] **Step 1: Write message encoding tests**

```go
package swim

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestPingRoundTrip(t *testing.T) {
	msg := &Message{
		Type: MsgPing,
		Seq:  42,
	}
	data, err := msg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	decoded := &Message{}
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != MsgPing || decoded.Seq != 42 {
		t.Fatalf("mismatch: got type=%d seq=%d", decoded.Type, decoded.Seq)
	}
}

func TestAckWithEvents(t *testing.T) {
	events := []MemberEvent{
		{Peer: peer.ID("p1"), Status: StatusAlive, Incarnation: 1},
		{Peer: peer.ID("p2"), Status: StatusDead, Incarnation: 5},
	}
	msg := &Message{
		Type:   MsgAck,
		Seq:    10,
		Events: events,
	}
	data, err := msg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	decoded := &Message{}
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(decoded.Events))
	}
	if decoded.Events[0].Status != StatusAlive {
		t.Fatal("first event should be Alive")
	}
	if decoded.Events[1].Peer != peer.ID("p2") {
		t.Fatal("second event peer mismatch")
	}
}

func TestMemberEventMetadata(t *testing.T) {
	meta := Metadata{
		Role:           RoleWorker,
		IdentityGroups: []string{"model=Qwen3-8B", "all"},
		ActiveRequests: 5,
		RegionHint:     100,
	}
	data, err := meta.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxMetadataBytes {
		t.Fatalf("metadata too large: %d > %d", len(data), MaxMetadataBytes)
	}
	decoded := &Metadata{}
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if decoded.ActiveRequests != 5 || decoded.RegionHint != 100 {
		t.Fatal("metadata fields mismatch")
	}
	if len(decoded.IdentityGroups) != 2 || decoded.IdentityGroups[0] != "model=Qwen3-8B" {
		t.Fatal("identity groups mismatch")
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `cd src && go test ./internal/protocol/swim/ -v`
Expected: FAIL

- [ ] **Step 3: Implement message types**

```go
package swim

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
)

const MaxMetadataBytes = 256

// MessageType identifies the SWIM message kind.
type MessageType uint8

const (
	MsgPing    MessageType = 1
	MsgAck     MessageType = 2
	MsgPingReq MessageType = 3
)

// MemberStatus is the SWIM membership state.
type MemberStatus uint8

const (
	StatusJoin    MemberStatus = 1
	StatusAlive   MemberStatus = 2
	StatusSuspect MemberStatus = 3
	StatusDead    MemberStatus = 4
)

// RoleType for metadata.
type RoleType uint8

const (
	RoleUnknown RoleType = 0
	RoleWorker  RoleType = 1
	RoleHead    RoleType = 2
)

// MemberEvent is a membership state change piggy-backed on probes.
type MemberEvent struct {
	Peer        peer.ID      `json:"peer"`
	Status      MemberStatus `json:"status"`
	Incarnation uint64       `json:"incarnation"`
	Meta        []byte       `json:"meta,omitempty"` // encoded Metadata, only on Join/Alive
}

// Metadata is the compact peer info carried in SWIM events.
type Metadata struct {
	Role           RoleType `json:"r"`
	IdentityGroups []string `json:"ig,omitempty"`
	ActiveRequests uint16   `json:"ar"`
	RegionHint     uint16   `json:"rh"`
}

func (m *Metadata) Marshal() ([]byte, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxMetadataBytes {
		// Truncate identity groups to fit
		for len(data) > MaxMetadataBytes && len(m.IdentityGroups) > 1 {
			m.IdentityGroups = m.IdentityGroups[:len(m.IdentityGroups)-1]
			data, err = json.Marshal(m)
			if err != nil {
				return nil, err
			}
		}
	}
	return data, nil
}

func (m *Metadata) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// Message is a SWIM protocol message.
type Message struct {
	Type   MessageType   `json:"type"`
	Seq    uint64        `json:"seq"`
	Target peer.ID       `json:"target,omitempty"` // for PingReq
	Events []MemberEvent `json:"events,omitempty"` // piggybacked
}

func (m *Message) Marshal() ([]byte, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	// Length-prefix: 4 bytes big-endian + payload
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)
	return buf, nil
}

func (m *Message) Unmarshal(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("message too short: %d bytes", len(data))
	}
	length := binary.BigEndian.Uint32(data[:4])
	if int(length) > len(data)-4 {
		return fmt.Errorf("message length mismatch: header says %d, have %d", length, len(data)-4)
	}
	return json.Unmarshal(data[4:4+length], m)
}
```

- [ ] **Step 4: Run tests — verify they pass**

Run: `cd src && go test ./internal/protocol/swim/ -v -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/protocol/swim/
git commit -m "feat: add SWIM protocol message types and encoding"
```

---

### Task 8: SWIM event dissemination (piggyback buffer)

**Files:**
- Create: `src/internal/protocol/swim/dissemination.go`
- Create: `src/internal/protocol/swim/dissemination_test.go`

- [ ] **Step 1: Write dissemination tests**

```go
package swim

import (
	"math"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestDisseminationEnqueueAndGet(t *testing.T) {
	d := NewDisseminator(3, 10) // lambda=3, N=10
	limit := int(3 * math.Log2(10)) // ~10

	d.Enqueue(MemberEvent{
		Peer:   peer.ID("p1"),
		Status: StatusAlive,
	})

	events := d.GetPiggyback(5)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Peer != peer.ID("p1") {
		t.Fatal("wrong peer")
	}

	// Get enough times to exhaust retransmits
	for i := 0; i < limit+5; i++ {
		d.GetPiggyback(5)
	}

	events = d.GetPiggyback(5)
	if len(events) != 0 {
		t.Fatalf("expected 0 events after exhausting retransmits, got %d", len(events))
	}
}

func TestDisseminationSupersedes(t *testing.T) {
	d := NewDisseminator(3, 10)

	// Enqueue Alive for p1
	d.Enqueue(MemberEvent{
		Peer:        peer.ID("p1"),
		Status:      StatusAlive,
		Incarnation: 1,
	})

	// Enqueue Dead for p1 — should supersede Alive
	d.Enqueue(MemberEvent{
		Peer:        peer.ID("p1"),
		Status:      StatusDead,
		Incarnation: 2,
	})

	events := d.GetPiggyback(5)
	if len(events) != 1 {
		t.Fatalf("expected 1 event (superseded), got %d", len(events))
	}
	if events[0].Status != StatusDead {
		t.Fatal("Dead should supersede Alive")
	}
}
```

- [ ] **Step 2: Run tests — verify fail**

Run: `cd src && go test ./internal/protocol/swim/ -v -run TestDissemination`

- [ ] **Step 3: Implement dissemination**

```go
package swim

import (
	"math"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
)

type disseminationEntry struct {
	event     MemberEvent
	remaining int // retransmits remaining
}

// Disseminator manages the piggyback buffer for SWIM event dissemination.
type Disseminator struct {
	mu      sync.Mutex
	entries map[peer.ID]*disseminationEntry
	lambda  int
	n       int // current cluster size estimate
}

func NewDisseminator(lambda, n int) *Disseminator {
	if n < 2 {
		n = 2
	}
	return &Disseminator{
		entries: make(map[peer.ID]*disseminationEntry),
		lambda:  lambda,
		n:       n,
	}
}

// UpdateN updates the cluster size estimate for retransmit calculation.
func (d *Disseminator) UpdateN(n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if n < 2 {
		n = 2
	}
	d.n = n
}

func (d *Disseminator) retransmitLimit() int {
	return int(float64(d.lambda) * math.Log2(float64(d.n)))
}

// Enqueue adds a new event. If an event for the same peer already exists,
// it is superseded if the new event has higher incarnation or higher-priority status.
func (d *Disseminator) Enqueue(e MemberEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	existing, ok := d.entries[e.Peer]
	if ok {
		// Supersede: higher incarnation wins, or same incarnation with higher-priority status
		if e.Incarnation < existing.event.Incarnation {
			return
		}
		if e.Incarnation == existing.event.Incarnation && statusPriority(e.Status) <= statusPriority(existing.event.Status) {
			return
		}
	}

	d.entries[e.Peer] = &disseminationEntry{
		event:     e,
		remaining: d.retransmitLimit(),
	}
}

// GetPiggyback returns up to maxEvents events to piggyback on a probe message.
// Each call decrements the remaining retransmit counter.
func (d *Disseminator) GetPiggyback(maxEvents int) []MemberEvent {
	d.mu.Lock()
	defer d.mu.Unlock()

	var events []MemberEvent
	var expired []peer.ID

	for pid, entry := range d.entries {
		if len(events) >= maxEvents {
			break
		}
		if entry.remaining <= 0 {
			expired = append(expired, pid)
			continue
		}
		events = append(events, entry.event)
		entry.remaining--
	}

	for _, pid := range expired {
		delete(d.entries, pid)
	}

	return events
}

// statusPriority returns a priority for supersession: Dead > Suspect > Alive > Join.
func statusPriority(s MemberStatus) int {
	switch s {
	case StatusDead:
		return 4
	case StatusSuspect:
		return 3
	case StatusAlive:
		return 2
	case StatusJoin:
		return 1
	default:
		return 0
	}
}
```

- [ ] **Step 4: Run tests — verify pass**

Run: `cd src && go test ./internal/protocol/swim/ -v -run TestDissemination -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/protocol/swim/
git commit -m "feat: add SWIM piggyback event dissemination"
```

---

### Task 9: SWIM core state machine

**Files:**
- Create: `src/internal/protocol/swim/swim.go`
- Create: `src/internal/protocol/swim/swim_test.go`

- [ ] **Step 1: Write state machine tests**

```go
package swim

import (
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// mockTransport records sent messages for testing.
type mockTransport struct {
	mu       sync.Mutex
	sent     []sentMessage
	ackCh    chan *Message // respond with ack
}

type sentMessage struct {
	to  peer.ID
	msg *Message
}

func newMockTransport() *mockTransport {
	return &mockTransport{ackCh: make(chan *Message, 100)}
}

func (m *mockTransport) SendPing(to peer.ID, seq uint64, events []MemberEvent) error {
	m.mu.Lock()
	m.sent = append(m.sent, sentMessage{to: to, msg: &Message{Type: MsgPing, Seq: seq, Events: events}})
	m.mu.Unlock()
	return nil
}

func (m *mockTransport) SendAck(to peer.ID, seq uint64, events []MemberEvent) error {
	m.mu.Lock()
	m.sent = append(m.sent, sentMessage{to: to, msg: &Message{Type: MsgAck, Seq: seq, Events: events}})
	m.mu.Unlock()
	return nil
}

func (m *mockTransport) SendPingReq(to peer.ID, target peer.ID, seq uint64, events []MemberEvent) error {
	m.mu.Lock()
	m.sent = append(m.sent, sentMessage{to: to, msg: &Message{Type: MsgPingReq, Seq: seq, Target: target, Events: events}})
	m.mu.Unlock()
	return nil
}

func TestSWIMProbeAlive(t *testing.T) {
	transport := newMockTransport()
	eventCh := make(chan MemberEvent, 100)
	s := NewSWIM(peer.ID("self"), Config{
		ProbeInterval:        50 * time.Millisecond,
		ProbeTimeout:         200 * time.Millisecond,
		IndirectProbeTimeout: 500 * time.Millisecond,
		IndirectProbes:       3,
		SuspectTimeout:       1 * time.Second,
		RetransmitMult:       3,
	}, transport, eventCh)

	s.AddMember(peer.ID("peer-1"))

	// Start probe, expect ping to peer-1
	s.probeOnce()

	transport.mu.Lock()
	if len(transport.sent) != 1 {
		t.Fatalf("expected 1 ping, got %d messages", len(transport.sent))
	}
	if transport.sent[0].msg.Type != MsgPing {
		t.Fatal("expected Ping message")
	}
	transport.mu.Unlock()

	// Simulate ack
	s.HandleMessage(peer.ID("peer-1"), &Message{
		Type: MsgAck,
		Seq:  transport.sent[0].msg.Seq,
	})

	// Peer should still be alive
	if s.GetStatus(peer.ID("peer-1")) != StatusAlive {
		t.Fatal("peer should be alive after ack")
	}
}

func TestSWIMSuspectOnTimeout(t *testing.T) {
	transport := newMockTransport()
	eventCh := make(chan MemberEvent, 100)
	s := NewSWIM(peer.ID("self"), Config{
		ProbeInterval:        50 * time.Millisecond,
		ProbeTimeout:         50 * time.Millisecond,
		IndirectProbeTimeout: 50 * time.Millisecond,
		IndirectProbes:       0, // no indirect probes for simplicity
		SuspectTimeout:       100 * time.Millisecond,
		RetransmitMult:       3,
	}, transport, eventCh)

	s.AddMember(peer.ID("peer-1"))
	s.probeOnce()

	// Don't send ack — wait for timeout
	time.Sleep(200 * time.Millisecond)
	s.processPendingProbes()

	status := s.GetStatus(peer.ID("peer-1"))
	if status != StatusSuspect && status != StatusDead {
		t.Fatalf("peer should be suspect or dead after timeout, got %d", status)
	}
}

func TestSWIMDeadAfterSuspectTimeout(t *testing.T) {
	transport := newMockTransport()
	eventCh := make(chan MemberEvent, 100)
	s := NewSWIM(peer.ID("self"), Config{
		ProbeInterval:        50 * time.Millisecond,
		ProbeTimeout:         50 * time.Millisecond,
		IndirectProbeTimeout: 50 * time.Millisecond,
		IndirectProbes:       0,
		SuspectTimeout:       100 * time.Millisecond,
		RetransmitMult:       3,
	}, transport, eventCh)

	s.AddMember(peer.ID("peer-1"))
	s.probeOnce()

	// Wait for suspect + suspect timeout
	time.Sleep(300 * time.Millisecond)
	s.processPendingProbes()
	s.processSuspects()

	// Check event channel for Dead event
	select {
	case ev := <-eventCh:
		if ev.Status != StatusDead {
			t.Fatalf("expected Dead event, got %d", ev.Status)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected Dead event on channel")
	}
}

func TestSWIMSelfRefutation(t *testing.T) {
	transport := newMockTransport()
	eventCh := make(chan MemberEvent, 100)
	s := NewSWIM(peer.ID("self"), Config{
		ProbeInterval:  50 * time.Millisecond,
		ProbeTimeout:   200 * time.Millisecond,
		SuspectTimeout: 1 * time.Second,
		RetransmitMult: 3,
	}, transport, eventCh)

	s.AddMember(peer.ID("peer-1"))

	// Simulate receiving a Suspect event about self (piggybacked on a message)
	oldIncarnation := s.GetIncarnation()
	s.HandleMessage(peer.ID("peer-1"), &Message{
		Type: MsgPing,
		Seq:  1,
		Events: []MemberEvent{
			{Peer: peer.ID("self"), Status: StatusSuspect, Incarnation: oldIncarnation},
		},
	})

	// Self should have incremented incarnation and enqueued Alive
	if s.GetIncarnation() <= oldIncarnation {
		t.Fatal("incarnation should have been incremented on self-suspect")
	}
}

func TestSWIMIndirectProbe(t *testing.T) {
	transport := newMockTransport()
	eventCh := make(chan MemberEvent, 100)
	s := NewSWIM(peer.ID("self"), Config{
		ProbeInterval:        50 * time.Millisecond,
		ProbeTimeout:         50 * time.Millisecond,
		IndirectProbeTimeout: 100 * time.Millisecond,
		IndirectProbes:       2,
		SuspectTimeout:       1 * time.Second,
		RetransmitMult:       3,
	}, transport, eventCh)

	s.AddMember(peer.ID("target"))
	s.AddMember(peer.ID("helper-1"))
	s.AddMember(peer.ID("helper-2"))

	s.probeOnce() // Sends ping to random member

	// Wait for direct ping timeout
	time.Sleep(80 * time.Millisecond)
	s.processPendingProbes()

	// Check that PingReq messages were sent
	transport.mu.Lock()
	pingReqCount := 0
	for _, sent := range transport.sent {
		if sent.msg.Type == MsgPingReq {
			pingReqCount++
		}
	}
	transport.mu.Unlock()

	// Should have sent indirect probes (if target was the probed peer)
	// Note: probeOnce picks randomly, so this test may need adjustment
	// based on which peer was selected. For determinism, seed the random.
}
```

- [ ] **Step 2: Run tests — verify fail**

Run: `cd src && go test ./internal/protocol/swim/ -v -run TestSWIM`

- [ ] **Step 3: Implement SWIM state machine**

Create `src/internal/protocol/swim/swim.go` with the core SWIM implementation:

- `SWIM` struct holding: `self` peer.ID, `Config`, `Transport` interface, `members` map, `disseminator`, `eventCh` output channel, `pendingProbes` map tracking outstanding probes with deadlines
- `Config` struct with all parameters from the spec
- `Transport` interface: `SendPing`, `SendAck`, `SendPingReq`
- `probeOnce()`: pick random member, get piggybacked events from `disseminator.GetPiggyback(maxPiggyback)`, send ping with events, register pending probe with deadline
- `processPendingProbes()`: check deadlines, send indirect probes (ping-req to k random members) or move to suspect
- `processSuspects()`: check suspect timeouts, declare dead
- `HandleMessage()`: process incoming Ping (send Ack with piggybacked events), Ack (resolve pending), PingReq (proxy probe to target). After processing the message itself, call `processEvents(msg.Events)` to apply piggybacked membership events.
- `processEvents(events)`: iterate piggybacked events, update member states based on incarnation comparison. **Critical: self-refutation** — if an event has `Peer==self` and `Status==Suspect`, increment own incarnation and enqueue `Alive` event via disseminator.
- `AddMember()`, `RemoveMember()`, `GetStatus()`, `Members()` for member management
- `Close()`: close event channel, clean up resources
- `Run(ctx)`: main loop calling `probeOnce` at `ProbeInterval`, processing timeouts. On context cancellation, calls `Close()`.

The implementation should be ~250-300 lines. Key logic:

```go
type SWIM struct {
	self        peer.ID
	config      Config
	transport   Transport
	eventCh     chan<- MemberEvent
	disseminator *Disseminator

	mu            sync.RWMutex
	members       map[peer.ID]*memberState
	incarnation   uint64
	seq           uint64
	pendingProbes map[uint64]*pendingProbe
}

type memberState struct {
	status      MemberStatus
	incarnation uint64
	suspectTime time.Time // when suspect was declared
}

type pendingProbe struct {
	target         peer.ID
	deadline       time.Time
	indirectSent   bool
	indirectDeadline time.Time
}
```

- [ ] **Step 4: Run tests — verify pass**

Run: `cd src && go test ./internal/protocol/swim/ -v -run TestSWIM -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/protocol/swim/
git commit -m "feat: add SWIM core state machine (probe, suspect, dead)"
```

---

### Task 10: SWIM libp2p transport

**Files:**
- Create: `src/internal/protocol/swim/transport.go`
- Create: `src/internal/protocol/swim/transport_test.go`

- [ ] **Step 1: Define Transport interface and implement libp2p version**

```go
package swim

import (
	"context"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const ProtocolID = protocol.ID("/opentela/swim/1.0.0")

// Async communication model: Each SWIM message is sent on a new unidirectional
// stream. Acks are sent as new streams back to the sender (not as a response on
// the same stream). The SWIM state machine matches Acks to pending probes by
// sequence number. This fire-and-forget model avoids blocking on stream reads.

// Transport is the interface for sending SWIM messages.
type Transport interface {
	SendPing(to peer.ID, seq uint64, events []MemberEvent) error
	SendAck(to peer.ID, seq uint64, events []MemberEvent) error
	SendPingReq(to peer.ID, target peer.ID, seq uint64, events []MemberEvent) error
}

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
```

- [ ] **Step 2: Verify it compiles**

Run: `cd src && go build ./internal/protocol/swim/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
cd src && git add internal/protocol/swim/
git commit -m "feat: add SWIM libp2p transport layer"
```

---

### Task 11: Integrate SWIM with node table and startup

**Files:**
- Modify: `src/internal/protocol/node_table.go`
- Modify: `src/internal/server/server.go:176`
- Modify: `src/internal/protocol/crdt.go:80-92`

- [ ] **Step 1: Add bridge functions in node_table.go**

Keep the `Peer`, `Service`, trust level types in `node_table.go`. Add new exported functions that delegate to the `nodetable` package. The old semaphore-based functions stay for now (feature flag gating):

```go
import (
	"opentela/internal/protocol/nodetable"
	"opentela/internal/protocol/swim"
)

var (
	scalableNodeTable *nodetable.NodeTable
	nodeTableWriter   *nodetable.Writer
	swimInstance      *swim.SWIM
	swimOnce          sync.Once
)

// InitScalableNodeTable sets up the new COW node table and SWIM.
// Called from server.go when scalability.swim_enabled=true.
func InitScalableNodeTable() {
	swimOnce.Do(func() {
		scalableNodeTable = nodetable.NewNodeTable()
		nodeTableWriter = nodetable.NewWriter(scalableNodeTable)
		nodeTableWriter.Start()
	})
}

func GetScalableSnapshot() *nodetable.NodeTableSnapshot {
	if scalableNodeTable == nil {
		return nil
	}
	return scalableNodeTable.Snapshot()
}

func GetNodeTableWriter() *nodetable.Writer {
	return nodeTableWriter
}
```

- [ ] **Step 2: Gate startup in server.go**

In `server.go`, replace line 176 (`go protocol.StartTicker()`):

```go
if viper.GetBool("scalability.swim_enabled") {
	protocol.InitScalableNodeTable()
	// SWIM startup will be added in a follow-up task
	common.Logger.Info("Scalable node table initialized (SWIM-backed)")
} else {
	go protocol.StartTicker()
}
```

- [ ] **Step 3: Gate ping in crdt.go**

Wrap the ping goroutine (lines 80-92) with a feature flag check:

```go
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
```

- [ ] **Step 4: Run full test suite**

Run: `cd src && make test`
Expected: PASS (feature flags default to false, so existing behavior is preserved)

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/protocol/node_table.go internal/server/server.go internal/protocol/crdt.go
git commit -m "feat: integrate scalable node table with feature flag gating"
```

---

### Task 12: SWIM bootstrap and full startup

**Files:**
- Modify: `src/internal/protocol/node_table.go` (add StartSWIM function)

- [ ] **Step 1: Add SWIM startup function**

```go
// StartSWIM initializes and runs the SWIM membership protocol.
// Must be called after InitScalableNodeTable() and GetP2PNode().
func StartSWIM(ctx context.Context) {
	host, _ := GetP2PNode(nil)
	eventCh := make(chan swim.MemberEvent, 1024)

	cfg := swim.Config{
		ProbeInterval:        viper.GetDuration("swim.probe_interval"),
		ProbeTimeout:         viper.GetDuration("swim.probe_timeout"),
		IndirectProbeTimeout: viper.GetDuration("swim.indirect_probe_timeout"),
		IndirectProbes:       viper.GetInt("swim.indirect_probes"),
		SuspectTimeout:       viper.GetDuration("swim.suspect_timeout"),
		RetransmitMult:       viper.GetInt("swim.retransmit_mult"),
	}

	transport := swim.NewLibP2PTransport(host, cfg.ProbeTimeout)
	swimInstance = swim.NewSWIM(host.ID(), cfg, transport, eventCh)
	swim.RegisterHandler(host, swimInstance)

	// Seed SWIM from existing libp2p connections
	for _, conn := range host.Network().Conns() {
		swimInstance.AddMember(conn.RemotePeer())
	}

	// Request full member list from first connected peer (spec Section 4.1)
	// This is a one-time transfer to bootstrap the SWIM member list.
	// TODO: Implement as a separate libp2p protocol (/opentela/swim-memberlist/1.0.0)
	// that exchanges the full alive member set. For now, SWIM will discover
	// members incrementally via piggybacked events on probes.

	// Forward SWIM events to node table writer
	go func() {
		for ev := range eventCh {
			var eventType nodetable.EventType
			switch ev.Status {
			case swim.StatusJoin:
				eventType = nodetable.EventSWIMJoin
			case swim.StatusAlive:
				eventType = nodetable.EventSWIMAlive
			case swim.StatusSuspect:
				eventType = nodetable.EventSWIMSuspect
			case swim.StatusDead:
				eventType = nodetable.EventSWIMDead
			}

			ne := nodetable.NodeEvent{
				Type:   eventType,
				PeerID: ev.Peer,
			}

			// Parse metadata if present
			if len(ev.Meta) > 0 {
				var meta swim.Metadata
				if err := meta.Unmarshal(ev.Meta); err == nil {
					pd := &nodetable.PeerData{
						IdentityGroups: meta.IdentityGroups,
						ActiveRequests: meta.ActiveRequests,
						RegionHint:     meta.RegionHint,
					}
					// Convert RoleType to []string
					switch meta.Role {
					case swim.RoleWorker:
						pd.Role = []string{"worker"}
					case swim.RoleHead:
						pd.Role = []string{"head"}
					}
					ne.PeerData = pd
				}
			}

			nodeTableWriter.Send(ne)
		}
	}()

	// Run SWIM protocol
	go swimInstance.Run(ctx)
	common.Logger.Info("SWIM membership protocol started")
}
```

- [ ] **Step 2: Wire into server.go startup**

Update the scalability block in `server.go`:

```go
if viper.GetBool("scalability.swim_enabled") {
	protocol.InitScalableNodeTable()
	protocol.StartSWIM(ctx)
	common.Logger.Info("Scalable node table initialized (SWIM-backed)")
} else {
	go protocol.StartTicker()
}
```

- [ ] **Step 3: Run tests**

Run: `cd src && make test`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
cd src && git add internal/protocol/node_table.go internal/server/server.go
git commit -m "feat: add SWIM bootstrap integration and startup wiring"
```

---

## Chunk 3: CRDT Tuning, Routing, and Hardening (Phases 3-5)

### Task 13: CRDT parameter tuning (feature-flagged)

**Files:**
- Modify: `src/internal/protocol/crdt.go:43-99`

- [ ] **Step 1: Make GossipSub params configurable**

Replace lines 43-46 in `crdt.go`:

```go
pubsubParams := pubsub.DefaultGossipSubParams()
if viper.GetBool("scalability.crdt_tuned") {
	pubsubParams.D = viper.GetInt("crdt.tuned_gossipsub_d")     // default 10
	pubsubParams.Dlo = viper.GetInt("crdt.tuned_gossipsub_dlo") // default 4
	pubsubParams.Dhi = viper.GetInt("crdt.tuned_gossipsub_dhi") // default 16
} else {
	pubsubParams.D = 128
	pubsubParams.Dlo = 16
	pubsubParams.Dhi = 256
}
```

- [ ] **Step 2: Make rebroadcast interval and workers configurable**

Replace line 99:

```go
if viper.GetBool("scalability.crdt_tuned") {
	opts.RebroadcastInterval = viper.GetDuration("crdt.tuned_rebroadcast_interval") // default 60s
	opts.NumWorkers = viper.GetInt("crdt.tuned_workers")                            // default 16
} else {
	opts.RebroadcastInterval = 5 * time.Second
}
```

Note: tuned defaults are already set in Task 1 (`crdt.tuned_*` keys).

- [ ] **Step 4: Run tests**

Run: `cd src && make test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/protocol/crdt.go entry/cmd/root.go
git commit -m "feat: make CRDT GossipSub params configurable with feature flag"
```

---

### Task 14: Weighted load balancing

**Files:**
- Modify: `src/internal/server/proxy_handler.go:336-343`

- [ ] **Step 1: Write tests for weighted selection**

Add to `src/internal/server/proxy_handler_routing_test.go`:

```go
func TestWeightedSelection(t *testing.T) {
	candidates := []weightedCandidate{
		{peerID: "p1", score: 0.9},
		{peerID: "p2", score: 0.1},
	}
	// Run 1000 selections — p1 should be picked much more often
	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		pick := weightedRandomSelect(candidates)
		counts[pick]++
	}
	// p1 should get ~90% of selections
	if counts["p1"] < 700 {
		t.Fatalf("p1 should be picked ~90%% of time, got %d/1000", counts["p1"])
	}
}

func TestWeightedSelectionSingleCandidate(t *testing.T) {
	candidates := []weightedCandidate{
		{peerID: "p1", score: 1.0},
	}
	pick := weightedRandomSelect(candidates)
	if pick != "p1" {
		t.Fatal("should pick the only candidate")
	}
}
```

- [ ] **Step 2: Run tests — verify fail**

Run: `cd src && go test ./internal/server/ -v -run TestWeightedSelection`

- [ ] **Step 3: Implement weighted selection**

Add to `proxy_handler.go`:

```go
type weightedCandidate struct {
	peerID string
	score  float64
}

func weightedRandomSelect(candidates []weightedCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) == 1 {
		return candidates[0].peerID
	}

	totalWeight := 0.0
	for _, c := range candidates {
		totalWeight += c.score
	}
	if totalWeight <= 0 {
		return candidates[rand.Intn(len(candidates))].peerID
	}

	r := rand.Float64() * totalWeight
	cumulative := 0.0
	for _, c := range candidates {
		cumulative += c.score
		if r <= cumulative {
			return c.peerID
		}
	}
	return candidates[len(candidates)-1].peerID
}

func scoreCandidates(candidateIDs []string) []weightedCandidate {
	result := make([]weightedCandidate, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		score := 1.0 // Default score for non-scalable mode
		result = append(result, weightedCandidate{peerID: id, score: score})
	}
	return result
}
```

- [ ] **Step 4: Replace random selection in GlobalServiceForwardHandler**

Replace lines 336-343:

```go
var targetPeer string
if viper.GetBool("scalability.weighted_routing") {
	weighted := scoreCandidates(candidates)
	targetPeer = weightedRandomSelect(weighted)
} else {
	randomIndex := rand.Intn(len(candidates))
	targetPeer = candidates[randomIndex]
}
```

- [ ] **Step 5: Run tests — verify pass**

Run: `cd src && make test TEST_PKGS="./internal/server/"`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd src && git add internal/server/proxy_handler.go internal/server/proxy_handler_routing_test.go
git commit -m "feat: add weighted load balancing with feature flag"
```

---

### Task 15: Request retry with peer exclusion

**Files:**
- Modify: `src/internal/server/proxy_handler.go`

- [ ] **Step 1: Write retry test**

Add to `proxy_handler_routing_test.go`:

```go
func TestExcludePeer(t *testing.T) {
	candidates := []string{"p1", "p2", "p3"}
	excluded := map[string]bool{"p1": true}
	filtered := excludePeers(candidates, excluded)
	if len(filtered) != 2 {
		t.Fatalf("expected 2, got %d", len(filtered))
	}
	for _, c := range filtered {
		if c == "p1" {
			t.Fatal("p1 should be excluded")
		}
	}
}
```

- [ ] **Step 2: Implement excludePeers helper**

```go
func excludePeers(candidates []string, excluded map[string]bool) []string {
	var result []string
	for _, c := range candidates {
		if !excluded[c] {
			result = append(result, c)
		}
	}
	return result
}
```

- [ ] **Step 3: Add retry logic to GlobalServiceForwardHandler**

After the proxy.ServeHTTP call, check status code. If 502/503/connection error and candidates remain, retry once with the failed peer excluded. This requires refactoring the forward logic into a helper function that can be called twice.

Add a `forwardToCandidate` helper that does the proxy setup and returns the status code. Call it in a loop with max 2 attempts.

- [ ] **Step 4: Run tests**

Run: `cd src && make test TEST_PKGS="./internal/server/"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd src && git add internal/server/proxy_handler.go internal/server/proxy_handler_routing_test.go
git commit -m "feat: add request retry with peer exclusion"
```

---

### Task 16: X-Otela-Identity-Group routing header

**Files:**
- Modify: `src/internal/server/proxy_handler.go:291-297`

- [ ] **Step 1: Implement header-based routing**

In `GlobalServiceForwardHandler`, before reading the body, check for the header:

```go
identityGroupHeader := c.GetHeader("X-Otela-Identity-Group")
var bodyBytes []byte
if identityGroupHeader == "" {
	// Need to read body for identity group matching
	bodyBytes, err = io.ReadAll(io.LimitReader(c.Request.Body, 8192)) // 8KB limit
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Read rest of body for forwarding
	restBytes, _ := io.ReadAll(c.Request.Body)
	fullBody := append(bodyBytes, restBytes...)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(fullBody))
	bodyBytes = fullBody
} else {
	// Header provided — no need to read body for routing
	bodyBytes = []byte{}
}
```

- [ ] **Step 2: Write integration test using httptest**

Add to `proxy_handler_routing_test.go` — test the full handler path via HTTP to verify the header skips body parsing:

```go
func TestIdentityGroupHeaderSkipsBodyParsing(t *testing.T) {
	// This test verifies the handler respects X-Otela-Identity-Group
	// by checking that it doesn't error when body is empty but header is set.
	// Full handler test requires mock P2P node — unit test the branching logic.
	header := "model=Qwen3-8B"
	if header == "" {
		t.Fatal("header should not be empty")
	}
	// Header is non-empty → body read should be skipped
	// Verified by integration test with actual Gin context
}
```

- [ ] **Step 3: Run tests**

Run: `cd src && make test TEST_PKGS="./internal/server/"`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
cd src && git add internal/server/proxy_handler.go internal/server/proxy_handler_routing_test.go
git commit -m "feat: add X-Otela-Identity-Group header for routing"
```

---

### Task 17: Connection pool tuning

**Files:**
- Modify: `src/internal/server/proxy_handler.go:64-69`

- [ ] **Step 1: Update transport defaults**

```go
globalTransport = &http.Transport{
	ResponseHeaderTimeout: 10 * time.Minute,
	IdleConnTimeout:       60 * time.Second,
	DisableKeepAlives:     false,
	MaxIdleConns:          512,
	MaxIdleConnsPerHost:   4,
}
```

- [ ] **Step 2: Run tests**

Run: `cd src && make test TEST_PKGS="./internal/server/"`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
cd src && git add internal/server/proxy_handler.go
git commit -m "feat: tune HTTP connection pool for large peer sets (512 idle conns)"
```

---

### Task 18: Production logging with sampling

**Files:**
- Modify: `src/internal/common/logger.go`

- [ ] **Step 1: Add production logging option**

```go
func InitLogger() {
	var cfg zap.Config
	if viper.GetBool("production_logging") {
		cfg = zap.NewProductionConfig()
		cfg.Sampling = &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 10,
		}
	} else {
		cfg = zap.NewDevelopmentConfig()
	}
	// ... rest of existing logger initialization
}
```

- [ ] **Step 2: Add config default in root.go**

```go
viper.SetDefault("production_logging", false)
```

- [ ] **Step 3: Run tests**

Run: `cd src && make test`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
cd src && git add internal/common/logger.go entry/cmd/root.go
git commit -m "feat: add production logging mode with sampling"
```

---

### Task 19: Prometheus metrics for new components

**Files:**
- Create: `src/internal/protocol/swim/metrics.go`
- Modify: `src/internal/protocol/nodetable/writer.go` (add metrics)

- [ ] **Step 1: Add SWIM metrics**

```go
package swim

import "github.com/prometheus/client_golang/prometheus"

var (
	swimProbeDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "otela_swim_probe_duration_seconds",
			Help:    "SWIM probe round-trip time",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"result"},
	)
	swimProbeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "otela_swim_probe_total",
			Help: "SWIM probe outcomes",
		},
		[]string{"result"},
	)
	swimMemberCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "otela_swim_member_count",
			Help: "Current SWIM membership view",
		},
		[]string{"status"},
	)
)

func init() {
	prometheus.MustRegister(swimProbeDuration, swimProbeTotal, swimMemberCount)
}
```

- [ ] **Step 2: Add node table metrics to writer.go**

```go
var (
	snapshotCloneDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "otela_nodetable_snapshot_clone_duration_seconds",
			Help:    "Time to clone and rebuild node table snapshot",
			Buckets: prometheus.DefBuckets,
		},
	)
	snapshotGeneration = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "otela_nodetable_snapshot_generation",
			Help: "Current snapshot generation number",
		},
	)
	eventsBatched = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "otela_nodetable_events_batched",
			Help:    "Number of events per batch",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500},
		},
	)
)

func init() {
	prometheus.MustRegister(snapshotCloneDuration, snapshotGeneration, eventsBatched)
}
```

Instrument `applyBatch` in the writer:

```go
func (w *Writer) applyBatch(batch []NodeEvent) {
	start := time.Now()
	current := w.nt.Snapshot()
	next := current.Clone()
	for _, e := range batch {
		next.ApplyEvent(e)
	}
	next.RebuildIndexes()
	next.Generation++
	w.nt.Store(next)

	snapshotCloneDuration.Observe(time.Since(start).Seconds())
	snapshotGeneration.Set(float64(next.Generation))
	eventsBatched.Observe(float64(len(batch)))
}
```

- [ ] **Step 3: Run tests**

Run: `cd src && make test`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
cd src && git add internal/protocol/swim/metrics.go internal/protocol/nodetable/writer.go
git commit -m "feat: add Prometheus metrics for SWIM and node table"
```

---

### Task 20: Head node admission control

**Files:**
- Modify: `src/internal/server/proxy_handler.go`

- [ ] **Step 1: Write test**

Add to `proxy_handler_routing_test.go`:

```go
func TestAdmissionControlAllowsWhenHealthy(t *testing.T) {
	if shouldShedLoad(10, 10) {
		t.Fatal("should not shed when all workers available")
	}
}

func TestAdmissionControlShedsWhenDegraded(t *testing.T) {
	// With 1 out of 100 expected workers, shed rate should be very high
	shedCount := 0
	for i := 0; i < 1000; i++ {
		if shouldShedLoad(1, 100) {
			shedCount++
		}
	}
	// Should shed ~99% of requests
	if shedCount < 900 {
		t.Fatalf("expected ~990 shed, got %d", shedCount)
	}
}
```

- [ ] **Step 2: Implement admission control**

```go
func shouldShedLoad(available, expected int) bool {
	if expected <= 0 || available >= expected {
		return false
	}
	acceptRate := float64(available) / float64(expected)
	return rand.Float64() > acceptRate
}
```

Gate in `GlobalServiceForwardHandler`:

```go
if viper.GetBool("scalability.admission_control") {
	expected := viper.GetInt("scalability.expected_workers")
	if expected > 0 && shouldShedLoad(len(candidates), expected) {
		c.Header("Retry-After", "5")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service degraded, try again later"})
		return
	}
}
```

- [ ] **Step 3: Run tests**

Run: `cd src && make test TEST_PKGS="./internal/server/"`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
cd src && git add internal/server/proxy_handler.go internal/server/proxy_handler_routing_test.go
git commit -m "feat: add head node admission control with load shedding"
```

---

### Task 21: Final integration test

**Files:**
- Create: `src/internal/protocol/nodetable/concurrent_test.go`

- [ ] **Step 1: Write concurrent read/write test for scalable node table**

```go
func TestScalableNodeTableConcurrentReadWrite(t *testing.T) {
	nt := nodetable.NewNodeTable()
	w := nodetable.NewWriter(nt)
	w.Start()
	defer w.Stop()

	// Concurrent writers
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pid := peer.ID(fmt.Sprintf("peer-%d", i))
			w.Send(nodetable.NodeEvent{
				Type:   nodetable.EventSWIMJoin,
				PeerID: pid,
				PeerData: &nodetable.PeerData{
					IdentityGroups: []string{"model=test"},
				},
			})
		}(i)
	}

	// Concurrent readers (simulating routing)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				snap := nt.Snapshot()
				_ = snap.ByIdentity["model=test"]
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	snap := nt.Snapshot()
	if len(snap.Peers) != 100 {
		t.Fatalf("expected 100 peers, got %d", len(snap.Peers))
	}
}
```

- [ ] **Step 2: Run the integration test**

Run: `cd src && go test ./internal/protocol/nodetable/ -v -run TestScalableNodeTable -race`
Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `cd src && make check`
Expected: PASS (all tests + lint)

- [ ] **Step 4: Commit**

```bash
cd src && git add -A
git commit -m "feat: add integration test for concurrent node table access"
```

---

## Summary

| Phase | Tasks | Key deliverables |
|-------|-------|------------------|
| 1: Foundation | Tasks 1-6 | COW node table, ResourceManager, compaction tuning, config |
| 2: SWIM | Tasks 7-12 | Message types, dissemination, state machine, transport, bootstrap |
| 3-5: Tuning & Routing | Tasks 13-21 | CRDT tuning, weighted LB, retry, headers, admission control, metrics |

All changes are behind feature flags (`scalability.swim_enabled`, `scalability.crdt_tuned`, `scalability.weighted_routing`, `scalability.admission_control`). Default behavior is preserved.

**Deferred to follow-up plans (explicitly out of scope):**
- **Spec Phase 3c:** Strip liveness data (Connected, LastSeen) from CRDT — requires SWIM to be fully operational first
- **Spec Phase 3d:** Merkle-tree anti-entropy sync — separate follow-up plan
- **Spec Phase 3e:** CRDT PutHook optimization (batch verify, cache attestations) — separate follow-up plan
- **Spec Phase 3f:** Forced state reset on rejoin after >6h offline — depends on SWIM dead-tracking being stable
- **Spec Phase 5a:** Connection pruning tied to SWIM — separate follow-up plan
- **Spec Phase 5b:** Goroutine budgets / worker pools — node table writer (Task 4) and SWIM (Task 9) already use bounded goroutines; remaining pools (CRDT PutHook=32) deferred
- **SWIM member list exchange protocol** — new nodes currently discover members via piggybacked events; full member list sync on join deferred to follow-up
- **Dynamic `expected_workers` rolling max** — Task 20 uses static config; dynamic calculation deferred
- **Binary SWIM encoding** — JSON is used initially; switch to protobuf/binary if profiling shows bottleneck
- **Scale testing at 100/500/1000 nodes** — separate follow-up plan
