"""benchmark_convergence CLI entry point."""
from __future__ import annotations

from pathlib import Path

import click


@click.group()
def cli() -> None:
    """OpenTela CRDT convergence-speed benchmark."""


@cli.command()
@click.option("--config", type=click.Path(exists=True, path_type=Path), required=True)
def doctor(config: Path) -> None:
    """Preflight checks for the benchmark."""
    from benchmark_convergence.doctor import run as run_doctor

    run_doctor(config)


@cli.command()
@click.option("--config", type=click.Path(exists=True, path_type=Path), required=True)
@click.option("--run-id", required=True)
@click.option("--resume", is_flag=True)
@click.option("--retry-failed", is_flag=True)
def run(config: Path, run_id: str, resume: bool, retry_failed: bool) -> None:
    """Run the full sweep."""
    from benchmark_convergence.run import sweep

    sweep(config=config, run_id=run_id, resume=resume, retry_failed=retry_failed)


@cli.command()
@click.option("--config", type=click.Path(exists=True, path_type=Path), required=True)
@click.option("--run-id", required=True)
@click.option("--n", "n", type=int, required=True)
@click.option("--rep", type=int, required=True)
def submit(config: Path, run_id: str, n: int, rep: int) -> None:
    """Submit a single (N, rep) cell as an sbatch job (used internally by `run`)."""
    from benchmark_convergence.submit import submit_cell

    submit_cell(config=config, run_id=run_id, n=n, rep=rep)


@cli.command("run-ssh")
@click.option(
    "--nodes", "nodes_file",
    type=click.Path(exists=True, path_type=Path), required=True,
    help="Text file with one SSH-reachable hostname per line. First line = coordinator.",
)
@click.option(
    "--otela-bin", type=click.Path(exists=True, path_type=Path), required=True,
    help="Path to the locally built otela binary (will be scp'd to each node).",
)
@click.option("--run-id", required=True, help="Run identifier; used as remote subdir.")
@click.option(
    "--output-dir", type=click.Path(path_type=Path),
    default=Path("runs"),
    help="Local directory under which results are written (default: ./runs).",
)
@click.option("--writes", type=int, default=20, help="Writes per cell.")
@click.option("--poll-ms", type=int, default=50)
@click.option("--per-write-timeout-s", type=int, default=30)
@click.option("--stabilization-timeout-s", type=int, default=60)
def run_ssh_cmd(nodes_file: Path, otela_bin: Path, run_id: str,
                output_dir: Path, writes: int, poll_ms: int,
                per_write_timeout_s: int, stabilization_timeout_s: int) -> None:
    """Run one cell across a list of SSH-reachable nodes (no SLURM)."""
    from benchmark_convergence.ssh_driver import run_ssh

    raw = nodes_file.read_text().splitlines()
    nodes = [
        ln.strip() for ln in raw
        if ln.strip() and not ln.lstrip().startswith("#")
    ]
    if len(nodes) < 2:
        raise click.UsageError(
            f"{nodes_file} must contain at least 2 hostnames "
            f"(found {len(nodes)})"
        )
    run_ssh(
        nodes=nodes, otela_bin=otela_bin, run_id=run_id,
        output_dir=output_dir, writes=writes, poll_ms=poll_ms,
        stab_to=stabilization_timeout_s, write_to=per_write_timeout_s,
    )


@cli.command()
@click.argument("run_dir", type=click.Path(exists=True, path_type=Path))
def aggregate(run_dir: Path) -> None:
    """Aggregate per-cell results.json into results.parquet."""
    from benchmark_convergence.aggregate import aggregate_run

    aggregate_run(run_dir)


@cli.command()
@click.argument("run_dir", type=click.Path(exists=True, path_type=Path))
def report(run_dir: Path) -> None:
    """Emit text-only summary.md from results.parquet."""
    from benchmark_convergence.report import build_report

    build_report(run_dir)


if __name__ == "__main__":
    cli()
