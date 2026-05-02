from __future__ import annotations

from pathlib import Path
import argparse
import json

from .measure import collect
from .model import load_config
from .remote import RemoteRunner
from .render import render_heatmap


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Profile pairwise network conditions across remote machines.")
    subparsers = parser.add_subparsers(dest="command", required=True)

    plan = subparsers.add_parser("plan", help="Show the remote commands that would be executed.")
    plan.add_argument("--config", type=Path, required=True)

    collect_parser = subparsers.add_parser("collect", help="Collect pairwise ping and optional iperf3 metrics.")
    collect_parser.add_argument("--config", type=Path, required=True)
    collect_parser.add_argument("--output", type=Path, default=Path("results/network-measurements.jsonl"))
    collect_parser.add_argument("--iperf", action="store_true", help="Also collect iperf3 bandwidth measurements.")
    collect_parser.add_argument("--dry-run", action="store_true")

    heatmap = subparsers.add_parser("heatmap", help="Render an HTML heatmap from JSONL measurements.")
    heatmap.add_argument("--input", type=Path, default=Path("results/network-measurements.jsonl"))
    heatmap.add_argument("--output", type=Path, default=Path("results/network-heatmap.html"))
    heatmap.add_argument("--kind", choices=["ping", "iperf3"], default="ping")
    heatmap.add_argument("--metric", default=None, help="Defaults to avg for ping, mbps for iperf3.")
    return parser


def show_plan(config_path: Path) -> None:
    config = load_config(config_path)
    runner = RemoteRunner(config, dry_run=True)
    rows = []
    for source in config.machines:
        for target in config.machines:
            if source == target:
                continue
            rows.append(
                {
                    "source": source.name,
                    "target": target.name,
                    "ping": runner.run(source, f"ping -c {config.ping_count} {target.address}").command,
                    "iperf_server": runner.run(target, f"iperf3 -s -1 -p {config.iperf_port}").command,
                    "iperf_client": runner.run(source, f"iperf3 -J -c {target.address} -p {config.iperf_port}").command,
                }
            )
    print(json.dumps(rows, indent=2))


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.command == "plan":
        show_plan(args.config)
        return 0
    if args.command == "collect":
        config = load_config(args.config)
        collect(config, args.output, include_iperf=args.iperf, dry_run=args.dry_run)
        return 0
    if args.command == "heatmap":
        metric = args.metric or ("avg" if args.kind == "ping" else "mbps")
        render_heatmap(args.input, args.output, args.kind, metric)
        return 0
    raise AssertionError(args.command)
