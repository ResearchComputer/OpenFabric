from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any
import json


@dataclass(frozen=True)
class Machine:
    name: str
    address: str
    rcc_host: str
    http_port: int | None = None
    libp2p_port: int | None = None
    bootstrap_extra: tuple[str, ...] = ()


@dataclass(frozen=True)
class ProfilerConfig:
    machines: list[Machine]
    remote_command: list[str] | None
    ping_count: int
    iperf_seconds: int
    iperf_port: int
    connect_timeout_seconds: int


def load_config(path: Path) -> ProfilerConfig:
    with path.open("r", encoding="utf-8") as f:
        raw = json.load(f)

    machines = [
        Machine(
            name=str(item["name"]),
            address=str(item.get("address", item["name"])),
            rcc_host=str(item.get("rcc_host", item["name"])),
            http_port=item.get("http_port"),
            libp2p_port=item.get("libp2p_port"),
            bootstrap_extra=tuple(item.get("bootstrap_extra", [])),
        )
        for item in raw["machines"]
    ]
    if len({machine.name for machine in machines}) != len(machines):
        raise ValueError("machine names must be unique")

    defaults: dict[str, Any] = {
        "ping_count": 5,
        "iperf_seconds": 5,
        "iperf_port": 5201,
        "connect_timeout_seconds": 10,
    }
    defaults.update(raw)

    remote_command = raw.get("remote_command")
    if remote_command is not None and not isinstance(remote_command, list):
        raise ValueError("remote_command must be a JSON array of command arguments")

    return ProfilerConfig(
        machines=machines,
        remote_command=remote_command,
        ping_count=int(defaults["ping_count"]),
        iperf_seconds=int(defaults["iperf_seconds"]),
        iperf_port=int(defaults["iperf_port"]),
        connect_timeout_seconds=int(defaults["connect_timeout_seconds"]),
    )
