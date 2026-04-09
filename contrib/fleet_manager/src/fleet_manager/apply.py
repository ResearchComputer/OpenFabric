from __future__ import annotations

from dataclasses import dataclass
from typing import Optional

import yaml
import click

from fleet_manager.cluster import ClusterConfig, ClusterConnection, load_cluster, job_identity
from fleet_manager.worker import worker_list, worker_cancel, Job
from fleet_manager.deploy import deploy


@dataclass
class Action:
    action: str  # "deploy" or "cancel"
    cluster: str
    backend: str
    cmd: str
    preset: str
    job_id: Optional[str] = None


def parse_fleet_file(path: str) -> list[tuple[str, str, str, str, int]]:
    with open(path) as f:
        data = yaml.safe_load(f)
    result = []
    for dep in data.get("deployments", []):
        result.append((
            dep["cluster"],
            dep["backend"],
            dep["cmd"],
            dep["preset"],
            dep.get("replicas", 1),
        ))
    return result


def compute_diff(
    desired: list[tuple[str, str, str, str, int]],
    live_jobs: dict[str, list[Job]],
) -> list[Action]:
    actions = []
    for cluster, backend, cmd, preset, replicas in desired:
        job_name = job_identity(backend, cmd, preset)
        cluster_jobs = live_jobs.get(cluster, [])
        matching = [j for j in cluster_jobs if j.name == job_name]
        current = len(matching)
        if current < replicas:
            for _ in range(replicas - current):
                actions.append(Action(action="deploy", cluster=cluster, backend=backend, cmd=cmd, preset=preset))
        elif current > replicas:
            excess = sorted(matching, key=lambda j: int(j.id) if j.id and str(j.id).isdigit() else 0, reverse=True)[: current - replicas]
            for job in excess:
                actions.append(Action(action="cancel", cluster=cluster, backend=backend, cmd=cmd, preset=preset, job_id=job.id))
    return actions


def apply(fleet_file: str, cluster_dir: str, dry_run: bool = False) -> None:
    desired = parse_fleet_file(fleet_file)
    cluster_names = sorted(set(c for c, _, _, _, _ in desired))
    click.echo(f"Fleet file: {fleet_file}")
    click.echo(f"Clusters: {', '.join(cluster_names)}")
    click.echo()
    configs: dict[str, ClusterConfig] = {}
    connections: dict[str, ClusterConnection] = {}
    live_jobs: dict[str, list[Job]] = {}
    for name in cluster_names:
        try:
            cfg = load_cluster(name, cluster_dir=cluster_dir)
            conn = ClusterConnection(cfg)
            configs[name] = cfg
            connections[name] = conn
            live_jobs[name] = worker_list(conn, target="slurm")
            click.echo(f"  {name}: {len(live_jobs[name])} running jobs")
        except Exception as e:
            click.echo(f"  {name}: ERROR - {e}")
            live_jobs[name] = []
    actions = compute_diff(desired, live_jobs)
    if not actions:
        click.echo("\nNo changes needed. Fleet is at desired state.")
        return
    click.echo(f"\nPlanned actions ({len(actions)}):")
    for a in actions:
        if a.action == "deploy":
            click.echo(f"  + deploy {a.backend} ({a.preset}) on {a.cluster}")
        elif a.action == "cancel":
            click.echo(f"  - cancel job {a.job_id} on {a.cluster}")
    if dry_run:
        click.echo("\n(dry run - no changes made)")
        return
    click.echo()
    results = []
    for a in actions:
        try:
            conn = connections[a.cluster]
            cfg = configs[a.cluster]
            if a.action == "deploy":
                job_ids = deploy(conn, cfg, a.preset, a.backend, a.cmd, replicas=1)
                results.append((a, "ok", job_ids[0]))
            elif a.action == "cancel":
                worker_cancel(conn, job_id=a.job_id, target="slurm")
                results.append((a, "ok", None))
        except Exception as e:
            results.append((a, "failed", str(e)))
    click.echo("\nSummary:")
    for a, status, detail in results:
        symbol = "+" if a.action == "deploy" else "-"
        if status == "ok":
            extra = f" (job {detail})" if detail else ""
            click.echo(f"  {symbol} {a.action} {a.backend} on {a.cluster}: OK{extra}")
        else:
            click.echo(f"  {symbol} {a.action} {a.backend} on {a.cluster}: FAILED - {detail}")
    for conn in connections.values():
        conn.close()
