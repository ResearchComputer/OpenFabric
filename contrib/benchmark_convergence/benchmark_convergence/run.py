"""Top-level sweep loop. Resumable via per-cell status files."""
from __future__ import annotations

import os
import time
from pathlib import Path

import yaml

from benchmark_convergence.submit import submit_cell

# Wait this long between cell submissions so that the previous cell's
# libp2p TCP TIME_WAIT clears before SLURM possibly reuses the same
# compute host. Linux default TIME_WAIT is 60s.
_INTER_CELL_DELAY_S = 90


def _status(cell_dir: Path) -> str:
    s = cell_dir / "status"
    if not s.exists():
        return "pending"
    return s.read_text().strip()


def sweep(*, config: Path, run_id: str, resume: bool, retry_failed: bool) -> None:
    cfg = yaml.safe_load(config.read_text())
    run_dir = Path(os.path.expandvars(cfg["paths"]["run_root"])) / run_id
    run_dir.mkdir(parents=True, exist_ok=True)

    sizes = cfg["sweep"]["sizes"]
    reps = int(cfg["sweep"]["reps"])

    first = True
    for n in sizes:
        for rep in range(reps):
            cell_dir = run_dir / f"N={n}" / f"rep={rep}"
            st = _status(cell_dir)
            if st == "complete" and resume:
                print(f"[skip] N={n} rep={rep} (already complete)")
                continue
            if st == "failed" and not retry_failed:
                print(f"[skip] N={n} rep={rep} (failed; pass --retry-failed)")
                continue
            if not first:
                print(f"[wait] {_INTER_CELL_DELAY_S}s before next cell "
                      f"(libp2p TIME_WAIT margin)")
                time.sleep(_INTER_CELL_DELAY_S)
            first = False
            print(f"[submit] N={n} rep={rep}")
            try:
                submit_cell(config=config, run_id=run_id, n=n, rep=rep)
            except Exception as e:
                print(f"[fail] N={n} rep={rep}: {e}")
                with (run_dir / "incidents.log").open("a") as f:
                    f.write(f"N={n} rep={rep}: {e}\n")
