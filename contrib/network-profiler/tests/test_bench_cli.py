import json
import sys
from unittest.mock import patch

import pytest

from network_profiler.cli import main


def test_bench_cli_help_lists_subcommand():
    with patch.object(sys, "argv", ["net-profiler", "bench", "--help"]):
        with pytest.raises(SystemExit) as e:
            main()
        assert e.value.code == 0


def test_bench_cli_invokes_run_bench(monkeypatch, tmp_path):
    cfg = tmp_path / "machines.json"
    cfg.write_text(json.dumps({
        "remote_command": ["echo", "{command}"],
        "machines": [{"name": "a", "address": "10.0.0.1", "rcc_host": "a"}],
    }))
    called = {}

    def fake_run_bench(**kwargs):
        called.update(kwargs)
        return 0

    monkeypatch.setattr("network_profiler.bench.run_bench", fake_run_bench)
    monkeypatch.setattr("network_profiler.cli.run_bench", fake_run_bench)
    with patch.object(sys, "argv", [
        "net-profiler", "bench", "--config", str(cfg),
        "--run-id", "rTEST", "--output", str(tmp_path / "out"),
    ]):
        rc = main()
    assert rc == 0
    assert called["run_id"] == "rTEST"
