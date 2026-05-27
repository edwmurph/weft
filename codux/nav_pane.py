from __future__ import annotations

import fcntl
import os
import select
import signal
import struct
import subprocess
import sys
import termios
import time
import tty
import uuid
from dataclasses import replace

from codux.config import ensure_config
from codux.launcher import PROJECT_ROOT, codux_cli_args, codux_cli_shell_command
from codux.navigation import select_grid_tab
from codux.render import HELP_POPUP_WIDTH, help_popup_height, nav_content_height, render_nav
from codux.state import (
    AppState,
    StateLockTimeout,
    StateStore,
    Tab,
    now_iso,
    state_after_closing_tab,
)
from codux.theme import Theme
from codux.title_sync import state_with_live_codex_titles
from codux.titles import CODEX_TITLE_TEMPLATE
from codux.tmux import TmuxController


RESET = Theme().reset
HIDE_CURSOR = "\033[?25l"
POPUP_STYLE = "fg=default,bg=default"
POPUP_BORDER_STYLE = "fg=default,bg=default"
SESSIONS_POPUP_WIDTH = 96
SESSIONS_POPUP_HEIGHT = 18
TITLE_POLL_INTERVAL = 0.5


def run_nav_pane() -> int:
    pane = NavPane()
    return pane.run()


class NavPane:
    def __init__(self) -> None:
        self.config = ensure_config()
        self.store = StateStore()
        self.state = self.store.ensure()
        self.tmux = TmuxController(self.config.tmux_session)
        self.pane_id = os.environ.get("TMUX_PANE", "")
        self.window_id = self.current_window_id()
        self.nav_panes_by_window: dict[str, str] = {}
        self.running = True
        self.last_payload = ""
        self.last_render = 0.0
        self.last_state_mtime = 0.0
        self.last_title_poll = 0.0
        self.skip_next_render = False
        self.resize_pending = False
        self.refresh_nav_pane_cache()

    def run(self) -> int:
        stdin_fd = sys.stdin.fileno()
        stdout_fd = sys.stdout.fileno()
        old_termios = None
        if sys.stdin.isatty():
            old_termios = termios.tcgetattr(stdin_fd)
            tty.setraw(stdin_fd)
        old_winch = signal.getsignal(signal.SIGWINCH)

        def handle_winch(_signum, _frame) -> None:
            self.resize_pending = True

        signal.signal(signal.SIGWINCH, handle_winch)
        try:
            os.write(stdout_fd, b"\033[?7l\033[?25l\033[2J")
            self.render(force=True)
            while self.running:
                readable, _, _ = select.select([stdin_fd], [], [], 0.02)
                if stdin_fd in readable:
                    data = os.read(stdin_fd, 8192)
                    if not data:
                        break
                    self.handle_input(data)
                if self.resize_pending:
                    self.resize_pending = False
                    self.render(force=True)
                self.render_if_state_changed()
        finally:
            os.write(stdout_fd, f"{RESET}\033[?7h\033[?25h".encode())
            signal.signal(signal.SIGWINCH, old_winch)
            if old_termios is not None:
                termios.tcsetattr(stdin_fd, termios.TCSADRAIN, old_termios)
        return 0

    def handle_input(self, data: bytes) -> None:
        bindings = self.config.key_bindings
        for key in nav_keys(data):
            if key == bindings.quit:
                subprocess.run(
                    ["tmux", "detach-client", "-s", self.config.tmux_session], check=False
                )
            elif key == bindings.focus_toggle:
                self.focus_codex()
            elif key == bindings.prev:
                self.select_grid(delta_column=-1)
            elif key == bindings.next:
                self.select_grid(delta_column=1)
            elif key == "Up":
                self.select_grid(delta_row=-1)
            elif key == "Down":
                self.select_grid(delta_row=1)
            elif key == bindings.move_left:
                self.move_column(-1)
            elif key == bindings.move_right:
                self.move_column(1)
            elif key == "Enter":
                self.focus_codex()
            elif key == bindings.new:
                self.new_tab()
                self.skip_next_render = True
            elif key == bindings.close:
                self.close_active_tab()
            elif key == bindings.rename:
                self.rename_prompt()
            elif key == bindings.sessions:
                self.sessions_popup()
            elif key == bindings.help:
                self.help_popup()
        if self.skip_next_render:
            self.skip_next_render = False
        else:
            self.render(force=True)

    def select_grid(self, delta_column: int = 0, delta_row: int = 0) -> None:
        state = self.current_state_for_input()
        target = select_grid_tab(
            state.tabs,
            state.active_tab_id,
            self.config.columns,
            delta_column=delta_column,
            delta_row=delta_row,
        )
        if target is None:
            return
        if target.tmux_window_id != self.window_id:
            self.select_nav_for_window(target.tmux_window_id)
            self.skip_next_render = True
        self.state = replace(state, active_tab_id=target.id, focus="nav")
        self.activate_window_async(target.tmux_window_id)

    def move_column(self, delta: int) -> None:
        state = self.store.read()
        target = state.active_tab
        if target is None:
            return
        previous_height = nav_content_height(self.config, state)
        current_index = (
            self.config.columns.index(target.column) if target.column in self.config.columns else 0
        )
        next_index = max(0, min(len(self.config.columns) - 1, current_index + delta))
        next_column = self.config.columns[next_index]
        if next_column == target.column:
            return

        def mutate(current: AppState) -> AppState:
            return replace(
                current,
                tabs=[
                    tab.with_updates(column=next_column) if tab.id == target.id else tab
                    for tab in current.tabs
                ],
                focus="nav",
            )

        self.state = self.store.update(mutate)
        next_height = nav_content_height(self.config, self.state)
        pinned_height = max(previous_height, next_height)
        if next_height > previous_height:
            self.tmux.refresh_window_frame_panes(
                self.config,
                self.state,
                target.tmux_window_id,
                min_nav_content_height=pinned_height,
            )
            self.render_snapshot(self.state)
        else:
            self.render_snapshot(self.state)
            self.tmux.refresh_window_frame_panes(
                self.config,
                self.state,
                target.tmux_window_id,
                min_nav_content_height=pinned_height,
            )
        self.render_snapshot(self.state)
        self.select_nav_for_window(target.tmux_window_id)
        self.skip_next_render = True

    def focus_codex(self) -> None:
        state = self.store.update(lambda current: replace(current, focus="codex"))
        active_window = (
            state.active_tab.tmux_window_id if state.active_tab else self.tmux.empty_window_id()
        )
        if not active_window:
            return
        pane_id = self.tmux.content_pane_for_window(active_window)
        if pane_id:
            self.tmux.select_pane(pane_id)
        self.refresh_static_panes_async()

    def close_active_tab(self) -> None:
        state = self.current_state_for_input()
        target = state.active_tab
        if target is None:
            return

        def mutate(current: AppState) -> AppState:
            current = replace(current, active_tab_id=target.id, focus="nav")
            return state_after_closing_tab(current, target.id)

        updated = self.store.update(mutate)
        self.state = updated
        next_tab = updated.active_tab
        if next_tab is not None:
            self.select_nav_for_window(next_tab.tmux_window_id)
        self.skip_next_render = True
        self.run_cli_async("_finish-close-window", target.tmux_window_id)

    def new_tab(self) -> None:
        tab_id = uuid.uuid4().hex[:8]
        title = CODEX_TITLE_TEMPLATE
        created_at = now_iso()
        current = self.store.read()
        pending_tab = Tab(
            id=tab_id,
            title=title,
            column=self.config.columns[0],
            tmux_session=self.config.tmux_session,
            tmux_window_id=self.window_id,
            tmux_pane_id=self.pane_id,
            created_at=created_at,
            updated_at=created_at,
        )
        self.render_snapshot(
            AppState(tabs=[*current.tabs, pending_tab], active_tab_id=tab_id, focus="codex")
        )

        created = self.tmux.claim_spare_tab_window(self.config, current, title, tab_id)
        tab = Tab(
            id=tab_id,
            title=title,
            column=self.config.columns[0],
            tmux_session=self.config.tmux_session,
            tmux_window_id=created.window_id,
            tmux_pane_id=created.content_pane_id,
            created_at=created_at,
            updated_at=created_at,
        )

        def mutate(current: AppState) -> AppState:
            return AppState(tabs=[*current.tabs, tab], active_tab_id=tab.id, focus="codex")

        self.state = self.store.update(mutate)
        self.tmux.refresh_window_frame_panes(self.config, self.state, tab.tmux_window_id)
        self.refresh_nav_pane_cache()
        self.tmux.select_window(tab.tmux_window_id)
        self.tmux.select_pane(created.content_pane_id)
        self.tmux.refresh_window_frame_colors(self.config, self.state, tab.tmux_window_id)
        self.tmux.prepare_spare_window_async()
        self.refresh_static_panes_async()
        self.tmux.remove_empty_windows()

    def select_nav_for_window(self, window_id: str) -> None:
        pane_id = self.nav_panes_by_window.get(window_id)
        if not pane_id:
            self.refresh_nav_pane_cache()
            pane_id = self.nav_panes_by_window.get(window_id)
        if pane_id:
            subprocess.run(
                ["tmux", "select-window", "-t", window_id, ";", "select-pane", "-t", pane_id],
                check=False,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
        else:
            subprocess.run(
                ["tmux", "select-window", "-t", window_id],
                check=False,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

    def current_state_for_input(self) -> AppState:
        tab = next(
            (tab for tab in self.state.tabs if tab.tmux_window_id == self.window_id),
            None,
        )
        if tab is not None and self.state.active_tab_id != tab.id:
            self.state = replace(self.state, active_tab_id=tab.id, focus="nav")
        return self.state

    def current_window_id(self) -> str:
        if not self.pane_id:
            return ""
        result = subprocess.run(
            ["tmux", "display-message", "-p", "-t", self.pane_id, "#{window_id}"],
            check=False,
            text=True,
            capture_output=True,
        )
        return result.stdout.strip() if result.returncode == 0 else ""

    def refresh_nav_pane_cache(self) -> None:
        result = subprocess.run(
            [
                "tmux",
                "list-windows",
                "-t",
                self.config.tmux_session,
                "-F",
                "#{window_id}\t#{@codux-nav-pane}",
            ],
            check=False,
            text=True,
            capture_output=True,
        )
        if result.returncode != 0:
            return
        self.nav_panes_by_window = {
            window_id: pane_id
            for line in result.stdout.splitlines()
            if (window_id := line.partition("\t")[0])
            if (pane_id := line.partition("\t")[2])
        }

    def refresh_static_panes_async(self) -> None:
        if not self.tmux.has_session():
            return
        subprocess.Popen(
            codux_cli_args("_refresh"),
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )

    def activate_window_async(self, window_id: str) -> None:
        subprocess.Popen(
            codux_cli_args("_activate-window", window_id),
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )

    def run_cli(self, *args: str) -> None:
        subprocess.run(
            codux_cli_args(*args),
            check=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

    def run_cli_async(self, *args: str) -> None:
        subprocess.Popen(
            codux_cli_args(*args),
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )

    def rename_prompt(self) -> None:
        command = codux_cli_shell_command("_popup-rename")
        subprocess.run(
            [
                "tmux",
                "display-popup",
                "-E",
                "-d",
                str(PROJECT_ROOT),
                "-w",
                "72",
                "-h",
                "10",
                "-s",
                POPUP_STYLE,
                "-S",
                POPUP_BORDER_STYLE,
                "-T",
                "Rename",
                command,
            ],
            check=False,
        )

    def help_popup(self) -> None:
        command = codux_cli_shell_command("_popup-help")
        subprocess.run(
            [
                "tmux",
                "display-popup",
                "-E",
                "-d",
                str(PROJECT_ROOT),
                "-w",
                str(HELP_POPUP_WIDTH),
                "-h",
                str(help_popup_height(self.config)),
                "-s",
                POPUP_STYLE,
                "-S",
                POPUP_BORDER_STYLE,
                "-T",
                "Codux",
                command,
            ],
            check=False,
        )

    def sessions_popup(self) -> None:
        command = codux_cli_shell_command("_popup-sessions")
        subprocess.run(
            [
                "tmux",
                "display-popup",
                "-E",
                "-d",
                str(PROJECT_ROOT),
                "-w",
                str(SESSIONS_POPUP_WIDTH),
                "-h",
                str(SESSIONS_POPUP_HEIGHT),
                "-s",
                POPUP_STYLE,
                "-S",
                POPUP_BORDER_STYLE,
                "-T",
                "Codux Sessions",
                command,
            ],
            check=False,
        )

    def render_if_state_changed(self) -> None:
        now = time.monotonic()
        if now - self.last_render < 0.02:
            return
        try:
            mtime = self.store.path.stat().st_mtime
        except OSError:
            mtime = 0.0
        titles_changed = self.poll_live_titles(now)
        if mtime != self.last_state_mtime or titles_changed:
            self.render(force=True)

    def poll_live_titles(self, now: float | None = None) -> bool:
        now = time.monotonic() if now is None else now
        if now - self.last_title_poll < TITLE_POLL_INTERVAL:
            return False
        self.last_title_poll = now
        try:
            current = self.store.read()
        except StateLockTimeout:
            return False
        updated = state_with_live_codex_titles(current, self.tmux.pane_title)
        if updated == current:
            return False
        try:
            written = self.store.update(lambda latest: updated if latest == current else latest)
        except StateLockTimeout:
            return False
        self.state = written
        return written == updated

    def render(self, force: bool = False) -> None:
        now = time.monotonic()
        if not force and now - self.last_render < 0.03:
            return
        self.last_render = now
        try:
            state = self.store.read()
        except StateLockTimeout:
            return
        self.state = state
        try:
            self.last_state_mtime = self.store.path.stat().st_mtime
        except OSError:
            self.last_state_mtime = 0.0
        self.render_snapshot(state)

    def render_snapshot(self, state: AppState) -> None:
        width, height = terminal_size()
        lines = render_nav(self.config, state, width).splitlines()
        visible_lines = [visible_pad(line, width) for line in lines[:height]]
        if len(visible_lines) < height:
            visible_lines.extend([" " * width] * (height - len(visible_lines)))
        payload = (
            HIDE_CURSOR
            + "\033[2J\033[H"
            + "".join(f"\033[{index};1H{line}" for index, line in enumerate(visible_lines, 1))
        )
        if payload != self.last_payload:
            os.write(sys.stdout.fileno(), payload.encode())
            self.last_payload = payload


def nav_keys(data: bytes) -> list[str]:
    mapping = {
        b"\x1b[D": "Left",
        b"\x1bOD": "Left",
        b"\x1b[C": "Right",
        b"\x1bOC": "Right",
        b"\x1b[A": "Up",
        b"\x1bOA": "Up",
        b"\x1b[B": "Down",
        b"\x1bOB": "Down",
        b"\x1b[1;2D": "S-Left",
        b"\x1b[1;2C": "S-Right",
        b"\x1b[1;2A": "S-Up",
        b"\x1b[1;2B": "S-Down",
        b"\x04": "C-d",
        b"\x11": "C-q",
        b"\r": "Enter",
        b"\n": "Enter",
    }
    keys: list[str] = []
    index = 0
    ordered = sorted(mapping.items(), key=lambda item: len(item[0]), reverse=True)
    while index < len(data):
        for raw, key in ordered:
            if data.startswith(raw, index):
                keys.append(key)
                index += len(raw)
                break
        else:
            char = data[index : index + 1]
            if 32 <= char[0] <= 126:
                keys.append(char.decode())
            index += 1
    return keys


def visible_pad(line: str, width: int) -> str:
    visible = ansi_visible_width(line)
    if visible >= width:
        return line
    return line + (" " * (width - visible))


def ansi_visible_width(text: str) -> int:
    width = 0
    index = 0
    while index < len(text):
        if text[index] == "\033":
            end = index + 1
            while end < len(text) and text[end] not in "mABCDEFGHJKSTfhl":
                end += 1
            index = min(len(text), end + 1)
            continue
        width += 1
        index += 1
    return width


def terminal_size() -> tuple[int, int]:
    try:
        size = fcntl.ioctl(sys.stdout.fileno(), termios.TIOCGWINSZ, b"\0" * 8)
        rows, cols, _, _ = struct.unpack("HHHH", size)
        return max(1, cols), max(1, rows)
    except OSError:
        return 80, 3
