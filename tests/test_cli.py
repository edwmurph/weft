from __future__ import annotations

import base64
import inspect
import io
import shlex
from types import SimpleNamespace

import pytest
import codux.cli as cli_module
import codux.launcher as launcher_module
from codux.cli import is_transient_codex_title, repair_and_render
from codux.config import APP_DIR_ENV, WORKDIR_ENV, CoduxConfig
from codux.state import (
    AppState,
    StateLockTimeout,
    StateStore,
    Tab,
    now_iso,
    state_after_closing_tab,
)
from codux.titles import CODEX_TITLE_TEMPLATE, normalize_codex_title
from codux.tmux import TmuxError
from rich.console import Console
from typer.testing import CliRunner


@pytest.fixture(autouse=True)
def isolate_runtime_lock(monkeypatch, tmp_path):
    monkeypatch.setattr(cli_module, "runtime_lock_path", lambda: tmp_path / "state.runtime.lock")


def tab(tab_id: str) -> Tab:
    created_at = now_iso()
    return Tab(
        id=tab_id,
        title=tab_id,
        column="inbox",
        tmux_session="codux",
        tmux_window_id=f"@{tab_id}",
        tmux_pane_id=f"%{tab_id}",
        created_at=created_at,
        updated_at=created_at,
    )


def test_codux_command_uses_uv_project_root_without_cd(monkeypatch):
    monkeypatch.delenv(APP_DIR_ENV, raising=False)
    monkeypatch.delenv(WORKDIR_ENV, raising=False)
    command = cli_module.codux_command()
    root = shlex.quote(str(launcher_module.PROJECT_ROOT))

    assert command == (f"uv --quiet --no-progress --directory {root} --project {root} run codux")
    assert "cd " not in command


def test_codux_command_preserves_runtime_environment(monkeypatch, tmp_path):
    runtime_dir = tmp_path / "runtime"
    workdir = tmp_path / "repo"
    monkeypatch.setenv(APP_DIR_ENV, str(runtime_dir))
    monkeypatch.setenv(WORKDIR_ENV, str(workdir))

    command = cli_module.codux_command()
    root = shlex.quote(str(launcher_module.PROJECT_ROOT))

    assert command == (
        f"env CODUX_HOME={shlex.quote(str(runtime_dir))} "
        f"CODUX_WORKDIR={shlex.quote(str(workdir))} "
        f"uv --quiet --no-progress --directory {root} --project {root} run codux"
    )


def test_start_entrypoint_dispatches_start_command(monkeypatch):
    calls: list[tuple[str, object]] = []
    original_argv = ["start", "--no-attach"]

    class FakeStartCommand:
        def main(self, *, args, prog_name):
            calls.append(("main", (list(args), prog_name)))

    class FakeRootCommand:
        def get_command(self, ctx, name):
            calls.append(("get", (ctx, name)))
            return FakeStartCommand()

    monkeypatch.setattr(cli_module.sys, "argv", [*original_argv])
    monkeypatch.setattr(cli_module.typer.main, "get_command", lambda app: FakeRootCommand())

    cli_module.start_entrypoint()

    assert calls == [
        ("get", (None, "start")),
        ("main", (["--no-attach"], "start")),
    ]
    assert cli_module.sys.argv == original_argv


def test_public_shell_commands_stay_limited():
    root_command = cli_module.typer.main.get_command(cli_module.app)

    visible_commands = {
        name for name, command in root_command.commands.items() if not command.hidden
    }

    assert visible_commands == {"config", "delete-session", "doctor", "quit", "sessions", "start"}
    assert "new" not in root_command.commands
    assert "rename" not in root_command.commands
    assert "status" not in root_command.commands


def test_root_help_explains_workdir_scoped_runtime():
    result = CliRunner().invoke(cli_module.app, ["--help"])

    assert result.exit_code == 0
    assert "Codux is scoped to the directory where you run it" in result.output
    assert "codux config info" in result.output


def test_root_help_hides_shell_completion_options():
    result = CliRunner().invoke(cli_module.app, ["--help"])

    assert result.exit_code == 0
    assert "--install-completion" not in result.output
    assert "--show-completion" not in result.output
    assert "--help" in result.output


def test_root_completion_options_remain_registered():
    root_command = cli_module.typer.main.get_command(cli_module.app)
    option_names = {
        option for param in root_command.params for option in getattr(param, "opts", ())
    }

    assert cli_module.COMPLETION_OPTION_NAMES <= option_names


def test_start_attaches_existing_workdir_session_without_state_lock(monkeypatch):
    events: list[tuple[str, str]] = []

    class FakeTmux:
        def __init__(self, session_name):
            events.append(("init", session_name))

        def has_session(self):
            events.append(("has-session", ""))
            return True

        def install_look_and_keys(self, config, command):
            events.append(("install", command))

        def attach(self):
            events.append(("attach", ""))

    monkeypatch.setattr(cli_module, "ensure_config", lambda: CoduxConfig())
    monkeypatch.setattr(cli_module, "TmuxController", FakeTmux)
    monkeypatch.setattr(
        cli_module,
        "StateStore",
        lambda: (_ for _ in ()).throw(AssertionError("state should not be opened")),
    )

    cli_module.start()

    assert events == [
        ("init", "codux"),
        ("has-session", ""),
        ("install", cli_module.codux_command()),
        ("attach", ""),
    ]


def test_start_repairs_existing_workdir_session_even_if_project_root_differs(monkeypatch):
    events: list[tuple[str, str]] = []

    class FakeTmux:
        def __init__(self, session_name):
            events.append(("init", session_name))

        def has_session(self):
            events.append(("has-session", ""))
            return True

        def project_root(self):
            return "/tmp/old-codux"

        def install_look_and_keys(self, config, command):
            events.append(("install", command))

        def attach(self):
            events.append(("attach", ""))

    monkeypatch.setattr(cli_module, "ensure_config", lambda: CoduxConfig())
    monkeypatch.setattr(cli_module, "TmuxController", FakeTmux)
    monkeypatch.setattr(
        cli_module,
        "StateStore",
        lambda: (_ for _ in ()).throw(AssertionError("state should not be opened")),
    )

    cli_module.start()

    assert events == [
        ("init", "codux"),
        ("has-session", ""),
        ("install", cli_module.codux_command()),
        ("attach", ""),
    ]


def test_start_reports_busy_state_when_no_workdir_session(monkeypatch):
    output = io.StringIO()

    class FakeStore:
        def ensure(self):
            raise StateLockTimeout("timed out waiting for state lock: /tmp/state.lock")

    class FakeTmux:
        def __init__(self, session_name):
            pass

        def has_session(self):
            return False

    monkeypatch.setattr(cli_module, "ensure_config", lambda: CoduxConfig())
    monkeypatch.setattr(cli_module, "TmuxController", FakeTmux)
    monkeypatch.setattr(cli_module, "StateStore", FakeStore)
    monkeypatch.setattr(cli_module, "console", Console(file=output))

    with pytest.raises(cli_module.typer.Exit) as exc:
        cli_module.start()

    assert exc.value.exit_code == 1
    assert "Codux state is busy" in output.getvalue()
    assert "tmux attach -t codux" in output.getvalue()


def test_quit_detaches_workdir_session_without_state_lock(monkeypatch):
    events: list[tuple[str, str]] = []

    class FakeTmux:
        def __init__(self, session_name):
            events.append(("init", session_name))

        def has_session(self):
            events.append(("has-session", ""))
            return True

        def detach_clients(self):
            events.append(("detach", ""))

    monkeypatch.setattr(cli_module, "ensure_config", lambda: CoduxConfig())
    monkeypatch.setattr(cli_module, "TmuxController", FakeTmux)
    monkeypatch.setattr(
        cli_module,
        "StateStore",
        lambda: (_ for _ in ()).throw(AssertionError("state should not be opened")),
    )

    cli_module.quit()

    assert events == [("init", "codux"), ("has-session", ""), ("detach", "")]


def test_quit_kill_stops_workdir_session_without_state_lock(monkeypatch):
    events: list[tuple[str, str]] = []

    class FakeTmux:
        def __init__(self, session_name):
            events.append(("init", session_name))

        def has_session(self):
            events.append(("has-session", ""))
            return True

        def detach_clients(self):
            events.append(("detach", ""))

        def kill_session(self):
            events.append(("kill", ""))

    monkeypatch.setattr(cli_module, "ensure_config", lambda: CoduxConfig())
    monkeypatch.setattr(cli_module, "TmuxController", FakeTmux)
    monkeypatch.setattr(
        cli_module,
        "StateStore",
        lambda: (_ for _ in ()).throw(AssertionError("state should not be opened")),
    )

    cli_module.quit(kill=True)

    assert events == [("init", "codux"), ("has-session", ""), ("kill", "")]


def test_sessions_command_lists_codux_sessions(monkeypatch):
    output = io.StringIO()
    listed = [
        cli_module.CoduxSession(
            name="codux-current",
            workdir="/Users/me/current",
            runtime_dir="/tmp/current",
            project_root="/repo",
            window_count=2,
            created_at=1,
            attached_clients=1,
            current=True,
        ),
        cli_module.CoduxSession(
            name="codux-other",
            workdir="/Users/me/other",
            runtime_dir="/tmp/other",
            project_root="/repo",
            window_count=3,
            created_at=2,
            attached_clients=0,
        ),
    ]

    monkeypatch.setattr(
        cli_module,
        "load_config_and_tmux",
        lambda: (CoduxConfig(tmux_session="codux-current"), object()),
    )
    monkeypatch.setattr(cli_module, "list_codux_sessions", lambda current: listed)
    monkeypatch.setattr(cli_module, "console", Console(file=output))

    cli_module.list_sessions_command()

    rendered = output.getvalue()
    assert "codux-current" in rendered
    assert "codux-other" in rendered
    assert "/Users/me/current" in rendered


def test_delete_session_command_kills_without_confirmation(monkeypatch):
    output = io.StringIO()
    deleted: list[str] = []

    monkeypatch.setattr(cli_module, "kill_codux_session", lambda name: deleted.append(name) or True)
    monkeypatch.setattr(cli_module, "console", Console(file=output))

    cli_module.delete_session_command("codux-old")

    assert deleted == ["codux-old"]
    assert "Deleted tmux session: codux-old" in output.getvalue()


def test_delete_session_command_reports_missing_session(monkeypatch):
    output = io.StringIO()

    monkeypatch.setattr(cli_module, "kill_codux_session", lambda name: False)
    monkeypatch.setattr(cli_module, "console", Console(file=output))

    with pytest.raises(cli_module.typer.Exit) as exc:
        cli_module.delete_session_command("missing")

    assert exc.value.exit_code == 1
    assert "tmux session not found: missing" in output.getvalue()


def test_config_info_reports_workdir_runtime_without_creating_config(monkeypatch, tmp_path):
    output = io.StringIO()
    home = tmp_path / "home"
    workdir = tmp_path / "repo"
    workdir.mkdir()
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.delenv(APP_DIR_ENV, raising=False)
    monkeypatch.delenv(WORKDIR_ENV, raising=False)
    monkeypatch.chdir(workdir)
    monkeypatch.setattr(cli_module, "console", Console(file=output, width=200))

    cli_module.config_info_command()

    rendered = output.getvalue()
    assert str(workdir) in rendered
    assert str(home / ".codux" / "workdirs") in rendered
    assert "config.toml" in rendered
    assert "state.json" in rendered
    assert "codux-" in rendered
    assert not cli_module.config_path().exists()


def test_config_path_command_prints_workdir_scoped_config(monkeypatch, tmp_path):
    output = io.StringIO()
    workdir = tmp_path / "repo"
    workdir.mkdir()
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.delenv(APP_DIR_ENV, raising=False)
    monkeypatch.delenv(WORKDIR_ENV, raising=False)
    monkeypatch.chdir(workdir)
    monkeypatch.setattr(cli_module, "console", Console(file=output, width=200))

    cli_module.config_path_command()

    rendered = output.getvalue().strip()
    assert rendered.endswith("config.toml")
    assert ".codux/workdirs/" in rendered


def test_config_show_creates_and_prints_default_config(monkeypatch, tmp_path):
    output = io.StringIO()
    workdir = tmp_path / "repo"
    workdir.mkdir()
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.delenv(APP_DIR_ENV, raising=False)
    monkeypatch.delenv(WORKDIR_ENV, raising=False)
    monkeypatch.chdir(workdir)
    monkeypatch.setattr(cli_module, "console", Console(file=output))

    cli_module.config_show_command()

    rendered = output.getvalue()
    assert cli_module.config_path().exists()
    assert "Codux runtime configuration for one launch directory" in rendered
    assert "tmux_session =" not in rendered
    assert 'columns = ["inbox", "implement", "ship"]' in rendered


def test_doctor_does_not_create_new_workspace_files(monkeypatch, tmp_path):
    output = io.StringIO()
    workdir = tmp_path / "repo"
    workdir.mkdir()

    class FakeTmux:
        def __init__(self, session_name):
            self.session_name = session_name

        @staticmethod
        def available():
            return True

        @staticmethod
        def version_text():
            return "tmux 3.5"

        def has_session(self):
            return False

    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.delenv(APP_DIR_ENV, raising=False)
    monkeypatch.delenv(WORKDIR_ENV, raising=False)
    monkeypatch.chdir(workdir)
    monkeypatch.setattr(cli_module, "TmuxController", FakeTmux)
    monkeypatch.setattr(cli_module.shutil, "which", lambda binary: f"/bin/{binary}")
    monkeypatch.setattr(cli_module, "console", Console(file=output))

    cli_module.doctor()

    rendered = output.getvalue()
    assert "config:" in rendered
    assert "created by `codux start`" in rendered
    assert not cli_module.config_path().exists()
    assert not cli_module.state_path().exists()


def test_config_init_refuses_existing_config_without_force(monkeypatch, tmp_path):
    output = io.StringIO()
    workdir = tmp_path / "repo"
    workdir.mkdir()
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.delenv(APP_DIR_ENV, raising=False)
    monkeypatch.delenv(WORKDIR_ENV, raising=False)
    monkeypatch.chdir(workdir)
    monkeypatch.setattr(cli_module, "console", Console(file=output))

    cli_module.config_init_command()
    with pytest.raises(cli_module.typer.Exit) as exc:
        cli_module.config_init_command()

    assert exc.value.exit_code == 1
    assert "config already exists" in output.getvalue()


def test_sessions_popup_requires_confirmation_before_delete(monkeypatch):
    output = io.StringIO()
    controls: list[str] = []
    deleted: list[str] = []
    session = cli_module.CoduxSession(
        name="codux-other",
        workdir="/Users/me/other",
        runtime_dir="/tmp/other",
        project_root="/repo",
        window_count=1,
        created_at=1,
        attached_clients=0,
    )
    keys = iter(["c", "y", "\x1b"])

    monkeypatch.setattr(
        cli_module,
        "load_config_and_tmux",
        lambda: (CoduxConfig(tmux_session="codux-current"), object()),
    )
    monkeypatch.setattr(
        cli_module,
        "other_codux_sessions",
        lambda current: [] if deleted else [session],
    )
    monkeypatch.setattr(
        cli_module,
        "kill_codux_session",
        lambda name: deleted.append(name) or True,
    )
    monkeypatch.setattr(cli_module, "read_single_key", lambda: next(keys))
    monkeypatch.setattr(cli_module, "write_terminal_control", controls.append)
    monkeypatch.setattr(cli_module, "console", Console(file=output))

    cli_module.popup_sessions_command()

    assert deleted == ["codux-other"]
    assert controls == [
        f"\033[?25l{cli_module.ENABLE_MOUSE_CAPTURE}\033[2J\033[H",
        f"{cli_module.DISABLE_MOUSE_CAPTURE}\033[?25h",
    ]
    assert "Delete codux-other? y/N" in output.getvalue()


def test_hidden_command_signatures_are_preserved():
    assert list(inspect.signature(cli_module.refresh_command).parameters) == []
    assert list(inspect.signature(cli_module.activate_window_command).parameters) == ["window_id"]
    assert list(inspect.signature(cli_module.focus_window_command).parameters) == [
        "window_id",
        "focus",
    ]


def test_state_after_closing_active_tab_selects_neighbor():
    first = tab("first")
    second = tab("second")
    state = AppState(tabs=[first, second], active_tab_id=first.id, focus="codex")

    updated = state_after_closing_tab(state, first.id)

    assert updated.tabs == [second]
    assert updated.active_tab_id == second.id
    assert updated.focus == "codex"


def test_state_after_closing_inactive_tab_preserves_active_tab():
    first = tab("first")
    second = tab("second")
    state = AppState(tabs=[first, second], active_tab_id=first.id, focus="nav")

    updated = state_after_closing_tab(state, second.id)

    assert updated.tabs == [first]
    assert updated.active_tab_id == first.id
    assert updated.focus == "nav"


def test_codex_runtime_titles_are_accepted_for_display():
    assert is_transient_codex_title("Starting | 019e")
    assert is_transient_codex_title("Ready | 019e")
    assert not is_transient_codex_title("Implement auth flow")
    assert normalize_codex_title("Starting | 019e") == "Starting | 019e"
    assert normalize_codex_title("Ready | 019e") == "Ready | 019e"
    assert normalize_codex_title("Working | 019e") == "Working | 019e"


def test_frame_pane_does_not_append_trailing_newline(monkeypatch):
    class FakeStdin:
        def __init__(self, data: bytes) -> None:
            self.buffer = io.BytesIO(data)

        def fileno(self) -> int:
            return 0

        def isatty(self) -> bool:
            return False

    output = io.StringIO()
    payload = base64.b64encode(b"abc").decode("ascii")

    monkeypatch.setattr(cli_module.sys, "stdin", FakeStdin(f"CODUX_FRAME:{payload}\n".encode()))
    monkeypatch.setattr(cli_module, "console", Console(file=output))

    cli_module.frame_pane_command()

    assert output.getvalue() == "\033[?25l\033[2J\033[Habc"


def test_frame_pane_disables_terminal_echo(monkeypatch):
    calls: list[tuple[str, object]] = []
    old_attrs = [1, 2, 3, cli_module.termios.ECHO | 0x100, 5, 6]

    monkeypatch.setattr(cli_module.termios, "tcgetattr", lambda fd: old_attrs)
    monkeypatch.setattr(
        cli_module.termios,
        "tcsetattr",
        lambda fd, when, attrs: calls.append(("set", attrs)),
    )

    assert cli_module._disable_stdin_echo(10) == old_attrs

    assert calls == [("set", [1, 2, 3, 0x100, 5, 6])]


def test_repair_and_render_recovers_live_tmux_tabs(monkeypatch, tmp_path):
    recovered = tab("live").with_updates(title="Recovered")
    refreshed: list[AppState] = []

    class FakeTmux:
        def has_session(self):
            return True

        def window_exists(self, window_id):
            return True

        def pane_exists(self, pane_id):
            return True

        def pane_title(self, pane_id):
            return None

        def recoverable_tabs(self, config):
            return [recovered]

        def active_tab_id_from_tmux(self):
            return recovered.id

        def refresh_static_panes(self, config, state):
            refreshed.append(state)

    store = StateStore(tmp_path / "state.json")
    store.write(AppState())

    state = repair_and_render(CoduxConfig(), store, FakeTmux())

    assert state.tabs == [recovered]
    assert state.active_tab_id == recovered.id
    assert refreshed[-1].active_tab_id == recovered.id


def test_repair_and_render_reads_live_codex_title(monkeypatch, tmp_path):
    active = tab("active").with_updates(title=CODEX_TITLE_TEMPLATE)
    store = StateStore(tmp_path / "state.json")
    store.write(AppState(tabs=[active], active_tab_id=active.id))

    class FakeTmux:
        def has_session(self):
            return True

        def window_exists(self, window_id):
            return True

        def pane_exists(self, pane_id):
            return True

        def pane_title(self, pane_id):
            return "Implement auth"

        def recoverable_tabs(self, config):
            return []

        def active_tab_id_from_tmux(self):
            return active.id

        def refresh_static_panes(self, config, state):
            pass

    state = repair_and_render(CoduxConfig(), store, FakeTmux())

    assert state.active_tab.codex_title == "Implement auth"
    assert store.read().active_tab.codex_title == "Implement auth"


def test_new_defaults_stored_title_to_codex_template(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    store.write(AppState())
    claimed_titles: list[str] = []

    class FakeTmux:
        def has_session(self):
            return True

        def ensure_session(self, config):
            pass

        def claim_spare_tab_window(self, config, state, title, tab_id):
            claimed_titles.append(title)
            return SimpleNamespace(
                window_id="@new",
                content_pane_id="%content",
                nav_pane_id="%nav",
            )

        def remove_empty_windows(self):
            pass

        def refresh_window_frame_panes(self, config, state, window_id):
            pass

        def refresh_window_frame_colors(self, config, state, window_id):
            pass

        def select_window(self, window_id):
            pass

        def select_pane(self, pane_id):
            pass

        def prepare_spare_window_async(self):
            pass

    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, FakeTmux()))
    monkeypatch.setattr(cli_module, "refresh_runtime_async", lambda: None)

    cli_module.new(None)

    assert claimed_titles == [CODEX_TITLE_TEMPLATE]
    assert store.read().active_tab.title == CODEX_TITLE_TEMPLATE


def test_new_focuses_codex_pane(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    store.write(AppState())
    events: list[tuple[str, str]] = []
    refreshed: list[AppState] = []
    color_refreshed: list[AppState] = []

    class FakeTmux:
        def has_session(self):
            return True

        def ensure_session(self, config):
            pass

        def claim_spare_tab_window(self, config, state, title, tab_id):
            return SimpleNamespace(
                window_id="@new",
                content_pane_id="%content",
                nav_pane_id="%nav",
            )

        def remove_empty_windows(self):
            events.append(("remove-empty", ""))

        def refresh_window_frame_panes(self, config, state, window_id):
            refreshed.append(state)

        def refresh_window_frame_colors(self, config, state, window_id):
            events.append(("colors", window_id))
            color_refreshed.append(state)

        def select_window(self, window_id):
            events.append(("window", window_id))

        def select_pane(self, pane_id):
            events.append(("pane", pane_id))

        def prepare_spare_window_async(self):
            pass

    fake_tmux = FakeTmux()
    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, fake_tmux))
    monkeypatch.setattr(cli_module, "refresh_runtime_async", lambda: None)

    cli_module.new("Test")

    state = store.read()
    assert state.active_tab is not None
    assert state.focus == "codex"
    assert state.active_tab.tmux_pane_id == "%content"
    assert refreshed[-1].focus == "codex"
    assert color_refreshed[-1].focus == "codex"
    assert events == [
        ("window", "@new"),
        ("pane", "%content"),
        ("colors", "@new"),
        ("remove-empty", ""),
    ]


def test_focus_window_ignores_frame_refresh_race(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    active = tab("active")
    store.write(AppState(tabs=[active], active_tab_id=active.id, focus="codex"))

    class FakeTmux:
        def active_window_id(self):
            return active.tmux_window_id

        def refresh_window_frame_colors(self, config, state, window_id):
            raise TmuxError("pane went away")

    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, FakeTmux()))

    cli_module.focus_window_command(active.tmux_window_id, "nav")

    state = store.read()
    assert state.active_tab_id == active.id
    assert state.focus == "nav"


def test_focus_window_is_best_effort_when_runtime_is_unavailable(monkeypatch):
    def fail_runtime():
        raise RuntimeError("runtime changed")

    monkeypatch.setattr(cli_module, "load_runtime", fail_runtime)

    cli_module.focus_window_command("@missing", "nav")


def test_activate_window_updates_state_without_runtime_lock(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    first = tab("first")
    second = tab("second")
    store.write(AppState(tabs=[first, second], active_tab_id=second.id, focus="nav"))
    refreshed: list[tuple[AppState, str]] = []

    class FakeTmux:
        def active_window_id(self):
            return first.tmux_window_id

        def refresh_window_frame_colors(self, config, state, window_id):
            refreshed.append((state, window_id))

    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, FakeTmux()))

    cli_module.activate_window_command(first.tmux_window_id)

    state = store.read()
    assert state.active_tab_id == first.id
    assert state.focus == "nav"
    assert refreshed == [(state, first.tmux_window_id)]


def test_stale_activate_window_does_not_override_newer_focus(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    previous = tab("previous")
    current = tab("current")
    store.write(AppState(tabs=[previous, current], active_tab_id=current.id, focus="codex"))

    class FakeTmux:
        def active_window_id(self):
            return current.tmux_window_id

        def refresh_window_frame_colors(self, config, state, window_id):
            raise AssertionError("stale activation should not refresh")

    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, FakeTmux()))

    cli_module.activate_window_command(previous.tmux_window_id)

    state = store.read()
    assert state.active_tab_id == current.id
    assert state.focus == "codex"


def test_finish_close_last_tab_renders_empty_before_killing_old_window(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    store.write(AppState(focus="nav"))
    events: list[tuple[str, object]] = []

    class FakeTmux:
        def ensure_empty_window(self, config):
            events.append(("ensure-empty", None))
            return "@empty"

        def refresh_window_frame_panes(self, config, state, window_id):
            events.append(("refresh-window", window_id))

        def select_window(self, window_id):
            events.append(("select-window", window_id))

        def nav_pane_for_window(self, window_id):
            events.append(("nav-pane", window_id))
            return "%nav"

        def select_pane(self, pane_id):
            events.append(("select-pane", pane_id))

        def kill_window(self, window_id):
            events.append(("kill-window", window_id))

    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, FakeTmux()))

    cli_module.finish_close_window_command("@old")

    assert events == [
        ("ensure-empty", None),
        ("refresh-window", "@empty"),
        ("select-window", "@empty"),
        ("nav-pane", "@empty"),
        ("select-pane", "%nav"),
        ("kill-window", "@old"),
    ]


def test_frame_pane_marks_tmux_pane_ready(monkeypatch):
    calls: list[tuple[str, ...]] = []
    monkeypatch.setenv("TMUX_PANE", "%1")

    def fake_run(args, **kwargs):
        calls.append(tuple(args))

    monkeypatch.setattr(cli_module.subprocess, "run", fake_run)

    cli_module._mark_frame_pane_ready()

    assert calls == [
        (
            "tmux",
            "set-option",
            "-p",
            "-t",
            "%1",
            cli_module.FRAME_HOST_OPTION,
            cli_module.FRAME_HOST_VERSION,
        )
    ]


def test_rename_active_tab_updates_state_and_tmux(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    active = tab("active")
    store.write(AppState(tabs=[active], active_tab_id=active.id, focus="codex"))
    events: list[tuple[str, str, str] | tuple[str, AppState]] = []

    class FakeTmux:
        def rename_window(self, window_id, title):
            events.append(("rename-window", window_id, title))

        def set_pane_title(self, pane_id, title):
            events.append(("set-pane-title", pane_id, title))

    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, FakeTmux()))
    monkeypatch.setattr(cli_module, "refresh_runtime_async", lambda: None)

    state = cli_module.rename_active_tab("Renamed")

    assert state.active_tab is not None
    assert state.active_tab.title == "Renamed"
    assert store.read().active_tab.title == "Renamed"
    assert events == [
        ("rename-window", active.tmux_window_id, "Renamed"),
        ("set-pane-title", active.tmux_pane_id, "Renamed"),
    ]


def test_rename_with_codex_placeholder_preserves_pane_title(monkeypatch, tmp_path):
    store = StateStore(tmp_path / "state.json")
    active = tab("active")
    store.write(AppState(tabs=[active], active_tab_id=active.id, focus="codex"))
    events: list[tuple[str, str, str]] = []

    class FakeTmux:
        def rename_window(self, window_id, title):
            events.append(("rename-window", window_id, title))

        def set_pane_title(self, pane_id, title):
            events.append(("set-pane-title", pane_id, title))

    monkeypatch.setattr(cli_module, "load_runtime", lambda: (CoduxConfig(), store, FakeTmux()))
    monkeypatch.setattr(cli_module, "refresh_runtime_async", lambda: None)

    state = cli_module.rename_active_tab(f"Task {CODEX_TITLE_TEMPLATE}")

    assert state.active_tab.title == f"Task {CODEX_TITLE_TEMPLATE}"
    assert events == [("rename-window", active.tmux_window_id, f"Task {CODEX_TITLE_TEMPLATE}")]


def test_help_popup_captures_mouse_until_escape(monkeypatch):
    output = io.StringIO()
    controls: list[str] = []

    monkeypatch.setattr(cli_module, "ensure_config", lambda: CoduxConfig())
    monkeypatch.setattr(cli_module, "console", Console(file=output))
    monkeypatch.setattr(cli_module, "read_single_key", lambda: "\x1b")
    monkeypatch.setattr(cli_module, "write_terminal_control", controls.append)

    cli_module.popup_help_command()

    assert controls == [
        f"\033[?25l{cli_module.ENABLE_MOUSE_CAPTURE}\033[2J\033[H",
        f"{cli_module.DISABLE_MOUSE_CAPTURE}\033[?25h",
    ]
    assert output.getvalue().splitlines()[1].strip() == (
        "Codux runs one tmux workspace per launch directory. Same directory reattaches."
    )
    assert not output.getvalue().endswith("\n")


def test_rename_popup_describes_template_variables():
    lines, cursor_row, cursor_column = cli_module._rename_popup_screen(
        f"Task {CODEX_TITLE_TEMPLATE}",
        len("Task "),
        "",
        80,
        10,
    )

    input_line = f"> Task {CODEX_TITLE_TEMPLATE}"

    assert "Enter a nav tab title template with live variables." in lines
    assert "Variables:" in lines
    assert "  {codex}  Live title from the Codex pane" in lines
    assert "New title template:" not in lines
    assert lines[lines.index("Rename active tab") + 1] == (
        "Enter a nav tab title template with live variables."
    )
    assert lines[lines.index("Variables:") - 1] == ""
    assert input_line in lines
    assert lines[lines.index(input_line) - 1] == ""
    assert lines[lines.index(input_line) + 1] == ""
    assert cursor_row == lines.index(input_line) + 1
    assert cursor_column == len("> Task ") + 1
    assert lines[-1] == "Enter save  Esc cancel  Arrows move cursor  Ctrl-A/E ends"


def test_rename_popup_highlights_input_row():
    rendered = cli_module._render_popup_line(
        "> Task",
        row=4,
        cursor_row=4,
        width=20,
    )

    assert rendered.startswith(cli_module.RENAME_INPUT_BACKGROUND)
    assert cli_module.CLEAR_TO_LINE_END in rendered
    assert rendered.endswith(cli_module.RESET_TERMINAL_STYLE)


def test_rename_popup_visual_cursor_is_one_row_above_edit_row():
    assert cli_module._visual_cursor_row(7) == 6
    assert cli_module._visual_cursor_row(1) == 1


def test_rename_input_ctrl_a_moves_cursor_to_start():
    buffer, cursor = cli_module._edit_rename_buffer("Task", 4, "\x01")
    buffer, cursor = cli_module._edit_rename_buffer(buffer, cursor, "New ")

    assert buffer == "New Task"
    assert cursor == len("New ")


def test_rename_input_arrow_keys_move_cursor():
    buffer, cursor = cli_module._edit_rename_buffer("Task", 4, "\x1b[D")
    buffer, cursor = cli_module._edit_rename_buffer(buffer, cursor, "\x1b[D")
    buffer, cursor = cli_module._edit_rename_buffer(buffer, cursor, "X")
    buffer, cursor = cli_module._edit_rename_buffer(buffer, cursor, "\x1b[C")
    buffer, cursor = cli_module._edit_rename_buffer(buffer, cursor, "Y")

    assert buffer == "TaXsYk"
    assert cursor == len("TaXsY")


def test_tty_key_reader_collects_arrow_escape_sequence(monkeypatch):
    keys = iter([b"\x1b", b"[", b"D"])

    monkeypatch.setattr(cli_module.os, "read", lambda fd, count: next(keys))
    monkeypatch.setattr(
        cli_module.select, "select", lambda reads, writes, errors, timeout: (reads, [], [])
    )

    assert cli_module._read_key_from_tty(9) == "\x1b[D"


def test_tty_key_reader_returns_bare_escape_when_sequence_never_arrives(monkeypatch):
    monkeypatch.setattr(cli_module.os, "read", lambda fd, count: b"\x1b")
    monkeypatch.setattr(
        cli_module.select, "select", lambda reads, writes, errors, timeout: ([], [], [])
    )

    assert cli_module._read_key_from_tty(9) == "\x1b"
