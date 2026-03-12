# OpenTela Configuration Reference

OpenTela reads configuration from a YAML file, environment variables, and CLI flags (in ascending priority).

**Config file location:** `$HOME/.config/opentela/cfg.yaml`
(Legacy path `$HOME/.config/ocf/cfg.yaml` is auto-detected.)

**Environment variables:** All keys can be set via env vars with the `OF_` prefix. Dots become underscores:
`billing.enabled` → `OF_BILLING_ENABLED`

**CLI override:** `otela start --config /path/to/cfg.yaml`

---

## Full Reference

### Node Identity

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `name` | string | `"relay"` | Human-readable node name |
| `seed` | string | `"0"` | Key generation seed. `"0"` = load existing key from disk |
| `mode` | string | `"node"` | Node mode: `standalone`, `local`, `full`, `node` |
| `public-addr` | string | `""` | Public address to advertise (set on bootstrap/head nodes) |
| `component` | string | `"server"` | Component to start: `server`, `ingress`, `all` |
| `subprocess` | string | `""` | External process to launch and supervise |
| `debug` | bool | `false` | Enable debug mode |
| `loglevel` | string | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| `cleanslate` | bool | `true` | Wipe local database on startup |

### Networking

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `port` | string | `"8092"` | HTTP API listen port |
| `tcpport` | string | `"43905"` | libp2p TCP listen port |
| `udpport` | string | `"59820"` | libp2p UDP/QUIC listen port |
| `p2p.port` | string | `"8093"` | P2P module port |
| `queue.port` | string | `"8094"` | Queue module port |
| `path` | string | `""` | Data directory path |
| `datadir` | string | `"$HOME/.otela"` | Usage tracking data directory |

### Bootstrap

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `bootstrap.sources` | []string | `[]` | Bootstrap sources: HTTP URLs, `dnsaddr://`, or multiaddrs |
| `bootstrap.static` | []string | *(hardcoded)* | Static bootstrap node list (built-in fallback) |
| `bootstrap.addr` | string | `""` | Single bootstrap address (legacy, prefer `sources`) |

### Service Registration

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `service.name` | string | `""` | Service to register (e.g., `"llm"`) |
| `service.port` | string | `""` | Local port the service listens on |

### CRDT / State Management

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `crdt.tombstone_retention` | duration | `"24h"` | How long deleted keys are retained |
| `crdt.tombstone_compaction_interval` | duration | `"1h"` | How often compaction runs |
| `crdt.tombstone_compaction_batch` | int | `512` | Keys processed per compaction batch |

### Wallet & Solana

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `account.wallet` | string | `""` | Path to wallet key file |
| `wallet.account` | string | `""` | Wallet public key (base58) |
| `solana.rpc` | string | `"https://api.mainnet-beta.solana.com"` | Solana RPC endpoint |
| `solana.mint` | string | `"EsmcTrd..."` | SPL token mint address for verification |
| `solana.skip_verification` | bool | `false` | Skip SPL token ownership check |

### Security

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `security.require_signed_binary` | bool | `true` | Reject peers without valid build attestation |
| `security.auth_url` | string | `""` | Auth server URL for token verification |
| `security.access_control.policy` | string | `"any"` | Access policy: `any`, `self`, `whitelist`, `blacklist` |
| `security.access_control.whitelist` | []string | `[]` | Allowed wallet pubkeys (when policy=`whitelist`) |
| `security.access_control.blacklist` | []string | `[]` | Blocked wallet pubkeys (when policy=`blacklist`) |
| `security.rate_limit.enabled` | bool | `false` | Enable per-IP rate limiting |
| `security.rate_limit.requests_per_second` | float | `100` | Requests per second limit |
| `security.rate_limit.burst` | int | `200` | Burst capacity |
| `trusted_wallets` | []string | `[]` | Explicitly trusted wallet pubkeys (trust level 2) |

### Billing

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `billing.enabled` | bool | `false` | Enable usage tracking and billing |
| `billing.value_threshold` | int | `10000000` | Lamport threshold to trigger aggregation |
| `billing.max_interval_minutes` | int | `60` | Max minutes before forcing aggregation |
| `billing.dispute_threshold_pct` | int | `10` | Percentage mismatch to flag disputes |
| `otela_token.mint_address` | string | `"BAYyKY..."` | OpenTela token mint address |
| `otela_token.decimals` | int | `9` | Token decimal places |
| `rates.default_per_1000_tokens` | int64 | — | Default rate per 1000 tokens |
| `rates.default_per_gpu_ms` | int64 | — | Default rate per GPU millisecond |
| `rates.config_path` | string | — | Path to external rates config file |

### Ingestion

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ingest.url` | string | `"http://localhost:8081"` | Ingest service URL |
| `ingest.port` | string | `"8081"` | Ingest service port |

### Vacuum

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `vacuum.interval` | int | `10` | Peer cleanup interval (seconds) |

---

## Example Configurations

### Minimal Worker Node

A worker node that registers an LLM service and joins an existing network.

```yaml
name: gpu-worker-01
port: "8092"
tcpport: "43905"
udpport: "59820"

service:
  name: llm
  port: "8000"

bootstrap:
  sources:
    - "dnsaddr://bootstrap.opentela.io"
```

### Head Node (Dispatcher)

A head node that receives client HTTP requests and forwards them to workers.

```yaml
name: head-eu-01
port: "8080"
tcpport: "43905"
udpport: "59820"
public-addr: "head-eu-01.opentela.io"

bootstrap:
  sources:
    - "dnsaddr://bootstrap.opentela.io"
```

### Bootstrap Node

A well-known node that helps peers discover each other.

```yaml
name: bootstrap-01
port: "8092"
tcpport: "43905"
udpport: "59820"
public-addr: "bootstrap.opentela.io"
mode: full
```

### Private Network (Signed Binaries Only)

A closed network where only nodes running signed binaries can participate.
Build all binaries with the same signing key (`make build-signed`).

```yaml
name: private-worker-01
port: "8092"
tcpport: "43905"
udpport: "59820"

security:
  require_signed_binary: true   # default; peers without valid attestation are rejected

service:
  name: llm
  port: "8000"

bootstrap:
  sources:
    - "/ip4/10.0.0.1/tcp/43905/p2p/QmBootstrapPeerID"
```

### Production Worker with Full Security

A production worker with wallet identity, access control, rate limiting, and billing.

```yaml
name: prod-worker-01
port: "8092"
tcpport: "43905"
udpport: "59820"
loglevel: info

service:
  name: llm
  port: "8000"

bootstrap:
  sources:
    - "dnsaddr://bootstrap.opentela.io"

wallet:
  account: "YourBase58WalletPubkey"

solana:
  rpc: "https://api.mainnet-beta.solana.com"
  mint: "EsmcTrdLkFqV3mv4CjLF3AmCx132ixfFSYYRWD78cDzR"

security:
  require_signed_binary: true
  access_control:
    policy: whitelist
    whitelist:
      - "AllowedWalletPubkey1"
      - "AllowedWalletPubkey2"
  rate_limit:
    enabled: true
    requests_per_second: 50
    burst: 100

trusted_wallets:
  - "TrustedOperatorWallet1"

billing:
  enabled: true
  value_threshold: 10000000
  max_interval_minutes: 60
  dispute_threshold_pct: 10

crdt:
  tombstone_retention: 24h
  tombstone_compaction_interval: 1h
  tombstone_compaction_batch: 512
```

### Development / Local Testing

For local development with security relaxed and verbose logging.

```yaml
name: dev-node
port: "8092"
tcpport: "43905"
udpport: "59820"
loglevel: debug
cleanslate: true
mode: standalone

security:
  require_signed_binary: false

solana:
  skip_verification: true
```

### Multi-Network Setup

To run separate networks (e.g., staging vs production), use different signing keys.
Each network's binaries embed a different public key in `attestation.go`, so nodes
from one network automatically reject peers from the other.

**Network A** (production):
```yaml
# Built with: BUILD_SIGN_KEY=$PROD_KEY make build-signed
security:
  require_signed_binary: true
bootstrap:
  sources:
    - "dnsaddr://prod-bootstrap.opentela.io"
```

**Network B** (staging):
```yaml
# Built with: BUILD_SIGN_KEY=$STAGING_KEY make build-signed
security:
  require_signed_binary: true
bootstrap:
  sources:
    - "dnsaddr://staging-bootstrap.opentela.io"
```

Nodes in Network A will reject Network B peers (and vice versa) because their
build signatures are verified against different public keys compiled into the binary.
