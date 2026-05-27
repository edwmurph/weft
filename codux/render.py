from __future__ import annotations

import textwrap
from pathlib import Path
from textwrap import shorten

from codux.config import CoduxConfig, render_dir
from codux.state import AppState


ACTIVE_COLOR = "117"
ACTIVE_BORDER_COLOR = f"\033[38;5;{ACTIVE_COLOR}m"
ACTIVE_TAB_COLOR = f"\033[48;5;{ACTIVE_COLOR}m\033[38;5;16m"
INACTIVE_BORDER_COLOR = "\033[38;5;244m"
RESET_COLOR = "\033[0m"
UNDERLINE = "\033[4m"
END_UNDERLINE = "\033[24m"
NAV_HORIZONTAL_PADDING = 2


def render_nav(config: CoduxConfig, state: AppState, width: int | None = None) -> str:
    gap = 2
    padding = " " * NAV_HORIZONTAL_PADDING
    inner_width = max(1, width - (NAV_HORIZONTAL_PADDING * 2)) if width is not None else None
    column_widths = nav_column_widths(len(config.columns), inner_width, gap)
    lines = [
        padding
        + (" " * gap).join(
            _underlined_header(column.upper(), column_widths[index])
            for index, column in enumerate(config.columns)
        )
        + padding
    ]
    active_id = state.active_tab_id
    tabs_by_column = {
        column: [tab for tab in state.tabs if tab.column == column] for column in config.columns
    }
    max_rows = max((len(tabs) for tabs in tabs_by_column.values()), default=0)
    for index in range(max(max_rows, 1)):
        cells = []
        for column_index, column in enumerate(config.columns):
            column_width = column_widths[column_index]
            tabs = tabs_by_column[column]
            if index < len(tabs):
                tab = tabs[index]
                label = shorten(tab.title, width=max(column_width, 3), placeholder="...")[
                    :column_width
                ]
                cell = label.ljust(column_width)
                cells.append(_paint_active_tab(cell) if tab.id == active_id else cell)
            else:
                cells.append(" " * column_width)
        lines.append(padding + (" " * gap).join(cells) + padding)
    return "\n".join(lines)


def nav_content_height(config: CoduxConfig, state: AppState) -> int:
    tabs_by_column = {
        column: [tab for tab in state.tabs if tab.column == column] for column in config.columns
    }
    max_rows = max((len(tabs) for tabs in tabs_by_column.values()), default=0)
    return 1 + max(max_rows, 1)


def nav_column_widths(count: int, width: int | None, gap: int) -> list[int]:
    if count <= 0:
        return []
    if width is None or width <= 0:
        return [24] * count
    available = max(0, width - gap * (count - 1))
    base, remainder = divmod(available, count)
    return [base + (1 if index < remainder else 0) for index in range(count)]


def render_empty_state() -> str:
    return "\n\nNo Codex sessions open\nPress n to create one."


def render_top_border(width: int, title: str, active: bool) -> str:
    return _paint_border(_horizontal_border(width, label=title), active)


def render_bottom_border(width: int, active: bool, label: str = "") -> str:
    return _paint_border(_horizontal_border(width, label=label), active)


def render_side_border(height: int, active: bool) -> str:
    return render_left_border(1, height, active)


def render_left_border(width: int, height: int, active: bool) -> str:
    return _paint_border(
        _vertical_border(width, height, top="╭", bottom="╰", align="left"),
        active,
    )


def render_right_border(width: int, height: int, active: bool) -> str:
    return _paint_border(
        _vertical_border(width, height, top="╮", bottom="╯", align="right"),
        active,
    )


def nav_shortcuts(config: CoduxConfig) -> str:
    bindings = config.key_bindings
    return (
        f"{bindings.new} new  arrows move cursor  shift+arrows move tab  {bindings.rename} rename  "
        f"{bindings.close} close  {bindings.help} help  "
        f"{bindings.focus_toggle} codex  {bindings.quit} quit"
    )


def codex_shortcuts(config: CoduxConfig) -> str:
    bindings = config.key_bindings
    return f"{bindings.focus_toggle} nav  {bindings.quit} quit"


def _horizontal_border(width: int, label: str) -> str:
    width = max(1, width)
    if width == 1:
        return "─"

    if label:
        text = f"{label} "
        max_text_width = width - 1
        if len(text) > max_text_width:
            text = text[: max(0, max_text_width - 3)].rstrip() + "..." if width > 4 else ""
        return text + ("─" * max(0, width - len(text) - 1)) + "╴"
    return "╶" + ("─" * max(0, width - 2)) + "╴"


def _vertical_border(width: int, height: int, top: str, bottom: str, align: str) -> str:
    width = max(1, width)
    height = max(1, height)
    middle = "│".ljust(width) if align == "left" else "│".rjust(width)
    top_line = (top + ("─" * (width - 1))) if align == "left" else (("─" * (width - 1)) + top)
    bottom_line = (
        (bottom + ("─" * (width - 1))) if align == "left" else (("─" * (width - 1)) + bottom)
    )
    if height == 1:
        return top_line
    if height == 2:
        return "\n".join([top_line, bottom_line])
    return "\n".join([top_line, *([middle] * (height - 2)), bottom_line])


def _paint_border(text: str, active: bool) -> str:
    color = ACTIVE_BORDER_COLOR if active else INACTIVE_BORDER_COLOR
    return f"{color}{text}{RESET_COLOR}"


def _paint_active_tab(text: str) -> str:
    return f"{ACTIVE_TAB_COLOR}{text}{RESET_COLOR}"


def _underlined_header(text: str, width: int) -> str:
    label = text[:width]
    return f"{UNDERLINE}{label}{END_UNDERLINE}" + (" " * max(0, width - len(label)))


def render_help(config: CoduxConfig) -> str:
    bindings = config.key_bindings
    return textwrap.dedent(
        f"""
        Codux shortcuts

        {bindings.new}        New Codex session
        Arrow keys     Move through the visible nav grid
        Shift + arrow keys    Move active tab left / right
        {bindings.rename}        Rename active tab
        {bindings.close}        Close active tab
        {bindings.help}        Show this help
        Esc      Close this help popup
        Enter    Focus the active Codex pane
        {bindings.focus_toggle}      Toggle focus between nav and Codex
        {bindings.quit}      Detach dashboard and leave sessions running

        Shell commands

        codux start
        codux new [TITLE]
        codux rename TITLE
        codux close
        codux status
        codux doctor
        codux quit
        """
    ).strip()


def write_render_files(
    config: CoduxConfig,
    state: AppState,
    base_dir: Path | None = None,
    nav_width: int | None = None,
) -> None:
    directory = render_dir(base_dir)
    directory.mkdir(parents=True, exist_ok=True)
    (directory / "nav.txt").write_text(render_nav(config, state, nav_width), encoding="utf-8")
    (directory / "empty.txt").write_text(render_empty_state(), encoding="utf-8")
