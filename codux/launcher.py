from __future__ import annotations

import shlex
from pathlib import Path


PROJECT_ROOT = Path(__file__).resolve().parent.parent


def codux_cli_args(*args: str) -> list[str]:
    return [
        "uv",
        "--directory",
        str(PROJECT_ROOT),
        "--project",
        str(PROJECT_ROOT),
        "run",
        "codux",
        *args,
    ]


def codux_cli_shell_command(*args: str) -> str:
    return " ".join(shlex.quote(arg) for arg in codux_cli_args(*args))
