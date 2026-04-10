from __future__ import annotations

import hashlib
import os
import subprocess
import tempfile
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

import paramiko
import yaml


@dataclass
class Preset:
    partition: str
    account: str
    time: str
    gpus: int | str
    nodes: int = 1
    cpus_per_task: int | None = None
    extra_sbatch: list[str] = field(default_factory=list)


@dataclass
class ProxyChains:
    """SSH SOCKS tunnel config for compute nodes without direct internet.

    When enabled, the job script opens an SSH tunnel to `proxy_target` from
    inside the SLURM allocation and exports HTTP(S)_PROXY/ALL_PROXY pointing
    at the local SOCKS port. Commands can also be wrapped explicitly with
    `proxychains4 -q -f <conf>` for apps that ignore proxy env vars.

    Set `skip_partitions` for partitions that already have internet access
    (e.g. `develbooster`, `dc-gpu-devel` on JSC) so the tunnel is skipped.
    """

    enabled: bool = False
    ssh_key: str = ""
    proxy_target: str = ""
    socks_port: int = 1080
    skip_partitions: list[str] = field(default_factory=list)


@dataclass
class ClusterConfig:
    name: str
    ssh_host: str
    ssh_host_any: str
    arch: str
    binary_local_path: str
    binary_remote_path: str
    relay_seed: str
    relay_peer_id: str
    relay_host_ip: str
    relay_port: str
    relay_tcp_port: str
    relay_udp_port: str
    relay_home_override: str
    relay_bootstrap: list[str]
    worker_seed: str
    worker_port: str
    worker_service_port: str
    presets: dict[str, Preset]
    relay_skip: bool = False
    modules: list[str] = field(default_factory=list)
    container_runtime: str = "apptainer"
    container_image: str = ""
    container_edf_template: Optional[str] = None
    container_edf_remote_path: Optional[str] = None
    container_sif_path: Optional[str] = None
    container_pull_if_missing: bool = True
    container_hf_cache: str = ""
    container_mounts: list[str] = field(default_factory=list)
    container_env: dict[str, str] = field(default_factory=dict)
    container_env_from_host: list[str] = field(default_factory=list)
    container_apptainer_flags: list[str] = field(default_factory=list)
    proxychains: ProxyChains = field(default_factory=ProxyChains)
    require_signed_binary: bool = False
    skip_verification: bool = True

    @property
    def relay_multiaddr(self) -> str:
        return f"/ip4/{self.relay_host_ip}/tcp/{self.relay_tcp_port}/p2p/{self.relay_peer_id}"

    @property
    def relay_config_remote_path(self) -> str:
        return "~/opentela/relay.cfg.yaml"

    @property
    def relay_log_path(self) -> str:
        return "~/opentela/relay.log"


def _parse_presets(raw_presets: dict) -> dict[str, Preset]:
    presets = {}
    for name, values in raw_presets.items():
        presets[name] = Preset(
            partition=values["partition"],
            account=values["account"],
            time=values["time"],
            gpus=values["gpus"],
            nodes=values.get("nodes", 1),
            cpus_per_task=values.get("cpus_per_task"),
            extra_sbatch=values.get("extra_sbatch", []),
        )
    return presets


def _parse_proxychains(raw: dict | None) -> ProxyChains:
    if not raw:
        return ProxyChains()
    return ProxyChains(
        enabled=bool(raw.get("enabled", False)),
        ssh_key=raw.get("ssh_key", ""),
        proxy_target=raw.get("proxy_target", ""),
        socks_port=int(raw.get("socks_port", 1080)),
        skip_partitions=list(raw.get("skip_partitions", [])),
    )


def load_cluster(name: str, cluster_dir: str = "./clusters") -> ClusterConfig:
    path = Path(cluster_dir) / f"{name}.yaml"
    if not path.exists():
        raise FileNotFoundError(f"Cluster config not found: {path}")
    with open(path) as f:
        raw = yaml.safe_load(f)
    _validate_raw(raw, path)
    ssh = raw["ssh"]
    relay = raw["relay"]
    worker = raw["worker"]
    container = raw.get("container", {})
    return ClusterConfig(
        name=raw["name"],
        ssh_host=ssh["host"],
        ssh_host_any=ssh.get("host_any", ssh["host"]),
        arch=raw["arch"],
        binary_local_path=raw["binary"]["local_path"],
        binary_remote_path=raw["binary"]["remote_path"],
        relay_seed=relay["seed"],
        relay_peer_id=relay["peer_id"],
        relay_host_ip=relay["host_ip"],
        relay_port=relay["port"],
        relay_tcp_port=relay["tcp_port"],
        relay_udp_port=relay["udp_port"],
        relay_home_override=relay["home_override"],
        relay_bootstrap=relay["bootstrap"],
        relay_skip=relay.get("skip", False),
        worker_seed=worker["seed"],
        worker_port=worker["port"],
        worker_service_port=worker["service_port"],
        presets=_parse_presets(raw["presets"]),
        modules=raw.get("modules", []),
        container_runtime=container.get("runtime", "apptainer"),
        container_image=container.get("image", ""),
        container_edf_template=container.get("edf_template"),
        container_edf_remote_path=container.get("edf_remote_path"),
        container_sif_path=container.get("sif_path"),
        container_pull_if_missing=container.get("pull_if_missing", True),
        container_hf_cache=container.get("hf_cache", ""),
        container_mounts=container.get("mounts", []),
        container_env=container.get("env", {}),
        container_env_from_host=container.get("env_from_host", []),
        container_apptainer_flags=container.get("apptainer_flags", []),
        proxychains=_parse_proxychains(raw.get("proxychains")),
        require_signed_binary=raw.get("security", {}).get("require_signed_binary", False),
        skip_verification=raw.get("solana", {}).get("skip_verification", True),
    )


def _validate_raw(raw: dict, path: Path) -> None:
    for key in ("name", "ssh", "arch", "binary", "relay", "worker", "presets"):
        if key not in raw:
            raise ValueError(f"{path}: missing required field '{key}'")
    if raw["arch"] not in ("amd64", "arm64"):
        raise ValueError(f"{path}: arch must be 'amd64' or 'arm64', got '{raw['arch']}'")
    for key in ("host",):
        if key not in raw["ssh"]:
            raise ValueError(f"{path}: missing required field 'ssh.{key}'")
    for key in ("local_path", "remote_path"):
        if key not in raw["binary"]:
            raise ValueError(f"{path}: missing required field 'binary.{key}'")
    for key in ("seed", "peer_id", "host_ip", "port", "tcp_port", "udp_port", "home_override", "bootstrap"):
        if key not in raw["relay"]:
            raise ValueError(f"{path}: missing required field 'relay.{key}'")
    for key in ("seed", "port", "service_port"):
        if key not in raw["worker"]:
            raise ValueError(f"{path}: missing required field 'worker.{key}'")
    if "container" not in raw or "runtime" not in raw.get("container", {}):
        raise ValueError(f"{path}: missing required field 'container.runtime'")
    if "image" not in raw.get("container", {}):
        raise ValueError(f"{path}: missing required field 'container.image'")
    container = raw["container"]
    runtime = container["runtime"]
    if runtime == "enroot":
        if not container.get("edf_template"):
            raise ValueError(f"{path}: enroot runtime requires 'container.edf_template'")
        if not container.get("edf_remote_path"):
            raise ValueError(f"{path}: enroot runtime requires 'container.edf_remote_path'")
    elif runtime == "apptainer":
        if not container.get("sif_path"):
            raise ValueError(f"{path}: apptainer runtime requires 'container.sif_path'")

    pc = raw.get("proxychains")
    if pc and pc.get("enabled"):
        for key in ("ssh_key", "proxy_target"):
            if not pc.get(key):
                raise ValueError(
                    f"{path}: proxychains.enabled requires 'proxychains.{key}'"
                )


def list_clusters(cluster_dir: str = "./clusters") -> list[str]:
    d = Path(cluster_dir)
    if not d.is_dir():
        return []
    return sorted(p.stem for p in d.glob("*.yaml"))


def job_identity(backend: str, cmd: str, preset_name: str) -> str:
    h = hashlib.sha256(f"{cmd}{preset_name}".encode()).hexdigest()[:8]
    return f"opentela-{backend}-{h}"


def _has_control_master(host: str) -> bool:
    result = subprocess.run(
        ["ssh", "-O", "check", host],
        capture_output=True, text=True, timeout=5,
    )
    return result.returncode == 0


class ClusterConnection:
    def __init__(self, config: ClusterConfig):
        self.config = config
        self._clients: dict[str, paramiko.SSHClient] = {}
        self._use_subprocess: dict[str, bool] = {}

    def _should_use_subprocess(self, host: str) -> bool:
        if host not in self._use_subprocess:
            self._use_subprocess[host] = _has_control_master(host)
        return self._use_subprocess[host]

    def _get_client(self, target: str) -> paramiko.SSHClient:
        if target in self._clients:
            return self._clients[target]
        host = self.config.ssh_host if target == "relay" else self.config.ssh_host_any
        ssh_config = paramiko.SSHConfig()
        ssh_config_path = Path.home() / ".ssh" / "config"
        if ssh_config_path.exists():
            with open(ssh_config_path) as f:
                ssh_config.parse(f)
        host_cfg = ssh_config.lookup(host)
        actual_host = host_cfg.get("hostname", host)
        username = host_cfg.get("user", os.environ.get("USER"))
        sock = None
        if "proxycommand" in host_cfg:
            sock = paramiko.ProxyCommand(host_cfg["proxycommand"])
        elif "proxyjump" in host_cfg:
            proxy_host = host_cfg["proxyjump"]
            proxy_cfg = ssh_config.lookup(proxy_host)
            proxy_hostname = proxy_cfg.get("hostname", proxy_host)
            proxy_user = proxy_cfg.get("user", username)
            sock = paramiko.ProxyCommand(
                f"ssh -W {actual_host}:22 {proxy_user}@{proxy_hostname}"
            )
        client = paramiko.SSHClient()
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        connect_kwargs = {"username": username, "allow_agent": True}
        if sock:
            connect_kwargs["sock"] = sock
        for attempt in range(2):
            try:
                client.connect(actual_host, **connect_kwargs)
                break
            except Exception:
                if attempt == 0:
                    time.sleep(3)
                else:
                    raise
        self._clients[target] = client
        return client

    def _get_host(self, target: str) -> str:
        return self.config.ssh_host if target == "relay" else self.config.ssh_host_any

    def run(self, cmd: str, target: str = "relay") -> tuple[str, str, int]:
        host = self._get_host(target)
        if self._should_use_subprocess(host):
            result = subprocess.run(
                ["ssh", host, cmd],
                capture_output=True, text=True, timeout=120,
            )
            return result.stdout, result.stderr, result.returncode
        client = self._get_client(target)
        stdin, stdout, stderr = client.exec_command(cmd)
        out = stdout.read().decode()
        err = stderr.read().decode()
        code = stdout.channel.recv_exit_status()
        return out, err, code

    def _resolve_remote_path(self, remote_path: str, target: str) -> str:
        if "~" in remote_path:
            out, _, _ = self.run("echo $HOME", target=target)
            home = out.strip()
            return remote_path.replace("~", home)
        return remote_path

    def put(self, local_path: str, remote_path: str, target: str = "relay") -> None:
        host = self._get_host(target)
        if self._should_use_subprocess(host):
            resolved = self._resolve_remote_path(remote_path, target)
            subprocess.run(
                ["scp", "-q", local_path, f"{host}:{resolved}"],
                check=True, timeout=300,
            )
            return
        client = self._get_client(target)
        resolved = self._resolve_remote_path(remote_path, target)
        sftp = client.open_sftp()
        try:
            sftp.put(local_path, resolved)
        finally:
            sftp.close()

    def put_string(self, content: str, remote_path: str, target: str = "relay") -> None:
        host = self._get_host(target)
        if self._should_use_subprocess(host):
            resolved = self._resolve_remote_path(remote_path, target)
            with tempfile.NamedTemporaryFile(mode="w", suffix=".tmp", delete=False) as f:
                f.write(content)
                tmp_path = f.name
            try:
                subprocess.run(
                    ["scp", "-q", tmp_path, f"{host}:{resolved}"],
                    check=True, timeout=60,
                )
            finally:
                os.unlink(tmp_path)
            return
        client = self._get_client(target)
        resolved = self._resolve_remote_path(remote_path, target)
        sftp = client.open_sftp()
        try:
            with sftp.file(resolved, "w") as f:
                f.write(content)
        finally:
            sftp.close()

    def close(self) -> None:
        for client in self._clients.values():
            client.close()
        self._clients.clear()
