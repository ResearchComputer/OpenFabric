#!/usr/bin/env python3
"""
routing_overhead.py — Measure OpenTela routing overhead as cluster size grows.

Sends requests through the P2P service-routing endpoint and collects latency
statistics. Can also aggregate results from multiple runs at different cluster
sizes and produce scaling plots.

Usage:
    # Single run against a live cluster:
    python routing_overhead.py --requests 500 --concurrency 20

    # Generate scaling plots from collected results:
    python routing_overhead.py --plot --results-dir results/20260306_120000
"""

import argparse
import asyncio
import json
import os
import statistics
import sys
import time
from pathlib import Path

import aiohttp


# ── Defaults ──────────────────────────────────────────────────────────────────
DEFAULT_HEAD_NODE = "http://localhost:8092"
DEFAULT_REQUESTS = 500
DEFAULT_CONCURRENCY = 20
DEFAULT_WARMUP = 50

ECHO_SERVICE_PATH = "/v1/service/llm/v1/echo"
DNT_STATS_PATH = "/v1/dnt/stats"
ECHO_PAYLOAD = {"model": "echo", "message": "benchmark-probe", "timestamp": 0}


# ── Core benchmark logic ─────────────────────────────────────────────────────

async def make_request(session: aiohttp.ClientSession, url: str) -> float | None:
    """Send one POST to the echo endpoint; return latency in seconds or None."""
    start = time.perf_counter()
    try:
        async with session.post(url, json=ECHO_PAYLOAD) as resp:
            await resp.read()
            if resp.status != 200:
                return None
    except Exception:
        return None
    return time.perf_counter() - start


async def worker(
    session: aiohttp.ClientSession,
    url: str,
    queue: asyncio.Queue,
    results: list[float],
):
    while True:
        try:
            queue.get_nowait()
        except asyncio.QueueEmpty:
            break
        latency = await make_request(session, url)
        if latency is not None:
            results.append(latency)
        queue.task_done()


async def get_cluster_size(session: aiohttp.ClientSession, base_url: str) -> dict:
    """Query /v1/dnt/stats and return the raw JSON."""
    url = base_url.rstrip("/") + DNT_STATS_PATH
    try:
        async with session.get(url) as resp:
            if resp.status == 200:
                return await resp.json()
    except Exception:
        pass
    return {}


async def run_benchmark(
    base_url: str,
    num_requests: int,
    concurrency: int,
    warmup: int,
) -> dict:
    """Run the routing benchmark and return a results dict."""
    echo_url = base_url.rstrip("/") + ECHO_SERVICE_PATH

    connector = aiohttp.TCPConnector(limit=concurrency)
    async with aiohttp.ClientSession(connector=connector) as session:
        # ── Cluster info ──
        stats = await get_cluster_size(session, base_url)
        cluster_size = stats.get("total_peers_known", -1)
        connected_peers = stats.get("connected_peers", -1)

        print(f"Cluster: {cluster_size} peers known, {connected_peers} connected")

        # ── Warmup ──
        print(f"Warming up ({warmup} requests) …")
        for _ in range(warmup):
            await make_request(session, echo_url)

        # ── Timed benchmark ──
        print(f"Benchmarking ({num_requests} requests, concurrency={concurrency}) …")
        queue: asyncio.Queue[int] = asyncio.Queue()
        for i in range(num_requests):
            queue.put_nowait(i)

        results: list[float] = []
        wall_start = time.perf_counter()

        tasks = [
            asyncio.create_task(worker(session, echo_url, queue, results))
            for _ in range(concurrency)
        ]
        await asyncio.gather(*tasks)
        wall_elapsed = time.perf_counter() - wall_start

    successful = len(results)
    if successful == 0:
        print("ERROR: all requests failed!", file=sys.stderr)
        return {"error": "all requests failed"}

    latencies_ms = [l * 1000 for l in results]
    quantiles = statistics.quantiles(latencies_ms, n=100)

    result = {
        "cluster_size": cluster_size,
        "connected_peers": connected_peers,
        "total_requests": num_requests,
        "successful_requests": successful,
        "failed_requests": num_requests - successful,
        "wall_time_s": round(wall_elapsed, 3),
        "throughput_rps": round(successful / wall_elapsed, 2),
        "latency_ms": {
            "avg": round(statistics.mean(latencies_ms), 3),
            "p50": round(quantiles[49], 3),
            "p90": round(quantiles[89], 3),
            "p99": round(quantiles[98], 3),
            "min": round(min(latencies_ms), 3),
            "max": round(max(latencies_ms), 3),
            "stdev": round(statistics.stdev(latencies_ms), 3) if successful > 1 else 0,
        },
        "concurrency": concurrency,
        "warmup_requests": warmup,
    }

    # ── Print summary ──
    print()
    print(f"  Cluster size:      {cluster_size}")
    print(f"  Successful:        {successful}/{num_requests}")
    print(f"  Wall time:         {wall_elapsed:.2f} s")
    print(f"  Throughput:        {result['throughput_rps']:.2f} req/s")
    print(f"  Avg latency:       {result['latency_ms']['avg']:.2f} ms")
    print(f"  P50 latency:       {result['latency_ms']['p50']:.2f} ms")
    print(f"  P90 latency:       {result['latency_ms']['p90']:.2f} ms")
    print(f"  P99 latency:       {result['latency_ms']['p99']:.2f} ms")
    print()

    return result


# ── Plotting ──────────────────────────────────────────────────────────────────

def load_results(results_dir: str) -> list[dict]:
    """Load all routing_*.json files from a directory, sorted by cluster_size."""
    results = []
    rdir = Path(results_dir)
    for f in sorted(rdir.glob("routing_*.json")):
        with open(f) as fh:
            data = json.load(fh)
            if "error" not in data:
                results.append(data)
    results.sort(key=lambda r: r.get("cluster_size", 0))
    return results


def plot_scaling(results: list[dict], output_dir: str):
    """Generate scaling plots from aggregated results."""
    import matplotlib.pyplot as plt
    from tabulate import tabulate as tabfmt

    if len(results) < 2:
        print("Need results from at least 2 cluster sizes to plot.", file=sys.stderr)
        return

    sizes = [r["cluster_size"] for r in results]
    avg_lat = [r["latency_ms"]["avg"] for r in results]
    p50_lat = [r["latency_ms"]["p50"] for r in results]
    p90_lat = [r["latency_ms"]["p90"] for r in results]
    p99_lat = [r["latency_ms"]["p99"] for r in results]
    throughput = [r["throughput_rps"] for r in results]

    outdir = Path(output_dir)
    outdir.mkdir(parents=True, exist_ok=True)

    # ── Latency vs cluster size ──
    fig, ax = plt.subplots(figsize=(10, 6))
    ax.plot(sizes, avg_lat, "o-", label="avg", linewidth=2)
    ax.plot(sizes, p50_lat, "s--", label="p50", linewidth=1.5)
    ax.plot(sizes, p90_lat, "^--", label="p90", linewidth=1.5)
    ax.plot(sizes, p99_lat, "D--", label="p99", linewidth=1.5)
    ax.set_xlabel("Cluster Size (nodes)")
    ax.set_ylabel("Latency (ms)")
    ax.set_title("Routing Latency vs. Cluster Size")
    ax.legend()
    ax.grid(True, alpha=0.3)
    fig.tight_layout()
    latency_path = outdir / "routing_latency_scaling.png"
    fig.savefig(latency_path, dpi=150)
    print(f"Saved: {latency_path}")
    plt.close(fig)

    # ── Throughput vs cluster size ──
    fig, ax = plt.subplots(figsize=(10, 6))
    ax.plot(sizes, throughput, "o-", color="green", linewidth=2)
    ax.set_xlabel("Cluster Size (nodes)")
    ax.set_ylabel("Throughput (req/s)")
    ax.set_title("Routing Throughput vs. Cluster Size")
    ax.grid(True, alpha=0.3)
    fig.tight_layout()
    throughput_path = outdir / "routing_throughput_scaling.png"
    fig.savefig(throughput_path, dpi=150)
    print(f"Saved: {throughput_path}")
    plt.close(fig)

    # ── Summary table ──
    table_data = []
    for r in results:
        table_data.append([
            r["cluster_size"],
            r["latency_ms"]["avg"],
            r["latency_ms"]["p50"],
            r["latency_ms"]["p90"],
            r["latency_ms"]["p99"],
            r["throughput_rps"],
            f'{r["successful_requests"]}/{r["total_requests"]}',
        ])

    headers = ["Nodes", "Avg(ms)", "P50(ms)", "P90(ms)", "P99(ms)", "RPS", "Success"]
    table_str = tabfmt(table_data, headers=headers, floatfmt=".2f", tablefmt="github")
    print("\n" + table_str + "\n")

    summary_path = outdir / "routing_summary.txt"
    with open(summary_path, "w") as f:
        f.write(table_str + "\n")
    print(f"Saved: {summary_path}")


# ── CLI ───────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        description="Benchmark OpenTela routing overhead as cluster size grows.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""\
Examples:
  # Run a single benchmark against a live cluster:
  %(prog)s --requests 500 --concurrency 20

  # Generate scaling plots from collected results:
  %(prog)s --plot --results-dir results/20260306_120000
""",
    )

    parser.add_argument(
        "--head-node-url",
        default=DEFAULT_HEAD_NODE,
        help=f"Head node base URL (default: {DEFAULT_HEAD_NODE})",
    )
    parser.add_argument(
        "--requests", "-n",
        type=int,
        default=DEFAULT_REQUESTS,
        help=f"Number of benchmark requests (default: {DEFAULT_REQUESTS})",
    )
    parser.add_argument(
        "--concurrency", "-c",
        type=int,
        default=DEFAULT_CONCURRENCY,
        help=f"Concurrent workers (default: {DEFAULT_CONCURRENCY})",
    )
    parser.add_argument(
        "--warmup",
        type=int,
        default=DEFAULT_WARMUP,
        help=f"Warmup requests (default: {DEFAULT_WARMUP})",
    )
    parser.add_argument(
        "--output", "-o",
        default=None,
        help="Output JSON file path (default: routing_<cluster_size>.json)",
    )
    parser.add_argument(
        "--plot",
        action="store_true",
        help="Plot mode: aggregate results from --results-dir and generate charts",
    )
    parser.add_argument(
        "--results-dir",
        default=".",
        help="Directory containing routing_*.json files (used with --plot)",
    )

    args = parser.parse_args()

    if args.plot:
        results = load_results(args.results_dir)
        if not results:
            print(f"No routing_*.json files found in {args.results_dir}", file=sys.stderr)
            sys.exit(1)
        plot_scaling(results, args.results_dir)
    else:
        result = asyncio.run(
            run_benchmark(
                base_url=args.head_node_url,
                num_requests=args.requests,
                concurrency=args.concurrency,
                warmup=args.warmup,
            )
        )

        if "error" in result:
            sys.exit(1)

        # Determine output path
        out_path = args.output
        if out_path is None:
            csize = result.get("cluster_size", "unknown")
            out_path = f"routing_{csize}.json"

        with open(out_path, "w") as f:
            json.dump(result, f, indent=2)
        print(f"Results saved to: {out_path}")


if __name__ == "__main__":
    main()
