# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

OpenTela (formerly OpenFabric) is a decentralized distributed computing platform that orchestrates computing resources across a peer-to-peer network. It uses libp2p networking, CRDT-based state management, and gossip protocols. Primary use case: distributed GPU node orchestration for LLM serving (SwissAI Initiative).

## Build & Development Commands

All commands run from the `src/` directory:

```bash
make build                                    # Build binary (output: src/build/entry)
make build-signed                             # Build signed binary (requires BUILD_SIGN_KEY)
make build-release                            # Optimized binaries for amd64 & arm64
make build-debug                              # Build with debug symbols
make test                                     # Run all tests with coverage
make test TEST_PKGS="./internal/protocol/..."  # Run tests for a specific package
make test VERBOSE=1                           # Verbose test output
make lint                                     # Run golangci-lint v2 (config in .golangci.yml)
make lint LINT_PKGS="./internal/server/..."   # Lint a specific package
make check                                    # Run both tests and lint
```

Integration tests (require Docker):
```bash
make test TEST_PKGS="./tests/integration/..." GOARGS="-tags=integration -count=1"
```

**CGO_ENABLED=0** for builds (no C deps), **CGO_ENABLED=1** for tests (race detector, set automatically by Makefile). Coverage output goes to `build/coverage.xml` and `build/coverage.txt`.

## CLI Commands

The binary (`otela`) supports these commands:

| Command | Purpose |
|---------|---------|
| `otela init` | Create `~/.config/opentela/`, default config, generate Ed25519 keypair |
| `otela start` | Start node (`--component server\|ingress\|all`) |
| `otela version` | Print version, commit, build date |
| `otela update` | Download latest binary from GitHub releases |
| `otela wallet <subcommand>` | Solana wallet management (create, list, info, export, import, balance, transfer, airdrop) |

## Architecture

### Network Topology

Nodes form a P2P mesh using libp2p. A **head node (dispatcher)** receives client HTTP requests and forwards them over libp2p to **worker nodes** running local services (e.g., vLLM). An optional **relay node** bridges network segments (e.g., between cloud VMs and HPC clusters).

### Request Routing Flow

1. Client sends request to head node at `/v1/service/:service/*path`
2. Head node queries the distributed node table for peers matching the requested **identity group** (e.g., `model=Qwen/Qwen3-8B`)
3. Three-tier matching with fallback (`X-Otela-Fallback` header): exact match → wildcard (`model=*`) → catch-all (`all`)
4. Request forwarded over libp2p to selected worker, which proxies to its local service

### Key Source Directories (`src/`)

- **`entry/cmd/`** — CLI entry point (Cobra). `root.go` initializes config (Viper), `start.go` starts the node, `config.go` manages defaults and feature flags.
- **`internal/protocol/`** — P2P networking core. `host.go` sets up libp2p host. `crdt.go` initializes CRDT datastore (Badger-backed). `node_table.go` tracks peers and services. `registrar.go` handles service registration.
- **`internal/server/`** — HTTP server (Gin). `server.go` sets up endpoints. `proxy_handler.go` contains routing logic (`GlobalServiceForward`, `P2PForwardHandler`). `crdt_handler.go` exposes CRDT APIs.
- **`internal/protocol/go-ds-crdt/`** — Forked CRDT datastore implementation with tombstone management and compaction.
- **`internal/attestation/`** — Build attestation: Ed25519 signing/verification of binaries. `cmd/buildsign/` provides `keygen` and `sign` CLI tools for release pipelines.
- **`internal/ingest/`** — Telemetry collection server (port 8081). Exposes `/status` and `/health` with CPU, memory, and GPU stats. Started via `--component ingress`.
- **`internal/metrics/`** — Prometheus metrics aggregation. Scrapes `/metrics` from peers, relabels with peer context (`peer_id`, `provider_id`, `service`, `model`), and exposes aggregated metrics.
- **`internal/usage/`** — Dual-attestation billing: usage extraction from response headers, aggregation, CRDT reconciliation, Solana settlement.
- **`internal/common/`** — Shared utilities, constants, logging, process management.
- **`internal/wallet/`** and **`internal/solana/`** — Solana wallet management and SPL token verification.
- **`internal/platform/`** — Hardware detection (NVIDIA GPU via nvidia-smi, Slurm environment).

### Key Patterns

- **CRDT consensus**: Badger DB + tombstone-based deletion (24h retention, hourly compaction). No central coordinator.
- **Gossip protocol**: libp2p PubSub for broadcasting, Kademlia DHT for peer discovery, 20s ping interval.
- **Config**: Viper with YAML config at `~/.config/opentela/cfg.yaml` (migrates from legacy `~/.config/ocf/`), env vars prefixed `OF_`.
- **Feature flags**: Opt-in scalability features in config — `scalability.swim_enabled`, `scalability.crdt_tuned`, `scalability.weighted_routing`, etc. All disabled by default.
- **Build attestation**: When `security.require_signed_binary=true`, nodes reject peers running unsigned binaries. Disabled by default for development.

## Deployment

### Test Mesh (`deploy/test/`)

Two-head-node test mesh on cloud VMs (ocf-1, ocf-2) with systemd service files. Both bootstrap from each other + Euler relay. Config uses `cleanslate=false` to preserve CRDT state across restarts.

### Euler HPC (`deploy/euler/`)

Relay node runs on Euler login node (overrides HOME to /tmp to avoid Lustre BadgerDB issues). Worker nodes run via SLURM jobs using Apptainer containers for GPU workloads (sglang/vLLM).

## Usage Tracking and Billing

Opt-in dual-attestation billing system. Services report usage via HTTP response headers (`X-Usage-Tokens`, `X-Usage-GPU-Ms`). Records aggregate locally until threshold triggers, then shared via CRDT for reconciliation and submitted to Solana for settlement.

Enable with `billing.enabled: true` in config or `OF_BILLING_ENABLED=true`. See `docs/proposals/2026-03-08-usage-tracking-design.md` for full design.

## CI

- **Tests**: `make test VERBOSE=1` in `src/` (GitHub Actions)
- **Lint**: golangci-lint v2.6 with 5m timeout, presets: comments, common-false-positives, legacy, std-error-handling
- **Release**: Triggered by `v*` tags; builds binaries and Docker images pushed to `ghcr.io`
