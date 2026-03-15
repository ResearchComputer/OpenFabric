import asyncio
import json

import httpx
from fastapi.testclient import TestClient

from proxy.server import Settings, create_app


def _make_test_client(handler):
    upstream = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    app = create_app(
        settings=Settings(opentela_base_url="http://opentela.test"),
        client=upstream,
    )
    return TestClient(app), upstream


def test_models_aggregates_across_services():
    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/services/llm/v1/models":
            return httpx.Response(
                200,
                json={
                    "object": "list",
                    "data": [
                        {"id": "gpt-4o-mini", "object": "model", "owned_by": "llm"},
                    ],
                },
            )
        if request.url.path == "/v1/services/embedding/v1/models":
            return httpx.Response(
                200,
                json={
                    "object": "list",
                    "data": [
                        {"id": "bge-small", "object": "model", "owned_by": "embedding"},
                    ],
                },
            )
        if request.url.path == "/v1/services/rerank/v1/models":
            return httpx.Response(
                200,
                json={
                    "object": "list",
                    "data": [
                        {"id": "bge-reranker", "object": "model", "owned_by": "rerank"},
                        {"id": "gpt-4o-mini", "object": "model", "owned_by": "duplicate"},
                    ],
                },
            )
        return httpx.Response(404)

    client, upstream = _make_test_client(handler)
    try:
        response = client.get("/v1/models")
        assert response.status_code == 200
        payload = response.json()
        assert payload["object"] == "list"
        assert [model["id"] for model in payload["data"]] == [
            "gpt-4o-mini",
            "bge-small",
            "bge-reranker",
        ]
    finally:
        client.close()
        asyncio.run(upstream.aclose())


def test_chat_completions_proxy_targets_llm_service():
    captured = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["path"] = request.url.path
        captured["authorization"] = request.headers.get("authorization")
        captured["identity_group"] = request.headers.get("x-otela-identity-group")
        captured["body"] = json.loads(request.read())
        return httpx.Response(200, json={"id": "chatcmpl_1", "choices": []})

    client, upstream = _make_test_client(handler)
    try:
        response = client.post(
            "/v1/chat/completions",
            headers={"Authorization": "Bearer test-token"},
            json={"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "hi"}]},
        )
        assert response.status_code == 200
        assert captured["path"] == "/v1/services/llm/v1/chat/completions"
        assert captured["authorization"] == "Bearer test-token"
        assert captured["identity_group"] == "model=gpt-4o-mini"
        assert captured["body"]["model"] == "gpt-4o-mini"
    finally:
        client.close()
        asyncio.run(upstream.aclose())


def test_anthropic_messages_translate_to_chat_completions():
    captured = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["path"] = request.url.path
        captured["body"] = json.loads(request.read())
        return httpx.Response(
            200,
            json={
                "id": "chatcmpl_123",
                "model": "claude-sonnet",
                "choices": [
                    {
                        "message": {"role": "assistant", "content": "hello back"},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {"prompt_tokens": 11, "completion_tokens": 7},
            },
        )

    client, upstream = _make_test_client(handler)
    try:
        response = client.post(
            "/v1/messages",
            json={
                "model": "claude-sonnet",
                "system": "You are terse.",
                "messages": [
                    {"role": "user", "content": [{"type": "text", "text": "hello"}]},
                ],
                "max_tokens": 128,
            },
        )
        assert response.status_code == 200
        assert captured["path"] == "/v1/services/llm/v1/chat/completions"
        assert captured["body"] == {
            "model": "claude-sonnet",
            "messages": [
                {"role": "system", "content": "You are terse."},
                {"role": "user", "content": "hello"},
            ],
            "max_tokens": 128,
        }

        payload = response.json()
        assert payload["type"] == "message"
        assert payload["role"] == "assistant"
        assert payload["content"] == [{"type": "text", "text": "hello back"}]
        assert payload["stop_reason"] == "end_turn"
        assert payload["usage"] == {"input_tokens": 11, "output_tokens": 7}
    finally:
        client.close()
        asyncio.run(upstream.aclose())
