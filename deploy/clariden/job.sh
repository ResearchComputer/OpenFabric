#!/bin/bash
#SBATCH --job-name=opentela-sglang
#SBATCH --account=infra02
#SBATCH --nodes=1
#SBATCH --ntasks-per-node=1
#SBATCH --gpus-per-task=1
#SBATCH --time=01:00:00
#SBATCH --partition=debug
#SBATCH --output=logs/opentela_%j.out
#SBATCH --error=logs/opentela_%j.err

set -euo pipefail

# --- Configuration ---
MODEL="Qwen/Qwen3-0.6B"
SGLANG_PORT=30000
OTELA_BINARY="$HOME/opentela/otela"
OTELA_CONFIG="$HOME/.config/opentela/cfg.yaml"

# --- Start sglang server inside container ---
echo "Starting sglang server for model $MODEL on port $SGLANG_PORT..."
srun --environment=$HOME/.edf/sglang.toml \
    python3 -m sglang.launch_server \
        --model-path "$MODEL" \
        --port "$SGLANG_PORT" \
        --host 127.0.0.1 \
        --trust-remote-code \
        --skip-server-warmup &

SGLANG_PID=$!

# --- Wait for sglang to be ready ---
echo "Waiting for sglang to be ready..."
for i in $(seq 1 180); do
    if curl -s "http://localhost:${SGLANG_PORT}/v1/models" > /dev/null 2>&1; then
        echo "sglang is ready after ${i}s"
        break
    fi
    if ! kill -0 "$SGLANG_PID" 2>/dev/null; then
        echo "ERROR: sglang process died"
        exit 1
    fi
    sleep 1
done

# Verify sglang is serving
if ! curl -s "http://localhost:${SGLANG_PORT}/v1/models" > /dev/null 2>&1; then
    echo "ERROR: sglang failed to start within 180s"
    kill "$SGLANG_PID" 2>/dev/null || true
    exit 1
fi

echo "sglang models:"
curl -s "http://localhost:${SGLANG_PORT}/v1/models" | python3 -m json.tool 2>/dev/null || true

# --- Start OpenTela worker (native binary, no container) ---
echo "Starting OpenTela worker..."
"$OTELA_BINARY" start --config "$OTELA_CONFIG" &
OTELA_PID=$!

echo "OpenTela worker started (PID=$OTELA_PID)"
echo "sglang server running (PID=$SGLANG_PID)"
echo "Job is running on $(hostname)"

# --- Wait for both processes ---
wait -n "$SGLANG_PID" "$OTELA_PID" 2>/dev/null
EXIT_CODE=$?

echo "A process exited with code $EXIT_CODE, shutting down..."
kill "$SGLANG_PID" "$OTELA_PID" 2>/dev/null || true
wait
exit $EXIT_CODE
