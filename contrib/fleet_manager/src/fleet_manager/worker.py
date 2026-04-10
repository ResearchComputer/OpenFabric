from __future__ import annotations

from dataclasses import dataclass
from typing import Optional

import click

from fleet_manager.cluster import ClusterConfig, ClusterConnection, Preset, job_identity
from fleet_manager.templates import render_template


@dataclass
class Job:
    id: str
    name: str
    state: str
    elapsed: str
    time_limit: str
    node: str


def select_template(runtime: str, nodes: int) -> str:
    if runtime == "apptainer":
        return "apptainer_multi.sh.j2" if nodes > 1 else "apptainer_single.sh.j2"
    elif runtime == "enroot":
        return "enroot_single.sh.j2"
    else:
        raise ValueError(f"Unsupported container runtime: {runtime}")


def build_exec_prefix(cfg: ClusterConfig) -> str:
    if cfg.container_runtime == "enroot":
        return f'srun --environment={cfg.container_edf_remote_path}'
    parts = ["apptainer exec"]
    for flag in cfg.container_apptainer_flags:
        parts.append(f"    {flag}")
    for mount in cfg.container_mounts:
        parts.append(f'    --bind "{mount}"')
    parts.append(f'    "{cfg.container_sif_path}"')
    return " \\\n".join(parts)


def worker_list(conn: ClusterConnection, target: str = "slurm") -> list[Job]:
    out, _, code = conn.run(
        'squeue -u $USER -o "%i|%j|%T|%M|%l|%N" --noheader',
        target=target,
    )
    if code != 0 or not out.strip():
        return []
    jobs = []
    for line in out.strip().split("\n"):
        parts = line.split("|")
        if len(parts) < 5:
            continue
        job = Job(
            id=parts[0].strip(),
            name=parts[1].strip(),
            state=parts[2].strip(),
            elapsed=parts[3].strip(),
            time_limit=parts[4].strip(),
            node=parts[5].strip() if len(parts) > 5 else "",
        )
        if job.name.startswith("opentela-"):
            jobs.append(job)
    return jobs


def _effective_proxychains(cfg: ClusterConfig, preset: Preset) -> dict:
    """Resolve the proxychains block for a given preset.

    Returns a dict the templates can read with an `enabled` key that's only
    True when the cluster enables proxychains AND the preset's partition is
    not in `skip_partitions`.
    """
    pc = cfg.proxychains
    enabled = pc.enabled and preset.partition not in pc.skip_partitions
    return {
        "enabled": enabled,
        "ssh_key": pc.ssh_key,
        "proxy_target": pc.proxy_target,
        "socks_port": pc.socks_port,
    }


def worker_submit(
    conn: ClusterConnection,
    cfg: ClusterConfig,
    preset: Preset,
    preset_name: str,
    backend: str,
    cmd: str,
) -> str:
    job_name = job_identity(backend, cmd, preset_name)
    template_name = select_template(cfg.container_runtime, preset.nodes)
    template_vars = {
        "job_name": job_name,
        "partition": preset.partition,
        "account": preset.account,
        "time": preset.time,
        "gpus": preset.gpus,
        "nodes": preset.nodes,
        "cpus_per_task": preset.cpus_per_task,
        "extra_sbatch": preset.extra_sbatch,
        "log_dir": "~/logs",
        "binary_path": cfg.binary_remote_path,
        "worker_config": "~/.config/opentela/cfg.yaml",
        "hf_cache": cfg.container_hf_cache,
        "container_exec_prefix": build_exec_prefix(cfg),
        "sif_path": cfg.container_sif_path,
        "container_image": cfg.container_image,
        "pull_if_missing": cfg.container_pull_if_missing,
        "user_cmd": cmd,
        "service_port": cfg.worker_service_port,
        "startup_timeout": 300,
        "modules": cfg.modules,
        "nccl_env": cfg.container_env,
        "edf_path": cfg.container_edf_remote_path,
        "container_mounts": cfg.container_mounts,
        "container_env": cfg.container_env,
        "apptainer_flags": cfg.container_apptainer_flags,
        "proxychains": _effective_proxychains(cfg, preset),
    }
    job_script = render_template(template_name, template_vars)
    conn.run("mkdir -p ~/opentela ~/logs", target="slurm")
    remote_script = f"~/opentela/job_{job_name}.sh"
    conn.put_string(job_script, remote_script, target="slurm")
    out, err, code = conn.run(f"cd ~/opentela && sbatch {remote_script}", target="slurm")
    if code != 0:
        raise RuntimeError(f"sbatch failed on {cfg.name}: {err.strip()}")
    job_id = out.strip().split()[-1]
    click.echo(f"  Submitted job {job_id} on {cfg.name} ({job_name})")
    return job_id


def worker_cancel(conn: ClusterConnection, job_id: Optional[str] = None, target: str = "slurm") -> None:
    if job_id:
        conn.run(f"scancel {job_id}", target=target)
        click.echo(f"  Cancelled job {job_id}")
    else:
        jobs = worker_list(conn, target=target)
        for job in jobs:
            conn.run(f"scancel {job.id}", target=target)
            click.echo(f"  Cancelled job {job.id} ({job.name})")


def worker_logs(conn: ClusterConnection, job_id: str, target: str = "slurm") -> tuple[str, str]:
    out_log, _, _ = conn.run(f"cat ~/logs/opentela_{job_id}.out 2>/dev/null", target=target)
    err_log, _, _ = conn.run(f"cat ~/logs/opentela_{job_id}.err 2>/dev/null", target=target)
    return out_log, err_log
