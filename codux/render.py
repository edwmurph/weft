from __future__ import annotations

from textwrap import shorten, wrap

from codux.config import CoduxConfig
from codux.state import AppState
from codux.theme import Theme
from codux.titles import render_display_title


NAV_HORIZONTAL_PADDING = 2
HELP_POPUP_WIDTH = 84
HELP_POPUP_VERTICAL_PADDING = 2
HELP_POPUP_CONTENT_WIDTH = HELP_POPUP_WIDTH - 2
HELP_POPUP_HORIZONTAL_PADDING = 2
HELP_POPUP_INDENT = " " * HELP_POPUP_HORIZONTAL_PADDING
HELP_POPUP_TEXT_WIDTH = HELP_POPUP_CONTENT_WIDTH - (HELP_POPUP_HORIZONTAL_PADDING * 2)
HELP_SHORTCUT_KEY_WIDTH = 13
HELP_DOCS_URL = "https://github.com/edwmurph/codux"
HELP_ISSUES_URL = "https://github.com/edwmurph/codux/issues"
DEFAULT_THEME = Theme()


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
                title = render_display_title(tab.title, tab.codex_title)
                label = shorten(title, width=max(column_width, 3), placeholder="...")[:column_width]
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


def render_empty_state(width: int | None = None, height: int | None = None) -> str:
    lines = ["No Codex tabs open", "Press n to create one."]
    if width is not None:
        lines = [line.center(max(1, width)).rstrip() for line in lines]
    if height is None:
        return "\n\n" + "\n".join(lines)

    top_padding = max(0, (height - len(lines)) // 2)
    return "\n".join([*([""] * top_padding), *lines])


def render_top_border(width: int, title: str, active: bool) -> str:
    return DEFAULT_THEME.paint_border(_horizontal_border(width, label=title), active)


def render_bottom_border(width: int, active: bool, label: str = "") -> str:
    return DEFAULT_THEME.paint_border(_horizontal_border(width, label=label), active)


def render_side_border(height: int, active: bool) -> str:
    return render_left_border(1, height, active)


def render_left_border(width: int, height: int, active: bool) -> str:
    return DEFAULT_THEME.paint_border(
        _vertical_border(width, height, top="╭", bottom="╰", align="left"),
        active,
    )


def render_right_border(width: int, height: int, active: bool) -> str:
    return DEFAULT_THEME.paint_border(
        _vertical_border(width, height, top="╮", bottom="╯", align="right"),
        active,
    )


def nav_shortcuts(config: CoduxConfig, other_session_count: int = 0) -> str:
    bindings = config.key_bindings
    return (
        f"{bindings.new} new tab  {bindings.rename} rename tab  "
        f"{bindings.close} close tab  ←/→/↑/↓ switch tab  shift + ←/→ move tab  "
        f"{bindings.sessions} sessions ({other_session_count})  "
        f"{bindings.focus_toggle} focus codex pane  {bindings.quit} quit  {bindings.help} help"
    )


def codex_shortcuts(config: CoduxConfig) -> str:
    bindings = config.key_bindings
    return f"{bindings.focus_toggle} focus nav pane  {bindings.quit} quit"


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
    return DEFAULT_THEME.paint_border(text, active)


def _paint_active_tab(text: str) -> str:
    return DEFAULT_THEME.paint_active_tab(text)


def _underlined_header(text: str, width: int) -> str:
    label = text[:width]
    return f"{DEFAULT_THEME.underline}{label}{DEFAULT_THEME.end_underline}" + (
        " " * max(0, width - len(label))
    )


def render_help(config: CoduxConfig) -> str:
    bindings = config.key_bindings
    lines = [
        "Codux runs one tmux workspace per launch directory. Same directory reattaches.",
        "Another directory gets its own config, state, and session.",
        "",
        f"Docs: {HELP_DOCS_URL}",
        f"Feature requests: {HELP_ISSUES_URL}",
        "",
        "Nav Pane Shortcuts",
        _help_shortcut_row(bindings.new, "New tab"),
        _help_shortcut_row(bindings.rename, "Rename tab"),
        _help_shortcut_row(bindings.close, "Close tab"),
        _help_shortcut_row("←/→/↑/↓", "Switch tab"),
        _help_shortcut_row("shift + ←/→", "Move tab between columns"),
        _help_shortcut_row(bindings.sessions, "Other dashboard sessions"),
        _help_shortcut_row(f"Enter/{bindings.focus_toggle}", "Focus the active Codex pane"),
        _help_shortcut_row(bindings.quit, "Detach dashboard and leave Codex tabs running"),
        _help_shortcut_row(bindings.help, "Help"),
        _help_shortcut_row("Esc", "Close help"),
        "",
        "Codex Pane Shortcuts",
        _help_shortcut_row(bindings.focus_toggle, "Focus the nav pane"),
        _help_shortcut_row(bindings.quit, "Detach dashboard and leave Codex tabs running"),
    ]
    return "\n".join(_padded_help_lines(lines))


def _padded_help_lines(lines: list[str]) -> list[str]:
    padded = [_help_blank_line()]
    for line in lines:
        wrapped_lines = _wrap_help_line(line)
        padded.extend(_pad_help_line(wrapped_line) for wrapped_line in wrapped_lines)
    padded.append(_help_blank_line())
    return padded


def _wrap_help_line(line: str) -> list[str]:
    if not line or len(line) <= HELP_POPUP_TEXT_WIDTH:
        return [line]
    return wrap(
        line,
        width=HELP_POPUP_TEXT_WIDTH,
        break_long_words=False,
        break_on_hyphens=False,
    )


def _pad_help_line(line: str) -> str:
    clipped = line[:HELP_POPUP_TEXT_WIDTH]
    right_padding = " " * (HELP_POPUP_TEXT_WIDTH - len(clipped))
    return f"{HELP_POPUP_INDENT}{clipped}{right_padding}{HELP_POPUP_INDENT}"


def _help_blank_line() -> str:
    return " " * HELP_POPUP_CONTENT_WIDTH


def _help_shortcut_row(key: str, description: str) -> str:
    return f"{key:<{HELP_SHORTCUT_KEY_WIDTH}}{description}"


def help_popup_height(config: CoduxConfig) -> int:
    return len(render_help(config).splitlines()) + HELP_POPUP_VERTICAL_PADDING


__all__ = [
    "HELP_DOCS_URL",
    "HELP_ISSUES_URL",
    "HELP_POPUP_CONTENT_WIDTH",
    "HELP_POPUP_WIDTH",
    "render_nav",
    "nav_content_height",
    "render_empty_state",
    "render_top_border",
    "render_bottom_border",
    "render_side_border",
    "render_left_border",
    "render_right_border",
    "nav_shortcuts",
    "codex_shortcuts",
    "render_help",
    "help_popup_height",
]
