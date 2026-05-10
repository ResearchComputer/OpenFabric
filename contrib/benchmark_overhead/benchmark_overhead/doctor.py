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


def diagnose_response(*, head_reachable: bool, server_timing_seen: bool,
                       sharegpt_present: bool, slurm_reachable: bool) -> Diagnosis:
    problems: list[str] = []
    if not head_reachable:
        problems.append("OpenTela head is not reachable on the configured URL.")
    if not server_timing_seen:
        problems.append("Server-Timing header missing on probe — set OF_OBSERVABILITY_TIMING_HEADERS=true on the head and roll out the deployment.")
    if not sharegpt_present:
        problems.append("ShareGPT cache not found at configured sharegpt_path.")
    if not slurm_reachable:
        problems.append("`sbatch` not on PATH — are you on a Clariden login node?")
    return Diagnosis(ok=not problems, problems=problems)


def _probe_head(head_url: str) -> tuple[bool, bool]:
    try:
        r = httpx.get(f"{head_url.rstrip('/')}/v1/dnt/bootstraps", timeout=5.0)
        if r.status_code != 200:
            return False, False
        timing = r.headers.get("Server-Timing", "")
        return True, bool(timing)
    except httpx.HTTPError:
        return False, False


def run_doctor(*, head_url: str, config_path: str | None) -> None:
    head_reachable, server_timing_seen = _probe_head(head_url)

    sharegpt_present = True
    if config_path:
        cfg = yaml.safe_load(Path(config_path).expanduser().read_text())
        sg = Path(cfg.get("sharegpt_path", "")).expanduser()
        sharegpt_present = sg.exists()

    slurm_reachable = shutil.which("sbatch") is not None

    diag = diagnose_response(
        head_reachable=head_reachable,
        server_timing_seen=server_timing_seen,
        sharegpt_present=sharegpt_present,
        slurm_reachable=slurm_reachable,
    )
    print(f"head_reachable={head_reachable}")
    print(f"server_timing_seen={server_timing_seen}")
    print(f"sharegpt_present={sharegpt_present}")
    print(f"slurm_reachable={slurm_reachable}")
    if diag.ok:
        print("OK — preflight passed.")
        return
    print("FAIL:")
    for p in diag.problems:
        print(f"  - {p}")
    raise SystemExit(1)
