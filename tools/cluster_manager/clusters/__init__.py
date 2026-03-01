from .baremetal import BaremetalCluster
from .slurm import SlurmCluster
from .kubernetes import KubernetesCluster

def create_cluster(name: str, config: dict):
    cluster_type = config.get("type")
    if cluster_type == "baremetal":
        return BaremetalCluster(name, config)
    elif cluster_type == "slurm":
        return SlurmCluster(name, config)
    elif cluster_type == "kubernetes":
        return KubernetesCluster(name, config)
    else:
        raise ValueError(f"Unknown cluster type '{cluster_type}' for cluster '{name}'")
