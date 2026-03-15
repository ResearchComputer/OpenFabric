"""
FastAPI proxy that exposes OpenAI-compatible endpoints on top of an OpenTela
head node's public /v1/services routing surface.
"""

from __future__ import annotations

import json
import os
from contextlib import asynccontextmanager
from dataclasses import dataclass
from typing import Any
from urllib.parse import quote

import httpx
import uvicorn
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse, Response, StreamingResponse


HOP_BY_HOP_HEADERS = {
    "connection",
    "content-length",
    "host",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailer",
    "transfer-encoding",
    "upgrade",
}


@dataclass(slots=True)
class Settings:
    opentela_base_url: str = "http://127.0.0.1:8092"
    llm_service: str = "llm"
    embedding_service: str = "embedding"
    rerank_service: str = "rerank"
    request_timeout_seconds: float = 900.0
    host: str = "0.0.0.0"
    port: int = 8091

    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            opentela_base_url=os.getenv("OTELA_BASE_URL", "http://127.0.0.1:8092").rstrip("/"),
            llm_service=os.getenv("OTELA_LLM_SERVICE", "llm"),
            embedding_service=os.getenv("OTELA_EMBEDDING_SERVICE", "embedding"),
            rerank_service=os.getenv("OTELA_RERANK_SERVICE", "rerank"),
            request_timeout_seconds=float(os.getenv("OTELA_PROXY_TIMEOUT_SECONDS", "900")),
            host=os.getenv("HOST", "0.0.0.0"),
            port=int(os.getenv("PORT", "8091")),
        )

    def model_services(self) -> list[str]:
        services: list[str] = []
        for service in (self.llm_service, self.embedding_service, self.rerank_service):
            if service and service not in services:
                services.append(service)
        return services


def _service_url(settings: Settings, service: str, path: str) -> str:
    return f"{settings.opentela_base_url}/v1/services/{quote(service, safe='')}{path}"


def _filter_request_headers(headers: httpx.Headers) -> dict[str, str]:
    return {
        key: value
        for key, value in headers.items()
        if key.lower() not in HOP_BY_HOP_HEADERS
    }


def _filter_response_headers(headers: httpx.Headers) -> dict[str, str]:
    return {
        key: value
        for key, value in headers.items()
        if key.lower() not in HOP_BY_HOP_HEADERS
    }


def _extract_model(payload: Any) -> str | None:
    if isinstance(payload, dict):
        model = payload.get("model")
        if isinstance(model, str) and model:
            return model
    return None


def _attach_identity_group(headers: dict[str, str], model: str | None) -> None:
    if model and "x-otela-identity-group" not in {key.lower() for key in headers}:
        headers["X-Otela-Identity-Group"] = f"model={model}"


def _extract_text_content(content: Any) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: list[str] = []
        for item in content:
            if not isinstance(item, dict):
                raise HTTPException(status_code=400, detail="Unsupported structured content item")
            item_type = item.get("type")
            if item_type != "text":
                raise HTTPException(status_code=400, detail=f"Unsupported content block type: {item_type}")
            text = item.get("text")
            if not isinstance(text, str):
                raise HTTPException(status_code=400, detail="Text content blocks must include a text field")
            parts.append(text)
        return "\n".join(parts)
    raise HTTPException(status_code=400, detail="Unsupported content format")


def _anthropic_to_openai_chat(payload: dict[str, Any]) -> dict[str, Any]:
    model = payload.get("model")
    if not isinstance(model, str) or not model:
        raise HTTPException(status_code=400, detail="Anthropic requests require a model")

    messages: list[dict[str, Any]] = []
    system = payload.get("system")
    if system:
        messages.append({"role": "system", "content": _extract_text_content(system)})

    for message in payload.get("messages", []):
        if not isinstance(message, dict):
            raise HTTPException(status_code=400, detail="Anthropic messages must be objects")
        role = message.get("role")
        if not isinstance(role, str) or not role:
            raise HTTPException(status_code=400, detail="Anthropic messages require a role")
        messages.append({"role": role, "content": _extract_text_content(message.get("content", ""))})

    translated: dict[str, Any] = {
        "model": model,
        "messages": messages,
    }
    passthrough_keys = {
        "metadata",
        "stop_sequences",
        "stream",
        "temperature",
        "top_p",
    }
    for key in passthrough_keys:
        if key in payload:
            translated[key] = payload[key]
    if "stop_sequences" in translated:
        translated["stop"] = translated.pop("stop_sequences")
    if "max_tokens" in payload:
        translated["max_tokens"] = payload["max_tokens"]
    return translated


def _openai_content_to_text(content: Any) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: list[str] = []
        for item in content:
            if not isinstance(item, dict):
                continue
            if item.get("type") == "text" and isinstance(item.get("text"), str):
                parts.append(item["text"])
        return "".join(parts)
    return ""


def _map_finish_reason(reason: Any) -> str | None:
    if reason == "stop":
        return "end_turn"
    if reason in {"length", "max_tokens"}:
        return "max_tokens"
    if reason in {"tool_calls", "function_call"}:
        return "tool_use"
    if isinstance(reason, str):
        return reason
    return None


def _openai_chat_to_anthropic(payload: dict[str, Any]) -> dict[str, Any]:
    choices = payload.get("choices") or []
    message = {}
    finish_reason = None
    if choices:
        choice = choices[0]
        if isinstance(choice, dict):
            message = choice.get("message") or {}
            finish_reason = choice.get("finish_reason")

    usage = payload.get("usage") or {}
    return {
        "id": payload.get("id", ""),
        "type": "message",
        "role": "assistant",
        "model": payload.get("model", ""),
        "content": [{"type": "text", "text": _openai_content_to_text(message.get("content", ""))}],
        "stop_reason": _map_finish_reason(finish_reason),
        "stop_sequence": None,
        "usage": {
            "input_tokens": usage.get("prompt_tokens", 0),
            "output_tokens": usage.get("completion_tokens", 0),
        },
    }


def _format_sse(event: str, payload: dict[str, Any]) -> bytes:
    return f"event: {event}\ndata: {json.dumps(payload)}\n\n".encode()


async def _iter_upstream_response(upstream: httpx.Response):
    try:
        async for chunk in upstream.aiter_raw():
            yield chunk
    finally:
        await upstream.aclose()


async def _iter_openai_as_anthropic(upstream: httpx.Response):
    sent_message_start = False
    sent_content_start = False
    sent_message_stop = False
    message_id = ""
    model = ""
    try:
        async for line in upstream.aiter_lines():
            if not line or not line.startswith("data:"):
                continue
            data = line[5:].strip()
            if data == "[DONE]":
                if sent_content_start:
                    yield _format_sse("content_block_stop", {"index": 0})
                if sent_message_start and not sent_message_stop:
                    yield _format_sse("message_stop", {})
                break

            chunk = json.loads(data)
            if not sent_message_start:
                message_id = chunk.get("id", "")
                model = chunk.get("model", "")
                yield _format_sse(
                    "message_start",
                    {
                        "type": "message_start",
                        "message": {
                            "id": message_id,
                            "type": "message",
                            "role": "assistant",
                            "model": model,
                            "content": [],
                            "stop_reason": None,
                            "stop_sequence": None,
                            "usage": {"input_tokens": 0, "output_tokens": 0},
                        },
                    },
                )
                sent_message_start = True

            choices = chunk.get("choices") or []
            if not choices:
                continue
            choice = choices[0]
            delta = choice.get("delta") or {}
            text = _openai_content_to_text(delta.get("content", ""))
            if text and not sent_content_start:
                yield _format_sse(
                    "content_block_start",
                    {"index": 0, "content_block": {"type": "text", "text": ""}},
                )
                sent_content_start = True
            if text:
                yield _format_sse(
                    "content_block_delta",
                    {"index": 0, "delta": {"type": "text_delta", "text": text}},
                )

            finish_reason = choice.get("finish_reason")
            if finish_reason is not None:
                if sent_content_start:
                    yield _format_sse("content_block_stop", {"index": 0})
                    sent_content_start = False
                yield _format_sse(
                    "message_delta",
                    {
                        "delta": {
                            "stop_reason": _map_finish_reason(finish_reason),
                            "stop_sequence": None,
                        },
                        "usage": {"output_tokens": 0},
                    },
                )
                yield _format_sse("message_stop", {})
                sent_message_stop = True
    finally:
        await upstream.aclose()


async def _send_upstream(
    request: Request,
    service: str,
    path: str,
    *,
    json_payload: dict[str, Any] | None = None,
) -> httpx.Response:
    client: httpx.AsyncClient = request.app.state.client
    settings: Settings = request.app.state.settings
    headers = _filter_request_headers(request.headers)
    body: bytes
    model: str | None = None
    if json_payload is None:
        body = await request.body()
        if body:
            try:
                model = _extract_model(json.loads(body))
            except ValueError:
                model = None
    else:
        body = json.dumps(json_payload).encode()
        headers["Content-Type"] = "application/json"
        model = _extract_model(json_payload)
    _attach_identity_group(headers, model)

    upstream_request = client.build_request(
        request.method,
        _service_url(settings, service, path),
        params=request.query_params,
        headers=headers,
        content=body,
    )
    return await client.send(upstream_request, stream=True)


async def _proxy_request(request: Request, service: str, path: str) -> Response:
    upstream = await _send_upstream(request, service, path)
    return StreamingResponse(
        _iter_upstream_response(upstream),
        status_code=upstream.status_code,
        headers=_filter_response_headers(upstream.headers),
    )


async def _proxy_models(request: Request) -> Response:
    client: httpx.AsyncClient = request.app.state.client
    settings: Settings = request.app.state.settings
    headers = _filter_request_headers(request.headers)
    models_by_id: dict[str, dict[str, Any]] = {}
    fallback_error: tuple[int, bytes, dict[str, str]] | None = None

    for service in settings.model_services():
        upstream_request = client.build_request(
            "GET",
            _service_url(settings, service, "/v1/models"),
            params=request.query_params,
            headers=headers,
        )
        response = await client.send(upstream_request)
        if response.status_code < 200 or response.status_code >= 300:
            if fallback_error is None:
                fallback_error = (
                    response.status_code,
                    response.content,
                    _filter_response_headers(response.headers),
                )
            continue
        try:
            payload = response.json()
        except ValueError:
            continue
        for model in payload.get("data", []):
            if isinstance(model, dict):
                model_id = model.get("id")
                if isinstance(model_id, str) and model_id and model_id not in models_by_id:
                    models_by_id[model_id] = model

    if not models_by_id:
        if fallback_error is not None:
            status_code, content, headers = fallback_error
            return Response(content=content, status_code=status_code, headers=headers)
        raise HTTPException(status_code=503, detail="No model metadata available from OpenTela services")

    return JSONResponse({"object": "list", "data": list(models_by_id.values())})


def create_app(
    settings: Settings | None = None,
    client: httpx.AsyncClient | None = None,
) -> FastAPI:
    settings = settings or Settings.from_env()

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        if client is not None:
            app.state.client = client
            app.state.client_owned = False
        else:
            timeout = httpx.Timeout(settings.request_timeout_seconds, connect=30.0)
            app.state.client = httpx.AsyncClient(timeout=timeout)
            app.state.client_owned = True
        yield
        if app.state.client_owned:
            await app.state.client.aclose()

    app = FastAPI(title="OpenTela Proxy", version="0.1.0", lifespan=lifespan)
    app.state.settings = settings

    @app.get("/health")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    @app.get("/v1/models")
    async def list_models(request: Request) -> Response:
        return await _proxy_models(request)

    @app.post("/v1/completions")
    async def completions(request: Request) -> Response:
        return await _proxy_request(request, settings.llm_service, "/v1/completions")

    @app.post("/v1/chat/completions")
    async def chat_completions(request: Request) -> Response:
        return await _proxy_request(request, settings.llm_service, "/v1/chat/completions")

    @app.post("/v1/responses")
    async def responses(request: Request) -> Response:
        return await _proxy_request(request, settings.llm_service, "/v1/responses")

    @app.post("/v1/embeddings")
    async def embeddings(request: Request) -> Response:
        return await _proxy_request(request, settings.embedding_service, "/v1/embeddings")

    @app.post("/v1/rerank")
    async def rerank(request: Request) -> Response:
        return await _proxy_request(request, settings.rerank_service, "/v1/rerank")

    @app.post("/v1/messages")
    async def anthropic_messages(request: Request) -> Response:
        payload = await request.json()
        translated_request = _anthropic_to_openai_chat(payload)
        upstream = await _send_upstream(
            request,
            settings.llm_service,
            "/v1/chat/completions",
            json_payload=translated_request,
        )
        if translated_request.get("stream"):
            if upstream.status_code < 200 or upstream.status_code >= 300:
                raw = await upstream.aread()
                await upstream.aclose()
                return Response(
                    content=raw,
                    status_code=upstream.status_code,
                    headers=_filter_response_headers(upstream.headers),
                )
            return StreamingResponse(
                _iter_openai_as_anthropic(upstream),
                status_code=upstream.status_code,
                headers={"Content-Type": "text/event-stream"},
            )

        raw = await upstream.aread()
        await upstream.aclose()
        if upstream.status_code < 200 or upstream.status_code >= 300:
            return Response(
                content=raw,
                status_code=upstream.status_code,
                headers=_filter_response_headers(upstream.headers),
            )
        return JSONResponse(_openai_chat_to_anthropic(json.loads(raw)))

    return app


app = create_app()


def main() -> None:
    settings = Settings.from_env()
    uvicorn.run("proxy.server:app", host=settings.host, port=settings.port)


if __name__ == "__main__":
    main()
