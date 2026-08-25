"""Unit tests for the SSH driver. Network and SSH I/O are not exercised; we
only verify the pure-Python helpers (config generation, nodes-file parsing)."""
from __future__ import annotations

import yaml

from benchmark_convergence.ssh_driver import _write_remote_config


def test_generated_remote_config_parses_back(monkeypatch, capsys) -> None:
    captured: dict[str, str] = {}

    def fake_run(cmd, input=None, **kw):  # type: ignore[no-redef]
        captured["cmd"] = " ".join(cmd)
        captured["input"] = input or ""

        class Result:
            returncode = 0

        return Result()

    import benchmark_convergence.ssh_driver as sd

    monkeypatch.setattr(sd.subprocess, "run", fake_run)

    _write_remote_config(
        "fake-host",
        http_port=8092, admin_port=8093,
        libp2p_port=43905, coord_port=47001,
        poll_ms=50, writes=20, stab_to=60, write_to=30, gap_ms=500,
    )

    cfg = yaml.safe_load(captured["input"])
    assert cfg["network"]["otela_http_port"] == 8092
    assert cfg["network"]["otela_admin_port"] == 8093
    assert cfg["network"]["otela_libp2p_port"] == 43905
    assert cfg["network"]["coordinator_tcp_port"] == 47001
    assert cfg["sweep"]["writes_per_rep"] == 20
    assert cfg["sweep"]["poll_interval_ms"] == 50
    # paths point at the remote install dir
    assert cfg["paths"]["otela_bin"].endswith("/otela")
