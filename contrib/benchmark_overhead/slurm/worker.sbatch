#!/bin/bash
#SBATCH --job-name=otela-bench-worker
#SBATCH --account=a-infra02
#SBATCH --output=/capstor/store/cscs/swissai/infra02/xyao/rcc/logs/%x-%j.out
#SBATCH --error=/capstor/store/cscs/swissai/infra02/xyao/rcc/logs/%x-%j.err
# Other SBATCH flags (partition, gres, time, environment) are passed by deploy.py via --export / command line.

set -euo pipefail

# --- Inputs (passed by deploy.py via --export) ---
: "${OTELA_BIN:?OTELA_BIN must be set (path inside Pyxis container, e.g. /ocfbin/otela-bench-arm)}"
: "${MODEL:?MODEL must be set}"
: "${TP_SIZE:?TP_SIZE must be set}"
: "${RUN_ID:?RUN_ID must be set}"
: "${BENCH_STATE_DIR:?BENCH_STATE_DIR must be set}"
: "${BOOTSTRAP_ADDR:?BOOTSTRAP_ADDR must be set (URL to /v1/dnt/bootstraps)}"

mkdir -p "${BENCH_STATE_DIR}/${RUN_ID}"
STATE_FILE="${BENCH_STATE_DIR}/${RUN_ID}/${MODEL//\//_}.json"

# Pick a free port for vLLM. otela's HTTP/libp2p ports use defaults (8092/43905);
# safe because each sbatch task gets its own node.
pick_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()'
}
VLLM_PORT=$(pick_port)
OTELA_PORT=8092
NODE=$(hostname -f)

echo "[${MODEL}] node=${NODE} vllm_port=${VLLM_PORT} otela_http_port=${OTELA_PORT}"

# Write the state file BEFORE the worker comes up so deploy.py can read it
# during the readiness wait.
cat >"${STATE_FILE}" <<EOF
{
  "node": "${NODE}",
  "sglang_port": ${VLLM_PORT},
  "otela_port": ${OTELA_PORT},
  "job_id": "${SLURM_JOB_ID}",
  "model": "${MODEL}",
  "run_id": "${RUN_ID}"
}
EOF
# (Field name kept as `sglang_port` for Python deploy module compatibility;
# the value is the vLLM port — vLLM exposes the same OpenAI-compatible
# /v1/chat/completions endpoint.)

# vLLM command (matches contrib/benchmark/cli/templates/slurm_job_consolidated.j2).
VLLM_CMD="vllm serve ${MODEL} --tensor-parallel-size ${TP_SIZE} --port ${VLLM_PORT} --no-enable-chunked-prefill --no-enable-prefix-caching --disable-cascade-attn --async-scheduling"

# observability.timing_headers via env var (otela reads at startup).
export OF_OBSERVABILITY_TIMING_HEADERS=true

# Explicit GPU visibility for vLLM TP>1. Slurm already sets CUDA_VISIBLE_DEVICES
# from --gres=gpu:N, but the in-cgroup mapping makes the visible IDs 0..N-1.
# Make it explicit so NCCL sees the right device set; also enable NCCL warnings.
if [ "${TP_SIZE}" != "1" ]; then
  # Let Slurm's CUDA_VISIBLE_DEVICES stand — overriding it can map to GPUs
  # we weren't actually allocated.
  export NCCL_DEBUG=INFO
  # Intra-node TP only — the GH200 GPUs share NVLink. The Pyxis env's
  # aws_ofi_nccl hook doesn't activate under sbatch --environment, so
  # pin NCCL to its built-in Socket transport for any inter-rank
  # coordination, with NVLink/SHM enabled for the bulk traffic.
  export NCCL_NET_PLUGIN=none   # don't try to dlopen aws_ofi_nccl
  export NCCL_NET=Socket        # built-in transport for bootstrap
  export NCCL_IB_DISABLE=1
  export NCCL_P2P_DISABLE=0
  export NCCL_SHM_DISABLE=0
  echo "[${MODEL}] TP=${TP_SIZE} CUDA_VISIBLE_DEVICES=${CUDA_VISIBLE_DEVICES:-unset}"
fi

# Per-job otela config dir on a container-mounted path so libp2p keys persist
# in a known location and don't collide with other workers in the mesh.
CONF_DIR="${BENCH_STATE_DIR}/${RUN_ID}/otela-${MODEL//\//_}"
mkdir -p "${CONF_DIR}"
echo "[${MODEL}] otela config dir: ${CONF_DIR}"

# Run otela directly inside the sbatch-level Pyxis container.
# (Nested srun inside Pyxis fails — the container's glibc is older than what
# slurm's auth plugin needs.) TP>1 NCCL setup is therefore not viable here;
# stick to TP=1 per model.
exec "${OTELA_BIN}" start \
  --config-dir "${CONF_DIR}" \
  --bootstrap.addr "${BOOTSTRAP_ADDR}" \
  --subprocess "${VLLM_CMD}" \
  --service.name llm \
  --service.port "${VLLM_PORT}"
