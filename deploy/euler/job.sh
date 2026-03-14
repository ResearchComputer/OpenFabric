#!/bin/bash
#SBATCH --job-name=opentela-sglang
#SBATCH --ntasks=1
#SBATCH --cpus-per-task=8
#SBATCH --mem-per-cpu=8G
#SBATCH --gpus=rtx_3090:1
#SBATCH --time=04:00:00
#SBATCH --output=logs/opentela_%j.out
#SBATCH --error=logs/opentela_%j.err

set -euo pipefail

# --- Configuration ---
MODEL="Qwen/Qwen3-0.6B"
SGLANG_PORT=30000
OTELA_BINARY="$HOME/opentela/entry"
OTELA_CONFIG="$HOME/.config/opentela/cfg.yaml"
SIF="$HOME/containers/sglang.sif"
HF_CACHE="$SCRATCH/.cache/huggingface"

# --- Setup ---
module purge
module load stack/2025-06
module load eth_proxy

mkdir -p "$HF_CACHE"

# Writable cache directories (container filesystem is read-only)
CACHE_DIR="$TMPDIR/sglang_cache"
mkdir -p "$CACHE_DIR/home" "$CACHE_DIR/flashinfer" "$CACHE_DIR/triton"

# --- Pull sglang container if not present ---
if [ ! -f "$SIF" ]; then
    echo "Pulling sglang container (one-time)..."
    mkdir -p "$HOME/containers"
    apptainer pull "$SIF" docker://lmsysorg/sglang:latest
fi

# --- Start sglang server ---
echo "Starting sglang server for model $MODEL on port $SGLANG_PORT..."
apptainer exec --nv \
    --containall \
    --writable-tmpfs \
    --bind "$SCRATCH:/scratch" \
    --bind "$TMPDIR:/tmp" \
    --bind "$HF_CACHE:$HF_CACHE" \
    --env HF_HOME="$HF_CACHE" \
    --env FLASHINFER_WORKSPACE_DIR="$CACHE_DIR/flashinfer" \
    --env TRITON_CACHE_DIR="$CACHE_DIR/triton" \
    "$SIF" \
    python3 -m sglang.launch_server \
        --model-path "$MODEL" \
        --port "$SGLANG_PORT" \
        --host 127.0.0.1 \
        --trust-remote-code \
        --skip-server-warmup &

SGLANG_PID=$!

# --- Wait for sglang to be ready ---
echo "Waiting for sglang to be ready..."
for i in $(seq 1 120); do
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
    echo "ERROR: sglang failed to start within 120s"
    kill "$SGLANG_PID" 2>/dev/null || true
    exit 1
fi

echo "sglang models:"
curl -s "http://localhost:${SGLANG_PORT}/v1/models" | python3 -m json.tool 2>/dev/null || true

# --- Start OpenTela worker ---
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
