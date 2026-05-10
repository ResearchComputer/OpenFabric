import json
from pathlib import Path

from benchmark_overhead.report import render_report


def _row(model: str, rps: int, path: str, rep: int, ttft: float) -> dict:
    return {
        "ts_unix": 1.0,
        "request_id": f"{model}-{rps}-{path}-{rep}",
        "cell": {"model": model, "rps": rps, "path": path, "rep": rep, "phase": "measure",
                  "cell_key": f"{model.replace('/','_')}-rps{rps}-{path}-rep{rep}"},
        "client": {"ttft_ms": ttft, "total_ms": ttft + 800.0, "status": 200, "output_tokens": 50},
        "stages_ms": {"head_recv": 0.4, "head_dnt": 0.3, "worker_sglang_ttft": ttft - 10.0},
    }


def test_render_report_writes_three_pdfs(tmp_path: Path):
    raw = tmp_path / "raw.jsonl"
    rows = []
    for path in ("direct", "otela"):
        for rps in (1, 4):
            for rep in (1, 2):
                for k in range(20):
                    base = 100 + (10 if path == "otela" else 0) + rps * 2
                    rows.append(_row("Qwen/Qwen3-8B", rps, path, rep, base + k))
    raw.write_text("\n".join(json.dumps(r) for r in rows) + "\n")

    render_report(str(tmp_path))
    plots_dir = tmp_path / "plots"
    assert (plots_dir / "overhead_vs_rps.pdf").exists()
    assert (plots_dir / "stage_breakdown.pdf").exists()
    assert (plots_dir / "ttft_cdf.pdf").exists()
