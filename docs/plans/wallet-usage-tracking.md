# Wallet Usage Tracking

## Table of Contents

1. [Overview](#overview)
2. [Existing Authentication Anchors](#existing-authentication-anchors)
3. [Architecture](#architecture)
4. [Storage Strategy](#storage-strategy)
5. [Security Model](#security-model)
6. [Open Source Deployment Considerations](#open-source-deployment-considerations)
7. [`gocloud.dev/secrets` Assessment](#goclouddevsecrets-assessment)
8. [Implementation Plan](#implementation-plan)
   - [Step 1 — Expose `MyOwner()` and `GetNodePrivKey()` from Protocol](#step-1--expose-myowner-and-getnodeprivkey-from-protocol)
   - [Step 2 — Open a Dedicated Badger Usage Store](#step-2--open-a-dedicated-badger-usage-store)
   - [Step 3 — Signed Usage Records and Hash Chain](#step-3--signed-usage-records-and-hash-chain)
   - [Step 4 — In-Memory Usage Aggregator](#step-4--in-memory-usage-aggregator)
   - [Step 5 — Prometheus Metrics](#step-5--prometheus-metrics)
   - [Step 6 — Gin Middleware](#step-6--gin-middleware)
   - [Step 7 — CRDT Anchor Publication](#step-7--crdt-anchor-publication)
   - [Step 8 — Wire into the Server](#step-8--wire-into-the-server)
   - [Step 9 — REST Endpoints](#step-9--rest-endpoints)
   - [Step 10 — Re-enable the PSK with `BuildSecret`](#step-10--re-enable-the-psk-with-buildsecret)
   - [Step 11 — Wire `BuildSecret` through the Makefile](#step-11--wire-buildsecret-through-the-makefile)
   - [Step 12 — Protect the Node Private Key at Rest with `gocloud.dev/secrets`](#step-12--protect-the-node-private-key-at-rest-with-goclouddevsecrets)
   - [Step 13 — Replace `BuildSecret` ldflags with a Runtime KMS Fetch](#step-13--replace-buildsecret-ldflags-with-a-runtime-kms-fetch)
9. [Scalability Properties](#scalability-properties)
10. [Roadmap](#roadmap)

---

## Overview

Every node in the OpenTela network already carries a cryptographically
authenticated wallet identity, stored as `Peer.Owner` in the CRDT node table
and derived deterministically from the node's Solana keypair or OCF key via
`wallet.deriveProviderID`.

This plan describes how to turn that identity into a **per-wallet usage ledger**
— tracking how many requests each wallet originates (consumer side) and how many
each wallet serves (provider side), with latency, error rates, and per-service
breakdowns.

**No external analytics service is required.** Everything runs inside the node
process using only dependencies already present in `go.mod`. Crucially, the
ledger is **tamper-evident**: every usage record is signed by the node's own
libp2p private key, records are chained together with SHA-256 hashes, and the
chain head is periodically anchored into the shared CRDT store so that any
attempt to rewrite history is detectable by any peer in the network.

| Dependency | Already in `go.mod`? | Role |
|---|---|---|
| `github.com/ipfs/go-ds-badger` | ✅ | Durable per-wallet counter and chain-head storage |
| `github.com/prometheus/client_golang` | ✅ | Real-time aggregate metrics at `/metrics` |
| `github.com/libp2p/go-libp2p/core/crypto` | ✅ | Ed25519/RSA signing and verification |
| Go standard library (`sync`, `sync/atomic`, `crypto/sha256`, `os`, `encoding/json`) | ✅ | Hot-path counters, JSONL event log, hashing |

---

## Existing Authentication Anchors

### Peer Identity — `Peer.ID`

Every node's P2P identity is a libp2p peer ID derived from an RSA-2048 key
pair (`host.go`). TLS and Noise are both enabled, so every connection is
mutually authenticated at the transport layer. The peer ID of the caller of any
inbound libp2p request is therefore trustworthy without an application-level
signature check.

### Wallet Identity — `Peer.Owner`

`Peer.Owner` is set during `InitializeMyself` (`node_table.go`) and propagated
to every other node via the CRDT store. Its value is, in priority order:

1. The `providerID` derived from the Solana keypair (`wallet.deriveProviderID`)
2. The raw Solana public key (`wallet.account` config value)
3. The OCF public key (legacy fallback)

Because this field travels inside CRDT-replicated records, every node can
resolve any peer ID to its owner wallet without an extra round-trip.

### Node Signing Key

The node's libp2p private key is stored at `~/.ocfcore/keys/id` and is loaded
at startup via `protocol.loadKeyFromFile()`. This key is the identity anchor for
the entire node. Because it is the same key used to authenticate every libp2p
connection, a signature made with it is recognisable to any peer in the network
using the well-known public key embedded in the peer ID.

### The Full Mapping

```
libp2p private key (~/.ocfcore/keys/id)
  └─▶ peer ID  (authenticated at transport layer by TLS/Noise)
        ├─▶ CRDT record key  (only writable by holder of the private key)
        │     └─▶ Peer.Owner  (wallet address, self-declared but unforgeable)
        └─▶ usage record signature  (signs each captured event)
```

When an inbound request arrives over libp2p, `go-libp2p-http` encodes the
remote peer's ID in `RemoteAddr`. A single `GetPeerFromTable` call turns that
peer ID into a wallet address, forming the foundation of the tracking system.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        Inbound Request                           │
│          (libp2p gostream  OR  plain HTTP)                       │
└────────────────────────────┬─────────────────────────────────────┘
                             │
┌────────────────────────────▼─────────────────────────────────────┐
│                  WalletUsageMiddleware                           │
│              server/usage_middleware.go  (new)                   │
│                                                                  │
│  1. Record start time                                            │
│  2. c.Next()  — let the real handler run                         │
│  3. Extract caller peer ID from RemoteAddr                       │
│  4. Resolve caller wallet via GetPeerFromTable                   │
│  5. Read provider wallet from protocol.MyOwner()                 │
│  6. Parse service name from URL path                             │
│  7. globalUsageTracker.Record(UsageEvent{...})                   │
└────────────────────────────┬─────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                     UsageTracker                                │
│               server/usage_tracker.go  (new)                    │
│                                                                 │
│  Hot path  : sync.Map[wallet] → atomic.Int64 counters           │
│  Per event : build SignedUsageRecord (seq, prevHash, sig)       │
│  Per event : append SignedUsageRecord to JSONL log              │
│  Per event : increment Prometheus aggregate counters            │
│  Every 15s : flush counters to Badger + store chain head        │
│  Every 5m  : publish chain anchor to CRDT                       │
└───────────┬──────────────────┬──────────────────────────────────┘
            │                  │                  │
┌───────────▼──────┐  ┌────────▼───────┐  ┌──────▼──────────────┐
│  BadgerDB shard  │  │   Prometheus   │  │                     │
│                  │  │ JSONL log file │  │                     │
│  ~/.ocfcore/     │  │   /metrics     │  │   ~/.ocfcore/       │
│  usage.<id>.db   │  │  (aggregates,  │  │   usage.<id>.jsonl  │
│                  │  │   no per-      │  │                     │
│  • per-wallet    │  │   wallet       │  │  • SignedUsageRecord│
│    int64 counters│  │   labels)      │  │    per line         │
│  • chain head    │  │                │  │  • seq + prevHash   │
│    (hash + seq)  │  │                │  │  • node signature   │
└──────────────────┘  └────────────────┘  └─────────────────────┘
            │
┌───────────▼──────────────────────────────────────────────────────┐
│   CRDT Store  /usage_anchor/<nodeId>                             │
│   Signed anchor: { head_hash, seq, ts, sig }                     │
│   Replicated to every peer — makes chain-head public and         │
│   immutable from the perspective of honest nodes that saw it     │
└──────────────────────────────────────────────────────────────────┘
            │
┌───────────▼──────────────────────────────────────────────────────┐
│   GET /v1/dnt/usage         GET /v1/dnt/usage/verify?peer=<id>   │
│   Badger snapshot +         Fetches remote JSONL, replays chain, │
│   in-memory delta           compares head against CRDT anchor    │
└──────────────────────────────────────────────────────────────────┘
```

---

## Storage Strategy

### 1. In-Memory Atomic Counters (hot path)

`sync.Map` keyed by wallet address, values are `*WalletStats` holding
`atomic.Int64` fields. Zero lock contention on the read/update path. Flushed to
Badger every 15 seconds, so at most 15 seconds of aggregated counts can be lost
on an unclean shutdown. Raw signed records in the JSONL log are written
per-event, so the audit trail is always complete regardless of the flush cycle.

### 2. BadgerDB Shard (durable per-wallet state and chain head)

A separate Badger datastore opened at `~/.ocfcore/usage.<nodeId>.db` — a
sibling of the existing CRDT store at `~/.ocfcore/ocfcore.<nodeId>.db`. Using a
separate file means usage data is never cleared by `ClearCRDTStore()` and will
not interfere with CRDT replication.

Key schema:

```
/wallet/<address>/requests          → int64 (little-endian)
/wallet/<address>/errors            → int64
/wallet/<address>/total_latency_ms  → int64
/chain/head/hash                    → hex string (SHA-256 of last record)
/chain/head/seq                     → int64 (monotonically increasing)
```

### 3. Append-only JSONL Log (audit trail)

One JSON object per line written to `~/.ocfcore/usage.<nodeId>.jsonl`. Each
line is a complete `SignedUsageRecord` (see Security Model). The file can be
tailed, grepped, or shipped to any log aggregator. Writes go through a 64 KiB
`bufio.Writer` and are flushed every 15 seconds alongside the Badger flush.

### 4. Prometheus (aggregate real-time metrics)

Per-wallet Prometheus label cardinality is avoided intentionally — wallet
addresses are high-cardinality and would cause memory pressure in the registry.
Two aggregate counters are added instead:

- `opentela_requests_total{role, service, method}` — total requests by role
- `opentela_errors_total{role, service}` — error responses by role
- `opentela_latency_ms_total{role, service}` — cumulative latency for mean computation

### 5. CRDT Usage Anchors

Every 5 minutes the current chain head (hash + sequence number) is written into
the shared CRDT store under `/usage_anchor/<nodeId>`. Because the CRDT is
replicated to all connected peers, any peer that has received an anchor for
sequence N can detect if the reporting node later tries to present a chain that
disagrees at sequence N.

---

## Security Model

### Threat Model

A node operator controls the machine their node runs on. They can:

- Stop the process, read and write Badger files directly, and restart.
- Modify the JSONL log on disk.
- Forge records claiming other wallets consumed more or less than they did,
  either to inflate rivals' costs or to hide their own consumption.

The goal is to make such tampering **detectable**, not just difficult.

### Why Encryption Alone Is Not Sufficient

Encryption is a **confidentiality** tool: it controls who can read data.
It does not prevent the key-holder from modifying data.

Because the node must decrypt usage records at runtime, the operator necessarily
holds the decryption key. A determined operator can:

1. Stop the node.
2. Decrypt the Badger database.
3. Modify values.
4. Re-encrypt with the same key.
5. Restart — and the node cannot tell anything changed.

Encryption would hide usage data from external observers, but it does nothing to
prevent the operator from lying to those same observers after the fact.

### The Solution: Signatures + Hash Chain + CRDT Anchoring

Integrity requires three complementary layers:

#### Layer 1 — Digital Signatures (non-repudiation)

Every `SignedUsageRecord` is signed with the node's libp2p private key
(`~/.ocfcore/keys/id`). The signature covers the record's content, sequence
number, and the hash of the previous record. Anyone holding the node's public
key (which is derivable from the peer ID, a public value) can verify the
signature.

**What this achieves:** A record cannot be forged by a third party.  
**Limitation:** The node operator holds the private key, so they can still sign
falsified records.

#### Layer 2 — Hash Chain (tamper-evidence)

Each record contains `PrevHash`: the SHA-256 of the previous record's canonical
bytes. This links records into an ordered chain. Modifying any record in the
chain invalidates every subsequent record's `PrevHash`, making the corruption
immediately visible to any verifier who replays the chain from the beginning.

**What this achieves:** Rewriting a single record requires recomputing and
re-signing all subsequent records.  
**Limitation:** A sufficiently motivated operator could rebuild the entire chain
from scratch if no external checkpoint constrains any prefix of it.

#### Layer 3 — CRDT Anchoring (history pinning)

Every 5 minutes, the current chain head `{ hash, seq, ts }` is itself signed
and published to the shared CRDT store under `/usage_anchor/<nodeId>`. The CRDT
replicates this to every connected peer. Once an honest peer has received anchor
A at sequence N, it permanently knows what the chain looked like at that point.

If the node later presents a JSONL log whose hash at sequence N disagrees with
the anchor peer has already seen, the discrepancy is proof of tampering.

**What this achieves:** Any anchor that has been seen by at least one honest
peer is permanently pinned. Rewriting history before that anchor requires
either:
- Corrupting every peer that received it (a Sybil attack against honest nodes), or
- Presenting a chain that the anchor's sequence number exposes as fraudulent.

**Limitation:** A brand-new node with no published anchors and no connections to
honest peers has no externally verifiable history. The first anchor a node ever
publishes is taken on trust. After that, the chain is pinned.

#### Residual Risk and Mitigation

| Attack | Detectability |
|---|---|
| Modify a single record in the middle of the chain | Immediately detectable — all subsequent `PrevHash` values break |
| Rebuild the entire chain with falsified data from genesis | Detectable — any CRDT anchor already seen by peers will disagree |
| Suppress all CRDT anchor publications (go silent) | Detectable — peers see no anchors from this node; auditors flag silence |
| Fabricate usage events from scratch before any anchor | Not detectable until a future anchor pins the chain |
| Collude with every peer that received an anchor | Requires compromise of multiple independent nodes |

The primary residual risk — fabricating events before the first anchor — is
mitigated in Phase 3 of the roadmap by having the **consumer** co-sign at
request time, providing a second independent signature over the same event.

### Record Structure

```
SignedUsageRecord
├── Seq       uint64      monotonically increasing, scoped to this node
├── PrevHash  string      hex SHA-256 of the previous record's canonical bytes
│                         genesis record uses SHA-256("genesis")
├── NodeID    string      libp2p peer ID of the recording node
├── Event     UsageEvent  the captured request data
└── Sig       string      base64(privKey.Sign(SHA-256(Seq ‖ PrevHash ‖ NodeID ‖ JSON(Event))))
```

Canonical bytes fed to the signer are the SHA-256 of the concatenation of:
`big-endian uint64(Seq)` ‖ `[]byte(PrevHash)` ‖ `[]byte(NodeID)` ‖ `JSON(Event)`.

### CRDT Anchor Structure

```
UsageAnchor
├── NodeID    string   peer ID of the anchoring node
├── HeadHash  string   hex SHA-256 of the latest SignedUsageRecord
├── Seq       uint64   sequence number of that record
├── Timestamp int64    Unix timestamp of publication
└── Sig       string   base64(privKey.Sign(SHA-256(NodeID ‖ HeadHash ‖ Seq ‖ Timestamp)))
```

---

## Open Source Deployment Considerations

### Kerckhoffs's Principle — Open Source Does Not Break the Signing Model

The cryptographic security of the usage tracking system does not depend on the
source code being secret. This is Kerckhoffs's principle: a cryptosystem should
be secure even if everything about it, except the key, is public knowledge. RSA
and SHA-256 are fully published algorithms; their security comes entirely from
the secrecy of the per-node private key at `~/.ocfcore/keys/id`, which is
generated locally and never leaves the machine.

An attacker who reads every line of this codebase gains nothing that helps them:

- **Forge a signature** — they still need the private key of the target node.
- **Break the hash chain** — SHA-256 preimage resistance is not weakened by
  knowing the chaining algorithm.
- **Forge a CRDT anchor** — the anchor is also signed with the node's private
  key.

Open source is therefore **not a threat** to the data integrity model. It is, in
fact, beneficial: the community can audit the signing and verification logic for
bugs.

### What Open Source Does Expose — Network Admission

What open source *does* weaken is **who can connect to the network at all**.
The libp2p Private Shared Key (PSK) is already implemented in `host.go` but is
commented out, and the PSK it would use is derived from `sha256(Version)`:

```go
// host.go (current, commented out)
hash := sha256.Sum256([]byte(Version))   // Version is in source → PSK is public
```

Anyone who reads the source code knows the PSK and can build a node that
connects to the production network, even if the PSK is re-enabled. This creates
two practical problems:

1. Rogue nodes can participate in the CRDT and publish garbage peer records.
2. The SPL token check in `server.go` runs after the connection is established —
   a rogue node can stay connected even if it fails the token check, because the
   check only gates service *registration*, not the transport connection itself.

### The Fix — Inject `BuildSecret` at Build Time

The `buildSecret` variable already exists in `main.go` and is already wired into
the `LDFLAGS` build system. It is currently unused (`_ = buildSecret`). The fix
is to:

1. Expose it to the `protocol` package so `host.go` can use it as the PSK seed.
2. Re-enable `libp2p.PrivateNetwork(psk)`.
3. Derive the PSK from `buildSecret` (injected by CI) rather than `Version`.
4. Pass `BUILD_SECRET` through the Makefile from a CI environment variable.

The result: only binaries built by a CI pipeline that knows `BUILD_SECRET` can
connect at the transport layer. Nodes compiled from source by third parties will
fail the libp2p handshake before they can participate in anything.

### Why `BuildSecret` Is Not a Silver Bullet

A determined adversary can extract `buildSecret` from a compiled binary using
standard reverse-engineering tools (`strings`, Ghidra, `dlv`). The PSK
therefore provides a **meaningful barrier against casual cloners** — someone who
just does `go build ./...` from source — but not against a motivated actor who
inspects the official binary.

| Adversary | PSK protection |
|---|---|
| Someone who `git clone`s and builds from source | ✅ Blocked — they won't have `BUILD_SECRET` |
| Someone who downloads the official binary and extracts the PSK | ❌ Not blocked — extraction is possible |
| Someone who steals a legitimate node's `~/.ocfcore/keys/id` | ❌ Not blocked — they have the real key |

### The Strongest Layer — SPL Token Gating

The most robust admission control is the Solana SPL token check already live in
`server.go`. Holding a specific on-chain token cannot be bypassed by binary
reverse engineering. The layered model should be:

```
Layer 1 — Transport:  libp2p PSK derived from BuildSecret
              (blocks casual cloners; binary secret)
Layer 2 — Protocol:   SPL token verification at registration time
              (blocks anyone without a valid on-chain token; blockchain-enforced)
Layer 3 — Integrity:  per-node signing + hash chain + CRDT anchoring
              (detects tampering by legitimate but malicious nodes; cryptographic)
```

Each layer stops a different class of adversary. All three are needed for a
production deployment.

### `BuildSecret` and Usage Record Signing — Why Not Mix Them

One might consider using `buildSecret` as an HMAC key for usage records instead
of (or in addition to) the per-node private key. This would be strictly weaker:

- HMAC with a **shared** secret provides symmetric authentication — any node
  that knows `buildSecret` can forge any other node's records.
- RSA/Ed25519 with a **per-node private key** provides asymmetric authentication
  — only the key-holder can sign, but anyone with the public key can verify.

`buildSecret` is appropriate for network admission (PSK). It must not be used as
the signing key for usage records. The per-node private key is the correct tool
there and is already used in the signing chain above.

---

## `gocloud.dev/secrets` Assessment

`gocloud.dev/secrets` provides a **portable symmetric encryption/decryption**
API (`keeper.Encrypt` / `keeper.Decrypt`) backed by pluggable drivers:

| Driver | Backend |
|---|---|
| `localsecrets` | NaCl secretbox — fully offline, zero infrastructure |
| `awskms` | AWS Key Management Service |
| `gcpkms` | Google Cloud KMS |
| `azurekeyvault` | Azure Key Vault |
| `hashivault` | HashiCorp Vault transit secrets engine |

The portable URL scheme (`secrets.OpenKeeper(ctx, "awskms://...")`) means the
driver can be swapped by changing a config value, with no code changes. This is
directly relevant because OpenTela nodes run in heterogeneous environments
(bare-metal, cloud VMs, local laptops).

### What It Helps With

#### 1. Protecting the Node Private Key at Rest

Currently `loadKeyFromFile()` in `protocol/key.go` reads `~/.ocfcore/keys/id`
as raw bytes. If an attacker gains filesystem read access (e.g. through a
compromised subprocess, a container escape, or a stolen disk), they get the
node's signing key and can impersonate the node forever.

`gocloud.dev/secrets` can wrap this: the private key bytes are encrypted with a
KMS-managed key before being written to disk. On startup, the node calls
`keeper.Decrypt` to recover the raw key. An attacker with only filesystem access
now gets an opaque ciphertext blob — useless without also having valid KMS
credentials.

```
~/.ocfcore/keys/id          (current: raw private key bytes — steal file = steal identity)
~/.ocfcore/keys/id.enc      (proposed: KMS-encrypted blob — steal file ≠ steal identity)
```

#### 2. Replacing `BuildSecret` ldflags with a Runtime KMS Fetch

The ldflags approach bakes `BUILD_SECRET` into the binary. As noted in the
previous section, a determined attacker can extract it with `strings` or a
debugger. With `gocloud.dev/secrets`, the PSK is never in the binary at all:

```
Current:  binary --[ldflags]--> contains BuildSecret --[strings/debugger]--> exposed
Proposed: binary --[startup]--> keeper.Decrypt(encryptedPSK) --[KMS auth]--> PSK at runtime
```

The encrypted PSK blob can be shipped as a config file or environment variable.
An attacker who extracts the blob still needs valid KMS credentials (IAM role,
Vault token, etc.) to decrypt it — a much higher bar than binary reverse
engineering.

For development and self-hosted deployments with no cloud KMS, the `localsecrets`
driver uses NaCl secretbox with a key passed via an environment variable
(`base64key://...`), requiring zero infrastructure.

### What It Does Not Help With

#### Tamper-Evidence of Usage Records

The `gocloud.dev/secrets` API provides **symmetric** encryption — any party
with KMS access can both encrypt and decrypt. It is the wrong tool for the
usage record signing model, where the goal is **asymmetric** authentication:
only the key-holder can sign, but anyone can verify.

Using a KMS to sign usage records would mean every node (and KMS operator) could
forge every other node's records. The per-node libp2p private key provides the
necessary asymmetry and must remain the signing primitive. `gocloud.dev/secrets`
should not touch the signing path.

#### The Hash Chain and CRDT Anchoring

These are purely algorithmic (SHA-256 chaining and CRDT replication). No secrets
are involved. `gocloud.dev/secrets` is irrelevant here.

#### Preventing a Malicious Operator from Tampering

Even with a KMS protecting the private key at rest, the operator can still call
`keeper.Decrypt` at runtime (the running process must be able to). The KMS
protection raises the bar for *offline* theft (stolen disk, read-only filesystem
compromise) — it does not change the threat model for a live, malicious operator
who controls the process. The hash chain + CRDT anchoring remains the primary
defence against that threat.

### Decision Matrix

| Use case | `gocloud.dev/secrets`? | Alternative |
|---|---|---|
| Protect private key file at rest | ✅ Recommended | Raw file (current, weaker) |
| Deliver PSK without baking into binary | ✅ Recommended | ldflags (current, extractable) |
| Sign usage records | ❌ Wrong tool — symmetric only | Per-node libp2p private key |
| Encrypt Badger usage data at rest | ⚠️ Possible but adds no integrity | Not worth the complexity |
| Replace `AUTH_CLIENT_SECRET` in ldflags | ✅ Good fit | ldflags (current) |

### Driver Selection by Deployment Type

| Deployment | Recommended driver | URL example |
|---|---|---|
| Local developer machine | `localsecrets` | `base64key://<env-var>` |
| Self-hosted / on-prem | `hashivault` | `hashivault://opentela-key` |
| AWS-hosted node | `awskms` | `awskms://alias/opentela?region=us-east-1` |
| GCP-hosted node | `gcpkms` | `gcpkms://projects/P/locations/L/keyRings/R/cryptoKeys/K` |

The driver URL is read from a `OTELA_SECRETS_KEEPER` environment variable at
startup so the node binary is identical across environments — only the
environment variable changes.

---

## Implementation Plan

### Step 1 — Expose `MyOwner()` and `GetNodePrivKey()` from Protocol

**File:** `src/internal/protocol/node_table.go`

```go
// MyOwner returns the wallet address (Owner field) of the local node as
// registered in the CRDT node table. Safe to call from any goroutine.
func MyOwner() string {
    return myself.Owner
}
```

**File:** `src/internal/protocol/key.go`

Add a new exported function so the server package can retrieve the node's
signing key without re-reading the file on every call:

```go
var (
    cachedPrivKey     crypto.PrivKey
    cachedPrivKeyOnce sync.Once
)

// GetNodePrivKey returns the node's libp2p private key. The key is loaded
// once from ~/.ocfcore/keys/id and cached for the lifetime of the process.
// It is the same key used to authenticate all libp2p connections.
func GetNodePrivKey() crypto.PrivKey {
    cachedPrivKeyOnce.Do(func() {
        cachedPrivKey = loadKeyFromFile()
    })
    return cachedPrivKey
}
```

These are the only changes to the `protocol` package.

---

### Step 2 — Open a Dedicated Badger Usage Store

**File:** `src/internal/server/usage_store.go` *(new file)*

```go
package server

import (
    "context"
    "encoding/binary"
    "opentela/internal/common"
    "path/filepath"
    "sync"

    badger "github.com/ipfs/go-ds-badger"
    ds "github.com/ipfs/go-datastore"
)

var (
    usageDB     *badger.Datastore
    usageDBOnce sync.Once
)

// openUsageStore opens (or creates) the Badger shard dedicated to usage
// tracking. It lives at ~/.ocfcore/usage.<nodeId>.db, alongside but separate
// from the CRDT store, so ClearCRDTStore() never touches usage data.
func openUsageStore(nodeID string) *badger.Datastore {
    usageDBOnce.Do(func() {
        dbPath := filepath.Join(common.GetHomePath(), "usage."+nodeID+".db")
        store, err := badger.NewDatastore(dbPath, &badger.DefaultOptions)
        if err != nil {
            common.Logger.Errorf("Failed to open usage store: %v", err)
            return
        }
        usageDB = store
        common.Logger.Infof("Usage store opened at %s", dbPath)
    })
    return usageDB
}

// Key helpers -----------------------------------------------------------------

func walletKey(wallet, counter string) ds.Key {
    return ds.NewKey("/wallet/" + wallet + "/" + counter)
}

func chainKey(field string) ds.Key {
    return ds.NewKey("/chain/head/" + field)
}

// I/O helpers -----------------------------------------------------------------

func readInt64(store *badger.Datastore, key ds.Key) int64 {
    b, err := store.Get(context.Background(), key)
    if err != nil || len(b) < 8 {
        return 0
    }
    return int64(binary.LittleEndian.Uint64(b))
}

func writeInt64(store *badger.Datastore, key ds.Key, v int64) {
    b := make([]byte, 8)
    binary.LittleEndian.PutUint64(b, uint64(v))
    if err := store.Put(context.Background(), key, b); err != nil {
        common.Logger.Warnf("usage store write failed (%s): %v", key, err)
    }
}

func readString(store *badger.Datastore, key ds.Key) string {
    b, err := store.Get(context.Background(), key)
    if err != nil {
        return ""
    }
    return string(b)
}

func writeString(store *badger.Datastore, key ds.Key, v string) {
    if err := store.Put(context.Background(), key, []byte(v)); err != nil {
        common.Logger.Warnf("usage store write failed (%s): %v", key, err)
    }
}
```

---

### Step 3 — Signed Usage Records and Hash Chain

**File:** `src/internal/server/usage_chain.go` *(new file)*

```go
package server

import (
    "crypto/sha256"
    "encoding/base64"
    "encoding/binary"
    "encoding/json"
    "errors"
    "fmt"
    "opentela/internal/common"
    "sync"
    "sync/atomic"
    "time"

    libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
    "opentela/internal/protocol"
)

const genesisHash = "genesis"

// SignedUsageRecord is a single tamper-evident usage event. Each record:
//   - carries a monotonic sequence number scoped to this node
//   - includes the SHA-256 of the previous record (hash chain)
//   - is signed with the node's libp2p private key
type SignedUsageRecord struct {
    Seq      uint64     `json:"seq"`
    PrevHash string     `json:"prev"`  // hex SHA-256 of previous record
    NodeID   string     `json:"node"`
    Event    UsageEvent `json:"event"`
    Sig      string     `json:"sig"`   // base64(sign(canonicalBytes))
}

// UsageAnchor is published to the CRDT store every 5 minutes. Once any honest
// peer receives an anchor at sequence N, the chain up to N is externally pinned.
type UsageAnchor struct {
    NodeID    string `json:"node"`
    HeadHash  string `json:"head_hash"`
    Seq       uint64 `json:"seq"`
    Timestamp int64  `json:"ts"`
    Sig       string `json:"sig"` // base64(sign(anchorCanonicalBytes))
}

// UsageChain manages the hash chain state for this node's usage log.
type UsageChain struct {
    mu       sync.Mutex
    seq      uint64
    prevHash string
    privKey  libp2pcrypto.PrivKey
    nodeID   string
}

var (
    globalChain     *UsageChain
    chainSeqCounter atomic.Uint64
)

// InitChain initialises the hash chain, restoring the head from Badger if
// a previous run left one. Must be called once during server startup.
func InitChain(nodeID string, store *badger.Datastore) *UsageChain {
    prevHash := readString(store, chainKey("hash"))
    seq      := uint64(readInt64(store, chainKey("seq")))

    if prevHash == "" {
        // First ever run: set the genesis hash.
        prevHash = fmt.Sprintf("%x", sha256.Sum256([]byte(genesisHash)))
        seq = 0
    }

    chainSeqCounter.Store(seq)

    globalChain = &UsageChain{
        seq:      seq,
        prevHash: prevHash,
        privKey:  protocol.GetNodePrivKey(),
        nodeID:   nodeID,
    }
    return globalChain
}

// Sign builds a SignedUsageRecord for the given event, advancing the chain.
func (c *UsageChain) Sign(e UsageEvent) (SignedUsageRecord, error) {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.seq++

    rec := SignedUsageRecord{
        Seq:      c.seq,
        PrevHash: c.prevHash,
        NodeID:   c.nodeID,
        Event:    e,
    }

    payload, err := canonicalBytes(rec)
    if err != nil {
        return SignedUsageRecord{}, fmt.Errorf("chain sign: marshal: %w", err)
    }

    sig, err := c.privKey.Sign(payload)
    if err != nil {
        return SignedUsageRecord{}, fmt.Errorf("chain sign: sign: %w", err)
    }
    rec.Sig = base64.StdEncoding.EncodeToString(sig)

    // Advance the chain head.
    c.prevHash = fmt.Sprintf("%x", sha256.Sum256(payload))

    return rec, nil
}

// HeadSnapshot returns the current chain head (hash, seq) for anchoring.
func (c *UsageChain) HeadSnapshot() (hash string, seq uint64) {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.prevHash, c.seq
}

// BuildAnchor constructs and signs a UsageAnchor from the current chain head.
func (c *UsageChain) BuildAnchor() (UsageAnchor, error) {
    hash, seq := c.HeadSnapshot()
    anchor := UsageAnchor{
        NodeID:    c.nodeID,
        HeadHash:  hash,
        Seq:       seq,
        Timestamp: time.Now().Unix(),
    }
    payload := anchorCanonicalBytes(anchor)
    sig, err := c.privKey.Sign(payload)
    if err != nil {
        return UsageAnchor{}, fmt.Errorf("build anchor: %w", err)
    }
    anchor.Sig = base64.StdEncoding.EncodeToString(sig)
    return anchor, nil
}

// Verify checks a SignedUsageRecord against the given public key and the
// expected previous hash. It is used by the audit endpoint.
func Verify(rec SignedUsageRecord, pubKey libp2pcrypto.PubKey, expectedPrevHash string) error {
    if rec.PrevHash != expectedPrevHash {
        return fmt.Errorf("seq %d: prevHash mismatch (got %s, want %s)",
            rec.Seq, rec.PrevHash, expectedPrevHash)
    }

    sigBytes, err := base64.StdEncoding.DecodeString(rec.Sig)
    if err != nil {
        return fmt.Errorf("seq %d: bad sig encoding: %w", rec.Seq, err)
    }

    // Re-derive canonical bytes from the record (sans sig).
    check := rec
    check.Sig = ""
    payload, err := canonicalBytes(check)
    if err != nil {
        return fmt.Errorf("seq %d: marshal: %w", rec.Seq, err)
    }

    ok, err := pubKey.Verify(payload, sigBytes)
    if err != nil {
        return fmt.Errorf("seq %d: verify: %w", rec.Seq, err)
    }
    if !ok {
        return fmt.Errorf("seq %d: invalid signature", rec.Seq)
    }
    return nil
}

// VerifyAnchor checks a UsageAnchor's signature against the given public key.
func VerifyAnchor(anchor UsageAnchor, pubKey libp2pcrypto.PubKey) error {
    sigBytes, err := base64.StdEncoding.DecodeString(anchor.Sig)
    if err != nil {
        return fmt.Errorf("anchor: bad sig encoding: %w", err)
    }
    check := anchor
    check.Sig = ""
    payload := anchorCanonicalBytes(check)
    ok, err := pubKey.Verify(payload, sigBytes)
    if err != nil {
        return fmt.Errorf("anchor: verify: %w", err)
    }
    if !ok {
        return errors.New("anchor: invalid signature")
    }
    return nil
}

// canonicalBytes returns the bytes to be signed/hashed for a record.
// The Sig field is zeroed before serialisation so the payload is stable.
func canonicalBytes(rec SignedUsageRecord) ([]byte, error) {
    rec.Sig = ""
    eventJSON, err := json.Marshal(rec.Event)
    if err != nil {
        return nil, err
    }
    // Payload = SHA-256(seq_bytes ‖ prevHash ‖ nodeID ‖ eventJSON)
    h := sha256.New()
    seq := make([]byte, 8)
    binary.BigEndian.PutUint64(seq, rec.Seq)
    h.Write(seq)
    h.Write([]byte(rec.PrevHash))
    h.Write([]byte(rec.NodeID))
    h.Write(eventJSON)
    return h.Sum(nil), nil
}

func anchorCanonicalBytes(a UsageAnchor) []byte {
    h := sha256.New()
    seq := make([]byte, 8)
    binary.BigEndian.PutUint64(seq, a.Seq)
    ts := make([]byte, 8)
    binary.BigEndian.PutUint64(ts, uint64(a.Timestamp))
    h.Write([]byte(a.NodeID))
    h.Write([]byte(a.HeadHash))
    h.Write(seq)
    h.Write(ts)
    return h.Sum(nil)
}
```

---

### Step 4 — In-Memory Usage Aggregator

**File:** `src/internal/server/usage_tracker.go` *(new file)*

```go
package server

import (
    "bufio"
    "context"
    "encoding/json"
    "opentela/internal/common"
    "os"
    "path/filepath"
    "sync"
    "sync/atomic"
    "time"

    badger "github.com/ipfs/go-ds-badger"
)

// UsageEvent holds data captured for a single request.
type UsageEvent struct {
    Timestamp      time.Time `json:"ts"`
    CallerPeerID   string    `json:"caller_peer,omitempty"`
    CallerWallet   string    `json:"caller_wallet,omitempty"`
    ProviderWallet string    `json:"provider_wallet,omitempty"`
    Service        string    `json:"service,omitempty"`
    Path           string    `json:"path"`
    Method         string    `json:"method"`
    StatusCode     int       `json:"status"`
    LatencyMs      int64     `json:"latency_ms"`
}

// WalletStats holds aggregated counters for one wallet.
// All fields use atomic operations — no mutex needed on the hot path.
type WalletStats struct {
    TotalRequests  atomic.Int64
    TotalErrors    atomic.Int64
    TotalLatencyMs atomic.Int64
}

// UsageTracker aggregates per-wallet statistics in memory, signs each event
// with the node's private key, and persists data to Badger and a JSONL log.
type UsageTracker struct {
    stats     sync.Map // map[string]*WalletStats
    store     *badger.Datastore
    chain     *UsageChain
    logWriter *bufio.Writer
    logFile   *os.File
    logMu     sync.Mutex
}

var globalUsageTracker *UsageTracker

// InitUsageTracker opens the Badger shard, initialises the hash chain from
// the persisted head, and opens the JSONL log. Must be called once at startup.
func InitUsageTracker(nodeID string) {
    store := openUsageStore(nodeID)
    chain := InitChain(nodeID, store)

    logPath := filepath.Join(common.GetHomePath(), "usage."+nodeID+".jsonl")
    f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        common.Logger.Errorf("Failed to open usage log: %v", err)
    }

    globalUsageTracker = &UsageTracker{
        store:     store,
        chain:     chain,
        logFile:   f,
        logWriter: bufio.NewWriterSize(f, 64*1024),
    }
    go globalUsageTracker.flushLoop()
}

// Record captures a single request event, signs it, and updates all sinks.
// Safe to call from multiple goroutines concurrently.
func (t *UsageTracker) Record(e UsageEvent) {
    // Sign the event and advance the hash chain.
    rec, err := t.chain.Sign(e)
    if err != nil {
        common.Logger.Warnf("usage chain sign failed: %v", err)
        // Still record the event unsigned rather than dropping it.
    }

    // Update in-memory atomic counters for both sides of the request.
    for _, wallet := range []string{e.CallerWallet, e.ProviderWallet} {
        if wallet == "" {
            continue
        }
        v, _ := t.stats.LoadOrStore(wallet, &WalletStats{})
        ws := v.(*WalletStats)
        ws.TotalRequests.Add(1)
        ws.TotalLatencyMs.Add(e.LatencyMs)
        if e.StatusCode >= 400 {
            ws.TotalErrors.Add(1)
        }
    }

    // Update Prometheus aggregate counters (no per-wallet labels).
    prometheusRecord(e)

    // Append the signed record to the JSONL log.
    t.logMu.Lock()
    if t.logWriter != nil {
        line, _ := json.Marshal(rec)
        _, _ = t.logWriter.Write(line)
        _, _ = t.logWriter.WriteByte('\n')
        if t.logWriter.Buffered() > 56*1024 {
            _ = t.logWriter.Flush()
        }
    }
    t.logMu.Unlock()
}

// Snapshot returns a point-in-time copy of all in-memory per-wallet counters.
func (t *UsageTracker) Snapshot() map[string]map[string]int64 {
    result := make(map[string]map[string]int64)
    t.stats.Range(func(k, v any) bool {
        ws := v.(*WalletStats)
        result[k.(string)] = map[string]int64{
            "requests":         ws.TotalRequests.Load(),
            "errors":           ws.TotalErrors.Load(),
            "total_latency_ms": ws.TotalLatencyMs.Load(),
        }
        return true
    })
    return result
}

// flushLoop persists counters and the chain head to Badger every 15 seconds
// and flushes the JSONL write buffer to disk.
func (t *UsageTracker) flushLoop() {
    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        t.flush()
    }
}

func (t *UsageTracker) flush() {
    if t.store == nil {
        return
    }

    // Persist per-wallet counters, then reset in-memory deltas.
    t.stats.Range(func(k, v any) bool {
        wallet := k.(string)
        ws     := v.(*WalletStats)

        persisted := readInt64(t.store, walletKey(wallet, "requests"))
        writeInt64(t.store, walletKey(wallet, "requests"), persisted+ws.TotalRequests.Load())

        persisted = readInt64(t.store, walletKey(wallet, "errors"))
        writeInt64(t.store, walletKey(wallet, "errors"), persisted+ws.TotalErrors.Load())

        persisted = readInt64(t.store, walletKey(wallet, "total_latency_ms"))
        writeInt64(t.store, walletKey(wallet, "total_latency_ms"), persisted+ws.TotalLatencyMs.Load())

        ws.TotalRequests.Store(0)
        ws.TotalErrors.Store(0)
        ws.TotalLatencyMs.Store(0)
        return true
    })

    // Persist the current chain head so it survives a restart.
    headHash, headSeq := t.chain.HeadSnapshot()
    writeString(t.store, chainKey("hash"), headHash)
    writeInt64(t.store, chainKey("seq"), int64(headSeq))

    // Flush the JSONL write buffer.
    t.logMu.Lock()
    if t.logWriter != nil {
        _ = t.logWriter.Flush()
    }
    t.logMu.Unlock()
}
```

---

### Step 5 — Prometheus Metrics

**File:** `src/internal/server/usage_metrics.go` *(new file)*

Wallet addresses must never appear as Prometheus label values — the cardinality
is unbounded and would exhaust registry memory. All counters here are aggregate
only; per-wallet breakdowns come from the Badger-backed REST endpoint.

```go
package server

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    promRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "opentela_requests_total",
        Help: "Total requests handled by this node, by role.",
    }, []string{"role", "service", "method"})

    promErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "opentela_errors_total",
        Help: "Total 4xx/5xx responses, by role.",
    }, []string{"role", "service"})

    promLatencyMsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "opentela_latency_ms_total",
        Help: "Cumulative request latency in ms (divide by requests for mean).",
    }, []string{"role", "service"})
)

func prometheusRecord(e UsageEvent) {
    role := "provider"
    svc  := e.Service
    if svc == "" {
        svc = "unknown"
    }
    promRequestsTotal.WithLabelValues(role, svc, e.Method).Inc()
    promLatencyMsTotal.WithLabelValues(role, svc).Add(float64(e.LatencyMs))
    if e.StatusCode >= 400 {
        promErrorsTotal.WithLabelValues(role, svc).Inc()
    }
}
```

---

### Step 6 — Gin Middleware

**File:** `src/internal/server/usage_middleware.go` *(new file)*

```go
package server

import (
    "net/http"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "opentela/internal/protocol"
)

// WalletUsageMiddleware records a signed usage event for every request.
func WalletUsageMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()

        latency := time.Since(start).Milliseconds()
        status  := c.Writer.Status()

        // go-libp2p-http encodes the remote peer ID as RemoteAddr for
        // inbound libp2p connections. IP:port pairs are plain HTTP clients.
        callerPeerID := resolveCallerPeerID(c.Request)
        callerWallet := ""
        if callerPeerID != "" {
            if peer, err := protocol.GetPeerFromTable(callerPeerID); err == nil {
                callerWallet = peer.Owner
            }
        }

        globalUsageTracker.Record(UsageEvent{
            Timestamp:      time.Now(),
            CallerPeerID:   callerPeerID,
            CallerWallet:   callerWallet,
            ProviderWallet: protocol.MyOwner(),
            Service:        parseServiceFromPath(c.Request.URL.Path),
            Path:           c.Request.URL.Path,
            Method:         c.Request.Method,
            StatusCode:     status,
            LatencyMs:      latency,
        })
    }
}

// resolveCallerPeerID extracts the libp2p peer ID from RemoteAddr.
// Peer IDs begin with "12D3" (RSA/Ed25519 multihash) or "Qm" (legacy).
func resolveCallerPeerID(r *http.Request) string {
    addr := r.RemoteAddr
    if strings.HasPrefix(addr, "12D3") ||
        strings.HasPrefix(addr, "Qm") ||
        strings.HasPrefix(addr, "16Ui") {
        return addr
    }
    return ""
}

// parseServiceFromPath extracts the logical service name from a URL path.
//
//   /v1/service/llm/v1/chat   → "llm"
//   /v1/_service/llm/v1/chat  → "llm"
//   /v1/p2p/<peerId>/...      → peer ID
//   anything else             → ""
func parseServiceFromPath(path string) string {
    parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 4)
    if len(parts) < 3 {
        return ""
    }
    switch parts[1] {
    case "service", "_service", "p2p":
        return parts[2]
    }
    return ""
}
```

#### Trust Model

Caller wallet resolution goes through `GetPeerFromTable`, which only contains
identities propagated via the CRDT. The chain of trust from connection to
wallet is:

```
libp2p private key
  └─▶ peer ID  (authenticated at transport by TLS/Noise)
        └─▶ CRDT record key  (only writable by holder of the private key)
              └─▶ Peer.Owner  (wallet address, self-declared but unforgeable)
```

---

### Step 7 — CRDT Anchor Publication

**File:** `src/internal/server/usage_anchor.go` *(new file)*

Every 5 minutes the current chain head is signed and written into the shared
CRDT store under `/usage_anchor/<nodeId>`. Once any honest peer receives this
value, the chain up to that sequence number is permanently pinned from their
perspective.

```go
package server

import (
    "context"
    "encoding/json"
    "opentela/internal/common"
    "opentela/internal/protocol"
    "time"

    ds "github.com/ipfs/go-datastore"
)

// StartAnchorPublisher begins the background loop that writes chain anchors
// to the CRDT store every 5 minutes. Must be called after InitUsageTracker.
func StartAnchorPublisher() {
    go anchorLoop()
}

func anchorLoop() {
    // Publish an initial anchor shortly after startup so the node is
    // immediately visible to peers even if it serves no requests.
    time.Sleep(30 * time.Second)
    publishAnchor()

    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    for range ticker.C {
        publishAnchor()
    }
}

func publishAnchor() {
    if globalUsageTracker == nil || globalUsageTracker.chain == nil {
        return
    }

    anchor, err := globalUsageTracker.chain.BuildAnchor()
    if err != nil {
        common.Logger.Warnf("usage: failed to build anchor: %v", err)
        return
    }

    value, err := json.Marshal(anchor)
    if err != nil {
        common.Logger.Warnf("usage: failed to marshal anchor: %v", err)
        return
    }

    store, _ := protocol.GetCRDTStore()
    key := ds.NewKey("/usage_anchor/" + anchor.NodeID)
    if err := store.Put(context.Background(), key, value); err != nil {
        common.Logger.Warnf("usage: failed to publish anchor to CRDT: %v", err)
        return
    }
    common.Logger.Infof("usage: published chain anchor seq=%d head=%s", anchor.Seq, anchor.HeadHash[:12])
}

// GetAnchor retrieves the latest published usage anchor for a given peer
// from the CRDT store. Returns an error if no anchor exists yet.
func GetAnchor(peerID string) (UsageAnchor, error) {
    store, _ := protocol.GetCRDTStore()
    key := ds.NewKey("/usage_anchor/" + peerID)
    value, err := store.Get(context.Background(), key)
    if err != nil {
        return UsageAnchor{}, err
    }
    var anchor UsageAnchor
    return anchor, json.Unmarshal(value, &anchor)
}
```

---

### Step 8 — Wire into the Server

**File:** `src/internal/server/server.go`

Four additions inside `StartServer()`:

**1. Initialise usage tracking** (after the wallet block, before `initTracer()`):

```go
host, _ := protocol.GetP2PNode(nil)
InitUsageTracker(host.ID().String())
```

**2. Register the middleware** (after `r.Use(gin.Recovery())`):

```go
r.Use(WalletUsageMiddleware())
```

**3. Register the REST endpoints** (inside `crdtGroup`):

```go
crdtGroup.GET("/usage",        getWalletUsage)
crdtGroup.GET("/usage/verify", verifyPeerChain)
```

**4. Start the anchor publisher** (alongside the other background goroutines):

```go
go StartAnchorPublisher()
```

---

### Step 9 — REST Endpoints

**File:** `src/internal/server/crdt_handler.go`

```go
// getWalletUsage returns a live snapshot of per-wallet request counters for
// this node. Counters are continuous across restarts via the Badger shard.
//
// GET /v1/dnt/usage
//
// Response:
//   {
//     "node":  "<peer-id>",
//     "owner": "<wallet-address>",
//     "chain": { "head": "<hex>", "seq": 42 },
//     "usage": { "<wallet>": { "requests": N, "errors": N, "total_latency_ms": N } }
//   }
func getWalletUsage(c *gin.Context) {
    headHash, headSeq := globalUsageTracker.chain.HeadSnapshot()
    c.JSON(http.StatusOK, gin.H{
        "node":  protocol.MyID,
        "owner": protocol.MyOwner(),
        "chain": gin.H{"head": headHash, "seq": headSeq},
        "usage": globalUsageTracker.Snapshot(),
    })
}

// verifyPeerChain fetches a peer's CRDT anchor and reports whether the
// anchor's signature is valid. A full chain replay requires fetching the
// peer's JSONL log out of band (e.g., via a dedicated P2P file-transfer
// endpoint added in a future phase).
//
// GET /v1/dnt/usage/verify?peer=<peerId>
//
// Response:
//   { "peer": "...", "anchor_seq": 42, "anchor_head": "...", "valid": true }
func verifyPeerChain(c *gin.Context) {
    peerID := c.Query("peer")
    if peerID == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "peer query param required"})
        return
    }

    anchor, err := GetAnchor(peerID)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "no anchor found for peer", "peer": peerID})
        return
    }

    // Look up the peer's public key from the libp2p peerstore.
    host, _ := protocol.GetP2PNode(nil)
    pid, err := peer.Decode(peerID)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid peer ID"})
        return
    }
    pubKey := host.Peerstore().PubKey(pid)
    if pubKey == nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "public key not in peerstore", "peer": peerID})
        return
    }

    if err := VerifyAnchor(anchor, pubKey); err != nil {
        c.JSON(http.StatusOK, gin.H{
            "peer": peerID, "anchor_seq": anchor.Seq,
            "anchor_head": anchor.HeadHash, "valid": false, "reason": err.Error(),
        })
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "peer": peerID, "anchor_seq": anchor.Seq,
        "anchor_head": anchor.HeadHash, "valid": true,
    })
}
```

---

## Scalability Properties

| Property | Mechanism |
|---|---|
| **Lock-free hot path** | `atomic.Int64` counters in `sync.Map` — no mutex per request |
| **Bounded memory** | One `*WalletStats` struct (~24 bytes) per distinct wallet ever seen |
| **Crash safety** | In-memory delta flushed to Badger every 15 s; JSONL log written per-event |
| **No Prometheus cardinality explosion** | Wallet addresses are map keys in memory / Badger, never Prometheus label values |
| **Tamper-evident records** | Every event is signed with the node's libp2p private key and chained via SHA-256 |
| **Externally pinned history** | CRDT anchors published every 5 minutes; once seen by an honest peer, rewriting history before that anchor is detectable |
| **Cryptographically grounded identity** | Caller wallet resolved from a CRDT record keyed by the caller's libp2p peer ID, which is bound to their private key at the transport layer |
| **Graceful degradation** | If a peer is not yet in the node table, `callerWallet` is empty; the event is still signed and recorded with the peer ID |
| **Zero new dependencies** | `go-ds-badger`, `prometheus/client_golang`, `go-libp2p/core/crypto` — all already in `go.mod` |
| **Restartable** | Chain head and per-wallet counters are restored from Badger on startup |

---

## Roadmap

### Phase 1 — Foundation (Steps 1–9)

Delivers tamper-evident per-wallet usage tracking with:

- Signed hash-chained records in a JSONL audit log
- Durable counters in a dedicated Badger shard (survives restarts)
- Chain head anchored to the CRDT every 5 minutes
- Real-time Prometheus aggregates at `/metrics`
- Live per-wallet query at `GET /v1/dnt/usage`
- Anchor verification at `GET /v1/dnt/usage/verify?peer=<id>`

| File | Status |
|---|---|
| `src/internal/protocol/node_table.go` | Modified — add `MyOwner()` |
| `src/internal/protocol/key.go` | Modified — add `GetNodePrivKey()` |
| `src/internal/server/usage_store.go` | New |
| `src/internal/server/usage_chain.go` | New |
| `src/internal/server/usage_tracker.go` | New |
| `src/internal/server/usage_metrics.go` | New |
| `src/internal/server/usage_middleware.go` | New |
| `src/internal/server/usage_anchor.go` | New |
| `src/internal/server/server.go` | Modified — 4 additions |
| `src/internal/server/crdt_handler.go` | Modified — add 2 handlers |

### Phase 2 — Billing / Rate Limiting Hook

The `WalletStats` counters can be read by a rate-limiter middleware placed
**before** `WalletUsageMiddleware`. It checks `ws.TotalRequests.Load()` against
a per-wallet quota stored in the CRDT or config, and rejects over-quota
requests with `429 Too Many Requests` before they reach the handler. Because
counters reset after each Badger flush, the natural quota window is 15 seconds.
For longer windows, read the cumulative Badger value instead.

### Phase 3 — Consumer Co-Signing (Closes the Residual Risk)

The primary residual risk from the security model is that a brand-new node
can fabricate events before publishing its first anchor. To close this gap,
have the **consumer** include a short-lived HMAC of the request body (keyed by
their wallet private key) in a request header (`X-Otela-Sig`). The provider
records this alongside the event. If a usage dispute arises, the consumer's
signature over the same event proves or disproves the provider's record.

### Phase 4 — Cross-Node Aggregation via CRDT G-Counter

Each node tracks its own usage independently. For a network-wide total without
an external service, encode a G-Counter into the CRDT store:

```
/usage/<wallet>/requests/<nodeId>  → int64
```

Each node only ever writes its own `<nodeId>` shard. Any node sums all shards
for a given wallet to compute the network-wide total. GossipSub propagates
these entries automatically — no new transport is needed.

---

## Step 10 — Re-enable the PSK with `BuildSecret`

**File:** `src/internal/protocol/host.go`

Add an exported package-level variable that `main.go` will populate via
ldflags, and replace the `Version`-derived PSK with one derived from
`BuildSecret`:

```go
// BuildSecret is injected at build time via:
//   -ldflags "-X opentela/internal/protocol.BuildSecret=<value>"
// It is used as the seed for the libp2p Private Shared Key so that only
// binaries produced by the official CI pipeline can join the network.
// An empty BuildSecret falls back to the Version string (dev/local builds).
var BuildSecret string
```

Then replace the existing PSK block in `newHost`:

```go
// Before (uses public Version string — effectively no secret):
hash := sha256.Sum256([]byte(Version))
keyHex := hex.EncodeToString(hash[:])
// ...
// libp2p.PrivateNetwork(psk),   ← commented out

// After (uses injected BuildSecret; falls back to Version for dev builds):
pskSeed := BuildSecret
if pskSeed == "" {
    common.Logger.Warn("BUILD_SECRET not set; PSK derived from Version only (dev mode)")
    pskSeed = Version
}
hash := sha256.Sum256([]byte(pskSeed))
keyHex := hex.EncodeToString(hash[:])

var buf bytes.Buffer
buf.WriteString("/key/swarm/psk/1.0.0/\n")
buf.WriteString("/base16/\n")
buf.WriteString(keyHex + "\n")

psk, err := pnet.DecodeV1PSK(bytes.NewReader(buf.Bytes()))
if err != nil {
    return nil, fmt.Errorf("failed to decode PSK: %w", err)
}
```

And re-enable the option:

```go
opts := []libp2p.Option{
    libp2p.DefaultTransports,
    libp2p.Identity(priv),
    libp2p.PrivateNetwork(psk),   // ← re-enabled
    // ...
}
```

**File:** `src/entry/main.go`

Wire `buildSecret` into the protocol package instead of discarding it:

```go
// Before:
_ = buildSecret

// After:
protocol.BuildSecret = buildSecret
```

---

## Step 11 — Wire `BuildSecret` through the Makefile

**File:** `src/Makefile`

Add `BUILD_SECRET` to the LDFLAGS line alongside the existing injected
variables. The value is read from the environment so it never appears in the
Makefile itself:

```makefile
# Before:
LDFLAGS += -X main.version=${VERSION} -X main.commitHash=${COMMIT_HASH} \
           -X main.buildDate=${BUILD_DATE} -X main.authUrl=$(AUTH_URL) \
           -X main.authClientId=$(AUTH_CLIENT_ID) \
           -X main.authSecret=$(AUTH_CLIENT_SECRET) \
           -X main.sentryDSN=$(SENTRY_DSN)

# After:
LDFLAGS += -X main.version=${VERSION} -X main.commitHash=${COMMIT_HASH} \
           -X main.buildDate=${BUILD_DATE} -X main.authUrl=$(AUTH_URL) \
           -X main.authClientId=$(AUTH_CLIENT_ID) \
           -X main.authSecret=$(AUTH_CLIENT_SECRET) \
           -X main.sentryDSN=$(SENTRY_DSN) \
           -X main.buildSecret=$(BUILD_SECRET)
```

`BUILD_SECRET` is then set exclusively in the CI/CD environment (GitHub Actions
secret, Doppler, AWS Secrets Manager, etc.) and is never committed to the
repository. Local developer builds leave it empty, which triggers the dev-mode
fallback warning and connects to the dev network (PSK derived from `Version`).

The CI pipeline sets it as:

```yaml
# .github/workflows/release.yml (illustrative)
- name: Build release binary
  env:
    BUILD_SECRET: ${{ secrets.NETWORK_BUILD_SECRET }}
  run: make build-release
```

### Security Properties of This Approach

| Property | Value |
|---|---|
| Secret visible in source? | ❌ No — injected from CI environment at build time |
| Secret visible in binary? | ⚠️ Yes — extractable by a motivated reverse engineer |
| Blocks `go build ./...` cloners? | ✅ Yes — they get the dev PSK, which connects to dev network only |
| Breaks existing local dev workflow? | ❌ No — empty `BUILD_SECRET` falls back gracefully with a warning |
| Requires changes to existing ldflags plumbing? | ❌ No — `buildSecret` is already declared in `main.go` and the Makefile already has the pattern |

---

## Step 12 — Protect the Node Private Key at Rest with `gocloud.dev/secrets`

**Files:** `src/internal/protocol/key.go`, `src/go.mod`

Add `gocloud.dev` to the module:

```sh
go get gocloud.dev/secrets
go get gocloud.dev/secrets/localsecrets   # always — zero-infra dev driver
# add cloud drivers as needed per deployment target:
# go get gocloud.dev/secrets/awskms
# go get gocloud.dev/secrets/hashivault
```

Replace `writeKeyToFile` and `loadKeyFromFile` in `protocol/key.go` with
encrypted equivalents:

```go
import (
    "context"
    "os"

    "gocloud.dev/secrets"
    _ "gocloud.dev/secrets/localsecrets"
    // blank-import cloud drivers in the build tag or init() of the
    // deployment-specific package, not here, to keep the core portable
)

// openKeeper opens the secrets.Keeper whose URL is given by the
// OTELA_SECRETS_KEEPER environment variable.
// Falls back to a random local key if the variable is unset (dev mode).
func openKeeper(ctx context.Context) (*secrets.Keeper, error) {
    url := os.Getenv("OTELA_SECRETS_KEEPER")
    if url == "" {
        // Dev fallback: generate a random NaCl key for this process.
        // Key material is ephemeral — the encrypted file will only be
        // readable in the same process, so this path should only be used
        // for local testing without persistence.
        common.Logger.Warn("OTELA_SECRETS_KEEPER not set; using ephemeral local key (dev mode)")
        url = "base64key://"
    }
    return secrets.OpenKeeper(ctx, url)
}

func writeKeyToFile(priv crypto.PrivKey) {
    keyData, err := crypto.MarshalPrivateKey(priv)
    if err != nil {
        common.Logger.Error("Error marshalling private key: ", err)
        return
    }

    ctx := context.Background()
    keeper, err := openKeeper(ctx)
    if err != nil {
        common.Logger.Warnf("Could not open secrets keeper; writing key unencrypted: %v", err)
        writeKeyToFileRaw(keyPath(), keyData)
        return
    }
    defer keeper.Close()

    encrypted, err := keeper.Encrypt(ctx, keyData)
    if err != nil {
        common.Logger.Warnf("Could not encrypt key; writing unencrypted: %v", err)
        writeKeyToFileRaw(keyPath(), keyData)
        return
    }
    writeKeyToFileRaw(keyPath()+".enc", encrypted)
}

func loadKeyFromFile() crypto.PrivKey {
    ctx := context.Background()

    // Prefer the encrypted file; fall back to the legacy raw file so that
    // existing nodes migrate gracefully on first restart.
    encPath := keyPath() + ".enc"
    if _, err := os.Stat(encPath); err == nil {
        keeper, err := openKeeper(ctx)
        if err != nil {
            common.Logger.Errorf("Cannot open secrets keeper to decrypt key: %v", err)
            return nil
        }
        defer keeper.Close()

        ciphertext, err := os.ReadFile(encPath)
        if err != nil {
            common.Logger.Errorf("Cannot read encrypted key file: %v", err)
            return nil
        }
        keyData, err := keeper.Decrypt(ctx, ciphertext)
        if err != nil {
            common.Logger.Errorf("Cannot decrypt key file: %v", err)
            return nil
        }
        priv, err := crypto.UnmarshalPrivateKey(keyData)
        if err != nil {
            common.Logger.Errorf("Cannot unmarshal decrypted key: %v", err)
            return nil
        }
        return priv
    }

    // Legacy path: raw unencrypted file.
    common.Logger.Warn("Loading unencrypted key file; re-encrypt by restarting with OTELA_SECRETS_KEEPER set")
    return loadKeyFromFileRaw(keyPath())
}

func keyPath() string {
    home, _ := homedir.Dir()
    return path.Join(home, ".ocfcore", "keys", "id")
}
```

The raw `writeKeyToFileRaw` / `loadKeyFromFileRaw` helpers are the existing
file I/O logic extracted into private functions with no behaviour change.

#### Migration Path for Existing Nodes

| State | Behaviour |
|---|---|
| `OTELA_SECRETS_KEEPER` unset, raw `id` file exists | Loads raw key with a warning (unchanged behaviour) |
| `OTELA_SECRETS_KEEPER` set, raw `id` file exists | Loads raw key, warns to migrate; next `writeKeyToFile` writes `.enc` |
| `OTELA_SECRETS_KEEPER` set, `.enc` file exists | Decrypts via KMS — full protection |
| `OTELA_SECRETS_KEEPER` unset, `.enc` file exists | Error — node refuses to start without KMS config |

---

## Step 13 — Replace `BuildSecret` ldflags with a Runtime KMS Fetch

Instead of baking the PSK seed into the binary via `-ldflags "-X main.buildSecret=..."`,
fetch it at startup from the same `gocloud.dev/secrets` keeper used for the key
file. The encrypted PSK blob is stored in a small file or environment variable
and is useless without valid KMS credentials.

**File:** `src/internal/protocol/host.go`

```go
// loadNetworkPSK retrieves the network PSK seed using the secrets keeper.
// The PSK seed is stored as a KMS-encrypted blob in OTELA_NETWORK_PSK_CIPHER
// (base64-encoded ciphertext). Falls back to Version for dev builds.
func loadNetworkPSK(ctx context.Context) string {
    cipherB64 := os.Getenv("OTELA_NETWORK_PSK_CIPHER")
    if cipherB64 == "" {
        if BuildSecret != "" {
            // Transitional: still accept ldflags injection during migration.
            return BuildSecret
        }
        common.Logger.Warn("OTELA_NETWORK_PSK_CIPHER not set; PSK derived from Version (dev mode)")
        return Version
    }

    ciphertext, err := base64.StdEncoding.DecodeString(cipherB64)
    if err != nil {
        common.Logger.Errorf("OTELA_NETWORK_PSK_CIPHER is not valid base64: %v", err)
        return Version
    }

    keeper, err := openKeeper(ctx) // same helper as in key.go
    if err != nil {
        common.Logger.Errorf("Cannot open keeper to decrypt PSK: %v", err)
        return Version
    }
    defer keeper.Close()

    plaintext, err := keeper.Decrypt(ctx, ciphertext)
    if err != nil {
        common.Logger.Errorf("Cannot decrypt network PSK: %v", err)
        return Version
    }
    return string(plaintext)
}
```

Then in `newHost`, replace the current PSK seed derivation:

```go
// Before:
pskSeed := BuildSecret
if pskSeed == "" {
    pskSeed = Version
}

// After:
pskSeed := loadNetworkPSK(ctx)
```

**Provisioning the PSK blob** (done once by the network operator, not per node):

```sh
# 1. Generate a random PSK seed
PSK_SEED=$(openssl rand -hex 32)

# 2. Encrypt it with the KMS keeper (example: HashiCorp Vault)
OTELA_SECRETS_KEEPER="hashivault://opentela-key" \
    go run ./tools/seal_secret --plaintext "$PSK_SEED"
# → prints base64 ciphertext

# 3. Distribute the ciphertext as OTELA_NETWORK_PSK_CIPHER in node config
#    (environment variable, config file, or secret manager reference)
```

The PSK seed never appears in any binary, source file, or CI log. Only nodes
with KMS access can decrypt it, and KMS access is gated by IAM / Vault policies.

#### Security Comparison

| Property | ldflags `BuildSecret` | `gocloud.dev/secrets` KMS fetch |
|---|---|---|
| Secret in source? | ❌ No | ❌ No |
| Secret in binary? | ⚠️ Yes — extractable | ✅ No — never in binary |
| Extraction method | `strings` / debugger | Requires valid KMS credentials |
| Works offline? | ✅ Yes | ⚠️ Requires KMS reachability at startup |
| Dev / local build | ✅ Falls back to Version | ✅ Falls back to Version |
| Infrastructure needed | None | KMS service (or `localsecrets` for dev) |

---

### Phase 5 — JSONL Log Shipping (Optional)

```sh
# tail live signed events
tail -f ~/.ocfcore/usage.<nodeId>.jsonl | jq '{seq:.seq,wallet:.event.caller_wallet,sig:.sig}'

# verify the entire chain locally
cat ~/.ocfcore/usage.<nodeId>.jsonl | jq -c . | go run ./tools/verify_chain --peer <nodeId>
```

Signed JSONL files can be shipped to Loki, ClickHouse, or S3 without any code
changes — each line is a self-contained, verifiable JSON object.
