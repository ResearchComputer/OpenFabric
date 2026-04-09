from __future__ import annotations

import time
from enum import Enum

import click

from fleet_manager.cluster import ClusterConfig, ClusterConnection
from fleet_manager.templates import render_template


class RelayStatus(Enum):
    RUNNING = "running"
    STOPPED = "stopped"
    DEGRADED = "degraded"


def relay_status(conn: ClusterConnection, cfg: ClusterConfig) -> RelayStatus:
    """Check if the relay is running on the cluster."""
    pattern = f"{cfg.binary_remote_path}.*relay.cfg.yaml"
    out, _, code = conn.run(f'pgrep -f "{pattern}"', target="relay")
    if code != 0:
        return RelayStatus.STOPPED

    # Process exists, check HTTP port
    out, _, code = conn.run(
        f'curl -s --connect-timeout 3 http://localhost:{cfg.relay_port}/v1/dnt/bootstraps',
        target="relay",
    )
    if code != 0:
        return RelayStatus.DEGRADED

    return RelayStatus.RUNNING


def relay_start(conn: ClusterConnection, cfg: ClusterConfig) -> None:
    """Start the relay on the cluster."""
    click.echo(f"  Starting relay on {cfg.name}...")

    # Ensure remote dirs
    conn.run("mkdir -p ~/opentela", target="relay")

    # Render and transfer relay config
    relay_config = render_template("relay.cfg.yaml.j2", {
        "cluster_name": cfg.name,
        "relay_seed": cfg.relay_seed,
        "relay_port": cfg.relay_port,
        "relay_tcp_port": cfg.relay_tcp_port,
        "relay_udp_port": cfg.relay_udp_port,
        "require_signed_binary": cfg.require_signed_binary,
        "skip_verification": cfg.skip_verification,
        "bootstrap_sources": cfg.relay_bootstrap,
    })
    conn.put_string(relay_config, cfg.relay_config_remote_path, target="relay")

    # Kill existing relay
    pattern = f"{cfg.binary_remote_path}.*relay.cfg.yaml"
    conn.run(f'pkill -f "{pattern}" 2>/dev/null || true', target="relay")
    time.sleep(1)

    # Clean and recreate home override
    conn.run(f'rm -rf {cfg.relay_home_override} && mkdir -p {cfg.relay_home_override}', target="relay")

    # Start relay
    cmd = (
        f'nohup env HOME={cfg.relay_home_override} '
        f'{cfg.binary_remote_path} start --config {cfg.relay_config_remote_path} '
        f'> {cfg.relay_log_path} 2>&1 &'
    )
    conn.run(cmd, target="relay")

    # Poll for readiness
    click.echo(f"  Waiting for relay to be ready...")
    for i in range(30):
        time.sleep(1)
        out, _, code = conn.run(
            f'curl -s --connect-timeout 2 http://localhost:{cfg.relay_port}/v1/dnt/bootstraps',
            target="relay",
        )
        if code == 0:
            click.echo(f"  Relay is ready (took {i+1}s)")
            return

    # Failed — show logs
    out, _, _ = conn.run(f'tail -30 {cfg.relay_log_path}', target="relay")
    raise RuntimeError(f"Relay failed to start on {cfg.name}. Last logs:\n{out}")


def relay_stop(conn: ClusterConnection, cfg: ClusterConfig) -> None:
    """Stop the relay on the cluster."""
    pattern = f"{cfg.binary_remote_path}.*relay.cfg.yaml"
    conn.run(f'pkill -f "{pattern}" 2>/dev/null || true', target="relay")
    conn.run(f'rm -rf {cfg.relay_home_override}', target="relay")
    click.echo(f"  Relay stopped on {cfg.name}")


def relay_ensure(conn: ClusterConnection, cfg: ClusterConfig) -> None:
    """Ensure the relay is running. Start it if not."""
    if cfg.relay_skip:
        click.echo(f"  Relay skipped on {cfg.name} (using WSS direct to heads)")
        return
    status = relay_status(conn, cfg)
    if status == RelayStatus.RUNNING:
        click.echo(f"  Relay already running on {cfg.name}")
        return
    relay_start(conn, cfg)
