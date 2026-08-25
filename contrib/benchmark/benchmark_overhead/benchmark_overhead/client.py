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

from contrib.benchmark.benchmark_overhead.benchmark_overhead.parse import parse_server_timing


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
    # Per-TCP-chunk arrival times in ms (from request send). One entry per
    # non-empty bytes chunk delivered by httpx. Used to study streaming
    # delivery patterns (e.g. coalescing, jitter) post-TTFT.
    chunk_ts_ms: list[float] = field(default_factory=list)
    # Tokens parsed out of each chunk (sums to output_tokens). When a chunk
    # carries multiple SSE events, the count is >1 — that's a coalescing
    # signal worth distinguishing from per-token delivery.
    chunk_token_counts: list[int] = field(default_factory=list)


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
    chunk_ts_ms: list[float] = []
    chunk_token_counts: list[int] = []
    try:
        async with client.stream("POST", url, headers=headers, json=payload) as resp:
            raw_st = resp.headers.get("Server-Timing", "") or resp.headers.get(
                "X-Otela-Worker-Timing", ""
            )
            stages = parse_server_timing(raw_st)
            async for chunk in resp.aiter_bytes():
                if not chunk:
                    continue
                now_ms = (time.perf_counter() - started) * 1000.0
                if ttft_ms is None:
                    ttft_ms = now_ms
                # Count SSE "data: ..." lines in this TCP chunk. Multiple
                # tokens per chunk means the path is coalescing.
                # Also pick up the head's `: otela-head-first-byte=...` SSE
                # comment line, used to surface the head's first-byte-sent
                # timing (HTTP trailers would have been more idiomatic but
                # Python HTTP clients don't expose them).
                tokens_here = 0
                for line in chunk.split(b"\n"):
                    line = line.strip()
                    if not line:
                        continue
                    if line.startswith(b": otela-head-first-byte="):
                        try:
                            stages["head_first_byte_sent"] = float(
                                line[len(b": otela-head-first-byte=") :].decode()
                            )
                        except ValueError:
                            pass
                        continue
                    if not line.startswith(b"data: "):
                        continue
                    if line == b"data: [DONE]":
                        continue
                    tokens_here += 1
                if tokens_here > 0:
                    chunk_ts_ms.append(now_ms)
                    chunk_token_counts.append(tokens_here)
                    output_tokens += tokens_here
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
                chunk_ts_ms=chunk_ts_ms,
                chunk_token_counts=chunk_token_counts,
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
            chunk_ts_ms=chunk_ts_ms,
            chunk_token_counts=chunk_token_counts,
        )


async def fire_closed_loop(
    *,
    url: str,
    n_requests: int,
    prompts: list[dict],
    model: str,
    output_path: Path,
    cell_meta: dict,
    rng,
    timeout_s: float = 60.0,
) -> int:
    """Closed-loop fire: at most ONE outstanding request at any time.

    Sends request N+1 only after request N has fully completed. Guarantees
    no overlapping requests on the worker -- the "truly unloaded" baseline
    for measuring mechanical per-stage cost. Each completed request appends
    one JSONL row to output_path. Returns the number of requests fired.
    """
    if n_requests <= 0:
        return 0
    output_path.parent.mkdir(parents=True, exist_ok=True)

    async with httpx.AsyncClient(timeout=timeout_s) as client:
        for i in range(n_requests):
            prompt = prompts[rng.integers(0, len(prompts))]
            payload = {
                "model": model,
                "stream": True,
                "messages": [{"role": "user", "content": prompt["prompt"]}],
                "max_tokens": prompt["max_output_tokens"],
                "min_tokens": prompt["max_output_tokens"],
                "ignore_eos": True,
            }
            req_id = f"bench-{cell_meta['cell_key']}-{uuid.uuid4().hex[:8]}-{i}"
            await _fire_and_record(client, url, payload, req_id, output_path, cell_meta)
        return n_requests


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

            tasks.append(
                asyncio.create_task(
                    _fire_and_record(
                        client, url, payload, req_id, output_path, cell_meta
                    )
                )
            )

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
    # Round chunk timestamps to 3 decimal places (us precision) to keep JSONL
    # small. For 128 tokens × 10k requests we add ~16 MB worst case.
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
            "chunk_ts_ms": [round(t, 3) for t in rec.chunk_ts_ms],
            "chunk_token_counts": rec.chunk_token_counts,
        },
        "stages_ms": rec.stages,
        "raw_server_timing": rec.raw_server_timing,
    }
    with output_path.open("a") as f:
        f.write(json.dumps(row) + "\n")
