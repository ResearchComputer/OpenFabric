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
