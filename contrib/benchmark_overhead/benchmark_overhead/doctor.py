"""Preflight checks before kicking off a long sweep."""

from __future__ import annotations

import shutil
import subprocess
from dataclasses import dataclass, field
from pathlib import Path

import httpx
import yaml


@dataclass
class Diagnosis:
    ok: bool
    problems: list[str] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)


def diagnose_response(*, head_reachable: bool,
                       sharegpt_present: bool, slurm_reachable: bool) -> Diagnosis:
    problems: list[str] = []
    if not head_reachable:
        problems.append("OpenTela head is not reachable on the configured URL.")
    if not sharegpt_present:
        problems.append("ShareGPT cache not found at configured sharegpt_path.")
    if not slurm_reachable:
        problems.append("`sbatch` not on PATH — are you on a Clariden login node?")
    return Diagnosis(ok=not problems, problems=problems)


def _probe_head(head_url: str) -> bool:
    try:
        r = httpx.get(f"{head_url.rstrip('/')}/v1/dnt/bootstraps", timeout=5.0)
        return r.status_code == 200
    except httpx.HTTPError:
        return False


def run_doctor(*, head_url: str, config_path: str | None) -> None:
    head_reachable = _probe_head(head_url)

    sharegpt_present = True
    if config_path:
        cfg = yaml.safe_load(Path(config_path).expanduser().read_text())
        sg_path = cfg.get("sharegpt_path") or ""
        if not sg_path:
            sharegpt_present = False
        else:
            sharegpt_present = Path(sg_path).expanduser().exists()

    slurm_reachable = shutil.which("sbatch") is not None

    diag = diagnose_response(
        head_reachable=head_reachable,
        sharegpt_present=sharegpt_present,
        slurm_reachable=slurm_reachable,
    )
    print(f"head_reachable={head_reachable}")
    print(f"sharegpt_present={sharegpt_present}")
    print(f"slurm_reachable={slurm_reachable}")
    print("Note: verify Server-Timing emission manually after a real bench request.")
    if diag.ok:
        print("OK — preflight passed.")
        return
    print("FAIL:")
    for p in diag.problems:
        print(f"  - {p}")
    raise SystemExit(1)
