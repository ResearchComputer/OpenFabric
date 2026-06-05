#!/usr/bin/env python3
"""
discovery_speed.py — Measure how fast nodes discover each other in OpenTela.

Polls the head node's DNT stats API to track peer discovery over time.
Outputs a JSON timeline and can generate comparison plots across cluster sizes.

Usage:
    # Measure discovery for a cluster that should have 51 total nodes:
    python discovery_speed.py --expected-nodes 51 --timeout 300

    # Plot discovery curves from multiple runs:
    python discovery_speed.py --plot --results-dir results/20260306_120000
"""

import argparse
import asyncio
import json
import os
import sys
import time
from pathlib import Path

import aiohttp


# ── Defaults ──────────────────────────────────────────────────────────────────
DEFAULT_HEAD_NODE = "http://localhost:8092"
DEFAULT_POLL_INTERVAL = 1.0  # seconds
DEFAULT_TIMEOUT = 300  # seconds

DNT_STATS_PATH = "/v1/dnt/stats"
DNT_TABLE_PATH = "/v1/dnt/table"


# ── Core discovery measurement ───────────────────────────────────────────────

async def poll_discovery(
    base_url: str,
    expected_nodes: int,
    poll_interval: float,
    timeout: float,
) -> dict:
    """Poll the head node and record the peer discovery timeline."""
    stats_url = base_url.rstrip("/") + DNT_STATS_PATH
    table_url = base_url.rstrip("/") + DNT_TABLE_PATH

    timeline: list[dict] = []
    start_time = time.monotonic()
    full_discovery_time: float | None = None

    connector = aiohttp.TCPConnector(limit=5)
    async with aiohttp.ClientSession(connector=connector) as session:
        print(f"Polling {stats_url} every {poll_interval}s (timeout={timeout}s)")
        print(f"Expecting {expected_nodes} total nodes")
        print()

        prev_known = -1
        while True:
            elapsed = time.monotonic() - start_time
            if elapsed > timeout:
                print(f"\nTimeout reached ({timeout}s)")
                break

            try:
                async with session.get(stats_url) as resp:
                    if resp.status == 200:
                        data = await resp.json()
                        peers_known = data.get("total_peers_known", 0)
                        connected = data.get("connected_peers", 0)
                    else:
                        peers_known = 0
                        connected = 0
            except Exception:
                # Head node might not be ready yet
                peers_known = 0
                connected = 0

            entry = {
                "elapsed_s": round(elapsed, 2),
                "peers_known": peers_known,
                "connected_peers": connected,
            }
            timeline.append(entry)

            if peers_known != prev_known:
                print(
                    f"  t={elapsed:6.1f}s  |  known={peers_known:4d}  |  "
                    f"connected={connected:4d}"
                )
                prev_known = peers_known

            # Check if all nodes are discovered
            if peers_known >= expected_nodes and full_discovery_time is None:
                full_discovery_time = elapsed
                print(f"\n  ✓ Full discovery at t={elapsed:.1f}s "
                      f"({peers_known} peers)")
                # Record a few more samples then stop
                for _ in range(5):
                    await asyncio.sleep(poll_interval)
                    elapsed2 = time.monotonic() - start_time
                    try:
                        async with session.get(stats_url) as resp2:
                            if resp2.status == 200:
                                data2 = await resp2.json()
                                timeline.append({
                                    "elapsed_s": round(elapsed2, 2),
                                    "peers_known": data2.get("total_peers_known", 0),
                                    "connected_peers": data2.get("connected_peers", 0),
                                })
                    except Exception:
                        pass
                break

            await asyncio.sleep(poll_interval)

    total_elapsed = time.monotonic() - start_time
    final_known = timeline[-1]["peers_known"] if timeline else 0

    # Compute discovery rate (peers per second) from the timeline
    rates: list[float] = []
    for i in range(1, len(timeline)):
        dt = timeline[i]["elapsed_s"] - timeline[i - 1]["elapsed_s"]
        dp = timeline[i]["peers_known"] - timeline[i - 1]["peers_known"]
        if dt > 0 and dp > 0:
            rates.append(dp / dt)

    result = {
        "expected_nodes": expected_nodes,
        "final_peers_known": final_known,
        "full_discovery_time_s": round(full_discovery_time, 2) if full_discovery_time else None,
        "total_elapsed_s": round(total_elapsed, 2),
        "discovery_complete": full_discovery_time is not None,
        "avg_discovery_rate_peers_per_s": round(sum(rates) / len(rates), 2) if rates else 0,
        "peak_discovery_rate_peers_per_s": round(max(rates), 2) if rates else 0,
        "poll_interval_s": poll_interval,
        "timeline": timeline,
    }

    print()
    print(f"  Final peers known:  {final_known}/{expected_nodes}")
    if full_discovery_time is not None:
        print(f"  Time to full disco: {full_discovery_time:.1f}s")
    else:
        print(f"  Discovery incomplete after {total_elapsed:.1f}s")
    if rates:
        print(f"  Avg discovery rate: {result['avg_discovery_rate_peers_per_s']:.1f} peers/s")
        print(f"  Peak discovery rate: {result['peak_discovery_rate_peers_per_s']:.1f} peers/s")
    print()

    return result


# ── Plotting ──────────────────────────────────────────────────────────────────

def load_results(results_dir: str) -> list[dict]:
    """Load all discovery_*.json files, sorted by expected_nodes."""
    results = []
    rdir = Path(results_dir)
    for f in sorted(rdir.glob("discovery_*.json")):
        with open(f) as fh:
            data = json.load(fh)
            results.append(data)
    results.sort(key=lambda r: r.get("expected_nodes", 0))
    return results


def plot_discovery(results: list[dict], output_dir: str):
    """Generate discovery curve plots."""
    import matplotlib.pyplot as plt
    from tabulate import tabulate as tabfmt

    if not results:
        print("No results to plot.", file=sys.stderr)
        return

    outdir = Path(output_dir)
    outdir.mkdir(parents=True, exist_ok=True)

    # ── Discovery curves: peers known vs time, one line per cluster size ──
    fig, ax = plt.subplots(figsize=(10, 6))

    for r in results:
        n = r["expected_nodes"]
        timeline = r.get("timeline", [])
        if not timeline:
            continue
        times = [e["elapsed_s"] for e in timeline]
        peers = [e["peers_known"] for e in timeline]
        label = f"{n} nodes"
        if r.get("full_discovery_time_s"):
            label += f" ({r['full_discovery_time_s']:.0f}s)"
        ax.plot(times, peers, "o-", label=label, markersize=3, linewidth=1.5)

    ax.set_xlabel("Time (seconds)")
    ax.set_ylabel("Peers Discovered")
    ax.set_title("Node Discovery Speed vs. Cluster Size")
    ax.legend()
    ax.grid(True, alpha=0.3)
    fig.tight_layout()

    curves_path = outdir / "discovery_curves.png"
    fig.savefig(curves_path, dpi=150)
    print(f"Saved: {curves_path}")
    plt.close(fig)

    # ── Time to full discovery vs cluster size ──
    sizes_with_time = [
        (r["expected_nodes"], r["full_discovery_time_s"])
        for r in results
        if r.get("full_discovery_time_s") is not None
    ]

    if len(sizes_with_time) >= 2:
        fig, ax = plt.subplots(figsize=(10, 6))
        sizes_plot = [s[0] for s in sizes_with_time]
        times_plot = [s[1] for s in sizes_with_time]
        ax.plot(sizes_plot, times_plot, "o-", color="red", linewidth=2, markersize=8)
        ax.set_xlabel("Cluster Size (nodes)")
        ax.set_ylabel("Time to Full Discovery (seconds)")
        ax.set_title("Discovery Time vs. Cluster Size")
        ax.grid(True, alpha=0.3)
        fig.tight_layout()
        time_path = outdir / "discovery_time_scaling.png"
        fig.savefig(time_path, dpi=150)
        print(f"Saved: {time_path}")
        plt.close(fig)

    # ── Summary table ──
    table_data = []
    for r in results:
        disco_time = r.get("full_discovery_time_s")
        table_data.append([
            r["expected_nodes"],
            r["final_peers_known"],
            f"{disco_time:.1f}" if disco_time else "incomplete",
            r.get("avg_discovery_rate_peers_per_s", "—"),
            r.get("peak_discovery_rate_peers_per_s", "—"),
        ])

    headers = ["Nodes", "Found", "Time(s)", "Avg Rate(p/s)", "Peak Rate(p/s)"]
    table_str = tabfmt(table_data, headers=headers, tablefmt="github")
    print("\n" + table_str + "\n")

    summary_path = outdir / "discovery_summary.txt"
    with open(summary_path, "w") as f:
        f.write(table_str + "\n")
    print(f"Saved: {summary_path}")


# ── CLI ───────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        description="Measure OpenTela peer discovery speed.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""\
Examples:
  # Measure discovery for a 51-node cluster (1 head + 50 workers):
  %(prog)s --expected-nodes 51 --timeout 300

  # Plot discovery curves from multiple runs:
  %(prog)s --plot --results-dir results/20260306_120000
""",
    )

    parser.add_argument(
        "--head-node-url",
        default=DEFAULT_HEAD_NODE,
        help=f"Head node base URL (default: {DEFAULT_HEAD_NODE})",
    )
    parser.add_argument(
        "--expected-nodes",
        type=int,
        default=10,
        help="Total number of nodes expected (head + workers). "
             "Discovery is complete when this count is reached.",
    )
    parser.add_argument(
        "--poll-interval",
        type=float,
        default=DEFAULT_POLL_INTERVAL,
        help=f"Seconds between polls (default: {DEFAULT_POLL_INTERVAL})",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=DEFAULT_TIMEOUT,
        help=f"Max seconds to wait for discovery (default: {DEFAULT_TIMEOUT})",
    )
    parser.add_argument(
        "--output", "-o",
        default=None,
        help="Output JSON file path (default: discovery_<expected_nodes>.json)",
    )
    parser.add_argument(
        "--plot",
        action="store_true",
        help="Plot mode: aggregate results from --results-dir and generate charts",
    )
    parser.add_argument(
        "--results-dir",
        default=".",
        help="Directory containing discovery_*.json files (used with --plot)",
    )

    args = parser.parse_args()

    if args.plot:
        results = load_results(args.results_dir)
        if not results:
            print(f"No discovery_*.json files found in {args.results_dir}",
                  file=sys.stderr)
            sys.exit(1)
        plot_discovery(results, args.results_dir)
    else:
        result = asyncio.run(
            poll_discovery(
                base_url=args.head_node_url,
                expected_nodes=args.expected_nodes,
                poll_interval=args.poll_interval,
                timeout=args.timeout,
            )
        )

        out_path = args.output
        if out_path is None:
            out_path = f"discovery_{result['expected_nodes']}.json"

        with open(out_path, "w") as f:
            json.dump(result, f, indent=2)
        print(f"Results saved to: {out_path}")


if __name__ == "__main__":
    main()
