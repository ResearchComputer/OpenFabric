from benchmark_overhead.doctor import diagnose_response, Diagnosis


def test_diagnose_response_all_good():
    diag = diagnose_response(
        head_reachable=True,
        server_timing_seen=True,
        sharegpt_present=True,
        slurm_reachable=True,
    )
    assert diag.ok is True
    assert diag.problems == []


def test_diagnose_response_missing_timing():
    diag = diagnose_response(
        head_reachable=True,
        server_timing_seen=False,
        sharegpt_present=True,
        slurm_reachable=True,
    )
    assert diag.ok is False
    assert any("Server-Timing" in p for p in diag.problems)


def test_diagnose_response_head_unreachable():
    diag = diagnose_response(
        head_reachable=False,
        server_timing_seen=False,
        sharegpt_present=True,
        slurm_reachable=True,
    )
    assert diag.ok is False
    assert any("head" in p.lower() for p in diag.problems)
