#!/bin/bash
set -e

NUM_WORKERS=${1:-10}

echo "Building Docker images..."
docker compose build head-node
docker compose build worker

echo "Starting Simulation with 1 head node and $NUM_WORKERS workers..."
docker compose up --scale worker=$NUM_WORKERS -d

echo ""
echo "================================================================"
echo " Done! The simulation is running in the background."
echo " You can check the cluster status at http://localhost:8092/v1/dnt/table"
echo " To view logs: docker compose logs -f"
echo " To shut down: docker compose down"
echo "================================================================"
