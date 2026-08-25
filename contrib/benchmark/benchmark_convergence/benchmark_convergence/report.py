"""Text-only summary report for a convergence-speed run."""
from __future__ import annotations

from pathlib import Path

import pandas as pd


def _pct(x: float) -> str:
    return f"{100 * x:.1f}%"


def _ms(ns: float | None) -> str:
    if ns is None or pd.isna(ns):
        return "—"
    return f"{ns / 1e6:.2f}"


def build_report(run_dir: Path) -> None:
    df = pd.read_parquet(run_dir / "results.parquet")
    rows = []
    for N, sub in df.groupby("N", sort=True):
        ok = sub[sub.status == "ok"]
        ns = ok.latency_ns.dropna()
        timeout_rate = (sub.status == "timeout").mean()
        rows.append({
            "N": int(N),
            "n_samples": len(ok),
            "p50_ms": _ms(ns.quantile(0.50)) if len(ns) else "—",
            "p95_ms": _ms(ns.quantile(0.95)) if len(ns) else "—",
            "p99_ms": _ms(ns.quantile(0.99)) if len(ns) else "—",
            "max_ms": _ms(ns.max()) if len(ns) else "—",
            "timeout_rate": _pct(timeout_rate),
        })

    out_dir = run_dir / "report"
    out_dir.mkdir(parents=True, exist_ok=True)
    summary = out_dir / "summary.md"

    lines = ["# Convergence Speed — Summary\n"]
    lines.append(f"Source: `{run_dir}`\n")
    lines.append("\n## Latency vs N\n")
    lines.append("| N | n_samples | p50_ms | p95_ms | p99_ms | max_ms | timeout_rate |")
    lines.append("|---|---|---|---|---|---|---|")
    for r in rows:
        lines.append(
            f"| {r['N']} | {r['n_samples']} | {r['p50_ms']} | "
            f"{r['p95_ms']} | {r['p99_ms']} | {r['max_ms']} | {r['timeout_rate']} |"
        )
    summary.write_text("\n".join(lines) + "\n")
