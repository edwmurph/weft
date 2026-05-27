from __future__ import annotations

import re

from codux.config import CoduxConfig
from codux.render import (
    codex_shortcuts,
    nav_column_widths,
    nav_content_height,
    nav_shortcuts,
    render_bottom_border,
    render_left_border,
    render_nav,
    render_right_border,
    render_side_border,
    render_top_border,
)
from codux.state import Tab, now_iso
from codux.state import AppState


ANSI_RE = re.compile(r"\033\[[0-9;?]*[A-Za-z]")


def test_nav_column_widths_fill_available_width():
    widths = nav_column_widths(count=3, width=100, gap=2)

    assert widths == [32, 32, 32]
    assert sum(widths) + 2 * 2 == 100


def test_render_nav_uses_configured_width():
    rendered = render_nav(CoduxConfig(), AppState(), width=60)

    first_line = rendered.splitlines()[0]
    second_line = rendered.splitlines()[1]
    plain_first_line = ANSI_RE.sub("", first_line)
    assert len(plain_first_line) == 60
    assert len(second_line) == 60
    assert "\033[4mINBOX\033[24m" in first_line
    assert "\033[4mIMPLEMENT\033[24m" in first_line
    assert "\033[4mSHIP\033[24m" in first_line
    assert plain_first_line[0:7] == "  INBOX"
    assert plain_first_line[22:31] == "IMPLEMENT"
    assert plain_first_line[41:45] == "SHIP"
    assert "-" not in second_line


def test_render_nav_keeps_padding_when_empty():
    rendered = render_nav(CoduxConfig(), AppState(), width=60)
    plain_lines = [ANSI_RE.sub("", line) for line in rendered.splitlines()]

    assert plain_lines[0].startswith("  INBOX")
    assert plain_lines[1].startswith("  ")


def test_render_nav_highlights_active_tab_without_marker():
    created_at = now_iso()
    tab = Tab(
        id="active",
        title="Active tab",
        column="inbox",
        tmux_session="codux",
        tmux_window_id="@1",
        tmux_pane_id="%1",
        created_at=created_at,
        updated_at=created_at,
    )

    rendered = render_nav(CoduxConfig(), AppState(tabs=[tab], active_tab_id=tab.id), width=60)

    assert "> Active tab" not in rendered
    assert "\033[48;5;117m\033[38;5;16mActive tab" in rendered


def test_render_top_border_draws_rounded_title_line():
    rendered = render_top_border(12, "NAV", active=False)

    assert rendered == "\033[38;5;244mNAV ───────╴\033[0m"


def test_render_bottom_border_draws_shortcuts_in_edge():
    rendered = render_bottom_border(20, active=True, label="C-a nav  C-q quit")

    assert rendered == "\033[38;5;117mC-a nav  C-q quit ─╴\033[0m"


def test_render_side_border_draws_requested_height():
    rendered = render_side_border(3, active=True)

    assert rendered.splitlines() == [
        "\033[38;5;117m╭",
        "│",
        "╰\033[0m",
    ]


def test_render_wide_left_border_caps_corners():
    rendered = render_left_border(3, 3, active=True)

    assert rendered.splitlines() == [
        "\033[38;5;117m╭──",
        "│  ",
        "╰──\033[0m",
    ]


def test_render_right_border_uses_right_corners():
    rendered = render_right_border(3, 3, active=False)

    assert rendered.splitlines() == [
        "\033[38;5;244m──╮",
        "  │",
        "──╯\033[0m",
    ]


def test_nav_content_height_tracks_tallest_column():
    created_at = now_iso()
    tabs = [
        Tab(
            id=str(index),
            title=f"Tab {index}",
            column="inbox",
            tmux_session="codux",
            tmux_window_id=f"@{index}",
            tmux_pane_id=f"%{index}",
            created_at=created_at,
            updated_at=created_at,
        )
        for index in range(3)
    ]

    assert nav_content_height(CoduxConfig(), AppState(tabs=tabs)) == 4


def test_shortcut_labels_are_pane_specific():
    config = CoduxConfig()

    assert "new" in nav_shortcuts(config)
    assert "new" not in codex_shortcuts(config)
