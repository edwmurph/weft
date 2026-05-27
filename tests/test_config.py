from __future__ import annotations

import pytest

import codux.config as config_module
from codux.config import (
    APP_DIR_ENV,
    ConfigError,
    DEFAULT_COLUMNS,
    app_dir,
    config_path,
    default_tmux_session,
    ensure_config,
    load_config,
)


def test_ensure_config_creates_default(tmp_path):
    config = ensure_config(tmp_path)

    assert config_path(tmp_path).exists()
    assert config.tmux_session == "codux"
    assert config.codex_command == "codex"
    assert config.columns == DEFAULT_COLUMNS
    assert config.key_bindings.new == "n"
    assert config.key_bindings.prev == "Left"
    assert config.key_bindings.move_right == "S-Right"
    assert config.key_bindings.focus_toggle == "C-d"


def test_source_checkout_runtime_defaults_are_worktree_isolated(monkeypatch, tmp_path):
    source_root = tmp_path / "repo" / ".worktrees" / "feature-a"
    source_root.mkdir(parents=True)
    (source_root / ".git").write_text("gitdir: ../../.git/worktrees/feature-a\n", encoding="utf-8")
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.delenv(APP_DIR_ENV, raising=False)
    monkeypatch.setattr(config_module, "SOURCE_ROOT", source_root)

    runtime_dir = app_dir()

    assert runtime_dir.parent == tmp_path / "home" / ".codux" / "worktrees"
    assert runtime_dir.name.startswith("feature-a-")
    assert default_tmux_session().startswith(f"codux-{runtime_dir.name}")


def test_codux_home_override_preserves_global_session_default(monkeypatch, tmp_path):
    monkeypatch.setenv(APP_DIR_ENV, str(tmp_path / "custom"))

    assert app_dir() == tmp_path / "custom"
    assert default_tmux_session() == "codux"


def test_load_config_accepts_custom_values(tmp_path):
    path = config_path(tmp_path)
    path.write_text(
        """
tmux_session = "custom"
codex_command = "codex --foo"
columns = ["One", "Two"]

[key_bindings]
new = "a"
prev = "b"
next = "c"
move_left = "d"
move_right = "e"
rename = "f"
close = "g"
help = "?"
focus_toggle = "C-b"
""",
        encoding="utf-8",
    )

    config = load_config(path)

    assert config.tmux_session == "custom"
    assert config.codex_command == "codex --foo"
    assert config.columns == ["One", "Two"]
    assert config.key_bindings.prev == "b"


def test_load_config_rejects_duplicate_columns(tmp_path):
    path = config_path(tmp_path)
    path.write_text('columns = ["One", "One"]\n', encoding="utf-8")

    with pytest.raises(ConfigError, match="unique"):
        load_config(path)


def test_load_config_rejects_non_list_columns(tmp_path):
    path = config_path(tmp_path)
    path.write_text('columns = "One"\n', encoding="utf-8")

    with pytest.raises(ConfigError, match="list"):
        load_config(path)


def test_ensure_config_migrates_old_generated_defaults(tmp_path):
    path = config_path(tmp_path)
    path.write_text(
        """
tmux_session = "codux"
codex_command = "codex"
columns = ["Backlog", "Active", "Review", "Done"]

[key_bindings]
new = "n"
prev = "h"
next = "l"
move_left = "H"
move_right = "L"
rename = "r"
close = "x"
help = "?"
focus_toggle = "C-a"
""",
        encoding="utf-8",
    )

    config = ensure_config(tmp_path)

    assert config.columns == DEFAULT_COLUMNS
    assert config.key_bindings.quit == "C-q"
    assert config.key_bindings.prev == "Left"
    assert config.key_bindings.next == "Right"
    assert config.key_bindings.move_left == "S-Left"
    assert config.key_bindings.move_right == "S-Right"
    assert config.key_bindings.focus_toggle == "C-d"
    text = path.read_text(encoding="utf-8")
    assert 'columns = ["inbox", "implement", "ship"]' in text
    assert 'quit = "C-q"' in text
    assert 'prev = "Left"' in text
    assert 'move_right = "S-Right"' in text
    assert 'focus_toggle = "C-d"' in text
