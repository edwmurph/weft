from __future__ import annotations

import errno
import fcntl
import os
import pty
import select
import shlex
import signal
import struct
import subprocess
import sys
import termios
import time
import tty
from dataclasses import dataclass
from dataclasses import replace

import pyte

from codux.codex_proxy import _probe_responses
from codux.config import CoduxConfig, ensure_config
from codux.navigation import select_grid_tab
from codux.render import (
    ACTIVE_COLOR,
    codex_shortcuts,
    nav_content_height,
    nav_shortcuts,
    render_nav,
)
from codux.state import AppState, StateStore


RESET = "\033[0m"
HIDE_CURSOR = "\033[?25l"
SHOW_CURSOR = "\033[?25h"
ACTIVE_BORDER = f"\033[38;5;{ACTIVE_COLOR}m"
INACTIVE_BORDER = "\033[38;5;244m"
MIN_CODEX_HEIGHT = 5
IGNORED_GENERATED_TITLES = {"", "CODEX", "NAV", "Codux Empty"}
ANSI_COLOR_RGB = {
    "black": (0, 0, 0),
    "red": (128, 0, 0),
    "green": (0, 128, 0),
    "brown": (128, 128, 0),
    "blue": (0, 0, 128),
    "magenta": (128, 0, 128),
    "cyan": (0, 128, 128),
    "white": (192, 192, 192),
    "brightblack": (128, 128, 128),
    "brightred": (255, 0, 0),
    "brightgreen": (0, 255, 0),
    "brightyellow": (255, 255, 0),
    "brightblue": (0, 0, 255),
    "brightmagenta": (255, 0, 255),
    "brightcyan": (0, 255, 255),
    "brightwhite": (255, 255, 255),
}


@dataclass(frozen=True)
class Rect:
    x: int
    y: int
    width: int
    height: int


@dataclass(frozen=True)
class Layout:
    width: int
    height: int
    nav_frame: Rect
    nav_inner: Rect
    codex_frame: Rect
    codex_inner: Rect


def run_host(tab_id: str | None) -> int:
    host = DashboardHost(tab_id)
    return host.run()


class DashboardHost:
    def __init__(self, tab_id: str | None) -> None:
        self.tab_id = tab_id
        self.config = ensure_config()
        self.store = StateStore()
        self.store.ensure()
        self.child: CodexChild | None = None
        self.layout: Layout | None = None
        self.focus = "codex" if tab_id else "nav"
        self.running = True
        self.last_state = self.store.read()
        self.last_render = 0.0
        self.last_child_title: str | None = None

    def run(self) -> int:
        stdin_fd = sys.stdin.fileno()
        stdout_fd = sys.stdout.fileno()
        old_termios = None
        if sys.stdin.isatty():
            old_termios = termios.tcgetattr(stdin_fd)
            tty.setraw(stdin_fd)
        old_winch = signal.getsignal(signal.SIGWINCH)

        def handle_winch(_signum, _frame) -> None:
            self.resize_child()
            self.render(force=True)

        signal.signal(signal.SIGWINCH, handle_winch)
        try:
            os.write(stdout_fd, b"\033[?1049h\033[?25l\033[2J")
            self.resize_child()
            self.render(force=True)
            while self.running:
                fds = [stdin_fd]
                if self.child and self.child.fd is not None:
                    fds.append(self.child.fd)
                readable, _, _ = select.select(fds, [], [], 0.1)
                if stdin_fd in readable:
                    data = os.read(stdin_fd, 8192)
                    if not data:
                        break
                    self.handle_input(data)
                if self.child and self.child.fd in readable:
                    self.child.read()
                    self.sync_child_title()
                    self.render()
                self.reap_child()
                self.render_if_state_changed()
        finally:
            if self.child:
                self.child.close()
            os.write(stdout_fd, f"{RESET}{SHOW_CURSOR}\033[?1049l".encode())
            signal.signal(signal.SIGWINCH, old_winch)
            if old_termios is not None:
                termios.tcsetattr(stdin_fd, termios.TCSADRAIN, old_termios)
        return 0

    def handle_input(self, data: bytes) -> None:
        state = self.store.read()
        focus = state.focus
        if self.tab_id is None and self.child is None:
            focus = "nav"
        if b"\x11" in data:
            subprocess.run(["tmux", "detach-client", "-s", self.config.tmux_session], check=False)
            return
        if b"\x04" in data:
            self.set_focus("codex" if focus == "nav" else "nav")
            chunks = [chunk for chunk in data.split(b"\x04") if chunk]
            if focus == "codex":
                return
            for chunk in chunks:
                self.handle_nav_input(chunk)
            return
        if focus == "nav":
            self.handle_nav_input(data)
        elif self.child:
            self.child.write(data)

    def handle_nav_input(self, data: bytes) -> None:
        for key in nav_keys(data):
            if key == "Left":
                self.select_grid(delta_column=-1)
            elif key == "Right":
                self.select_grid(delta_column=1)
            elif key == "Up":
                self.select_grid(delta_row=-1)
            elif key == "Down":
                self.select_grid(delta_row=1)
            elif key == "S-Left":
                self.move_column(-1)
            elif key == "S-Right":
                self.move_column(1)
            elif key == "Enter":
                self.set_focus("codex")
            elif key == "n":
                self.run_cli("new")
            elif key == "x":
                self.run_cli("close")
            elif key == "r":
                self.rename_prompt()
            elif key == "?":
                self.help_popup()
        self.render(force=True)

    def select_grid(self, delta_column: int = 0, delta_row: int = 0) -> None:
        state = self.store.read()
        target = select_grid_tab(
            state.tabs,
            state.active_tab_id,
            self.config.columns,
            delta_column=delta_column,
            delta_row=delta_row,
        )
        if target is None:
            return
        self.store.update(lambda current: replace(current, active_tab_id=target.id, focus="nav"))
        subprocess.run(["tmux", "select-window", "-t", target.tmux_window_id], check=False)

    def move_column(self, delta: int) -> None:
        state = self.store.read()
        target = state.active_tab
        if target is None:
            return
        current_index = (
            self.config.columns.index(target.column) if target.column in self.config.columns else 0
        )
        next_index = max(0, min(len(self.config.columns) - 1, current_index + delta))
        next_column = self.config.columns[next_index]

        def mutate(current: AppState) -> AppState:
            return replace(
                current,
                tabs=[
                    tab.with_updates(column=next_column) if tab.id == target.id else tab
                    for tab in current.tabs
                ],
                focus="nav",
            )

        self.store.update(mutate)

    def set_focus(self, focus: str) -> None:
        self.store.update(lambda current: replace(current, focus=focus))
        self.render(force=True)

    def run_cli(self, *args: str) -> None:
        subprocess.run(
            [sys.executable, "-m", "codux.cli", *args],
            check=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

    def rename_prompt(self) -> None:
        command = f"{shlex.quote(sys.executable)} -m codux.cli _popup-rename"
        subprocess.run(
            ["tmux", "display-popup", "-w", "72", "-h", "10", "-T", "Rename", command],
            check=False,
        )

    def help_popup(self) -> None:
        command = f"{shlex.quote(sys.executable)} -m codux.cli _popup-help"
        subprocess.run(
            ["tmux", "display-popup", "-w", "72", "-h", "22", "-T", "Codux", command],
            check=False,
        )

    def render_if_state_changed(self) -> None:
        state = self.store.read()
        if state != self.last_state:
            self.render(force=True)

    def sync_child_title(self) -> None:
        if self.child is None or self.tab_id is None:
            return
        raw_title = str(getattr(self.child.screen, "title", "")).strip()
        if raw_title == self.last_child_title:
            return
        self.last_child_title = raw_title
        title = generated_child_title(raw_title)
        if title is None:
            return

        state = self.store.read()
        target = next((tab for tab in state.tabs if tab.id == self.tab_id), None)
        if target is None or target.title == title:
            return

        def mutate(current: AppState) -> AppState:
            return replace(
                current,
                tabs=[
                    tab.with_updates(title=title) if tab.id == self.tab_id else tab
                    for tab in current.tabs
                ],
            )

        self.store.update(mutate)
        subprocess.run(["tmux", "rename-window", "-t", target.tmux_window_id, title], check=False)
        subprocess.run(["tmux", "select-pane", "-t", target.tmux_pane_id, "-T", title], check=False)

    def render(self, force: bool = False) -> None:
        now = time.monotonic()
        if not force and now - self.last_render < 0.03:
            return
        self.last_render = now
        state = self.store.read()
        self.last_state = state
        layout = self.current_layout(state)
        if self.layout != layout:
            self.layout = layout
            self.resize_child()
        lines = render_dashboard(self.config, state, layout, self.child)
        cursor = self.child_cursor(layout, state)
        payload = "".join(f"\033[{index};1H{line}" for index, line in enumerate(lines, 1))
        if cursor is None:
            payload += HIDE_CURSOR
        else:
            payload += f"\033[{cursor[1]};{cursor[0]}H{SHOW_CURSOR}"
        os.write(sys.stdout.fileno(), payload.encode())

    def current_layout(self, state: AppState) -> Layout:
        width, height = terminal_size()
        width = max(width, 20)
        height = max(height, 8)
        nav_frame_height = min(max(nav_content_height(self.config, state) + 2, 3), height - 3)
        codex_height = max(MIN_CODEX_HEIGHT, height - nav_frame_height)
        if nav_frame_height + codex_height > height:
            codex_height = height - nav_frame_height
        nav_frame = Rect(1, 1, width, nav_frame_height)
        nav_inner = Rect(2, 2, max(1, width - 2), max(1, nav_frame_height - 2))
        codex_frame = Rect(1, nav_frame_height + 1, width, codex_height)
        codex_inner = Rect(
            2,
            nav_frame_height + 2,
            max(1, width - 2),
            max(1, codex_height - 2),
        )
        return Layout(width, height, nav_frame, nav_inner, codex_frame, codex_inner)

    def resize_child(self) -> None:
        if not self.tab_id:
            return
        state = self.store.read()
        layout = self.current_layout(state)
        width = max(1, layout.codex_inner.width)
        height = max(1, layout.codex_inner.height)
        if self.child is None:
            self.child = CodexChild(self.config.codex_command, width, height)
        else:
            self.child.resize(width, height)

    def reap_child(self) -> None:
        if self.child is None or self.child.pid is None:
            return
        try:
            pid, _ = os.waitpid(self.child.pid, os.WNOHANG)
        except ChildProcessError:
            self.child.pid = None
            return
        if pid:
            self.child.pid = None
            self.child.fd = None

    def child_cursor(self, layout: Layout, state: AppState) -> tuple[int, int] | None:
        if state.focus != "codex" or not self.child:
            return None
        cursor = self.child.screen.cursor
        if cursor.hidden:
            return None
        x = layout.codex_inner.x + min(cursor.x, layout.codex_inner.width - 1)
        y = layout.codex_inner.y + min(cursor.y, layout.codex_inner.height - 1)
        return x, y


class CodexChild:
    def __init__(self, command: str, width: int, height: int) -> None:
        self.command = shlex.split(command) or ["codex"]
        self.screen = pyte.Screen(width, height)
        self.stream = pyte.Stream(self.screen)
        self.filter = ProbeFilter()
        self.pid: int | None = None
        self.fd: int | None = None
        self.spawn(width, height)

    def spawn(self, width: int, height: int) -> None:
        pid, fd = pty.fork()
        if pid == 0:
            prepare_codex_env()
            os.execvp(self.command[0], self.command)
        self.pid = pid
        self.fd = fd
        self.resize(width, height)

    def read(self) -> None:
        if self.fd is None:
            return
        try:
            data = os.read(self.fd, 8192)
        except OSError as exc:
            if exc.errno == errno.EIO:
                self.fd = None
                return
            raise
        if data:
            visible = self.filter.feed(data, self.fd)
            if visible:
                self.stream.feed(visible.decode(errors="ignore"))

    def write(self, data: bytes) -> None:
        if self.fd is not None:
            os.write(self.fd, data)

    def resize(self, width: int, height: int) -> None:
        self.screen.resize(lines=height, columns=width)
        if self.fd is None:
            return
        size = struct.pack("HHHH", height, width, 0, 0)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, size)

    def close(self) -> None:
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None


class ProbeFilter:
    def __init__(self) -> None:
        self.responses = _probe_responses()
        self.pending = b""
        self.keep = max(len(pattern) for pattern in self.responses) - 1

    def feed(self, data: bytes, child_fd: int) -> bytes:
        self.pending += data
        visible = bytearray()
        while self.pending:
            match = self.next_match()
            if match is not None:
                index, pattern, response = match
                visible.extend(self.pending[:index])
                os.write(child_fd, response)
                self.pending = self.pending[index + len(pattern) :]
                continue
            if len(self.pending) <= self.keep:
                break
            cutoff = len(self.pending) - self.keep
            visible.extend(self.pending[:cutoff])
            self.pending = self.pending[cutoff:]
        return bytes(visible)

    def next_match(self) -> tuple[int, bytes, bytes] | None:
        matches = [
            (index, pattern, response)
            for pattern, response in self.responses.items()
            if (index := self.pending.find(pattern)) >= 0
        ]
        return min(matches, default=None, key=lambda item: item[0])


def render_dashboard(
    config: CoduxConfig,
    state: AppState,
    layout: Layout,
    child: CodexChild | None,
) -> list[str]:
    lines: list[str] = []
    nav_active = state.focus == "nav"
    nav_lines = render_nav(config, state, layout.nav_inner.width).splitlines()
    lines.extend(
        render_box(
            layout.nav_frame.width,
            "NAV",
            nav_active,
            nav_lines,
            bottom_label=nav_shortcuts(config) if nav_active else "",
        )
    )

    codex_active = state.focus == "codex"
    if child is None:
        empty = ["No Codex sessions open", "Press n to create one."]
        content = center_lines(empty, layout.codex_inner.width, layout.codex_inner.height)
    else:
        content = pyte_screen_lines(
            child.screen, layout.codex_inner.width, layout.codex_inner.height
        )
    lines.extend(
        render_box(
            layout.codex_frame.width,
            "CODEX",
            codex_active,
            content,
            bottom_label=codex_shortcuts(config) if codex_active else "",
        )
    )
    if len(lines) < layout.height:
        lines.extend([" " * layout.width] * (layout.height - len(lines)))
    return lines[: layout.height]


def render_box(
    width: int,
    title: str,
    active: bool,
    content_lines: list[str],
    bottom_label: str = "",
) -> list[str]:
    color = ACTIVE_BORDER if active else INACTIVE_BORDER
    inner_width = max(0, width - 2)
    frame = [
        color + "╭" + border_inner(inner_width, title) + "╮" + RESET,
    ]
    for line in content_lines:
        frame.append(color + "│" + RESET + visible_pad(line, inner_width) + color + "│" + RESET)
    frame.append(color + "╰" + border_inner(inner_width, bottom_label) + "╯" + RESET)
    return frame


def center_lines(lines: list[str], width: int, height: int) -> list[str]:
    result = [" " * width for _ in range(height)]
    start_y = max(0, (height - len(lines)) // 2)
    for index, line in enumerate(lines[:height]):
        y = start_y + index
        if y < height:
            result[y] = ("  " + line)[:width].ljust(width)
    return result


def pyte_screen_lines(screen: pyte.Screen, width: int, height: int) -> list[str]:
    lines: list[str] = []
    for row in range(height):
        pieces = []
        current_style: str | None = None
        for col in range(width):
            char = screen.buffer[row].get(col)
            if char is None:
                style = RESET
                data = " "
            else:
                style = cell_style(char)
                data = char.data or " "
            if style != current_style:
                pieces.append(style)
                current_style = style
            pieces.append(data)
        pieces.append(RESET)
        lines.append("".join(pieces))
    return lines


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


def border_inner(width: int, label: str) -> str:
    width = max(0, width)
    if width == 0:
        return ""
    if not label or width == 1:
        return "─" * width
    max_label_width = max(0, width - 2)
    label = f" {label} "
    if len(label) > max_label_width:
        label = label[: max(0, max_label_width - 3)].rstrip() + "..." if max_label_width > 3 else ""
    return "─" + label + ("─" * (width - len(label) - 1))


def generated_child_title(title: str) -> str | None:
    title = title.strip()
    if title in IGNORED_GENERATED_TITLES:
        return None
    if is_transient_codex_title(title):
        return None
    if title.endswith(".local"):
        return None
    return title


def is_transient_codex_title(title: str) -> bool:
    return (
        title.startswith(("Ready | ", "Starting | "))
        or " Ready | " in title
        or " Starting | " in title
        or title in {"Ready", "Starting"}
        or title.endswith((" Ready", " Starting"))
    )


def cell_style(char) -> str:
    fg = char.fg
    bg = char.bg
    if char.reverse:
        fg, bg = bg, fg
    codes: list[str] = []
    if fg != "default":
        codes.append(color_code(fg, foreground=True))
    if bg != "default":
        codes.append(color_code(bg, foreground=False))
    if char.bold:
        codes.append("1")
    if char.italics:
        codes.append("3")
    if char.underscore:
        codes.append("4")
    if char.blink:
        codes.append("5")
    if char.strikethrough:
        codes.append("9")
    return "\033[" + ";".join(code for code in codes if code) + "m" if codes else RESET


def color_code(value: str, foreground: bool) -> str:
    prefix = "38" if foreground else "48"
    rgb = None
    if value in ANSI_COLOR_RGB:
        rgb = ANSI_COLOR_RGB[value]
    elif len(value) == 6:
        try:
            rgb = (int(value[0:2], 16), int(value[2:4], 16), int(value[4:6], 16))
        except ValueError:
            rgb = None
    if rgb is None:
        return ""
    return f"{prefix};2;{rgb[0]};{rgb[1]};{rgb[2]}"


def nav_keys(data: bytes) -> list[str]:
    mapping = {
        b"\x1b[D": "Left",
        b"\x1b[C": "Right",
        b"\x1b[A": "Up",
        b"\x1b[B": "Down",
        b"\x1b[1;2D": "S-Left",
        b"\x1b[1;2C": "S-Right",
        b"\x1b[1;2A": "S-Up",
        b"\x1b[1;2B": "S-Down",
        b"\r": "Enter",
        b"\n": "Enter",
    }
    keys: list[str] = []
    index = 0
    while index < len(data):
        matched = False
        for raw, key in sorted(mapping.items(), key=lambda item: len(item[0]), reverse=True):
            if data.startswith(raw, index):
                keys.append(key)
                index += len(raw)
                matched = True
                break
        if matched:
            continue
        char = data[index : index + 1]
        if char in {b"n", b"x", b"r", b"?"}:
            keys.append(char.decode())
        index += 1
    return keys


def prepare_codex_env() -> None:
    for name in ("NO_COLOR", "CODEX_CI", "CI", "CLICOLOR_FORCE", "FORCE_COLOR"):
        os.environ.pop(name, None)
    os.environ["TERM"] = "tmux-256color"
    os.environ["COLORTERM"] = "truecolor"
    os.environ["CLICOLOR"] = "1"


def terminal_size() -> tuple[int, int]:
    try:
        size = fcntl.ioctl(sys.stdout.fileno(), termios.TIOCGWINSZ, b"\0" * 8)
        rows, cols, _, _ = struct.unpack("HHHH", size)
        return cols, rows
    except OSError:
        return 80, 24
