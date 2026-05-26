from __future__ import annotations

from codux.config import CoduxConfig
from codux.state import AppState, Tab, now_iso
from codux.tmux import TmuxController


def tab(tab_id: str, window_id: str) -> Tab:
    created_at = now_iso()
    return Tab(
        id=tab_id,
        title=tab_id,
        column="inbox",
        tmux_session="codux",
        tmux_window_id=window_id,
        tmux_pane_id=f"%{tab_id}",
        created_at=created_at,
        updated_at=created_at,
    )


def test_nav_border_stays_active_across_tab_windows_when_focus_is_nav():
    controller = TmuxController("codux")
    first = tab("one", "@1")
    second = tab("two", "@2")
    state = AppState(tabs=[first, second], active_tab_id=first.id, focus="nav")

    assert controller._border_is_active(second.tmux_window_id, "NAV_TOP", state)
    assert not controller._border_is_active(second.tmux_window_id, "CODEX_TOP", state)


def test_codex_border_stays_active_across_tab_windows_when_focus_is_codex():
    controller = TmuxController("codux")
    first = tab("one", "@1")
    second = tab("two", "@2")
    state = AppState(tabs=[first, second], active_tab_id=first.id, focus="codex")

    assert controller._border_is_active(second.tmux_window_id, "CODEX_TOP", state)
    assert not controller._border_is_active(second.tmux_window_id, "NAV_TOP", state)


def test_create_tab_window_does_not_select_detached_window(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []
    selected_windows: list[str] = []

    def fake_tmux(args, check=True):
        commands.append(tuple(args))
        if args and args[0] == "new-window":
            return "@9\t%9"
        return ""

    monkeypatch.setattr(controller, "ensure_session", lambda config: None)
    monkeypatch.setattr(controller, "_tmux", fake_tmux)
    monkeypatch.setattr(controller, "_split_nav_pane", lambda config, pane_id: "%10")
    monkeypatch.setattr(controller, "select_window", selected_windows.append)

    created = controller.create_tab_window(CoduxConfig(), "New Codex", "tab123")

    assert created.window_id == "@9"
    assert created.content_pane_id == "%9"
    assert created.nav_pane_id == "%10"
    assert selected_windows == []
    assert commands[0][0] == "new-window"
    assert "-d" in commands[0]
