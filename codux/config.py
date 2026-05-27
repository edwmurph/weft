from __future__ import annotations

import os
import tomllib
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


APP_DIR_ENV = "CODUX_HOME"
OLD_DEFAULT_COLUMNS = ["Backlog", "Active", "Review", "Done"]
DEFAULT_COLUMNS = ["inbox", "implement", "ship"]


class ConfigError(ValueError):
    """Raised when a Codux config file is invalid."""


@dataclass(frozen=True)
class KeyBindings:
    new: str = "n"
    prev: str = "Left"
    next: str = "Right"
    move_left: str = "S-Left"
    move_right: str = "S-Right"
    rename: str = "r"
    close: str = "c"
    help: str = "?"
    focus_toggle: str = "C-d"
    quit: str = "C-q"

    @classmethod
    def from_mapping(cls, raw: dict[str, Any] | None) -> KeyBindings:
        values = raw or {}
        bindings = cls(
            new=str(values.get("new", cls.new)),
            prev=str(values.get("prev", values.get("previous", cls.prev))),
            next=str(values.get("next", cls.next)),
            move_left=str(values.get("move_left", cls.move_left)),
            move_right=str(values.get("move_right", cls.move_right)),
            rename=str(values.get("rename", cls.rename)),
            close=str(values.get("close", cls.close)),
            help=str(values.get("help", cls.help)),
            focus_toggle=str(values.get("focus_toggle", cls.focus_toggle)),
            quit=str(values.get("quit", cls.quit)),
        )
        bindings.validate()
        return bindings

    def validate(self) -> None:
        for name, value in self.__dict__.items():
            if not value.strip():
                raise ConfigError(f"key binding {name!r} must be a non-empty string")


@dataclass(frozen=True)
class CoduxConfig:
    tmux_session: str = "codux"
    codex_command: str = "codex"
    columns: list[str] = field(default_factory=lambda: DEFAULT_COLUMNS.copy())
    key_bindings: KeyBindings = field(default_factory=KeyBindings)

    @classmethod
    def from_mapping(
        cls, raw: dict[str, Any], tmux_session_default: str | None = None
    ) -> CoduxConfig:
        key_binding_raw = raw.get("key_bindings", raw.get("bindings", {}))
        raw_columns = raw.get("columns", DEFAULT_COLUMNS)
        if not isinstance(raw_columns, list):
            raise ConfigError("columns must be a list of strings")
        columns = [str(column).strip() for column in raw_columns]
        if columns == OLD_DEFAULT_COLUMNS:
            columns = DEFAULT_COLUMNS.copy()
        config = cls(
            tmux_session=str(raw.get("tmux_session", tmux_session_default or cls.tmux_session)),
            codex_command=str(raw.get("codex_command", cls.codex_command)),
            columns=columns,
            key_bindings=KeyBindings.from_mapping(key_binding_raw),
        )
        config.validate()
        return config

    def validate(self) -> None:
        if not self.tmux_session.strip():
            raise ConfigError("tmux_session must be a non-empty string")
        if not self.codex_command.strip():
            raise ConfigError("codex_command must be a non-empty string")
        if not self.columns:
            raise ConfigError("columns must include at least one column")
        normalized = [column.strip() for column in self.columns]
        if any(not column for column in normalized):
            raise ConfigError("columns must be non-empty strings")
        if len(set(normalized)) != len(normalized):
            raise ConfigError("columns must be unique")
        self.key_bindings.validate()


def app_dir() -> Path:
    if configured := os.environ.get(APP_DIR_ENV):
        return Path(configured).expanduser()
    return Path.home() / ".codux"


def default_tmux_session(base_dir: Path | None = None) -> str:
    return CoduxConfig.tmux_session


def config_path(base_dir: Path | None = None) -> Path:
    return (base_dir or app_dir()) / "config.toml"


def render_dir(base_dir: Path | None = None) -> Path:
    return (base_dir or app_dir()) / "render"


def default_config_text(tmux_session: str | None = None) -> str:
    columns = ", ".join(f'"{column}"' for column in DEFAULT_COLUMNS)
    tmux_session = tmux_session or default_tmux_session()
    return f"""# Codux runtime configuration.
tmux_session = "{tmux_session}"
codex_command = "codex"
# Ordered columns shown in the nav pane.
columns = [{columns}]

[key_bindings]
new = "n"
prev = "Left"
next = "Right"
move_left = "S-Left"
move_right = "S-Right"
rename = "r"
close = "c"
help = "?"
focus_toggle = "C-d"
quit = "C-q"
"""


def ensure_config(base_dir: Path | None = None) -> CoduxConfig:
    path = config_path(base_dir)
    if not path.exists():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            default_config_text(tmux_session=default_tmux_session(base_dir)),
            encoding="utf-8",
        )
    else:
        migrate_default_config(path)
    return load_config(path)


def migrate_default_config(path: Path) -> None:
    text = path.read_text(encoding="utf-8")
    updated = text.replace(
        'columns = ["Backlog", "Active", "Review", "Done"]',
        'columns = ["inbox", "implement", "ship"]',
    )
    if "[key_bindings]" in updated:
        if "\nquit =" not in updated:
            updated = updated.replace(
                'focus_toggle = "C-a"\n',
                'focus_toggle = "C-a"\nquit = "C-q"\n',
            )
            updated = updated.replace(
                'focus_toggle = "C-d"\n',
                'focus_toggle = "C-d"\nquit = "C-q"\n',
            )
        updated = updated.replace('prev = "h"\n', 'prev = "Left"\n')
        updated = updated.replace('next = "l"\n', 'next = "Right"\n')
        updated = updated.replace('move_left = "H"\n', 'move_left = "S-Left"\n')
        updated = updated.replace('move_right = "L"\n', 'move_right = "S-Right"\n')
        updated = updated.replace('close = "x"\n', 'close = "c"\n')
        updated = updated.replace('focus_toggle = "C-a"\n', 'focus_toggle = "C-d"\n')
    if updated != text:
        path.write_text(updated, encoding="utf-8")


def load_config(path: Path | None = None) -> CoduxConfig:
    config_file = path or config_path()
    try:
        raw = tomllib.loads(config_file.read_text(encoding="utf-8"))
    except tomllib.TOMLDecodeError as exc:
        raise ConfigError(f"could not parse {config_file}: {exc}") from exc
    return CoduxConfig.from_mapping(raw, tmux_session_default=default_tmux_session())
