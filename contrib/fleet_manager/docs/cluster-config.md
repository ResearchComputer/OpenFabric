# Cluster Configuration

Each cluster is defined by a YAML file in the clusters directory. The file name (without `.yaml`) is the cluster name used in CLI commands.

## File Location

Place cluster configs in one of:
- `./clusters/` (project-local)
- `~/.config/opentela/fleet/clusters/` (user-level)

## Full Example

```yaml
name: jsc

ssh:
  host: jsc-login          # SSH host for relay operations
  host_any: jsc-login      # SSH host for SLURM submission (if different)

arch: amd64                 # amd64 or arm64

binary:
  local_path: ../binaries/otela-amd64    # local path to OpenTela binary
  remote_path: ~/opentela/otela          # where to deploy it on the cluster

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
  skip: false              # set true if using WSS direct to heads (no relay needed)

worker:
  seed: "jsc-worker-seed"
  port: 43910
  service_port: 8000       # port where the serving backend listens

modules:                    # modules to load before running
  - GCC
  - CUDA/12

container:
  runtime: apptainer       # apptainer or enroot
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
    gpus: 4                # per node
    cpus_per_task: 48
    nodes: 2               # multi-node job
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

## Required Fields

| Field | Description |
|-------|-------------|
| `name` | Cluster identifier |
| `ssh.host` | SSH hostname for relay operations |
| `arch` | CPU architecture: `amd64` or `arm64` |
| `binary.local_path` | Local path to the OpenTela binary |
| `binary.remote_path` | Remote path to deploy the binary |
| `relay.*` | Relay node configuration (seed, peer_id, ports, bootstrap) |
| `worker.*` | Worker configuration (seed, port, service_port) |
| `container.runtime` | Container runtime: `apptainer` or `enroot` |
| `container.image` | Container image URI |
| `presets` | At least one hardware preset |

## Container Runtimes

### Apptainer

Requires `container.sif_path`. The fleet manager runs:

```bash
apptainer exec [flags] --bind [mounts] [sif_path] [your_command]
```

### Enroot

Requires `container.edf_template` and `container.edf_remote_path`. The fleet manager runs:

```bash
srun --environment=[edf_path] [your_command]
```

## Presets

Each preset defines SLURM job parameters:

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `partition` | yes | | SLURM partition |
| `account` | yes | | SLURM account |
| `time` | yes | | Job time limit (HH:MM:SS) |
| `gpus` | yes | | GPU count or type (e.g., `4` or `"rtx_3090:1"`) |
| `nodes` | no | `1` | Number of nodes. >1 triggers multi-node template |
| `cpus_per_task` | no | none | CPUs per task |
| `extra_sbatch` | no | `[]` | Additional SBATCH lines (include `#SBATCH` prefix) |

### Multi-Node Presets

When `nodes > 1`, the fleet manager automatically:
- Discovers the master node from `$SLURM_NODELIST`
- Sets up NCCL environment variables from `container.env`
- Wraps your command in `srun --ntasks-per-node=1` with a per-node launcher
- Checks health on the master node

Your `--cmd` should include any distributed arguments needed by your backend (e.g., `--nnodes`, `--node-rank` for sglang).

## Examples

### ETH Euler (Apptainer, x86_64, RTX 3090)

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

Usage:

```bash
otela-fleet start euler \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port \$SERVICE_PORT --host 127.0.0.1" \
  --preset RTX3090_1
```

### JSC JUWELS Booster (Apptainer, A100, relay skipped)

This cluster uses WSS direct to head nodes, so the relay is skipped. Multi-node presets are available for large models.

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

Usage:

```bash
# Single-node with tensor parallelism
otela-fleet start jsc \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path Qwen/Qwen3-8B --port \$SERVICE_PORT --host 127.0.0.1 --tp-size 4" \
  --preset A100_4

# Multi-node (fleet manager handles NCCL, srun, master discovery)
otela-fleet start jsc \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path meta-llama/Llama-3-70B --port \$SERVICE_PORT --host 0.0.0.0 --tp 8 --nnodes 2" \
  --preset A100_8_multinode
```

### CSCS Clariden (Enroot, ARM64)

This cluster uses enroot as the container runtime instead of apptainer. Requires an EDF template.

```yaml
name: clariden

ssh:
  host: clariden-ln003       # fixed login node for relay
  host_any: clariden          # round-robin for SLURM submission

arch: arm64

binary:
  local_path: ./binaries/otela-arm64
  remote_path: ~/opentela/otela

relay:
  seed: "199"
  peer_id: QmNXYTKxCgREE5RKLk7cFwCGFmW66sb3T2CE8UUSHacW7g
  host_ip: "172.28.38.18"
  port: "18092"
  tcp_port: "18905"
  udp_port: "18820"
  home_override: /tmp/opentela-relay
  bootstrap:
    - "/ip4/140.238.223.116/tcp/43905/p2p/QmPneGvHmWMngc8BboFasEJQ7D2aN9C65iMDwgCRGaTazs"
    - "/ip4/152.67.64.117/tcp/43905/p2p/Qmf8AY2HccRM9uLrR9qQdjwBM46qstT7dEFmfFX6RWD4AA"

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

Usage:

```bash
otela-fleet start clariden \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port \$SERVICE_PORT --host 127.0.0.1" \
  --preset GH200_1
```
