from network_profiler.bench_config import build_host_config, build_multiaddr
from network_profiler.model import Machine


def test_build_multiaddr_ipv4():
    m = Machine(name="clariden", address="10.0.0.1", rcc_host="clariden")
    assert build_multiaddr(m, libp2p_port=19091, peer_id="12D3KooWAAA") == \
        "/ip4/10.0.0.1/tcp/19091/p2p/12D3KooWAAA"


def test_build_multiaddr_dns_fallback():
    m = Machine(name="clariden", address="clariden.cscs.ch", rcc_host="clariden")
    assert build_multiaddr(m, libp2p_port=19091, peer_id="12D3KooWAAA") == \
        "/dns4/clariden.cscs.ch/tcp/19091/p2p/12D3KooWAAA"


def test_host_config_excludes_self_from_bootstrap():
    machines = [
        Machine(name="a", address="10.0.0.1", rcc_host="a"),
        Machine(name="b", address="10.0.0.2", rcc_host="b"),
        Machine(name="c", address="10.0.0.3", rcc_host="c"),
    ]
    peer_ids = {"a": "PIDA", "b": "PIDB", "c": "PIDC"}
    cfg = build_host_config(
        self_machine=machines[0],
        all_machines=machines,
        peer_ids=peer_ids,
        run_id="run123",
        http_port=19090,
        libp2p_port=19091,
    )
    assert cfg["port"] == "19090"
    assert cfg["tcpport"] == "19091"
    assert cfg["cleanslate"] is True
    bootstrap = cfg["bootstrap"]["static"]
    assert len(bootstrap) == 2
    assert any("PIDB" in addr for addr in bootstrap)
    assert any("PIDC" in addr for addr in bootstrap)
    assert all("PIDA" not in addr for addr in bootstrap)


def test_host_config_yaml_serialisable():
    import yaml
    machines = [Machine(name="a", address="10.0.0.1", rcc_host="a")]
    cfg = build_host_config(
        self_machine=machines[0],
        all_machines=machines,
        peer_ids={"a": "PIDA"},
        run_id="run123",
        http_port=19090,
        libp2p_port=19091,
    )
    out = yaml.safe_dump(cfg)
    assert "port: '19090'" in out or "port: \"19090\"" in out
