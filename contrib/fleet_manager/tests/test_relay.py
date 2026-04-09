from unittest.mock import MagicMock, patch
import pytest

from fleet_manager.relay import relay_status, relay_ensure, RelayStatus


def _mock_conn_and_config():
    conn = MagicMock()
    cfg = MagicMock()
    cfg.name = "test"
    cfg.binary_remote_path = "~/opentela/otela"
    cfg.relay_config_remote_path = "~/opentela/relay.cfg.yaml"
    cfg.relay_port = "18092"
    cfg.relay_home_override = "/tmp/relay"
    cfg.relay_log_path = "~/opentela/relay.log"
    cfg.relay_seed = "99"
    cfg.relay_tcp_port = "18905"
    cfg.relay_udp_port = "18820"
    cfg.relay_bootstrap = ["/ip4/1.2.3.4/tcp/43905/p2p/QmA"]
    cfg.require_signed_binary = False
    cfg.skip_verification = True
    cfg.relay_skip = False
    return conn, cfg


def test_relay_status_running():
    conn, cfg = _mock_conn_and_config()
    conn.run.side_effect = [
        ("12345\n", "", 0),
        ('{"bootstraps":[]}', "", 0),
    ]
    status = relay_status(conn, cfg)
    assert status == RelayStatus.RUNNING


def test_relay_status_stopped():
    conn, cfg = _mock_conn_and_config()
    conn.run.side_effect = [
        ("", "", 1),
    ]
    status = relay_status(conn, cfg)
    assert status == RelayStatus.STOPPED


def test_relay_status_degraded():
    conn, cfg = _mock_conn_and_config()
    conn.run.side_effect = [
        ("12345\n", "", 0),
        ("", "Connection refused", 7),
    ]
    status = relay_status(conn, cfg)
    assert status == RelayStatus.DEGRADED


def test_relay_ensure_already_running():
    conn, cfg = _mock_conn_and_config()
    conn.run.side_effect = [
        ("12345\n", "", 0),
        ('{"bootstraps":[]}', "", 0),
    ]
    with patch("fleet_manager.relay.relay_start") as mock_start:
        relay_ensure(conn, cfg)
        mock_start.assert_not_called()


def test_relay_ensure_starts_if_stopped():
    conn, cfg = _mock_conn_and_config()
    conn.run.side_effect = [
        ("", "", 1),
    ]
    with patch("fleet_manager.relay.relay_start") as mock_start:
        relay_ensure(conn, cfg)
        mock_start.assert_called_once_with(conn, cfg)
