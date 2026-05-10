"""Sbatch lifecycle: spin up workers, wait for DNT readiness, tear down."""

from __future__ import annotations

import datetime
import json
import logging
import os
import subprocess
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import httpx
import yaml

log = logging.getLogger(__name__)


@dataclass(frozen=True)
class WorkerState:
    node: str
    sglang_port: int
    otela_port: int
    job_id: str
    model: str
    run_id: str


def read_worker_state(path: Path) -> WorkerState | None:
    p = Path(path)
    if not p.exists():
        return None
    data = json.loads(p.read_text())
    return WorkerState(
        node=data["node"],
        sglang_port=int(data["sglang_port"]),
        otela_port=int(data["otela_port"]),
        job_id=str(data["job_id"]),
        model=data["model"],
        run_id=data["run_id"],
    )


def is_model_ready_in_dnt(dnt_response: dict, *, model: str) -> bool:
    """Return True if any provider in the DNT lookup advertises the model."""
    needle = f"model={model}"
    for prov in dnt_response.get("providers") or []:
        for svc in prov.get("service") or []:
            for ig in svc.get("identityGroup") or []:
                if ig == needle:
                    return True
    return False


def _state_path(state_dir: Path, run_id: str, model: str) -> Path:
    safe = model.replace("/", "_")
    return state_dir / run_id / f"{safe}.json"


def _submit_sbatch(*, model: str, model_cfg: dict, run_id: str, cfg: dict) -> str:
    """Returns the sbatch job ID."""
    slurm = cfg["slurm"]
    state_dir = Path(slurm["bench_state_dir"]).expanduser()
    sbatch_script = Path(__file__).parent.parent / "slurm" / "worker.sbatch"
    env_export = ",".join([
        f"OTELA_BIN={cfg['otela']['binary_path']}",
        f"SGLANG_SIF={model_cfg['apptainer_image']}",
        f"MODEL={model}",
        f"RUN_ID={run_id}",
        f"BENCH_STATE_DIR={state_dir}",
        f"BOOTSTRAP_URL={cfg['otela']['bootstrap_url']}",
    ])
    cmd = [
        "sbatch",
        f"--partition={slurm['partition']}",
        f"--account={slurm['account']}",
        f"--gres={model_cfg['gres']}",
        f"--time={slurm['time_limit']}",
        f"--export={env_export}",
        str(sbatch_script),
    ]
    log.info("submitting: %s", " ".join(cmd))
    out = subprocess.check_output(cmd, text=True).strip()
    # sbatch prints "Submitted batch job 12345"
    return out.split()[-1]


def _wait_state_file(path: Path, timeout_s: int) -> WorkerState:
    start = time.time()
    while time.time() - start < timeout_s:
        s = read_worker_state(path)
        if s is not None:
            return s
        time.sleep(2)
    raise TimeoutError(f"worker state file not written within {timeout_s}s: {path}")


def _wait_dnt_ready(*, head_url: str, model: str, timeout_s: int) -> None:
    start = time.time()
    backoff = 2
    while time.time() - start < timeout_s:
        try:
            r = httpx.get(f"{head_url.rstrip('/')}/v1/dnt/lookup?service=llm", timeout=5.0)
            r.raise_for_status()
            if is_model_ready_in_dnt(r.json(), model=model):
                return
        except (httpx.HTTPError, ValueError) as e:
            log.debug("DNT poll failed: %s", e)
        time.sleep(backoff)
    raise TimeoutError(f"model {model} not registered in DNT within {timeout_s}s")


def ensure_worker(*, model: str, model_cfg: dict, run_id: str, cfg: dict, state_dir: Path) -> WorkerState:
    """Submit sbatch and block until both state file and DNT advertise readiness."""
    state_path = _state_path(state_dir, run_id, model)
    if state_path.exists():
        log.info("reusing existing worker for %s", model)
        return read_worker_state(state_path)  # type: ignore[return-value]
    job_id = _submit_sbatch(model=model, model_cfg=model_cfg, run_id=run_id, cfg=cfg)
    log.info("sbatch %s for %s", job_id, model)
    state = _wait_state_file(state_path, timeout_s=600)
    _wait_dnt_ready(head_url=cfg["head_url"], model=model, timeout_s=600)
    return state


def run_deploy(config_path: str) -> None:
    cfg = yaml.safe_load(Path(config_path).expanduser().read_text())
    run_id = os.environ.get("OTELA_BENCH_RUN_ID") or _new_run_id()
    state_dir = Path(cfg["slurm"]["bench_state_dir"]).expanduser()
    (state_dir / run_id).mkdir(parents=True, exist_ok=True)
    print(f"run_id={run_id}")
    states = []
    for m in cfg["models"]:
        st = ensure_worker(
            model=m["name"],
            model_cfg=m,
            run_id=run_id,
            cfg=cfg,
            state_dir=state_dir,
        )
        states.append(st)
    summary = {"run_id": run_id, "workers": [s.__dict__ for s in states]}
    print(json.dumps(summary, indent=2))


def _new_run_id() -> str:
    return "r-" + datetime.datetime.now().strftime("%Y%m%dT%H%M%S")


def run_teardown(run_id: str) -> None:
    """scancel every sbatch job spawned by the run."""
    cfg_root = Path("~/.opentela-bench").expanduser() / run_id
    for state_file in cfg_root.glob("*.json"):
        s = read_worker_state(state_file)
        if s is None:
            continue
        log.info("scancel %s (%s)", s.job_id, s.model)
        subprocess.run(["scancel", s.job_id], check=False)
        state_file.unlink(missing_ok=True)
