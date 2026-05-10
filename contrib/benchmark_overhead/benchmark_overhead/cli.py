"""Command-line entry point for the benchmark-overhead tool."""

import click


@click.group()
def cli() -> None:
    """OpenTela routing-overhead benchmark."""


@cli.command()
@click.option("--head-url", required=True, help="OpenTela head URL, e.g. http://relay:8092")
@click.option("--config", "config_path", type=click.Path(exists=True), help="Sweep config YAML")
def doctor(head_url: str, config_path: str | None) -> None:
    """Preflight: check head reachability, Server-Timing emission, ShareGPT cache, Slurm."""
    from benchmark_overhead.doctor import run_doctor

    run_doctor(head_url=head_url, config_path=config_path)


@cli.command()
@click.option("--config", "config_path", required=True, type=click.Path(exists=True))
@click.option("--teardown", is_flag=True, help="Cancel all sbatch jobs spawned by previous deploys.")
@click.option("--run-id", default=None, help="Run ID; required for --teardown.")
def deploy(config_path: str, teardown: bool, run_id: str | None) -> None:
    """Spin up (or tear down) sbatch workers per the sweep config."""
    from benchmark_overhead.deploy import run_deploy, run_teardown

    if teardown:
        if not run_id:
            raise click.UsageError("--teardown requires --run-id")
        run_teardown(run_id)
    else:
        run_deploy(config_path)


@cli.command()
@click.option("--config", "config_path", required=True, type=click.Path(exists=True))
@click.option("--output", "output_dir", required=True, type=click.Path())
def run(config_path: str, output_dir: str) -> None:
    """Execute the sweep and write per-request JSONL + per-cell summaries."""
    from benchmark_overhead.run import run_sweep

    run_sweep(config_path=config_path, output_dir=output_dir)


@cli.command()
@click.argument("run_dir", type=click.Path(exists=True, file_okay=False))
def report(run_dir: str) -> None:
    """Render plots from an existing run directory's raw.jsonl."""
    from benchmark_overhead.report import render_report

    render_report(run_dir)
