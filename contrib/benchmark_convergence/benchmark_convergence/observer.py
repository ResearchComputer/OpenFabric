"""Observer agent. Runs on tasks with SLURM_PROCID > 0."""
from __future__ import annotations

import socket
import subprocess
import threading
import time
from dataclasses import dataclass

import httpx

from benchmark_convergence.protocol import read_messages, write_message


@dataclass
class ObserverContext:
    task_id: int
    host: str
    coord_addr: tuple[str, int]
    local_otela_url: str           # http://127.0.0.1:8092
    local_admin_url: str           # http://127.0.0.1:8093
    poll_interval_ms: int
    run_id: str


def _chrony_offset_ns() -> int:
    """Parse `chronyc tracking` 'System time' offset; return signed ns.

    Example chrony output:
        System time     :  0.000123456 seconds fast of NTP time
    Sign: positive when local clock is "fast" (ahead of) reference.
    """
    try:
        out = subprocess.run(
            ["chronyc", "tracking"], capture_output=True, text=True, check=False
        ).stdout
    except (FileNotFoundError, PermissionError, OSError):
        # chronyc unusable on this node (missing, or present but not
        # executable by the user — Euler compute nodes are the latter).
        # The kernel is still NTP-synced; treat offset as 0 and accept
        # the residual NTP-level skew (~<1 ms on Euler) as noise.
        return 0
    for line in out.splitlines():
        if line.lower().startswith("system time"):
            parts = line.split()
            try:
                secs = float(parts[3])
            except (IndexError, ValueError):
                return 0
            sign = 1 if "fast" in line.lower() else -1
            return sign * int(secs * 1e9)
    return 0


def _poll_dnt_loop(ctx: ObserverContext, sock_lock: threading.Lock,
                   sock_w, stop: threading.Event) -> None:
    seen: set[str] = set()
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
        new_events = []
        # /v1/dnt/table returns dict: peer_id -> Peer object
        for _peer_id, peer in data.items():
            for svc in (peer.get("service") or []):
                name = svc.get("name", "")
                if name.startswith("convbench-w") and name not in seen:
                    seen.add(name)
                    new_events.append({"name": name, "seen_ns": t0})
        if new_events:
            with sock_lock:
                write_message(sock_w, {
                    "type": "seen",
                    "task_id": ctx.task_id,
                    "events": new_events,
                })
        time.sleep(interval)


def _handle_writes(ctx: ObserverContext, sock_lock: threading.Lock,
                   sock_w, sock_r, stop: threading.Event) -> None:
    """Receive write commands from the coordinator and POST locally.

    Returns when the coordinator sends {"type": "stop"} or the stream closes.
    """
    client = httpx.Client(timeout=2.0)
    for msg in read_messages(sock_r):
        if msg.get("type") == "stop":
            stop.set()
            return
        if msg.get("type") == "write":
            name = msg["service_name"]
            seq = int(msg["seq"])
            try:
                r = client.post(
                    f"{ctx.local_admin_url}/v1/_admin/register",
                    json={"name": name, "port": 65000},
                )
                r.raise_for_status()
                t_write = int(r.json()["registered_at_ns"])
            except Exception as e:
                with sock_lock:
                    write_message(sock_w, {
                        "type": "writer_error",
                        "task_id": ctx.task_id,
                        "seq": seq,
                        "error": repr(e),
                    })
                continue
            with sock_lock:
                write_message(sock_w, {
                    "type": "writer_done",
                    "task_id": ctx.task_id,
                    "seq": seq,
                    "service_name": name,
                    "t_write_ns": t_write,
                })


def main(ctx: ObserverContext) -> None:
    s = socket.create_connection(ctx.coord_addr, timeout=30.0)
    # The connect timeout above also becomes the socket's read/write
    # timeout. Clear it so the control-channel reader can block
    # indefinitely waiting for the next command from the coordinator.
    s.settimeout(None)
    sock_r = s.makefile("rb")
    sock_w = s.makefile("wb")
    sock_lock = threading.Lock()
    stop = threading.Event()

    write_message(sock_w, {
        "type": "hello",
        "task_id": ctx.task_id,
        "host": ctx.host,
    })
    write_message(sock_w, {
        "type": "chrony_start",
        "task_id": ctx.task_id,
        "offset_ns": _chrony_offset_ns(),
    })

    poll_t = threading.Thread(
        target=_poll_dnt_loop,
        args=(ctx, sock_lock, sock_w, stop),
        daemon=True,
    )
    poll_t.start()

    _handle_writes(ctx, sock_lock, sock_w, sock_r, stop)
    stop.set()
    poll_t.join(timeout=2.0)

    with sock_lock:
        write_message(sock_w, {
            "type": "chrony_end",
            "task_id": ctx.task_id,
            "offset_ns": _chrony_offset_ns(),
        })
        write_message(sock_w, {"type": "bye", "task_id": ctx.task_id})
    s.close()
