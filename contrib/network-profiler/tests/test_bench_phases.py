from network_profiler.bench import phase_init, phase_discover
from network_profiler.model import Machine, ProfilerConfig
from network_profiler.remote import RemoteRunner


def make_runner():
    config = ProfilerConfig(
        machines=[
            Machine(name="a", address="10.0.0.1", rcc_host="a"),
            Machine(name="b", address="10.0.0.2", rcc_host="b"),
        ],
        remote_command=["bash", "-lc", "{command}"],
        ping_count=5,
        iperf_seconds=5,
        iperf_port=5201,
        connect_timeout_seconds=10,
    )
    return RemoteRunner(config, dry_run=True), config


def test_phase_init_runs_otela_init_per_host():
    runner, config = make_runner()
    results = phase_init(runner, config.machines, run_id="run123")
    assert set(results.keys()) == {"a", "b"}
    # Each host gets its own bench dir suffix so multiple nodes on the
    # same machine (smoke test) don't collide.
    assert "/tmp/otela-bench-run123-a" in results["a"]
    assert "/tmp/otela-bench-run123-b" in results["b"]
    for cmd in results.values():
        assert "otela init" in cmd


def test_phase_discover_parses_peer_id():
    config = ProfilerConfig(
        machines=[Machine(name="a", address="10.0.0.1", rcc_host="a")],
        remote_command=None,
        ping_count=5,
        iperf_seconds=5,
        iperf_port=5201,
        connect_timeout_seconds=10,
    )

    class FakeRunner:
        def run(self, machine, command, timeout=None):
            from network_profiler.remote import CommandResult
            return CommandResult(machine.name, command, 0, "12D3KooWAAA\n", "")

    peer_ids = phase_discover(FakeRunner(), config.machines, run_id="run123")
    assert peer_ids == {"a": "12D3KooWAAA"}


import base64

from network_profiler.bench import phase_configure_and_push


def test_phase_configure_and_push_writes_yaml_per_host():
    machines = [
        Machine(name="a", address="10.0.0.1", rcc_host="a"),
        Machine(name="b", address="10.0.0.2", rcc_host="b"),
    ]
    peer_ids = {"a": "PIDA", "b": "PIDB"}

    captured: list[tuple[str, str, bytes]] = []

    class FakeRunner:
        def run(self, machine, command, timeout=None, stdin=None):
            captured.append((machine.name, command, stdin or b""))
            from network_profiler.remote import CommandResult
            return CommandResult(machine.name, command, 0, "", "")

    phase_configure_and_push(
        FakeRunner(),
        machines,
        peer_ids,
        run_id="run123",
        http_port=19090,
        libp2p_port=19091,
    )
    assert {h for h, _, _ in captured} == {"a", "b"}
    for host, cmd, stdin in captured:
        assert "base64 -d" in cmd
        # Per-host bench dir
        assert f"/tmp/otela-bench-run123-{host}/cfg.yaml" in cmd
        decoded = base64.b64decode(stdin).decode()
        assert "bootstrap:" in decoded
        if host == "a":
            assert "PIDB" in decoded and "PIDA" not in decoded
        else:
            assert "PIDA" in decoded and "PIDB" not in decoded


import json

from network_profiler.bench import phase_start, phase_converge
from network_profiler.remote import CommandResult


class ScriptedRunner:
    """Returns canned responses keyed on (host, command-substring)."""
    def __init__(self, responses):
        self.responses = responses
        self.calls = []

    def run(self, machine, command, timeout=None, stdin=None):
        self.calls.append((machine.name, command))
        for substr, payload in self.responses.get(machine.name, []):
            if substr in command:
                rc, stdout, stderr = payload
                return CommandResult(machine.name, command, rc, stdout, stderr)
        raise AssertionError(f"unexpected command for {machine.name}: {command}")


def test_phase_start_polls_health_then_succeeds():
    machines = [Machine(name="a", address="x", rcc_host="a")]
    runner = ScriptedRunner({
        "a": [
            ("nohup otela start", (0, "", "")),
            ("curl -fsS http://127.0.0.1:19090/v1/health", (0, '{"status":"ok"}', "")),
        ],
    })
    phase_start(runner, machines, run_id="run123", http_port=19090, max_wait_s=5)
    assert any("nohup otela start" in c for _, c in runner.calls)
    assert any("/v1/health" in c for _, c in runner.calls)


def test_phase_converge_returns_when_all_peers_seen():
    machines = [
        Machine(name="a", address="x", rcc_host="a"),
        Machine(name="b", address="y", rcc_host="b"),
    ]
    peer_ids = {"a": "12D3PIDA", "b": "12D3PIDB"}
    table_a = json.dumps({"peers": [{"id": "12D3PIDB"}]})
    table_b = json.dumps({"peers": [{"id": "12D3PIDA"}]})
    runner = ScriptedRunner({
        "a": [("dnt/table", (0, table_a, ""))],
        "b": [("dnt/table", (0, table_b, ""))],
    })
    convergence = phase_converge(runner, machines, peer_ids, http_port=19090, max_wait_s=5)
    assert convergence["a"]["complete"] is True
    assert convergence["b"]["complete"] is True


import tempfile
from pathlib import Path

from network_profiler.bench import phase_sweep


def test_phase_sweep_writes_jsonl_record_per_pair_and_kind():
    machines = [
        Machine(name="a", address="x", rcc_host="a"),
        Machine(name="b", address="y", rcc_host="b"),
    ]
    peer_ids = {"a": "PIDA", "b": "PIDB"}
    canned = json.dumps({
        "ok": True, "kind": "latency",
        "metrics": {"avg_ns": 1234567, "avg_ms": 1.234},
    })
    runner = ScriptedRunner({
        "a": [("otela probe", (0, canned, ""))],
        "b": [("otela probe", (0, canned, ""))],
    })
    with tempfile.TemporaryDirectory() as tmp:
        out_path = Path(tmp) / "measurements.jsonl"
        phase_sweep(
            runner, machines, peer_ids,
            run_id="run123",
            output=out_path,
            kinds=[
                {"kind": "latency", "count": 10, "bytes": 0},
                {"kind": "throughput", "count": 1, "bytes": 1048576},
            ],
        )
        lines = out_path.read_text().splitlines()
        assert len(lines) == 4  # 2 ordered pairs × 2 kinds
        records = [json.loads(l) for l in lines]
        assert {(r["source"], r["target"], r["kind"]) for r in records} == {
            ("a", "b", "latency"),
            ("b", "a", "latency"),
            ("a", "b", "throughput"),
            ("b", "a", "throughput"),
        }
        assert all(r["source_peer_id"] for r in records)
        assert all(r["target_peer_id"] for r in records)
        assert all(r["ok"] for r in records)
