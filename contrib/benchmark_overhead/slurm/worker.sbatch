#!/bin/bash
#SBATCH --job-name=otela-bench-worker
#SBATCH --output=%x-%j.out
#SBATCH --error=%x-%j.err
# Other SBATCH flags (partition, account, gres, time) are passed by deploy.py via --export and the sbatch command line.

set -euo pipefail

# --- Inputs (passed by deploy.py via --export) ---
: "${OTELA_BIN:?OTELA_BIN must be set}"
: "${SGLANG_SIF:?SGLANG_SIF must be set}"
: "${MODEL:?MODEL must be set}"
: "${RUN_ID:?RUN_ID must be set}"
: "${BENCH_STATE_DIR:?BENCH_STATE_DIR must be set}"
: "${BOOTSTRAP_URL:?BOOTSTRAP_URL must be set}"

mkdir -p "${BENCH_STATE_DIR}/${RUN_ID}"
STATE_FILE="${BENCH_STATE_DIR}/${RUN_ID}/${MODEL//\//_}.json"

# Pick two free ports by binding ephemeral sockets and immediately closing them.
pick_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()'
}
SGLANG_PORT=$(pick_port)
OTELA_PORT=$(pick_port)
NODE=$(hostname -f)

echo "Starting sglang for ${MODEL} on ${NODE}:${SGLANG_PORT}, otela worker on ${OTELA_PORT}"

# Refresh bootstraps from the head.
BOOTSTRAPS=$(curl -fsSL "${BOOTSTRAP_URL}" | python3 -c 'import sys,json; print(" ".join(f"--bootstrap {b}" for b in json.load(sys.stdin)))')

# Start sglang in background.
apptainer run --nv \
  --bind /capstor:/capstor \
  "${SGLANG_SIF}" \
  python3 -m sglang.launch_server \
    --model-path "${MODEL}" \
    --host 0.0.0.0 \
    --port "${SGLANG_PORT}" \
  >sglang.log 2>&1 &
SGLANG_PID=$!

# Wait for sglang to come up.
for i in $(seq 1 120); do
  if curl -fsS "http://127.0.0.1:${SGLANG_PORT}/v1/models" >/dev/null 2>&1; then
    echo "sglang up after ${i}s"
    break
  fi
  sleep 1
  if ! kill -0 "$SGLANG_PID" 2>/dev/null; then
    echo "sglang died during startup; see sglang.log" >&2
    exit 1
  fi
done

# Write the state file BEFORE starting otela so deploy.py can find it.
cat >"${STATE_FILE}" <<EOF
{
  "node": "${NODE}",
  "sglang_port": ${SGLANG_PORT},
  "otela_port": ${OTELA_PORT},
  "job_id": "${SLURM_JOB_ID}",
  "model": "${MODEL}",
  "run_id": "${RUN_ID}"
}
EOF

# Build a per-job otela config dir and config file.
CONF_DIR="${BENCH_STATE_DIR}/${RUN_ID}/otela-${MODEL//\//_}"
mkdir -p "${CONF_DIR}"
cat >"${CONF_DIR}/cfg.yaml" <<EOF
name: bench-worker-${MODEL//\//_}
port: "${OTELA_PORT}"
role: worker
mode: full
loglevel: info
cleanslate: true
service:
  name: llm
  port: "${SGLANG_PORT}"
security:
  require_signed_binary: false
observability:
  timing_headers: true
billing:
  enabled: false
EOF

# Run otela in foreground (so sbatch holds the allocation until we exit).
exec "${OTELA_BIN}" start --config-dir "${CONF_DIR}" --component server ${BOOTSTRAPS}
