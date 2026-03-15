import pytest
from clusters.baremetal import BaremetalCluster

def test_baremetal_missing_host():
    config = {"type": "baremetal"}
    cluster = BaremetalCluster("test-bm", config)

    with pytest.raises(ValueError, match="missing 'host' config"):
        cluster.connect()

def test_baremetal_connect_success(mocker):
    mock_paramiko_client = mocker.patch("clusters.baremetal.paramiko.SSHClient")
    mock_instance = mock_paramiko_client.return_value

    config = {"type": "baremetal", "host": "test-host", "user": "test-user", "port": 2222}
    cluster = BaremetalCluster("test-bm", config)

    cluster.connect()

    # Check that connect was called with appropriate arguments
    mock_instance.connect.assert_called_once()
    args, kwargs = mock_instance.connect.call_args
    assert kwargs.get("hostname") == "test-host"
    assert kwargs.get("port") == 2222
    assert kwargs.get("username") == "test-user"

def test_baremetal_connect_ssh_config(mocker):
    mock_paramiko_client = mocker.patch("clusters.baremetal.paramiko.SSHClient")
    mock_instance = mock_paramiko_client.return_value

    # Mock paramiko.SSHConfig
    mock_ssh_config_class = mocker.patch("clusters.baremetal.paramiko.SSHConfig")
    mock_ssh_config_instance = mock_ssh_config_class.return_value
    mock_ssh_config_instance.lookup.return_value = {
        "hostname": "resolved.host.com",
        "user": "ssh-user",
        "port": "2222",
        "identityfile": ["~/.ssh/my_key"]
    }

    mocker.patch("os.path.exists", return_value=True)
    mocker.patch("builtins.open", mocker.mock_open(read_data="Host my-alias\n  HostName resolved.host.com\n"))

    config = {"type": "baremetal", "host": "my-alias"}
    cluster = BaremetalCluster("test-bm", config)

    cluster.connect()

    mock_instance.connect.assert_called_once()
    args, kwargs = mock_instance.connect.call_args
    assert kwargs.get("hostname") == "resolved.host.com"
    assert kwargs.get("username") == "ssh-user"
    assert kwargs.get("port") == 2222

def test_baremetal_spin_up_success(mocker):
    mock_paramiko_client = mocker.patch("clusters.baremetal.paramiko.SSHClient")
    mock_instance = mock_paramiko_client.return_value

    # Mocking stdout, stderr for successful command execution
    mock_stdout = mocker.Mock()
    mock_stdout.channel.recv_exit_status.return_value = 0
    mock_stdout.read.return_value = b"success"

    mock_stderr = mocker.Mock()
    mock_stderr.read.return_value = b""

    mock_instance.exec_command.return_value = (mocker.Mock(), mock_stdout, mock_stderr)

    config = {"type": "baremetal", "host": "test-host"}
    cluster = BaremetalCluster("test-bm", config)

    # inject the mocked client
    cluster.ssh_client = mock_instance

    cluster.spin_up("my-service", "echo 'hello'")
    mock_instance.exec_command.assert_called_once_with("echo 'hello'")

def test_baremetal_spin_up_failure(mocker):
    mock_paramiko_client = mocker.patch("clusters.baremetal.paramiko.SSHClient")
    mock_instance = mock_paramiko_client.return_value

    # Mocking stdout, stderr for failed command execution
    mock_stdout = mocker.Mock()
    mock_stdout.channel.recv_exit_status.return_value = 1
    mock_stdout.read.return_value = b""

    mock_stderr = mocker.Mock()
    mock_stderr.read.return_value = b"error occurred"

    mock_instance.exec_command.return_value = (mocker.Mock(), mock_stdout, mock_stderr)

    config = {"type": "baremetal", "host": "test-host"}
    cluster = BaremetalCluster("test-bm", config)

    cluster.ssh_client = mock_instance

    with pytest.raises(RuntimeError, match="error occurred"):
        cluster.spin_up("my-service", "echo 'hello'")
