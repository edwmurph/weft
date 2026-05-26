from __future__ import annotations

from codux.config import CoduxConfig
from codux.host import Layout, Rect, ansi_visible_width, render_dashboard
from codux.state import AppState


def test_dashboard_host_draws_continuous_rounded_boxes():
    layout = Layout(
        width=40,
        height=10,
        nav_frame=Rect(1, 1, 40, 4),
        nav_inner=Rect(2, 2, 38, 2),
        codex_frame=Rect(1, 5, 40, 6),
        codex_inner=Rect(2, 6, 38, 4),
    )

    lines = render_dashboard(CoduxConfig(), AppState(focus="nav"), layout, child=None)

    assert "╭─ NAV " in lines[0]
    assert lines[0].rstrip().endswith("╮\033[0m")
    assert lines[3].startswith("\033[38;5;117m╰─ n new")
    assert lines[3].rstrip().endswith("─╯\033[0m")
    assert lines[4].startswith("\033[38;5;244m╭─ CODEX ")
    assert "╭── ─" not in "\n".join(lines)
    assert "── ──╮" not in "\n".join(lines)
    assert all(ansi_visible_width(line) == 40 for line in lines)
