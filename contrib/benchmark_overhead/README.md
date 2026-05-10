# benchmark_overhead

Measures OpenTela routing overhead on top of LLM inference.

See `docs/superpowers/specs/2026-05-10-benchmark-overhead-design.md` for the full design.

## End-to-end runbook (Clariden login node)

1. Build OpenTela locally and copy to Clariden:
   ```
   cd src && make build && scp build/entry clariden:~/opentela/otela
   ```
2. Roll out the same binary to the CSCS k8s relay and set
   `OF_OBSERVABILITY_TIMING_HEADERS=true` on its deployment env.
3. On a Clariden login node:
   ```
   cd contrib/benchmark_overhead
   uv sync --extra dev
   python -m benchmark_overhead doctor \
       --head-url http://148.187.108.172:8092 \
       --config benchmark_overhead/config/default.yaml
   python -m benchmark_overhead deploy --config benchmark_overhead/config/default.yaml
   python -m benchmark_overhead run \
       --config benchmark_overhead/config/default.yaml \
       --output ~/runs
   python -m benchmark_overhead report ~/runs/<run_id>
   ```
4. Teardown when done:
   ```
   python -m benchmark_overhead deploy --config benchmark_overhead/config/default.yaml --teardown --run-id <run_id>
   ```

## Caveat

The OpenTela path egresses through the CSCS k8s ingress; the direct path stays inside Clariden. The A/B measures realistic deployment overhead, not pure libp2p forward cost.
