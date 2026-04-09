from __future__ import annotations

import hashlib
import os
from pathlib import Path

import click

from fleet_manager.cluster import ClusterConfig, ClusterConnection, Preset
from fleet_manager.relay import relay_ensure
from fleet_manager.worker import worker_submit
from fleet_manager.templates import render_template


def _local_sha256(path: str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(8192), b""):
            h.update(chunk)
    return h.hexdigest()


def _sync_binary(conn: ClusterConnection, local_path: str, remote_path: str, target: str = "relay") -> None:
    local_hash = _local_sha256(local_path)
    out, _, code = conn.run(f'sha256sum {remote_path} 2>/dev/null', target=target)
    if code == 0:
        remote_hash = out.strip().split()[0]
        if remote_hash == local_hash:
            click.echo(f"  Binary up to date")
            return
    click.echo(f"  Transferring binary...")
    conn.run(f'mkdir -p $(dirname {remote_path})', target=target)
    conn.put(local_path, remote_path, target=target)
    conn.run(f'chmod +x {remote_path}', target=target)


def deploy(
    conn: ClusterConnection,
    cfg: ClusterConfig,
    preset_name: str,
    backend: str,
    cmd: str,
    replicas: int = 1,
) -> list[str]:
    if preset_name not in cfg.presets:
        raise ValueError(f"Unknown preset '{preset_name}' for cluster '{cfg.name}'. Available: {list(cfg.presets.keys())}")
    preset = cfg.presets[preset_name]
    click.echo(f"Deploying to {cfg.name} (preset={preset_name}, backend={backend}, replicas={replicas})...")
    _sync_binary(conn, cfg.binary_local_path, cfg.binary_remote_path)
    dirs = "~/opentela ~/.config/opentela ~/logs"
    if cfg.container_runtime == "enroot":
        dirs += " ~/.edf"
    conn.run(f"mkdir -p {dirs}", target="slurm")
    if cfg.relay_skip:
        bootstrap_sources = cfg.relay_bootstrap
    else:
        bootstrap_sources = [cfg.relay_multiaddr]
    worker_config = render_template("worker.cfg.yaml.j2", {
        "cluster_name": cfg.name,
        "worker_seed": cfg.worker_seed,
        "worker_port": cfg.worker_port,
        "service_port": cfg.worker_service_port,
        "require_signed_binary": cfg.require_signed_binary,
        "skip_verification": cfg.skip_verification,
        "bootstrap_sources": bootstrap_sources,
    })
    conn.put_string(worker_config, "~/.config/opentela/cfg.yaml", target="slurm")
    if cfg.container_runtime == "enroot" and cfg.container_edf_template:
        host_env = {}
        for var in cfg.container_env_from_host:
            val = os.environ.get(var, "")
            if val:
                host_env[var] = val
        edf_content = render_template(cfg.container_edf_template, {
            "container_image": cfg.container_image,
            "container_mounts": cfg.container_mounts,
            "container_env": cfg.container_env,
            "host_env": host_env,
        })
        conn.put_string(edf_content, cfg.container_edf_remote_path, target="slurm")
    relay_ensure(conn, cfg)
    job_ids = []
    for _ in range(replicas):
        job_id = worker_submit(conn, cfg, preset, preset_name, backend, cmd)
        job_ids.append(job_id)
    click.echo(f"  Done! {len(job_ids)} job(s) submitted: {', '.join(job_ids)}")
    return job_ids
