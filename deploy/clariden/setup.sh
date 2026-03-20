#!/bin/bash
# Setup script: build OpenTela for ARM64 and transfer to Clariden
# Run this locally before submitting the SLURM job.
#
# Usage: bash deploy/clariden/setup.sh

set -euo pipefail

CLARIDEN_HOST="${CLARIDEN_HOST:-clariden}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "=== Building OpenTela binary (linux/arm64) ==="
cd "$PROJECT_DIR/src"
GOOS=linux GOARCH=arm64 make build
echo "Binary built: src/build/entry"

echo ""
echo "=== Transferring to Clariden ==="
ssh "${CLARIDEN_HOST}" 'mkdir -p ~/opentela ~/logs ~/.config/opentela ~/.edf'
scp "$PROJECT_DIR/src/build/entry" "${CLARIDEN_HOST}:~/opentela/otela"
scp "$SCRIPT_DIR/worker.cfg.yaml" "${CLARIDEN_HOST}:~/.config/opentela/cfg.yaml"
scp "$SCRIPT_DIR/job.sh" "${CLARIDEN_HOST}:~/opentela/job.sh"
scp "$SCRIPT_DIR/sglang.toml" "${CLARIDEN_HOST}:~/.edf/sglang.toml"
ssh "${CLARIDEN_HOST}" 'chmod +x ~/opentela/otela'

echo ""
echo "=== Done ==="
echo "Files transferred to Clariden:"
echo "  ~/opentela/otela             - OpenTela binary (arm64)"
echo "  ~/opentela/job.sh            - SLURM job script"
echo "  ~/.config/opentela/cfg.yaml  - Worker config"
echo "  ~/.edf/sglang.toml           - sglang container EDF"
echo ""
echo "Next steps on Clariden:"
echo "  ssh ${CLARIDEN_HOST}"
echo "  cd ~/opentela && sbatch job.sh"
echo "  squeue -u \$USER  # monitor the job"
