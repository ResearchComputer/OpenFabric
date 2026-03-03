from fastapi import FastAPI, Request, Depends, HTTPException, status
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import StreamingResponse, JSONResponse
from typing import AsyncGenerator, Optional
import asyncio
import os
import json

from . import models


app = FastAPI(title="OpenAI-like API (skeleton)")

# Optional: allow CORS for local dev
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

AVAILABLE_MODELS = ["gpt-fake-1", "gpt-fake-2", "echo"]

@app.get("/v1/models", response_model=models.ModelsListResponse )
async def list_models():
    return {"data": [{"id": m, "object": "model"} for m in AVAILABLE_MODELS], "object": "list"}


@app.post("/v1/echo")
async def echo_endpoint(request: Request):
    try:
        data = await request.json()
        return data
    except Exception:
        return {"error": "Invalid JSON payload"}


@app.post("/v1/completions", response_model=models.CompletionResponse)
async def create_completion(req: models.CompletionRequest):

    text = "I am just pretending to be a model, sorry!"
    choice = {
        "text": text,
        "index": 0,
        "logprobs": None,
        "finish_reason": "stop",
    }
    return {"id": "cmpl-fake-1", "object": "text_completion", "created": int(asyncio.get_event_loop().time()), "model": req.model, "choices": [choice], "usage": {"prompt_tokens": 0, "completion_tokens": len(text.split()), "total_tokens": len(text.split())}}


@app.post("/v1/chat/completions", response_model=models.ChatCompletionResponse)
async def create_chat_completion(req: models.ChatCompletionRequest):
    # Non-streaming chat completion (stub)
    resp_text = "I am just pretending to be a model, sorry!"
    message = {"role": "assistant", "content": resp_text}
    choice = {"index": 0, "message": message, "finish_reason": "stop"}
    return {"id": "chatcmpl-fake-1", "object": "chat.completion", "created": int(asyncio.get_event_loop().time()), "model": req.model, "choices": [choice], "usage": {"prompt_tokens": 0, "completion_tokens": len(resp_text.split()), "total_tokens": len(resp_text.split())}}


@app.post("/v1/chat/completions/stream")
async def stream_chat_completion(req: models.ChatCompletionRequest):
    """
    Streaming chat completion using Server-Sent Events (SSE).
    Clients should treat each line as a JSON fragment similar to OpenAI streaming.
    """

    async def event_generator() -> AsyncGenerator[bytes, None]:
        # This yields chunks as bytes. In a real integration you'd connect to your model streaming API.
        for chunk in "I am just pretending to be a model, sorry!".split(" "):
            payload = {"id": "chatcmpl-fake-1", "object": "chat.completion.chunk", "delta": {"role": "assistant", "content": chunk}}
            yield (f"data: {json.dumps(payload)}\n\n").encode("utf-8")
            await asyncio.sleep(0)  # allow event loop to run
        yield b"data: [DONE]\n\n"

    return StreamingResponse(event_generator(), media_type="text/event-stream")


@app.post("/v1/embeddings", response_model=models.EmbeddingResponse)
async def create_embedding(req: models.EmbeddingRequest):
    emb = []
    for text in req.input:
        s = sum(ord(c) for c in text) % 1000
        # make an 8-dim vector
        emb = [((s >> i) % 100) / 100.0 for i in range(8)]
        break
    return {"object": "list", "data": [{"object": "embedding", "embedding": emb, "index": 0}], "model": req.model}


# Basic health
@app.get("/health")
async def health():
    return {"status": "ok"}