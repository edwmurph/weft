from __future__ import annotations

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
