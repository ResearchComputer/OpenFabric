"""Preflight checks. Run on the Euler login node before sweeping."""
from __future__ import annotations

import os
import shutil
import subprocess
import sys
from pathlib import Path

import yaml


def _ok(msg: str) -> None:
    print(f"  ok    {msg}")


def _fail(msg: str) -> None:
    print(f"  FAIL  {msg}")


def run(config: Path) -> None:
    cfg = yaml.safe_load(config.read_text())
    failures = 0

    print("[doctor] paths")
    otela = Path(os.path.expandvars(cfg["paths"]["otela_bin"]))
    if otela.exists() and os.access(otela, os.X_OK):
        _ok(f"{otela} is executable")
    else:
        _fail(f"{otela} missing or not executable")
        failures += 1

    scratch = Path(os.path.expandvars(cfg["paths"]["run_root"]))
    scratch.mkdir(parents=True, exist_ok=True)
    if os.access(scratch, os.W_OK):
        _ok(f"{scratch} writable")
    else:
        _fail(f"{scratch} not writable")
        failures += 1

    print("[doctor] tools")
    for tool in ("sbatch", "srun"):
        if shutil.which(tool):
            _ok(f"{tool} on PATH")
        else:
            _fail(f"{tool} not on PATH")
            failures += 1
    if shutil.which("chronyc"):
        _ok("chronyc on PATH (clock offset will be measured)")
    else:
        print("  warn  chronyc not on PATH; clock offsets will be 0 "
              "(NTP-level skew remains as residual noise)")

    print("[doctor] slurm account")
    account = cfg["slurm"].get("account", "")
    if account:
        _ok(f"account = {account}")
    else:
        _fail("slurm.account is empty; set it in config")
        failures += 1

    print("[doctor] otela --help")
    try:
        out = subprocess.run(
            [str(otela), "start", "--help"],
            capture_output=True, text=True, timeout=10, check=True,
        )
        if "admin.enabled" in out.stdout or "admin.enabled" in out.stderr:
            _ok("otela start --admin.enabled flag present")
        else:
            _fail("otela binary does not expose --admin.enabled "
                  "(rebuild from feat/bench_overhead branch with admin route)")
            failures += 1
    except Exception as e:
        _fail(f"otela start --help failed: {e}")
        failures += 1

    if failures:
        print(f"\n{failures} check(s) failed", file=sys.stderr)
        sys.exit(1)
    print("\nall checks passed")
