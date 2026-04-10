# otela-fleet

Fleet manager for OpenTela deployments across HPC clusters.

Deploy LLM serving backends (sglang, vLLM, etc.) to SLURM clusters with a single command. You provide the serving command; the fleet manager handles container execution, health checks, process supervision, and OpenTela worker coordination.

## Install

```bash
pip install otela-fleet
```

## Usage

```bash
# List clusters and presets
otela-fleet clusters
otela-fleet presets jsc

# Start a serving job
otela-fleet start jsc \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port \$SERVICE_PORT --host 127.0.0.1" \
  --preset A100_4 \
  --replicas 2

# Check status and logs
otela-fleet status jsc
otela-fleet logs jsc 12345

# Stop jobs
otela-fleet stop jsc 12345

# Declarative deployment
otela-fleet apply fleet.yaml --dry-run
otela-fleet apply fleet.yaml
```

## Configuration

Cluster configs are YAML files stored in `~/.config/opentela/fleet/clusters/` (or `./clusters/` in the current directory). Each cluster file contains:

- **infrastructure**: SSH host, relay settings, container runtime, mounts
- **presets**: named hardware/SLURM configurations (`A100_4`, `rtx3090_1`, etc.). Each preset specifies partition, account, time, gpus, nodes, etc.
- **proxychains** *(optional)*: SSH SOCKS tunnel for compute nodes without direct internet (e.g. JSC `booster`). The tunnel is skipped automatically on partitions listed in `skip_partitions`.

See `clusters/jsc.yaml` for a fully-featured example.

### User command

The `--cmd` flag is passed verbatim to the container. Inside the container, the command can reference:

- `$SERVICE_PORT` — where the backend should listen (the OpenTela worker polls this port for readiness)
- `$HF_HOME` — Hugging Face cache directory

### Multi-node

If a preset sets `nodes: N` with `N > 1`, the fleet manager picks the multi-node template automatically. The user's `--cmd` is launched on every node via `srun --ntasks-per-node=1`. `$MASTER_ADDR`, `$MASTER_PORT`, `$NNODES`, and `$NODE_RANK` are exported for the backend to consume.
