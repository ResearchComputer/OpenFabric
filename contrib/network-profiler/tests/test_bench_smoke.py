import json
import os
import subprocess
import sys
from pathlib import Path

import pytest


REPO_ROOT = Path(__file__).resolve().parents[3]
ENTRY = REPO_ROOT / "src" / "build" / "entry"


@pytest.mark.skipif(not ENTRY.exists(), reason="src/build/entry not built")
def test_local_two_node_bench(tmp_path):
    # The bench invokes `otela ...` over `bash -c`. The repo binary is
    # `src/build/entry`; expose it as `otela` via a bin/ dir we prepend
    # to PATH and pass through to bash explicitly.
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    (bin_dir / "otela").symlink_to(ENTRY)

    bash_path = f"PATH={bin_dir}:{os.environ.get('PATH', '')}; {{command}}"

    machines_json = tmp_path / "machines.json"
    machines_json.write_text(json.dumps({
        "remote_command": ["bash", "-c", bash_path],
        "machines": [
            {"name": "n1", "address": "127.0.0.1", "rcc_host": "n1", "http_port": 29090, "libp2p_port": 29091},
            {"name": "n2", "address": "127.0.0.1", "rcc_host": "n2", "http_port": 29100, "libp2p_port": 29101},
        ],
    }))

    env = os.environ.copy()
    env["PATH"] = f"{bin_dir}:{env['PATH']}"
    output = tmp_path / "out"

    proc = subprocess.run(
        [
            sys.executable, "-m", "network_profiler", "bench",
            "--config", str(machines_json),
            "--run-id", "smoke",
            "--output", str(output),
            "--latency-count", "3",
            "--throughput-count", "1",
            "--throughput-bytes", "65536",
        ],
        cwd=REPO_ROOT / "contrib" / "network-profiler",
        env=env,
        capture_output=True, text=True, timeout=300,
    )
    assert proc.returncode == 0, f"bench failed: {proc.stdout}\n{proc.stderr}"
    measurements = (output / "measurements.jsonl").read_text().splitlines()
    assert len(measurements) == 6  # 2 ordered pairs x 3 kinds
    records = [json.loads(l) for l in measurements]
    assert all(r["ok"] for r in records), \
        "\n".join(f"{r['source']}->{r['target']} {r['kind']}: {r.get('error')}" for r in records if not r["ok"])
    summary = json.loads((output / "run.json").read_text())
    assert summary["totals"]["ok"] == 6
