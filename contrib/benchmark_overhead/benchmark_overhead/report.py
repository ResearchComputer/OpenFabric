"""Render plots from a run's raw.jsonl. Idempotent; no side effects on raw data."""

from __future__ import annotations

import json
import logging
from collections import defaultdict
from pathlib import Path
from typing import Iterable

import matplotlib

matplotlib.use("Agg")  # noqa: E402
import matplotlib.pyplot as plt  # noqa: E402
import numpy as np  # noqa: E402

log = logging.getLogger(__name__)


def _read_rows(raw_path: Path) -> list[dict]:
    rows: list[dict] = []
    with raw_path.open() as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            r = json.loads(line)
            if r.get("cell", {}).get("phase") != "measure":
                continue
            if r.get("client", {}).get("status") != 200:
                continue
            rows.append(r)
    return rows


def _group(rows: list[dict], *keys: str) -> dict[tuple, list[dict]]:
    out: dict[tuple, list[dict]] = defaultdict(list)
    for r in rows:
        cell = r["cell"]
        out[tuple(cell[k] for k in keys)].append(r)
    return out


def _plot_overhead_vs_rps(rows: list[dict], out_path: Path) -> None:
    fig, ax = plt.subplots(figsize=(6, 4))
    by_model_path = _group(rows, "model", "rps", "path")
    models = sorted({k[0] for k in by_model_path})
    rps_values = sorted({k[1] for k in by_model_path})
    for model in models:
        direct_p50 = [
            float(np.median([r["client"]["ttft_ms"] for r in by_model_path.get((model, rps, "direct"), []) if r["client"]["ttft_ms"]]))
            if by_model_path.get((model, rps, "direct")) else float("nan")
            for rps in rps_values
        ]
        otela_p50 = [
            float(np.median([r["client"]["ttft_ms"] for r in by_model_path.get((model, rps, "otela"), []) if r["client"]["ttft_ms"]]))
            if by_model_path.get((model, rps, "otela")) else float("nan")
            for rps in rps_values
        ]
        delta = [o - d for o, d in zip(otela_p50, direct_p50)]
        ax.plot(rps_values, delta, marker="o", label=model)
    ax.set_xlabel("Arrival rate (RPS)")
    ax.set_ylabel("p50 TTFT overhead (otela − direct, ms)")
    ax.set_xscale("log")
    ax.legend(fontsize=8)
    ax.grid(True, alpha=0.3)
    fig.tight_layout()
    fig.savefig(out_path)
    plt.close(fig)


def _plot_stage_breakdown(rows: list[dict], out_path: Path) -> None:
    """Stacked bar of mean stage time at the median RPS, per model, otela path only."""
    by = _group([r for r in rows if r["cell"]["path"] == "otela"], "model", "rps")
    rps_values = sorted({k[1] for k in by})
    if not rps_values:
        plt.figure().savefig(out_path)
        plt.close()
        return
    chosen_rps = rps_values[len(rps_values) // 2]
    models = sorted({k[0] for k in by if k[1] == chosen_rps})
    stage_order = [
        "head_recv", "head_dnt", "head_peer_select", "head_p2p_to_worker_first_byte",
        "worker_local_proxy", "worker_sglang_ttft",
    ]
    means = {m: {s: 0.0 for s in stage_order} for m in models}
    for m in models:
        rs = by.get((m, chosen_rps), [])
        for s in stage_order:
            vals = [r["stages_ms"].get(s, 0.0) for r in rs if s in r.get("stages_ms", {})]
            means[m][s] = float(np.mean(vals)) if vals else 0.0

    fig, ax = plt.subplots(figsize=(7, 4))
    x = np.arange(len(models))
    bottom = np.zeros(len(models))
    for s in stage_order:
        heights = np.array([means[m][s] for m in models])
        ax.bar(x, heights, bottom=bottom, label=s)
        bottom += heights
    ax.set_xticks(x)
    ax.set_xticklabels(models, rotation=15, ha="right")
    ax.set_ylabel(f"Mean stage time at RPS={chosen_rps} (ms)")
    ax.legend(fontsize=7, loc="upper left")
    fig.tight_layout()
    fig.savefig(out_path)
    plt.close(fig)


def _plot_ttft_cdf(rows: list[dict], out_path: Path) -> None:
    by = _group(rows, "model", "rps", "path")
    models = sorted({k[0] for k in by})
    n_models = len(models)
    fig, axes = plt.subplots(1, max(1, n_models), figsize=(4 * max(1, n_models), 4), squeeze=False)
    rps_values = sorted({k[1] for k in by})
    chosen_rps = rps_values[len(rps_values) // 2] if rps_values else None
    for i, m in enumerate(models):
        ax = axes[0][i]
        for path in ("direct", "otela"):
            ttfts = sorted(
                r["client"]["ttft_ms"]
                for r in by.get((m, chosen_rps, path), [])
                if r["client"].get("ttft_ms") is not None
            )
            if not ttfts:
                continue
            ys = np.linspace(0, 1, len(ttfts))
            ax.plot(ttfts, ys, label=path)
        ax.set_title(f"{m} @ {chosen_rps} RPS")
        ax.set_xlabel("TTFT (ms)")
        ax.set_ylabel("CDF")
        ax.legend()
        ax.grid(True, alpha=0.3)
    fig.tight_layout()
    fig.savefig(out_path)
    plt.close(fig)


def render_report(run_dir: str) -> None:
    rd = Path(run_dir).expanduser()
    raw = rd / "raw.jsonl"
    if not raw.exists():
        raise FileNotFoundError(raw)
    rows = _read_rows(raw)
    plots = rd / "plots"
    plots.mkdir(exist_ok=True)
    _plot_overhead_vs_rps(rows, plots / "overhead_vs_rps.pdf")
    _plot_stage_breakdown(rows, plots / "stage_breakdown.pdf")
    _plot_ttft_cdf(rows, plots / "ttft_cdf.pdf")
