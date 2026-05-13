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
