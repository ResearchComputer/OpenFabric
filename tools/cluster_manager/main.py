#!/usr/bin/env python3
import os
import argparse
import yaml
import logging
from clusters import create_cluster

logging.basicConfig(level=logging.INFO, format='%(levelname)s: %(message)s')
logger = logging.getLogger(__name__)

def load_config(config_path: str):
    if not os.path.exists(config_path):
        logger.error(f"Configuration file not found: {config_path}")
        return None
    with open(config_path, 'r') as f:
        try:
            return yaml.safe_load(f)
        except yaml.YAMLError as e:
            logger.error(f"Error parsing YAML: {e}")
            return None

def main():
    parser = argparse.ArgumentParser(description="Cluster Manager for OpenTela")
    parser.add_argument("--config", default="config.yaml", help="Path to the configuration file")
    args = parser.parse_args()

    config = load_config(args.config)
    if not config:
        return

    clusters = {}

    # Initialize and connect clusters
    logger.info("Initializing clusters...")
    for cluster_conf in config.get("clusters", []):
        name = cluster_conf.get("name")
        if not name:
            logger.warning("Skipping cluster without a name.")
            continue

        try:
            cluster = create_cluster(name, cluster_conf)
            cluster.connect()
            clusters[name] = cluster
        except Exception as e:
            logger.error(f"Failed to initialize cluster '{name}': {e}")

    # Spin up services
    logger.info("Spinning up services...")
    for service_conf in config.get("services", []):
        name = service_conf.get("name")
        target_cluster = service_conf.get("cluster")
        command = service_conf.get("command")

        if not all([name, target_cluster, command]):
            logger.warning("Skipping invalid service configuration. Requires 'name', 'cluster', and 'command'.")
            continue

        if target_cluster not in clusters:
            logger.error(f"Target cluster '{target_cluster}' for service '{name}' is not available or failed to connect.")
            continue

        try:
            clusters[target_cluster].spin_up(name, command)
        except Exception as e:
            logger.error(f"Failed to start service '{name}': {e}")

    # Disconnect clusters
    logger.info("Disconnecting from clusters...")
    for cluster in clusters.values():
        try:
            cluster.disconnect()
        except Exception as e:
            logger.error(f"Failed to disconnect from cluster '{cluster.name}': {e}")

if __name__ == "__main__":
    main()
