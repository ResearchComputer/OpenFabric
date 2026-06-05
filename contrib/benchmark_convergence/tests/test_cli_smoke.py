from click.testing import CliRunner

from benchmark_convergence.cli import cli


def test_cli_has_expected_subcommands() -> None:
    runner = CliRunner()
    result = runner.invoke(cli, ["--help"])
    assert result.exit_code == 0
    for sub in ("doctor", "submit", "run", "report", "aggregate"):
        assert sub in result.output


def test_cli_run_help_smokes() -> None:
    runner = CliRunner()
    result = runner.invoke(cli, ["run", "--help"])
    assert result.exit_code == 0
