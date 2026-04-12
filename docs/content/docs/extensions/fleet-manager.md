# Fleet Manager

The OpenTela fleet manager helps you launch, inspect, and reconcile serving
workloads across one or more SLURM-backed clusters. It wraps cluster
configuration, job submission, and multi-cluster deployment into a single CLI:
`otela-fleet`.

## Installation

```bash
pip install otela-fleet
```

Or from source:

```bash
cd contrib/fleet_manager
pip install -e .
```

## Configuration Directory

`otela-fleet` looks for cluster configs in this order:

1. `./clusters/` in the current directory
2. `~/.config/opentela/fleet/clusters/`

You can override this with `--cluster-dir`:

```bash
otela-fleet --cluster-dir /path/to/clusters start jsc ...
```

To create the default user-level directory:

```bash
mkdir -p ~/.config/opentela/fleet/clusters
```

Each cluster is defined by a YAML file in that directory. The filename, without
the `.yaml` suffix, becomes the cluster name you use in CLI commands.

## Quick Start

### List available clusters

```bash
otela-fleet clusters
```

Example output:

```text
Clusters (~/.config/opentela/fleet/clusters):
  jsc  (amd64, apptainer)  presets: A100_4, A100_8_multinode, A100_4_dev
  euler  (amd64, apptainer)  presets: RTX3090_1
```

### Inspect presets for a cluster

```bash
otela-fleet presets jsc
```

Example output:

```text
Presets for jsc:
  A100_4
    partition: booster  account: my-account
    gpus: 4  1 node  time: 04:00:00
    cpus_per_task: 48
  A100_4_dev
    partition: develbooster  account: my-account
    gpus: 4  1 node  time: 00:30:00
```

### Start a serving job

```bash
otela-fleet start jsc \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port \$SERVICE_PORT --host 127.0.0.1" \
  --preset A100_4_dev \
  --replicas 1
```

The fleet manager will:

- Sync the OpenTela binary to the cluster
- Ensure the relay is running, if the cluster requires one
- Submit a SLURM job that runs your command inside the configured container

### Check status, logs, and stop jobs

```bash
otela-fleet status jsc
otela-fleet logs jsc 12345
otela-fleet stop jsc 12345
otela-fleet stop jsc
```

The last command stops all OpenTela jobs on the cluster.

## Environment Variables Available to `--cmd`

Your `--cmd` runs inside the container with these variables available:

| Variable | Description |
|----------|-------------|
| `$SERVICE_PORT` | Port the backend should listen on, from `worker.service_port` |
| `$HF_HOME` | Hugging Face cache directory, from `container.hf_cache` |

## Cluster Configuration

Cluster configs can live in:

- `./clusters/`
- `~/.config/opentela/fleet/clusters/`

### Full example

```yaml
name: jsc

ssh:
  host: jsc-login
  host_any: jsc-login

arch: amd64

binary:
  local_path: ../binaries/otela-amd64
  remote_path: ~/opentela/otela

relay:
  seed: "jsc-relay-seed"
  peer_id: "12D3KooW..."
  host_ip: 10.0.0.1
  port: 43905
  tcp_port: 43906
  udp_port: 43907
  home_override: /tmp/opentela-relay
  bootstrap:
    - "/ip4/1.2.3.4/tcp/43905/p2p/12D3KooW..."
  skip: false

worker:
  seed: "jsc-worker-seed"
  port: 43910
  service_port: 8000

modules:
  - GCC
  - CUDA/12

container:
  runtime: apptainer
  image: oras://ghcr.io/org/sglang:latest
  sif_path: ~/opentela/sglang.sif
  pull_if_missing: true
  hf_cache: /tmp/hf_cache
  mounts:
    - /tmp:/tmp
  env:
    NCCL_SOCKET_IFNAME: ib0
  env_from_host:
    - HPC_SDK_PATH
  apptainer_flags:
    - "--nv"
    - "--containall"

security:
  require_signed_binary: false

solana:
  skip_verification: true

presets:
  A100_4:
    partition: booster
    account: my-account
    time: "04:00:00"
    gpus: 4
    cpus_per_task: 48
    nodes: 1
    extra_sbatch:
      - "#SBATCH --exclusive"

  A100_8_multinode:
    partition: booster
    account: my-account
    time: "08:00:00"
    gpus: 4
    cpus_per_task: 48
    nodes: 2
    extra_sbatch:
      - "#SBATCH --exclusive"

  A100_4_dev:
    partition: develbooster
    account: my-account
    time: "00:30:00"
    gpus: 4
    cpus_per_task: 48
    nodes: 1
```

### Required fields

| Field | Description |
|-------|-------------|
| `name` | Cluster identifier |
| `ssh.host` | SSH hostname for relay operations |
| `arch` | CPU architecture: `amd64` or `arm64` |
| `binary.local_path` | Local path to the OpenTela binary |
| `binary.remote_path` | Remote path to deploy the binary |
| `relay.*` | Relay node configuration, including seed, peer ID, ports, and bootstrap peers |
| `worker.*` | Worker configuration, including seed, port, and service port |
| `container.runtime` | Container runtime: `apptainer` or `enroot` |
| `container.image` | Container image URI |
| `presets` | At least one hardware preset |

### Container runtimes

#### Apptainer

Requires `container.sif_path`. The fleet manager runs:

```bash
apptainer exec [flags] --bind [mounts] [sif_path] [your_command]
```

#### Enroot

Requires `container.edf_template` and `container.edf_remote_path`. The fleet
manager runs:

```bash
srun --environment=[edf_path] [your_command]
```

### Presets

Each preset defines the SLURM parameters for a deployment:

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `partition` | yes | | SLURM partition |
| `account` | yes | | SLURM account |
| `time` | yes | | Job time limit in `HH:MM:SS` |
| `gpus` | yes | | GPU count or type, for example `4` or `"rtx_3090:1"` |
| `nodes` | no | `1` | Number of nodes. Values above `1` trigger the multi-node template |
| `cpus_per_task` | no | none | CPUs per task |
| `extra_sbatch` | no | `[]` | Additional `#SBATCH` lines |

### Multi-node presets

When `nodes > 1`, the fleet manager automatically:

- Discovers the master node from `$SLURM_NODELIST`
- Sets up NCCL environment variables from `container.env`
- Wraps your command in `srun --ntasks-per-node=1` with a per-node launcher
- Checks health on the master node

Your `--cmd` should still include any distributed arguments required by the
backend, such as `--nnodes` or `--node-rank`.

## Example Cluster Configurations

### ETH Euler

Apptainer on `amd64` with an RTX 3090 preset:

```yaml
name: euler

ssh:
  host: euler

arch: amd64

binary:
  local_path: ./binaries/otela-amd64
  remote_path: ~/opentela/entry

relay:
  seed: "99"
  peer_id: QmV4B8rADS7ygMQ37tSNQnDHX9ujmYEZBEDVSkkxavvxnZ
  host_ip: "129.132.93.93"
  port: "18092"
  tcp_port: "18905"
  udp_port: "18820"
  home_override: /tmp/opentela-relay
  bootstrap:
    - "/ip4/140.238.223.116/tcp/43905/p2p/QmPneGvHmWMngc8BboFasEJQ7D2aN9C65iMDwgCRGaTazs"
    - "/ip4/152.67.64.117/tcp/43905/p2p/Qmf8AY2HccRM9uLrR9qQdjwBM46qstT7dEFmfFX6RWD4AA"

worker:
  seed: "100"
  port: "8092"
  service_port: "30000"

modules:
  - "stack/2025-06"
  - "eth_proxy"

container:
  runtime: apptainer
  image: "lmsysorg/sglang:latest"
  sif_path: "~/containers/sglang.sif"
  pull_if_missing: true
  hf_cache: "$SCRATCH/.cache/huggingface"
  mounts:
    - "$SCRATCH:/scratch"
    - "$TMPDIR:/tmp"
  env:
    FLASHINFER_WORKSPACE_DIR: "$TMPDIR/sglang_cache/flashinfer"
    TRITON_CACHE_DIR: "$TMPDIR/sglang_cache/triton"
  apptainer_flags:
    - "--containall"
    - "--writable-tmpfs"
    - "--nv"

security:
  require_signed_binary: false

solana:
  skip_verification: true

presets:
  RTX3090_1:
    partition: null
    account: null
    time: "04:00:00"
    gpus: "rtx_3090:1"
    cpus_per_task: 8
    nodes: 1
    extra_sbatch:
      - "#SBATCH --mem-per-cpu=8G"
```

```bash
otela-fleet start euler \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port \$SERVICE_PORT --host 127.0.0.1" \
  --preset RTX3090_1
```

### JSC JUWELS Booster

This cluster uses WSS directly to head nodes, so the relay is skipped. It also
supports multi-node presets for larger deployments.

```yaml
name: jsc

ssh:
  host: jsc

arch: amd64

binary:
  local_path: ./binaries/otela-amd64
  remote_path: ~/opentela/entry

relay:
  seed: "299"
  peer_id: QmPneGvHmWMngc8BboFasEJQ7D2aN9C65iMDwgCRGaTazs
  host_ip: "127.0.0.1"
  port: "18092"
  tcp_port: "43900"
  udp_port: "18820"
  home_override: /tmp/opentela-relay
  skip: true
  bootstrap:
    - "https://bootstraps.opentela.ai/v1/dnt/bootstraps"

worker:
  seed: "300"
  port: "8092"
  service_port: "30000"

container:
  runtime: apptainer
  image: "lmsysorg/sglang:dev"
  sif_path: "/p/scratch/laionize/yao4/containers/sglang-dev.sif"
  pull_if_missing: true
  hf_cache: "/p/scratch/laionize/yao4/models"
  mounts:
    - "/p/scratch/laionize/yao4:/p/scratch/laionize/yao4"
    - "/p/home/jusers/yao4/juwels:/p/home/jusers/yao4/juwels"
  env:
    FLASHINFER_WORKSPACE_DIR: "/p/scratch/laionize/yao4/sglang_cache/flashinfer"
    TRITON_CACHE_DIR: "/p/scratch/laionize/yao4/sglang_cache/triton"
    TVM_FFI_CACHE_PATH: "/p/scratch/laionize/yao4/sglang_cache/tvm_ffi"
    XDG_CACHE_HOME: "/p/scratch/laionize/yao4/sglang_cache/xdg"
    TMPDIR: "/p/scratch/laionize/yao4/sglang_cache/tmp"
  apptainer_flags:
    - "--containall"
    - "--writable-tmpfs"
    - "--nv"

security:
  require_signed_binary: false

solana:
  skip_verification: true

presets:
  A100_4:
    partition: booster
    account: laionize
    time: "04:00:00"
    gpus: 4
    nodes: 1
    extra_sbatch:
      - "#SBATCH --gpus-per-node=4"

  A100_4_dev:
    partition: develbooster
    account: laionize
    time: "00:30:00"
    gpus: 4
    nodes: 1
    extra_sbatch:
      - "#SBATCH --gpus-per-node=4"

  A100_8_multinode:
    partition: booster
    account: laionize
    time: "08:00:00"
    gpus: 4
    nodes: 2
    extra_sbatch:
      - "#SBATCH --gpus-per-node=4"
```

```bash
# Single node with tensor parallelism
otela-fleet start jsc \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path Qwen/Qwen3-8B --port \$SERVICE_PORT --host 127.0.0.1 --tp-size 4" \
  --preset A100_4

# Multi-node deployment
otela-fleet start jsc \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path meta-llama/Llama-3-70B --port \$SERVICE_PORT --host 0.0.0.0 --tp 8 --nnodes 2" \
  --preset A100_8_multinode
```

### CSCS Clariden

This cluster uses enroot on `arm64`. A long-running relay already exists on the
cluster, so workers bootstrap directly from it.

```yaml
name: clariden

ssh:
  host: clariden-ln003
  host_any: clariden

arch: arm64

binary:
  local_path: ./binaries/otela-arm64
  remote_path: ~/opentela/otela

relay:
  seed: "199"
  peer_id: QmeUuaFBbFuHQa7mLo3VzywEaEN4wi4XDAhwBPPqZ8eG4Q
  host_ip: "148.187.108.172"
  port: "18092"
  tcp_port: "18905"
  udp_port: "18820"
  home_override: /tmp/opentela-relay
  skip: true
  bootstrap:
    - "/ip4/148.187.108.172/tcp/18905/p2p/QmeUuaFBbFuHQa7mLo3VzywEaEN4wi4XDAhwBPPqZ8eG4Q"

worker:
  seed: "200"
  port: "8092"
  service_port: "30000"

container:
  runtime: enroot
  image: "lmsysorg/sglang:latest"
  edf_template: clariden_sglang.toml.j2
  edf_remote_path: ~/.edf/sglang.toml
  hf_cache: "/capstor/store/cscs/swissai/a09/xyao/models"
  mounts:
    - "/users/xyao:/users/xyao"
    - "/iopsstor/scratch/cscs/xyao:/iopsstor/scratch/cscs/xyao"
    - "/capstor:/capstor"
  env:
    HF_HOME: "/capstor/store/cscs/swissai/a09/xyao/models"
  env_from_host:
    - HF_TOKEN

security:
  require_signed_binary: false

solana:
  skip_verification: true

presets:
  GH200_1:
    partition: debug
    account: infra02
    time: "01:00:00"
    gpus: 1
    nodes: 1
    extra_sbatch:
      - "#SBATCH --ntasks-per-node=1"
      - "#SBATCH --gpus-per-task=1"
```

```bash
otela-fleet start clariden \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port \$SERVICE_PORT --host 127.0.0.1" \
  --preset GH200_1
```

## Declarative Deployments with `otela-fleet apply`

For multi-cluster or repeatable deployments, define the desired state in a
fleet file and reconcile it with:

```bash
otela-fleet apply fleet.yaml
```

### Fleet file format

```yaml
deployments:
  - cluster: jsc
    backend: sglang
    cmd: "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port $SERVICE_PORT --host 127.0.0.1 --tp-size 4"
    preset: A100_4
    replicas: 2

  - cluster: jsc
    backend: vllm
    cmd: "python3 -m vllm.entrypoints.openai.api_server --model meta-llama/Llama-3-8B --port $SERVICE_PORT"
    preset: A100_4_dev
    replicas: 1

  - cluster: euler
    backend: sglang
    cmd: "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port $SERVICE_PORT --host 127.0.0.1"
    preset: RTX3090_1
    replicas: 1
```

### Dry run

```bash
otela-fleet apply fleet.yaml --dry-run
```

Example output:

```text
Fleet file: fleet.yaml
Clusters: euler, jsc

  jsc: 1 running jobs
  euler: 0 running jobs

Planned actions (3):
  + deploy sglang (A100_4) on jsc
  + deploy vllm (A100_4_dev) on jsc
  + deploy sglang (RTX3090_1) on euler

(dry run - no changes made)
```

### Reconciliation model

The fleet manager compares the desired state in the fleet file against the live
SLURM jobs:

- Too few replicas: submit additional jobs
- Too many replicas: cancel excess jobs, newest first
- Correct count: do nothing

### Job identity

Each deployment is identified by a hash of `backend + cmd + preset`. That means:

- Changing the command triggers a redeploy
- Changing the preset triggers a redeploy
- Changing only `replicas` scales the deployment without redeploying

### Scaling and removal

To scale a deployment, change `replicas` and apply again:

```yaml
deployments:
  - cluster: jsc
    backend: sglang
    cmd: "..."
    preset: A100_4
    replicas: 4
```

```bash
otela-fleet apply fleet.yaml
```

To remove a deployment, set `replicas: 0` or remove the entry and re-apply.
