"""Async open-loop request driver. One coroutine per scheduled arrival."""

from __future__ import annotations

import asyncio
import json
import time
import uuid
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

import httpx

from benchmark_overhead.parse import parse_server_timing


@dataclass
class RequestRecord:
    """One row of per-request bench data."""

    request_id: str
    ts_unix: float
    status: int
    ttft_ms: float | None
    total_ms: float
    output_tokens: int
    stages: dict[str, float] = field(default_factory=dict)
    raw_server_timing: str = ""
    error: str = ""


async def fire_request(
    *,
    client: httpx.AsyncClient,
    url: str,
    payload: dict[str, Any],
    request_id: str,
) -> RequestRecord:
    """Send one OpenAI-style chat completion request and record per-stage timings.

    Streams the response (assumes payload sets "stream": true). TTFT = time from
    request send to the first non-empty SSE data event.
    """
    headers = {
        "Content-Type": "application/json",
        "X-Otela-Request-Id": request_id,
    }
    started = time.perf_counter()
    ts_unix = time.time()
    ttft_ms: float | None = None
    output_tokens = 0
    raw_st = ""
    stages: dict[str, float] = {}
    try:
        async with client.stream("POST", url, headers=headers, json=payload) as resp:
            raw_st = resp.headers.get("Server-Timing", "") or resp.headers.get("X-Otela-Worker-Timing", "")
            stages = parse_server_timing(raw_st)
            async for chunk in resp.aiter_bytes():
                if not chunk:
                    continue
                if ttft_ms is None:
                    ttft_ms = (time.perf_counter() - started) * 1000.0
                # Count SSE "data: ..." lines; treat each non-[DONE] event as one output token chunk.
                # This is a coarse proxy; for token-accurate counts use the engine's metrics.
                for line in chunk.split(b"\n"):
                    line = line.strip()
                    if not line or not line.startswith(b"data: "):
                        continue
                    if line == b"data: [DONE]":
                        continue
                    output_tokens += 1
            total_ms = (time.perf_counter() - started) * 1000.0
            return RequestRecord(
                request_id=request_id,
                ts_unix=ts_unix,
                status=resp.status_code,
                ttft_ms=ttft_ms,
                total_ms=total_ms,
                output_tokens=output_tokens,
                stages=stages,
                raw_server_timing=raw_st,
            )
    except Exception as e:
        total_ms = (time.perf_counter() - started) * 1000.0
        return RequestRecord(
            request_id=request_id,
            ts_unix=ts_unix,
            status=0,
            ttft_ms=ttft_ms,
            total_ms=total_ms,
            output_tokens=output_tokens,
            stages=stages,
            raw_server_timing=raw_st,
            error=str(e),
        )


async def fire_open_loop(
    *,
    url: str,
    arrivals_s: list[float],
    prompts: list[dict],
    model: str,
    output_path: Path,
    cell_meta: dict,
    rng,
    timeout_s: float = 60.0,
) -> int:
    """Fire one request per scheduled arrival time. Each request samples a prompt
    uniformly. Each completed (or failed) request appends one JSONL row to output_path.

    Returns the number of requests fired.
    """
    if not arrivals_s:
        return 0
    output_path.parent.mkdir(parents=True, exist_ok=True)

    async with httpx.AsyncClient(timeout=timeout_s) as client:
        start_wall = time.perf_counter()
        tasks: list[asyncio.Task] = []
        for i, t in enumerate(arrivals_s):
            wait = t - (time.perf_counter() - start_wall)
            if wait > 0:
                await asyncio.sleep(wait)
            prompt = prompts[rng.integers(0, len(prompts))]
            payload = {
                "model": model,
                "stream": True,
                "messages": [{"role": "user", "content": prompt["prompt"]}],
                "max_tokens": prompt["max_output_tokens"],
                # Force vLLM to emit exactly max_tokens tokens — eliminates the
                # output-length variance that otherwise dominates p95/p99.
                "min_tokens": prompt["max_output_tokens"],
                "ignore_eos": True,
            }
            req_id = f"bench-{cell_meta['cell_key']}-{uuid.uuid4().hex[:8]}-{i}"

            tasks.append(asyncio.create_task(
                _fire_and_record(client, url, payload, req_id, output_path, cell_meta)
            ))

        await asyncio.gather(*tasks, return_exceptions=False)
        return len(tasks)


async def _fire_and_record(
    client: httpx.AsyncClient,
    url: str,
    payload: dict,
    req_id: str,
    output_path: Path,
    cell_meta: dict,
) -> None:
    rec = await fire_request(client=client, url=url, payload=payload, request_id=req_id)
    row = {
        "ts_unix": rec.ts_unix,
        "request_id": rec.request_id,
        "cell": cell_meta,
        "client": {
            "ttft_ms": rec.ttft_ms,
            "total_ms": rec.total_ms,
            "output_tokens": rec.output_tokens,
            "status": rec.status,
            "error": rec.error,
        },
        "stages_ms": rec.stages,
        "raw_server_timing": rec.raw_server_timing,
    }
    with output_path.open("a") as f:
        f.write(json.dumps(row) + "\n")
