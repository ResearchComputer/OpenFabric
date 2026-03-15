#!/bin/bash
# Setup script: build OpenTela and transfer to Euler
# Run this locally before submitting the SLURM job.
#
# Usage: bash deploy/euler/setup.sh

set -euo pipefail

EULER_USER="${EULER_USER:-xiayao}"
EULER_HOST="${EULER_HOST:-euler.ethz.ch}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "=== Building OpenTela binary (linux/amd64) ==="
cd "$PROJECT_DIR/src"
GOOS=linux GOARCH=amd64 make build
echo "Binary built: src/build/entry"

echo ""
echo "=== Transferring to Euler ==="
ssh "${EULER_USER}@${EULER_HOST}" 'mkdir -p ~/opentela ~/logs ~/.config/opentela'
scp "$PROJECT_DIR/src/build/entry" "${EULER_USER}@${EULER_HOST}:~/opentela/entry"
scp "$SCRIPT_DIR/worker.cfg.yaml" "${EULER_USER}@${EULER_HOST}:~/.config/opentela/cfg.yaml"
scp "$SCRIPT_DIR/job.sh" "${EULER_USER}@${EULER_HOST}:~/opentela/job.sh"
ssh "${EULER_USER}@${EULER_HOST}" 'chmod +x ~/opentela/entry'

echo ""
echo "=== Done ==="
echo "Files transferred to Euler:"
echo "  ~/opentela/entry          - OpenTela binary"
echo "  ~/opentela/job.sh         - SLURM job script"
echo "  ~/.config/opentela/cfg.yaml - Worker config"
echo ""
echo "Next steps on Euler:"
echo "  ssh ${EULER_USER}@${EULER_HOST}"
echo "  cd ~/opentela && mkdir -p ~/logs && sbatch job.sh"
echo "  myjobs  # monitor the job"
