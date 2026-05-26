from __future__ import annotations

from codux.cli import is_transient_codex_title
from codux.state import AppState, Tab, now_iso, state_after_closing_tab


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
