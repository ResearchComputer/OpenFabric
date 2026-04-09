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

Cluster configs are YAML files stored in `~/.config/opentela/fleet/clusters/` (or `./clusters/` in the current directory).

## Documentation

- [Getting Started](docs/getting-started.md)
- [Cluster Configuration](docs/cluster-config.md)
- [Fleet Apply](docs/fleet-apply.md)
