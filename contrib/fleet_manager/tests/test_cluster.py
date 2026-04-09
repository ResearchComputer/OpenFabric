# tests/test_cluster.py
import pytest
import tempfile
from pathlib import Path

import yaml

from fleet_manager.cluster import ClusterConfig, Preset, load_cluster, list_clusters


MINIMAL_CLUSTER = {
    "name": "test-cluster",
    "ssh": {"host": "test-host"},
    "arch": "amd64",
    "binary": {"local_path": "./bin/otela", "remote_path": "~/opentela/otela"},
    "relay": {
        "seed": "1",
        "peer_id": "QmTest123",
        "host_ip": "10.0.0.1",
        "port": "18092",
        "tcp_port": "18905",
        "udp_port": "18820",
        "home_override": "/tmp/relay",
        "bootstrap": ["/ip4/1.2.3.4/tcp/43905/p2p/QmPeer1"],
    },
    "worker": {"seed": "2", "port": "8092", "service_port": "30000"},
    "container": {"runtime": "apptainer", "image": "sglang:latest", "sif_path": "~/sglang.sif"},
    "security": {"require_signed_binary": False},
    "solana": {"skip_verification": True},
    "presets": {
        "A100_4": {
            "partition": "booster",
            "account": "my-account",
            "time": "04:00:00",
            "gpus": 4,
            "nodes": 1,
            "cpus_per_task": 48,
        },
    },
}


def _write_yaml(dir_path, name, data):
    path = Path(dir_path) / f"{name}.yaml"
    path.write_text(yaml.dump(data))
    return path


def test_load_cluster_with_presets():
    with tempfile.TemporaryDirectory() as d:
        _write_yaml(d, "test-cluster", MINIMAL_CLUSTER)
        cfg = load_cluster("test-cluster", cluster_dir=d)
        assert cfg.name == "test-cluster"
        assert cfg.ssh_host == "test-host"
        assert cfg.ssh_host_any == "test-host"
        assert cfg.arch == "amd64"
        assert "A100_4" in cfg.presets
        preset = cfg.presets["A100_4"]
        assert preset.partition == "booster"
        assert preset.account == "my-account"
        assert preset.time == "04:00:00"
        assert preset.gpus == 4
        assert preset.nodes == 1
        assert preset.cpus_per_task == 48


def test_load_cluster_preset_defaults():
    data = {**MINIMAL_CLUSTER}
    data["presets"] = {
        "minimal": {
            "partition": "debug",
            "account": "test",
            "time": "00:30:00",
            "gpus": 1,
        },
    }
    with tempfile.TemporaryDirectory() as d:
        _write_yaml(d, "test-cluster", data)
        cfg = load_cluster("test-cluster", cluster_dir=d)
        preset = cfg.presets["minimal"]
        assert preset.nodes == 1
        assert preset.cpus_per_task is None
        assert preset.extra_sbatch == []


def test_load_cluster_invalid_arch():
    data = {**MINIMAL_CLUSTER, "arch": "mips"}
    with tempfile.TemporaryDirectory() as d:
        _write_yaml(d, "bad", data)
        with pytest.raises(ValueError, match="arch"):
            load_cluster("bad", cluster_dir=d)


def test_load_cluster_missing_presets():
    data = {**MINIMAL_CLUSTER}
    del data["presets"]
    with tempfile.TemporaryDirectory() as d:
        _write_yaml(d, "bad", data)
        with pytest.raises(ValueError, match="presets"):
            load_cluster("bad", cluster_dir=d)


def test_load_cluster_enroot_missing_edf():
    data = {**MINIMAL_CLUSTER}
    data["container"] = {"runtime": "enroot", "image": "img:latest"}
    with tempfile.TemporaryDirectory() as d:
        _write_yaml(d, "bad", data)
        with pytest.raises(ValueError, match="edf_template"):
            load_cluster("bad", cluster_dir=d)


def test_load_cluster_not_found():
    with tempfile.TemporaryDirectory() as d:
        with pytest.raises(FileNotFoundError):
            load_cluster("nonexistent", cluster_dir=d)


def test_list_clusters():
    with tempfile.TemporaryDirectory() as d:
        _write_yaml(d, "euler", MINIMAL_CLUSTER)
        _write_yaml(d, "clariden", {**MINIMAL_CLUSTER, "name": "clariden"})
        names = list_clusters(cluster_dir=d)
        assert sorted(names) == ["clariden", "euler"]


def test_relay_multiaddr():
    with tempfile.TemporaryDirectory() as d:
        _write_yaml(d, "test-cluster", MINIMAL_CLUSTER)
        cfg = load_cluster("test-cluster", cluster_dir=d)
        assert cfg.relay_multiaddr == "/ip4/10.0.0.1/tcp/18905/p2p/QmTest123"
