"""SSH-based driver: orchestrate the convergence benchmark across a list of
SSH-reachable hosts. No SLURM, no shared filesystem required.

Setup per host (idempotent):
  - scp the local otela binary to ~/.opentela-bench/otela
  - rsync the benchmark_convergence/ package
  - pip install --user httpx pyyaml

Run:
  1. Spawn task_entry on host[0] (procid=0). Capture its BOOTSTRAP_ADDR
     line from stdout.
  2. Spawn task_entry on host[1..N-1] in parallel, passing BOOTSTRAP_ADDR
     as an env var.
  3. Wait for the coordinator to finish (it writes results.json on host[0]).
  4. scp the cell directory back to the local output dir.
"""
from __future__ import annotations

import shlex
import subprocess
import time
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent  # contrib/benchmark_convergence/
REMOTE_DIR = "~/.opentela-bench"


def _ssh(host: str, cmd: str, check: bool = False, **kw) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["ssh", "-o", "BatchMode=yes", host, cmd],
        capture_output=True, text=True, check=check, **kw,
    )


def _setup_node(host: str, otela_bin: Path) -> None:
    """scp otela + rsync package + pip-install deps. Idempotent."""
    print(f"[ssh_driver] setup {host}: mkdir")
    _ssh(host, f"mkdir -p {REMOTE_DIR}/runs", check=True)

    print(f"[ssh_driver] setup {host}: scp otela")
    subprocess.run(
        ["scp", "-q", str(otela_bin), f"{host}:{REMOTE_DIR}/otela"],
        check=True,
    )
    _ssh(host, f"chmod +x {REMOTE_DIR}/otela", check=True)

    print(f"[ssh_driver] setup {host}: rsync package")
    subprocess.run([
        "rsync", "-az", "--delete",
        "--exclude", "__pycache__", "--exclude", ".venv",
        "--exclude", "uv.lock", "--exclude", ".pytest_cache",
        "--exclude", "runs",
        f"{PROJECT_ROOT}/",
        f"{host}:{REMOTE_DIR}/benchmark_convergence/",
    ], check=True)

    print(f"[ssh_driver] setup {host}: pip install httpx pyyaml")
    rc = _ssh(host, "pip3 install --user --quiet httpx pyyaml", check=False)
    if rc.returncode != 0:
        # Some systems use `pip` instead of `pip3`.
        rc = _ssh(host, "pip install --user --quiet httpx pyyaml", check=False)
    if rc.returncode != 0:
        raise RuntimeError(
            f"{host}: failed to install httpx + pyyaml\n"
            f"stdout: {rc.stdout}\nstderr: {rc.stderr}"
        )


def _write_remote_config(host: str, *, http_port: int, admin_port: int,
                         libp2p_port: int, coord_port: int, poll_ms: int,
                         writes: int, stab_to: int, write_to: int,
                         gap_ms: int) -> None:
    """Write a YAML config file on the remote (task_entry reads YAML)."""
    cfg = f"""\
slurm:
  account: ""
paths:
  otela_bin: "{REMOTE_DIR}/otela"
  run_root:  "{REMOTE_DIR}/runs"
  config_root_in_task: "/tmp/convbench"
network:
  otela_http_port: {http_port}
  otela_admin_port: {admin_port}
  otela_libp2p_port: {libp2p_port}
  coordinator_tcp_port: {coord_port}
sweep:
  sizes: [0]
  reps: 1
  writes_per_rep: {writes}
  stabilization_timeout_s: {stab_to}
  per_write_timeout_s: {write_to}
  poll_interval_ms: {poll_ms}
  inter_write_gap_ms: {gap_ms}
"""
    cfg_path = f"{REMOTE_DIR}/benchmark_convergence/benchmark_convergence/config/ssh.yaml"
    # heredoc-safe install: write through ssh stdin
    subprocess.run(
        ["ssh", "-o", "BatchMode=yes", host, f"cat > {cfg_path}"],
        input=cfg, text=True, check=True,
    )


def _spawn_task_entry(host: str, *, procid: int, n: int, run_id: str,
                      bootstrap_addr: str | None = None,
                      capture_stdout: bool = False) -> subprocess.Popen:
    cfg_path = f"{REMOTE_DIR}/benchmark_convergence/benchmark_convergence/config/ssh.yaml"
    run_dir = f"{REMOTE_DIR}/runs/{run_id}"
    env_parts = [
        f"SLURM_PROCID={procid}",
        f"RUN_ID={shlex.quote(run_id)}",
        f"N={n}",
        "REP=0",
        f"RUN_DIR={run_dir}",
        f"CONFIG_PATH={cfg_path}",
        f"PYTHONPATH={REMOTE_DIR}/benchmark_convergence",
    ]
    if bootstrap_addr:
        env_parts.append(f"BOOTSTRAP_ADDR={shlex.quote(bootstrap_addr)}")
    env_str = " ".join(env_parts)
    cmd = (
        f"cd {REMOTE_DIR}/benchmark_convergence && "
        f"{env_str} python3 -m benchmark_convergence.task_entry"
    )
    return subprocess.Popen(
        ["ssh", "-o", "BatchMode=yes", host, cmd],
        stdout=subprocess.PIPE if capture_stdout else None,
        stderr=subprocess.PIPE,
        text=True, bufsize=1,
    )


def _read_until(proc: subprocess.Popen, marker: str,
                timeout_s: float = 180.0) -> str:
    """Read coord stdout until we see a line starting with `marker`. Raises on
    timeout or premature exit."""
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        if proc.poll() is not None:
            # Process exited before emitting marker.
            stderr = proc.stderr.read() if proc.stderr else ""
            raise RuntimeError(
                f"coordinator exited (rc={proc.returncode}) before emitting "
                f"{marker!r}\nstderr: {stderr}"
            )
        # blocking readline; relies on caller-set stdout PIPE
        line = proc.stdout.readline() if proc.stdout else ""
        if line.startswith(marker):
            return line.strip()
    raise TimeoutError(f"did not see {marker!r} within {timeout_s}s")


def run_ssh(*, nodes: list[str], otela_bin: Path, run_id: str,
            output_dir: Path,
            http_port: int = 8092, admin_port: int = 8093,
            libp2p_port: int = 43905, coord_port: int = 47001,
            writes: int = 20, poll_ms: int = 50,
            stab_to: int = 60, write_to: int = 30,
            gap_ms: int = 500) -> Path:
    """Run one (N=len(nodes), rep=0) cell across `nodes` over SSH.

    Returns the local path to the per-cell results.json.
    """
    if len(nodes) < 2:
        raise ValueError(f"need at least 2 nodes, got {len(nodes)}")

    n = len(nodes)
    coord_host = nodes[0]
    print(f"[ssh_driver] {n} nodes; coordinator={coord_host}")

    print("[ssh_driver] Phase 1: setup nodes")
    for node in nodes:
        _setup_node(node, otela_bin)
        _write_remote_config(
            node, http_port=http_port, admin_port=admin_port,
            libp2p_port=libp2p_port, coord_port=coord_port, poll_ms=poll_ms,
            writes=writes, stab_to=stab_to, write_to=write_to, gap_ms=gap_ms,
        )

    print(f"[ssh_driver] Phase 2: spawn coordinator on {coord_host}")
    coord_proc = _spawn_task_entry(
        coord_host, procid=0, n=n, run_id=run_id, capture_stdout=True,
    )
    bootstrap_line = _read_until(coord_proc, "BOOTSTRAP_ADDR=", timeout_s=180)
    bootstrap_addr = bootstrap_line.split("=", 1)[1]
    print(f"[ssh_driver] coordinator bootstrap: {bootstrap_addr}")

    print(f"[ssh_driver] Phase 3: spawn {n - 1} observers")
    obs_procs: list[subprocess.Popen] = []
    for k, host in enumerate(nodes[1:], start=1):
        obs_procs.append(_spawn_task_entry(
            host, procid=k, n=n, run_id=run_id, bootstrap_addr=bootstrap_addr,
        ))

    print("[ssh_driver] Phase 4: wait for coordinator")
    coord_proc.wait()
    print(f"[ssh_driver] coordinator exited rc={coord_proc.returncode}")
    if coord_proc.returncode != 0:
        stderr = coord_proc.stderr.read() if coord_proc.stderr else ""
        print(f"[ssh_driver] coord stderr:\n{stderr}")
    for p in obs_procs:
        try:
            p.wait(timeout=30)
        except subprocess.TimeoutExpired:
            p.terminate()

    print("[ssh_driver] Phase 5: scp results back")
    output_dir.mkdir(parents=True, exist_ok=True)
    rc = subprocess.run([
        "scp", "-rq",
        f"{coord_host}:{REMOTE_DIR}/runs/{run_id}",
        str(output_dir),
    ]).returncode
    if rc != 0:
        raise RuntimeError(f"scp results failed (rc={rc})")

    local_cell = output_dir / run_id / f"N={n}" / "rep=0"
    results_json = local_cell / "results.json"
    if not results_json.exists():
        raise RuntimeError(
            f"results.json missing at {results_json}; check coord stderr above"
        )
    print(f"[ssh_driver] done. results: {results_json}")
    return results_json
