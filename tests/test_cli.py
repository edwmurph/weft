from __future__ import annotations

import shlex
from types import SimpleNamespace

import codux.cli as cli_module
from codux.cli import is_transient_codex_title, repair_and_render
from codux.config import CoduxConfig
from codux.state import AppState, StateStore, Tab, now_iso, state_after_closing_tab
from codux.tmux import TmuxError


def tab(tab_id: str) -> Tab:
    created_at = now_iso()
    return Tab(
        id=tab_id,
        title=tab_id,
        column="inbox",
        tmux_session="codux",
        tmux_window_id=f"@{tab_id}",
        tmux_pane_id=f"%{tab_id}",
        created_at=created_at,
        updated_at=created_at,
    )


def test_codux_command_runs_from_project_root(monkeypatch):
    monkeypatch.setattr(cli_module.sys, "executable", "/tmp/codux python")

    command = cli_module.codux_command()

    assert command == (
        f"cd {shlex.quote(str(cli_module.PROJECT_ROOT))} && "
        f"{shlex.quote('/tmp/codux python')} -m codux.cli"
    )


def test_state_after_closing_active_tab_selects_neighbor():
    first = tab("first")
    second = tab("second")
    state = AppState(tabs=[first, second], active_tab_id=first.id, focus="codex")

    updated = state_after_closing_tab(state, first.id)

    assert updated.tabs == [second]
    assert updated.active_tab_id == second.id
    assert updated.focus == "codex"


def test_state_after_closing_inactive_tab_preserves_active_tab():
    first = tab("first")
    second = tab("second")
    state = AppState(tabs=[first, second], active_tab_id=first.id, focus="nav")

    updated = state_after_closing_tab(state, second.id)

    assert updated.tabs == [first]
    assert updated.active_tab_id == first.id
    assert updated.focus == "nav"


def test_transient_codex_titles_are_ignored():
    assert is_transient_codex_title("Starting | 019e")
    assert is_transient_codex_title("| Starting | 019e")
    assert is_transient_codex_title("- Starting")
    assert is_transient_codex_title("Ready | 019e")
    assert not is_transient_codex_title("Implement auth flow")


def test_repair_and_render_recovers_live_tmux_tabs(monkeypatch, tmp_path):
    recovered = tab("live").with_updates(title="Recovered")
    refreshed: list[AppState] = []

    class FakeTmux:
        def has_session(self):
            return True

        def window_exists(self, window_id):
            return True

        def pane_exists(self, pane_id):
            return True

        def pane_title(self, pane_id):
            return None

        def recoverable_tabs(self, config):
            return [recovered]

        def active_tab_id_from_tmux(self):
            return recovered.id

        def refresh_static_panes(self, config, state):
            refreshed.append(state)

    monkeypatch.setattr("codux.cli.write_render_files", lambda config, state: None)
    store = StateStore(tmp_path / "state.json")
    store.write(AppState())

    state = repair_and_render(CoduxConfig(), store, FakeTmux())

    assert state.tabs == [recovered]
    assert state.active_tab_id == recovered.id
    assert refreshed[-1].active_tab_id == recovered.id


def test_new_focuses_codex_pane(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    store.write(AppState())
    events: list[tuple[str, str]] = []
    refreshed: list[AppState] = []

    class FakeTmux:
        def has_session(self):
            return True

        def ensure_session(self, config):
            pass

        def claim_spare_tab_window(self, config, state, title, tab_id):
            return SimpleNamespace(
                window_id="@new",
                content_pane_id="%content",
                nav_pane_id="%nav",
            )

        def remove_empty_windows(self):
            pass

        def refresh_window_frame_panes(self, config, state, window_id):
            refreshed.append(state)

        def select_window(self, window_id):
            events.append(("window", window_id))

        def select_pane(self, pane_id):
            events.append(("pane", pane_id))

        def prepare_spare_window_async(self):
            pass

    fake_tmux = FakeTmux()
    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, fake_tmux))
    monkeypatch.setattr(cli_module, "refresh_runtime_async", lambda: None)

    cli_module.new("Test")

    state = store.read()
    assert state.active_tab is not None
    assert state.focus == "codex"
    assert state.active_tab.tmux_pane_id == "%content"
    assert refreshed[-1].focus == "codex"
    assert events == [("window", "@new"), ("pane", "%content")]


def test_focus_window_ignores_frame_refresh_race(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    active = tab("active")
    store.write(AppState(tabs=[active], active_tab_id=active.id, focus="codex"))

    class FakeTmux:
        def refresh_window_frame_colors(self, config, state, window_id):
            raise TmuxError("pane went away")

    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, FakeTmux()))

    cli_module.focus_window_command(active.tmux_window_id, "nav")

    state = store.read()
    assert state.active_tab_id == active.id
    assert state.focus == "nav"


def test_focus_window_is_best_effort_when_runtime_is_unavailable(monkeypatch):
    def fail_runtime():
        raise RuntimeError("runtime changed")

    monkeypatch.setattr(cli_module, "load_runtime", fail_runtime)

    cli_module.focus_window_command("@missing", "nav")


def test_rename_active_tab_updates_state_and_tmux(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    active = tab("active")
    store.write(AppState(tabs=[active], active_tab_id=active.id, focus="codex"))
    events: list[tuple[str, str, str] | tuple[str, AppState]] = []

    class FakeTmux:
        def rename_window(self, window_id, title):
            events.append(("rename-window", window_id, title))

        def set_pane_title(self, pane_id, title):
            events.append(("set-pane-title", pane_id, title))

    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, FakeTmux()))
    monkeypatch.setattr(
        cli_module,
        "write_render_files",
        lambda config, state: events.append(("render", state)),
    )
    monkeypatch.setattr(cli_module, "refresh_runtime_async", lambda: None)

    state = cli_module.rename_active_tab("Renamed")

    assert state.active_tab is not None
    assert state.active_tab.title == "Renamed"
    assert store.read().active_tab.title == "Renamed"
    assert events == [
        ("rename-window", active.tmux_window_id, "Renamed"),
        ("set-pane-title", active.tmux_pane_id, "Renamed"),
        ("render", state),
    ]
