import asyncio
import socket

import pytest
from aiohttp import web

from benchmark_overhead.probe_net import measure_net_rtt, probe_url_for


def _free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def test_probe_url_for_otela_head():
    url = "http://head.example:8092/v1/service/llm/v1/chat/completions"
    assert probe_url_for(url) == "http://head.example:8092/v1/dnt/bootstraps"


def test_probe_url_for_direct_worker():
    url = "http://10.0.0.1:30000/v1/chat/completions"
    assert probe_url_for(url) == "http://10.0.0.1:30000/health"


@pytest.mark.asyncio
async def test_measure_net_rtt_against_stub_head():
    async def bootstraps(_: web.Request) -> web.Response:
        return web.json_response({"bootstraps": []})

    app = web.Application()
    app.router.add_get("/v1/dnt/bootstraps", bootstraps)
    runner = web.AppRunner(app)
    await runner.setup()
    port = _free_port()
    site = web.TCPSite(runner, "127.0.0.1", port)
    await site.start()

    try:
        request_url = f"http://127.0.0.1:{port}/v1/service/llm/v1/chat/completions"
        # The probe is sync httpx and would block the asyncio loop; run it in a thread.
        stats = await asyncio.to_thread(measure_net_rtt, request_url, n_samples=5)
        assert stats["n_ok"] == 5
        assert stats["probe_url"].endswith("/v1/dnt/bootstraps")
        assert stats["p50_ms"] >= 0.0
        assert stats["p95_ms"] >= stats["p50_ms"]
        assert stats["std_ms"] >= 0.0
    finally:
        await runner.cleanup()


def test_measure_net_rtt_unreachable_returns_zero_ok():
    # Bind+close to get an almost-certainly unused port, then probe it.
    port = _free_port()
    stats = measure_net_rtt(
        f"http://127.0.0.1:{port}/v1/chat/completions",
        n_samples=2,
        timeout_s=0.25,
    )
    assert stats["n_ok"] == 0
    assert stats["p50_ms"] != stats["p50_ms"]  # NaN
