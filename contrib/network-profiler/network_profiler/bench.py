from __future__ import annotations

from .model import Machine
from .remote import RemoteRunner


def bench_dir(run_id: str, host: str) -> str:
    """Per-host bench dir. Suffixing by host lets multiple nodes share
    one filesystem (e.g. localhost smoke test) without colliding."""
    return f"/tmp/otela-bench-{run_id}-{host}"


def phase_init(runner: RemoteRunner | object, machines: list[Machine], run_id: str) -> dict[str, str]:
    """Run `otela init --config-dir <dir>` on each host. Returns host -> rendered command."""
    out: dict[str, str] = {}
    for m in machines:
        d = bench_dir(run_id, m.name)
        cmd = f"mkdir -p {d} && otela init --config-dir {d}"
        result = runner.run(m, cmd, timeout=60)
        if result.returncode != 0:
            raise RuntimeError(f"phase_init failed on {m.name}: {result.stderr or result.stdout}")
        out[m.name] = result.command
    return out


def phase_discover(runner: RemoteRunner | object, machines: list[Machine], run_id: str) -> dict[str, str]:
    """Run `otela peer-id --config-dir <dir>` on each host. Returns host -> peer id."""
    out: dict[str, str] = {}
    for m in machines:
        cmd = f"otela peer-id --config-dir {bench_dir(run_id, m.name)}"
        result = runner.run(m, cmd, timeout=30)
        if result.returncode != 0:
            raise RuntimeError(f"phase_discover failed on {m.name}: {result.stderr or result.stdout}")
        peer_id = result.stdout.strip()
        if not peer_id:
            raise RuntimeError(f"phase_discover got empty peer-id from {m.name}")
        out[m.name] = peer_id
    return out
