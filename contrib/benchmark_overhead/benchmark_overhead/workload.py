"""ShareGPT prompt loader and open-loop Poisson schedule generator."""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path

import numpy as np

# Heuristic char->token ratio. Sufficient for filtering; the actual token
# count comes from the model's tokenizer at request time (we don't tokenize
# in the bench client to keep dependencies minimal).
CHARS_PER_TOKEN = 4


@dataclass(frozen=True)
class Prompt:
    """A single bench request's input and output budget."""

    id: str
    prompt: str
    max_output_tokens: int


def load_sharegpt(
    path: str | Path,
    *,
    max_input_tokens: int,
    max_output_tokens: int,
) -> list[dict]:
    """Read a ShareGPT JSON file and return prompts that fit the input budget.

    Each returned dict has keys: id, prompt, max_output_tokens.
    """
    p = Path(path).expanduser()
    if not p.exists():
        raise FileNotFoundError(p)

    with p.open() as f:
        data = json.load(f)

    out: list[dict] = []
    char_budget = max_input_tokens * CHARS_PER_TOKEN
    for entry in data:
        convs = entry.get("conversations") or []
        if not convs:
            continue
        first = convs[0]
        if first.get("from") != "human":
            continue
        text = first.get("value", "")
        if not text or len(text) > char_budget:
            continue
        out.append(
            {
                "id": str(entry.get("id", len(out))),
                "prompt": text,
                "max_output_tokens": max_output_tokens,
            }
        )
    return out


def poisson_schedule(
    *,
    rps: float,
    duration_s: float,
    rng: np.random.Generator,
) -> list[float]:
    """Generate a sorted list of absolute arrival times in [0, duration_s).

    Uses exponential interarrivals with mean 1/rps. Returns [] if rps<=0.
    """
    if rps <= 0:
        return []
    arrivals: list[float] = []
    t = 0.0
    while True:
        gap = rng.exponential(1.0 / rps)
        t += gap
        if t >= duration_s:
            break
        arrivals.append(t)
    return arrivals
