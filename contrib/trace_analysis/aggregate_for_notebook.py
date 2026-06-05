"""Aggregate one bench_overhead run into the per-stage summary CSV that
overhead.ipynb expects, augmented with the new client_net probe columns
and per-rep stats for confidence-interval plotting.

Usage:
    python aggregate_for_notebook.py <run_dir> <out_csv>

For each (model, rps, path) cell, emits one row containing:
- pooled p50/std across all reps (original columns: `<stage>_p50_ms`,
  `<stage>_std_ms`) — for the legacy decomposition path
- per-rep p50 mean and std across the N reps (new columns:
  `<stage>_p50_reps_mean_ms`, `<stage>_p50_reps_std_ms`, `n_reps`) — feed
  these to a 95%% CI as `mean +/- 1.96 * std / sqrt(n_reps)`
- client_net probe stats (one value per rep, median + std across reps)

The per-rep p50s come from Tukey 1.5xIQR outlier-removed samples
per-rep, so a single noisy rep doesn't pollute the others' values."""

from __future__ import annotations

import csv
import json
import sys
from collections import defaultdict
from pathlib import Path

import numpy as np

STAGES = [
    "head_recv",
    "head_dnt",
    "head_peer_select",
    "head_p2p_to_worker_first_byte",
    "head_first_byte_sent",
    "worker_local_proxy",
    "worker_sglang_ttft",
]

# Per-rep TTFT percentiles emitted for distribution analysis.
TTFT_PCTS = [25, 50, 75, 90, 95, 99]


def _iqr_inliers(values: list[float]) -> np.ndarray:
    if len(values) < 4:
        return np.asarray(values, dtype=float)
    a = np.asarray(values, dtype=float)
    q1, q3 = np.percentile(a, [25, 75])
    iqr = q3 - q1
    lo, hi = q1 - 1.5 * iqr, q3 + 1.5 * iqr
    return a[(a >= lo) & (a <= hi)]


def _p50_std(values: list[float]) -> tuple[float, float, int]:
    inliers = _iqr_inliers(values)
    if not len(inliers):
        return float("nan"), float("nan"), 0
    return (
        float(np.percentile(inliers, 50)),
        float(np.std(inliers, ddof=0)),
        int(len(inliers)),
    )


def _mean_std(values: list[float]) -> tuple[float, float]:
    if not values:
        return float("nan"), float("nan")
    a = np.asarray(values, dtype=float)
    return float(np.mean(a)), float(np.std(a, ddof=0))


def _inter_arrivals_post_ttft(rec_client: dict) -> list[float]:
    """Return per-chunk inter-arrival times (ms) for one request, starting
    AFTER TTFT (so the gap from request-send to first chunk is excluded).
    Multi-token chunks contribute the same inter-arrival once; the count of
    tokens-per-chunk is exposed separately as a coalescing signal."""
    ts = rec_client.get("chunk_ts_ms") or []
    if len(ts) < 2:
        return []
    return [ts[i] - ts[i - 1] for i in range(1, len(ts))]


def main(
    run_dir: str,
    out_csv: str,
    drop_reps: set[int] | None = None,
    interarrival_out: str | None = None,
) -> None:
    drop_reps = drop_reps or set()
    # Group raw rows by (model, rps, path, rep) so we can compute per-rep p50s
    # independently before pooling.
    by_cell_rep: dict[tuple, list[dict]] = defaultdict(list)
    with open(Path(run_dir) / "raw.jsonl") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                r = json.loads(line)
            except json.JSONDecodeError:
                continue
            c = r.get("cell") or {}
            if c.get("phase") != "measure":
                continue
            if (r.get("client") or {}).get("status") != 200:
                continue
            rep = int(c["rep"])
            if rep in drop_reps:
                continue
            by_cell_rep[(c["model"], int(c["rps"]), c["path"], rep)].append(r)

    # Re-pivot: (model, rps, path) -> rep -> rows
    by_cell: dict[tuple, dict[int, list[dict]]] = defaultdict(dict)
    for (model, rps, path, rep), rs in by_cell_rep.items():
        by_cell[(model, rps, path)][rep] = rs

    fieldnames = [
        "model",
        "rps",
        "path",
        "n_total",
        "n_inlier",
        "n_reps",
        "ttft_p50_ms",
        "ttft_std_ms",
        "ttft_p50_reps_mean_ms",
        "ttft_p50_reps_std_ms",
        "total_p50_ms",
        "total_std_ms",
        "total_p50_reps_mean_ms",
        "total_p50_reps_std_ms",
        "tpot_p50_ms",
        "tpot_std_ms",
        "tpot_p50_reps_mean_ms",
        "tpot_p50_reps_std_ms",
        "throughput_tps_reps_mean",
        "throughput_tps_reps_std",
        # Per-chunk inter-arrival stats (ms). Pooled across all requests
        # and reps; reflects the post-TTFT delivery pattern at the client.
        "iat_p50_ms",
        "iat_p95_ms",
        "iat_p99_ms",
        "iat_mean_ms",
        "iat_std_ms",
        "iat_n_samples",
        "iat_avg_tokens_per_chunk",
        # Per-rep TTFT percentiles: each cell's p_NN, averaged across reps.
        # Used to compute per-percentile overhead (Δp_NN) with honest SEM.
    ]
    for pct in TTFT_PCTS:
        fieldnames += [f"ttft_p{pct}_reps_mean_ms", f"ttft_p{pct}_reps_std_ms"]
    fieldnames += [
        "otela_only_p50_ms",
        "otela_only_std_ms",
    ]
    for s in STAGES:
        fieldnames += [
            f"{s}_p50_ms",
            f"{s}_std_ms",
            f"{s}_p50_reps_mean_ms",
            f"{s}_p50_reps_std_ms",
        ]
    fieldnames += ["client_net_p50_median_ms", "client_net_p50_std_ms"]

    out = []
    for (model, rps, path), rep_to_rows in sorted(by_cell.items()):
        all_rows = [r for rs in rep_to_rows.values() for r in rs]
        n_total = len(all_rows)

        ttfts_all = [
            r["client"]["ttft_ms"]
            for r in all_rows
            if r["client"].get("ttft_ms") is not None
        ]
        totals_all = [
            r["client"]["total_ms"]
            for r in all_rows
            if r["client"].get("total_ms") is not None
        ]
        ttft_p50, ttft_std, n_in = _p50_std(ttfts_all)
        total_p50, total_std, _ = _p50_std(totals_all)

        # Per-request TPOT = (total - ttft) / (output_tokens - 1).
        # Requires both timestamps + at least 2 output tokens.
        tpots_all: list[float] = []
        for r in all_rows:
            cli = r["client"]
            t_ttft = cli.get("ttft_ms")
            t_total = cli.get("total_ms")
            n_tok = cli.get("output_tokens")
            if t_ttft is None or t_total is None or n_tok is None or n_tok < 2:
                continue
            tpots_all.append((t_total - t_ttft) / (n_tok - 1))
        tpot_p50, tpot_std, _ = _p50_std(tpots_all)

        stage_vals_all = {s: [] for s in STAGES}
        for r in all_rows:
            sm = r.get("stages_ms") or {}
            for s in STAGES:
                if s in sm:
                    stage_vals_all[s].append(float(sm[s]))
        stage_stats_pooled = {s: _p50_std(stage_vals_all[s]) for s in STAGES}

        # Pooled per-chunk inter-arrival times (post-TTFT) and tokens-per-chunk.
        iats: list[float] = []
        toks_per_chunk: list[int] = []
        for r in all_rows:
            iats.extend(_inter_arrivals_post_ttft(r["client"]))
            counts = (r["client"] or {}).get("chunk_token_counts") or []
            toks_per_chunk.extend(int(x) for x in counts if x)
        if iats:
            ia = np.asarray(iats, dtype=float)
            iat_p50 = float(np.percentile(ia, 50))
            iat_p95 = float(np.percentile(ia, 95))
            iat_p99 = float(np.percentile(ia, 99))
            iat_mean = float(np.mean(ia))
            iat_std = float(np.std(ia, ddof=0))
            iat_n = int(ia.size)
        else:
            iat_p50 = iat_p95 = iat_p99 = iat_mean = iat_std = float("nan")
            iat_n = 0
        iat_tpc = float(np.mean(toks_per_chunk)) if toks_per_chunk else float("nan")

        if stage_vals_all["worker_sglang_ttft"]:
            paired = [
                t - s
                for t, s in zip(ttfts_all, stage_vals_all["worker_sglang_ttft"])
                if t is not None and s is not None
            ]
            otela_only_p50, otela_only_std, _ = _p50_std(paired)
        else:
            otela_only_p50, otela_only_std = float("nan"), float("nan")

        # Per-rep p50s: one number per rep, then mean+std across reps.
        rep_p50s: dict[str, list[float]] = defaultdict(list)
        for rep in sorted(rep_to_rows):
            rs = rep_to_rows[rep]
            rep_ttfts = [
                r["client"]["ttft_ms"]
                for r in rs
                if r["client"].get("ttft_ms") is not None
            ]
            rep_totals = [
                r["client"]["total_ms"]
                for r in rs
                if r["client"].get("total_ms") is not None
            ]
            if rep_ttfts:
                rep_arr = np.asarray(rep_ttfts, dtype=float)
                for pct in TTFT_PCTS:
                    rep_p50s[f"ttft_p{pct}"].append(float(np.percentile(rep_arr, pct)))
            rep_tpots: list[float] = []
            rep_throughputs: list[float] = []
            for r in rs:
                cli = r["client"]
                t_ttft = cli.get("ttft_ms")
                t_total = cli.get("total_ms")
                n_tok = cli.get("output_tokens")
                if t_ttft is None or t_total is None or n_tok is None or n_tok < 2:
                    continue
                rep_tpots.append((t_total - t_ttft) / (n_tok - 1))
                if t_total > 0:
                    rep_throughputs.append(n_tok * 1000.0 / t_total)
            p, _, _ = _p50_std(rep_ttfts)
            if not np.isnan(p):
                rep_p50s["ttft"].append(p)
            p, _, _ = _p50_std(rep_totals)
            if not np.isnan(p):
                rep_p50s["total"].append(p)
            p, _, _ = _p50_std(rep_tpots)
            if not np.isnan(p):
                rep_p50s["tpot"].append(p)
            # Per-rep throughput is the mean across the rep's requests
            # (tokens/sec); aggregating across reps gives the cross-rep CI.
            if rep_throughputs:
                rep_p50s["throughput"].append(float(np.mean(rep_throughputs)))
            for s in STAGES:
                vals = [
                    float(r["stages_ms"][s])
                    for r in rs
                    if (r.get("stages_ms") or {}).get(s) is not None
                ]
                p, _, _ = _p50_std(vals)
                if not np.isnan(p):
                    rep_p50s[s].append(p)

        n_reps = len(rep_to_rows)
        ttft_reps_mean, ttft_reps_std = _mean_std(rep_p50s["ttft"])
        total_reps_mean, total_reps_std = _mean_std(rep_p50s["total"])
        tpot_reps_mean, tpot_reps_std = _mean_std(rep_p50s["tpot"])
        thr_reps_mean, thr_reps_std = _mean_std(rep_p50s["throughput"])
        ttft_pct_stats = {pct: _mean_std(rep_p50s[f"ttft_p{pct}"]) for pct in TTFT_PCTS}

        # client_net: one value per rep, taken from cell_meta (identical
        # across all rows within a rep). Aggregate across the N reps.
        per_rep_p50: dict[int, float] = {}
        for r in all_rows:
            c = r["cell"]
            rep = int(c["rep"])
            v = c.get("client_net_p50_ms")
            if v is not None and not (isinstance(v, float) and v != v):
                per_rep_p50.setdefault(rep, float(v))
        cn_vals = list(per_rep_p50.values())
        if cn_vals:
            cn_med = float(np.median(cn_vals))
            cn_std = float(np.std(cn_vals, ddof=0))
        else:
            cn_med, cn_std = float("nan"), float("nan")

        row = {
            "model": model,
            "rps": rps,
            "path": path,
            "n_total": n_total,
            "n_inlier": n_in,
            "n_reps": n_reps,
            "ttft_p50_ms": ttft_p50,
            "ttft_std_ms": ttft_std,
            "ttft_p50_reps_mean_ms": ttft_reps_mean,
            "ttft_p50_reps_std_ms": ttft_reps_std,
            "total_p50_ms": total_p50,
            "total_std_ms": total_std,
            "total_p50_reps_mean_ms": total_reps_mean,
            "total_p50_reps_std_ms": total_reps_std,
            "tpot_p50_ms": tpot_p50,
            "tpot_std_ms": tpot_std,
            "tpot_p50_reps_mean_ms": tpot_reps_mean,
            "tpot_p50_reps_std_ms": tpot_reps_std,
            "throughput_tps_reps_mean": thr_reps_mean,
            "throughput_tps_reps_std": thr_reps_std,
            "iat_p50_ms": iat_p50,
            "iat_p95_ms": iat_p95,
            "iat_p99_ms": iat_p99,
            "iat_mean_ms": iat_mean,
            "iat_std_ms": iat_std,
            "iat_n_samples": iat_n,
            "iat_avg_tokens_per_chunk": iat_tpc,
            **{
                f"ttft_p{pct}_reps_mean_ms": ttft_pct_stats[pct][0] for pct in TTFT_PCTS
            },
            **{f"ttft_p{pct}_reps_std_ms": ttft_pct_stats[pct][1] for pct in TTFT_PCTS},
            "otela_only_p50_ms": otela_only_p50,
            "otela_only_std_ms": otela_only_std,
            "client_net_p50_median_ms": cn_med,
            "client_net_p50_std_ms": cn_std,
        }
        for s in STAGES:
            p50, std, _ = stage_stats_pooled[s]
            rmean, rstd = _mean_std(rep_p50s[s])
            row[f"{s}_p50_ms"] = p50
            row[f"{s}_std_ms"] = std
            row[f"{s}_p50_reps_mean_ms"] = rmean
            row[f"{s}_p50_reps_std_ms"] = rstd
        out.append(row)

    with open(out_csv, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=fieldnames)
        w.writeheader()
        for r in out:
            w.writerow(
                {
                    k: (
                        ""
                        if (isinstance(v, float) and v != v)
                        else f"{v:.4f}" if isinstance(v, float) else v
                    )
                    for k, v in r.items()
                }
            )
    print(f"wrote {out_csv} ({len(out)} rows, {n_reps} reps each)")

    # Optional companion file: one row per inter-arrival sample. Used by the
    # notebook to render per-(rps,path) CDF/ECDF plots.
    if interarrival_out:
        with open(interarrival_out, "w", newline="") as f:
            w = csv.writer(f)
            w.writerow(["rps", "path", "rep", "iat_ms", "tokens_in_chunk"])
            for (model, rps, path, rep), rs in sorted(by_cell_rep.items()):
                for r in rs:
                    cli = r["client"] or {}
                    ts = cli.get("chunk_ts_ms") or []
                    counts = cli.get("chunk_token_counts") or []
                    for i in range(1, len(ts)):
                        cnt = counts[i] if i < len(counts) else ""
                        w.writerow([rps, path, rep, f"{ts[i] - ts[i-1]:.3f}", cnt])
        print(f"wrote {interarrival_out}")


if __name__ == "__main__":
    import argparse

    p = argparse.ArgumentParser()
    p.add_argument("run_dir")
    p.add_argument("out_csv")
    p.add_argument(
        "--drop-reps",
        default="",
        help="comma-separated rep numbers to skip (e.g. '1' to drop warmup-shoulder rep)",
    )
    p.add_argument(
        "--inter-arrivals-out",
        default="",
        help="optional path to write the per-chunk inter-arrival samples CSV (for CDF plots)",
    )
    a = p.parse_args()
    drop = {int(s) for s in a.drop_reps.split(",") if s.strip()}
    main(
        a.run_dir,
        a.out_csv,
        drop_reps=drop,
        interarrival_out=a.inter_arrivals_out or None,
    )
