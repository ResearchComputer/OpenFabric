import json
import time
from pathlib import Path
from unittest.mock import patch

import pytest

from contrib.benchmark.benchmark_overhead.benchmark_overhead.deploy import (
    WorkerState,
    read_worker_state,
    is_model_ready_in_dnt,
)


def test_read_worker_state_parses_json(tmp_path: Path):
    p = tmp_path / "Qwen_Qwen3-8B.json"
    p.write_text(json.dumps({
        "node": "nid001",
        "sglang_port": 30001,
        "otela_port": 19090,
        "job_id": "12345",
        "model": "Qwen/Qwen3-8B",
        "run_id": "r-1",
    }))
    s = read_worker_state(p)
    assert isinstance(s, WorkerState)
    assert s.node == "nid001"
    assert s.sglang_port == 30001
    assert s.otela_port == 19090
    assert s.job_id == "12345"


def test_read_worker_state_missing_returns_none(tmp_path: Path):
    assert read_worker_state(tmp_path / "missing.json") is None


def test_is_model_ready_true_when_provider_listed():
    sample = {
        "providers": [
            {"id": "Qm...", "service": [{"name": "llm", "identityGroup": ["model=Qwen/Qwen3-8B"]}]},
        ],
    }
    assert is_model_ready_in_dnt(sample, model="Qwen/Qwen3-8B") is True


def test_is_model_ready_false_when_not_listed():
    sample = {
        "providers": [
            {"id": "Qm...", "service": [{"name": "llm", "identityGroup": ["model=other"]}]},
        ],
    }
    assert is_model_ready_in_dnt(sample, model="Qwen/Qwen3-8B") is False


def test_is_model_ready_handles_empty():
    assert is_model_ready_in_dnt({}, model="x") is False
    assert is_model_ready_in_dnt({"providers": []}, model="x") is False
