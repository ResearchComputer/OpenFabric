import json
from pathlib import Path
import typer
from rich.console import Console
from rich.table import Table
from otela_cli.core.config import load_config
from solana.rpc.api import Client
from solders.pubkey import Pubkey
from solders.keypair import Keypair
from solders.message import MessageV0
from solders.transaction import VersionedTransaction
from spl.token.instructions import (
    transfer_checked,
    TransferCheckedParams,
    get_associated_token_address
)

app = typer.Typer()
console = Console()

def get_solana_client(config):
    rpc_url = config.get("solana_rpc_url", "https://api.devnet.solana.com")
    return Client(rpc_url), rpc_url

def ensure_authenticated(config):
    pubkey_str = config.get("session_pubkey")
    if not pubkey_str:
        console.print("[red]Error: Not authenticated. Please run 'otela-cli auth login' first.[/red]")
        raise typer.Exit(code=1)
    return Pubkey.from_string(pubkey_str)

def get_keypair(config):
    wallet_path = config.get("wallet_path")
    if not wallet_path or not Path(wallet_path).exists():
        console.print("[red]Error: Wallet file not found.[/red]")
        raise typer.Exit(code=1)
    try:
        with open(wallet_path, "r") as f:
            return Keypair.from_bytes(bytes(json.load(f)))
    except Exception as e:
        console.print(f"[red]Error loading wallet: {e}[/red]")
        raise typer.Exit(code=1)

@app.command()
def balance():
    """Check OpenTela token and SOL balances for the active wallet."""
    config = load_config()
    client, rpc_url = get_solana_client(config)
    pubkey = ensure_authenticated(config)
    mint_str = config.get("opentela_token_mint", "xTRCFBHAfjepfKNStvWQ7xmHwFS7aJ85oufa1BoXedL")
    mint = Pubkey.from_string(mint_str)

    console.print(f"[dim]Checking balances on {rpc_url}...[/dim]")

    with console.status("[bold green]Fetching balances from Solana...[/bold green]"):
        try:
            sol_resp = client.get_balance(pubkey)
            sol_balance = sol_resp.value / 1_000_000_000

            ata = get_associated_token_address(pubkey, mint)
            token_resp = client.get_token_account_balance(ata)

            if "error" in str(token_resp) or not token_resp.value:
                otela_balance = 0.0
            else:
                otela_balance = token_resp.value.ui_amount

        except Exception as e:
            if "could not find account" in str(e).lower() or "Invalid param" in str(e):
                otela_balance = 0.0
            else:
                console.print(f"[red]Error fetching balance from Solana RPC:[/red] {e}")
                raise typer.Exit(code=1)

    table = Table(title=f"Wallet Balances: {str(pubkey)[:8]}...")
    table.add_column("Asset", style="cyan", no_wrap=True)
    table.add_column("Balance", style="magenta")
    table.add_row("SOL", f"{sol_balance:.4f}")
    table.add_row("OpenTela Token (TELA)", f"{otela_balance:.2f}")

    console.print(table)
    console.print(f"\n[dim]Token Mint: {mint_str}[/dim]")

@app.command()
def transfer(
    recipient: str = typer.Argument(..., help="The Solana public key of the recipient"),
    amount: float = typer.Argument(..., help="The amount of OpenTela tokens to transfer")
):
    """Send OpenTela tokens to another address."""
    config = load_config()
    client, rpc_url = get_solana_client(config)
    sender_keypair = get_keypair(config)
    sender_pubkey = sender_keypair.pubkey()

    try:
        recipient_pubkey = Pubkey.from_string(recipient)
    except Exception:
        console.print(f"[red]Error: Invalid recipient address '{recipient}'.[/red]")
        raise typer.Exit(code=1)

    mint_str = config.get("opentela_token_mint", "xTRCFBHAfjepfKNStvWQ7xmHwFS7aJ85oufa1BoXedL")
    mint = Pubkey.from_string(mint_str)
    decimals = 9 # Assuming 9 decimals for standard SPL token
    raw_amount = int(amount * (10 ** decimals))

    console.print(f"[bold]Preparing transfer of {amount} TELA to {recipient[:8]}...[/bold]")

    with console.status("[bold green]Building transaction...[/bold green]"):
        sender_ata = get_associated_token_address(sender_pubkey, mint)
        recipient_ata = get_associated_token_address(recipient_pubkey, mint)

        # We need the Token Program ID
        TOKEN_PROGRAM_ID = Pubkey.from_string("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")

        ix = transfer_checked(
            TransferCheckedParams(
                program_id=TOKEN_PROGRAM_ID,
                source=sender_ata,
                mint=mint,
                dest=recipient_ata,
                owner=sender_pubkey,
                amount=raw_amount,
                decimals=decimals,
                signers=[]
            )
        )

        try:
            recent_blockhash = client.get_latest_blockhash().value.blockhash
            msg = MessageV0.try_compile(
                payer=sender_pubkey,
                instructions=[ix],
                address_lookup_table_accounts=[],
                recent_blockhash=recent_blockhash,
            )
            tx = VersionedTransaction(msg, [sender_keypair])

            console.print("[dim]Sending transaction to network...[/dim]")
            resp = client.send_transaction(tx)
            signature = resp.value

            console.print(f"\n[green]✓ Transfer successful![/green]")
            console.print(f"Signature: [cyan]{signature}[/cyan]")

        except Exception as e:
            console.print(f"\n[red]✗ Transfer failed:[/red] {e}")
            if "custom program error" in str(e).lower() or "insufficient funds" in str(e).lower():
                console.print("[yellow]Hint: You may not have enough tokens, or the recipient might not have an initialized Associated Token Account (ATA).[/yellow]")
            raise typer.Exit(code=1)

@app.command()
def stake():
    """Manage token staking for network participation or node operation."""
    console.print("Staking tokens... (Requires OpenTela staking program ID - Not yet fully implemented)")

@app.command()
def unstake():
    """Unstake tokens."""
    console.print("Unstaking tokens... (Requires OpenTela staking program ID - Not yet fully implemented)")
