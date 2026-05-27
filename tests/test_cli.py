from __future__ import annotations

from codux.cli import is_transient_codex_title, repair_and_render
from codux.config import CoduxConfig
from codux.state import AppState, StateStore, Tab, now_iso, state_after_closing_tab


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
