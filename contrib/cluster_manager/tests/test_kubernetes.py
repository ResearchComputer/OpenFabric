import pytest
import subprocess
from clusters.kubernetes import KubernetesCluster

def test_kubernetes_connect_success(mocker):
    # Mock subprocess.run to simulate a successful command
    mock_run = mocker.patch("subprocess.run")

    config = {"type": "kubernetes", "context": "minikube"}
    cluster = KubernetesCluster("test-k8s", config)

    cluster.connect()

    # Assert that subprocess.run was called with correct arguments
    mock_run.assert_called_once_with(
        ["kubectl", "cluster-info", "--context", "minikube"],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True
    )

def test_kubernetes_connect_failure(mocker):
    # Mock subprocess.run to raise an exception
    mock_run = mocker.patch("subprocess.run", side_effect=subprocess.CalledProcessError(1, ["kubectl"], stderr="error"))

    config = {"type": "kubernetes"}
    cluster = KubernetesCluster("test-k8s", config)

    with pytest.raises(subprocess.CalledProcessError):
        cluster.connect()

def test_kubernetes_spin_up_success(mocker):
    # Mock subprocess.run to simulate a successful command
    mock_run = mocker.patch("subprocess.run")
    # Add a mock object for return value
    mock_run.return_value = mocker.Mock(stdout="deployment created")

    config = {"type": "kubernetes", "kubeconfig": "~/.kube/config"}
    cluster = KubernetesCluster("test-k8s", config)

    cluster.spin_up("my-service", "kubectl apply -f deployment.yaml")

    # Assert kubectl command includes kubeconfig
    mock_run.assert_called_once_with(
        ["kubectl", "apply", "-f", "deployment.yaml", "--kubeconfig", "~/.kube/config"],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True
    )

def test_kubernetes_spin_up_failure(mocker):
    # Mock subprocess.run to raise an exception
    mock_run = mocker.patch("subprocess.run", side_effect=subprocess.CalledProcessError(1, ["kubectl"], stderr="error"))

    config = {"type": "kubernetes"}
    cluster = KubernetesCluster("test-k8s", config)

    with pytest.raises(subprocess.CalledProcessError):
        cluster.spin_up("my-service", "kubectl apply -f deployment.yaml")
