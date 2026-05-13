"""Sweep orchestrator: warmup -> measure -> drain per (model, rps, path, rep)."""

from __future__ import annotations

import asyncio
import csv
import datetime
import json
import logging
from pathlib import Path
from typing import Any

import numpy as np
import yaml

from benchmark_overhead.client import fire_open_loop
from benchmark_overhead.deploy import WorkerState, ensure_worker
from benchmark_overhead.workload import fixed_prompt, load_sharegpt, poisson_schedule

log = logging.getLogger(__name__)


def summarize_cell(rows: list[dict], *, cell_meta: dict) -> dict:
    n_total = len(rows)
    ok = [r for r in rows if r["client"]["status"] == 200]
    n_ok = len(ok)

    def pct(values: list[float], q: float) -> float | None:
        if not values:
            return None
        return float(np.percentile(values, q))

    ttfts = [r["client"]["ttft_ms"] for r in ok if r["client"]["ttft_ms"] is not None]
    totals = [r["client"]["total_ms"] for r in ok]

    stage_keys: set[str] = set()
    for r in ok:
        stage_keys.update(r.get("stages_ms", {}).keys())
    stage_means: dict[str, float] = {}
    for k in stage_keys:
        vals = [r["stages_ms"].get(k, 0.0) for r in ok if k in r.get("stages_ms", {})]
        if vals:
            stage_means[f"{k}_mean_ms"] = float(np.mean(vals))

    return {
        **cell_meta,
        "n_total": n_total,
        "n_ok": n_ok,
        "error_rate": (n_total - n_ok) / n_total if n_total else 0.0,
        "ttft_p50_ms": pct(ttfts, 50),
        "ttft_p95_ms": pct(ttfts, 95),
        "ttft_p99_ms": pct(ttfts, 99),
        "total_p50_ms": pct(totals, 50),
        "total_p95_ms": pct(totals, 95),
        "total_p99_ms": pct(totals, 99),
        **stage_means,
    }


def run_sweep(*, config_path: str, output_dir: str) -> None:
    import os
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    cfg = yaml.safe_load(Path(config_path).expanduser().read_text())
    run_id = os.environ.get("OTELA_BENCH_RUN_ID") or (
        "r-" + datetime.datetime.now().strftime("%Y%m%dT%H%M%S")
    )
    out_root = Path(output_dir).expanduser() / run_id
    out_root.mkdir(parents=True, exist_ok=True)
    (out_root / "config.yaml").write_text(yaml.safe_dump(cfg))

    raw_path = out_root / "raw.jsonl"
    cells_path = out_root / "cells.csv"
    meta_path = out_root / "meta.json"

    meta = {
        "run_id": run_id,
        "start": datetime.datetime.utcnow().isoformat() + "Z",
        "end": None,
        "head_url": cfg["head_url"],
        "git_commit": _git_commit(),
        "workers": [],
    }
    meta_path.write_text(json.dumps(meta, indent=2))

    state_dir = Path(cfg["slurm"]["bench_state_dir"]).expanduser()
    workload = cfg.get("workload") or {}
    if workload.get("kind") == "fixed":
        prompts = fixed_prompt(
            input_tokens=int(workload.get("input_tokens", 256)),
            output_tokens=int(workload.get("output_tokens", 128)),
        )
        log.info("using fixed workload: input=%d tokens, output=%d tokens",
                 workload.get("input_tokens", 256), workload.get("output_tokens", 128))
    else:
        prompts = load_sharegpt(
            cfg["sharegpt_path"], max_input_tokens=512, max_output_tokens=128
        )
        if not prompts:
            raise RuntimeError(f"no prompts loaded from {cfg['sharegpt_path']}")
    log.info("loaded %d prompts", len(prompts))

    cells_rows: list[dict] = []
    import subprocess
    # Resume support: cells already present in raw.jsonl are skipped so a
    # crashed sweep can be relaunched with the same OTELA_BENCH_RUN_ID.
    done_keys = _existing_cell_keys(raw_path)
    if done_keys:
        log.info("resume: %d cells already measured in %s", len(done_keys), raw_path)
    for i, m in enumerate(cfg["models"]):
        all_cell_keys = [
            f"{m['name'].replace('/', '_')}-rps{rps}-{path}-rep{rep}"
            for rps in cfg["arrival_rates_rps"]
            for path in cfg["paths"]
            for rep in range(1, int(cfg["repetitions"]) + 1)
        ]
        if all(k in done_keys for k in all_cell_keys):
            log.info("skip model %s — all %d cells already measured", m["name"], len(all_cell_keys))
            continue
        state = ensure_worker(
            model=m["name"],
            model_cfg=m,
            run_id=run_id,
            cfg=cfg,
            state_dir=state_dir,
        )
        meta["workers"].append(state.__dict__)
        meta_path.write_text(json.dumps(meta, indent=2))
        for rps in cfg["arrival_rates_rps"]:
            for path in cfg["paths"]:
                url = _url_for_path(path, head_url=cfg["head_url"], state=state)
                for rep in range(1, int(cfg["repetitions"]) + 1):
                    cell_key = f"{m['name'].replace('/', '_')}-rps{rps}-{path}-rep{rep}"
                    cell_meta = {
                        "model": m["name"],
                        "rps": rps,
                        "path": path,
                        "rep": rep,
                        "cell_key": cell_key,
                    }
                    if cell_key in done_keys:
                        log.info("skip cell (already measured): %s", cell_key)
                        cell_rows = _read_cell_rows(raw_path, cell_key)
                        cells_rows.append(summarize_cell(cell_rows, cell_meta=cell_meta))
                        continue
                    log.info("cell: %s", cell_key)
                    rng = np.random.default_rng(hash(cell_key) & 0xFFFFFFFF)
                    asyncio.run(_run_cell(
                        url=url, rps=rps, prompts=prompts, model=m["name"],
                        warmup_s=cfg["warmup_s"], duration_s=cfg["duration_s"],
                        cell_meta=cell_meta, raw_path=raw_path, rng=rng,
                    ))
                    cell_rows = _read_cell_rows(raw_path, cell_key)
                    cells_rows.append(summarize_cell(cell_rows, cell_meta=cell_meta))
        # Free GPU before the next model. Required when QOS caps jobs/user
        # (e.g. debug-qos: MaxJobsPerUser=1) — without this, the next
        # ensure_worker's sbatch sits PENDING forever.
        if i < len(cfg["models"]) - 1:
            log.info("scancel worker %s for %s before next model", state.job_id, m["name"])
            subprocess.run(["scancel", state.job_id], check=False)

    _write_cells_csv(cells_path, cells_rows)
    meta["end"] = datetime.datetime.utcnow().isoformat() + "Z"
    meta_path.write_text(json.dumps(meta, indent=2))
    print(f"wrote {raw_path}, {cells_path}, and {meta_path}")


def _git_commit() -> str:
    import subprocess
    try:
        return subprocess.check_output(
            ["git", "rev-parse", "HEAD"], stderr=subprocess.DEVNULL, text=True
        ).strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return "unknown"


async def _run_cell(*, url: str, rps: int, prompts: list[dict], model: str,
                     warmup_s: int, duration_s: int,
                     cell_meta: dict, raw_path: Path, rng) -> None:
    # Warmup: same arrivals, but write to /dev/null sink.
    warmup_path = raw_path.parent / ".warmup.jsonl"
    warmup_arrivals = poisson_schedule(rps=float(rps), duration_s=float(warmup_s), rng=rng)
    await fire_open_loop(
        url=url, arrivals_s=warmup_arrivals, prompts=prompts, model=model,
        output_path=warmup_path, cell_meta={**cell_meta, "phase": "warmup"}, rng=rng,
    )
    warmup_path.unlink(missing_ok=True)

    # Measure
    arrivals = poisson_schedule(rps=float(rps), duration_s=float(duration_s), rng=rng)
    await fire_open_loop(
        url=url, arrivals_s=arrivals, prompts=prompts, model=model,
        output_path=raw_path, cell_meta={**cell_meta, "phase": "measure"}, rng=rng,
    )


def _url_for_path(path: str, *, head_url: str, state: WorkerState) -> str:
    if path == "otela":
        return f"{head_url.rstrip('/')}/v1/service/llm/v1/chat/completions"
    if path == "direct":
        return f"http://{state.node}:{state.sglang_port}/v1/chat/completions"
    raise ValueError(f"unknown path {path!r}")


def _existing_cell_keys(raw_path: Path) -> set[str]:
    """Scan raw.jsonl once and return the set of cell_keys that already have
    at least one measure row. Used for resume after a crashed sweep."""
    keys: set[str] = set()
    if not raw_path.exists():
        return keys
    with raw_path.open() as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                r = json.loads(line)
            except json.JSONDecodeError:
                continue
            cell = r.get("cell", {})
            if cell.get("phase") == "measure" and cell.get("cell_key"):
                keys.add(cell["cell_key"])
    return keys


def _read_cell_rows(raw_path: Path, cell_key: str) -> list[dict]:
    """Read measure rows for one cell. Tolerates malformed JSONL lines
    (concurrent async appends at high RPS can interleave bytes from
    different rows; skip those instead of crashing the whole sweep)."""
    rows: list[dict] = []
    bad = 0
    with raw_path.open() as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                r = json.loads(line)
            except json.JSONDecodeError:
                bad += 1
                continue
            if r.get("cell", {}).get("cell_key") == cell_key and r.get("cell", {}).get("phase") == "measure":
                rows.append(r)
    if bad:
        log.warning("skipped %d malformed jsonl lines in %s", bad, raw_path)
    return rows


def _write_cells_csv(path: Path, rows: list[dict]) -> None:
    if not rows:
        return
    keys: set[str] = set()
    for r in rows:
        keys.update(r.keys())
    fieldnames = sorted(keys)
    with path.open("w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=fieldnames)
        w.writeheader()
        for r in rows:
            w.writerow(r)
