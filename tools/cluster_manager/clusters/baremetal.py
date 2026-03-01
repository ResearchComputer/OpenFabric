import logging
import paramiko
import os
from .base import Cluster

logger = logging.getLogger(__name__)

class BaremetalCluster(Cluster):
    def __init__(self, name: str, config: dict):
        super().__init__(name, config)
        self.ssh_client = paramiko.SSHClient()
        self.ssh_client.set_missing_host_key_policy(paramiko.AutoAddPolicy())

    def connect(self):
        host = self.config.get('host')

        if not host:
            raise ValueError(f"Baremetal cluster {self.name} missing 'host' config")

        # Parse ~/.ssh/config
        ssh_config = paramiko.SSHConfig()
        ssh_config_path = os.path.expanduser("~/.ssh/config")
        if os.path.exists(ssh_config_path):
            with open(ssh_config_path) as f:
                ssh_config.parse(f)

        host_config = ssh_config.lookup(host)

        resolved_host = host_config.get('hostname', host)
        resolved_user = self.config.get('user') or host_config.get('user')
        resolved_port = self.config.get('port') or int(host_config.get('port', 22))

        key_filename = self.config.get('key_filename')
        identity_file = host_config.get('identityfile')
        if not key_filename and identity_file:
            # Paramiko usually returns a list for IdentityFile
            if isinstance(identity_file, list):
                key_filename = os.path.expanduser(identity_file[0])
            else:
                key_filename = os.path.expanduser(identity_file)

        proxy_command = None
        proxy_cmd_str = host_config.get('proxycommand')
        if proxy_cmd_str:
            proxy_command = paramiko.ProxyCommand(proxy_cmd_str)

        proxy_jump_str = host_config.get('proxyjump')
        if proxy_jump_str and not proxy_command:
            # Simplified proxyjump using ssh
            proxy_command = paramiko.ProxyCommand(f"ssh -W %h:%p {proxy_jump_str}")

        logger.info(f"Connecting to baremetal cluster '{self.name}' at {resolved_host}:{resolved_port}...")
        try:
            self.ssh_client.connect(
                hostname=resolved_host,
                port=resolved_port,
                username=resolved_user,
                key_filename=key_filename,
                sock=proxy_command
            )
            logger.info(f"Successfully connected to '{self.name}'.")
        except Exception as e:
            logger.error(f"Failed to connect to '{self.name}': {e}")
            raise

    def spin_up(self, service_name: str, command: str):
        logger.info(f"Spinning up service '{service_name}' on baremetal cluster '{self.name}'...")
        try:
            logger.debug(f"Executing: {command}")
            stdin, stdout, stderr = self.ssh_client.exec_command(command)
            exit_status = stdout.channel.recv_exit_status()
            out = stdout.read().decode('utf-8')
            err = stderr.read().decode('utf-8')

            if exit_status == 0:
                logger.info(f"Service '{service_name}' output:\n{out}")
            else:
                logger.error(f"Service '{service_name}' failed with exit code {exit_status}:\n{err}")
                raise RuntimeError(f"Command execution failed: {err}")
        except Exception as e:
            logger.error(f"Failed to spin up '{service_name}': {e}")
            raise

    def disconnect(self):
        if self.ssh_client:
            self.ssh_client.close()
            logger.info(f"Disconnected from '{self.name}'.")
