from __future__ import annotations

import errno
import fcntl
import os
import plistlib
import pty
import select
import shlex
import signal
import struct
import sys
import termios
import tty
from collections.abc import Callable, Mapping
from pathlib import Path


PROBE_QUERIES = (
    b"\x1b]10;?\x1b\\",
    b"\x1b]10;?\x07",
    b"\x1b]11;?\x1b\\",
    b"\x1b]11;?\x07",
)
MAX_PATTERN_LEN = max(len(pattern) for pattern in PROBE_QUERIES)
DEFAULT_FG_RGB = (232, 235, 237)
DEFAULT_BG_RGB = (29, 38, 42)
RGB_ENV_SEPARATOR = ","


def run_codex_proxy(command: list[str] | None = None) -> int:
    command = command or ["codex"]
    responses = _probe_responses()
    pid, master_fd = pty.fork()
    if pid == 0:
        _prepare_child_env()
        os.execvp(command[0], command)

    stdin_fd = sys.stdin.fileno()
    stdout_fd = sys.stdout.fileno()
    old_termios = None
    if sys.stdin.isatty():
        old_termios = termios.tcgetattr(stdin_fd)
        tty.setraw(stdin_fd)

    proxy = _ProbeProxy(responses)
    loading = _LoadingIndicator(stdout_fd)
    _copy_terminal_size(stdin_fd, master_fd)
    old_winch = signal.getsignal(signal.SIGWINCH)

    def handle_winch(_signum, _frame) -> None:
        _copy_terminal_size(stdin_fd, master_fd)

    signal.signal(signal.SIGWINCH, handle_winch)
    try:
        loading.render()
        while True:
            readable, _, _ = select.select([stdin_fd, master_fd], [], [], loading.timeout)
            if not readable:
                loading.render()
                continue
            if stdin_fd in readable:
                data = os.read(stdin_fd, 8192)
                if not data:
                    break
                os.write(master_fd, data)
            if master_fd in readable:
                try:
                    data = os.read(master_fd, 8192)
                except OSError as exc:
                    if exc.errno == errno.EIO:
                        break
                    raise
                if not data:
                    break
                proxy.feed(data, stdout_fd, master_fd, before_stdout=loading.clear)
    finally:
        loading.clear()
        proxy.flush(stdout_fd)
        signal.signal(signal.SIGWINCH, old_winch)
        if old_termios is not None:
            termios.tcsetattr(stdin_fd, termios.TCSADRAIN, old_termios)
        os.close(master_fd)

    _, status = os.waitpid(pid, 0)
    if os.WIFEXITED(status):
        return os.WEXITSTATUS(status)
    if os.WIFSIGNALED(status):
        return 128 + os.WTERMSIG(status)
    return 1


class _ProbeProxy:
    def __init__(self, responses: dict[bytes, bytes]) -> None:
        self.responses = responses
        self.pending = b""

    def feed(
        self,
        data: bytes,
        stdout_fd: int,
        child_fd: int,
        before_stdout: Callable[[], None] | None = None,
    ) -> None:
        self.pending += data
        self._drain(stdout_fd, child_fd, final=False, before_stdout=before_stdout)

    def flush(self, stdout_fd: int) -> None:
        if self.pending:
            os.write(stdout_fd, self.pending)
            self.pending = b""

    def _drain(
        self,
        stdout_fd: int,
        child_fd: int,
        final: bool,
        before_stdout: Callable[[], None] | None = None,
    ) -> None:
        while self.pending:
            match = self._next_match()
            if match is not None:
                index, pattern, response = match
                if index:
                    self._write_stdout(stdout_fd, self.pending[:index], before_stdout)
                os.write(child_fd, response)
                self.pending = self.pending[index + len(pattern) :]
                continue

            keep = 0 if final else MAX_PATTERN_LEN - 1
            if len(self.pending) <= keep:
                return
            cutoff = len(self.pending) - keep
            self._write_stdout(stdout_fd, self.pending[:cutoff], before_stdout)
            self.pending = self.pending[cutoff:]

    def _write_stdout(
        self,
        stdout_fd: int,
        data: bytes,
        before_stdout: Callable[[], None] | None,
    ) -> None:
        if not data:
            return
        if before_stdout is not None:
            before_stdout()
        os.write(stdout_fd, data)

    def _next_match(self) -> tuple[int, bytes, bytes] | None:
        matches = [
            (index, pattern, response)
            for pattern, response in self.responses.items()
            if (index := self.pending.find(pattern)) >= 0
        ]
        return min(matches, default=None, key=lambda item: item[0])


def _probe_responses() -> dict[bytes, bytes]:
    fg, bg = _terminal_rgb()
    return {
        b"\x1b]10;?\x1b\\": _osc_color_response(10, fg),
        b"\x1b]10;?\x07": _osc_color_response(10, fg),
        b"\x1b]11;?\x1b\\": _osc_color_response(11, bg),
        b"\x1b]11;?\x07": _osc_color_response(11, bg),
    }


def terminal_color_env(environ: Mapping[str, str] | None = None) -> dict[str, str]:
    fg, bg = _terminal_rgb(os.environ if environ is None else environ)
    return {
        "CODUX_FG_RGB": _format_rgb_env(fg),
        "CODUX_BG_RGB": _format_rgb_env(bg),
        "COLORFGBG": _colorfgbg_for_rgb(fg, bg),
    }


def _terminal_rgb(
    environ: Mapping[str, str] | None = None,
) -> tuple[tuple[int, int, int], tuple[int, int, int]]:
    environ = os.environ if environ is None else environ
    fg = _rgb_env(environ.get("CODUX_FG_RGB"))
    bg = _rgb_env(environ.get("CODUX_BG_RGB"))
    if fg is not None and bg is not None:
        return fg, bg

    colorfgbg = environ.get("CODUX_COLORFGBG")
    if colorfgbg:
        return _colorfgbg_rgb(colorfgbg)

    iterm = _iterm_profile_rgb(environ)
    if iterm is not None:
        return iterm

    return DEFAULT_FG_RGB, DEFAULT_BG_RGB


def _colorfgbg_rgb(value: str) -> tuple[tuple[int, int, int], tuple[int, int, int]]:
    parts = [part for part in value.replace(";", " ").split() if part.lstrip("-").isdigit()]
    if len(parts) >= 2:
        return _ansi_color(int(parts[0])), _ansi_color(int(parts[-1]))
    return DEFAULT_FG_RGB, DEFAULT_BG_RGB


def _rgb_env(value: str | None) -> tuple[int, int, int] | None:
    if not value:
        return None
    value = value.strip()
    if value.startswith("#") and len(value) == 7:
        try:
            return tuple(int(value[index : index + 2], 16) for index in (1, 3, 5))
        except ValueError:
            return None
    parts = [
        part.strip()
        for part in value.replace("rgb:", "")
        .replace("/", RGB_ENV_SEPARATOR)
        .split(RGB_ENV_SEPARATOR)
    ]
    if len(parts) != 3:
        return None
    channels: list[int] = []
    for part in parts:
        try:
            number = int(part, 16) // 257 if len(part) == 4 else int(part)
        except ValueError:
            return None
        channels.append(max(0, min(255, number)))
    return tuple(channels)  # type: ignore[return-value]


def _iterm_profile_rgb(
    environ: Mapping[str, str],
) -> tuple[tuple[int, int, int], tuple[int, int, int]] | None:
    if not _looks_like_iterm(environ):
        return None
    prefs_path = Path.home() / "Library" / "Preferences" / "com.googlecode.iterm2.plist"
    try:
        prefs = plistlib.loads(prefs_path.read_bytes())
    except (OSError, plistlib.InvalidFileException):
        return None

    profiles = prefs.get("New Bookmarks")
    if not isinstance(profiles, list):
        return None

    profile = _find_iterm_profile(profiles, prefs, environ)
    if profile is None:
        return None
    fg = _iterm_color(profile.get("Foreground Color"))
    bg = _iterm_color(profile.get("Background Color"))
    if fg is None or bg is None:
        return None
    return fg, bg


def _looks_like_iterm(environ: Mapping[str, str]) -> bool:
    return (
        environ.get("TERM_PROGRAM") == "iTerm.app"
        or environ.get("LC_TERMINAL") == "iTerm2"
        or bool(environ.get("ITERM_PROFILE"))
    )


def _find_iterm_profile(
    profiles: list[object],
    prefs: Mapping[str, object],
    environ: Mapping[str, str],
) -> Mapping[str, object] | None:
    profile_name = environ.get("ITERM_PROFILE")
    if profile_name:
        for profile in profiles:
            if isinstance(profile, Mapping) and profile.get("Name") == profile_name:
                return profile

    default_guid = prefs.get("Default Bookmark Guid")
    for profile in profiles:
        if isinstance(profile, Mapping) and profile.get("Guid") == default_guid:
            return profile
    return None


def _iterm_color(raw: object) -> tuple[int, int, int] | None:
    if not isinstance(raw, Mapping):
        return None
    try:
        red = float(raw["Red Component"])
        green = float(raw["Green Component"])
        blue = float(raw["Blue Component"])
    except (KeyError, TypeError, ValueError):
        return None
    return (
        max(0, min(255, round(red * 255))),
        max(0, min(255, round(green * 255))),
        max(0, min(255, round(blue * 255))),
    )


def _format_rgb_env(rgb: tuple[int, int, int]) -> str:
    return RGB_ENV_SEPARATOR.join(str(channel) for channel in rgb)


def _colorfgbg_for_rgb(
    fg: tuple[int, int, int],
    bg: tuple[int, int, int],
) -> str:
    fg_index = 15 if sum(fg) >= 384 else 0
    bg_index = 15 if sum(bg) >= 384 else 0
    return f"{fg_index};{bg_index}"


def _ansi_color(index: int) -> tuple[int, int, int]:
    palette = [
        (0, 0, 0),
        (128, 0, 0),
        (0, 128, 0),
        (128, 128, 0),
        (0, 0, 128),
        (128, 0, 128),
        (0, 128, 128),
        (192, 192, 192),
        (128, 128, 128),
        (255, 0, 0),
        (0, 255, 0),
        (255, 255, 0),
        (0, 0, 255),
        (255, 0, 255),
        (0, 255, 255),
        (255, 255, 255),
    ]
    if 0 <= index < len(palette):
        return palette[index]
    return (0, 0, 0)


def _osc_color_response(slot: int, rgb: tuple[int, int, int]) -> bytes:
    channels = "/".join(f"{channel * 257:04x}" for channel in rgb)
    return f"\x1b]{slot};rgb:{channels}\x1b\\".encode()


class _LoadingIndicator:
    frames = "|/-\\"

    def __init__(self, stdout_fd: int) -> None:
        self.stdout_fd = stdout_fd
        self.active = True
        self.index = 0
        self.timeout = 0.08

    def render(self) -> None:
        if not self.active:
            self.timeout = None
            return
        rows, cols = _terminal_dimensions(self.stdout_fd)
        message = f"{self.frames[self.index % len(self.frames)]} Starting Codex"
        self.index += 1
        row = max(1, rows // 2)
        col = max(1, ((cols - len(message)) // 2) + 1)
        payload = f"\x1b[?25l\x1b[2J\x1b[{row};{col}H{message}"
        os.write(self.stdout_fd, payload.encode())

    def clear(self) -> None:
        if not self.active:
            return
        self.active = False
        self.timeout = None
        os.write(self.stdout_fd, b"\x1b[2J\x1b[H")


def _prepare_child_env() -> None:
    for name in ("NO_COLOR", "CODEX_CI", "CI", "CLICOLOR_FORCE", "FORCE_COLOR"):
        os.environ.pop(name, None)
    os.environ["TERM"] = os.environ.get("CODUX_CHILD_TERM", "xterm-256color")
    os.environ["COLORTERM"] = "truecolor"
    os.environ["CLICOLOR"] = "1"
    os.environ.update(terminal_color_env())


def _terminal_dimensions(fd: int) -> tuple[int, int]:
    try:
        size = fcntl.ioctl(fd, termios.TIOCGWINSZ, b"\0" * 8)
        rows, cols, _, _ = struct.unpack("HHHH", size)
        return max(1, rows), max(1, cols)
    except OSError:
        return 24, 80


def _copy_terminal_size(source_fd: int, target_fd: int) -> None:
    try:
        size = fcntl.ioctl(source_fd, termios.TIOCGWINSZ, b"\0" * 8)
        fcntl.ioctl(target_fd, termios.TIOCSWINSZ, size)
    except OSError:
        rows, cols = 24, 80
        size = struct.pack("HHHH", rows, cols, 0, 0)
        fcntl.ioctl(target_fd, termios.TIOCSWINSZ, size)


def split_command(command: str) -> list[str]:
    return shlex.split(command) or ["codex"]
