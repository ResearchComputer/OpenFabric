#!/bin/bash
#SBATCH --job-name=convbench
#SBATCH --ntasks-per-node=1
#SBATCH --cpus-per-task=2
#SBATCH --mem-per-cpu=2G
#SBATCH --time=00:20:00
#SBATCH --output=%x-%j.out
#SBATCH --error=%x-%j.err
# --nodes, --account passed via sbatch CLI by submit.py.

set -euo pipefail

module purge
module load eth_proxy

# Exported by submit.py via --export
: "${RUN_ID:?RUN_ID must be set}"
: "${N:?N must be set}"
: "${REP:?REP must be set}"
: "${RUN_DIR:?RUN_DIR must be set}"
: "${CONFIG_PATH:?CONFIG_PATH must be set}"

cd "$(dirname "$CONFIG_PATH")/.."

srun --kill-on-bad-exit=1 \
     uv run python -m benchmark_convergence.task_entry
