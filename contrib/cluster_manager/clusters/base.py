from abc import ABC, abstractmethod
from typing import Dict, Any

class Cluster(ABC):
    def __init__(self, name: str, config: Dict[str, Any]):
        self.name = name
        self.config = config

    @abstractmethod
    def connect(self):
        """Establish connection or verify access to the cluster."""
        pass

    @abstractmethod
    def spin_up(self, service_name: str, command: str):
        """Spin up a service on the cluster."""
        pass

    def disconnect(self):
        """Disconnect from the cluster if applicable."""
        pass
