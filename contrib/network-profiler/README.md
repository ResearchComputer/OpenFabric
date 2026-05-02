# Network Profiler

Pairwise network profiler for machines managed through
[`ResearchComputer/remote-cluster-controller`](https://github.com/ResearchComputer/remote-cluster-controller).

It runs commands on every source machine, targets every other machine, records
latency with `ping`, optionally records bandwidth with `iperf3`, and renders an
HTML heatmap.

## Configure machines

Copy `examples/machines.example.json` and update:

- `machines[].name`: label used in output.
- `machines[].address`: address reachable from the other remote machines.
- `machines[].rcc_host`: host identifier understood by `remote-cluster-controller`.
- `remote_command`: argv template for running a shell command on a remote host.

The default remote command template is:

```json
[
  "remote-cluster-controller",
  "exec",
  "--host",
  "{host}",
  "--",
  "bash",
  "-lc",
  "{command}"
]
```

If your local checkout exposes a different CLI, change only `remote_command`.
The template supports `{host}`, `{name}`, `{address}`, and `{command}`.

## Run

Show what will be executed:

```bash
python3 -m network_profiler plan --config machines.json
```

Collect latency only:

```bash
python3 -m network_profiler collect --config machines.json --output results/network.jsonl
```

Collect latency and bandwidth:

```bash
python3 -m network_profiler collect --config machines.json --iperf --output results/network.jsonl
```

Render heatmaps:

```bash
python3 -m network_profiler heatmap --input results/network.jsonl --output results/ping.html
python3 -m network_profiler heatmap --input results/network.jsonl --kind iperf3 --output results/iperf.html
```

## Remote requirements

Each remote machine needs:

- `bash`
- `ping`
- `iperf3` only when `--iperf` is used
- network paths that allow source machines to reach target `address` values
- TCP access to `iperf_port` between remote machines when collecting bandwidth

The JSONL output preserves every command, success flag, parsed metrics, and
failure text so failed links can be inspected without rerunning the whole sweep.
