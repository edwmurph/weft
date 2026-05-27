from __future__ import annotations

import os
import shlex
from pathlib import Path

from codux.config import APP_DIR_ENV, WORKDIR_ENV


PROJECT_ROOT = Path(__file__).resolve().parent.parent


def codux_cli_args(*args: str) -> list[str]:
    command = [
        "uv",
        "--directory",
        str(PROJECT_ROOT),
        "--project",
        str(PROJECT_ROOT),
        "run",
        "codux",
        *args,
    ]
    environment = [
        f"{name}={value}" for name in (APP_DIR_ENV, WORKDIR_ENV) if (value := os.environ.get(name))
    ]
    if environment:
        return ["env", *environment, *command]
    return command


def codux_cli_shell_command(*args: str) -> str:
    return " ".join(shlex.quote(arg) for arg in codux_cli_args(*args))
