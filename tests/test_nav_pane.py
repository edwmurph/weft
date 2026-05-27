from __future__ import annotations

import shlex
from types import SimpleNamespace

import codux.nav_pane as nav_pane_module
from codux.config import CoduxConfig
from codux.nav_pane import NavPane, nav_keys
from codux.state import AppState, StateStore, Tab, now_iso


def tab(tab_id: str, column: str = "inbox") -> Tab:
    created_at = now_iso()
    return Tab(
        id=tab_id,
        title=tab_id,
        column=column,
        tmux_session="codux",
        tmux_window_id=f"@{tab_id}",
        tmux_pane_id=f"%{tab_id}",
        created_at=created_at,
        updated_at=created_at,
    )


def test_nav_keys_use_ctrl_d_for_focus_toggle():
    assert nav_keys(b"\x04") == ["C-d"]
    assert nav_keys(b"\x01") == []


def test_nav_pane_cli_helpers_run_from_project_root(monkeypatch):
    calls: list[tuple[list[str], object]] = []

    def fake_run(args, **kwargs):
        calls.append((args, kwargs.get("cwd")))

    monkeypatch.setattr(nav_pane_module.subprocess, "run", fake_run)

    pane = NavPane.__new__(NavPane)
    pane.run_cli("_finish-close-window", "@1")

    assert calls == [
        (
            [
                "uv",
                "--directory",
                str(nav_pane_module.PROJECT_ROOT),
                "--project",
                str(nav_pane_module.PROJECT_ROOT),
                "run",
                "python",
                "-m",
                "codux.cli",
                "_finish-close-window",
                "@1",
            ],
            None,
        )
    ]


def test_rename_popup_runs_from_project_root(monkeypatch):
    calls: list[list[str]] = []

    def fake_run(args, **kwargs):
        calls.append(args)

    monkeypatch.setattr(nav_pane_module.subprocess, "run", fake_run)

    pane = NavPane.__new__(NavPane)
    pane.rename_prompt()

    command = calls[0][-1]
    assert calls[0] == [
        "tmux",
        "display-popup",
        "-E",
        "-d",
        str(nav_pane_module.PROJECT_ROOT),
        "-w",
        "72",
        "-h",
        "10",
        "-s",
        "fg=default,bg=default",
        "-S",
        "fg=default,bg=default",
        "-T",
        "Rename",
        command,
    ]
    root = shlex.quote(str(nav_pane_module.PROJECT_ROOT))
    assert command == (
        f"uv --directory {root} --project {root} run python -m codux.cli _popup-rename"
    )
    assert "cd " not in command


def test_move_column_refreshes_frame_before_redraw(tmp_path):
    config = CoduxConfig()
    active = tab("active")
    state = AppState(tabs=[active], active_tab_id=active.id, focus="nav")
    store = StateStore(tmp_path / "state.json")
    store.write(state)
    events: list[tuple[str, object]] = []

    class FakeTmux:
        def refresh_window_frame_panes(self, config_arg, state_arg, window_id):
            events.append(("frame", state_arg.active_tab.column))

    pane = NavPane.__new__(NavPane)
    pane.config = config
    pane.store = store
    pane.state = state
    pane.tmux = FakeTmux()
    pane.skip_next_render = False
    pane.render = lambda force=False: events.append(("render", force))
    pane.select_nav_for_window = lambda window_id: events.append(("select", window_id))
    pane.refresh_static_panes_async = lambda: events.append(("refresh-async", True))

    pane.move_column(1)

    assert store.read().active_tab.column == "implement"
    assert events == [
        ("frame", "implement"),
        ("render", True),
        ("select", active.tmux_window_id),
        ("refresh-async", True),
    ]
    assert pane.skip_next_render


def test_close_last_tab_skips_redrawing_closing_nav_pane(tmp_path):
    config = CoduxConfig()
    active = tab("active")
    state = AppState(tabs=[active], active_tab_id=active.id, focus="nav")
    store = StateStore(tmp_path / "state.json")
    store.write(state)
    events: list[tuple[str, str]] = []

    pane = NavPane.__new__(NavPane)
    pane.config = config
    pane.store = store
    pane.state = state
    pane.tmux = object()
    pane.skip_next_render = False
    pane.current_state_for_input = lambda: store.read()
    pane.select_nav_for_window = lambda window_id: events.append(("select", window_id))
    pane.run_cli_async = lambda *args: events.append(("run", " ".join(args)))

    pane.close_active_tab()

    assert store.read() == AppState(tabs=[], active_tab_id=None, focus="nav")
    assert pane.skip_next_render
    assert events == [("run", f"_finish-close-window {active.tmux_window_id}")]


def test_new_tab_refreshes_codex_frame_colors_after_selecting_codex(tmp_path):
    config = CoduxConfig()
    store = StateStore(tmp_path / "state.json")
    store.write(AppState(focus="nav"))
    events: list[tuple[str, object]] = []
    frame_states: list[AppState] = []
    color_states: list[AppState] = []

    class FakeTmux:
        def claim_spare_tab_window(self, config_arg, state_arg, title, tab_id):
            return SimpleNamespace(
                window_id="@new",
                content_pane_id="%content",
                nav_pane_id="%nav",
            )

        def remove_empty_windows(self):
            events.append(("remove-empty", True))

        def refresh_window_frame_panes(self, config_arg, state_arg, window_id):
            events.append(("frame", window_id))
            frame_states.append(state_arg)

        def select_window(self, window_id):
            events.append(("window", window_id))

        def select_pane(self, pane_id):
            events.append(("pane", pane_id))

        def refresh_window_frame_colors(self, config_arg, state_arg, window_id):
            events.append(("colors", window_id))
            color_states.append(state_arg)

        def prepare_spare_window_async(self):
            events.append(("prepare-spare", True))

    pane = NavPane.__new__(NavPane)
    pane.config = config
    pane.store = store
    pane.state = store.read()
    pane.tmux = FakeTmux()
    pane.pane_id = "%nav"
    pane.window_id = "@old"
    pane.refresh_nav_pane_cache = lambda: events.append(("refresh-cache", True))
    pane.refresh_static_panes_async = lambda: events.append(("refresh-async", True))
    pane.render_snapshot = lambda state: events.append(("snapshot-focus", state.focus))

    pane.new_tab()

    state = store.read()
    assert state.active_tab is not None
    assert state.focus == "codex"
    assert frame_states[-1].focus == "codex"
    assert color_states[-1].focus == "codex"
    assert events == [
        ("snapshot-focus", "codex"),
        ("frame", "@new"),
        ("refresh-cache", True),
        ("window", "@new"),
        ("pane", "%content"),
        ("colors", "@new"),
        ("prepare-spare", True),
        ("refresh-async", True),
        ("remove-empty", True),
    ]
