from __future__ import annotations

from collections.abc import Sequence

from codux.state import Tab


def select_grid_tab(
    tabs: Sequence[Tab],
    active_tab_id: str | None,
    columns: Sequence[str],
    *,
    delta_column: int = 0,
    delta_row: int = 0,
) -> Tab | None:
    if not tabs:
        return None
    if not columns:
        return tabs[0]

    tabs_by_column = {column: [tab for tab in tabs if tab.column == column] for column in columns}
    active_tab = next((tab for tab in tabs if tab.id == active_tab_id), tabs[0])
    column_index = columns.index(active_tab.column) if active_tab.column in columns else 0
    column_tabs = tabs_by_column[columns[column_index]]
    row_index = _tab_row(column_tabs, active_tab)

    if delta_row:
        return _select_row(column_tabs, row_index + delta_row) or active_tab
    if delta_column:
        target_column_index = column_index + delta_column
        while 0 <= target_column_index < len(columns):
            target_tabs = tabs_by_column[columns[target_column_index]]
            selected = _select_row(target_tabs, row_index)
            if selected is not None:
                return selected
            target_column_index += delta_column
        return active_tab
    return active_tab


def _tab_row(column_tabs: Sequence[Tab], tab: Tab) -> int:
    return next(
        (index for index, column_tab in enumerate(column_tabs) if column_tab.id == tab.id), 0
    )


def _select_row(column_tabs: Sequence[Tab], row_index: int) -> Tab | None:
    if not column_tabs:
        return None
    clamped_index = max(0, min(len(column_tabs) - 1, row_index))
    return column_tabs[clamped_index]
