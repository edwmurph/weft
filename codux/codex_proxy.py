from __future__ import annotations

import errno
import fcntl
import os
import pty
import select
import shlex
import signal
import struct
import sys
import termios
import tty


PROBE_PATTERNS = {
    b"\x1b]10;?\x1b\\": b"\x1b]10;rgb:ffff/ffff/ffff\x1b\\",
    b"\x1b]10;?\x07": b"\x1b]10;rgb:ffff/ffff/ffff\x1b\\",
    b"\x1b]11;?\x1b\\": b"\x1b]11;rgb:0000/0000/0000\x1b\\",
    b"\x1b]11;?\x07": b"\x1b]11;rgb:0000/0000/0000\x1b\\",
}
MAX_PATTERN_LEN = max(len(pattern) for pattern in PROBE_PATTERNS)


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
    _copy_terminal_size(stdin_fd, master_fd)
    old_winch = signal.getsignal(signal.SIGWINCH)

    def handle_winch(_signum, _frame) -> None:
        _copy_terminal_size(stdin_fd, master_fd)

    signal.signal(signal.SIGWINCH, handle_winch)
    try:
        while True:
            readable, _, _ = select.select([stdin_fd, master_fd], [], [])
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
                proxy.feed(data, stdout_fd, master_fd)
    finally:
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

    def feed(self, data: bytes, stdout_fd: int, child_fd: int) -> None:
        self.pending += data
        self._drain(stdout_fd, child_fd, final=False)

    def flush(self, stdout_fd: int) -> None:
        if self.pending:
            os.write(stdout_fd, self.pending)
            self.pending = b""

    def _drain(self, stdout_fd: int, child_fd: int, final: bool) -> None:
        while self.pending:
            match = self._next_match()
            if match is not None:
                index, pattern, response = match
                if index:
                    os.write(stdout_fd, self.pending[:index])
                os.write(child_fd, response)
                self.pending = self.pending[index + len(pattern) :]
                continue

            keep = 0 if final else MAX_PATTERN_LEN - 1
            if len(self.pending) <= keep:
                return
            cutoff = len(self.pending) - keep
            os.write(stdout_fd, self.pending[:cutoff])
            self.pending = self.pending[cutoff:]

    def _next_match(self) -> tuple[int, bytes, bytes] | None:
        matches = [
            (index, pattern, response)
            for pattern, response in self.responses.items()
            if (index := self.pending.find(pattern)) >= 0
        ]
        return min(matches, default=None, key=lambda item: item[0])


def _probe_responses() -> dict[bytes, bytes]:
    fg, bg = _colorfgbg_rgb()
    return {
        b"\x1b]10;?\x1b\\": _osc_color_response(10, fg),
        b"\x1b]10;?\x07": _osc_color_response(10, fg),
        b"\x1b]11;?\x1b\\": _osc_color_response(11, bg),
        b"\x1b]11;?\x07": _osc_color_response(11, bg),
    }


def _colorfgbg_rgb() -> tuple[tuple[int, int, int], tuple[int, int, int]]:
    value = os.environ.get("COLORFGBG", "")
    parts = [part for part in value.replace(";", " ").split() if part.lstrip("-").isdigit()]
    if len(parts) >= 2:
        return _ansi_color(int(parts[0])), _ansi_color(int(parts[-1]))
    return (255, 255, 255), (0, 0, 0)


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


def _prepare_child_env() -> None:
    for name in ("NO_COLOR", "CODEX_CI", "CI", "CLICOLOR_FORCE", "FORCE_COLOR"):
        os.environ.pop(name, None)
    os.environ["TERM"] = "tmux-256color"
    os.environ["COLORTERM"] = "truecolor"
    os.environ["CLICOLOR"] = "1"


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
