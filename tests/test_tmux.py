from __future__ import annotations

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
