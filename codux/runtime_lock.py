from __future__ import annotations

import fcntl
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

from codux.state import state_path


def runtime_lock_path(state_file: Path | None = None) -> Path:
    return (state_file or state_path()).with_suffix(".runtime.lock")


@contextmanager
def runtime_lock(
    *,
    path: Path | None = None,
    state_file: Path | None = None,
    wait: bool = False,
) -> Iterator[bool]:
    lock_path = path or runtime_lock_path(state_file)
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    lock_flags = fcntl.LOCK_EX if wait else fcntl.LOCK_EX | fcntl.LOCK_NB
    with open(lock_path, "w", encoding="utf-8") as lock_file:
        try:
            fcntl.flock(lock_file.fileno(), lock_flags)
        except BlockingIOError:
            yield False
            return
        yield True
