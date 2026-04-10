from unittest.mock import MagicMock
import pytest

from fleet_manager.cluster import Preset, ProxyChains
from fleet_manager.worker import worker_list, worker_cancel, select_template, build_exec_prefix, Job, _effective_proxychains


def _mock_conn():
    return MagicMock()


def test_worker_list_parses_squeue():
    conn = _mock_conn()
    conn.run.return_value = (
        "123456|opentela-sglang-a3f1b2c4|RUNNING|0:05:00|01:00:00|nid007663\n"
        "123457|opentela-vllm-b4e2d1f0|PENDING|0:00:00|04:00:00|\n",
        "", 0
    )
    jobs = worker_list(conn)
    assert len(jobs) == 2
    assert jobs[0].id == "123456"
    assert jobs[0].name == "opentela-sglang-a3f1b2c4"
    assert jobs[0].state == "RUNNING"
    assert jobs[1].state == "PENDING"


def test_worker_list_empty():
    conn = _mock_conn()
    conn.run.return_value = ("", "", 0)
    jobs = worker_list(conn)
    assert jobs == []


def test_worker_list_filters_non_opentela():
    conn = _mock_conn()
    conn.run.return_value = (
        "111|other-job|RUNNING|0:01:00|01:00:00|node1\n"
        "222|opentela-sglang-a3f1b2c4|RUNNING|0:05:00|01:00:00|node2\n",
        "", 0
    )
    jobs = worker_list(conn)
    assert len(jobs) == 1
    assert jobs[0].id == "222"


def test_worker_cancel_specific():
    conn = _mock_conn()
    conn.run.return_value = ("", "", 0)
    worker_cancel(conn, job_id="123456")
    conn.run.assert_called_once()
    cmd = conn.run.call_args[0][0]
    assert "scancel 123456" in cmd


def test_select_template_apptainer_single():
    assert select_template("apptainer", 1) == "apptainer_single.sh.j2"


def test_select_template_apptainer_multi():
    assert select_template("apptainer", 2) == "apptainer_multi.sh.j2"


def test_select_template_enroot_single():
    assert select_template("enroot", 1) == "enroot_single.sh.j2"


def test_build_exec_prefix_apptainer():
    cfg = MagicMock()
    cfg.container_runtime = "apptainer"
    cfg.container_apptainer_flags = ["--nv", "--containall"]
    cfg.container_mounts = ["/tmp:/tmp", "/scratch:/scratch"]
    cfg.container_sif_path = "~/sglang.sif"
    cfg.container_hf_cache = "/scratch/hf"
    cfg.container_env = {"NCCL_SOCKET_IFNAME": "ib0"}
    prefix = build_exec_prefix(cfg)
    assert "apptainer exec" in prefix
    assert "--nv" in prefix
    assert "--containall" in prefix
    assert "/tmp:/tmp" in prefix
    assert "/scratch:/scratch" in prefix
    assert "~/sglang.sif" in prefix


def test_build_exec_prefix_enroot():
    cfg = MagicMock()
    cfg.container_runtime = "enroot"
    cfg.container_edf_remote_path = "~/.edf/sglang.toml"
    prefix = build_exec_prefix(cfg)
    assert "srun" in prefix
    assert "~/.edf/sglang.toml" in prefix


def test_effective_proxychains_disabled_by_default():
    cfg = MagicMock()
    cfg.proxychains = ProxyChains()
    preset = Preset(partition="booster", account="a", time="1:00:00", gpus=4)
    result = _effective_proxychains(cfg, preset)
    assert result["enabled"] is False


def test_effective_proxychains_enabled_passthrough():
    cfg = MagicMock()
    cfg.proxychains = ProxyChains(
        enabled=True,
        ssh_key="~/.ssh/id_jsc",
        proxy_target="jureca",
        socks_port=1080,
        skip_partitions=["develbooster"],
    )
    preset = Preset(partition="booster", account="a", time="1:00:00", gpus=4)
    result = _effective_proxychains(cfg, preset)
    assert result["enabled"] is True
    assert result["ssh_key"] == "~/.ssh/id_jsc"
    assert result["proxy_target"] == "jureca"
    assert result["socks_port"] == 1080


def test_effective_proxychains_skipped_on_skip_partition():
    cfg = MagicMock()
    cfg.proxychains = ProxyChains(
        enabled=True,
        ssh_key="~/.ssh/id_jsc",
        proxy_target="jureca",
        skip_partitions=["develbooster"],
    )
    preset = Preset(partition="develbooster", account="a", time="1:00:00", gpus=4)
    result = _effective_proxychains(cfg, preset)
    assert result["enabled"] is False
