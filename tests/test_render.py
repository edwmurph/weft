from __future__ import annotations

import re

from codux.config import CoduxConfig
from codux.render import (
    HELP_DOCS_URL,
    HELP_ISSUES_URL,
    HELP_POPUP_CONTENT_WIDTH,
    HELP_POPUP_WIDTH,
    codex_shortcuts,
    help_popup_height,
    nav_column_widths,
    nav_content_height,
    nav_shortcuts,
    render_bottom_border,
    render_empty_state,
    render_help,
    render_left_border,
    render_nav,
    render_right_border,
    render_side_border,
    render_top_border,
)
from codux.state import Tab, now_iso
from codux.state import AppState
from codux.titles import CODEX_TITLE_TEMPLATE


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


def test_render_empty_state_centers_in_available_pane():
    rendered = render_empty_state(width=30, height=6)

    assert rendered.splitlines() == [
        "",
        "",
        "      No Codex tabs open",
        "    Press n to create one.",
    ]


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


def test_render_nav_substitutes_codex_placeholder():
    created_at = now_iso()
    tab = Tab(
        id="active",
        title=f"Task {CODEX_TITLE_TEMPLATE}",
        codex_title="Implement auth",
        column="inbox",
        tmux_session="codux",
        tmux_window_id="@1",
        tmux_pane_id="%1",
        created_at=created_at,
        updated_at=created_at,
    )

    rendered = render_nav(CoduxConfig(), AppState(tabs=[tab], active_tab_id=tab.id), width=80)
    plain = ANSI_RE.sub("", rendered)

    assert "Task Implement auth" in plain


def test_render_nav_substitutes_codex_startup_title():
    created_at = now_iso()
    tab = Tab(
        id="active",
        title=CODEX_TITLE_TEMPLATE,
        codex_title="Starting | 019e",
        column="inbox",
        tmux_session="codux",
        tmux_window_id="@1",
        tmux_pane_id="%1",
        created_at=created_at,
        updated_at=created_at,
    )

    rendered = render_nav(CoduxConfig(), AppState(tabs=[tab], active_tab_id=tab.id), width=80)
    plain = ANSI_RE.sub("", rendered)

    assert "Starting | 019e" in plain


def test_render_nav_shows_pending_codex_placeholder_until_live_title():
    created_at = now_iso()
    tab = Tab(
        id="active",
        title=CODEX_TITLE_TEMPLATE,
        column="inbox",
        tmux_session="codux",
        tmux_window_id="@1",
        tmux_pane_id="%1",
        created_at=created_at,
        updated_at=created_at,
    )

    rendered = render_nav(CoduxConfig(), AppState(tabs=[tab], active_tab_id=tab.id), width=80)
    plain_lines = [ANSI_RE.sub("", line) for line in rendered.splitlines()]

    assert "Codex" not in "\n".join(plain_lines)
    assert plain_lines[1].strip() == "..."


def test_render_nav_leaves_manual_titles_undecorated():
    created_at = now_iso()
    tab = Tab(
        id="active",
        title="Manual title",
        codex_title="Live Codex title",
        column="inbox",
        tmux_session="codux",
        tmux_window_id="@1",
        tmux_pane_id="%1",
        created_at=created_at,
        updated_at=created_at,
    )

    rendered = render_nav(CoduxConfig(), AppState(tabs=[tab], active_tab_id=tab.id), width=80)
    plain = ANSI_RE.sub("", rendered)

    assert "Manual title" in plain
    assert "Live Codex title" not in plain
    assert "..." not in plain


def test_render_top_border_draws_rounded_title_line():
    rendered = render_top_border(12, "NAV", active=False)

    assert rendered == "\033[38;5;244mNAV ───────╴\033[0m"


def test_render_top_border_draws_right_label_before_corner():
    rendered = render_top_border(30, "NAV", active=False, right_label="~/code/configs")

    assert rendered == "\033[38;5;244mNAV ─────────── ~/code/configs\033[0m"


def test_render_bottom_border_draws_shortcuts_in_edge():
    rendered = render_bottom_border(20, active=True, label="C-d nav  C-q quit")

    assert rendered == "\033[38;5;117mC-d nav  C-q quit ─╴\033[0m"


def test_render_bottom_border_draws_right_label_flush():
    rendered = render_bottom_border(
        80,
        active=True,
        label="C-d nav  C-q quit",
        right_label="●",
    )

    assert rendered == (
        "\033[38;5;117m"
        "C-d nav  C-q quit ──────────────────────────────────────────────────────────── ●"
        "\033[0m"
    )


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
    shortcuts = nav_shortcuts(config, other_session_count=2)

    assert shortcuts == "C-q quit  ? help"
    assert "focus codex pane" not in shortcuts
    assert "new" not in codex_shortcuts(config)
    assert codex_shortcuts(config) == "C-q quit"
    assert "focus nav pane" not in codex_shortcuts(config)


def test_help_orients_user_and_lists_shortcuts():
    rendered = render_help(CoduxConfig())
    lines = rendered.splitlines()

    assert lines[0] == " " * HELP_POPUP_CONTENT_WIDTH
    assert lines[-1] == " " * HELP_POPUP_CONTENT_WIDTH
    assert {len(line) for line in lines} == {HELP_POPUP_CONTENT_WIDTH}
    assert lines[1].strip() == (
        "Codux runs one tmux workspace per launch directory. Same directory reattaches."
    )
    assert lines[2].strip() == "Another directory gets its own config, state, and session."
    assert ";" not in lines[1]
    assert ";" not in lines[2]
    assert "—" not in lines[1]
    assert "—" not in lines[2]
    assert "Config:" not in rendered
    assert "Controls:" not in rendered
    assert "Current columns:" not in rendered
    assert f"Docs: {HELP_DOCS_URL}" in rendered
    assert rendered.index(f"Docs: {HELP_DOCS_URL}") < rendered.index(
        f"Feature requests: {HELP_ISSUES_URL}"
    )
    assert rendered.index(f"Feature requests: {HELP_ISSUES_URL}") < rendered.index(
        "Nav Pane Shortcuts"
    )
    assert "#config-and-state" not in rendered
    assert "Nav Pane Shortcuts" in rendered
    assert "Codex Pane Shortcuts" in rendered
    assert "New tab" in rendered
    assert "Switch tab" in rendered
    assert "Move tab between columns" in rendered
    assert "Other dashboard sessions" not in rendered
    assert "Detach dashboard and leave Codex tabs running" in rendered
    assert "New session" not in rendered
    assert "Select session" not in rendered
    assert "Move active session" not in rendered
    assert "Shell Commands" not in rendered
    assert "Shell Commands (MVP)" not in rendered
    assert "←/→/↑/↓" in rendered
    assert "shift + ←/→" in rendered
    assert "⇧←/⇧→" not in rendered
    assert "Shift+Left/Right" not in rendered
    assert "Other keys pass through to Codex." not in rendered
    assert "codux start" not in rendered
    assert "codux doctor" not in rendered
    assert "codux sessions" not in rendered
    assert "codux delete-session SESSION" not in rendered
    assert "codux quit [--kill]" not in rendered
    assert "codux new" not in rendered
    assert "codux rename" not in rendered
    assert "codux status" not in rendered
    assert f"Feature requests: {HELP_ISSUES_URL}" in rendered


def test_help_popup_height_covers_rendered_help():
    config = CoduxConfig()

    assert HELP_POPUP_WIDTH == 84
    assert help_popup_height(config) > len(render_help(config).splitlines())
