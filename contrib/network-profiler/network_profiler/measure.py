from __future__ import annotations

from datetime import datetime, timezone
from pathlib import Path
from typing import Any
import json
import re
import time

from .model import Machine, ProfilerConfig
from .remote import RemoteRunner

PING_RE = re.compile(
    r"rtt min/avg/max/(?:mdev|stddev) = "
    r"(?P<min>[0-9.]+)/(?P<avg>[0-9.]+)/(?P<max>[0-9.]+)/(?P<jitter>[0-9.]+) ms"
)


def timestamp() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def parse_ping(output: str) -> dict[str, float] | None:
    match = PING_RE.search(output)
    if not match:
        return None
    return {key: float(value) for key, value in match.groupdict().items()}


def parse_iperf(output: str) -> dict[str, float] | None:
    try:
        data = json.loads(output)
    except json.JSONDecodeError:
        return None

    end = data.get("end", {})
    summary = end.get("sum_received") or end.get("sum") or end.get("sum_sent")
    if not summary:
        return None
    bits_per_second = float(summary.get("bits_per_second", 0.0))
    return {
        "mbps": bits_per_second / 1_000_000,
        "seconds": float(summary.get("seconds", 0.0)),
        "bytes": float(summary.get("bytes", 0.0)),
    }


def measurement_record(
    kind: str,
    source: Machine,
    target: Machine,
    ok: bool,
    command: str,
    metrics: dict[str, Any] | None = None,
    error: str | None = None,
) -> dict[str, Any]:
    return {
        "timestamp": timestamp(),
        "kind": kind,
        "source": source.name,
        "target": target.name,
        "target_address": target.address,
        "ok": ok,
        "metrics": metrics or {},
        "error": error,
        "command": command,
    }


def append_jsonl(path: Path, record: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as f:
        f.write(json.dumps(record, sort_keys=True) + "\n")


def iter_pairs(machines: list[Machine]) -> list[tuple[Machine, Machine]]:
    return [(source, target) for source in machines for target in machines if source != target]


def collect(
    config: ProfilerConfig,
    output: Path,
    include_iperf: bool,
    dry_run: bool = False,
) -> None:
    runner = RemoteRunner(config, dry_run=dry_run)
    for source, target in iter_pairs(config.machines):
        ping_cmd = f"ping -c {config.ping_count} -W {config.connect_timeout_seconds} {shlex_quote(target.address)}"
        ping = runner.run(source, ping_cmd, timeout=config.connect_timeout_seconds * config.ping_count + 10)
        ping_metrics = parse_ping(ping.stdout)
        append_jsonl(
            output,
            measurement_record(
                "ping",
                source,
                target,
                ping.returncode == 0 and ping_metrics is not None,
                ping.command,
                ping_metrics,
                None if ping.returncode == 0 else ping.stderr.strip() or ping.stdout.strip(),
            ),
        )

        if not include_iperf:
            continue

        server_cmd = (
            f"nohup iperf3 -s -1 -p {config.iperf_port} >/tmp/network-profiler-iperf3.log 2>&1 &"
        )
        server = runner.run(target, server_cmd, timeout=config.connect_timeout_seconds)
        if server.returncode != 0:
            append_jsonl(
                output,
                measurement_record(
                    "iperf3",
                    source,
                    target,
                    False,
                    server.command,
                    error=server.stderr.strip() or server.stdout.strip(),
                ),
            )
            continue

        time.sleep(1)
        iperf_cmd = (
            f"iperf3 -J -c {shlex_quote(target.address)} "
            f"-p {config.iperf_port} -t {config.iperf_seconds}"
        )
        iperf = runner.run(source, iperf_cmd, timeout=config.iperf_seconds + config.connect_timeout_seconds + 10)
        iperf_metrics = parse_iperf(iperf.stdout)
        append_jsonl(
            output,
            measurement_record(
                "iperf3",
                source,
                target,
                iperf.returncode == 0 and iperf_metrics is not None,
                iperf.command,
                iperf_metrics,
                None if iperf.returncode == 0 else iperf.stderr.strip() or iperf.stdout.strip(),
            ),
        )


def shlex_quote(value: str) -> str:
    import shlex

    return shlex.quote(value)
