import os
import json
import time
from pathlib import Path
import typer
from rich.console import Console
from otela_cli.core.config import load_config, save_config
from solders.keypair import Keypair
import base58
import base64

app = typer.Typer()
console = Console()

@app.command()
def login(keypair_path: str = typer.Option(None, "--keypair", "-k", help="Path to your Solana keypair JSON file")):
    """Authenticate against the OpenTela network using a local wallet."""
    config = load_config()

    path_to_use = keypair_path if keypair_path else config.get("wallet_path")
    wallet_file = Path(path_to_use).expanduser()

    if not wallet_file.exists():
        console.print(f"[red]Error: Wallet file not found at {wallet_file}[/red]")
        console.print("Please provide a valid keypair file, or generate one using: [bold]solana-keygen new[/bold]")
        raise typer.Exit(code=1)

    try:
        with open(wallet_file, "r") as f:
            keypair_data = json.load(f)
            keypair = Keypair.from_bytes(bytes(keypair_data))
    except Exception as e:
        console.print(f"[red]Failed to load keypair from {wallet_file}: {e}[/red]")
        raise typer.Exit(code=1)

    # In a real implementation, you'd send a signed attestation to the network
    # For now, we sign a login timestamp to prove ownership and store the session locally
    timestamp = int(time.time())
    payload = {
        "action": "cli_login",
        "wallet_pubkey": str(keypair.pubkey()),
        "timestamp": timestamp
    }

    payload_bytes = json.dumps(payload, separators=(',', ':')).encode('utf-8')
    signature = keypair.sign_message(payload_bytes)

    # Store session info locally
    config["wallet_path"] = str(wallet_file)
    config["session_pubkey"] = str(keypair.pubkey())
    save_config(config)

    console.print(f"[green]Successfully logged in to OpenTela network![/green]")
    console.print(f"Active Identity: [bold]{keypair.pubkey()}[/bold]")
    console.print(f"Signature generated: [dim]{str(signature)}[/dim]")

@app.command()
def logout():
    """Log out from the OpenTela network."""
    config = load_config()
    if config.get("session_pubkey"):
        config["session_pubkey"] = None
        save_config(config)
        console.print("[yellow]Logged out successfully.[/yellow]")
    else:
        console.print("[yellow]You are not currently logged in.[/yellow]")

@app.command()
def whoami():
    """Display current authentication status and active identity."""
    config = load_config()
    pubkey = config.get("session_pubkey")
    if pubkey:
        console.print(f"[green]Authenticated[/green]")
        console.print(f"Identity: [bold]{pubkey}[/bold]")
        console.print(f"Wallet Path: {config.get('wallet_path')}")
        console.print(f"Current RPC: {config.get('rpc_url')}")
        console.print(f"Default Cluster: {config.get('default_cluster')}")
    else:
        console.print("[red]Not authenticated. Please run 'otela-cli auth login'.[/red]")

@app.command()
def config_set(key: str, value: str):
    """Set local configuration."""
    config = load_config()
    config[key] = value
    save_config(config)
    console.print(f"[green]Set config[/green]: {key} = {value}")

@app.command()
def config_get(key: str):
    """Get local configuration."""
    config = load_config()
    if key in config:
        console.print(f"{key}: {config[key]}")
    else:
        console.print(f"[red]Configuration key '{key}' not found.[/red]")
