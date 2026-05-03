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
