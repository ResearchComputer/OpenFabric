from __future__ import annotations

import re
from typing import Any

from .model import Machine

_IPV4_RE = re.compile(r"^\d+\.\d+\.\d+\.\d+$")


def build_multiaddr(machine: Machine, libp2p_port: int, peer_id: str) -> str:
    if _IPV4_RE.match(machine.address):
        return f"/ip4/{machine.address}/tcp/{libp2p_port}/p2p/{peer_id}"
    return f"/dns4/{machine.address}/tcp/{libp2p_port}/p2p/{peer_id}"


def build_host_config(
    self_machine: Machine,
    all_machines: list[Machine],
    peer_ids: dict[str, str],
    run_id: str,
    http_port: int,
    libp2p_port: int,
) -> dict[str, Any]:
    bootstrap = [
        build_multiaddr(m, libp2p_port, peer_ids[m.name])
        for m in all_machines
        if m.name != self_machine.name
    ]
    return {
        "port": str(http_port),
        "tcpport": str(libp2p_port),
        "udpport": str(libp2p_port + 1),
        "cleanslate": True,
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
