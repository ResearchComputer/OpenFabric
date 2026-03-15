import typer
from .commands.auth.cli import app as auth_app
from .commands.cluster.cli import app as cluster_app
from .commands.network.cli import app as network_app
from .commands.wallet.cli import app as wallet_app

app = typer.Typer(
    help="OpenTela CLI - Interact with the OpenTela network, manage resources, and deploy workloads."
)

app.add_typer(auth_app, name="auth", help="Authentication and Configuration")
app.add_typer(cluster_app, name="cluster", help="Cluster Management")
app.add_typer(network_app, name="network", help="Network and Resource Discovery")
app.add_typer(wallet_app, name="wallet", help="Financial and Token Integration")

if __name__ == "__main__":
    app()
