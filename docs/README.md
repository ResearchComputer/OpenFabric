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

### Toolchain versions

**`package-lock.json` must stay installable by npm 10.x.** Cloudflare Workers Builds pins
npm to 10.9.2 and — unlike `NODE_VERSION` — offers no `NPM_VERSION` override, so the
build image's npm is not negotiable.

This matters because npm 10 and npm 12 disagree on how optional peer dependencies are
recorded — notably the nested `jayson > ws@7 > utf-8-validate@5` node. A lockfile written
by npm 12 omits it, and `npm ci` under npm 10 then fails with
`Missing: utf-8-validate@5.0.10 from lock file`.

If your local npm is 12.x, regenerate the lockfile with npm 10 before committing:

```bash
npx npm@10.9.2 install --package-lock-only
```

The result installs cleanly under both npm 10 and npm 12, so this costs local users
nothing. `docs/.node-version` pins Node to 22.23.1 (Workers Builds reads `.node-version`),
which keeps the bundled npm on 10.x.

### Deployment configuration

The site deploys as an OpenNext **Worker** (`wrangler.jsonc`, `x-opennext: 1`), built by
Cloudflare Workers Builds from a git clone.

`NEXT_PUBLIC_*` values are **inlined into the client bundle at build time**. They must be
set under *Settings → Build → Build variables and secrets* — setting them under
*Settings → Variables & Secrets* (runtime) has no effect, because the browser bundle was
already compiled without them. `.env.local` is gitignored and never reaches the build.

| Build variable | Needed for | If unset |
|----------------|-----------|----------|
| `NEXT_PUBLIC_NEON_AUTH_URL` | Account sign-in | `/account` renders "Account sign-in is not configured on this deployment" |
| `NEXT_PUBLIC_AUTH_API_BASE_URL` | Wallet-derived API keys | Falls back to `http://localhost:8090` — unreachable, and blocked as mixed content over HTTPS |
| `NEXT_PUBLIC_API_BASE_URL` | `sk-` keys + `/v1/models` | Defaults to `https://api.opentela.ai` (correct in production) |

See `.env.example` for the full set and current values. Build variables only take effect
on a **new build** — redeploy after changing them.

Two settings outside this repo must also allow the deployment's origin, or sign-in fails
after the bundle is fixed:

- **Neon Auth trusted domains** — register the origin the console is served from.
- **`CORS_ALLOWED_ORIGINS`** — a [Fly.io](https://fly.io) secret on the **`opentela-api`**
  app (the service behind `api.opentela.ai`; it lives outside this repo, and is not the
  Go server in `src/`, which sends `Access-Control-Allow-Origin: *`). Update with:

  ```bash
  fly secrets set CORS_ALLOWED_ORIGINS="https://opentela.ai,http://localhost:3000" -a opentela-api
  ```

  `set` replaces the whole value, so always pass the complete list. Secrets are
  write-only — to check what is currently allowed, send a preflight and see whether the
  origin is echoed back:

  ```bash
  curl -si -X OPTIONS https://api.opentela.ai/manage/keys \
    -H "Origin: https://opentela.ai" -H "Access-Control-Request-Method: GET" \
    | grep -i access-control-allow-origin
  ```

  As of 2026-07-28 the allowlist is `https://opentela.ai` and `http://localhost:3000`.
  `docs.opentela.ai` is **not** allowed — `/account` there cannot manage `sk-` keys.

**Pages:**
- `/account` — wallet + Neon Auth login, API-key management (wallet keys + `sk-` keys), and a live `/v1/models` services catalog.

The OpenTela binary source code is available at [eth-easl/OpenTela](https://github.com/eth-easl/OpenTela).

## License

MIT
