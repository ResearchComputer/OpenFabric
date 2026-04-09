# Getting Started

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

otela-fleet looks for cluster configs in this order:

1. `./clusters/` in the current directory (for project-local configs)
2. `~/.config/opentela/fleet/clusters/` (user-level configs)

You can override this with `--cluster-dir`:

```bash
otela-fleet --cluster-dir /path/to/clusters start jsc ...
```

### Setting up the config directory

```bash
mkdir -p ~/.config/opentela/fleet/clusters
```

Copy or create cluster YAML files in this directory. See [cluster-config.md](cluster-config.md) for the format.

## Quick Start

### 1. List available clusters

```bash
otela-fleet clusters
```

Output:
```
Clusters (~/.config/opentela/fleet/clusters):
  jsc  (amd64, apptainer)  presets: A100_4, A100_8_multinode, A100_4_dev
  euler  (amd64, apptainer)  presets: RTX3090_1
```

### 2. List presets for a cluster

```bash
otela-fleet presets jsc
```

Output:
```
Presets for jsc:
  A100_4
    partition: booster  account: my-account
    gpus: 4  1 node  time: 04:00:00
    cpus_per_task: 48
  A100_4_dev
    partition: develbooster  account: my-account
    gpus: 4  1 node  time: 00:30:00
```

### 3. Start a serving job

```bash
otela-fleet start jsc \
  --backend sglang \
  --cmd "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port \$SERVICE_PORT --host 127.0.0.1" \
  --preset A100_4_dev \
  --replicas 1
```

The fleet manager will:
- Sync the OpenTela binary to the cluster
- Ensure the relay is running (if configured)
- Submit a SLURM job that runs your command inside the container

### 4. Check status

```bash
otela-fleet status jsc
```

### 5. View logs

```bash
otela-fleet logs jsc 12345
```

### 6. Stop a job

```bash
# Stop a specific job
otela-fleet stop jsc 12345

# Stop all opentela jobs on the cluster
otela-fleet stop jsc
```

## Environment Variables in Commands

Your `--cmd` runs inside the container with these environment variables available:

| Variable | Description |
|----------|-------------|
| `$SERVICE_PORT` | Port the backend should listen on (from cluster config `worker.service_port`) |
| `$HF_HOME` | Hugging Face cache directory (from cluster config `container.hf_cache`) |

## Next Steps

- [Cluster Configuration](cluster-config.md) -- how to write cluster YAML files
- [Fleet Apply](fleet-apply.md) -- declarative multi-cluster deployments
