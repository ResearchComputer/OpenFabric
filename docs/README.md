# OpenTela

A decentralized distributed computing platform that orchestrates GPU resources across a peer-to-peer network for LLM serving.

## Overview

OpenTela connects GPU resources into a unified pool using:
- **libp2p networking** for peer-to-peer communication
- **CRDT-based state management** for distributed consensus
- **Identity group routing** for intelligent request distribution
- **Solana settlement** for automated usage billing

Primary use case: Distributed GPU node orchestration for LLM serving, powering projects like the [SwissAI Initiative](https://serving.swissai.cscs.ch/).

## Quick Start

### Installation

```bash
# x86_64
wget https://github.com/eth-easl/OpenTela/releases/latest/download/otela-amd64 -O otela && chmod +x otela

# arm64
wget https://github.com/eth-easl/OpenTela/releases/latest/download/otela-arm64 -O otela && chmod +x otela
```

### Spin Up a Cluster

**Head Node:**
```bash
./otela start --mode standalone --public-addr {YOUR_IP} --seed 0
```

**Worker Node:**
```bash
./otela start \
  --bootstrap.addr /ip4/{HEAD_IP}/tcp/43905/p2p/{HEAD_PEER_ID} \
  --subprocess "vllm serve Qwen/Qwen3-8B --port 8080" \
  --service.name llm \
  --service.port 8080
```

### Send Requests

```python
import openai
client = openai.OpenAI(
    base_url="http://{HEAD_IP}:8092/v1/service/llm/v1",
    api_key="test-token"
)
response = client.chat.completions.create(
    model="Qwen/Qwen3-8B",
    messages=[{"role": "user", "content": "Hello!"}]
)
```

## Documentation

Full documentation is available at the [OpenTela Docs](https://docs.opentela.ai) or browse the [`content/docs/`](content/docs/) directory:

- **Tutorial** — Installation, spinup, routing, wallets, settlement
- **Advanced** — CRDT internals, performance, security
- **Blog** — Real-world deployments (SwissAI)
- **Proposals** — Design documents

## Development

This repository contains the documentation site built with Next.js and Fumadocs.

```bash
npm install
npm run dev
```

**Pages:**
- `/account` — wallet + Neon Auth login, API-key management (wallet keys + `sk-` keys), and a live `/v1/models` services catalog. Account sign-in requires `NEXT_PUBLIC_NEON_AUTH_URL` plus backend `CORS_ALLOWED_ORIGINS` allow-listing and a Neon trusted-domain registration.

The OpenTela binary source code is available at [eth-easl/OpenTela](https://github.com/eth-easl/OpenTela).

## License

MIT
