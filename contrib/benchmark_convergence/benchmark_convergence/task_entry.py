"""Single entrypoint invoked by srun on every task in a cell allocation.

Reads $SLURM_PROCID, starts a local otela process, then dispatches to
either coordinator.main (procid 0) or observer.main (procid > 0).
"""
from __future__ import annotations

import os
import socket
import subprocess
import sys
import time
from pathlib import Path

import httpx
import yaml


def _hostname() -> str:
    return socket.getfqdn()


def _wait_for(url: str, timeout_s: float = 60.0) -> None:
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        try:
            r = httpx.get(url, timeout=1.0)
            if r.status_code < 500:
                return
        except Exception:
            pass
        time.sleep(0.5)
    raise RuntimeError(f"timeout waiting for {url}")


def _start_otela(*, bin_path: Path, config_dir: Path, http_port: int,
                 admin_port: int, bootstrap_addr: str | None,
                 service_name: str) -> subprocess.Popen:
    """Launch a local otela process.

    Note: there is no `--port` CLI flag for the HTTP port; it's set via
    config or the OF_PORT env var. We pass it via OF_PORT to make the
    intent explicit at this layer. The libp2p TCP port (--tcpport) and
    admin port (--admin.port) DO have flags.
    """
    config_dir.mkdir(parents=True, exist_ok=True)
    # cfg.yaml overrides relevant flag defaults:
    #   bootstrap.static = []  -> isolates test mesh from production
    #   security.require_signed_binary = false  -> allow unsigned dev
    #     binaries to accept each other's CRDT entries (default true
    #     would silently drop peer entries from unsigned builds).
    (config_dir / "cfg.yaml").write_text(
        "bootstrap:\n  static: []\n"
        "security:\n  require_signed_binary: false\n"
    )
    cmd = [
        str(bin_path), "start",
        "--config-dir", str(config_dir),
        "--admin.enabled",
        "--admin.port", str(admin_port),
        "--service.name", service_name,
        "--service.port", "65000",
    ]
    if bootstrap_addr:
        cmd += ["--bootstrap.addr", bootstrap_addr]
    else:
        # Coordinator: standalone (no upstream — this is THE bootstrap).
        cmd += ["--mode", "standalone"]
    env = os.environ.copy()
    env["OF_PORT"] = str(http_port)
    log = (config_dir / "otela.log").open("w")
    return subprocess.Popen(cmd, stdout=log, stderr=subprocess.STDOUT, env=env)


def _primary_ipv4() -> str:
    """Return the node's primary outbound IPv4 (UDP-connect trick; no packet sent)."""
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        s.connect(("10.255.255.255", 1))
        return s.getsockname()[0]
    finally:
        s.close()


def _read_my_multiaddr(http_port: int, libp2p_port: int) -> str:
    """Construct /ip4/<our_ip>/tcp/<libp2p_port>/p2p/<our_peer_id>.

    /v1/self is the localhost-only endpoint returning this node's full
    Peer struct (id field is the libp2p peer ID).
    """
    r = httpx.get(f"http://127.0.0.1:{http_port}/v1/self", timeout=2.0)
    r.raise_for_status()
    peer_id = r.json()["id"]
    ip = _primary_ipv4()
    return f"/ip4/{ip}/tcp/{libp2p_port}/p2p/{peer_id}"


def _coord_ip_from_multiaddr(addr: str) -> str:
    # /ip4/10.205.0.123/tcp/43905/p2p/12D3Koo...
    parts = addr.split("/")
    if len(parts) > 2 and parts[1] in ("ip4", "ip6"):
        return parts[2]
    raise ValueError(f"cannot parse coord IP from {addr!r}")


def main() -> None:
    procid = int(os.environ["SLURM_PROCID"])
    run_id = os.environ["RUN_ID"]
    n = int(os.environ["N"])
    rep = int(os.environ["REP"])
    run_dir = Path(os.environ["RUN_DIR"])
    config_path = Path(os.environ["CONFIG_PATH"])
    cfg = yaml.safe_load(config_path.read_text())
    net = cfg["network"]
    sweep = cfg["sweep"]

    http_port = int(net["otela_http_port"])
    admin_port = int(net["otela_admin_port"])
    libp2p_port = int(net["otela_libp2p_port"])
    coord_port = int(net["coordinator_tcp_port"])
    poll_ms = int(sweep["poll_interval_ms"])

    otela_bin = Path(os.path.expandvars(cfg["paths"]["otela_bin"]))
    tmp_root = Path(os.path.expandvars(cfg["paths"]["config_root_in_task"]))
    config_dir = tmp_root / run_id / f"task-{procid}"
    cell_dir = run_dir / f"N={n}" / f"rep={rep}"
    cell_dir.mkdir(parents=True, exist_ok=True)

    bootstrap_file = cell_dir / "bootstrap.txt"

    if procid == 0:
        proc = _start_otela(
            bin_path=otela_bin, config_dir=config_dir,
            http_port=http_port, admin_port=admin_port,
            bootstrap_addr=None,
            service_name=f"convbench-node-{procid}",
        )
        _wait_for(f"http://127.0.0.1:{http_port}/v1/health")
        my_addr = _read_my_multiaddr(http_port, libp2p_port)
        # Two distribution channels for the multiaddr:
        # 1. bootstrap.txt: used by the SLURM path (observers poll the
        #    shared $RUN_DIR for this file).
        # 2. stdout: used by the SSH driver (no shared filesystem; the
        #    driver captures this line and passes it to observers via
        #    BOOTSTRAP_ADDR env var).
        bootstrap_file.parent.mkdir(parents=True, exist_ok=True)
        bootstrap_file.write_text(my_addr)
        print(f"BOOTSTRAP_ADDR={my_addr}", flush=True)

        from benchmark_convergence.coordinator import (
            CoordinatorContext, main as coord_main,
        )
        coord_main(CoordinatorContext(
            task_id=0,
            host=_hostname(),
            n=n,
            listen_port=coord_port,
            local_otela_url=f"http://127.0.0.1:{http_port}",
            local_admin_url=f"http://127.0.0.1:{admin_port}",
            writes_per_rep=int(sweep["writes_per_rep"]),
            stabilization_timeout_s=int(sweep["stabilization_timeout_s"]),
            per_write_timeout_s=int(sweep["per_write_timeout_s"]),
            inter_write_gap_ms=int(sweep["inter_write_gap_ms"]),
            poll_interval_ms=poll_ms,
            run_id=run_id,
            cell_dir=cell_dir,
            rep=rep,
        ))
        _stop_otela(proc)
        return

    # Observers: prefer BOOTSTRAP_ADDR env var (SSH driver passes it
    # directly). Fall back to bootstrap.txt for the SLURM path.
    bootstrap_addr = os.environ.get("BOOTSTRAP_ADDR", "").strip()
    if not bootstrap_addr:
        deadline = time.time() + 300
        while not bootstrap_file.exists() and time.time() < deadline:
            time.sleep(0.5)
        if not bootstrap_file.exists():
            print("bootstrap.txt never appeared", file=sys.stderr)
            sys.exit(1)
        bootstrap_addr = bootstrap_file.read_text().strip()

    proc = _start_otela(
        bin_path=otela_bin, config_dir=config_dir,
        http_port=http_port, admin_port=admin_port,
        bootstrap_addr=bootstrap_addr,
        service_name=f"convbench-node-{procid}",
    )
    _wait_for(f"http://127.0.0.1:{http_port}/v1/health")

    coord_ip = _coord_ip_from_multiaddr(bootstrap_addr)

    from benchmark_convergence.observer import ObserverContext, main as obs_main
    obs_main(ObserverContext(
        task_id=procid,
        host=_hostname(),
        coord_addr=(coord_ip, coord_port),
        local_otela_url=f"http://127.0.0.1:{http_port}",
        local_admin_url=f"http://127.0.0.1:{admin_port}",
        poll_interval_ms=poll_ms,
        run_id=run_id,
    ))
    _stop_otela(proc)


def _stop_otela(proc: subprocess.Popen) -> None:
    """Cleanly terminate the otela subprocess. Without explicit wait, the
    Python parent exits, leaving otela's libp2p TCP port in TIME_WAIT.
    If Slurm reallocates the same host to the next cell within ~60s, the
    next otela startup hits 'bind: address already in use'."""
    proc.terminate()
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            pass


if __name__ == "__main__":
    main()
