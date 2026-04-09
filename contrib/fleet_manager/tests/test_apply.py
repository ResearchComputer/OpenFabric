from unittest.mock import MagicMock
import tempfile
import pytest
import yaml

from fleet_manager.apply import parse_fleet_file, compute_diff, Action
from fleet_manager.cluster import job_identity


def test_parse_fleet_file():
    with tempfile.NamedTemporaryFile(suffix=".yaml", mode="w", delete=False) as f:
        yaml.dump({
            "deployments": [
                {
                    "cluster": "jsc",
                    "backend": "sglang",
                    "cmd": "sglang serve Qwen/Qwen3-0.6B --tp-size 4",
                    "preset": "A100_4",
                    "replicas": 2,
                },
                {
                    "cluster": "jsc",
                    "backend": "vllm",
                    "cmd": "vllm serve meta-llama/Llama-3-8B",
                    "preset": "A100_4_dev",
                    "replicas": 1,
                },
            ]
        }, f)
        f.flush()
        result = parse_fleet_file(f.name)
    assert len(result) == 2
    assert result[0] == ("jsc", "sglang", "sglang serve Qwen/Qwen3-0.6B --tp-size 4", "A100_4", 2)
    assert result[1] == ("jsc", "vllm", "vllm serve meta-llama/Llama-3-8B", "A100_4_dev", 1)


def _mock_job(name, job_id):
    m = MagicMock()
    m.name = name
    m.id = job_id
    return m


def test_compute_diff_needs_deploy():
    job_name = job_identity("sglang", "sglang serve Qwen/Qwen3-0.6B", "A100_4")
    desired = [("jsc", "sglang", "sglang serve Qwen/Qwen3-0.6B", "A100_4", 2)]
    live_jobs = {
        "jsc": [_mock_job(job_name, "100")],
    }
    actions = compute_diff(desired, live_jobs)
    deploys = [a for a in actions if a.action == "deploy"]
    assert len(deploys) == 1
    assert deploys[0].cluster == "jsc"
    assert deploys[0].backend == "sglang"


def test_compute_diff_needs_cancel():
    job_name = job_identity("sglang", "sglang serve Qwen/Qwen3-0.6B", "A100_4")
    desired = [("jsc", "sglang", "sglang serve Qwen/Qwen3-0.6B", "A100_4", 1)]
    live_jobs = {
        "jsc": [
            _mock_job(job_name, "100"),
            _mock_job(job_name, "200"),
        ],
    }
    actions = compute_diff(desired, live_jobs)
    cancels = [a for a in actions if a.action == "cancel"]
    assert len(cancels) == 1
    assert cancels[0].job_id == "200"


def test_compute_diff_cancel_numeric_order():
    """Ensure the newest job is cancelled using numeric (not lexicographic) ordering.

    With string comparison, "99" > "100", so the wrong job would be selected.
    With int comparison, 100 > 99, so job "100" (the newer one) is correctly cancelled.
    """
    job_name = job_identity("sglang", "sglang serve Qwen/Qwen3-0.6B", "A100_4")
    desired = [("jsc", "sglang", "sglang serve Qwen/Qwen3-0.6B", "A100_4", 1)]
    live_jobs = {
        "jsc": [
            _mock_job(job_name, "99"),
            _mock_job(job_name, "100"),
        ],
    }
    actions = compute_diff(desired, live_jobs)
    cancels = [a for a in actions if a.action == "cancel"]
    assert len(cancels) == 1
    assert cancels[0].job_id == "100"


def test_compute_diff_no_op():
    job_name = job_identity("sglang", "sglang serve Qwen/Qwen3-0.6B", "A100_4")
    desired = [("jsc", "sglang", "sglang serve Qwen/Qwen3-0.6B", "A100_4", 1)]
    live_jobs = {
        "jsc": [_mock_job(job_name, "100")],
    }
    actions = compute_diff(desired, live_jobs)
    assert len(actions) == 0
