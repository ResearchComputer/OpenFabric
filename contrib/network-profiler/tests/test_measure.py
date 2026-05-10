from network_profiler.measure import parse_iperf, parse_ping


def test_parse_linux_ping_summary() -> None:
    output = "rtt min/avg/max/mdev = 1.234/5.678/9.012/0.321 ms"
    assert parse_ping(output) == {
        "min": 1.234,
        "avg": 5.678,
        "max": 9.012,
        "jitter": 0.321,
    }


def test_parse_iperf_sum_received() -> None:
    output = '{"end":{"sum_received":{"bits_per_second":123000000,"seconds":5,"bytes":1000}}}'
    assert parse_iperf(output) == {"mbps": 123.0, "seconds": 5.0, "bytes": 1000.0}
