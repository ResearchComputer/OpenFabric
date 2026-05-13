# benchmark_convergence

Measures CRDT update-propagation latency across an N-node OpenTela mesh.

See `docs/superpowers/specs/2026-05-13-convergence-speed-design.md` for the design.

## End-to-end runbook (Euler login node)

1. Build `otela` locally and copy to Euler:
   ```
   cd src && make build && scp build/entry euler:$SCRATCH/opentela/otela
   ```
2. On Euler login node:
   ```
   cd contrib/benchmark_convergence
   uv sync --extra dev
   python -m benchmark_convergence doctor \
       --config benchmark_convergence/config/default.yaml
   python -m benchmark_convergence run \
       --config benchmark_convergence/config/default.yaml \
       --run-id "$(date +%Y%m%d)-conv-001"
   python -m benchmark_convergence aggregate \
       "$SCRATCH/<run_id>"
   python -m benchmark_convergence report "$SCRATCH/<run_id>"
   ```
3. Resume / retry:
   ```
   python -m benchmark_convergence run ... --resume
   python -m benchmark_convergence run ... --retry-failed
   ```
