from __future__ import annotations

import base64
import os
import shlex
import shutil
import subprocess
import time
from dataclasses import dataclass

from codux.config import CoduxConfig
from codux.launcher import PROJECT_ROOT, codux_cli_args, codux_cli_shell_command
from codux.navigation import select_grid_tab
from codux.render import (
    HELP_POPUP_WIDTH,
    codex_shortcuts,
    help_popup_height,
    nav_content_height,
    nav_shortcuts,
    render_bottom_border,
    render_left_border,
    render_right_border,
    render_top_border,
)
from codux.state import AppState, Tab, now_iso
from codux.titles import recovered_tab_title, title_uses_codex_placeholder
from codux.tmux_api import TmuxError  # noqa: F401
from codux.tmux_api import run_tmux
from codux.tmux_snapshot import TmuxSnapshot, fetch_snapshot


EMPTY_WINDOW_OPTION = "@codux-empty"
SPARE_WINDOW_OPTION = "@codux-spare"
TAB_ID_OPTION = "@codux-tab-id"
NAV_HOST_OPTION = "@codux-nav-host"
NAV_HOST_VERSION = "16"
FRAME_HOST_OPTION = "@codux-frame-host"
FRAME_HOST_VERSION = "6"
PROJECT_ROOT_OPTION = "@codux-project-root"
CODEX_PANE_TITLE = "CODEX"
NAV_PANE_TITLE = "NAV"
FRAME_EDGE_SIZE = 1
FRAME_SIDE_WIDTH = 3
FRAME_LAYOUT_VERSION = "6"
FRAME_VERSION_OPTION = "@codux-frame-version"
NAV_FRAME_EXTRA_HEIGHT = 4
DEFAULT_NAV_FRAME_HEIGHT = "7"
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
    "NO_COLOR",
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
        return run_tmux(["-V"]).strip()

    def has_session(self) -> bool:
        result = run_tmux(["has-session", "-t", self.session_name], check=False)
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
        self.set_project_root(str(PROJECT_ROOT))
        self._tmux(["set-option", "-t", self.session_name, "status", "off"])
        snapshot = self._snapshot()
        for window_id, _ in self._codux_windows(snapshot):
            self._install_window_options(window_id)
        self.repair_window_sizes()
        self._install_hooks(codux_command)
        self._install_bindings(config, codux_command)

    def set_project_root(self, project_root: str) -> None:
        self._set_session_option(PROJECT_ROOT_OPTION, project_root)

    def project_root(self) -> str | None:
        if not self.has_session():
            return None
        value = self._session_option(PROJECT_ROOT_OPTION)
        if value:
            return value
        return self._project_root_from_hooks()

    def _project_root_from_hooks(self) -> str | None:
        raw = self._tmux(["show-hooks", "-t", self.session_name], check=False)
        for line in raw.splitlines():
            project_root = _project_root_from_hook_line(line)
            if project_root:
                return project_root
        return None

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
                    title=recovered_tab_title(title),
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
            codux_cli_args("_prepare-spare-window"),
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
        if not title_uses_codex_placeholder(title):
            self._set_pane_title(created.content_pane_id, title)
        self.rename_window(window_id, title)

    def ensure_empty_window(self, config: CoduxConfig) -> str:
        empty_window = self.empty_window_id()
        if empty_window and self.window_exists(empty_window):
            self._ensure_empty_window_titles(empty_window)
            return empty_window
        spare_window = self.spare_window()
        if spare_window is not None:
            self.rename_window(spare_window.window_id, "codux")
            self._mark_empty_window(spare_window)
            return spare_window.window_id
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
        result = run_tmux(
            ["list-windows", "-t", self.session_name, "-F", "#{window_id}"],
            check=False,
        )
        if result.returncode != 0:
            return False
        return window_id in result.stdout.splitlines()

    def pane_exists(self, pane_id: str) -> bool:
        if not pane_id:
            return False
        result = run_tmux(
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
        result = run_tmux(["display-message", "-p", "-t", pane_id, "#{pane_title}"], check=False)
        if result.returncode != 0:
            return None
        return result.stdout.strip()

    def detach_clients(self) -> None:
        self._tmux(["detach-client", "-s", self.session_name], check=False)

    def kill_session(self) -> None:
        self._tmux(["kill-session", "-t", self.session_name], check=False)

    def active_pane_id(self) -> str | None:
        result = run_tmux(
            ["display-message", "-p", "-t", self.session_name, "#{pane_id}"],
            check=False,
        )
        if result.returncode != 0:
            return None
        return result.stdout.strip()

    def active_window_id(self) -> str | None:
        result = run_tmux(
            ["display-message", "-p", "-t", self.session_name, "#{window_id}"],
            check=False,
        )
        if result.returncode != 0:
            return None
        return result.stdout.strip()

    def _snapshot(self) -> TmuxSnapshot:
        return fetch_snapshot(
            self._tmux,
            session_name=self.session_name,
            empty_window_option=EMPTY_WINDOW_OPTION,
            spare_window_option=SPARE_WINDOW_OPTION,
            tab_id_option=TAB_ID_OPTION,
            frame_version_option=FRAME_VERSION_OPTION,
            nav_host_option=NAV_HOST_OPTION,
            frame_host_option=FRAME_HOST_OPTION,
        )

    def nav_pane_for_window(self, window_id: str) -> str | None:
        snapshot = self._snapshot()
        return self._nav_pane_for_window_from_snapshot(window_id, snapshot)

    def _nav_pane_for_window_from_snapshot(
        self, window_id: str, snapshot: TmuxSnapshot
    ) -> str | None:
        window = snapshot.window(window_id)
        panes = snapshot.window_panes(window_id)
        pane_ids = {pane.pane_id for pane in panes}
        if window and window.nav_pane_configured in pane_ids:
            return window.nav_pane_configured
        for pane in panes:
            if pane.role == NAV_PANE_TITLE or pane.title == NAV_PANE_TITLE:
                return pane.pane_id
        return None

    def content_pane_for_window(self, window_id: str) -> str | None:
        snapshot = self._snapshot()
        return self._content_pane_for_window_from_snapshot(window_id, snapshot)

    def _content_pane_for_window_from_snapshot(
        self, window_id: str, snapshot: TmuxSnapshot
    ) -> str | None:
        window = snapshot.window(window_id)
        panes = snapshot.window_panes(window_id)
        pane_ids = {pane.pane_id for pane in panes}
        if window and window.codex_pane_configured in pane_ids:
            configured = window.codex_pane_configured
            if configured and configured != window.nav_pane_configured:
                return configured
        nav_pane_id = window.nav_pane_configured if window else ""
        for pane in panes:
            if pane.role == CODEX_PANE_TITLE and pane.pane_id != nav_pane_id:
                return pane.pane_id
        for pane in panes:
            if pane.role not in {NAV_PANE_TITLE, *BORDER_ROLES} and pane.title != NAV_PANE_TITLE:
                return pane.pane_id
        return None

    def _ensure_empty_window_titles(self, window_id: str) -> None:
        snapshot = self._snapshot()
        nav_pane_id, content_pane_id = self._ensure_native_window(window_id, snapshot)
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
        snapshot = self._snapshot()
        if config is not None and state is not None:
            self._refresh_navigation_targets(config, state, snapshot)
        for window_id, is_empty in self._codux_windows(snapshot):
            nav_pane_id, content_pane_id = self._ensure_native_window(window_id, snapshot)
            if nav_pane_id and content_pane_id:
                if self._ensure_window_frame(window_id, snapshot):
                    snapshot = self._snapshot()
            if nav_pane_id:
                self._ensure_nav_interactive_pane(nav_pane_id, snapshot)
                if config is not None and state is not None:
                    self._resize_nav_frame(
                        window_id,
                        nav_pane_id,
                        nav_content_height(config, state),
                        snapshot,
                    )
                    snapshot = self._snapshot()
            if content_pane_id:
                self._set_window_option(window_id, "@codux-codex-pane", content_pane_id)
                self._set_pane_role(content_pane_id, CODEX_PANE_TITLE)
                if is_empty:
                    self._set_pane_title(content_pane_id, CODEX_PANE_TITLE)
                    self._render_frame_pane(
                        content_pane_id,
                        self._empty_content(content_pane_id, snapshot),
                        snapshot,
                    )
                elif config is not None and self._pane_needs_codex_respawn(
                    window_id,
                    content_pane_id,
                    snapshot,
                ):
                    self._respawn_codex_pane(config, content_pane_id)
            if config is not None and state is not None:
                for pane_id, role in self._border_panes(window_id, snapshot).items():
                    pane = snapshot.panes.get(pane_id)
                    if pane is None:
                        continue
                    self._refresh_border_pane(
                        config,
                        window_id,
                        pane_id,
                        role,
                        state,
                        width=pane.width,
                        height=pane.height,
                        snapshot=snapshot,
                    )

    def refresh_frame_panes(self, config: CoduxConfig, state: AppState) -> None:
        self.refresh_static_panes(config, state)

    def refresh_window_frame_panes(
        self,
        config: CoduxConfig,
        state: AppState,
        window_id: str,
        *,
        min_nav_content_height: int | None = None,
    ) -> None:
        if not self.has_session() or not self.window_exists(window_id):
            return
        snapshot = self._snapshot()
        nav_pane_id, content_pane_id = self._ensure_native_window(window_id, snapshot)
        if nav_pane_id and content_pane_id:
            if self._ensure_window_frame(window_id, snapshot):
                snapshot = self._snapshot()
        if nav_pane_id:
            self._ensure_nav_interactive_pane(nav_pane_id, snapshot)
            desired_nav_content_height = nav_content_height(config, state)
            if min_nav_content_height is not None:
                desired_nav_content_height = max(
                    desired_nav_content_height,
                    min_nav_content_height,
                )
            self._resize_nav_frame(
                window_id,
                nav_pane_id,
                desired_nav_content_height,
                snapshot,
            )
            snapshot = self._snapshot()
        self._refresh_navigation_targets(config, state, snapshot)
        window = snapshot.window(window_id)
        if content_pane_id and window and window.empty == "1":
            self._render_frame_pane(
                content_pane_id,
                self._empty_content(content_pane_id, snapshot),
                snapshot,
            )
        for pane_id, role in self._border_panes(window_id, snapshot).items():
            pane = snapshot.panes.get(pane_id)
            if pane is None:
                continue
            self._refresh_border_pane(
                config,
                window_id,
                pane_id,
                role,
                state,
                width=pane.width,
                height=pane.height,
                snapshot=snapshot,
            )

    def resize_nav_frame_for_window(
        self,
        config: CoduxConfig,
        state: AppState,
        window_id: str,
    ) -> None:
        if not self.has_session() or not self.window_exists(window_id):
            return
        snapshot = self._snapshot()
        nav_pane_id = self.nav_pane_for_window(window_id)
        if nav_pane_id:
            self._resize_nav_frame(
                window_id, nav_pane_id, nav_content_height(config, state), snapshot
            )

    def refresh_window_frame_colors(
        self,
        config: CoduxConfig,
        state: AppState,
        window_id: str,
    ) -> None:
        if not self.has_session() or not self.window_exists(window_id):
            return
        snapshot = self._snapshot()
        for pane_id, role in self._border_panes(window_id, snapshot).items():
            pane = snapshot.panes.get(pane_id)
            if pane is None:
                continue
            content = self._border_content(config, window_id, role, state, pane.width, pane.height)
            self._render_frame_pane(pane_id, content, snapshot)

    def _codux_windows(self, snapshot: TmuxSnapshot) -> list[tuple[str, bool]]:
        windows: list[tuple[str, bool]] = []
        for window in snapshot.windows.values():
            windows.append((window.window_id, window.empty == "1"))
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

    def _refresh_navigation_targets(
        self, config: CoduxConfig, state: AppState, snapshot: TmuxSnapshot
    ) -> None:
        for tab in state.tabs:
            nav_pane_id = (
                self._nav_pane_for_window_from_snapshot(tab.tmux_window_id, snapshot)
                or tab.tmux_pane_id
            )
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
        snapshot = self._snapshot()
        return self._window_panes_from_snapshot(window_id, snapshot)

    def _window_panes_from_snapshot(
        self, window_id: str, snapshot: TmuxSnapshot
    ) -> tuple[str | None, str | None]:
        window = snapshot.window(window_id)
        panes = [pane for pane in snapshot.window_panes(window_id) if pane.role not in BORDER_ROLES]
        if not panes:
            return None, None

        pane_ids = {pane.pane_id for pane in panes}
        configured_nav = window.nav_pane_configured if window else ""
        configured_codex = window.codex_pane_configured if window else ""
        nav_pane_id = configured_nav if configured_nav in pane_ids else None
        content_pane_id = (
            configured_codex
            if configured_codex in pane_ids and configured_codex != nav_pane_id
            else None
        )

        nav_pane_id = nav_pane_id or next(
            (
                pane.pane_id
                for pane in panes
                if pane.role == NAV_PANE_TITLE or pane.title == NAV_PANE_TITLE
            ),
            None,
        )
        if nav_pane_id is None and len(panes) > 1:
            nav_pane_id = min(panes, key=lambda pane: pane.top).pane_id

        content_pane_id = content_pane_id or next(
            (
                pane.pane_id
                for pane in panes
                if pane.role == CODEX_PANE_TITLE and pane.pane_id != nav_pane_id
            ),
            None,
        )
        if content_pane_id is None:
            content_pane_id = next(
                (
                    pane.pane_id
                    for pane in panes
                    if pane.pane_id != nav_pane_id
                    and pane.role != NAV_PANE_TITLE
                    and pane.title != NAV_PANE_TITLE
                ),
                None,
            )
        if content_pane_id is None and nav_pane_id is None:
            content_pane_id = panes[0].pane_id
        return nav_pane_id, content_pane_id

    def _border_panes(self, window_id: str, snapshot: TmuxSnapshot) -> dict[str, str]:
        panes: dict[str, str] = {}
        for pane in snapshot.window_panes(window_id):
            if pane.role in BORDER_ROLES:
                panes[pane.pane_id] = pane.role
        return panes

    def _ensure_native_window(
        self, window_id: str, snapshot: TmuxSnapshot
    ) -> tuple[str | None, str | None]:
        self._install_window_options(window_id)
        nav_pane_id, content_pane_id = self._window_panes_from_snapshot(window_id, snapshot)
        if content_pane_id is None and nav_pane_id is not None:
            self._kill_border_panes(window_id, BORDER_ROLES, snapshot)
            self._set_pane_role(nav_pane_id, CODEX_PANE_TITLE)
            self._set_pane_title(nav_pane_id, CODEX_PANE_TITLE)
            self._set_window_option(window_id, "@codux-codex-pane", nav_pane_id)
            self._set_window_option(window_id, "@codux-nav-pane", "")
            content_pane_id = nav_pane_id
            nav_pane_id = None
        if content_pane_id is None:
            return nav_pane_id, content_pane_id
        if nav_pane_id is None or nav_pane_id == content_pane_id:
            self._kill_border_panes(window_id, BORDER_ROLES, snapshot)
            nav_pane_id = self._split_nav_pane(content_pane_id, check=False)
            if nav_pane_id is None:
                return None, None
        if self._kill_duplicate_managed_panes(window_id, {nav_pane_id, content_pane_id}, snapshot):
            self._kill_border_panes(window_id, BORDER_ROLES, snapshot)
        self._configure_native_panes(window_id, nav_pane_id, content_pane_id)
        return nav_pane_id, content_pane_id

    def _kill_duplicate_managed_panes(
        self, window_id: str, keep_pane_ids: set[str], snapshot: TmuxSnapshot
    ) -> bool:
        killed = False
        for pane_id in self._duplicate_managed_panes_from_snapshot(
            window_id, keep_pane_ids, snapshot
        ):
            self._tmux(["kill-pane", "-t", pane_id], check=False)
            killed = True
        return killed

    def _duplicate_managed_panes_from_snapshot(
        self, window_id: str, keep_pane_ids: set[str], snapshot: TmuxSnapshot
    ) -> list[str]:
        duplicates: list[str] = []
        for pane in snapshot.window_panes(window_id):
            pane_id = pane.pane_id
            role = pane.role
            title = pane.title
            start_command = pane.start_command
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
        if title in {NAV_PANE_TITLE, CODEX_PANE_TITLE}:
            return True
        return any(
            marker in start_command
            for marker in (
                "_nav-pane",
                "_loading-pane",
                "_frame-pane",
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
        self._set_pane_role(content_pane_id, CODEX_PANE_TITLE)

    def _ensure_window_frame(self, window_id: str, snapshot: TmuxSnapshot) -> bool:
        changed = False
        self._install_window_options(window_id)
        window = snapshot.window(window_id)
        if not window or window.frame_version != FRAME_LAYOUT_VERSION:
            self._kill_border_panes(window_id, BORDER_ROLES, snapshot)
            self._set_window_option(window_id, FRAME_VERSION_OPTION, FRAME_LAYOUT_VERSION)
            snapshot = self._snapshot()
            changed = True
        nav_pane_id, content_pane_id = self._window_panes_from_snapshot(window_id, snapshot)
        if nav_pane_id:
            changed = (
                self._ensure_pane_frame(window_id, nav_pane_id, NAV_PANE_TITLE, snapshot) or changed
            )
        if content_pane_id:
            changed = (
                self._ensure_pane_frame(window_id, content_pane_id, CODEX_PANE_TITLE, snapshot)
                or changed
            )
        return changed

    def _resize_nav_frame(
        self,
        window_id: str,
        nav_pane_id: str,
        desired_content_height: int,
        snapshot: TmuxSnapshot,
    ) -> None:
        desired_content_height = max(2, desired_content_height)
        desired_frame_height = desired_content_height + NAV_FRAME_EXTRA_HEIGHT
        border_panes = self._role_to_pane(window_id, snapshot)
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
        self._normalize_frame_edges(window_id, snapshot)

    def _role_to_pane(self, window_id: str, snapshot: TmuxSnapshot) -> dict[str, str]:
        return {role: pane_id for pane_id, role in self._border_panes(window_id, snapshot).items()}

    def _normalize_frame_edges(self, window_id: str, snapshot: TmuxSnapshot) -> None:
        for role, pane_id in self._role_to_pane(window_id, snapshot).items():
            if role.endswith(("_TOP", "_BOTTOM")):
                self._tmux(
                    ["resize-pane", "-t", pane_id, "-y", str(FRAME_EDGE_SIZE)],
                    check=False,
                )

    def _ensure_pane_frame(
        self, window_id: str, content_pane_id: str, role: str, snapshot: TmuxSnapshot
    ) -> bool:
        expected_roles = {f"{role}_{suffix}" for suffix in BORDER_SUFFIXES}
        border_panes = self._border_panes(window_id, snapshot)
        existing_roles = set(border_panes.values()) & expected_roles
        if existing_roles == expected_roles and not self._frame_needs_rebuild(
            border_panes, expected_roles, snapshot
        ):
            return False
        changed = False
        if existing_roles:
            self._kill_border_panes(window_id, expected_roles, snapshot)
            changed = True

        pane = snapshot.panes.get(content_pane_id)
        width, height = (pane.width, pane.height) if pane else self.pane_size(content_pane_id)
        if role == NAV_PANE_TITLE and height < 5:
            self._tmux(
                ["resize-pane", "-t", content_pane_id, "-y", DEFAULT_NAV_FRAME_HEIGHT],
                check=False,
            )
            width, height = self.pane_size(content_pane_id)
            changed = True
        if width < 12 or height < 5:
            return changed

        side_width = str(FRAME_SIDE_WIDTH)
        self._create_border_pane(content_pane_id, f"{role}_LEFT", ["-h", "-b", "-l", side_width])
        self._create_border_pane(content_pane_id, f"{role}_RIGHT", ["-h", "-l", side_width])
        edge_size = str(FRAME_EDGE_SIZE)
        self._create_border_pane(content_pane_id, f"{role}_TOP", ["-v", "-b", "-l", edge_size])
        self._create_border_pane(content_pane_id, f"{role}_BOTTOM", ["-v", "-l", edge_size])
        return True

    def _frame_needs_rebuild(
        self, border_panes: dict[str, str], roles: set[str], snapshot: TmuxSnapshot
    ) -> bool:
        for role in roles:
            if list(border_panes.values()).count(role) != 1:
                return True
        for pane_id, role in border_panes.items():
            if role in roles and role.endswith(("_TOP", "_BOTTOM")):
                pane = snapshot.panes.get(pane_id)
                _, height = (pane.width, pane.height) if pane else self.pane_size(pane_id)
                if height != FRAME_EDGE_SIZE:
                    return True
        return False

    def _kill_border_panes(self, window_id: str, roles: set[str], snapshot: TmuxSnapshot) -> None:
        for pane_id, role in self._border_panes(window_id, snapshot).items():
            if role in roles:
                self._tmux(["kill-pane", "-t", pane_id], check=False)

    def _create_border_pane(
        self, target_pane_id: str, role: str, split_args: list[str]
    ) -> str | None:
        result = _run_tmux(
            [
                "split-window",
                "-d",
                *split_args,
                "-P",
                "-F",
                "#{pane_id}",
                "-t",
                target_pane_id,
                self._frame_pane_shell_command(),
            ],
            check=False,
        )
        if result.returncode != 0:
            return None
        pane_id = result.stdout.strip()
        if not pane_id:
            return None
        self._set_pane_role(pane_id, role)
        self._set_pane_title(pane_id, role)
        return pane_id

    def _refresh_border_pane(
        self,
        config: CoduxConfig,
        window_id: str,
        pane_id: str,
        role: str,
        state: AppState,
        *,
        width: int,
        height: int,
        snapshot: TmuxSnapshot,
    ) -> None:
        content = self._border_content(config, window_id, role, state, width, height)
        self._render_frame_pane(pane_id, content, snapshot)

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
            return render_bottom_border(width, active, self._shortcut_label(config, logical_role))
        elif suffix == "LEFT":
            return render_left_border(width, height, active)
        elif suffix == "RIGHT":
            return render_right_border(width, height, active)
        return ""

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

    def _pane_needs_codex_respawn(
        self, window_id: str, pane_id: str, snapshot: TmuxSnapshot
    ) -> bool:
        window = snapshot.window(window_id)
        if window and window.spare == "1":
            return False
        pane = snapshot.panes.get(pane_id)
        start_command = pane.start_command if pane else self._pane_start_command(pane_id)
        current_command = pane.current_command if pane else self._pane_current_command(pane_id)
        return (
            current_command == "sleep"
            or "_loading-pane" in start_command
            or "_nav-pane" in start_command
            or "_frame-pane" in start_command
        )

    def _respawn_codex_pane(self, config: CoduxConfig, pane_id: str) -> None:
        self._tmux(["respawn-pane", "-k", "-t", pane_id, self._codex_shell_command(config)])
        self._tmux(["clear-history", "-t", pane_id], check=False)
        self._set_pane_role(pane_id, CODEX_PANE_TITLE)

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

    def _ensure_nav_interactive_pane(self, pane_id: str, snapshot: TmuxSnapshot) -> None:
        pane = snapshot.panes.get(pane_id)
        current = pane.nav_host_version if pane else ""
        if current == NAV_HOST_VERSION:
            return
        self._tmux(
            ["respawn-pane", "-k", "-t", pane_id, self._nav_shell_command(pane_id)],
            check=False,
        )
        self._tmux(["clear-history", "-t", pane_id], check=False)
        self._set_pane_option(pane_id, NAV_HOST_OPTION, NAV_HOST_VERSION)

    def _ensure_frame_pane(self, pane_id: str, snapshot: TmuxSnapshot) -> None:
        pane = snapshot.panes.get(pane_id)
        current = pane.frame_host_version if pane else ""
        if current == FRAME_HOST_VERSION:
            return
        self._tmux(
            ["respawn-pane", "-k", "-t", pane_id, self._frame_pane_shell_command()],
            check=False,
        )
        self._tmux(["clear-history", "-t", pane_id], check=False)
        self._wait_for_frame_pane_ready(pane_id)

    def _wait_for_frame_pane_ready(self, pane_id: str) -> None:
        deadline = time.monotonic() + 0.5
        while time.monotonic() < deadline:
            if self._pane_option(pane_id, FRAME_HOST_OPTION) == FRAME_HOST_VERSION:
                return
            time.sleep(0.01)

    def _render_frame_pane(self, pane_id: str, content: str, snapshot: TmuxSnapshot) -> None:
        self._ensure_frame_pane(pane_id, snapshot)
        payload = base64.b64encode(content.encode("utf-8")).decode("ascii")
        self._tmux(["send-keys", "-l", "-t", pane_id, f"CODUX_FRAME:{payload}"], check=False)
        self._tmux(["send-keys", "-t", pane_id, "Enter"], check=False)

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

    def _split_nav_pane(self, content_pane_id: str, *, check: bool = True) -> str | None:
        command = [
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
            self._frame_pane_shell_command(),
        ]
        result = _run_tmux(command, check=check)
        if isinstance(result, str):
            nav_pane_id = result.strip()
        else:
            if result.returncode != 0:
                return None
            nav_pane_id = result.stdout.strip()
        if not nav_pane_id:
            return None
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
        snapshot = self._snapshot()
        for window_id, _ in self._codux_windows(snapshot):
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

    def _frame_pane_shell_command(self) -> str:
        return f"stty -echo 2>/dev/null || true; exec {codux_cli_shell_command('_frame-pane')}"

    def _nav_shell_command(self, pane_id: str) -> str:
        return f"env TMUX_PANE={shlex.quote(pane_id)} {codux_cli_shell_command('_nav-pane')}"

    def _empty_shell_command(self) -> str:
        return self._frame_pane_shell_command()

    def _empty_content(
        self, pane_id: str | None = None, snapshot: TmuxSnapshot | None = None
    ) -> str:
        from codux.render import render_empty_state

        pane = snapshot.panes.get(pane_id) if pane_id and snapshot else None
        return render_empty_state(pane.width if pane else None, pane.height if pane else None)

    def _loading_shell_command(self) -> str:
        return codux_cli_shell_command("_loading-pane")

    def _codex_shell_command(self, config: CoduxConfig) -> str:
        return f"unset NO_COLOR; exec {config.codex_command}"

    def _codux_cli_command(self) -> str:
        return codux_cli_shell_command()

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
        activate_command = (
            f"{codux_command} _activate-window {target_window} >/dev/null 2>&1 || true"
        )
        tmux_command = (
            f'select-window -t "{target_window}" ; '
            f'select-pane -t "{target_pane}" ; '
            f"run-shell {shlex.quote(activate_command)}"
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
            f"-w {HELP_POPUP_WIDTH} -h {help_popup_height(CoduxConfig())} "
            f"-s fg=default,bg=default -S fg=default,bg=default "
            f"-T Codux {shlex.quote(f'{command} _popup-help')}"
        )

    def _set_window_option(self, window_id: str, option: str, value: str) -> None:
        self._tmux(["set-option", "-w", "-t", window_id, option, value], check=False)

    def _window_option(self, window_id: str, option: str) -> str:
        return self._tmux(
            ["show-option", "-w", "-qv", "-t", window_id, option], check=False
        ).strip()

    def _set_session_option(self, option: str, value: str) -> None:
        self._tmux(["set-option", "-t", self.session_name, option, value], check=False)

    def _session_option(self, option: str) -> str:
        return self._tmux(
            ["show-option", "-qv", "-t", self.session_name, option], check=False
        ).strip()

    def _set_pane_title(self, pane_id: str, title: str) -> None:
        self._tmux(["select-pane", "-t", pane_id, "-T", title], check=False)

    def _set_pane_role(self, pane_id: str, title: str) -> None:
        self._tmux(["set-option", "-p", "-t", pane_id, "@codux-role", title], check=False)

    def _set_pane_option(self, pane_id: str, option: str, value: str) -> None:
        self._tmux(["set-option", "-p", "-t", pane_id, option, value], check=False)

    def _pane_option(self, pane_id: str, option: str) -> str:
        return self._tmux(["show-option", "-p", "-qv", "-t", pane_id, option], check=False).strip()

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

    def _tmux(self, args: list[str], check: bool = True) -> str:
        result = _run_tmux(args, check=check)
        if isinstance(result, str):
            return result
        return result.stdout


def _run_tmux(args: list[str], check: bool = True):
    return run_tmux(args, check=check)


def _project_root_from_hook_line(line: str) -> str | None:
    try:
        fields = shlex.split(line)
    except ValueError:
        return None
    for field in fields:
        project_root = _project_root_from_codux_command(field)
        if project_root:
            return project_root
    return None


def _project_root_from_codux_command(command: str) -> str | None:
    try:
        tokens = shlex.split(command)
    except ValueError:
        return None
    if "codux" not in tokens:
        return None
    for option in ("--directory", "--project"):
        if option not in tokens:
            continue
        index = tokens.index(option)
        if index + 1 < len(tokens):
            return tokens[index + 1]
    return None
