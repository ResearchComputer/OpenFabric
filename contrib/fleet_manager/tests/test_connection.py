from unittest.mock import patch, MagicMock
import pytest

from fleet_manager.cluster import ClusterConnection


def _mock_config(ssh_host="host1", ssh_host_any="host2"):
    cfg = MagicMock()
    cfg.ssh_host = ssh_host
    cfg.ssh_host_any = ssh_host_any
    return cfg


@patch("fleet_manager.cluster.paramiko")
def test_connection_run(mock_paramiko):
    mock_client = MagicMock()
    mock_paramiko.SSHClient.return_value = mock_client
    mock_paramiko.SSHConfig.return_value = MagicMock(lookup=MagicMock(return_value={}))
    stdin, stdout, stderr = MagicMock(), MagicMock(), MagicMock()
    stdout.read.return_value = b"hello\n"
    stderr.read.return_value = b""
    stdout.channel.recv_exit_status.return_value = 0
    mock_client.exec_command.return_value = (stdin, stdout, stderr)
    conn = ClusterConnection(_mock_config())
    out, err, code = conn.run("echo hello", target="relay")
    assert out == "hello\n"
    assert code == 0


@patch("fleet_manager.cluster.paramiko")
def test_connection_uses_relay_host(mock_paramiko):
    mock_client = MagicMock()
    mock_paramiko.SSHClient.return_value = mock_client
    mock_paramiko.SSHConfig.return_value = MagicMock(lookup=MagicMock(return_value={}))
    stdin, stdout, stderr = MagicMock(), MagicMock(), MagicMock()
    stdout.read.return_value = b""
    stderr.read.return_value = b""
    stdout.channel.recv_exit_status.return_value = 0
    mock_client.exec_command.return_value = (stdin, stdout, stderr)
    conn = ClusterConnection(_mock_config(ssh_host="relay-host", ssh_host_any="any-host"))
    conn.run("test", target="relay")
    args, kwargs = mock_client.connect.call_args
    assert args[0] == "relay-host"


@patch("fleet_manager.cluster.paramiko")
def test_connection_uses_slurm_host(mock_paramiko):
    mock_client = MagicMock()
    mock_paramiko.SSHClient.return_value = mock_client
    mock_paramiko.SSHConfig.return_value = MagicMock(lookup=MagicMock(return_value={}))
    stdin, stdout, stderr = MagicMock(), MagicMock(), MagicMock()
    stdout.read.return_value = b""
    stderr.read.return_value = b""
    stdout.channel.recv_exit_status.return_value = 0
    mock_client.exec_command.return_value = (stdin, stdout, stderr)
    conn = ClusterConnection(_mock_config(ssh_host="relay-host", ssh_host_any="any-host"))
    conn.run("sbatch job.sh", target="slurm")
    args, kwargs = mock_client.connect.call_args
    assert args[0] == "any-host"
