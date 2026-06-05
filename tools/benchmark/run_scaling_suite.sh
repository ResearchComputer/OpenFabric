#!/bin/bash
# run_scaling_suite.sh — Orchestrate the full OpenTela scaling benchmark.
#
# Runs discovery_speed.py and routing_overhead.py at each cluster size,
# then generates combined scaling plots.
#
# Usage:
#   ./run_scaling_suite.sh [sizes...]
#   ./run_scaling_suite.sh 10 25 50 100 200
#
# Prerequisites:
#   - Docker images must be built (run from local-demo/simulation/)
#   - Python venv with requirements.txt installed

set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SIMULATION_DIR="$(cd "$SCRIPT_DIR/../../local-demo/simulation" && pwd)"

# Default cluster sizes if none provided
SIZES="${@:-10 25 50 100}"

# Benchmark parameters
REQUESTS=500
CONCURRENCY=20
WARMUP=50
POLL_INTERVAL=1
DISCOVERY_TIMEOUT=300

HEAD_NODE_URL="http://localhost:8092"

# Results directory
TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
RESULTS_DIR="$SCRIPT_DIR/results/$TIMESTAMP"
mkdir -p "$RESULTS_DIR"

echo "================================================================"
echo " OpenTela Scaling Benchmark Suite"
echo " Sizes:       $SIZES"
echo " Results dir: $RESULTS_DIR"
echo " Simulation:  $SIMULATION_DIR"
echo "================================================================"
echo ""

# ── Helper functions ──────────────────────────────────────────────────────────

wait_for_head_node() {
    local timeout=60
    local elapsed=0
    echo "  Waiting for head node to be ready ..."
    while ! curl -s "$HEAD_NODE_URL/v1/health" > /dev/null 2>&1; do
        sleep 2
        elapsed=$((elapsed + 2))
        if [ "$elapsed" -ge "$timeout" ]; then
            echo "  ERROR: Head node not ready after ${timeout}s" >&2
            return 1
        fi
    done
    echo "  Head node ready (${elapsed}s)"
}

teardown_cluster() {
    echo "  Tearing down cluster ..."
    cd "$SIMULATION_DIR"
    docker compose down --remove-orphans --timeout 10 2>/dev/null || true
    # Brief pause to let ports free up
    sleep 3
}

# ── Main loop ─────────────────────────────────────────────────────────────────

# Ensure clean state
teardown_cluster

for N in $SIZES; do
    TOTAL_NODES=$((N + 1))  # N workers + 1 head node
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Cluster size: $N workers ($TOTAL_NODES total nodes)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # ── Start cluster ──
    echo "  Starting cluster with $N workers ..."
    cd "$SIMULATION_DIR"
    docker compose up --scale worker="$N" -d 2>&1 | tail -3

    # Wait for head node API
    wait_for_head_node

    # ── Discovery benchmark ──
    echo ""
    echo "  ── Discovery Benchmark ──"
    python3 "$SCRIPT_DIR/discovery_speed.py" \
        --head-node-url "$HEAD_NODE_URL" \
        --expected-nodes "$TOTAL_NODES" \
        --poll-interval "$POLL_INTERVAL" \
        --timeout "$DISCOVERY_TIMEOUT" \
        --output "$RESULTS_DIR/discovery_${TOTAL_NODES}.json"

    # ── Routing benchmark ──
    echo ""
    echo "  ── Routing Overhead Benchmark ──"
    # Brief settle time after discovery completes
    sleep 5
    python3 "$SCRIPT_DIR/routing_overhead.py" \
        --head-node-url "$HEAD_NODE_URL" \
        --requests "$REQUESTS" \
        --concurrency "$CONCURRENCY" \
        --warmup "$WARMUP" \
        --output "$RESULTS_DIR/routing_${TOTAL_NODES}.json"

    # ── Teardown ──
    teardown_cluster
done

# ── Generate combined plots ──────────────────────────────────────────────────

echo ""
echo "================================================================"
echo " Generating scaling plots ..."
echo "================================================================"
echo ""

python3 "$SCRIPT_DIR/routing_overhead.py" \
    --plot --results-dir "$RESULTS_DIR"

echo ""

python3 "$SCRIPT_DIR/discovery_speed.py" \
    --plot --results-dir "$RESULTS_DIR"

echo ""
echo "================================================================"
echo " Done! All results saved to: $RESULTS_DIR"
echo "================================================================"
echo ""
echo "Files:"
ls -la "$RESULTS_DIR/"
