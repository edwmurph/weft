from __future__ import annotations

import shutil
from dataclasses import dataclass
from pathlib import Path

from codux.tmux import PROJECT_ROOT_OPTION, RUNTIME_DIR_OPTION, WORKDIR_OPTION
from codux.tmux_api import run_tmux


@dataclass(frozen=True)
class CoduxSession:
    name: str
    workdir: str
    runtime_dir: str
    project_root: str
    window_count: int
    created_at: int
    attached_clients: int
    current: bool = False


def list_codux_sessions(current_session: str | None = None) -> list[CoduxSession]:
    try:
        result = run_tmux(
            [
                "list-sessions",
                "-F",
                (
                    f"#{{session_name}}\t#{{session_windows}}\t#{{session_created}}\t"
                    f"#{{session_attached}}\t#{{{WORKDIR_OPTION}}}\t"
                    f"#{{{RUNTIME_DIR_OPTION}}}\t#{{{PROJECT_ROOT_OPTION}}}"
                ),
            ],
            check=False,
        )
    except OSError:
        return []
    if result.returncode != 0:
        return []

    sessions: list[CoduxSession] = []
    for line in result.stdout.splitlines():
        parts = line.split("\t")
        if len(parts) != 7:
            continue
        name, windows, created, attached, workdir, runtime_dir, project_root = parts
        if not _looks_like_codux_session(name, workdir, runtime_dir, project_root):
            continue
        sessions.append(
            CoduxSession(
                name=name,
                workdir=workdir,
                runtime_dir=runtime_dir,
                project_root=project_root,
                window_count=_safe_int(windows),
                created_at=_safe_int(created),
                attached_clients=_safe_int(attached),
                current=name == current_session,
            )
        )
    return sorted(sessions, key=lambda session: (not session.current, session.name))


def other_codux_sessions(current_session: str) -> list[CoduxSession]:
    return [session for session in list_codux_sessions(current_session) if not session.current]


def other_codux_session_count(current_session: str) -> int:
    return len(other_codux_sessions(current_session))


def current_tmux_session() -> str | None:
    try:
        result = run_tmux(["display-message", "-p", "#{session_name}"], check=False)
    except OSError:
        return None
    if result.returncode != 0:
        return None
    name = result.stdout.strip()
    return name or None


def kill_codux_session(session_name: str) -> bool:
    try:
        result = run_tmux(["kill-session", "-t", session_name], check=False)
    except OSError:
        return False
    return result.returncode == 0


def codux_workspaces_dir() -> Path:
    return Path.home() / ".codux" / "workdirs"


def list_codux_workspaces() -> list[Path]:
    root = codux_workspaces_dir()
    try:
        return sorted(path for path in root.iterdir() if path.is_dir() and not path.is_symlink())
    except OSError:
        return []


def delete_codux_workspace(path: Path) -> bool:
    try:
        root = codux_workspaces_dir().resolve()
        path.resolve().relative_to(root)
        if path.is_symlink():
            return False
        shutil.rmtree(path)
    except FileNotFoundError:
        return True
    except (OSError, ValueError):
        return False
    return True


def display_path(path: str) -> str:
    if not path:
        return "-"
    try:
        path_obj = Path(path).expanduser().resolve()
        home = Path.home().resolve()
        return f"~/{path_obj.relative_to(home)}"
    except (OSError, ValueError):
        return path


def _looks_like_codux_session(
    name: str,
    workdir: str,
    runtime_dir: str,
    project_root: str,
) -> bool:
    return bool(
        workdir or runtime_dir or project_root or name == "codux" or name.startswith("codux-")
    )


def _safe_int(value: str) -> int:
    try:
        return int(value)
    except ValueError:
        return 0
