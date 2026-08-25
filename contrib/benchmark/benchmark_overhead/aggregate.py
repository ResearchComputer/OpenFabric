"""Standalone aggregator: read raw.jsonl, write summary CSV with p50/p95/p99
+ stddev across reps (outliers removed via Tukey 1.5×IQR), and an
overhead-by-load table comparing otela vs direct.

Usage: python aggregate.py <run_dir>
"""

from __future__ import annotations

import csv
import json
import sys
from collections import defaultdict
from pathlib import Path

import numpy as np


def parse_raw(path: Path) -> list[dict]:
    rows: list[dict] = []
    bad = 0
    with path.open() as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                rows.append(json.loads(line))
            except json.JSONDecodeError:
                bad += 1
    if bad:
        print(f"warn: skipped {bad} malformed lines in {path}", file=sys.stderr)
    return rows


def by_cell(rows: list[dict]) -> dict[tuple, list[dict]]:
    out: dict[tuple, list[dict]] = defaultdict(list)
    for r in rows:
        c = r.get("cell") or {}
        if c.get("phase") != "measure":
            continue
        key = (c.get("model"), c.get("rps"), c.get("path"), c.get("rep"))
        if None in key:
            continue
        out[key].append(r)
    return out


def remove_outliers_iqr(values: list[float]) -> list[float]:
    if len(values) < 4:
        return values
    a = np.asarray(values, dtype=float)
    q1, q3 = np.percentile(a, [25, 75])
    iqr = q3 - q1
    lo, hi = q1 - 1.5 * iqr, q3 + 1.5 * iqr
    return a[(a >= lo) & (a <= hi)].tolist()


def cell_metrics(rows: list[dict]) -> dict:
    ok = [r for r in rows if (r.get("client") or {}).get("status") == 200]
    totals = [
        r["client"]["total_ms"] for r in ok if r["client"].get("total_ms") is not None
    ]
    ttfts = [
        r["client"]["ttft_ms"] for r in ok if r["client"].get("ttft_ms") is not None
    ]
    totals_clean = remove_outliers_iqr(totals)
    ttfts_clean = remove_outliers_iqr(ttfts)
    # Idle client<->origin RTT (probed once per cell pre-workload; identical
    # across every request row in the cell). Take it from the first row.
    cell = (rows[0].get("cell") if rows else None) or {}
    return {
        "n_total": len(rows),
        "n_ok": len(ok),
        "error_rate": (len(rows) - len(ok)) / len(rows) if rows else 0.0,
        "total_p50_ms": (
            float(np.percentile(totals_clean, 50)) if totals_clean else None
        ),
        "total_p95_ms": (
            float(np.percentile(totals_clean, 95)) if totals_clean else None
        ),
        "total_p99_ms": (
            float(np.percentile(totals_clean, 99)) if totals_clean else None
        ),
        "total_mean_ms": float(np.mean(totals_clean)) if totals_clean else None,
        "total_std_ms": float(np.std(totals_clean)) if totals_clean else None,
        "ttft_p50_ms": float(np.percentile(ttfts_clean, 50)) if ttfts_clean else None,
        "ttft_p95_ms": float(np.percentile(ttfts_clean, 95)) if ttfts_clean else None,
        "ttft_p99_ms": float(np.percentile(ttfts_clean, 99)) if ttfts_clean else None,
        "client_net_p50_ms": cell.get("client_net_p50_ms"),
        "client_net_std_ms": cell.get("client_net_std_ms"),
    }


def main(run_dir: str) -> None:
    run_path = Path(run_dir)
    raw = parse_raw(run_path / "raw.jsonl")
    cells = by_cell(raw)

    # Per-cell rows
    per_cell_path = run_path / "cells_summary.csv"
    with per_cell_path.open("w", newline="") as f:
        w = csv.writer(f)
        w.writerow(
            [
                "model",
                "rps",
                "path",
                "rep",
                "n_total",
                "n_ok",
                "error_rate",
                "total_p50_ms",
                "total_p95_ms",
                "total_p99_ms",
                "total_mean_ms",
                "total_std_ms",
                "ttft_p50_ms",
                "ttft_p95_ms",
                "ttft_p99_ms",
            ]
        )
        for (model, rps, path, rep), rows in sorted(cells.items()):
            m = cell_metrics(rows)
            w.writerow(
                [
                    model,
                    rps,
                    path,
                    rep,
                    m["n_total"],
                    m["n_ok"],
                    f"{m['error_rate']:.4f}",
                    _fmt(m["total_p50_ms"]),
                    _fmt(m["total_p95_ms"]),
                    _fmt(m["total_p99_ms"]),
                    _fmt(m["total_mean_ms"]),
                    _fmt(m["total_std_ms"]),
                    _fmt(m["ttft_p50_ms"]),
                    _fmt(m["ttft_p95_ms"]),
                    _fmt(m["ttft_p99_ms"]),
                ]
            )
    print(f"wrote {per_cell_path}")

    # Aggregated across reps: median over reps for each (model,rps,path)
    agg: dict[tuple, dict] = defaultdict(lambda: defaultdict(list))
    for (model, rps, path, rep), rows in cells.items():
        m = cell_metrics(rows)
        for k in (
            "total_p50_ms",
            "total_p95_ms",
            "total_p99_ms",
            "ttft_p50_ms",
            "client_net_p50_ms",
        ):
            v = m[k]
            if v is not None:
                agg[(model, rps, path)][k].append(v)
        agg[(model, rps, path)]["n_total"].append(m["n_total"])
        agg[(model, rps, path)]["n_ok"].append(m["n_ok"])

    summary_path = run_path / "summary.csv"
    with summary_path.open("w", newline="") as f:
        w = csv.writer(f)
        w.writerow(
            [
                "model",
                "rps",
                "path",
                "reps",
                "n_total_sum",
                "n_ok_sum",
                "error_rate",
                "p50_median_ms",
                "p50_std_ms",
                "p95_median_ms",
                "p95_std_ms",
                "p99_median_ms",
                "p99_std_ms",
                "ttft_p50_median_ms",
                "client_net_p50_median_ms",
                "client_net_p50_std_ms",
            ]
        )
        for (model, rps, path), d in sorted(agg.items()):
            n_total = int(sum(d["n_total"]))
            n_ok = int(sum(d["n_ok"]))
            err = (n_total - n_ok) / n_total if n_total else 0.0
            w.writerow(
                [
                    model,
                    rps,
                    path,
                    len(d["total_p50_ms"]),
                    n_total,
                    n_ok,
                    f"{err:.4f}",
                    _stat_median(d["total_p50_ms"]),
                    _stat_std(d["total_p50_ms"]),
                    _stat_median(d["total_p95_ms"]),
                    _stat_std(d["total_p95_ms"]),
                    _stat_median(d["total_p99_ms"]),
                    _stat_std(d["total_p99_ms"]),
                    _stat_median(d["ttft_p50_ms"]),
                    _stat_median(d["client_net_p50_ms"]),
                    _stat_std(d["client_net_p50_ms"]),
                ]
            )
    print(f"wrote {summary_path}")

    # Overhead table: paired (rps, model) → otela − direct
    overhead_path = run_path / "overhead.csv"
    with overhead_path.open("w", newline="") as f:
        w = csv.writer(f)
        w.writerow(
            [
                "model",
                "rps",
                "direct_p50_ms",
                "otela_p50_ms",
                "overhead_p50_ms",
                "direct_p95_ms",
                "otela_p95_ms",
                "overhead_p95_ms",
                "direct_p99_ms",
                "otela_p99_ms",
                "overhead_p99_ms",
                "direct_error_rate",
                "otela_error_rate",
            ]
        )
        models = sorted({m for (m, _, _) in agg})
        rpss = sorted({r for (_, r, _) in agg})
        for model in models:
            for rps in rpss:
                d = agg.get((model, rps, "direct"))
                o = agg.get((model, rps, "otela"))
                if not d or not o:
                    continue
                d_p50 = (
                    float(np.median(d["total_p50_ms"])) if d["total_p50_ms"] else None
                )
                o_p50 = (
                    float(np.median(o["total_p50_ms"])) if o["total_p50_ms"] else None
                )
                d_p95 = (
                    float(np.median(d["total_p95_ms"])) if d["total_p95_ms"] else None
                )
                o_p95 = (
                    float(np.median(o["total_p95_ms"])) if o["total_p95_ms"] else None
                )
                d_p99 = (
                    float(np.median(d["total_p99_ms"])) if d["total_p99_ms"] else None
                )
                o_p99 = (
                    float(np.median(o["total_p99_ms"])) if o["total_p99_ms"] else None
                )
                d_err = (
                    (sum(d["n_total"]) - sum(d["n_ok"])) / sum(d["n_total"])
                    if sum(d["n_total"])
                    else 0
                )
                o_err = (
                    (sum(o["n_total"]) - sum(o["n_ok"])) / sum(o["n_total"])
                    if sum(o["n_total"])
                    else 0
                )
                w.writerow(
                    [
                        model,
                        rps,
                        _fmt(d_p50),
                        _fmt(o_p50),
                        _fmt(_sub(o_p50, d_p50)),
                        _fmt(d_p95),
                        _fmt(o_p95),
                        _fmt(_sub(o_p95, d_p95)),
                        _fmt(d_p99),
                        _fmt(o_p99),
                        _fmt(_sub(o_p99, d_p99)),
                        f"{d_err:.4f}",
                        f"{o_err:.4f}",
                    ]
                )
    print(f"wrote {overhead_path}")


def _fmt(v):
    return "" if v is None else f"{v:.2f}"


def _sub(a, b):
    if a is None or b is None:
        return None
    return a - b


def _stat_median(values: list[float]) -> str:
    return _fmt(float(np.median(values))) if values else ""


def _stat_std(values: list[float]) -> str:
    return _fmt(float(np.std(values))) if values else ""


if __name__ == "__main__":
    main(sys.argv[1])
