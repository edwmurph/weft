from __future__ import annotations

import shlex
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

from codux.config import CoduxConfig, render_dir
from codux.render import (
    codex_shortcuts,
    nav_content_height,
    nav_shortcuts,
    render_bottom_border,
    render_left_border,
    render_right_border,
    render_top_border,
    write_render_files,
)
from codux.navigation import select_grid_tab
from codux.state import AppState, Tab


EMPTY_WINDOW_OPTION = "@codux-empty"
TAB_ID_OPTION = "@codux-tab-id"
HOST_PANE_OPTION = "@codux-host"
NAV_HOST_OPTION = "@codux-nav-host"
NAV_HOST_VERSION = "3"
CODEX_PANE_TITLE = "CODEX"
NAV_PANE_TITLE = "NAV"
OLD_NAV_PANE_TITLE = "Codux Nav"
FRAME_EDGE_SIZE = 1
FRAME_SIDE_WIDTH = 3
FRAME_LAYOUT_VERSION = "5"
FRAME_VERSION_OPTION = "@codux-frame-version"
NAV_FRAME_EXTRA_HEIGHT = 4
DEFAULT_NAV_FRAME_HEIGHT = "7"
BORDER_SUFFIXES = ("TOP", "BOTTOM", "LEFT", "RIGHT")
BORDER_ROLES = {
    f"{logical_role}_{suffix}"
    for logical_role in (NAV_PANE_TITLE, CODEX_PANE_TITLE)
    for suffix in BORDER_SUFFIXES
}


class TmuxError(RuntimeError):
    """Raised when a tmux command fails."""


@dataclass(frozen=True)
class CreatedWindow:
    window_id: str
    content_pane_id: str
    nav_pane_id: str


class TmuxController:
    def __init__(self, session_name: str) -> None:
        self.session_name = session_name

    @staticmethod
    def available() -> bool:
        return shutil.which("tmux") is not None

    @staticmethod
    def version_text() -> str:
        return _run_tmux(["-V"]).strip()

    def has_session(self) -> bool:
        result = _run_tmux(["has-session", "-t", self.session_name], check=False)
        return result.returncode == 0

    def ensure_session(self, config: CoduxConfig) -> None:
        if self.has_session():
            return
        created = self._new_session_empty_window(config)
        self._set_window_option(created.window_id, EMPTY_WINDOW_OPTION, "1")
        self._set_window_option(created.window_id, "@codux-nav-pane", created.nav_pane_id)
        self._set_window_option(created.window_id, "@codux-codex-pane", created.content_pane_id)
        self._set_pane_role(created.content_pane_id, CODEX_PANE_TITLE)
        self._set_pane_option(created.content_pane_id, HOST_PANE_OPTION, "1")
        self._set_pane_title(created.content_pane_id, CODEX_PANE_TITLE)
        if created.nav_pane_id != created.content_pane_id:
            self._set_pane_role(created.nav_pane_id, NAV_PANE_TITLE)
            self._set_pane_title(created.nav_pane_id, NAV_PANE_TITLE)
        self.select_window(created.window_id)

    def attach(self) -> None:
        subprocess.run(["tmux", "attach-session", "-t", self.session_name], check=True)

    def install_look_and_keys(self, config: CoduxConfig, codux_command: str) -> None:
        self._install_terminal_options()
        self._install_session_environment()
        self._tmux(["set-option", "-t", self.session_name, "status", "off"])
        self._tmux(
            [
                "set-window-option",
                "-t",
                self.session_name,
                "pane-active-border-style",
                "fg=default",
            ]
        )
        self._tmux(
            ["set-window-option", "-t", self.session_name, "pane-border-style", "fg=default"]
        )
        self._tmux(["set-window-option", "-t", self.session_name, "allow-set-title", "on"])
        self._tmux(["set-window-option", "-t", self.session_name, "automatic-rename", "off"])
        self._tmux(["set-window-option", "-t", self.session_name, "pane-border-status", "off"])
        self._tmux(["set-window-option", "-t", self.session_name, "pane-border-lines", "spaces"])
        self._install_hooks(codux_command)
        self._install_bindings(config, codux_command)

    def supports_rounded_borders(self) -> bool:
        result = _run_tmux(
            ["set-window-option", "-t", self.session_name, "pane-border-lines", "rounded"],
            check=False,
        )
        return result.returncode == 0

    def create_tab_window(
        self,
        config: CoduxConfig,
        title: str,
        tab_id: str,
    ) -> CreatedWindow:
        self.ensure_session(config)
        raw = self._tmux(
            [
                "new-window",
                "-d",
                "-P",
                "-F",
                "#{window_id}\t#{pane_id}",
                "-t",
                f"{self.session_name}:",
                "-n",
                title,
                self._host_shell_command(tab_id),
            ]
        ).strip()
        window_id, content_pane_id = raw.split("\t", 1)
        nav_pane_id = content_pane_id
        self._set_window_option(window_id, EMPTY_WINDOW_OPTION, "0")
        self._set_window_option(window_id, TAB_ID_OPTION, tab_id)
        self._set_window_option(window_id, "@codux-nav-pane", nav_pane_id)
        self._set_window_option(window_id, "@codux-codex-pane", content_pane_id)
        self._set_pane_role(content_pane_id, CODEX_PANE_TITLE)
        self._set_pane_option(content_pane_id, HOST_PANE_OPTION, "1")
        self._set_pane_title(content_pane_id, title)
        if nav_pane_id != content_pane_id:
            self._set_pane_role(nav_pane_id, NAV_PANE_TITLE)
            self._set_pane_title(nav_pane_id, NAV_PANE_TITLE)
        self.select_pane(content_pane_id)
        return CreatedWindow(
            window_id=window_id, content_pane_id=content_pane_id, nav_pane_id=nav_pane_id
        )

    def ensure_empty_window(self, config: CoduxConfig) -> str:
        empty_window = self.empty_window_id()
        if empty_window and self.window_exists(empty_window):
            self._ensure_empty_window_titles(empty_window)
            return empty_window
        if not self.has_session():
            created = self._new_session_empty_window(config)
        else:
            created = self._new_empty_window(config)
        self._set_window_option(created.window_id, EMPTY_WINDOW_OPTION, "1")
        self._set_window_option(created.window_id, "@codux-nav-pane", created.nav_pane_id)
        self._set_window_option(created.window_id, "@codux-codex-pane", created.content_pane_id)
        self._set_pane_role(created.content_pane_id, CODEX_PANE_TITLE)
        self._set_pane_option(created.content_pane_id, HOST_PANE_OPTION, "1")
        self._set_pane_title(created.content_pane_id, CODEX_PANE_TITLE)
        if created.nav_pane_id != created.content_pane_id:
            self._set_pane_role(created.nav_pane_id, NAV_PANE_TITLE)
            self._set_pane_title(created.nav_pane_id, NAV_PANE_TITLE)
        return created.window_id

    def remove_empty_windows(self) -> None:
        for window_id in self.empty_window_ids():
            if self.window_exists(window_id):
                self._tmux(["kill-window", "-t", window_id], check=False)

    def empty_window_id(self) -> str | None:
        ids = self.empty_window_ids()
        return ids[0] if ids else None

    def empty_window_ids(self) -> list[str]:
        if not self.has_session():
            return []
        raw = self._tmux(
            [
                "list-windows",
                "-t",
                self.session_name,
                "-F",
                f"#{{window_id}}\t#{{{EMPTY_WINDOW_OPTION}}}",
            ]
        )
        return [line.split("\t", 1)[0] for line in raw.splitlines() if line.endswith("\t1")]

    def window_exists(self, window_id: str) -> bool:
        result = _run_tmux(["display-message", "-p", "-t", window_id, "#{window_id}"], check=False)
        return result.returncode == 0

    def pane_exists(self, pane_id: str) -> bool:
        result = _run_tmux(["display-message", "-p", "-t", pane_id, "#{pane_id}"], check=False)
        return result.returncode == 0

    def kill_window(self, window_id: str) -> None:
        self._tmux(["kill-window", "-t", window_id], check=False)

    def rename_window(self, window_id: str, title: str) -> None:
        self._tmux(["rename-window", "-t", window_id, title], check=False)

    def select_window(self, window_id: str) -> None:
        self._tmux(["select-window", "-t", window_id], check=False)

    def select_pane(self, pane_id: str) -> None:
        self._tmux(["select-pane", "-t", pane_id], check=False)

    def set_pane_title(self, pane_id: str, title: str) -> None:
        self._set_pane_title(pane_id, title)

    def pane_title(self, pane_id: str) -> str | None:
        result = _run_tmux(["display-message", "-p", "-t", pane_id, "#{pane_title}"], check=False)
        if result.returncode != 0:
            return None
        return result.stdout.strip()

    def detach_clients(self) -> None:
        self._tmux(["detach-client", "-s", self.session_name], check=False)

    def active_pane_id(self) -> str | None:
        result = _run_tmux(
            ["display-message", "-p", "-t", self.session_name, "#{pane_id}"],
            check=False,
        )
        if result.returncode != 0:
            return None
        return result.stdout.strip()

    def nav_pane_for_window(self, window_id: str) -> str | None:
        pane_option = self._window_option(window_id, "@codux-nav-pane")
        if pane_option and self.pane_exists(pane_option):
            return pane_option
        panes = self._tmux(
            ["list-panes", "-t", window_id, "-F", "#{pane_id}\t#{@codux-role}\t#{pane_title}"],
            check=False,
        )
        for line in panes.splitlines():
            pane_id, _, rest = line.partition("\t")
            role, _, title = rest.partition("\t")
            if role == NAV_PANE_TITLE or title in {NAV_PANE_TITLE, OLD_NAV_PANE_TITLE}:
                return pane_id
        return None

    def content_pane_for_window(self, window_id: str) -> str | None:
        pane_option = self._window_option(window_id, "@codux-codex-pane")
        if pane_option and self.pane_exists(pane_option):
            return pane_option
        panes = self._tmux(
            ["list-panes", "-t", window_id, "-F", "#{pane_id}\t#{@codux-role}\t#{pane_title}"],
            check=False,
        )
        for line in panes.splitlines():
            pane_id, _, rest = line.partition("\t")
            role, _, title = rest.partition("\t")
            if role == CODEX_PANE_TITLE or (
                role not in {NAV_PANE_TITLE, *BORDER_ROLES}
                and title not in {NAV_PANE_TITLE, OLD_NAV_PANE_TITLE}
            ):
                return pane_id
        return None

    def _ensure_empty_window_titles(self, window_id: str) -> None:
        nav_pane_id, content_pane_id = self._window_panes(window_id)
        if not nav_pane_id and not content_pane_id:
            return
        if nav_pane_id:
            self._set_window_option(window_id, "@codux-nav-pane", nav_pane_id)
            if nav_pane_id != content_pane_id:
                self._set_pane_role(nav_pane_id, NAV_PANE_TITLE)
                self._set_pane_title(nav_pane_id, NAV_PANE_TITLE)
        if content_pane_id:
            self._set_window_option(window_id, "@codux-codex-pane", content_pane_id)
            self._set_pane_role(content_pane_id, CODEX_PANE_TITLE)

    def refresh_static_panes(
        self,
        config: CoduxConfig | None = None,
        state: AppState | None = None,
    ) -> None:
        if not self.has_session():
            return
        if config is not None and state is not None:
            self._refresh_navigation_targets(config, state)
        for window_id, is_empty in self._codux_windows():
            nav_pane_id, content_pane_id = self._window_panes(window_id)
            host_pane_id = content_pane_id or nav_pane_id
            if host_pane_id:
                self._set_window_option(window_id, "@codux-codex-pane", host_pane_id)
                self._set_window_option(window_id, "@codux-nav-pane", host_pane_id)
                self._set_pane_role(host_pane_id, CODEX_PANE_TITLE)
                if content_pane_id is None or nav_pane_id == content_pane_id:
                    self._set_pane_option(host_pane_id, HOST_PANE_OPTION, "1")
                    self._set_pane_title(host_pane_id, CODEX_PANE_TITLE)
                    if config is not None and state is not None:
                        write_render_files(config, state)
                    continue
                self._unset_pane_option(host_pane_id, HOST_PANE_OPTION)
            if nav_pane_id:
                self._set_window_option(window_id, "@codux-nav-pane", nav_pane_id)
                self._set_pane_role(nav_pane_id, NAV_PANE_TITLE)
                self._unset_pane_option(nav_pane_id, HOST_PANE_OPTION)
                self._set_pane_title(nav_pane_id, NAV_PANE_TITLE)
                self._ensure_nav_interactive_pane(nav_pane_id)
                if config is not None and state is not None:
                    self._resize_nav_frame(
                        window_id, nav_pane_id, nav_content_height(config, state)
                    )
            if content_pane_id:
                self._set_window_option(window_id, "@codux-codex-pane", content_pane_id)
                self._set_pane_role(content_pane_id, CODEX_PANE_TITLE)
                self._unset_pane_option(content_pane_id, HOST_PANE_OPTION)
                if not is_empty:
                    self._set_pane_title(content_pane_id, CODEX_PANE_TITLE)
            if config is not None and state is not None:
                write_render_files(config, state)
                for pane_id, role in self._border_panes(window_id).items():
                    self._refresh_border_pane(config, window_id, pane_id, role, state)

    def refresh_frame_panes(self, config: CoduxConfig, state: AppState) -> None:
        self.refresh_static_panes(config, state)

    def _codux_windows(self) -> list[tuple[str, bool]]:
        raw = self._tmux(
            [
                "list-windows",
                "-t",
                self.session_name,
                "-F",
                f"#{{window_id}}\t#{{{EMPTY_WINDOW_OPTION}}}",
            ],
            check=False,
        )
        windows: list[tuple[str, bool]] = []
        for line in raw.splitlines():
            window_id, _, empty_value = line.partition("\t")
            if window_id:
                windows.append((window_id, empty_value == "1"))
        return windows

    def _refresh_navigation_targets(self, config: CoduxConfig, state: AppState) -> None:
        for tab in state.tabs:
            nav_pane_id = self.nav_pane_for_window(tab.tmux_window_id) or tab.tmux_pane_id
            self._set_nav_target_options(
                tab,
                nav_pane_id,
                left=select_grid_tab(state.tabs, tab.id, config.columns, delta_column=-1),
                right=select_grid_tab(state.tabs, tab.id, config.columns, delta_column=1),
                up=select_grid_tab(state.tabs, tab.id, config.columns, delta_row=-1),
                down=select_grid_tab(state.tabs, tab.id, config.columns, delta_row=1),
            )

    def _set_nav_target_options(
        self,
        tab: Tab,
        nav_pane_id: str,
        *,
        left: Tab | None,
        right: Tab | None,
        up: Tab | None,
        down: Tab | None,
    ) -> None:
        targets = {
            "left": left or tab,
            "right": right or tab,
            "up": up or tab,
            "down": down or tab,
        }
        self._set_window_option(tab.tmux_window_id, "@codux-this-nav-pane", nav_pane_id)
        for direction, target in targets.items():
            target_nav_pane = self.nav_pane_for_window(target.tmux_window_id) or target.tmux_pane_id
            self._set_window_option(
                tab.tmux_window_id,
                f"@codux-nav-{direction}",
                target.tmux_window_id,
            )
            self._set_window_option(
                tab.tmux_window_id,
                f"@codux-nav-{direction}-pane",
                target_nav_pane,
            )

    def pane_width(self, pane_id: str) -> int | None:
        raw = self._tmux(["display-message", "-p", "-t", pane_id, "#{pane_width}"], check=False)
        try:
            return int(raw.strip())
        except ValueError:
            return None

    def _window_panes(self, window_id: str) -> tuple[str | None, str | None]:
        raw = self._tmux(
            [
                "list-panes",
                "-t",
                window_id,
                "-F",
                "#{pane_id}\t#{@codux-role}\t#{pane_title}\t#{pane_top}",
            ],
            check=False,
        )
        panes: list[tuple[str, str, str, int]] = []
        for line in raw.splitlines():
            pane_id, _, rest = line.partition("\t")
            role, _, rest = rest.partition("\t")
            title, _, top_value = rest.partition("\t")
            if not pane_id:
                continue
            try:
                pane_top = int(top_value)
            except ValueError:
                pane_top = 0
            panes.append((pane_id, role, title, pane_top))
        if not panes:
            return None, None

        nav_pane_id = next(
            (
                pane_id
                for pane_id, role, title, _ in panes
                if role == NAV_PANE_TITLE or title in {NAV_PANE_TITLE, OLD_NAV_PANE_TITLE}
            ),
            None,
        )
        if nav_pane_id is None and len(panes) > 1:
            nav_pane_id = min(panes, key=lambda pane: pane[3])[0]
        content_pane_id = next(
            (pane_id for pane_id, role, _, _ in panes if role == CODEX_PANE_TITLE),
            None,
        )
        if content_pane_id is None:
            content_pane_id = next(
                (
                    pane_id
                    for pane_id, role, _, _ in panes
                    if pane_id != nav_pane_id and role not in BORDER_ROLES
                ),
                None,
            )
        return nav_pane_id, content_pane_id

    def _border_panes(self, window_id: str) -> dict[str, str]:
        raw = self._tmux(
            ["list-panes", "-t", window_id, "-F", "#{pane_id}\t#{@codux-role}"],
            check=False,
        )
        panes: dict[str, str] = {}
        for line in raw.splitlines():
            pane_id, _, role = line.partition("\t")
            if role in BORDER_ROLES:
                panes[pane_id] = role
        return panes

    def _ensure_window_frame(self, window_id: str) -> None:
        if self._window_option(window_id, FRAME_VERSION_OPTION) != FRAME_LAYOUT_VERSION:
            self._kill_border_panes(window_id, BORDER_ROLES)
            self._set_window_option(window_id, FRAME_VERSION_OPTION, FRAME_LAYOUT_VERSION)
        nav_pane_id, content_pane_id = self._window_panes(window_id)
        if nav_pane_id:
            self._ensure_pane_frame(window_id, nav_pane_id, NAV_PANE_TITLE)
        if content_pane_id:
            self._ensure_pane_frame(window_id, content_pane_id, CODEX_PANE_TITLE)

    def _resize_nav_frame(
        self,
        window_id: str,
        nav_pane_id: str,
        desired_content_height: int,
    ) -> None:
        desired_content_height = max(2, desired_content_height)
        desired_frame_height = desired_content_height + NAV_FRAME_EXTRA_HEIGHT
        border_panes = self._role_to_pane(window_id)
        left_pane = border_panes.get(f"{NAV_PANE_TITLE}_LEFT")
        top_pane = border_panes.get(f"{NAV_PANE_TITLE}_TOP")
        bottom_pane = border_panes.get(f"{NAV_PANE_TITLE}_BOTTOM")
        if left_pane:
            self._tmux(
                ["resize-pane", "-t", left_pane, "-y", str(desired_frame_height)], check=False
            )
        else:
            self._tmux(
                ["resize-pane", "-t", nav_pane_id, "-y", str(desired_frame_height)],
                check=False,
            )
        if top_pane:
            self._tmux(["resize-pane", "-t", top_pane, "-y", str(FRAME_EDGE_SIZE)], check=False)
        if bottom_pane:
            self._tmux(
                ["resize-pane", "-t", bottom_pane, "-y", str(FRAME_EDGE_SIZE)],
                check=False,
            )
        self._tmux(
            ["resize-pane", "-t", nav_pane_id, "-y", str(desired_content_height)], check=False
        )
        self._normalize_frame_edges(window_id)

    def _role_to_pane(self, window_id: str) -> dict[str, str]:
        return {role: pane_id for pane_id, role in self._border_panes(window_id).items()}

    def _normalize_frame_edges(self, window_id: str) -> None:
        for role, pane_id in self._role_to_pane(window_id).items():
            if role.endswith(("_TOP", "_BOTTOM")):
                self._tmux(
                    ["resize-pane", "-t", pane_id, "-y", str(FRAME_EDGE_SIZE)],
                    check=False,
                )

    def _ensure_pane_frame(self, window_id: str, content_pane_id: str, role: str) -> None:
        expected_roles = {f"{role}_{suffix}" for suffix in BORDER_SUFFIXES}
        border_panes = self._border_panes(window_id)
        existing_roles = set(border_panes.values()) & expected_roles
        if existing_roles == expected_roles and not self._frame_needs_rebuild(
            border_panes, expected_roles
        ):
            return
        if existing_roles:
            self._kill_border_panes(window_id, expected_roles)

        width, height = self.pane_size(content_pane_id)
        if width < 12 or height < 5:
            return

        side_width = str(FRAME_SIDE_WIDTH)
        self._create_border_pane(content_pane_id, f"{role}_LEFT", ["-h", "-b", "-l", side_width])
        self._create_border_pane(content_pane_id, f"{role}_RIGHT", ["-h", "-l", side_width])
        edge_size = str(FRAME_EDGE_SIZE)
        self._create_border_pane(content_pane_id, f"{role}_TOP", ["-v", "-b", "-l", edge_size])
        self._create_border_pane(content_pane_id, f"{role}_BOTTOM", ["-v", "-l", edge_size])

    def _frame_needs_rebuild(self, border_panes: dict[str, str], roles: set[str]) -> bool:
        for pane_id, role in border_panes.items():
            if role in roles and role.endswith(("_TOP", "_BOTTOM")):
                _, height = self.pane_size(pane_id)
                if height != FRAME_EDGE_SIZE:
                    return True
        return False

    def _kill_border_panes(self, window_id: str, roles: set[str]) -> None:
        for pane_id, role in self._border_panes(window_id).items():
            if role in roles:
                self._tmux(["kill-pane", "-t", pane_id], check=False)

    def _create_border_pane(self, target_pane_id: str, role: str, split_args: list[str]) -> str:
        pane_id = self._tmux(
            [
                "split-window",
                "-d",
                *split_args,
                "-P",
                "-F",
                "#{pane_id}",
                "-t",
                target_pane_id,
                self._sleep_command(),
            ]
        ).strip()
        self._set_pane_role(pane_id, role)
        self._unset_pane_option(pane_id, HOST_PANE_OPTION)
        self._set_pane_title(pane_id, role)
        return pane_id

    def _refresh_border_pane(
        self,
        config: CoduxConfig,
        window_id: str,
        pane_id: str,
        role: str,
        state: AppState,
    ) -> None:
        width, height = self.pane_size(pane_id)
        active = self._border_is_active(window_id, role, state)
        logical_role, _, suffix = role.partition("_")
        if suffix == "TOP":
            content = render_top_border(width, logical_role, active)
        elif suffix == "BOTTOM":
            content = render_bottom_border(
                width,
                active,
                self._shortcut_label(config, logical_role) if active else "",
            )
        elif suffix == "LEFT":
            content = render_left_border(width, height, active)
        elif suffix == "RIGHT":
            content = render_right_border(width, height, active)
        else:
            return
        path = self._border_render_path(pane_id)
        path.write_text(content, encoding="utf-8")
        self._respawn_static_pane(pane_id, path)

    def _border_is_active(self, _window_id: str, role: str, state: AppState) -> bool:
        if role.startswith(f"{NAV_PANE_TITLE}_"):
            return state.focus == "nav"
        if role.startswith(f"{CODEX_PANE_TITLE}_"):
            return state.focus == "codex"
        return False

    def _shortcut_label(self, config: CoduxConfig, role: str) -> str:
        if role == NAV_PANE_TITLE:
            return nav_shortcuts(config)
        if role == CODEX_PANE_TITLE:
            return codex_shortcuts(config)
        return ""

    def _border_render_path(self, pane_id: str) -> Path:
        safe_pane_id = pane_id.replace("%", "")
        return render_dir() / f"border-{safe_pane_id}.txt"

    def pane_size(self, pane_id: str) -> tuple[int, int]:
        raw = self._tmux(
            ["display-message", "-p", "-t", pane_id, "#{pane_width}\t#{pane_height}"],
            check=False,
        )
        width_value, _, height_value = raw.strip().partition("\t")
        try:
            return int(width_value), int(height_value)
        except ValueError:
            return 1, 1

    def _respawn_static_pane(self, pane_id: str, path: Path) -> None:
        self._tmux(
            ["respawn-pane", "-k", "-t", pane_id, self._display_file_command(path)],
            check=False,
        )
        self._tmux(["clear-history", "-t", pane_id], check=False)

    def _ensure_nav_interactive_pane(self, pane_id: str) -> None:
        current = self._tmux(
            ["show-option", "-p", "-qv", "-t", pane_id, NAV_HOST_OPTION],
            check=False,
        ).strip()
        if current == NAV_HOST_VERSION:
            return
        self._tmux(["respawn-pane", "-k", "-t", pane_id, self._nav_shell_command()], check=False)
        self._tmux(["clear-history", "-t", pane_id], check=False)
        self._set_pane_option(pane_id, NAV_HOST_OPTION, NAV_HOST_VERSION)

    def _new_session_empty_window(self, config: CoduxConfig) -> CreatedWindow:
        raw = self._tmux(
            [
                "new-session",
                "-d",
                "-s",
                self.session_name,
                "-n",
                "codux",
                "-P",
                "-F",
                "#{window_id}\t#{pane_id}",
                self._host_shell_command(None),
            ]
        ).strip()
        window_id, content_pane_id = raw.split("\t", 1)
        nav_pane_id = content_pane_id
        return CreatedWindow(window_id, content_pane_id, nav_pane_id)

    def _new_empty_window(self, config: CoduxConfig) -> CreatedWindow:
        raw = self._tmux(
            [
                "new-window",
                "-d",
                "-P",
                "-F",
                "#{window_id}\t#{pane_id}",
                "-t",
                f"{self.session_name}:",
                "-n",
                "codux",
                self._host_shell_command(None),
            ]
        ).strip()
        window_id, content_pane_id = raw.split("\t", 1)
        nav_pane_id = content_pane_id
        return CreatedWindow(window_id, content_pane_id, nav_pane_id)

    def _split_nav_pane(self, config: CoduxConfig, content_pane_id: str) -> str:
        nav_pane_id = self._tmux(
            [
                "split-window",
                "-d",
                "-v",
                "-b",
                "-l",
                DEFAULT_NAV_FRAME_HEIGHT,
                "-P",
                "-F",
                "#{pane_id}",
                "-t",
                content_pane_id,
                self._display_file_command(render_dir() / "nav.txt"),
            ]
        ).strip()
        self._tmux(["select-layout", "-t", content_pane_id, "even-vertical"], check=False)
        self._tmux(["resize-pane", "-t", nav_pane_id, "-y", DEFAULT_NAV_FRAME_HEIGHT], check=False)
        return nav_pane_id

    def _sleep_command(self) -> str:
        return "sh -lc 'stty -echo -icanon min 0 time 0 2>/dev/null; exec sleep 2147483647'"

    def _display_file_command(self, path: Path) -> str:
        quoted_path = shlex.quote(str(path))
        script = (
            "stty -echo -icanon min 0 time 0 2>/dev/null; "
            "printf '\\033[?25l\\033[2J\\033[H'; "
            f"if [ -f {quoted_path} ]; then cat {quoted_path}; fi; "
            "exec sleep 2147483647"
        )
        return f"sh -lc {shlex.quote(script)}"

    def _nav_shell_command(self) -> str:
        return f"{shlex.quote(sys.executable)} -m codux.cli _nav-pane"

    def _codex_shell_command(self, config: CoduxConfig) -> str:
        proxy_command = (
            f"{shlex.quote(sys.executable)} -m codux.cli _codex-proxy -- {config.codex_command}"
        )
        return (
            "env "
            "-u NO_COLOR "
            "-u CODEX_CI "
            "-u CI "
            "-u CLICOLOR_FORCE "
            "-u FORCE_COLOR "
            "TERM=tmux-256color "
            "COLORTERM=truecolor "
            "CLICOLOR=1 "
            f"{proxy_command}"
        )

    def _host_shell_command(self, tab_id: str | None) -> str:
        command = f"{shlex.quote(sys.executable)} -m codux.cli _host"
        if tab_id is not None:
            command += f" --tab-id {shlex.quote(tab_id)}"
        return command

    def _codux_cli_command(self) -> str:
        return f"{shlex.quote(sys.executable)} -m codux.cli"

    def _install_bindings(self, config: CoduxConfig, codux_command: str) -> None:
        bindings = config.key_bindings
        scoped_session = f"#{{==:#{{session_name}},{self.session_name}}}"
        scoped_nav_pane = (
            "#{&&:"
            f"#{{==:#{{session_name}},{self.session_name}}},"
            f"#{{==:#{{@codux-role}},{NAV_PANE_TITLE}}}"
            "}"
        )
        for key in (
            bindings.new,
            bindings.prev,
            bindings.next,
            bindings.move_left,
            bindings.move_right,
            bindings.close,
            bindings.help,
            bindings.rename,
            bindings.focus_toggle,
            "Up",
            "Down",
            "Left",
            "Right",
            "Enter",
            "C-m",
            "h",
            "l",
            "H",
            "L",
        ):
            self._tmux(["unbind-key", "-n", key], check=False)
        self._tmux(
            [
                "bind-key",
                "-n",
                bindings.focus_toggle,
                "if-shell",
                "-F",
                scoped_session,
                self._run_shell_command(f"{codux_command} toggle-focus"),
                f"send-keys {bindings.focus_toggle}",
            ],
            check=False,
        )
        self._tmux(
            [
                "bind-key",
                "-n",
                bindings.quit,
                "if-shell",
                "-F",
                scoped_session,
                f"detach-client -s {shlex.quote(self.session_name)}",
                f"send-keys {shlex.quote(bindings.quit)}",
            ],
            check=False,
        )
        for key, direction in (
            ("Left", "left"),
            ("Right", "right"),
            ("Up", "up"),
            ("Down", "down"),
        ):
            self._bind_direct_nav_arrow(key, direction, scoped_nav_pane, codux_command)

    def _bind_direct_nav_arrow(
        self,
        key: str,
        direction: str,
        scoped_nav_pane: str,
        codux_command: str,
    ) -> None:
        target_window = f"#{{@codux-nav-{direction}}}"
        target_pane = f"#{{@codux-nav-{direction}-pane}}"
        condition = f"#{{&&:{scoped_nav_pane},#{{&&:{target_window},{target_pane}}}}}"
        activate_command = f"{codux_command} _activate-window {target_window} >/dev/null 2>&1"
        tmux_command = (
            f'select-window -t "{target_window}" ; '
            f'select-pane -t "{target_pane}" ; '
            f"run-shell -b {shlex.quote(activate_command)}"
        )
        self._tmux(
            [
                "bind-key",
                "-n",
                key,
                "if-shell",
                "-F",
                condition,
                f"run-shell -bC {shlex.quote(tmux_command)}",
                f"send-keys {key}",
            ],
            check=False,
        )

    def _bind_host_or_nav_key(
        self,
        key: str,
        action: str,
        scoped_nav_pane: str,
        *,
        action_is_tmux_command: bool = False,
    ) -> None:
        action_command = action if action_is_tmux_command else self._run_shell_command(action)
        self._tmux(
            [
                "bind-key",
                "-n",
                key,
                "if-shell",
                "-F",
                scoped_nav_pane,
                action_command,
                f"send-keys {key}",
            ],
            check=False,
        )

    def _install_hooks(self, codux_command: str) -> None:
        refresh_command = self._run_shell_command(f"{codux_command} _refresh")
        for hook_name in ("client-attached", "client-resized", "window-resized"):
            self._tmux(
                ["set-hook", "-t", self.session_name, hook_name, refresh_command],
                check=False,
            )

    def _fast_focus_command(self, direction: str, command: str) -> str:
        moves = "; ".join(f"select-pane -{direction}" for _ in range(3))
        return f"{moves}; {self._run_shell_command(command)}"

    def _run_shell_command(self, command: str) -> str:
        return f"run-shell -b {shlex.quote(f'{command} >/dev/null 2>&1')}"

    def _rename_prompt_command(self, command: str) -> str:
        rename_command = f'{command} rename "%%"'
        return f"command-prompt -p 'Rename tab:' {shlex.quote(self._run_shell_command(rename_command))}"

    def _help_popup_command(self, command: str) -> str:
        return f"display-popup -w 72 -h 22 -T Codux {shlex.quote(f'{command} _popup-help')}"

    def _shortcut_footer(self, config: CoduxConfig) -> str:
        bindings = config.key_bindings
        return (
            f" {bindings.new} new | arrows select | "
            f"{bindings.move_left}/{bindings.move_right} move tab | {bindings.rename} rename | "
            f"{bindings.close} close | {bindings.help} help | "
            f"{bindings.focus_toggle} focus | {bindings.quit} quit "
        )

    def _set_window_option(self, window_id: str, option: str, value: str) -> None:
        self._tmux(["set-option", "-w", "-t", window_id, option, value], check=False)

    def _window_option(self, window_id: str, option: str) -> str:
        return self._tmux(
            ["show-option", "-w", "-qv", "-t", window_id, option], check=False
        ).strip()

    def _set_pane_title(self, pane_id: str, title: str) -> None:
        self._tmux(["select-pane", "-t", pane_id, "-T", title], check=False)

    def _set_pane_role(self, pane_id: str, title: str) -> None:
        self._tmux(["set-option", "-p", "-t", pane_id, "@codux-role", title], check=False)

    def _set_pane_option(self, pane_id: str, option: str, value: str) -> None:
        self._tmux(["set-option", "-p", "-t", pane_id, option, value], check=False)

    def _unset_pane_option(self, pane_id: str, option: str) -> None:
        self._tmux(["set-option", "-p", "-u", "-t", pane_id, option], check=False)

    def _install_terminal_options(self) -> None:
        self._tmux(["set-option", "-s", "default-terminal", "tmux-256color"], check=False)
        self._tmux(["set-option", "-s", "escape-time", "0"], check=False)
        features = self._tmux(["show-options", "-s", "-qv", "terminal-features"], check=False)
        if "*:RGB" not in features:
            self._tmux(["set-option", "-as", "terminal-features", ",*:RGB"], check=False)

    def _install_session_environment(self) -> None:
        for name in ("NO_COLOR", "CODEX_CI", "CI", "CLICOLOR_FORCE", "FORCE_COLOR"):
            self._tmux(["set-environment", "-t", self.session_name, "-u", name], check=False)
        self._tmux(["set-environment", "-t", self.session_name, "COLORTERM", "truecolor"])
        self._tmux(["set-environment", "-t", self.session_name, "CLICOLOR", "1"])

    def _tmux(self, args: list[str], check: bool = True) -> str:
        result = _run_tmux(args, check=check)
        if isinstance(result, str):
            return result
        return result.stdout


def _run_tmux(args: list[str], check: bool = True):
    command = ["tmux", *args]
    result = subprocess.run(command, check=False, text=True, capture_output=True)
    if check and result.returncode != 0:
        raise TmuxError(result.stderr.strip() or f"tmux command failed: {' '.join(command)}")
    if check:
        return result.stdout
    return result
