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

## Alternative: SSH-based driver (no SLURM)

For arbitrary SSH-reachable nodes (pre-allocated VMs, hand-picked Euler compute
nodes via `salloc`, the existing test mesh, etc.). No shared filesystem
required — the driver captures the bootstrap multiaddr from the coordinator's
stdout and passes it to observers via env var.

1. Write a `nodes.txt`, one hostname per line. The **first line is the
   coordinator** (also runs an otela node). Comments with `#` allowed.
   ```
   # nodes.txt
   ocf-1
   ocf-2
   eu-a2p-481
   eu-a2p-486
   ```
2. Build otela locally (only on your laptop):
   ```
   cd src && make build
   ```
3. Run one cell:
   ```
   cd contrib/benchmark_convergence
   uv sync --extra dev
   python -m benchmark_convergence run-ssh \
       --nodes nodes.txt \
       --otela-bin ../../src/build/entry \
       --run-id "$(date +%Y%m%d)-ssh-001"
   ```
   The driver scp's the binary + rsyncs the package to `~/.opentela-bench` on
   each node, runs `pip install --user httpx pyyaml` (idempotent), spawns
   coord + observers, and on completion copies `results.json` back into
   `./runs/<run_id>/N=<n>/rep=0/`.
4. Aggregate and report (same as the SLURM path):
   ```
   python -m benchmark_convergence aggregate runs/<run_id>
   python -m benchmark_convergence report runs/<run_id>
   ```

Requirements per remote node: SSH key auth set up, Python 3.11+, `pip`
available, and outbound network from each node to peer nodes on the chosen
libp2p TCP port (default 43905) and the coordinator TCP port (default 47001).
