"""Parser for the Server-Timing / X-Otela-Worker-Timing header format."""

from __future__ import annotations


def parse_server_timing(header: str | None) -> dict[str, float]:
    """Parse a Server-Timing-style header into {stage_name: duration_ms}.

    Tolerates malformed entries: missing `dur=` or unparseable values record
    the stage name with 0.0. Empty or None input returns an empty dict.
    """
    if not header:
        return {}
    out: dict[str, float] = {}
    for raw in header.split(","):
        entry = raw.strip()
        if not entry:
            continue
        parts = entry.split(";", 1)
        name = parts[0].strip()
        ms = 0.0
        if len(parts) == 2:
            for piece in parts[1].split(";"):
                piece = piece.strip()
                if piece.startswith("dur="):
                    try:
                        ms = float(piece[len("dur="):])
                    except ValueError:
                        ms = 0.0
                    break
        out[name] = ms
    return out
