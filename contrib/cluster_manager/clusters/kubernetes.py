import subprocess
import logging
from .base import Cluster

logger = logging.getLogger(__name__)

class KubernetesCluster(Cluster):
    def connect(self):
        context = self.config.get('context')
        kubeconfig = self.config.get('kubeconfig')

        logger.info(f"Verifying connection to Kubernetes cluster '{self.name}'...")
        cmd = ["kubectl", "cluster-info"]
        if context:
            cmd.extend(["--context", context])
        if kubeconfig:
            cmd.extend(["--kubeconfig", kubeconfig])

        try:
            subprocess.run(cmd, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            logger.info(f"Successfully connected to Kubernetes cluster '{self.name}'.")
        except subprocess.CalledProcessError as e:
            logger.error(f"Failed to verify Kubernetes cluster '{self.name}': {e.stderr}")
            raise

    def spin_up(self, service_name: str, command: str):
        context = self.config.get('context')
        kubeconfig = self.config.get('kubeconfig')

        logger.info(f"Spinning up service '{service_name}' on Kubernetes cluster '{self.name}'...")

        args = command.split()
        if args and args[0] == "kubectl":
            if context and "--context" not in args:
                args.extend(["--context", context])
            if kubeconfig and "--kubeconfig" not in args:
                args.extend(["--kubeconfig", kubeconfig])

        try:
            logger.debug(f"Executing: {' '.join(args)}")
            result = subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            logger.info(f"Service '{service_name}' output:\n{result.stdout}")
        except subprocess.CalledProcessError as e:
            logger.error(f"Failed to spin up '{service_name}': {e.stderr}")
            raise
