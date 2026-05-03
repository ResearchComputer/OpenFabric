from network_profiler.model import Machine, ProfilerConfig
from network_profiler.remote import RemoteRunner


def test_remote_command_template_expands_host_and_command() -> None:
    config = ProfilerConfig(
        machines=[],
        remote_command=["rcc", "run", "{host}", "{command}"],
        ping_count=5,
        iperf_seconds=5,
        iperf_port=5201,
        connect_timeout_seconds=10,
    )
    machine = Machine(name="node-a", address="10.0.0.1", rcc_host="cluster/node-a")
    assert RemoteRunner(config).build_args(machine, "hostname") == [
        "rcc",
        "run",
        "cluster/node-a",
        "hostname",
    ]


def test_remote_runner_accepts_stdin():
    config = ProfilerConfig(
        machines=[Machine(name="a", address="x", rcc_host="a")],
        remote_command=["sh", "-c", "cat"],
        ping_count=1, iperf_seconds=1, iperf_port=5201, connect_timeout_seconds=1,
    )
    runner = RemoteRunner(config, dry_run=False)
    result = runner.run(config.machines[0], "ignored", stdin=b"hello")
    assert result.returncode == 0
    assert result.stdout == "hello"
