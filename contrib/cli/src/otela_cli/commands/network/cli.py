import typer
import httpx
from rich.console import Console
from rich.table import Table
from otela_cli.core.config import load_config

app = typer.Typer()
console = Console()

@app.command("list-clusters")
def list_clusters():
    """List available computational clusters in the network."""
    console.print("Listing clusters...")

@app.command("list-nodes")
def list_nodes():
    """Show individual nodes and their current status on the OpenTela network."""
    config = load_config()
    # Provide a default if they haven't updated config yet
    rpc_url = config.get("rpc_url", "http://140.238.223.116:8092").rstrip("/")
    if "api.opentela.network" in rpc_url:
        rpc_url = "http://140.238.223.116:8092"

    endpoint = f"{rpc_url}/v1/dnt/peers_status"

    with console.status(f"[bold green]Fetching nodes from {rpc_url}...[/bold green]"):
        try:
            response = httpx.get(endpoint, timeout=10.0)
            response.raise_for_status()
            data = response.json()
        except httpx.RequestError as e:
            console.print(f"[red]Failed to connect to the network RPC:[/red] {e}")
            raise typer.Exit(code=1)
        except Exception as e:
            console.print(f"[red]Error fetching nodes:[/red] {e}")
            raise typer.Exit(code=1)

    peers = data.get("peers", [])

    if not peers:
        console.print("[yellow]No nodes found on the network.[/yellow]")
        return

    table = Table(title="OpenTela Network Nodes")
    table.add_column("Node ID", style="cyan", no_wrap=True)
    table.add_column("Status", style="magenta")

    for peer in peers:
        peer_id = peer.get("id", "Unknown")
        status = peer.get("connectedness", "Unknown")

        # Colorize status
        if status.lower() == "connected":
            status_display = f"[green]{status}[/green]"
        elif status.lower() == "notconnected":
            status_display = f"[red]{status}[/red]"
        else:
            status_display = f"[yellow]{status}[/yellow]"

        table.add_row(peer_id, status_display)

    console.print(table)
    console.print(f"\nTotal Nodes: [bold]{len(peers)}[/bold]")

@app.command("list-models")
def list_models():
    """List available AI models hosted on the network."""
    console.print("Listing models...")

@app.command()
def info():
    """Show general network statistics and health."""
    config = load_config()
    rpc_url = config.get("rpc_url", "http://140.238.223.116:8092").rstrip("/")
    if "api.opentela.network" in rpc_url:
        rpc_url = "http://140.238.223.116:8092"

    console.print(f"Connecting to RPC: [cyan]{rpc_url}[/cyan]...")

    with console.status("[bold green]Fetching network info...[/bold green]"):
        try:
            # Check Health
            health_resp = httpx.get(f"{rpc_url}/v1/health", timeout=5.0)
            health_resp.raise_for_status()
            health_status = health_resp.json().get("status", "unknown")

            # Check DNT Stats
            stats_resp = httpx.get(f"{rpc_url}/v1/dnt/stats", timeout=5.0)
            stats_resp.raise_for_status()
            stats_data = stats_resp.json()

        except httpx.RequestError as e:
            console.print(f"[red]Failed to connect to the network RPC:[/red] {e}")
            raise typer.Exit(code=1)

    # Display Health
    health_color = "green" if health_status == "ok" else "red"

    table = Table(title="OpenTela Network Info", show_header=False)
    table.add_column("Metric", style="cyan", no_wrap=True)
    table.add_column("Value", style="magenta")

    table.add_row("RPC Endpoint", rpc_url)
    table.add_row("Head Node Health", f"[{health_color}]{health_status.upper()}[/{health_color}]")

    if stats_data:
        total_peers = stats_data.get("total_peers_known", 0)
        connected_peers = stats_data.get("connected_peers", 0)
        table.add_row("Total Known Peers", str(total_peers))
        table.add_row("Connected Peers", str(connected_peers))

    console.print(table)
