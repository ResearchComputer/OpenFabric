import os
from pathlib import Path

import click

from fleet_manager.cluster import load_cluster, list_clusters, ClusterConnection
from fleet_manager.relay import relay_status
from fleet_manager.worker import worker_list, worker_cancel, worker_logs
from fleet_manager.deploy import deploy
from fleet_manager.apply import apply as apply_fleet


def _default_cluster_dir() -> str:
    """Return the default cluster config directory.

    Precedence:
    1. ./clusters (if it exists in cwd)
    2. ~/.config/opentela/fleet/clusters/
    """
    local = Path("./clusters")
    if local.is_dir():
        return str(local)
    return str(Path.home() / ".config" / "opentela" / "fleet" / "clusters")


@click.group()
@click.option("--cluster-dir", default=None, help="Path to cluster YAML configs")
@click.pass_context
def cli(ctx, cluster_dir):
    """Fleet manager for OpenTela deployments."""
    ctx.ensure_object(dict)
    ctx.obj["cluster_dir"] = cluster_dir or _default_cluster_dir()


@cli.command("start")
@click.argument("cluster")
@click.option("--backend", required=True, help="Serving backend (e.g., sglang, vllm)")
@click.option("--cmd", required=True, help="Serving command to run inside container")
@click.option("--preset", required=True, help="Hardware preset name from cluster config")
@click.option("--replicas", default=1, help="Number of independent SLURM jobs")
@click.pass_context
def start_cmd(ctx, cluster, backend, cmd, preset, replicas):
    """Start serving jobs on a cluster."""
    cfg = load_cluster(cluster, ctx.obj["cluster_dir"])
    conn = ClusterConnection(cfg)
    try:
        job_ids = deploy(conn, cfg, preset, backend, cmd, replicas=replicas)
        for jid in job_ids:
            click.echo(f"  Check logs: otela-fleet logs {cluster} {jid}")
    finally:
        conn.close()


@cli.command("stop")
@click.argument("cluster")
@click.argument("job_id", required=False)
@click.pass_context
def stop_cmd(ctx, cluster, job_id):
    """Stop jobs on a cluster."""
    cfg = load_cluster(cluster, ctx.obj["cluster_dir"])
    conn = ClusterConnection(cfg)
    try:
        worker_cancel(conn, job_id=job_id, target="slurm")
    finally:
        conn.close()


@cli.command()
@click.argument("cluster", required=False)
@click.pass_context
def status(ctx, cluster):
    """Show cluster status: relay, running jobs."""
    cluster_dir = ctx.obj["cluster_dir"]
    names = [cluster] if cluster else list_clusters(cluster_dir)
    for name in names:
        try:
            cfg = load_cluster(name, cluster_dir)
            conn = ClusterConnection(cfg)
            click.echo(f"\n=== {name} ===")
            rs = relay_status(conn, cfg)
            click.echo(f"  Relay: {rs.value}")
            jobs = worker_list(conn, target="slurm")
            if jobs:
                click.echo(f"  Jobs ({len(jobs)}):")
                for j in jobs:
                    click.echo(f"    {j.id}  {j.name}  {j.state}  {j.elapsed}/{j.time_limit}  {j.node}")
            else:
                click.echo("  Jobs: none")
            conn.close()
        except Exception as e:
            click.echo(f"\n=== {name} ===")
            click.echo(f"  ERROR: {e}")


@cli.command()
@click.argument("cluster")
@click.argument("job_id")
@click.pass_context
def logs(ctx, cluster, job_id):
    """Show logs for a job."""
    cfg = load_cluster(cluster, ctx.obj["cluster_dir"])
    conn = ClusterConnection(cfg)
    try:
        out_log, err_log = worker_logs(conn, job_id, target="slurm")
        if out_log:
            click.echo("=== STDOUT ===")
            click.echo(out_log)
        if err_log:
            click.echo("=== STDERR ===")
            click.echo(err_log)
        if not out_log and not err_log:
            click.echo("No logs found.")
    finally:
        conn.close()


@cli.command("apply")
@click.argument("fleet_file")
@click.option("--dry-run", is_flag=True, help="Show what would change")
@click.pass_context
def apply_cmd(ctx, fleet_file, dry_run):
    """Apply a fleet definition file."""
    apply_fleet(fleet_file, ctx.obj["cluster_dir"], dry_run=dry_run)


@cli.command("clusters")
@click.pass_context
def clusters_cmd(ctx):
    """List available clusters."""
    cluster_dir = ctx.obj["cluster_dir"]
    names = list_clusters(cluster_dir)
    if not names:
        click.echo(f"No clusters found in {cluster_dir}")
        return
    click.echo(f"Clusters ({cluster_dir}):")
    for name in names:
        try:
            cfg = load_cluster(name, cluster_dir)
            preset_names = ", ".join(cfg.presets.keys())
            click.echo(f"  {name}  ({cfg.arch}, {cfg.container_runtime})  presets: {preset_names}")
        except Exception as e:
            click.echo(f"  {name}  ERROR: {e}")


@cli.command("presets")
@click.argument("cluster")
@click.pass_context
def presets_cmd(ctx, cluster):
    """List presets for a cluster."""
    cfg = load_cluster(cluster, ctx.obj["cluster_dir"])
    if not cfg.presets:
        click.echo(f"No presets defined for {cluster}")
        return
    click.echo(f"Presets for {cluster}:")
    for name, preset in cfg.presets.items():
        nodes_str = f"{preset.nodes} node{'s' if preset.nodes > 1 else ''}"
        click.echo(f"  {name}")
        click.echo(f"    partition: {preset.partition}  account: {preset.account}")
        click.echo(f"    gpus: {preset.gpus}  {nodes_str}  time: {preset.time}")
        if preset.cpus_per_task:
            click.echo(f"    cpus_per_task: {preset.cpus_per_task}")
        if preset.extra_sbatch:
            click.echo(f"    extra: {', '.join(preset.extra_sbatch)}")
