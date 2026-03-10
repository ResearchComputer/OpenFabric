---
title: "Security Hardening"
date: "2026-03-10"
tldr: "Build attestation, node trust model, rate limiting, and transport encryption — what is implemented, how to configure it, and what the limitations are."
---

# Security Hardening

## Overview

OpenTela is a decentralized network where untrusted operators can run nodes. The security model addresses four concerns:

1. **Binary integrity** — Is this node running an official, unmodified binary?
2. **Operator identity** — Does the node operator actually control the wallet they claim?
3. **Abuse prevention** — How do we limit request floods and resource exhaustion?
4. **Transport confidentiality** — Is traffic between nodes encrypted?

Each layer is independently configurable and backward-compatible with older nodes that pre-date these features.

---

## 1. Build Attestation

### How it works

During a release build, the CI pipeline signs the string `version|commitHash` with a maintainer Ed25519 private key. The hex-encoded signature is injected into the binary via ldflags (`-X main.buildSig=...`). The corresponding public key is compiled into every binary.

At runtime:

- On startup, the node packages its version, commit hash, and signature into a `BuildAttestation` field on its CRDT peer record.
- When any node receives a peer record, it verifies the signature against the embedded maintainer public key.
- The result is stored in `Peer.SignedBuild` (a locally-computed boolean, not trusted from the network).

### Configuration

```yaml
security:
  require_signed_binary: false   # default: accept unsigned peers with a warning
```

When `true`, peers without a valid build attestation are rejected from the node table entirely — they cannot participate in routing.

When `false` (default), unsigned peers are accepted but `Peer.SignedBuild` is set to `false`, and a warning is logged. This allows gradual rollout without breaking existing deployments.

### Setup (one-time)

```bash
cd src

# Generate a maintainer Ed25519 keypair
go run ./internal/attestation/cmd/buildsign keygen
# Output:
#   public:  <hex>
#   private: <hex>
```

1. Put the public key in `internal/attestation/attestation.go` → `maintainerPubKeyHex` constant.
2. Store the private key as a GitHub Actions secret named `BUILD_SIGN_KEY`.

The release workflow (`.github/workflows/release.yml`) will automatically sign builds when `BUILD_SIGN_KEY` is set.

### Files

| File | Role |
|------|------|
| `internal/attestation/attestation.go` | `Sign()`, `Verify()`, maintainer public key |
| `internal/attestation/cmd/buildsign/` | CLI tool for `keygen` and `sign` |
| `internal/protocol/node_table.go` | `Peer.BuildAttestation`, verification in `UpdateNodeTableHook` |
| `entry/main.go` | Receives `buildSig` via ldflags |
| `.github/workflows/release.yml` | "Sign build attestation" step |

---

## 2. Node Trust Model

### Trust levels

| Level | Name | Meaning |
|-------|------|---------|
| 0 | Untrusted | No identity attestation, or attestation is invalid |
| 1 | Self-attested | Operator signed `{peerID, walletPubkey, timestamp}` with their Ed25519 wallet key, and the signature is valid |
| 2 | User-trusted | Self-attested AND the wallet is in the local node's `trusted_wallets` list, OR the wallet matches the local node's own wallet (self-trust) |
| 3 | KYC-verified | Reserved for future use (centralized KYC oracle) |

### How identity attestation works

On startup, if the node has a wallet configured, it creates an `IdentityAttestation`:

```json
{
  "peer_id": "QmABC...",
  "wallet_pubkey": "5xyz...",
  "timestamp": 1741564800,
  "signature": "<base64 Ed25519 signature over the JSON of the above three fields>"
}
```

This is published in the CRDT peer record. When another node receives this record, it:

1. Reconstructs the signed payload from `{peer_id, wallet_pubkey, timestamp}`.
2. Decodes the `wallet_pubkey` (base58) into an Ed25519 public key.
3. Verifies the signature.
4. If valid, sets `TrustLevel = 1`. Then checks if the wallet is in `trusted_wallets` or matches the local wallet for elevation to level 2.

### Trust-aware routing

Clients can request a minimum trust level via the `X-Otela-Trust` HTTP header:

```
X-Otela-Trust: 1
```

The head node filters candidate workers to only include peers at or above the requested trust level. If no peers qualify, it returns `503 Service Unavailable` with a descriptive error — it does **not** silently fall back to untrusted peers.

Default (no header or `X-Otela-Trust: 0`): all connected peers are eligible, regardless of trust level.

### Configuration

```yaml
# Wallets you explicitly trust (grants trust level 2 to matching peers)
trusted_wallets:
  - "5YourFriendsPubkey..."
  - "7AnotherOperator..."

# Your own wallet (self-trust: peers with the same wallet get level 2)
wallet:
  account: "YourOwnPubkey..."
```

### Files

| File | Role |
|------|------|
| `internal/wallet/identity.go` | `SignIdentity()`, `VerifyIdentity()` |
| `internal/protocol/node_table.go` | Trust level constants, `Peer.IdentityAttestation`, `Peer.TrustLevel`, verification in `UpdateNodeTableHook` |
| `internal/server/proxy_handler.go` | `filterByTrust()`, `X-Otela-Trust` header parsing |
| `internal/server/cors.go` | `X-Otela-Trust` in allowed headers |

---

## 3. Rate Limiting

### How it works

An in-memory per-IP token-bucket rate limiter sits as Gin middleware before all HTTP handlers. Each client IP gets an independent bucket. Stale entries (no requests for 3 minutes) are automatically evicted.

When the limit is exceeded, the server returns:

```
HTTP 429 Too Many Requests
Retry-After: 1
{"error": "rate limit exceeded"}
```

### Configuration

```yaml
security:
  rate_limit:
    enabled: true              # default: false
    requests_per_second: 100   # default: 100
    burst: 200                 # default: 200
```

Disabled by default. When enabled, the middleware is active for all endpoints (health, CRDT, P2P forwarding, service forwarding).

### Files

| File | Role |
|------|------|
| `internal/server/ratelimit.go` | Token-bucket middleware using `golang.org/x/time/rate` |
| `internal/server/server.go` | Middleware registration |

---

## 4. Transport Encryption

### Current state

libp2p is configured with two security transports:

- **TLS 1.3** (`/tls/1.0.0`) — preferred
- **Noise** (`/noise`) — fallback

Both are registered in `host.go`. libp2p automatically negotiates the strongest mutually-supported protocol on every connection. All inter-node traffic is encrypted by default.

The negotiated security protocol is logged on each connection event:

```
Connected to peer: QmABC...  security=/tls/1.0.0  Total connections: 5
```

### PSK (Private Network)

Code for pre-shared key network isolation exists in `host.go` but is currently commented out. When enabled, only nodes sharing the same PSK can form a network. This is a separate concern from build attestation — PSK isolates the network, while build attestation verifies binary integrity.

---

## Caveats and Limitations

### Build attestation is software-only

A modified binary can report a fake `BuildAttestation` with a valid signature copied from a legitimate build of the same version. Build attestation prevents:

- Accidental use of unofficial or development builds in production
- Naive tampering (modifying the binary without updating the attestation)

It does **not** prevent a determined attacker who patches the binary to replay a legitimate attestation. True binary integrity requires hardware-backed remote attestation (Intel TDX, AMD SEV-SNP), which is not yet implemented.

### Identity attestation proves key ownership, not identity

`IdentityAttestation` proves that the node operator controls a particular Ed25519 private key. It does not prove:

- Who the operator is (that requires KYC, which is not yet implemented)
- That the operator is operating the node honestly
- That the node's hardware or software has not been tampered with

A malicious operator with a valid wallet can still run a compromised node that passes identity attestation at trust level 1.

### Trust level is computed locally

`TrustLevel` is not a network-wide consensus value. Each node computes it independently based on its own `trusted_wallets` config. This means:

- The same peer may have `TrustLevel = 2` on one head node and `TrustLevel = 1` on another, depending on their respective trust lists.
- A head node's trust decisions are only as good as its own configuration.

### Rate limiting is per-node, not network-wide

Each node enforces rate limits independently. A distributed attacker could spread requests across multiple head nodes to circumvent per-node limits. Network-wide rate limiting would require coordination (e.g., shared state via CRDT), which is not implemented.

Rate limiting is also IP-based only. Clients behind a shared NAT or proxy will share the same bucket. Per-wallet rate limiting is a planned future enhancement.

### SignedBuild and TrustLevel are in the JSON peer record

Both `signed_build` and `trust_level` are present in the serialized JSON peer record that propagates via CRDT. However, receiving nodes **always recompute these values locally** from the raw attestation data — they are never trusted from the network. A malicious node setting `"signed_build": true` or `"trust_level": 3` in its record has no effect on other nodes.

### Backward compatibility

- Nodes running versions before these security features will have `BuildAttestation = nil`, `IdentityAttestation = nil`, and `TrustLevel = 0`.
- They can still join the network and serve requests unless `security.require_signed_binary` is set to `true`.
- Different signed versions (e.g., v1.0.0 and v2.0.0) can communicate freely — the attestation verifies the maintainer's signature, not a specific version hash.
- The `X-Otela-Trust` header is opt-in. Clients that don't send it get the previous behavior (all peers eligible).

### No revocation mechanism

There is currently no way to revoke a compromised maintainer signing key or a compromised wallet. If the maintainer key is leaked:

- All binaries signed with it remain "valid" until the public key constant is rotated and a new release is cut.
- Old nodes will still accept the compromised key until they upgrade.

Similarly, there is no wallet blocklist. A compromised wallet at trust level 2 (in someone's `trusted_wallets`) stays trusted until manually removed from the config.
