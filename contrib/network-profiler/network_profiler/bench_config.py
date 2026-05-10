from __future__ import annotations

import re
from typing import Any

from .model import Machine

_IPV4_RE = re.compile(r"^\d+\.\d+\.\d+\.\d+$")


def build_multiaddr(machine: Machine, libp2p_port: int, peer_id: str) -> str:
    if _IPV4_RE.match(machine.address):
        return f"/ip4/{machine.address}/tcp/{libp2p_port}/p2p/{peer_id}"
    return f"/dns4/{machine.address}/tcp/{libp2p_port}/p2p/{peer_id}"


def _peer_libp2p_port(m: Machine, default: int) -> int:
    return m.libp2p_port or default


def build_host_config(
    self_machine: Machine,
    all_machines: list[Machine],
    peer_ids: dict[str, str],
    run_id: str,
    http_port: int,
    libp2p_port: int,
    extra_bootstraps: list[str] | None = None,
) -> dict[str, Any]:
    http = self_machine.http_port or http_port
    tcp = self_machine.libp2p_port or libp2p_port
    bootstrap = [
        build_multiaddr(m, _peer_libp2p_port(m, libp2p_port), peer_ids[m.name])
        for m in all_machines
        if m.name != self_machine.name
    ]
    if extra_bootstraps:
        bootstrap.extend(extra_bootstraps)
    if self_machine.bootstrap_extra:
        bootstrap.extend(self_machine.bootstrap_extra)
    return {
        "port": str(http),
        "tcpport": str(tcp),
        "udpport": str(tcp + 1),
        "cleanslate": True,
        "reachability": "auto",
        "bootstrap": {"static": bootstrap},
        "security": {"require_signed_binary": False},
        "scalability": {
            "swim_enabled": False,
            "crdt_tuned": False,
            "weighted_routing": False,
        },
        "billing": {"enabled": False},
        "bench": {"run_id": run_id},
    }
