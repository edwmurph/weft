from __future__ import annotations

from codux.navigation import select_grid_tab
from codux.state import Tab, now_iso


def test_select_grid_tab_moves_vertically_within_column():
    tabs = [
        make_tab("a", "First", "inbox"),
        make_tab("b", "Second", "inbox"),
        make_tab("c", "Third", "implement"),
    ]

    assert select_grid_tab(tabs, "a", COLUMNS, delta_row=1).id == "b"
    assert select_grid_tab(tabs, "b", COLUMNS, delta_row=-1).id == "a"


def test_select_grid_tab_moves_horizontally_by_visible_row():
    tabs = [
        make_tab("a", "Inbox 1", "inbox"),
        make_tab("b", "Inbox 2", "inbox"),
        make_tab("c", "Implement 1", "implement"),
        make_tab("d", "Ship 1", "ship"),
    ]

    assert select_grid_tab(tabs, "b", COLUMNS, delta_column=1).id == "c"
    assert select_grid_tab(tabs, "c", COLUMNS, delta_column=1).id == "d"


def test_select_grid_tab_skips_empty_columns():
    tabs = [
        make_tab("a", "Inbox", "inbox"),
        make_tab("b", "Ship", "ship"),
    ]

    assert select_grid_tab(tabs, "a", COLUMNS, delta_column=1).id == "b"


def test_select_grid_tab_stays_at_grid_edge():
    tabs = [make_tab("a", "Inbox", "inbox")]

    assert select_grid_tab(tabs, "a", COLUMNS, delta_column=-1).id == "a"
    assert select_grid_tab(tabs, "a", COLUMNS, delta_row=1).id == "a"


def make_tab(tab_id: str, title: str, column: str) -> Tab:
    created_at = now_iso()
    return Tab(
        id=tab_id,
        title=title,
        column=column,
        tmux_session="codux",
        tmux_window_id=f"@{tab_id}",
        tmux_pane_id=f"%{tab_id}",
        created_at=created_at,
        updated_at=created_at,
    )


COLUMNS = ["inbox", "implement", "ship"]
