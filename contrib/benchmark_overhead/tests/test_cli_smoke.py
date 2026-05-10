from click.testing import CliRunner

from benchmark_overhead.cli import cli


def test_cli_help_lists_subcommands():
    runner = CliRunner()
    result = runner.invoke(cli, ["--help"])
    assert result.exit_code == 0
    for sub in ("doctor", "deploy", "run", "report"):
        assert sub in result.output


def test_cli_doctor_exists():
    runner = CliRunner()
    result = runner.invoke(cli, ["doctor", "--help"])
    assert result.exit_code == 0
