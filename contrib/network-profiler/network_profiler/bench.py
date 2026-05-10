from __future__ import annotations

import base64
import json
import signal
import sys
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path

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
    extra_bootstraps: list[str] | None = None,
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
            extra_bootstraps=extra_bootstraps,
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
            f"nohup otela start --config {cfg_path} --config-dir {d} "
            f">>{log_path} 2>&1 &"
        )
        result = runner.run(m, cmd, timeout=30)
        if result.returncode != 0:
            raise RuntimeError(f"phase_start could not launch on {m.name}: {result.stderr}")

    for m in machines:
        port = _http_port_for(m, http_port)
        deadline = time.monotonic() + max_wait_s
        while True:
            health = runner.run(
                m,
                f"curl -fsS http://127.0.0.1:{port}/v1/health",
                timeout=30,
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
                timeout=30,
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


def phase_punch(
    runner,
    machines: list[Machine],
    peer_ids: dict[str, str],
    http_port: int,
    attempts: int = 5,
    wait_ms: int = 2000,
    timeout_ms: int = 30000,
) -> dict[str, dict]:
    """For each (src, dst) pair, ask the source node to upgrade its connection
    to the target via DCUtR. Records final connectedness per pair so the run
    summary can show which pairs are actually direct vs circuit-only."""
    out: dict[str, dict] = {}
    for src in machines:
        port = _http_port_for(src, http_port)
        for dst in machines:
            if dst.name == src.name:
                continue
            target_pid = peer_ids[dst.name]
            body = json.dumps({
                "target": target_pid,
                "attempts": attempts,
                "wait_ms": wait_ms,
                "timeout_ms": timeout_ms,
            })
            cmd = (
                f"curl -fsS -m {timeout_ms // 1000 + 5} "
                f"-H 'Content-Type: application/json' "
                f"-X POST -d {json.dumps(body)} "
                f"http://127.0.0.1:{port}/v1/probe/holepunch"
            )
            result = runner.run(src, cmd, timeout=timeout_ms // 1000 + 10)
            entry = {"connectedness": "Unknown", "attempts": 0, "ok": False}
            if result.returncode == 0:
                try:
                    parsed = json.loads(result.stdout.strip())
                    entry["connectedness"] = parsed.get("connectedness", "Unknown")
                    entry["attempts"] = parsed.get("attempts", 0)
                    entry["ok"] = parsed.get("ok", False)
                    if parsed.get("last_error"):
                        entry["last_error"] = parsed["last_error"]
                except Exception as e:
                    entry["error"] = f"parse: {e}"
            else:
                entry["error"] = (result.stderr or result.stdout or "").strip()[:200]
            out[f"{src.name}->{dst.name}"] = entry
    return out


def phase_sweep(
    runner,
    machines: list[Machine],
    peer_ids: dict[str, str],
    run_id: str,
    output: Path,
    kinds: list[dict],
) -> dict[str, int]:
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    ok = 0
    failed = 0
    for spec in kinds:
        kind = spec["kind"]
        count = spec.get("count", 20)
        nbytes = spec.get("bytes", 0)
        for src in machines:
            for dst in machines:
                if src.name == dst.name:
                    continue
                cfg_path = f"{bench_dir(run_id, src.name)}/cfg.yaml"
                cmd = (
                    f"otela probe --target {peer_ids[dst.name]} "
                    f"--kind {kind} --count {count} --bytes {nbytes} "
                    f"--config {cfg_path}"
                )
                result = runner.run(src, cmd, timeout=count * 5 + nbytes // (1 << 20) + 60)
                record = _build_record(
                    src=src,
                    dst=dst,
                    src_peer_id=peer_ids[src.name],
                    dst_peer_id=peer_ids[dst.name],
                    kind=kind,
                    count=count,
                    nbytes=nbytes,
                    run_id=run_id,
                    result=result,
                )
                with output.open("a", encoding="utf-8") as f:
                    f.write(json.dumps(record, sort_keys=True) + "\n")
                if record["ok"]:
                    ok += 1
                else:
                    failed += 1
    return {"ok": ok, "failed": failed}


def _build_record(*, src, dst, src_peer_id, dst_peer_id, kind, count, nbytes, run_id, result) -> dict:
    parsed = None
    if result.returncode == 0 and result.stdout:
        try:
            parsed = json.loads(result.stdout.strip().splitlines()[-1])
        except json.JSONDecodeError:
            parsed = None
    ok = bool(parsed and parsed.get("ok"))
    return {
        "run_id": run_id,
        "timestamp": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "kind": kind,
        "source": src.name,
        "target": dst.name,
        "source_peer_id": src_peer_id,
        "target_peer_id": dst_peer_id,
        "ok": ok,
        "config": {"count": count, "bytes": nbytes if kind == "throughput" else None},
        "metrics": (parsed or {}).get("metrics", {}) if ok else {},
        "error": None if ok else (
            (parsed or {}).get("error") if parsed else (result.stderr or result.stdout or "no output")
        ),
        "command": result.command,
    }


def phase_teardown(runner, machines: list[Machine], run_id: str) -> None:
    for m in machines:
        d = bench_dir(run_id, m.name)
        cfg_path = f"{d}/cfg.yaml"
        runner.run(m, f"pkill -f 'otela start --config {cfg_path}' || true", timeout=30)
        runner.run(m, f"rm -rf {d} || true", timeout=30)


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


def new_run_id() -> str:
    return time.strftime("%Y-%m-%dT%H-%M-%S") + "-" + uuid.uuid4().hex[:6]


def run_bench(
    *,
    runner,
    machines: list[Machine],
    output_dir: Path,
    run_id: str,
    http_port: int = 19090,
    libp2p_port: int = 19091,
    latency_count: int = 20,
    throughput_count: int = 3,
    throughput_bytes: int = 10 * 1024 * 1024,
    keep: bool = False,
    extra_bootstraps: list[str] | None = None,
) -> int:
    output_dir = Path(output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    measurements_path = output_dir / "measurements.jsonl"
    run_json_path = output_dir / "run.json"

    started_at = datetime.now(timezone.utc)
    phase_timings: dict[str, float] = {}
    convergence: dict[str, dict] = {}
    punch_results: dict[str, dict] = {}
    peer_ids: dict[str, str] = {}
    totals: dict[str, int] = {}
    teardown_done = False

    def teardown():
        nonlocal teardown_done
        if teardown_done or keep:
            return
        teardown_done = True
        t0 = time.monotonic()
        try:
            phase_teardown(runner, machines, run_id)
        except Exception as e:
            print(f"warning: teardown error: {e}")
        phase_timings["teardown_s"] = round(time.monotonic() - t0, 2)

    def on_sigint(signum, frame):
        print("\ninterrupted, running teardown")
        teardown()
        sys.exit(130)

    signal.signal(signal.SIGINT, on_sigint)
    try:
        t0 = time.monotonic()
        phase_init(runner, machines, run_id)
        phase_timings["init_s"] = round(time.monotonic() - t0, 2)

        t0 = time.monotonic()
        peer_ids = phase_discover(runner, machines, run_id)
        phase_timings["discover_s"] = round(time.monotonic() - t0, 2)

        t0 = time.monotonic()
        phase_configure_and_push(
            runner, machines, peer_ids, run_id,
            http_port=http_port, libp2p_port=libp2p_port,
            extra_bootstraps=extra_bootstraps,
        )
        phase_timings["configure_s"] = round(time.monotonic() - t0, 2)

        t0 = time.monotonic()
        phase_start(runner, machines, run_id, http_port=http_port)
        phase_timings["start_s"] = round(time.monotonic() - t0, 2)

        t0 = time.monotonic()
        convergence = phase_converge(runner, machines, peer_ids, http_port=http_port)
        phase_timings["converge_s"] = round(time.monotonic() - t0, 2)

        t0 = time.monotonic()
        punch_results = phase_punch(runner, machines, peer_ids, http_port=http_port)
        phase_timings["punch_s"] = round(time.monotonic() - t0, 2)

        t0 = time.monotonic()
        totals = phase_sweep(
            runner, machines, peer_ids, run_id,
            output=measurements_path,
            kinds=[
                {"kind": "libp2p_ping", "count": latency_count, "bytes": 0},
                {"kind": "latency", "count": latency_count, "bytes": 0},
                {"kind": "throughput", "count": throughput_count, "bytes": throughput_bytes},
            ],
        )
        phase_timings["sweep_s"] = round(time.monotonic() - t0, 2)
    finally:
        teardown()

    ended_at = datetime.now(timezone.utc)
    summary = {
        "run_id": run_id,
        "started_at": started_at.isoformat(timespec="seconds"),
        "ended_at": ended_at.isoformat(timespec="seconds"),
        "machines": [
            {
                "name": m.name,
                "address": m.address,
                "peer_id": peer_ids.get(m.name),
                "http_port": http_port,
                "libp2p_port": libp2p_port,
            }
            for m in machines
        ],
        "convergence": convergence,
        "punch": punch_results,
        "phases": phase_timings,
        "totals": totals,
    }
    run_json_path.write_text(json.dumps(summary, indent=2, sort_keys=True))
    return 0
