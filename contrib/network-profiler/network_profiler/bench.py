from __future__ import annotations

import base64
import json
import time

import yaml

from .bench_config import build_host_config
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


def phase_configure_and_push(
    runner,
    machines: list[Machine],
    peer_ids: dict[str, str],
    run_id: str,
    http_port: int,
    libp2p_port: int,
) -> dict[str, str]:
    """Render per-host cfg.yaml and push via stdin-piped base64."""
    out: dict[str, str] = {}
    for m in machines:
        d = bench_dir(run_id, m.name)
        cfg_path = f"{d}/cfg.yaml"
        cfg = build_host_config(
            self_machine=m,
            all_machines=machines,
            peer_ids=peer_ids,
            run_id=run_id,
            http_port=http_port,
            libp2p_port=libp2p_port,
        )
        yaml_bytes = yaml.safe_dump(cfg, sort_keys=True).encode("utf-8")
        b64 = base64.b64encode(yaml_bytes)
        cmd = f"mkdir -p {d} && base64 -d > {cfg_path}"
        result = runner.run(m, cmd, timeout=30, stdin=b64)
        if result.returncode != 0:
            raise RuntimeError(f"phase_configure_and_push failed on {m.name}: {result.stderr or result.stdout}")
        out[m.name] = cfg_path
    return out


def _http_port_for(m: Machine, default: int) -> int:
    return getattr(m, "http_port", None) or default


def phase_start(
    runner,
    machines: list[Machine],
    run_id: str,
    http_port: int,
    max_wait_s: int = 30,
) -> None:
    for m in machines:
        d = bench_dir(run_id, m.name)
        cfg_path = f"{d}/cfg.yaml"
        log_path = f"{d}/log"
        cmd = (
            f"nohup otela start --config {cfg_path} "
            f">>{log_path} 2>&1 &"
        )
        result = runner.run(m, cmd, timeout=10)
        if result.returncode != 0:
            raise RuntimeError(f"phase_start could not launch on {m.name}: {result.stderr}")

    for m in machines:
        port = _http_port_for(m, http_port)
        deadline = time.monotonic() + max_wait_s
        while True:
            health = runner.run(
                m,
                f"curl -fsS http://127.0.0.1:{port}/v1/health",
                timeout=10,
            )
            if health.returncode == 0:
                break
            if time.monotonic() > deadline:
                raise RuntimeError(
                    f"phase_start: {m.name} did not become healthy within {max_wait_s}s"
                )
            time.sleep(1)


def phase_converge(
    runner,
    machines: list[Machine],
    peer_ids: dict[str, str],
    http_port: int,
    max_wait_s: int = 60,
) -> dict[str, dict]:
    out: dict[str, dict] = {}
    expected = {p for p in peer_ids.values()}
    for m in machines:
        port = _http_port_for(m, http_port)
        deadline = time.monotonic() + max_wait_s
        complete = False
        elapsed_s = 0.0
        start_t = time.monotonic()
        while time.monotonic() <= deadline:
            tbl = runner.run(
                m,
                f"curl -s http://127.0.0.1:{port}/v1/dnt/table",
                timeout=10,
            )
            if tbl.returncode == 0:
                seen = _extract_peer_ids(tbl.stdout)
                if expected - {peer_ids[m.name]} <= seen:
                    complete = True
                    break
            time.sleep(1)
        elapsed_s = time.monotonic() - start_t
        out[m.name] = {"complete": complete, "elapsed_s": round(elapsed_s, 2)}
    return out


def _extract_peer_ids(table_json: str) -> set[str]:
    """Tolerant extraction: walk any list/dict and collect string values that
    look like libp2p PeerIDs (start with 12D3 or Qm)."""
    try:
        data = json.loads(table_json)
    except json.JSONDecodeError:
        return set()
    found: set[str] = set()

    def walk(obj):
        if isinstance(obj, dict):
            for v in obj.values():
                walk(v)
        elif isinstance(obj, list):
            for item in obj:
                walk(item)
        elif isinstance(obj, str):
            if obj.startswith("12D3") or obj.startswith("Qm"):
                found.add(obj)

    walk(data)
    return found
