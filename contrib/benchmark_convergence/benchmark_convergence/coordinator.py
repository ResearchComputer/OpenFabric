"""Coordinator. Runs on the task with SLURM_PROCID == 0."""
from __future__ import annotations

import json
import os
import socket
import threading
import time
import uuid
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import httpx

from benchmark_convergence.protocol import read_messages, write_message


@dataclass
class CoordinatorContext:
    task_id: int
    host: str
    n: int                   # total tasks (including coordinator)
    listen_port: int
    local_otela_url: str
    local_admin_url: str
    writes_per_rep: int
    stabilization_timeout_s: int
    per_write_timeout_s: int
    inter_write_gap_ms: int
    poll_interval_ms: int
    run_id: str
    cell_dir: Path
    rep: int


@dataclass
class _ObserverConn:
    task_id: int
    host: str
    conn: socket.socket
    reader: Any
    writer: Any
    lock: threading.Lock = field(default_factory=threading.Lock)


@dataclass
class _State:
    observers: dict[int, _ObserverConn] = field(default_factory=dict)
    chrony_start: dict[str, int] = field(default_factory=dict)
    chrony_end: dict[str, int] = field(default_factory=dict)
    seen: dict[str, dict[int, int]] = field(default_factory=dict)
    writer_done: dict[tuple[int, int], int] = field(default_factory=dict)
    writer_error: dict[tuple[int, int], str] = field(default_factory=dict)
    lock: threading.Lock = field(default_factory=threading.Lock)


def _accept_loop(server: socket.socket, state: _State,
                 expected: int, ready: threading.Event,
                 ctx: CoordinatorContext) -> None:
    """Accept connections until we have `expected` observers (N - 1)."""
    while len(state.observers) < expected:
        client, _ = server.accept()
        reader = client.makefile("rb")
        writer = client.makefile("wb")
        first = next(read_messages(reader), None)
        if not first or first.get("type") != "hello":
            client.close()
            continue
        oc = _ObserverConn(
            task_id=int(first["task_id"]),
            host=first.get("host", ""),
            conn=client, reader=reader, writer=writer,
        )
        with state.lock:
            state.observers[oc.task_id] = oc
        threading.Thread(
            target=_drain_observer, args=(oc, state),
            daemon=True,
        ).start()
    ready.set()


def _drain_observer(oc: _ObserverConn, state: _State) -> None:
    for msg in read_messages(oc.reader):
        t = msg.get("type")
        with state.lock:
            if t == "chrony_start":
                state.chrony_start[oc.host] = int(msg["offset_ns"])
            elif t == "chrony_end":
                state.chrony_end[oc.host] = int(msg["offset_ns"])
            elif t == "seen":
                for ev in msg.get("events", []):
                    state.seen.setdefault(ev["name"], {})[oc.task_id] = int(ev["seen_ns"])
            elif t == "writer_done":
                state.writer_done[(oc.task_id, int(msg["seq"]))] = int(msg["t_write_ns"])
            elif t == "writer_error":
                state.writer_error[(oc.task_id, int(msg["seq"]))] = msg.get("error", "")


def _self_poll_loop(ctx: CoordinatorContext, state: _State,
                    stop: threading.Event) -> None:
    """Coordinator's own DNT poll. Populates state.seen[name][task_id=0] so
    task 0 participates in measurement when not the writer."""
    seen_local: set[str] = set()
    interval = ctx.poll_interval_ms / 1000.0
    client = httpx.Client(timeout=1.0)
    while not stop.is_set():
        t0 = time.time_ns()
        try:
            r = client.get(f"{ctx.local_otela_url}/v1/dnt/table")
            r.raise_for_status()
            data = r.json()
        except Exception:
            time.sleep(interval)
            continue
        for _peer_id, peer in data.items():
            for svc in (peer.get("service") or []):
                name = svc.get("name", "")
                if name.startswith("convbench-w") and name not in seen_local:
                    seen_local.add(name)
                    with state.lock:
                        state.seen.setdefault(name, {})[ctx.task_id] = t0
        time.sleep(interval)


def _self_write(ctx: CoordinatorContext, service_name: str) -> int:
    r = httpx.post(
        f"{ctx.local_admin_url}/v1/_admin/register",
        json={"name": service_name, "port": 65000},
        timeout=2.0,
    )
    r.raise_for_status()
    return int(r.json()["registered_at_ns"])


def _wait_seen_by_all(state: _State, service_name: str,
                      expected_task_ids: set[int],
                      deadline: float) -> tuple[dict[int, int], set[int]]:
    while time.time() < deadline:
        with state.lock:
            seen = dict(state.seen.get(service_name, {}))
        if all(tid in seen for tid in expected_task_ids):
            return seen, set()
        time.sleep(0.05)
    with state.lock:
        seen = dict(state.seen.get(service_name, {}))
    missing = expected_task_ids - set(seen.keys())
    return seen, missing


def _stabilize(ctx: CoordinatorContext, state: _State) -> dict[str, Any]:
    name = f"convbench-warmup-{ctx.run_id}-{uuid.uuid4().hex[:6]}"
    t_warmup = _self_write(ctx, name)
    deadline = time.time() + ctx.stabilization_timeout_s
    _seen, missing = _wait_seen_by_all(
        state, name,
        expected_task_ids=set(state.observers.keys()),
        deadline=deadline,
    )
    return {
        "warmup_at_ns": t_warmup,
        "stable_at_ns": time.time_ns(),
        "missing": sorted(missing),
    }


def _send_write_cmd(oc: _ObserverConn, seq: int, name: str) -> None:
    with oc.lock:
        write_message(oc.writer, {
            "type": "write", "seq": seq, "service_name": name,
        })


def _stop_all(state: _State) -> None:
    for oc in state.observers.values():
        try:
            with oc.lock:
                write_message(oc.writer, {"type": "stop"})
        except Exception:
            pass


def main(ctx: CoordinatorContext) -> None:
    started_at = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind(("0.0.0.0", ctx.listen_port))
    server.listen(ctx.n * 2)

    state = _State()
    ready = threading.Event()
    threading.Thread(
        target=_accept_loop,
        args=(server, state, ctx.n - 1, ready, ctx),
        daemon=True,
    ).start()

    if not ready.wait(timeout=ctx.stabilization_timeout_s + 60):
        raise RuntimeError(
            f"only {len(state.observers)} observers connected of {ctx.n - 1}"
        )

    from benchmark_convergence.observer import _chrony_offset_ns
    state.chrony_start[ctx.host] = _chrony_offset_ns()

    self_stop = threading.Event()
    threading.Thread(
        target=_self_poll_loop, args=(ctx, state, self_stop),
        daemon=True,
    ).start()

    stab = _stabilize(ctx, state)
    if stab["missing"]:
        raise RuntimeError(
            f"failed_stabilization: tasks {stab['missing']} did not see warmup"
        )

    flags: list[str] = []
    writes_record: list[dict[str, Any]] = []

    for seq in range(ctx.writes_per_rep):
        writer_id = seq % ctx.n
        name = f"convbench-w-{ctx.run_id}-s{seq}-{writer_id}"

        if writer_id == 0:
            try:
                t_write = _self_write(ctx, name)
            except Exception as e:
                writes_record.append({
                    "seq": seq, "writer_task_id": writer_id,
                    "service_name": name, "t_write_ns": None,
                    "seen": {}, "error": repr(e),
                })
                time.sleep(ctx.inter_write_gap_ms / 1000.0)
                continue
        else:
            oc = state.observers[writer_id]
            _send_write_cmd(oc, seq, name)
            deadline = time.time() + 5.0
            t_write = None
            while time.time() < deadline:
                with state.lock:
                    t = state.writer_done.get((writer_id, seq))
                if t is not None:
                    t_write = t
                    break
                time.sleep(0.05)
            if t_write is None:
                writes_record.append({
                    "seq": seq, "writer_task_id": writer_id,
                    "service_name": name, "t_write_ns": None,
                    "seen": {}, "error": "writer_no_ack",
                })
                time.sleep(ctx.inter_write_gap_ms / 1000.0)
                continue

        expected = set(range(ctx.n)) - {writer_id}
        deadline = time.time() + ctx.per_write_timeout_s
        seen_map, missing = _wait_seen_by_all(state, name, expected, deadline)
        seen_out: dict[str, Any] = {str(k): v for k, v in seen_map.items()}
        for m in missing:
            seen_out[str(m)] = "TIMEOUT"

        writes_record.append({
            "seq": seq, "writer_task_id": writer_id,
            "service_name": name, "t_write_ns": t_write,
            "seen": seen_out,
        })
        time.sleep(ctx.inter_write_gap_ms / 1000.0)

    state.chrony_end[ctx.host] = _chrony_offset_ns()
    self_stop.set()
    _stop_all(state)
    # Allow observers time to call chronyc tracking (subprocess; can be
    # slow on loaded compute nodes) and send chrony_end + bye before we
    # snapshot state for the results.json.
    time.sleep(3.0)
    finished_at = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")

    nodes = [{"task_id": 0, "host": ctx.host, "peer_id": "",
              "role": "coordinator"}]
    for tid, oc in sorted(state.observers.items()):
        nodes.append({"task_id": tid, "host": oc.host, "peer_id": "",
                      "role": "observer"})

    for host, o_start in state.chrony_start.items():
        o_end = state.chrony_end.get(host)
        if o_end is not None and abs(o_end - o_start) > 5_000_000:
            flags.append("clock_drift")
            break

    out = {
        "run_id": ctx.run_id,
        "N": ctx.n,
        "rep": ctx.rep,
        "job_id": os.environ.get("SLURM_JOB_ID", ""),
        "started_at": started_at,
        "finished_at": finished_at,
        "nodes": nodes,
        "clock_offsets_ns": {
            "start": dict(state.chrony_start),
            "end": dict(state.chrony_end),
        },
        "stabilization": stab,
        "writes": writes_record,
        "flags": flags,
    }

    ctx.cell_dir.mkdir(parents=True, exist_ok=True)
    (ctx.cell_dir / "results.json").write_text(json.dumps(out, indent=2))
    (ctx.cell_dir / "status").write_text("complete")
