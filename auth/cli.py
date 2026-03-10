#!/usr/bin/env python3
"""
otela-auth: CLI for managing OpenTela API keys.

Usage:
    python -m auth.cli create --wallet <pubkey> --keypair <path> [--label <label>]
    python -m auth.cli list   --wallet <pubkey>
    python -m auth.cli revoke --wallet <pubkey> --key-id <id>
    python -m auth.cli verify --token <token>

The --keypair flag points to a Solana-CLI-format keypair file (JSON array of
64 ints). The CLI uses it to sign the ownership challenge automatically.
"""

import json
import sys
import time

import base58
import click
import httpx
from nacl.signing import SigningKey
from rich.console import Console
from rich.table import Table

DEFAULT_AUTH_URL = "http://localhost:8090"

console = Console()


def load_signing_key(keypair_path: str) -> tuple[SigningKey, str]:
    """Load a Solana-CLI keypair file and return (SigningKey, base58 pubkey)."""
    with open(keypair_path) as f:
        ints = json.load(f)
    if len(ints) != 64:
        console.print(f"[red]Invalid keypair: expected 64 bytes, got {len(ints)}[/red]")
        sys.exit(1)
    secret = bytes(ints)
    sk = SigningKey(secret[:32])
    pubkey = base58.b58encode(bytes(sk.verify_key)).decode()
    return sk, pubkey


@click.group()
@click.option(
    "--auth-url",
    envvar="OTELA_AUTH_URL",
    default=DEFAULT_AUTH_URL,
    help="Auth server URL (env: OTELA_AUTH_URL)",
)
@click.pass_context
def cli(ctx, auth_url: str):
    """OpenTela API key management."""
    ctx.ensure_object(dict)
    ctx.obj["auth_url"] = auth_url.rstrip("/")


@cli.command()
@click.option("--wallet", help="Wallet public key (base58). Auto-derived from keypair if omitted.")
@click.option("--keypair", required=True, type=click.Path(exists=True), help="Path to Solana keypair file")
@click.option("--label", default="", help="Human-readable label for this key")
@click.pass_context
def create(ctx, wallet: str | None, keypair: str, label: str):
    """Create a new API key bound to your wallet."""
    sk, derived_wallet = load_signing_key(keypair)
    if wallet and wallet != derived_wallet:
        console.print(f"[yellow]Warning: --wallet ({wallet}) differs from keypair pubkey ({derived_wallet}). Using keypair.[/yellow]")
    wallet = derived_wallet

    # Sign a challenge to prove wallet ownership.
    challenge = f"otela-auth:{wallet}:{int(time.time())}"
    signed = sk.sign(challenge.encode())
    signature = base58.b58encode(signed.signature).decode()

    url = f"{ctx.obj['auth_url']}/api/keys"
    resp = httpx.post(url, json={
        "wallet": wallet,
        "signature": signature,
        "challenge": challenge,
        "label": label,
    })

    if resp.status_code != 200:
        console.print(f"[red]Error {resp.status_code}: {resp.text}[/red]")
        sys.exit(1)

    data = resp.json()
    console.print()
    console.print("[green]API key created successfully![/green]")
    console.print()
    console.print(f"  Key ID:  {data['key_id']}")
    console.print(f"  Token:   [bold]{data['token']}[/bold]")
    console.print(f"  Wallet:  {data['wallet']}")
    if label:
        console.print(f"  Label:   {label}")
    console.print()
    console.print("[yellow]Save the token now — it cannot be retrieved later.[/yellow]")


@cli.command("list")
@click.option("--wallet", required=True, help="Wallet public key (base58)")
@click.pass_context
def list_keys(ctx, wallet: str):
    """List API keys for a wallet."""
    url = f"{ctx.obj['auth_url']}/api/keys"
    resp = httpx.get(url, params={"wallet": wallet})

    if resp.status_code != 200:
        console.print(f"[red]Error {resp.status_code}: {resp.text}[/red]")
        sys.exit(1)

    keys = resp.json()
    if not keys:
        console.print(f"No API keys found for wallet {wallet}")
        return

    table = Table(title=f"API Keys for {wallet[:16]}...")
    table.add_column("Key ID")
    table.add_column("Label")
    table.add_column("Created")
    table.add_column("Status")

    for k in keys:
        status = "[red]revoked[/red]" if k["revoked"] else "[green]active[/green]"
        table.add_row(k["key_id"], k["label"] or "-", k["created_at"], status)

    console.print(table)


@cli.command()
@click.option("--wallet", required=True, help="Wallet public key (base58)")
@click.option("--key-id", required=True, help="Key ID to revoke")
@click.pass_context
def revoke(ctx, wallet: str, key_id: str):
    """Revoke an API key."""
    url = f"{ctx.obj['auth_url']}/api/keys/{key_id}"
    resp = httpx.delete(url, params={"wallet": wallet})

    if resp.status_code != 200:
        console.print(f"[red]Error {resp.status_code}: {resp.text}[/red]")
        sys.exit(1)

    console.print(f"[green]Key {key_id} revoked.[/green]")


@cli.command()
@click.option("--token", required=True, help="Bearer token to verify")
@click.pass_context
def verify(ctx, token: str):
    """Verify a bearer token and show the associated wallet."""
    url = f"{ctx.obj['auth_url']}/api/keys/verify"
    resp = httpx.post(url, json={"token": token})

    if resp.status_code != 200:
        console.print(f"[red]Error {resp.status_code}: {resp.text}[/red]")
        sys.exit(1)

    data = resp.json()
    console.print(f"  Wallet:  {data['wallet']}")
    console.print(f"  Key ID:  {data['key_id']}")


if __name__ == "__main__":
    cli()
