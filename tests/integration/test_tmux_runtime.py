from __future__ import annotations

import json
import os
import shlex
import shutil
import subprocess
import time
import uuid
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import pytest

from codux.launcher import PROJECT_ROOT
from codux.tmux import FRAME_HOST_VERSION, NAV_HOST_VERSION, RUNTIME_DIR_OPTION, WORKDIR_OPTION


pytestmark = [
    pytest.mark.integration,
    pytest.mark.skipif(
        os.environ.get("CODUX_RUN_INTEGRATION") != "1",
        reason="set CODUX_RUN_INTEGRATION=1 to run live tmux integration tests",
    ),
]


@dataclass(frozen=True)
class LiveCoduxRuntime:
    env: dict[str, str]
    runtime_dir: Path
    workdir: Path
    session_name: str
    fake_codex_env_file: Path


@pytest.fixture()
def live_codux_runtime(tmp_path: Path) -> LiveCoduxRuntime:
    real_tmux = shutil.which("tmux")
    if real_tmux is None:
        pytest.skip("tmux is required for live integration tests")
    if shutil.which("uv") is None:
        pytest.skip("uv is required for live integration tests")

    run_id = f"codux-it-{uuid.uuid4().hex[:12]}"
    wrapper_dir = tmp_path / "bin"
    runtime_dir = tmp_path / "codux-home"
    workdir = tmp_path / "workspace"
    fake_codex = tmp_path / "fake-codex.sh"
    fake_codex_env_file = tmp_path / "fake-codex-env.txt"
    wrapper_dir.mkdir()
    runtime_dir.mkdir()
    workdir.mkdir()

    tmux_wrapper = wrapper_dir / "tmux"
    tmux_wrapper.write_text(
        f'#!/bin/sh\nexec {shlex.quote(real_tmux)} -L {shlex.quote(run_id)} "$@"\n',
        encoding="utf-8",
    )
    tmux_wrapper.chmod(0o700)

    fake_codex.write_text(
        "#!/bin/sh\n"
        "printf '\\033]2;Fake Codex Ready\\007'\n"
        "{\n"
        "  printf 'CODUX_HOME=%s\\n' \"${CODUX_HOME-}\"\n"
        "  printf 'CODUX_WORKDIR=%s\\n' \"${CODUX_WORKDIR-}\"\n"
        "  printf 'NO_COLOR=%s\\n' \"${NO_COLOR-}\"\n"
        '} > "$FAKE_CODEX_ENV_FILE"\n'
        "trap 'exit 0' HUP INT TERM\n"
        "while :; do sleep 1; done\n",
        encoding="utf-8",
    )
    fake_codex.chmod(0o700)

    (runtime_dir / "config.toml").write_text(
        f"tmux_session = {json.dumps(run_id)}\ncodex_command = {json.dumps(str(fake_codex))}\n",
        encoding="utf-8",
    )

    env = os.environ.copy()
    env.pop("TMUX", None)
    env.pop("TMUX_PANE", None)
    env.update(
        {
            "CODUX_HOME": str(runtime_dir),
            "CODUX_WORKDIR": str(workdir),
            "FAKE_CODEX_ENV_FILE": str(fake_codex_env_file),
            "NO_COLOR": "1",
            "PATH": f"{wrapper_dir}{os.pathsep}{env.get('PATH', '')}",
            "TERM": env.get("TERM", "xterm-256color"),
        }
    )

    runtime = LiveCoduxRuntime(
        env=env,
        runtime_dir=runtime_dir,
        workdir=workdir,
        session_name=run_id,
        fake_codex_env_file=fake_codex_env_file,
    )
    try:
        yield runtime
    finally:
        subprocess.run(
            ["tmux", "kill-server"],
            env=env,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
            timeout=5,
        )


def test_start_no_attach_creates_isolated_tmux_workspace(
    live_codux_runtime: LiveCoduxRuntime,
) -> None:
    result = run_codux(live_codux_runtime, "start", "--no-attach")

    assert live_codux_runtime.session_name in result.stdout
    assert session_option(live_codux_runtime, WORKDIR_OPTION) == str(live_codux_runtime.workdir)
    assert session_option(live_codux_runtime, RUNTIME_DIR_OPTION) == str(
        live_codux_runtime.runtime_dir
    )
    windows = wait_for("empty and spare windows", lambda: ready_window_rows(live_codux_runtime))
    panes = pane_rows(live_codux_runtime)
    roles = {pane["role"] for pane in panes}

    assert [window["empty"] for window in windows].count("1") == 1
    assert [window["spare"] for window in windows].count("1") == 1
    assert {"NAV", "CODEX", "NAV_TOP", "NAV_BOTTOM", "CODEX_TOP", "CODEX_BOTTOM"} <= roles
    assert any(pane["role"] == "NAV" and pane["nav_host"] == NAV_HOST_VERSION for pane in panes)
    assert any(pane["role"] != "NAV" and pane["frame_host"] == FRAME_HOST_VERSION for pane in panes)


def test_nav_new_tab_launches_fake_codex_in_isolated_tmux_runtime(
    live_codux_runtime: LiveCoduxRuntime,
) -> None:
    run_codux(live_codux_runtime, "start", "--no-attach")
    empty_nav_pane = wait_for(
        "empty window nav pane",
        lambda: next(
            (
                window["nav_pane"]
                for window in ready_window_rows(live_codux_runtime)
                if window["empty"] == "1"
            ),
            None,
        ),
    )

    run_tmux(live_codux_runtime, ["send-keys", "-t", empty_nav_pane, "n"])

    state = wait_for(
        "active fake Codex tab state", lambda: active_fake_codex_state(live_codux_runtime)
    )
    fake_codex_env = wait_for(
        "fake Codex environment file",
        lambda: (
            live_codux_runtime.fake_codex_env_file.read_text(encoding="utf-8")
            if live_codux_runtime.fake_codex_env_file.exists()
            else None
        ),
    )
    active_tab = state["tabs"][0]
    active_pane = wait_for(
        "active fake Codex pane",
        lambda: next(
            (
                pane
                for pane in pane_rows(live_codux_runtime)
                if pane["pane_id"] == active_tab["tmux_pane_id"]
            ),
            None,
        ),
    )

    assert fake_codex_env == "CODUX_HOME=\nCODUX_WORKDIR=\nNO_COLOR=\n"
    assert active_tab["tmux_session"] == live_codux_runtime.session_name
    assert active_tab["title"] == "{codex}"
    assert active_tab["codex_title"] == "Fake Codex Ready"
    assert state["focus"] == "codex"
    assert active_pane["role"] == "CODEX"
    assert active_pane["title"] == "Fake Codex Ready"
    assert "unset NO_COLOR CODUX_HOME CODUX_WORKDIR; exec" in active_pane["start_command"]


def run_codux(
    runtime: LiveCoduxRuntime,
    *args: str,
    timeout: float = 20,
) -> subprocess.CompletedProcess[str]:
    command = [
        "uv",
        "--quiet",
        "--no-progress",
        "--directory",
        str(PROJECT_ROOT),
        "--project",
        str(PROJECT_ROOT),
        "run",
        "codux",
        *args,
    ]
    return run_command(command, runtime.env, timeout=timeout)


def run_tmux(
    runtime: LiveCoduxRuntime,
    args: list[str],
    *,
    timeout: float = 10,
) -> subprocess.CompletedProcess[str]:
    return run_command(["tmux", *args], runtime.env, timeout=timeout)


def session_option(runtime: LiveCoduxRuntime, option: str) -> str:
    result = run_tmux(runtime, ["show-option", "-t", runtime.session_name, "-qv", option])
    return result.stdout.strip()


def run_command(
    command: list[str],
    env: dict[str, str],
    *,
    timeout: float,
) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(
        command,
        cwd=PROJECT_ROOT,
        env=env,
        text=True,
        capture_output=True,
        check=False,
        timeout=timeout,
    )
    if result.returncode != 0:
        pytest.fail(
            "command failed\n"
            f"command: {shlex.join(command)}\n"
            f"exit: {result.returncode}\n"
            f"stdout:\n{result.stdout}\n"
            f"stderr:\n{result.stderr}"
        )
    return result


def ready_window_rows(runtime: LiveCoduxRuntime) -> list[dict[str, str]]:
    rows = window_rows(runtime)
    assert rows
    assert sum(1 for row in rows if row["empty"] == "1") == 1
    assert sum(1 for row in rows if row["spare"] == "1") == 1
    for row in rows:
        assert row["nav_pane"]
        assert row["codex_pane"]
    return rows


def active_fake_codex_state(runtime: LiveCoduxRuntime) -> dict[str, Any]:
    state_path = runtime.runtime_dir / "state.json"
    assert state_path.exists()
    state = json.loads(state_path.read_text(encoding="utf-8"))
    tabs = state.get("tabs", [])
    assert len(tabs) == 1
    assert state.get("active_tab_id") == tabs[0]["id"]
    assert tabs[0].get("codex_title") == "Fake Codex Ready"
    return state


def window_rows(runtime: LiveCoduxRuntime) -> list[dict[str, str]]:
    result = run_tmux(
        runtime,
        [
            "list-windows",
            "-t",
            runtime.session_name,
            "-F",
            (
                "#{window_id}\t#{window_name}\t#{@codux-empty}\t#{@codux-spare}\t"
                "#{@codux-tab-id}\t#{@codux-nav-pane}\t#{@codux-codex-pane}"
            ),
        ],
    )
    rows = []
    for line in result.stdout.splitlines():
        parts = line.split("\t")
        if len(parts) != 7:
            continue
        window_id, name, empty, spare, tab_id, nav_pane, codex_pane = parts
        rows.append(
            {
                "window_id": window_id,
                "name": name,
                "empty": empty,
                "spare": spare,
                "tab_id": tab_id,
                "nav_pane": nav_pane,
                "codex_pane": codex_pane,
            }
        )
    return rows


def pane_rows(runtime: LiveCoduxRuntime) -> list[dict[str, str]]:
    result = run_tmux(
        runtime,
        [
            "list-panes",
            "-s",
            "-t",
            runtime.session_name,
            "-F",
            (
                "#{window_id}\t#{pane_id}\t#{@codux-role}\t#{pane_title}\t"
                "#{pane_current_command}\t#{pane_start_command}\t#{@codux-nav-host}\t"
                "#{@codux-frame-host}"
            ),
        ],
    )
    rows = []
    for line in result.stdout.splitlines():
        parts = line.split("\t")
        if len(parts) != 8:
            continue
        (
            window_id,
            pane_id,
            role,
            title,
            current_command,
            start_command,
            nav_host,
            frame_host,
        ) = parts
        rows.append(
            {
                "window_id": window_id,
                "pane_id": pane_id,
                "role": role,
                "title": title,
                "current_command": current_command,
                "start_command": start_command,
                "nav_host": nav_host,
                "frame_host": frame_host,
            }
        )
    return rows


def wait_for(
    description: str,
    callback: Callable[[], Any],
    *,
    timeout: float = 8,
    interval: float = 0.05,
) -> Any:
    deadline = time.monotonic() + timeout
    last_error: AssertionError | None = None
    while time.monotonic() < deadline:
        try:
            value = callback()
            if value:
                return value
        except AssertionError as exc:
            last_error = exc
        time.sleep(interval)
    if last_error is not None:
        raise AssertionError(f"timed out waiting for {description}: {last_error}") from last_error
    raise AssertionError(f"timed out waiting for {description}")
