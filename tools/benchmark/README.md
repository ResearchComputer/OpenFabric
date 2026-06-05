# OpenTela Scaling Benchmark Tools

Tools to analyze how OpenTela scales as the number of nodes increases.

## Setup

From the project root:

```bash
uv venv .venv
source .venv/bin/activate
uv pip install -r tools/benchmark/requirements.txt
```

## Tools

### `routing_overhead.py` — Routing Latency vs. Cluster Size

Measures the overhead of routing requests through the P2P mesh by sending echo requests through the head node's service proxy endpoint.

```bash
# Single run against a live cluster:
python routing_overhead.py --requests 500 --concurrency 20

# Generate scaling plots from collected results:
python routing_overhead.py --plot --results-dir results/20260306_120000
```

**Outputs**: `routing_<cluster_size>.json` with latency percentiles (avg, p50, p90, p99), throughput, and success rate.

### `discovery_speed.py` — Peer Discovery Timeline

Polls the head node's DNT stats to track how quickly nodes discover each other after the cluster starts.

```bash
# Measure discovery for a 51-node cluster (1 head + 50 workers):
python discovery_speed.py --expected-nodes 51 --timeout 300

# Plot discovery curves from multiple runs:
python discovery_speed.py --plot --results-dir results/20260306_120000
```

**Outputs**: `discovery_<expected_nodes>.json` with a time-series of peers discovered, plus time-to-full-discovery and discovery rates.

### `run_scaling_suite.sh` — Full Automated Sweep

Orchestrates the entire scaling analysis end-to-end: for each cluster size, it starts a Docker Compose simulation, runs both benchmarks, tears down, and generates combined plots.

```bash
# Run with default sizes (10, 25, 50, 100):
./run_scaling_suite.sh

# Run with custom sizes:
./run_scaling_suite.sh 5 10 25 50 100 200
```

**Prerequisites**: Docker images must be built first. Run from the `tools/benchmark/` directory.

**Outputs** (in `results/<timestamp>/`):
- `routing_*.json` — per-size routing results
- `discovery_*.json` — per-size discovery timelines
- `routing_latency_scaling.png` — latency vs. cluster size
- `routing_throughput_scaling.png` — throughput vs. cluster size
- `discovery_curves.png` — peers discovered vs. time (one line per size)
- `discovery_time_scaling.png` — time-to-full-discovery vs. cluster size
- `*_summary.txt` — tabular summaries

## Architecture

These tools use the existing [simulation infrastructure](../../local-demo/simulation/):
- **Head node**: `localhost:8092` with DNT APIs (`/v1/dnt/stats`, `/v1/dnt/table`)
- **Service routing**: `POST /v1/service/llm/v1/echo` proxies through the P2P mesh
- **Workers**: Run a mock echo server that returns the request payload
