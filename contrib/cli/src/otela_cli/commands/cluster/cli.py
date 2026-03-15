import os
import yaml
from pathlib import Path
import typer
from rich.console import Console
from otela_cli.clusters import create_cluster

app = typer.Typer()
console = Console()

def load_config(config_path: str):
    if not os.path.exists(config_path):
        console.print(f"[red]Error:[/red] Configuration file not found: {config_path}")
        raise typer.Exit(code=1)
    with open(config_path, 'r') as f:
        try:
            return yaml.safe_load(f)
        except yaml.YAMLError as e:
            console.print(f"[red]Error parsing YAML:[/red] {e}")
            raise typer.Exit(code=1)

@app.command()
def create(config_path: str = typer.Option("config.yaml", "--config", "-c", help="Path to the cluster configuration YAML")):
    """Provision a new cluster based on a configuration file."""
    if not create_cluster:
        console.print("[red]Error: Could not import OpenTela cluster manager modules.[/red]")
        raise typer.Exit(code=1)

    console.print(f"[bold blue]Loading cluster config from {config_path}[/bold blue]...")
    config = load_config(config_path)
    clusters = {}

    with console.status("[bold green]Initializing clusters...[/bold green]"):
        for cluster_conf in config.get("clusters", []):
            name = cluster_conf.get("name")
            if not name:
                console.print("[yellow]Warning: Skipping cluster without a name.[/yellow]")
                continue

            try:
                cluster = create_cluster(name, cluster_conf)
                cluster.connect()
                clusters[name] = cluster
                console.print(f"[green]✓[/green] Connected to cluster: [bold]{name}[/bold]")
            except Exception as e:
                console.print(f"[red]✗[/red] Failed to initialize cluster '{name}': {e}")

    if not clusters:
        console.print("[red]No clusters were successfully initialized.[/red]")
        return

    # Spin up services
    console.print("\n[bold blue]Spinning up services...[/bold blue]")
    for service_conf in config.get("services", []):
        name = service_conf.get("name")
        target_cluster = service_conf.get("cluster")
        command = service_conf.get("command")

        if not all([name, target_cluster, command]):
            console.print("[yellow]Warning: Skipping invalid service config (needs name, cluster, command).[/yellow]")
            continue

        if target_cluster not in clusters:
            console.print(f"[red]Error:[/red] Target cluster '{target_cluster}' for service '{name}' is not available.")
            continue

        try:
            console.print(f"  Starting service [bold cyan]{name}[/bold cyan] on {target_cluster}...")
            clusters[target_cluster].spin_up(name, command)
            console.print(f"  [green]✓[/green] Service {name} started")
        except Exception as e:
            console.print(f"  [red]✗[/red] Failed to start service '{name}': {e}")

    # Disconnect clusters
    console.print("\n[dim]Cleaning up connections...[/dim]")
    for cluster in clusters.values():
        try:
            cluster.disconnect()
        except Exception as e:
            console.print(f"[red]Failed to disconnect from '{cluster.name}': {e}[/red]")

    console.print("\n[bold green]Cluster creation process complete![/bold green]")

@app.command()
def scale():
    """Add or remove nodes from an existing cluster."""
    console.print("Scaling cluster... (Not yet implemented)")

@app.command()
def status():
    """Monitor the health and metrics of a specific cluster."""
    console.print("Cluster status... (Not yet implemented)")

@app.command()
def destroy():
    """Tear down an existing cluster."""
    console.print("Destroying cluster... (Not yet implemented)")
