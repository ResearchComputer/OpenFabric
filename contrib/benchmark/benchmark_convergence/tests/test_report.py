from pathlib import Path

import pandas as pd

from benchmark_convergence.report import build_report


def _mk_df() -> pd.DataFrame:
    return pd.DataFrame([
        {"N": 8, "latency_ns": 1_000_000, "status": "ok",      "flags": []},
        {"N": 8, "latency_ns": 2_000_000, "status": "ok",      "flags": []},
        {"N": 8, "latency_ns": None,      "status": "timeout", "flags": []},
        {"N": 16, "latency_ns": 5_000_000, "status": "ok",     "flags": []},
        {"N": 16, "latency_ns": 6_000_000, "status": "ok",     "flags": []},
    ])


def test_report_writes_summary_md(tmp_path: Path) -> None:
    run_dir = tmp_path / "run"
    run_dir.mkdir()
    _mk_df().to_parquet(run_dir / "results.parquet", index=False)

    build_report(run_dir)

    out = run_dir / "report" / "summary.md"
    text = out.read_text()
    assert "| N |" in text
    assert "| 8 |" in text
    assert "| 16 |" in text
    assert "33" in text  # timeout rate for N=8 is 1/3 ≈ 33.3%
