from __future__ import annotations

from dataclasses import dataclass
import os
import shlex
import subprocess

from .model import Machine, ProfilerConfig


@dataclass(frozen=True)
class CommandResult:
    host: str
    command: str
    returncode: int
    stdout: str
    stderr: str


class RemoteRunner:
    def __init__(self, config: ProfilerConfig, dry_run: bool = False):
        self.config = config
        self.dry_run = dry_run

    def build_args(self, machine: Machine, command: str) -> list[str]:
        template = self.config.remote_command
        if template is None:
            env_template = os.environ.get("NETWORK_PROFILER_REMOTE_CMD")
            if env_template:
                return [
                    part.format(
                        host=machine.rcc_host,
                        name=machine.name,
                        address=machine.address,
                        command=command,
                    )
                    for part in shlex.split(env_template)
                ]
            template = [
                "remote-cluster-controller",
                "exec",
                "--host",
                "{host}",
                "--",
                "bash",
                "-lc",
                "{command}",
            ]

        return [
            str(part).format(
                host=machine.rcc_host,
                name=machine.name,
                address=machine.address,
                command=command,
            )
            for part in template
        ]

    def run(self, machine: Machine, command: str, timeout: int | None = None) -> CommandResult:
        args = self.build_args(machine, command)
        if self.dry_run:
            rendered = shlex.join(args)
            return CommandResult(machine.name, rendered, 0, rendered + "\n", "")

        completed = subprocess.run(
            args,
            check=False,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=timeout,
        )
        return CommandResult(
            host=machine.name,
            command=shlex.join(args),
            returncode=completed.returncode,
            stdout=completed.stdout,
            stderr=completed.stderr,
        )
