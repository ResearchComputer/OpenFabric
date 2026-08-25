import io

from benchmark_convergence.protocol import read_messages, write_message


def test_round_trip_multiple_messages() -> None:
    buf = io.BytesIO()
    write_message(buf, {"type": "hello", "task_id": 1})
    write_message(buf, {"type": "seen", "events": [{"name": "x", "seen_ns": 1}]})
    buf.seek(0)
    msgs = list(read_messages(buf))
    assert msgs == [
        {"type": "hello", "task_id": 1},
        {"type": "seen", "events": [{"name": "x", "seen_ns": 1}]},
    ]


def test_partial_frame_yields_nothing() -> None:
    buf = io.BytesIO(b'{"type":"hello",')  # no newline
    assert list(read_messages(buf)) == []
