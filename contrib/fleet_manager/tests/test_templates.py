from fleet_manager.templates import render_template


def _apptainer_single_vars():
    return {
        "job_name": "opentela-sglang-a3f1b2c4",
        "partition": "booster",
        "account": "my-account",
        "time": "04:00:00",
        "gpus": 4,
        "nodes": 1,
        "cpus_per_task": 48,
        "extra_sbatch": ["#SBATCH --exclusive"],
        "log_dir": "~/logs",
        "binary_path": "~/opentela/otela",
        "worker_config": "~/.config/opentela/cfg.yaml",
        "hf_cache": "/tmp/hf_cache",
        "container_exec_prefix": 'apptainer exec \\\n    --nv \\\n    --bind "/tmp:/tmp" \\\n    "~/sglang.sif"',
        "sif_path": "~/sglang.sif",
        "container_image": "lmsysorg/sglang:latest",
        "pull_if_missing": True,
        "user_cmd": "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port $SERVICE_PORT --host 127.0.0.1",
        "service_port": "30000",
        "startup_timeout": 300,
        "modules": ["GCC", "CUDA/12"],
    }


def test_render_apptainer_single():
    result = render_template("apptainer_single.sh.j2", _apptainer_single_vars())
    assert "#SBATCH --job-name=opentela-sglang-a3f1b2c4" in result
    assert "#SBATCH --account=my-account" in result
    assert "#SBATCH --partition=booster" in result
    assert "#SBATCH --time=04:00:00" in result
    assert "#SBATCH --gpus-per-node=4" in result
    assert "#SBATCH --cpus-per-task=48" in result
    assert "module load GCC" in result
    assert "module load CUDA/12" in result
    assert "export SERVICE_PORT=30000" in result
    assert "export HF_HOME=" in result
    assert "apptainer exec" in result
    assert "sglang.launch_server" in result
    assert "wait -n" in result
    assert "otela" in result


def test_render_apptainer_single_no_optional():
    vars = _apptainer_single_vars()
    vars["account"] = None
    vars["partition"] = None
    vars["cpus_per_task"] = None
    vars["extra_sbatch"] = []
    vars["modules"] = []
    vars["pull_if_missing"] = False
    result = render_template("apptainer_single.sh.j2", vars)
    assert "#SBATCH --account" not in result
    assert "#SBATCH --partition" not in result
    assert "#SBATCH --cpus-per-task" not in result
    assert "module load" not in result
    assert "apptainer pull" not in result


def test_render_enroot_single():
    vars = _apptainer_single_vars()
    vars["container_exec_prefix"] = "srun --environment=~/.edf/sglang.toml"
    vars["edf_path"] = "~/.edf/sglang.toml"
    result = render_template("enroot_single.sh.j2", vars)
    assert "srun --environment" in result
    assert "sglang.launch_server" in result
    assert "wait -n" in result


def test_render_apptainer_multi():
    vars = _apptainer_single_vars()
    vars["nodes"] = 2
    vars["nccl_env"] = {"NCCL_SOCKET_IFNAME": "ib0"}
    vars["container_mounts"] = ["/tmp:/tmp"]
    vars["container_env"] = {"NCCL_SOCKET_IFNAME": "ib0"}
    vars["apptainer_flags"] = ["--nv"]
    result = render_template("apptainer_multi.sh.j2", vars)
    assert "#SBATCH --nodes=2" in result
    assert "MASTER_ADDR" in result
    assert "srun" in result
    assert "LAUNCHER" in result
    assert "wait -n" in result


def test_render_apptainer_single_with_proxychains():
    vars = _apptainer_single_vars()
    vars["proxychains"] = {
        "enabled": True,
        "ssh_key": "~/.ssh/id_ed25519_jsc",
        "proxy_target": "jureca05.fz-juelich.de",
        "socks_port": 1080,
    }
    result = render_template("apptainer_single.sh.j2", vars)
    assert "SSH SOCKS tunnel" in result
    assert "jureca05.fz-juelich.de" in result
    assert "~/.ssh/id_ed25519_jsc" in result
    assert 'HTTPS_PROXY="socks5h://127.0.0.1:$SOCKS_PORT"' in result
    assert "proxychains4 -q" in result
    assert 'kill "$TUNNEL_PID"' in result


def test_render_apptainer_single_without_proxychains():
    vars = _apptainer_single_vars()
    vars["proxychains"] = {"enabled": False}
    result = render_template("apptainer_single.sh.j2", vars)
    assert "SSH SOCKS tunnel" not in result
    assert "proxychains4 -q" not in result


def test_render_apptainer_multi_with_proxychains():
    vars = _apptainer_single_vars()
    vars["nodes"] = 2
    vars["nccl_env"] = {"NCCL_SOCKET_IFNAME": "ib0"}
    vars["container_mounts"] = ["/tmp:/tmp"]
    vars["container_env"] = {"NCCL_SOCKET_IFNAME": "ib0"}
    vars["apptainer_flags"] = ["--nv"]
    vars["proxychains"] = {
        "enabled": True,
        "ssh_key": "~/.ssh/id_ed25519_jsc",
        "proxy_target": "jureca05",
        "socks_port": 1080,
    }
    result = render_template("apptainer_multi.sh.j2", vars)
    assert "SSH SOCKS tunnel" in result
    assert "jureca05" in result


def test_render_worker_config():
    result = render_template("worker.cfg.yaml.j2", {
        "cluster_name": "test",
        "worker_seed": "100",
        "worker_port": "8092",
        "service_port": "30000",
        "require_signed_binary": False,
        "skip_verification": True,
        "bootstrap_sources": ["/ip4/1.2.3.4/tcp/18905/p2p/QmTest"],
    })
    assert "name: test-worker" in result
    assert 'seed: "100"' in result


def test_render_relay_config():
    result = render_template("relay.cfg.yaml.j2", {
        "cluster_name": "test",
        "relay_seed": "99",
        "relay_port": "18092",
        "relay_tcp_port": "18905",
        "relay_udp_port": "18820",
        "require_signed_binary": False,
        "skip_verification": True,
        "bootstrap_sources": ["/ip4/1.2.3.4/tcp/43905/p2p/QmA"],
    })
    assert "name: test-relay" in result
    assert 'seed: "99"' in result
