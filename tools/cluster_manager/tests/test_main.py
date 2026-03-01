import pytest
import yaml
from main import load_config, main

def test_load_config_success(tmp_path):
    config_file = tmp_path / "config.yaml"
    config_data = {"clusters": [{"name": "test"}], "services": []}
    config_file.write_text(yaml.dump(config_data))

    loaded_config = load_config(str(config_file))
    assert loaded_config == config_data

def test_load_config_not_found():
    assert load_config("nonexistent_file.yaml") is None

def test_load_config_invalid_yaml(tmp_path):
    config_file = tmp_path / "config.yaml"
    config_file.write_text("invalid: yaml: file: :")

    assert load_config(str(config_file)) is None

def test_main_execution(mocker, tmp_path):
    # Setup test config
    config_file = tmp_path / "config.yaml"
    config_data = {
        "clusters": [
            {"name": "cluster1", "type": "baremetal", "host": "localhost"}
        ],
        "services": [
            {"name": "service1", "cluster": "cluster1", "command": "echo 'test'"}
        ]
    }
    config_file.write_text(yaml.dump(config_data))

    # Mock sys.argv
    mocker.patch("sys.argv", ["main.py", "--config", str(config_file)])

    # Mock create_cluster
    mock_cluster = mocker.Mock()
    mock_cluster.name = "cluster1"
    mock_create_cluster = mocker.patch("main.create_cluster", return_value=mock_cluster)

    # Run main
    main()

    # Asserts
    mock_create_cluster.assert_called_once_with("cluster1", config_data["clusters"][0])
    mock_cluster.connect.assert_called_once()
    mock_cluster.spin_up.assert_called_once_with("service1", "echo 'test'")
    mock_cluster.disconnect.assert_called_once()
