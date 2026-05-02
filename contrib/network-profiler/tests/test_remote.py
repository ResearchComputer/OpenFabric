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
