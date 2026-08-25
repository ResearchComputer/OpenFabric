"""Idle client->server RTT probe.

The benchmark already compares `otela` (client -> head -> worker) vs
`direct` (client -> worker) TTFT. The TTFT delta therefore conflates two
unrelated effects: (1) OpenTela's per-stage cost, and (2) the difference
in network latency from the client to two different hosts (e.g. CSCS k8s
ingress vs. an internal Clariden node).

This probe measures (2) directly: per cell, just before the workload
fires, we hit a small no-op endpoint at the cell's target origin and
record HTTP TTFB. The notebook subtracts (otela_p50 - direct_p50) from
the unattributed `others` residual so the breakdown reflects libp2p
return-leg cost only.

Connection setup is amortized across each cell's open-loop workload
(httpx.AsyncClient pools connections), so the probe warms up one
connection and then measures N samples over the warm pool — matching
the conditions a real request sees."""

from __future__ import annotations

import logging
import time
from urllib.parse import urlparse

import httpx
import numpy as np

log = logging.getLogger(__name__)


def probe_url_for(request_url: str) -> str:
    """Pick a small no-op endpoint at the same origin as `request_url`.

    - OpenTela head (path contains `/v1/service/`) -> `/v1/dnt/bootstraps`
      (same endpoint `doctor` uses; returns a small JSON immediately).
    - Direct sglang worker -> `/health` (tiny 200 OK).
    """
    p = urlparse(request_url)
    origin = f"{p.scheme}://{p.netloc}"
    if "/v1/service/" in p.path:
        return f"{origin}/v1/dnt/bootstraps"
    return f"{origin}/health"


def measure_net_rtt(
    request_url: str,
    *,
    n_samples: int = 20,
    timeout_s: float = 2.0,
) -> dict[str, float | int | str]:
    """Probe a no-op endpoint and return HTTP TTFB stats in milliseconds.

    Warms one connection (not counted), then issues `n_samples` GETs over
    the pooled client. Returns p50/p95/mean/std over OK samples, plus
    `n_ok` and the resolved `probe_url`. If every sample fails the stat
    fields are NaN and n_ok=0; the caller decides whether to abort."""
    probe_url = probe_url_for(request_url)
    samples: list[float] = []
    with httpx.Client(timeout=timeout_s) as c:
        try:
            c.get(probe_url)
        except httpx.HTTPError as e:
            log.debug("probe warmup failed for %s: %s", probe_url, e)
        for _ in range(n_samples):
            t0 = time.perf_counter()
            try:
                r = c.get(probe_url)
                _ = r.content
                if r.status_code == 200:
                    samples.append((time.perf_counter() - t0) * 1000.0)
            except httpx.HTTPError as e:
                log.debug("probe sample failed for %s: %s", probe_url, e)

    if not samples:
        return {
            "probe_url": probe_url,
            "n_ok": 0,
            "p50_ms": float("nan"),
            "p95_ms": float("nan"),
            "mean_ms": float("nan"),
            "std_ms": float("nan"),
        }

    a = np.asarray(samples, dtype=float)
    return {
        "probe_url": probe_url,
        "n_ok": int(len(samples)),
        "p50_ms": float(np.percentile(a, 50)),
        "p95_ms": float(np.percentile(a, 95)),
        "mean_ms": float(np.mean(a)),
        "std_ms": float(np.std(a, ddof=0)),
    }
