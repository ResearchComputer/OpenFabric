from unittest.mock import MagicMock, patch
import pytest

from fleet_manager.deploy import deploy, _sync_binary
from fleet_manager.cluster import Preset


def _mock_cfg():
    cfg = MagicMock()
    cfg.name = "test"
    cfg.binary_local_path = "/tmp/otela"
    cfg.binary_remote_path = "~/opentela/otela"
    cfg.container_runtime = "apptainer"
    cfg.container_edf_template = None
    cfg.container_edf_remote_path = None
    cfg.container_image = "sglang:latest"
    cfg.container_mounts = ["/a:/a"]
    cfg.container_env = {"HF_HOME": "/models"}
    cfg.container_env_from_host = []
    cfg.container_hf_cache = "/models"
    cfg.relay_multiaddr = "/ip4/1.2.3.4/tcp/18905/p2p/QmTest"
    cfg.relay_skip = False
    cfg.worker_seed = "200"
    cfg.worker_port = "8092"
    cfg.worker_service_port = "30000"
    cfg.require_signed_binary = False
    cfg.skip_verification = True
    cfg.relay_bootstrap = ["/ip4/1.2.3.4/tcp/43905/p2p/QmA"]
    cfg.presets = {
        "A100_4": Preset(
            partition="booster",
            account="my-account",
            time="04:00:00",
            gpus=4,
            nodes=1,
            cpus_per_task=48,
        ),
    }
    return cfg


@patch("fleet_manager.deploy._sync_binary")
@patch("fleet_manager.deploy.relay_ensure")
@patch("fleet_manager.deploy.worker_submit", return_value="12345")
@patch("fleet_manager.deploy.render_template", return_value="rendered")
def test_deploy_calls_all_steps(mock_render, mock_submit, mock_relay, mock_sync):
    conn = MagicMock()
    cfg = _mock_cfg()
    job_ids = deploy(conn, cfg, "A100_4", "sglang", "sglang serve Qwen/Qwen3-0.6B", replicas=1)
    assert job_ids == ["12345"]
    mock_sync.assert_called_once()
    mock_relay.assert_called_once()
    mock_submit.assert_called_once()


@patch("fleet_manager.deploy._sync_binary")
@patch("fleet_manager.deploy.relay_ensure")
@patch("fleet_manager.deploy.worker_submit", side_effect=["111", "222"])
@patch("fleet_manager.deploy.render_template", return_value="rendered")
def test_deploy_multiple_replicas(mock_render, mock_submit, mock_relay, mock_sync):
    conn = MagicMock()
    cfg = _mock_cfg()
    job_ids = deploy(conn, cfg, "A100_4", "sglang", "sglang serve Qwen/Qwen3-0.6B", replicas=2)
    assert job_ids == ["111", "222"]
    assert mock_submit.call_count == 2


@patch("fleet_manager.deploy._sync_binary")
@patch("fleet_manager.deploy.relay_ensure")
@patch("fleet_manager.deploy.worker_submit", return_value="12345")
@patch("fleet_manager.deploy.render_template", return_value="rendered")
def test_deploy_unknown_preset_raises(mock_render, mock_submit, mock_relay, mock_sync):
    conn = MagicMock()
    cfg = _mock_cfg()
    with pytest.raises(ValueError, match="Unknown preset"):
        deploy(conn, cfg, "NONEXISTENT", "sglang", "sglang serve Qwen/Qwen3-0.6B")


def test_sync_binary_skips_if_same_hash():
    conn = MagicMock()
    conn.run.return_value = ("abc123  /path/otela\n", "", 0)
    with patch("fleet_manager.deploy._local_sha256", return_value="abc123"):
        _sync_binary(conn, "/local/otela", "~/opentela/otela")
    conn.put.assert_not_called()


def test_sync_binary_transfers_if_different():
    conn = MagicMock()
    conn.run.return_value = ("different_hash  /path/otela\n", "", 0)
    with patch("fleet_manager.deploy._local_sha256", return_value="abc123"):
        _sync_binary(conn, "/local/otela", "~/opentela/otela")
    conn.put.assert_called_once()
