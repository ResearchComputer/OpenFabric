import json
from pathlib import Path

import numpy as np
import pytest

from benchmark_overhead.workload import (
    load_sharegpt,
    poisson_schedule,
)


def test_poisson_schedule_mean_matches_rps(tmp_path):
    rng = np.random.default_rng(42)
    arrivals = poisson_schedule(rps=10.0, duration_s=60.0, rng=rng)
    # arrivals are absolute times in [0, duration_s); count should be ~600.
    assert len(arrivals) > 500 and len(arrivals) < 700
    assert all(0.0 <= t < 60.0 for t in arrivals)
    # Monotonic
    assert all(arrivals[i] <= arrivals[i + 1] for i in range(len(arrivals) - 1))


def test_poisson_schedule_zero_rps_empty(tmp_path):
    rng = np.random.default_rng(0)
    assert poisson_schedule(rps=0.0, duration_s=10.0, rng=rng) == []


def test_load_sharegpt_filters_by_max_tokens(tmp_path: Path):
    # Synthetic ShareGPT-like file
    sample = [
        {"id": "a", "conversations": [{"from": "human", "value": "short"}]},
        {"id": "b", "conversations": [{"from": "human", "value": "x " * 5000}]},
    ]
    p = tmp_path / "sg.json"
    p.write_text(json.dumps(sample))
    items = load_sharegpt(p, max_input_tokens=512, max_output_tokens=128)
    # Only the short conversation should survive (heuristic 4 chars/token).
    assert len(items) == 1
    assert items[0]["prompt"].startswith("short")
    assert items[0]["max_output_tokens"] == 128


def test_load_sharegpt_missing_file_raises(tmp_path: Path):
    with pytest.raises(FileNotFoundError):
        load_sharegpt(tmp_path / "missing.json", max_input_tokens=512, max_output_tokens=128)
