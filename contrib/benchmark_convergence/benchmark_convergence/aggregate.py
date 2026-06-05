"""Aggregate per-cell results.json files into a tidy parquet table."""
from __future__ import annotations

import json
from pathlib import Path

import pandas as pd


_OK = "ok"
_TIMEOUT = "timeout"


def _host_for_task(nodes: list[dict], task_id: int) -> str:
    for n in nodes:
        if int(n["task_id"]) == task_id:
            return n["host"]
    raise KeyError(f"task_id {task_id} not in nodes manifest")


def aggregate_cell(json_path: Path, output: Path | None = None) -> pd.DataFrame:
    """Aggregate one per-cell results.json into a DataFrame.

    Returns a DataFrame with one row per (write, observer). Self-observations
    (writer == observer) are excluded. If `output` is given, the DataFrame is
    written to parquet at that path.
    """
    raw = json.loads(json_path.read_text())
    nodes = raw["nodes"]
    cell_flags: list[str] = list(raw.get("flags", []))
    offsets_start: dict[str, int] = raw["clock_offsets_ns"]["start"]

    rows: list[dict] = []
    for w in raw["writes"]:
        writer_id = int(w["writer_task_id"])
        writer_host = _host_for_task(nodes, writer_id)
        for obs_id_str, t_seen in w["seen"].items():
            obs_id = int(obs_id_str)
            if obs_id == writer_id:
                continue
            obs_host = _host_for_task(nodes, obs_id)
            o_writer = int(offsets_start[writer_host])
            o_obs = int(offsets_start[obs_host])

            if t_seen == "TIMEOUT":
                status = _TIMEOUT
                t_seen_val: int | None = None
                latency: int | None = None
            else:
                status = _OK
                t_seen_val = int(t_seen)
                latency = t_seen_val - int(w["t_write_ns"]) - (o_obs - o_writer)

            rows.append({
                "run_id": raw["run_id"],
                "N": int(raw["N"]),
                "rep": int(raw["rep"]),
                "seq": int(w["seq"]),
                "writer_task_id": writer_id,
                "observer_task_id": obs_id,
                "writer_host": writer_host,
                "observer_host": obs_host,
                "t_write_ns": int(w["t_write_ns"]),
                "t_seen_ns": t_seen_val,
                "clock_offset_writer_ns": o_writer,
                "clock_offset_observer_ns": o_obs,
                "latency_ns": latency,
                "status": status,
                "flags": cell_flags,
            })

    df = pd.DataFrame(rows)
    if output is not None:
        output.parent.mkdir(parents=True, exist_ok=True)
        df.to_parquet(output, index=False)
    return df


def aggregate_run(run_dir: Path) -> pd.DataFrame:
    """Walk a run dir for N=*/rep=*/results.json and concatenate them.

    Writes the combined frame to <run_dir>/results.parquet.
    """
    frames: list[pd.DataFrame] = []
    for results_json in sorted(run_dir.glob("N=*/rep=*/results.json")):
        frames.append(aggregate_cell(results_json))
    if not frames:
        raise FileNotFoundError(f"no results.json under {run_dir}")
    df = pd.concat(frames, ignore_index=True)
    out = run_dir / "results.parquet"
    df.to_parquet(out, index=False)
    return df
