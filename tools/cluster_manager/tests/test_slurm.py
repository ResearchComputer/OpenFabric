import pytest
from clusters.slurm import SlurmCluster

def test_slurm_missing_host():
    config = {"type": "slurm"}
    cluster = SlurmCluster("test-slurm", config)

    with pytest.raises(ValueError, match="missing 'host' config"):
        cluster.connect()

def test_slurm_connect_success(mocker):
    mock_paramiko_client = mocker.patch("clusters.slurm.paramiko.SSHClient")
    mock_instance = mock_paramiko_client.return_value

    # Mock sinfo success
    mock_stdout = mocker.Mock()
    mock_stdout.channel.recv_exit_status.return_value = 0
    mock_stderr = mocker.Mock()
    mock_instance.exec_command.return_value = (mocker.Mock(), mock_stdout, mock_stderr)

    config = {"type": "slurm", "host": "test-host", "user": "test-user"}
    cluster = SlurmCluster("test-slurm", config)

    cluster.connect()

    mock_instance.connect.assert_called_once()
    mock_instance.exec_command.assert_called_once_with("sinfo --version")

def test_slurm_connect_failure(mocker):
    mock_paramiko_client = mocker.patch("clusters.slurm.paramiko.SSHClient")
    mock_instance = mock_paramiko_client.return_value

    # Mock sinfo failure
    mock_stdout = mocker.Mock()
    mock_stdout.channel.recv_exit_status.return_value = 1
    mock_stderr = mocker.Mock()
    mock_stderr.read.return_value = b"command not found"
    mock_instance.exec_command.return_value = (mocker.Mock(), mock_stdout, mock_stderr)

    config = {"type": "slurm", "host": "test-host"}
    cluster = SlurmCluster("test-slurm", config)

    with pytest.raises(RuntimeError, match="Slurm verification failed: command not found"):
        cluster.connect()

def test_slurm_spin_up_success(mocker):
    mock_paramiko_client = mocker.patch("clusters.slurm.paramiko.SSHClient")
    mock_instance = mock_paramiko_client.return_value

    mock_stdout = mocker.Mock()
    mock_stdout.channel.recv_exit_status.return_value = 0
    mock_stdout.read.return_value = b"Submitted batch job 12345\n"

    mock_stderr = mocker.Mock()
    mock_stderr.read.return_value = b""

    mock_instance.exec_command.return_value = (mocker.Mock(), mock_stdout, mock_stderr)

    config = {"type": "slurm", "host": "test-host"}
    cluster = SlurmCluster("test-slurm", config)
    cluster.ssh_client = mock_instance

    cluster.spin_up("my-job", "sbatch job.sh")
    mock_instance.exec_command.assert_called_once_with("sbatch job.sh")
