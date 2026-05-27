from __future__ import annotations

import shlex

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
    monkeypatch.setattr(nav_pane_module.sys, "executable", "/tmp/codux python")

    def fake_run(args, **kwargs):
        calls.append((args, kwargs.get("cwd")))

    monkeypatch.setattr(nav_pane_module.subprocess, "run", fake_run)

    pane = NavPane.__new__(NavPane)
    pane.run_cli("_finish-close-window", "@1")

    assert calls == [
        (
            ["/tmp/codux python", "-m", "codux.cli", "_finish-close-window", "@1"],
            nav_pane_module.PROJECT_ROOT,
        )
    ]


def test_rename_popup_runs_from_project_root(monkeypatch):
    calls: list[list[str]] = []
    monkeypatch.setattr(nav_pane_module.sys, "executable", "/tmp/codux python")

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
    assert command == (
        f"cd {shlex.quote(str(nav_pane_module.PROJECT_ROOT))} && "
        f"{shlex.quote('/tmp/codux python')} -m codux.cli _popup-rename"
    )


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
