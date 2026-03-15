# OpenTela CLI (`otela-cli`)

The `otela-cli` is a client tool designed to facilitate interaction with the OpenTela network, manage resources, and deploy workloads.

## Core Features & Subcommands

### 1. Authentication & Configuration (`auth`, `config`)
- **`otela-cli auth login / logout`**: Authenticate against the OpenTela network, potentially using Solana wallets or standard API keys.
- **`otela-cli auth whoami`**: Display current authentication status and active identity.
- **`otela-cli config set / get`**: Manage local configuration (RPC endpoints, default cluster, logging level).

### 2. Network & Resource Discovery (`list`, `info`)
- **`otela-cli list clusters`**: List available computational clusters in the network.
- **`otela-cli list nodes`**: Show individual nodes, their capabilities (e.g., LLM serving, compute), and current load.
- **`otela-cli list models`**: List available AI models hosted on the network.
- **`otela-cli info`**: Show general network statistics and health.

### 3. Cluster Management (`cluster`)
*Integrates the existing `cluster_manager` functionality.*
- **`otela-cli cluster create`**: Provision a new cluster based on a configuration file.
- **`otela-cli cluster scale`**: Add or remove nodes from an existing cluster.
- **`otela-cli cluster status`**: Monitor the health and metrics of a specific cluster.
- **`otela-cli cluster destroy`**: Tear down an existing cluster.

### 4. Workload Deployment (`deploy`, `run`)
- **`otela-cli deploy <manifest.yaml>`**: Deploy an LLM serving or dispatcher workload to the OpenTela network.
- **`otela-cli run <image>`**: Quickly spin up a single-node task or simulation.
- **`otela-cli logs <workload-id>`**: Stream logs from a running deployed workload.

### 5. Financial & Token Integration (`wallet`)
*Integrates with the Solana-based token program in `tokens/`.*
- **`otela-cli wallet balance`**: Check OpenTela token balance.
- **`otela-cli wallet transfer`**: Send tokens to another address.
- **`otela-cli wallet stake / unstake`**: Manage token staking for network participation or node operation.

## Technology Stack
- **Language**: Python
- **Dependency Management**: `uv`
- **CLI Framework**: `Typer`
- **API Communication**: `httpx` or `requests` for interacting with OpenTela endpoints.
- **Web3/Solana**: `solders` and `solana-py` for token and wallet operations.
