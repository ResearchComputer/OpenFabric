"""Submit a single (N, rep) cell as an sbatch job and wait for it."""

from __future__ import annotations

import os
import subprocess
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent  # contrib/benchmark_convergence/
SBATCH_SCRIPT = ROOT / "slurm" / "convergence.sbatch"


def submit_cell(
    *, config: Path, run_id: str, n: int, rep: int, wait: bool = True
) -> int:
    cfg = yaml.safe_load(config.read_text())
    run_dir = Path(os.path.expandvars(cfg["paths"]["run_root"])) / run_id
    cell_dir = run_dir / f"N={n}" / f"rep={rep}"
    cell_dir.mkdir(parents=True, exist_ok=True)
    # On retry, wipe stale per-cell artifacts. A previous failed run
    # could have left bootstrap.txt pointing at a coord IP that no
    # longer exists; observers would read it and immediately
    # ConnectionRefused.
    for stale in ("bootstrap.txt", "results.json"):
        (cell_dir / stale).unlink(missing_ok=True)
    (cell_dir / "status").write_text("running")

    env = {
        "RUN_ID": run_id,
        "N": str(n),
        "REP": str(rep),
        "RUN_DIR": str(run_dir),
        "CONFIG_PATH": str(config.resolve()),
    }

    cmd = [
        "sbatch",
        f"--nodes={n}",
        f"--export=ALL,RUN_ID={env['RUN_ID']},N={env['N']},REP={env['REP']},"
        f"RUN_DIR={env['RUN_DIR']},CONFIG_PATH={env['CONFIG_PATH']}",
    ]
    account = cfg["slurm"].get("account", "")
    if account:
        cmd += [f"--account={account}"]
    if wait:
        cmd += ["--wait"]
    cmd += [str(SBATCH_SCRIPT)]

    proc = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if proc.returncode != 0:
        (cell_dir / "status").write_text("failed")
        raise RuntimeError(
            f"sbatch failed (rc={proc.returncode}):\n"
            f"stdout: {proc.stdout}\nstderr: {proc.stderr}"
        )

    if not (cell_dir / "results.json").exists():
        (cell_dir / "status").write_text("failed")
        return proc.returncode

    (cell_dir / "status").write_text("complete")
    return 0
