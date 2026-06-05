from benchmark_overhead.parse import parse_server_timing


def test_canonical_head_only():
    s = "recv;dur=0.4, dnt;dur=0.3, peer_select;dur=0.05, p2p_to_worker_first_byte;dur=14.2"
    out = parse_server_timing(s)
    assert out == {
        "recv": 0.4,
        "dnt": 0.3,
        "peer_select": 0.05,
        "p2p_to_worker_first_byte": 14.2,
    }


def test_with_worker_stages():
    s = "recv;dur=0.4, dnt;dur=0.3, worker_local_proxy;dur=1.1, worker_sglang_ttft;dur=220.0"
    out = parse_server_timing(s)
    assert out["worker_local_proxy"] == 1.1
    assert out["worker_sglang_ttft"] == 220.0


def test_empty_returns_empty_dict():
    assert parse_server_timing("") == {}
    assert parse_server_timing(None) == {}


def test_malformed_entries_skipped():
    s = "recv;dur=0.4, garbage, dnt;dur=notanumber, peer_select;dur=0.05"
    out = parse_server_timing(s)
    assert out["recv"] == 0.4
    assert out["peer_select"] == 0.05
    assert "garbage" in out and out["garbage"] == 0.0
    # dnt with malformed dur is recorded as 0.0
    assert out["dnt"] == 0.0


def test_single_entry_no_trailing_comma():
    out = parse_server_timing("recv;dur=1.5")
    assert out == {"recv": 1.5}
