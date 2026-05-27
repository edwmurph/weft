from __future__ import annotations

import fcntl
import json
import os
import tempfile
import time
from collections.abc import Callable
from dataclasses import asdict, dataclass, field
from datetime import UTC, datetime
from pathlib import Path
from typing import Any, Literal

from codux.config import app_dir


FocusTarget = Literal["nav", "codex"]


@dataclass(frozen=True)
class Tab:
    id: str
    title: str
    column: str
    tmux_session: str
    tmux_window_id: str
    tmux_pane_id: str
    created_at: str
    updated_at: str

    @classmethod
    def from_mapping(cls, raw: dict[str, Any]) -> Tab:
        return cls(
            id=str(raw["id"]),
            title=str(raw["title"]),
            column=str(raw["column"]),
            tmux_session=str(raw["tmux_session"]),
            tmux_window_id=str(raw["tmux_window_id"]),
            tmux_pane_id=str(raw["tmux_pane_id"]),
            created_at=str(raw["created_at"]),
            updated_at=str(raw["updated_at"]),
        )

    def with_updates(self, **changes: Any) -> Tab:
        data = asdict(self)
        data.update(changes)
        data["updated_at"] = now_iso()
        return Tab.from_mapping(data)


@dataclass(frozen=True)
class AppState:
    tabs: list[Tab] = field(default_factory=list)
    active_tab_id: str | None = None
    focus: FocusTarget = "codex"

    @classmethod
    def from_mapping(cls, raw: dict[str, Any]) -> AppState:
        focus = raw.get("focus", "codex")
        if focus not in {"nav", "codex"}:
            focus = "codex"
        return cls(
            tabs=[Tab.from_mapping(tab) for tab in raw.get("tabs", [])],
            active_tab_id=raw.get("active_tab_id"),
            focus=focus,
        )

    def to_mapping(self) -> dict[str, Any]:
        return {
            "tabs": [asdict(tab) for tab in self.tabs],
            "active_tab_id": self.active_tab_id,
            "focus": self.focus,
        }

    @property
    def active_tab(self) -> Tab | None:
        if self.active_tab_id is None:
            return None
        return next((tab for tab in self.tabs if tab.id == self.active_tab_id), None)


class StateError(ValueError):
    """Raised when state cannot be loaded."""


class StateLockTimeout(StateError):
    """Raised when another process holds the state lock too long."""


class StateStore:
    def __init__(
        self,
        path: Path | None = None,
        *,
        lock_timeout: float = 2.0,
        lock_poll_interval: float = 0.02,
    ) -> None:
        self.path = path or state_path()
        self.lock_path = self.path.with_suffix(".lock")
        self.lock_timeout = lock_timeout
        self.lock_poll_interval = lock_poll_interval

    def ensure(self) -> AppState:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.lock_path.touch(exist_ok=True)
        with self._lock():
            if not self.path.exists():
                self._write_unlocked(AppState())
            return self._read_unlocked()

    def read(self) -> AppState:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.lock_path.touch(exist_ok=True)
        with self._lock():
            if not self.path.exists():
                return AppState()
            return self._read_unlocked()

    def write(self, state: AppState) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.lock_path.touch(exist_ok=True)
        with self._lock():
            self._write_unlocked(state)

    def update(self, mutator: Callable[[AppState], AppState]) -> AppState:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.lock_path.touch(exist_ok=True)
        with self._lock():
            current = self._read_unlocked() if self.path.exists() else AppState()
            updated = mutator(current)
            self._write_unlocked(updated)
            return updated

    def _lock(self):
        lock_file = self.lock_path.open("a+", encoding="utf-8")
        deadline = time.monotonic() + self.lock_timeout
        while True:
            try:
                fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                return _HeldFileLock(lock_file)
            except BlockingIOError as exc:
                if time.monotonic() >= deadline:
                    lock_file.close()
                    raise StateLockTimeout(
                        f"timed out waiting for state lock: {self.lock_path}"
                    ) from exc
                time.sleep(self.lock_poll_interval)

    def _read_unlocked(self) -> AppState:
        try:
            return AppState.from_mapping(json.loads(self.path.read_text(encoding="utf-8")))
        except json.JSONDecodeError as exc:
            raise StateError(f"could not parse {self.path}: {exc}") from exc

    def _write_unlocked(self, state: AppState) -> None:
        payload = json.dumps(state.to_mapping(), indent=2, sort_keys=True)
        with tempfile.NamedTemporaryFile(
            "w",
            encoding="utf-8",
            dir=self.path.parent,
            delete=False,
            prefix=f".{self.path.name}.",
            suffix=".tmp",
        ) as tmp:
            tmp.write(payload)
            tmp.write("\n")
            tmp_path = Path(tmp.name)
        os.replace(tmp_path, self.path)


class _HeldFileLock:
    def __init__(self, lock_file) -> None:
        self.lock_file = lock_file

    def __enter__(self) -> None:
        return None

    def __exit__(self, exc_type, exc, traceback) -> None:
        fcntl.flock(self.lock_file.fileno(), fcntl.LOCK_UN)
        self.lock_file.close()


def state_path(base_dir: Path | None = None) -> Path:
    return (base_dir or app_dir()) / "state.json"


def now_iso() -> str:
    return datetime.now(UTC).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def prune_stale_tabs(
    state: AppState,
    exists: Callable[[Tab], bool],
) -> tuple[AppState, bool]:
    kept = [tab for tab in state.tabs if exists(tab)]
    changed = len(kept) != len(state.tabs)
    active_tab_id = state.active_tab_id
    if active_tab_id not in {tab.id for tab in kept}:
        active_tab_id = kept[0].id if kept else None
        changed = True
    repaired = AppState(tabs=kept, active_tab_id=active_tab_id, focus=state.focus)
    return repaired, changed


def state_after_closing_tab(state: AppState, target_id: str) -> AppState:
    target_index = next((index for index, tab in enumerate(state.tabs) if tab.id == target_id), -1)
    if target_index < 0:
        return state
    remaining = [tab for tab in state.tabs if tab.id != target_id]
    if not remaining:
        return AppState(tabs=[], active_tab_id=None, focus="nav")

    remaining_ids = {tab.id for tab in remaining}
    if state.active_tab_id in remaining_ids:
        active_tab_id = state.active_tab_id
    else:
        next_index = min(target_index, len(remaining) - 1)
        active_tab_id = remaining[next_index].id
    return AppState(tabs=remaining, active_tab_id=active_tab_id, focus=state.focus)
