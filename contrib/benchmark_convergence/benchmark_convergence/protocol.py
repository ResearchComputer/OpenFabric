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

    Uses readline() rather than read(N): BufferedReader.read(N) on a
    socket makefile blocks until N bytes OR EOF, which hangs on
    sub-N-byte messages (the case for our protocol). readline() returns
    as soon as it sees \\n or EOF.
    """
    while True:
        line = stream.readline()
        if not line:  # EOF
            break
        if not line.endswith(b"\n"):
            # Partial frame at EOF — discard.
            break
        line = line.strip()
        if not line:
            continue
        yield json.loads(line)
