from benchmark_overhead.run import summarize_cell


def test_summarize_cell_basic():
    rows = [
        {"client": {"ttft_ms": 100.0, "total_ms": 1000.0, "status": 200, "output_tokens": 50},
         "stages_ms": {"head_recv": 0.5, "head_dnt": 0.3, "worker_sglang_ttft": 90.0}},
        {"client": {"ttft_ms": 200.0, "total_ms": 1500.0, "status": 200, "output_tokens": 50},
         "stages_ms": {"head_recv": 0.5, "head_dnt": 0.3, "worker_sglang_ttft": 190.0}},
        {"client": {"ttft_ms": None, "total_ms": 5000.0, "status": 500, "output_tokens": 0},
         "stages_ms": {}},
    ]
    s = summarize_cell(rows, cell_meta={"model": "x", "rps": 4, "path": "otela", "rep": 1})
    assert s["model"] == "x"
    assert s["n_total"] == 3
    assert s["n_ok"] == 2
    assert abs(s["error_rate"] - 1 / 3) < 1e-6
    assert s["ttft_p50_ms"] == 150.0  # midpoint of 100, 200
    # Worker-stage means computed only over OK rows
    assert s["worker_sglang_ttft_mean_ms"] == 140.0


def test_summarize_cell_all_failed():
    rows = [{"client": {"ttft_ms": None, "total_ms": 5000.0, "status": 500, "output_tokens": 0},
             "stages_ms": {}}]
    s = summarize_cell(rows, cell_meta={"model": "x", "rps": 4, "path": "otela", "rep": 1})
    assert s["n_ok"] == 0
    assert s["error_rate"] == 1.0
    assert s["ttft_p50_ms"] is None
