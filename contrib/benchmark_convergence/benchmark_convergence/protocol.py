"""JSON-line framing for the coordinator/observer control channel."""
from __future__ import annotations

import json
from typing import IO, Any, Iterable


def write_message(stream: IO[bytes], msg: dict[str, Any]) -> None:
    line = (json.dumps(msg, separators=(",", ":")) + "\n").encode("utf-8")
    stream.write(line)
    if hasattr(stream, "flush"):
        stream.flush()


def read_messages(stream: IO[bytes]) -> Iterable[dict[str, Any]]:
    """Yield complete JSON objects, one per newline-terminated frame.

    Partial trailing data (no newline) is silently skipped.
    """
    buf = b""
    while True:
        chunk = stream.read(4096)
        if not chunk:
            break
        buf += chunk
        while b"\n" in buf:
            line, buf = buf.split(b"\n", 1)
            line = line.strip()
            if not line:
                continue
            yield json.loads(line)
