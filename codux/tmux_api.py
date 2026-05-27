from __future__ import annotations

import subprocess
from dataclasses import dataclass


class TmuxError(RuntimeError):
    """Raised when a tmux command fails."""


def run_tmux(args: list[str], *, check: bool = True):
    command = ["tmux", *args]
    result = subprocess.run(command, check=False, text=True, capture_output=True)
    if check and result.returncode != 0:
        raise TmuxError(result.stderr.strip() or f"tmux command failed: {' '.join(command)}")
    if check:
        return result.stdout
    return result


@dataclass(frozen=True)
class TmuxApi:
    session_name: str

    def tmux(self, args: list[str], *, check: bool = True):
        return run_tmux(args, check=check)
