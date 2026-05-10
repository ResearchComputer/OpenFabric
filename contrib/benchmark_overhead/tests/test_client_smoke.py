import asyncio
import socket

import httpx
import pytest
from aiohttp import web

from benchmark_overhead.client import RequestRecord, fire_request


def _free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


@pytest.mark.asyncio
async def test_fire_request_records_ttft_and_total():
    async def handler(request: web.Request) -> web.StreamResponse:
        resp = web.StreamResponse(
            headers={
                "Content-Type": "text/event-stream",
                "Server-Timing": "head_recv;dur=0.1, head_dnt;dur=0.2",
            }
        )
        await resp.prepare(request)
        await asyncio.sleep(0.02)
        await resp.write(b"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
        await asyncio.sleep(0.01)
        await resp.write(b"data: [DONE]\n\n")
        return resp

    app = web.Application()
    app.router.add_post("/v1/chat/completions", handler)
    runner = web.AppRunner(app)
    await runner.setup()

    port = _free_port()
    site = web.TCPSite(runner, "127.0.0.1", port)
    await site.start()

    try:
        async with httpx.AsyncClient(timeout=10.0) as client:
            rec = await fire_request(
                client=client,
                url=f"http://127.0.0.1:{port}/v1/chat/completions",
                payload={
                    "model": "test",
                    "messages": [{"role": "user", "content": "hi"}],
                    "stream": True,
                },
                request_id="t-0",
            )
        assert isinstance(rec, RequestRecord)
        assert rec.status == 200
        assert rec.ttft_ms is not None and rec.ttft_ms >= 15.0
        assert rec.total_ms >= rec.ttft_ms
        assert rec.stages.get("head_recv") == 0.1
        assert rec.stages.get("head_dnt") == 0.2
    finally:
        await runner.cleanup()
