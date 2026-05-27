from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class PaneSnapshot:
    pane_id: str
    window_id: str
    role: str
    title: str
    top: int
    left: int
    width: int
    height: int
    current_command: str
    start_command: str
    nav_host_version: str
    frame_host_version: str


@dataclass(frozen=True)
class WindowSnapshot:
    window_id: str
    name: str
    width: int
    height: int
    empty: str
    spare: str
    tab_id: str
    nav_pane_configured: str
    codex_pane_configured: str
    frame_version: str


@dataclass(frozen=True)
class TmuxSnapshot:
    windows: dict[str, WindowSnapshot]
    panes: dict[str, PaneSnapshot]
    panes_by_window: dict[str, list[PaneSnapshot]]

    def window(self, window_id: str) -> WindowSnapshot | None:
        return self.windows.get(window_id)

    def window_panes(self, window_id: str) -> list[PaneSnapshot]:
        return self.panes_by_window.get(window_id, [])


def _parse_int(value: str, default: int = 0) -> int:
    try:
        return int(value)
    except ValueError:
        return default


def fetch_snapshot(
    tmux,
    *,
    session_name: str,
    empty_window_option: str,
    spare_window_option: str,
    tab_id_option: str,
    frame_version_option: str,
    nav_host_option: str,
    frame_host_option: str,
) -> TmuxSnapshot:
    raw_windows = tmux(
        [
            "list-windows",
            "-t",
            session_name,
            "-F",
            (
                "#{window_id}\t#{window_name}\t#{window_width}\t#{window_height}\t"
                f"#{{{empty_window_option}}}\t#{{{spare_window_option}}}\t#{{{tab_id_option}}}\t"
                "#{@codux-nav-pane}\t#{@codux-codex-pane}\t"
                f"#{{{frame_version_option}}}"
            ),
        ],
        check=False,
    )
    windows: dict[str, WindowSnapshot] = {}
    for line in raw_windows.splitlines():
        parts = line.split("\t")
        if len(parts) != 10:
            continue
        (
            window_id,
            name,
            width,
            height,
            empty,
            spare,
            tab_id,
            nav_configured,
            codex_configured,
            frame_version,
        ) = parts
        if not window_id:
            continue
        windows[window_id] = WindowSnapshot(
            window_id=window_id,
            name=name,
            width=_parse_int(width, 0),
            height=_parse_int(height, 0),
            empty=empty,
            spare=spare,
            tab_id=tab_id,
            nav_pane_configured=nav_configured,
            codex_pane_configured=codex_configured,
            frame_version=frame_version,
        )

    raw_panes = tmux(
        [
            "list-panes",
            "-s",
            "-t",
            session_name,
            "-F",
            (
                "#{pane_id}\t#{window_id}\t#{@codux-role}\t#{pane_title}\t"
                "#{pane_top}\t#{pane_left}\t#{pane_width}\t#{pane_height}\t"
                "#{pane_current_command}\t#{pane_start_command}\t"
                f"#{{{nav_host_option}}}\t#{{{frame_host_option}}}"
            ),
        ],
        check=False,
    )
    panes: dict[str, PaneSnapshot] = {}
    panes_by_window: dict[str, list[PaneSnapshot]] = {}
    for line in raw_panes.splitlines():
        parts = line.split("\t")
        if len(parts) != 12:
            continue
        (
            pane_id,
            window_id,
            role,
            title,
            top,
            left,
            width,
            height,
            current_command,
            start_command,
            nav_host_version,
            frame_host_version,
        ) = parts
        if not pane_id or not window_id:
            continue
        pane = PaneSnapshot(
            pane_id=pane_id,
            window_id=window_id,
            role=role,
            title=title,
            top=_parse_int(top, 0),
            left=_parse_int(left, 0),
            width=_parse_int(width, 1),
            height=_parse_int(height, 1),
            current_command=current_command,
            start_command=start_command,
            nav_host_version=nav_host_version,
            frame_host_version=frame_host_version,
        )
        panes[pane_id] = pane
        panes_by_window.setdefault(window_id, []).append(pane)

    return TmuxSnapshot(windows=windows, panes=panes, panes_by_window=panes_by_window)
