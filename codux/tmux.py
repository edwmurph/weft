from __future__ import annotations

import os
import re
import shlex
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

from codux.config import CoduxConfig, render_dir
from codux.navigation import select_grid_tab
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
from codux.state import AppState, Tab, now_iso


EMPTY_WINDOW_OPTION = "@codux-empty"
SPARE_WINDOW_OPTION = "@codux-spare"
TAB_ID_OPTION = "@codux-tab-id"
LEGACY_HOST_PANE_OPTION = "@codux-host"
NAV_HOST_OPTION = "@codux-nav-host"
NAV_HOST_VERSION = "13"
STATIC_HOST_OPTION = "@codux-static-host"
STATIC_HOST_VERSION = "2"
CODEX_PANE_TITLE = "CODEX"
NAV_PANE_TITLE = "NAV"
OLD_NAV_PANE_TITLE = "Codux Nav"
FRAME_EDGE_SIZE = 1
FRAME_SIDE_WIDTH = 3
FRAME_LAYOUT_VERSION = "6"
FRAME_VERSION_OPTION = "@codux-frame-version"
NAV_FRAME_EXTRA_HEIGHT = 4
DEFAULT_NAV_FRAME_HEIGHT = "7"
PROJECT_ROOT = Path(__file__).resolve().parent.parent
BORDER_SUFFIXES = ("TOP", "BOTTOM", "LEFT", "RIGHT")
BORDER_ROLES = {
    f"{logical_role}_{suffix}"
    for logical_role in (NAV_PANE_TITLE, CODEX_PANE_TITLE)
    for suffix in BORDER_SUFFIXES
}
STALE_CODEX_COLOR_ENV = (
    "CODUX_FG_RGB",
    "CODUX_BG_RGB",
    "CODUX_COLORFGBG",
    "CODUX_CHILD_TERM",
)
TERMINAL_ENV_PASSTHROUGH = (
    "CLICOLOR",
    "COLORFGBG",
    "COLORTERM",
    "ITERM_PROFILE",
    "LC_TERMINAL",
    "LC_TERMINAL_VERSION",
    "TERM_FEATURES",
    "TERM_PROGRAM",
    "TERM_PROGRAM_VERSION",
)
ANSI_RE = re.compile(r"\033\[[0-9;?]*[A-Za-z]")


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
        self._mark_empty_window(created)
        self.select_window(created.window_id)

    def attach(self) -> None:
        subprocess.run(["tmux", "attach-session", "-t", self.session_name], check=True)

    def install_look_and_keys(self, config: CoduxConfig, codux_command: str) -> None:
        self._install_terminal_options()
        self._install_session_environment()
        self._tmux(["set-option", "-t", self.session_name, "status", "off"])
        for window_id, _ in self._codux_windows():
            self._install_window_options(window_id)
        self.repair_window_sizes()
        self._install_hooks(codux_command)
        self._install_bindings(config, codux_command)

    def create_tab_window(
        self,
        config: CoduxConfig,
        title: str,
        tab_id: str,
    ) -> CreatedWindow:
        self.ensure_session(config)
        created = self._new_detached_window(config, title, self._codex_shell_command(config))
        self._mark_tab_window(created, title, tab_id)
        return created

    def claim_spare_tab_window(
        self,
        config: CoduxConfig,
        state: AppState,
        title: str,
        tab_id: str,
    ) -> CreatedWindow:
        created = self.spare_window() or self.ensure_spare_window(config, state)
        self._mark_tab_window(created, title, tab_id)
        self.rename_window(created.window_id, title)
        self._respawn_codex_pane(config, created.content_pane_id)
        return created

    def ensure_spare_window(self, config: CoduxConfig, state: AppState) -> CreatedWindow:
        existing = self.spare_window()
        if existing is not None:
            return existing
        created = self._new_detached_window(config, "Codux Loading", self._loading_shell_command())
        self._set_window_option(created.window_id, SPARE_WINDOW_OPTION, "1")
        self._set_window_option(created.window_id, EMPTY_WINDOW_OPTION, "0")
        self.refresh_window_frame_panes(config, state, created.window_id)
        return created

    def spare_window(self) -> CreatedWindow | None:
        for window_id in self.spare_window_ids():
            if not self.window_exists(window_id):
                continue
            nav_pane_id = self.nav_pane_for_window(window_id)
            content_pane_id = self.content_pane_for_window(window_id)
            if nav_pane_id and content_pane_id:
                return CreatedWindow(window_id, content_pane_id, nav_pane_id)
        return None

    def spare_window_ids(self) -> list[str]:
        if not self.has_session():
            return []
        raw = self._tmux(
            [
                "list-windows",
                "-t",
                self.session_name,
                "-F",
                f"#{{window_id}}\t#{{{SPARE_WINDOW_OPTION}}}",
            ],
            check=False,
        )
        return [line.split("\t", 1)[0] for line in raw.splitlines() if line.endswith("\t1")]

    def recoverable_tabs(self, config: CoduxConfig) -> list[Tab]:
        if not self.has_session():
            return []
        raw = self._tmux(
            [
                "list-windows",
                "-t",
                self.session_name,
                "-F",
                (
                    f"#{{window_id}}\t#{{window_name}}\t#{{{TAB_ID_OPTION}}}\t"
                    f"#{{{EMPTY_WINDOW_OPTION}}}\t#{{{SPARE_WINDOW_OPTION}}}"
                ),
            ],
            check=False,
        )
        created_at = now_iso()
        tabs: list[Tab] = []
        for line in raw.splitlines():
            parts = line.split("\t")
            if len(parts) != 5:
                continue
            window_id, title, tab_id, empty, spare = parts
            if not window_id or not tab_id or empty == "1" or spare == "1":
                continue
            content_pane_id = self.content_pane_for_window(window_id)
            if content_pane_id is None:
                continue
            tabs.append(
                Tab(
                    id=tab_id,
                    title=title or "New Codex",
                    column=config.columns[0],
                    tmux_session=self.session_name,
                    tmux_window_id=window_id,
                    tmux_pane_id=content_pane_id,
                    created_at=created_at,
                    updated_at=created_at,
                )
            )
        return tabs

    def active_tab_id_from_tmux(self) -> str | None:
        window_id = self.active_window_id()
        if window_id is None:
            return None
        tab_id = self._window_option(window_id, TAB_ID_OPTION).strip()
        return tab_id or None

    def prepare_spare_window_async(self) -> None:
        subprocess.Popen(
            [sys.executable, "-m", "codux.cli", "_prepare-spare-window"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )

    def start_codex_pane(self, config: CoduxConfig, pane_id: str) -> None:
        self._respawn_codex_pane(config, pane_id)

    def _new_detached_window(
        self,
        config: CoduxConfig,
        title: str,
        command: str,
    ) -> CreatedWindow:
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
                command,
            ]
        ).strip()
        window_id, content_pane_id = raw.split("\t", 1)
        self._install_window_options(window_id)
        self._sync_window_to_attached_client(window_id)
        nav_pane_id = self._split_nav_pane(content_pane_id)
        self._configure_native_panes(window_id, nav_pane_id, content_pane_id)
        return CreatedWindow(
            window_id=window_id, content_pane_id=content_pane_id, nav_pane_id=nav_pane_id
        )

    def _mark_tab_window(self, created: CreatedWindow, title: str, tab_id: str) -> None:
        window_id = created.window_id
        self._set_window_option(window_id, EMPTY_WINDOW_OPTION, "0")
        self._set_window_option(window_id, SPARE_WINDOW_OPTION, "0")
        self._set_window_option(window_id, TAB_ID_OPTION, tab_id)
        self._set_pane_title(created.content_pane_id, title)
        self.rename_window(window_id, title)

    def ensure_empty_window(self, config: CoduxConfig) -> str:
        empty_window = self.empty_window_id()
        if empty_window and self.window_exists(empty_window):
            self._ensure_empty_window_titles(empty_window)
            return empty_window
        if not self.has_session():
            created = self._new_session_empty_window(config)
        else:
            created = self._new_empty_window(config)
        self._mark_empty_window(created)
        return created.window_id

    def _mark_empty_window(self, created: CreatedWindow) -> None:
        self._set_window_option(created.window_id, EMPTY_WINDOW_OPTION, "1")
        self._set_window_option(created.window_id, SPARE_WINDOW_OPTION, "0")
        self._set_window_option(created.window_id, TAB_ID_OPTION, "")
        self._set_pane_title(created.content_pane_id, CODEX_PANE_TITLE)
        self._set_pane_title(created.nav_pane_id, NAV_PANE_TITLE)

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
        if not window_id:
            return False
        result = _run_tmux(
            ["list-windows", "-t", self.session_name, "-F", "#{window_id}"],
            check=False,
        )
        if result.returncode != 0:
            return False
        return window_id in result.stdout.splitlines()

    def pane_exists(self, pane_id: str) -> bool:
        if not pane_id:
            return False
        result = _run_tmux(
            ["list-panes", "-s", "-t", self.session_name, "-F", "#{pane_id}"],
            check=False,
        )
        if result.returncode != 0:
            return False
        return pane_id in result.stdout.splitlines()

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

    def active_window_id(self) -> str | None:
        result = _run_tmux(
            ["display-message", "-p", "-t", self.session_name, "#{window_id}"],
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
        nav_pane_id, content_pane_id = self._ensure_native_window(window_id)
        if nav_pane_id:
            self._set_pane_title(nav_pane_id, NAV_PANE_TITLE)
        if content_pane_id:
            self._set_pane_title(content_pane_id, CODEX_PANE_TITLE)

    def refresh_static_panes(
        self,
        config: CoduxConfig | None = None,
        state: AppState | None = None,
    ) -> None:
        if not self.has_session():
            return
        self.repair_window_sizes()
        if config is not None and state is not None:
            write_render_files(config, state)
            self._refresh_navigation_targets(config, state)
        for window_id, is_empty in self._codux_windows():
            nav_pane_id, content_pane_id = self._ensure_native_window(window_id)
            if nav_pane_id and content_pane_id:
                self._ensure_window_frame(window_id)
            if nav_pane_id:
                self._ensure_nav_interactive_pane(nav_pane_id)
                if config is not None and state is not None:
                    self._resize_nav_frame(
                        window_id,
                        nav_pane_id,
                        nav_content_height(config, state),
                    )
            if content_pane_id:
                self._set_window_option(window_id, "@codux-codex-pane", content_pane_id)
                self._set_pane_role(content_pane_id, CODEX_PANE_TITLE)
                self._unset_pane_option(content_pane_id, LEGACY_HOST_PANE_OPTION)
                if is_empty:
                    self._ensure_static_pane(content_pane_id, render_dir() / "empty.txt")
                    self._set_pane_title(content_pane_id, CODEX_PANE_TITLE)
                elif config is not None and self._pane_needs_codex_respawn(
                    window_id,
                    content_pane_id,
                ):
                    self._respawn_codex_pane(config, content_pane_id)
            if config is not None and state is not None:
                for pane_id, role in self._border_panes(window_id).items():
                    self._refresh_border_pane(config, window_id, pane_id, role, state)

    def refresh_frame_panes(self, config: CoduxConfig, state: AppState) -> None:
        self.refresh_static_panes(config, state)

    def refresh_window_frame_panes(
        self,
        config: CoduxConfig,
        state: AppState,
        window_id: str,
    ) -> None:
        if not self.has_session() or not self.window_exists(window_id):
            return
        nav_pane_id, content_pane_id = self._ensure_native_window(window_id)
        if nav_pane_id and content_pane_id:
            self._ensure_window_frame(window_id)
        if nav_pane_id:
            self._ensure_nav_interactive_pane(nav_pane_id)
            self._resize_nav_frame(window_id, nav_pane_id, nav_content_height(config, state))
        write_render_files(config, state)
        if content_pane_id and self._window_option(window_id, EMPTY_WINDOW_OPTION) == "1":
            self._ensure_static_pane(content_pane_id, render_dir() / "empty.txt")
        for pane_id, role in self._border_panes(window_id).items():
            self._refresh_border_pane(config, window_id, pane_id, role, state)

    def resize_nav_frame_for_window(
        self,
        config: CoduxConfig,
        state: AppState,
        window_id: str,
    ) -> None:
        if not self.has_session() or not self.window_exists(window_id):
            return
        nav_pane_id = self.nav_pane_for_window(window_id)
        if nav_pane_id:
            self._resize_nav_frame(window_id, nav_pane_id, nav_content_height(config, state))

    def refresh_window_frame_colors(
        self,
        config: CoduxConfig,
        state: AppState,
        window_id: str,
    ) -> None:
        if not self.has_session() or not self.window_exists(window_id):
            return
        write_render_files(config, state)
        for pane_id, role in self._border_panes(window_id).items():
            path = self._border_render_path(pane_id)
            size = self._rendered_border_size(path)
            if size is None:
                self._refresh_border_pane(config, window_id, pane_id, role, state)
                continue
            width, height = size
            path.write_text(
                self._border_content(config, window_id, role, state, width, height),
                encoding="utf-8",
            )

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

    def _install_window_options(self, window_id: str) -> None:
        for option, value in (
            ("window-style", "fg=default,bg=default"),
            ("window-active-style", "fg=default,bg=default"),
            ("pane-active-border-style", "fg=default,bg=default"),
            ("pane-border-style", "fg=default,bg=default"),
            ("allow-set-title", "on"),
            ("automatic-rename", "off"),
            ("mode-style", "fg=default,bg=default"),
            ("pane-border-status", "off"),
            ("pane-border-lines", "spaces"),
        ):
            self._tmux(["set-window-option", "-t", window_id, option, value], check=False)

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
            if not pane_id or role in BORDER_ROLES:
                continue
            try:
                pane_top = int(top_value)
            except ValueError:
                pane_top = 0
            panes.append((pane_id, role, title, pane_top))
        if not panes:
            return None, None

        pane_ids = {pane_id for pane_id, _, _, _ in panes}
        configured_nav_pane_id = self._window_option(window_id, "@codux-nav-pane")
        configured_content_pane_id = self._window_option(window_id, "@codux-codex-pane")
        nav_pane_id = configured_nav_pane_id if configured_nav_pane_id in pane_ids else None
        content_pane_id = (
            configured_content_pane_id
            if configured_content_pane_id in pane_ids and configured_content_pane_id != nav_pane_id
            else None
        )

        nav_pane_id = nav_pane_id or next(
            (
                pane_id
                for pane_id, role, title, _ in panes
                if role == NAV_PANE_TITLE or title in {NAV_PANE_TITLE, OLD_NAV_PANE_TITLE}
            ),
            None,
        )
        if nav_pane_id is None and len(panes) > 1:
            nav_pane_id = min(panes, key=lambda pane: pane[3])[0]

        content_pane_id = content_pane_id or next(
            (
                pane_id
                for pane_id, role, _, _ in panes
                if role == CODEX_PANE_TITLE and pane_id != nav_pane_id
            ),
            None,
        )
        if content_pane_id is None:
            content_pane_id = next(
                (
                    pane_id
                    for pane_id, role, title, _ in panes
                    if pane_id != nav_pane_id
                    and role != NAV_PANE_TITLE
                    and title not in {NAV_PANE_TITLE, OLD_NAV_PANE_TITLE}
                ),
                None,
            )
        if content_pane_id is None and nav_pane_id is None:
            content_pane_id = panes[0][0]
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

    def _ensure_native_window(self, window_id: str) -> tuple[str | None, str | None]:
        self._install_window_options(window_id)
        nav_pane_id, content_pane_id = self._window_panes(window_id)
        if content_pane_id is None and nav_pane_id is not None:
            self._kill_border_panes(window_id, BORDER_ROLES)
            self._set_pane_role(nav_pane_id, CODEX_PANE_TITLE)
            self._set_pane_title(nav_pane_id, CODEX_PANE_TITLE)
            self._set_window_option(window_id, "@codux-codex-pane", nav_pane_id)
            self._set_window_option(window_id, "@codux-nav-pane", "")
            content_pane_id = nav_pane_id
            nav_pane_id = None
        if content_pane_id is None:
            return nav_pane_id, content_pane_id
        if nav_pane_id is None or nav_pane_id == content_pane_id:
            self._kill_border_panes(window_id, BORDER_ROLES)
            nav_pane_id = self._split_nav_pane(content_pane_id)
        if self._kill_duplicate_managed_panes(window_id, {nav_pane_id, content_pane_id}):
            self._kill_border_panes(window_id, BORDER_ROLES)
        self._configure_native_panes(window_id, nav_pane_id, content_pane_id)
        return nav_pane_id, content_pane_id

    def _kill_duplicate_managed_panes(self, window_id: str, keep_pane_ids: set[str]) -> bool:
        killed = False
        for pane_id in self._duplicate_managed_panes(window_id, keep_pane_ids):
            self._tmux(["kill-pane", "-t", pane_id], check=False)
            killed = True
        return killed

    def _duplicate_managed_panes(self, window_id: str, keep_pane_ids: set[str]) -> list[str]:
        raw = self._tmux(
            [
                "list-panes",
                "-t",
                window_id,
                "-F",
                "#{pane_id}\t#{@codux-role}\t#{pane_title}\t#{pane_start_command}",
            ],
            check=False,
        )
        duplicates: list[str] = []
        for line in raw.splitlines():
            pane_id, _, rest = line.partition("\t")
            role, _, rest = rest.partition("\t")
            title, _, start_command = rest.partition("\t")
            if (
                pane_id
                and pane_id not in keep_pane_ids
                and role not in BORDER_ROLES
                and self._is_managed_duplicate_pane(role, title, start_command)
            ):
                duplicates.append(pane_id)
        return duplicates

    def _is_managed_duplicate_pane(self, role: str, title: str, start_command: str) -> bool:
        if role in {NAV_PANE_TITLE, CODEX_PANE_TITLE}:
            return True
        if title in {NAV_PANE_TITLE, CODEX_PANE_TITLE, OLD_NAV_PANE_TITLE}:
            return True
        return any(
            marker in start_command
            for marker in (
                "_nav-pane",
                "_loading-pane",
                "_codex-proxy",
                "_host",
                "empty.txt",
                "nav.txt",
            )
        )

    def _configure_native_panes(
        self,
        window_id: str,
        nav_pane_id: str,
        content_pane_id: str,
    ) -> None:
        self._set_window_option(window_id, "@codux-nav-pane", nav_pane_id)
        self._set_window_option(window_id, "@codux-codex-pane", content_pane_id)
        self._set_pane_role(nav_pane_id, NAV_PANE_TITLE)
        self._set_pane_title(nav_pane_id, NAV_PANE_TITLE)
        self._unset_pane_option(nav_pane_id, LEGACY_HOST_PANE_OPTION)
        self._set_pane_role(content_pane_id, CODEX_PANE_TITLE)
        self._unset_pane_option(content_pane_id, LEGACY_HOST_PANE_OPTION)

    def _ensure_window_frame(self, window_id: str) -> None:
        self._install_window_options(window_id)
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
            ["resize-pane", "-t", nav_pane_id, "-y", str(desired_content_height)],
            check=False,
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
        for role in roles:
            if list(border_panes.values()).count(role) != 1:
                return True
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
        self._unset_pane_option(pane_id, LEGACY_HOST_PANE_OPTION)
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
        content = self._border_content(config, window_id, role, state, width, height)
        path = self._border_render_path(pane_id)
        path.write_text(content, encoding="utf-8")
        self._ensure_static_pane(pane_id, path)

    def _border_content(
        self,
        config: CoduxConfig,
        window_id: str,
        role: str,
        state: AppState,
        width: int,
        height: int,
    ) -> str:
        active = self._border_is_active(window_id, role, state)
        logical_role, _, suffix = role.partition("_")
        if suffix == "TOP":
            return render_top_border(width, logical_role, active)
        elif suffix == "BOTTOM":
            return render_bottom_border(
                width,
                active,
                self._shortcut_label(config, logical_role) if active else "",
            )
        elif suffix == "LEFT":
            return render_left_border(width, height, active)
        elif suffix == "RIGHT":
            return render_right_border(width, height, active)
        return ""

    def _rendered_border_size(self, path: Path) -> tuple[int, int] | None:
        try:
            lines = path.read_text(encoding="utf-8").splitlines()
        except OSError:
            return None
        if not lines:
            return None
        width = max(len(ANSI_RE.sub("", line)) for line in lines)
        return max(1, width), len(lines)

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

    def _pane_current_command(self, pane_id: str) -> str:
        return self._tmux(
            ["display-message", "-p", "-t", pane_id, "#{pane_current_command}"],
            check=False,
        ).strip()

    def _pane_start_command(self, pane_id: str) -> str:
        return self._tmux(
            ["display-message", "-p", "-t", pane_id, "#{pane_start_command}"],
            check=False,
        ).strip()

    def _pane_needs_codex_respawn(self, window_id: str, pane_id: str) -> bool:
        if self._window_option(window_id, SPARE_WINDOW_OPTION) == "1":
            return False
        start_command = self._pane_start_command(pane_id)
        return (
            self._pane_current_command(pane_id) == "sleep"
            or "_loading-pane" in start_command
            or "_nav-pane" in start_command
            or "_codex-proxy" in start_command
            or "_host" in start_command
            or "empty.txt" in start_command
        )

    def _respawn_codex_pane(self, config: CoduxConfig, pane_id: str) -> None:
        self._tmux(["respawn-pane", "-k", "-t", pane_id, self._codex_shell_command(config)])
        self._tmux(["clear-history", "-t", pane_id], check=False)
        self._set_pane_role(pane_id, CODEX_PANE_TITLE)
        self._unset_pane_option(pane_id, LEGACY_HOST_PANE_OPTION)
        self._unset_pane_option(pane_id, STATIC_HOST_OPTION)
        self._set_pane_title(pane_id, CODEX_PANE_TITLE)

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

    def _ensure_static_pane(self, pane_id: str, path: Path) -> None:
        current = self._tmux(
            ["show-option", "-p", "-qv", "-t", pane_id, STATIC_HOST_OPTION],
            check=False,
        ).strip()
        if current == STATIC_HOST_VERSION:
            return
        self._tmux(
            ["respawn-pane", "-k", "-t", pane_id, self._display_file_command(path)],
            check=False,
        )
        self._tmux(["clear-history", "-t", pane_id], check=False)
        self._set_pane_option(pane_id, STATIC_HOST_OPTION, STATIC_HOST_VERSION)

    def _ensure_nav_interactive_pane(self, pane_id: str) -> None:
        current = self._tmux(
            ["show-option", "-p", "-qv", "-t", pane_id, NAV_HOST_OPTION],
            check=False,
        ).strip()
        if current == NAV_HOST_VERSION:
            return
        self._tmux(
            ["respawn-pane", "-k", "-t", pane_id, self._nav_shell_command(pane_id)],
            check=False,
        )
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
                self._empty_shell_command(),
            ]
        ).strip()
        window_id, content_pane_id = raw.split("\t", 1)
        self._install_window_options(window_id)
        self._sync_window_to_attached_client(window_id)
        nav_pane_id = self._split_nav_pane(content_pane_id)
        self._configure_native_panes(window_id, nav_pane_id, content_pane_id)
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
                self._empty_shell_command(),
            ]
        ).strip()
        window_id, content_pane_id = raw.split("\t", 1)
        self._install_window_options(window_id)
        self._sync_window_to_attached_client(window_id)
        nav_pane_id = self._split_nav_pane(content_pane_id)
        self._configure_native_panes(window_id, nav_pane_id, content_pane_id)
        return CreatedWindow(window_id, content_pane_id, nav_pane_id)

    def _split_nav_pane(self, content_pane_id: str) -> str:
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
        self._tmux(["resize-pane", "-t", nav_pane_id, "-y", DEFAULT_NAV_FRAME_HEIGHT], check=False)
        return nav_pane_id

    def attached_client_size(self) -> tuple[int, int] | None:
        raw = self._tmux(
            [
                "list-clients",
                "-t",
                self.session_name,
                "-F",
                "#{client_width}\t#{client_height}",
            ],
            check=False,
        )
        sizes: list[tuple[int, int]] = []
        for line in raw.splitlines():
            width_value, _, height_value = line.partition("\t")
            try:
                width = int(width_value)
                height = int(height_value)
            except ValueError:
                continue
            sizes.append((width, height))
        return max(sizes, key=lambda size: size[0] * size[1], default=None)

    def repair_window_sizes(self) -> None:
        for window_id, _ in self._codux_windows():
            self._sync_window_to_attached_client(window_id)

    def _sync_window_to_attached_client(self, window_id: str) -> None:
        size = self.attached_client_size() or self.largest_window_size()
        if size is None:
            return
        width, height = size
        if self.window_size(window_id) == (width, height):
            return
        self._resize_window(window_id, width, height)
        self._tmux(
            ["set-window-option", "-t", window_id, "window-size", "latest"],
            check=False,
        )

    def window_size(self, window_id: str) -> tuple[int, int] | None:
        raw = self._tmux(
            [
                "display-message",
                "-p",
                "-t",
                window_id,
                "#{window_width}\t#{window_height}",
            ],
            check=False,
        )
        width_value, _, height_value = raw.strip().partition("\t")
        try:
            return int(width_value), int(height_value)
        except ValueError:
            return None

    def largest_window_size(self) -> tuple[int, int] | None:
        raw = self._tmux(
            [
                "list-windows",
                "-t",
                self.session_name,
                "-F",
                "#{window_width}\t#{window_height}",
            ],
            check=False,
        )
        sizes: list[tuple[int, int]] = []
        for line in raw.splitlines():
            width_value, _, height_value = line.partition("\t")
            try:
                width = int(width_value)
                height = int(height_value)
            except ValueError:
                continue
            sizes.append((width, height))
        return max(sizes, key=lambda size: size[0] * size[1], default=None)

    def _resize_window(self, window_id: str, width: int, height: int) -> None:
        self._tmux(
            ["resize-window", "-t", window_id, "-x", str(width), "-y", str(height)],
            check=False,
        )

    def _sleep_command(self) -> str:
        return "sh -lc 'stty -echo -icanon min 0 time 0 2>/dev/null; exec sleep 2147483647'"

    def _display_file_command(self, path: Path) -> str:
        quoted_path = shlex.quote(str(path))
        script = (
            "stty -echo -icanon min 0 time 0 2>/dev/null; "
            "last=''; "
            "while :; do "
            f"fingerprint=$(cksum {quoted_path} 2>/dev/null || true); "
            'if [ "$fingerprint" != "$last" ]; then '
            'last="$fingerprint"; '
            "printf '\\033[?25l\\033[2J\\033[H'; "
            f"if [ -f {quoted_path} ]; then cat {quoted_path}; fi; "
            "fi; "
            "sleep 0.05; "
            "done"
        )
        return f"sh -lc {shlex.quote(script)}"

    def _nav_shell_command(self, pane_id: str) -> str:
        return (
            f"cd {shlex.quote(str(PROJECT_ROOT))} && "
            f"env TMUX_PANE={shlex.quote(pane_id)} {shlex.quote(sys.executable)} -m codux.cli _nav-pane"
        )

    def _empty_shell_command(self) -> str:
        return self._display_file_command(render_dir() / "empty.txt")

    def _loading_shell_command(self) -> str:
        return (
            f"cd {shlex.quote(str(PROJECT_ROOT))} && "
            f"{shlex.quote(sys.executable)} -m codux.cli _loading-pane"
        )

    def _codex_shell_command(self, config: CoduxConfig) -> str:
        return f"exec {config.codex_command}"

    def _codux_cli_command(self) -> str:
        return f"cd {shlex.quote(str(PROJECT_ROOT))} && {shlex.quote(sys.executable)} -m codux.cli"

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
            bindings.quit,
            "Up",
            "Down",
            "Left",
            "Right",
            "Enter",
            "C-m",
            "C-a",
            "C-d",
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
                self._direct_focus_toggle_command(codux_command, bindings.focus_toggle),
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
        for key in ("Enter", "C-m"):
            self._tmux(
                [
                    "bind-key",
                    "-n",
                    key,
                    "if-shell",
                    "-F",
                    scoped_nav_pane,
                    self._direct_focus_command(
                        codux_command,
                        "#{@codux-codex-pane}",
                        "codex",
                    ),
                    f"send-keys {key}",
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

    def _direct_focus_toggle_command(self, codux_command: str, key: str) -> str:
        nav_condition = "#{&&:#{==:#{@codux-role},NAV},#{@codux-codex-pane}}"
        codex_condition = "#{&&:#{==:#{@codux-role},CODEX},#{@codux-nav-pane}}"
        focus_codex = self._direct_focus_command(codux_command, "#{@codux-codex-pane}", "codex")
        focus_nav = self._direct_focus_command(codux_command, "#{@codux-nav-pane}", "nav")
        fallback = f"send-keys {key}"
        return (
            f"if-shell -F {shlex.quote(nav_condition)} "
            f"{shlex.quote(focus_codex)} "
            f"{shlex.quote(f'if-shell -F {shlex.quote(codex_condition)} {shlex.quote(focus_nav)} {shlex.quote(fallback)}')}"
        )

    def _direct_focus_command(self, codux_command: str, target_pane: str, focus: str) -> str:
        focus_command = (
            f"{codux_command} _focus-window #{{window_id}} {focus} >/dev/null 2>&1 || true"
        )
        tmux_command = f'select-pane -t "{target_pane}" ; run-shell -b {shlex.quote(focus_command)}'
        return f"run-shell -bC {shlex.quote(tmux_command)}"

    def _install_hooks(self, codux_command: str) -> None:
        refresh_command = self._run_shell_command(f"{codux_command} _refresh")
        for hook_name in ("client-attached", "client-resized", "window-resized"):
            self._tmux(
                ["set-hook", "-t", self.session_name, hook_name, refresh_command],
                check=False,
            )

    def _run_shell_command(self, command: str) -> str:
        return f"run-shell -b {shlex.quote(f'{command} >/dev/null 2>&1')}"

    def _rename_prompt_command(self, command: str) -> str:
        return (
            f"display-popup -E -d {shlex.quote(str(PROJECT_ROOT))} "
            f"-w 72 -h 10 -s fg=default,bg=default -S fg=default,bg=default "
            f"-T Rename {shlex.quote(f'{command} _popup-rename')}"
        )

    def _help_popup_command(self, command: str) -> str:
        return (
            f"display-popup -E -d {shlex.quote(str(PROJECT_ROOT))} "
            f"-w 72 -h 22 -s fg=default,bg=default -S fg=default,bg=default "
            f"-T Codux {shlex.quote(f'{command} _popup-help')}"
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
        self._tmux(["set-option", "-s", "extended-keys", "on"], check=False)
        self._tmux(["set-option", "-s", "extended-keys-format", "xterm"], check=False)
        self._tmux(["set-option", "-s", "focus-events", "on"], check=False)
        features = self._tmux(["show-options", "-s", "-qv", "terminal-features"], check=False)
        if "*:RGB" not in features:
            self._tmux(["set-option", "-as", "terminal-features", ",*:RGB"], check=False)
        if "extkeys" not in features:
            self._tmux(["set-option", "-as", "terminal-features", ",*:extkeys"], check=False)

    def _install_session_environment(self) -> None:
        for name in STALE_CODEX_COLOR_ENV:
            self._tmux(["set-environment", "-t", self.session_name, "-u", name], check=False)

        for name in TERMINAL_ENV_PASSTHROUGH:
            if value := os.environ.get(name):
                self._tmux(["set-environment", "-t", self.session_name, name, value])
            else:
                self._tmux(["set-environment", "-t", self.session_name, "-u", name], check=False)

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
