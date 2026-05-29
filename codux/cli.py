from __future__ import annotations

import shlex
import shutil
import subprocess
import sys
import time
import uuid
import base64
import os
import select
import termios
import zlib
from dataclasses import replace
from functools import wraps

import typer
from rich.console import Console
from rich.table import Table

from codux.config import (
    ConfigError,
    CoduxConfig,
    app_dir,
    config_path,
    current_workdir,
    default_config_text,
    default_tmux_session,
    ensure_config,
    ensure_runtime_environment,
    load_config,
)
from codux.launcher import codux_cli_args, codux_cli_shell_command
from codux.navigation import select_grid_tab
from codux.nav_pane import run_nav_pane
from codux.render import render_empty_state, render_help, render_nav
from codux.runtime_lock import runtime_lock
from codux.runtime_lock import runtime_lock_path as default_runtime_lock_path
from codux.sessions import (
    CoduxSession,
    current_tmux_session,
    delete_codux_workspace,
    display_path,
    kill_codux_session,
    list_codux_sessions,
    list_codux_workspaces,
    other_codux_sessions,
)
from codux.state import (
    AppState,
    FocusTarget,
    StateError,
    StateLockTimeout,
    StateStore,
    Tab,
    now_iso,
    prune_stale_tabs,
    state_after_closing_tab,
    state_path,
)
from codux.title_sync import state_with_live_codex_titles
from codux import titles as title_helpers
from codux.titles import (
    CODEX_TITLE_TEMPLATE,
    TITLE_TEMPLATE_VARIABLES,
    normalize_codex_title,
    title_uses_codex_placeholder,
)
from codux.tmux import FRAME_HOST_OPTION, FRAME_HOST_VERSION, TmuxController


COMPLETION_OPTION_NAMES = {"--install-completion", "--show-completion"}
APP_HELP = """
Start, inspect, or detach Codux tmux workspaces for Codex.

Codux is scoped to the directory where you run it. Each launch directory gets:
- a stable runtime directory under ~/.codux/workdirs/<workdir-id>
- config.toml and state.json files
- a tmux session

Running Codux again from the same directory reattaches to that workspace.
Running Codux from a different directory creates a separate one.

Use `codux config info` to see the active workdir, runtime directory, config
file, state file, and tmux session.
"""
CONFIG_HELP = """
Inspect or initialize the config.toml for the current Codux workdir.

The config file is workdir-scoped by default. It controls:
- tmux session name
- Codex pane command
- nav columns and key bindings
"""


class CoduxTyperGroup(typer.core.TyperGroup):
    def format_help(self, ctx, formatter) -> None:
        completion_options = [
            param
            for param in self.params
            if any(option in COMPLETION_OPTION_NAMES for option in getattr(param, "opts", ()))
        ]
        original_hidden = [option.hidden for option in completion_options]
        for option in completion_options:
            option.hidden = True
        try:
            super().format_help(ctx, formatter)
        finally:
            for option, hidden in zip(completion_options, original_hidden, strict=True):
                option.hidden = hidden


app = typer.Typer(
    cls=CoduxTyperGroup,
    help=APP_HELP,
    invoke_without_command=True,
    no_args_is_help=False,
    subcommand_metavar="[COMMAND]",
)
config_app = typer.Typer(help=CONFIG_HELP, no_args_is_help=True)
app.add_typer(config_app, name="config")
console = Console()
RENAME_INPUT_PREFIX = "> "
RENAME_INPUT_BACKGROUND = "\033[48;2;30;35;45m"
RESET_TERMINAL_STYLE = "\033[0m"
CLEAR_TO_LINE_END = "\033[K"
SESSIONS_POPUP_WIDTH = 96
SESSIONS_POPUP_HEIGHT = 18
ENABLE_MOUSE_CAPTURE = "\033[?1000h\033[?1002h\033[?1006h"
DISABLE_MOUSE_CAPTURE = "\033[?1006l\033[?1002l\033[?1000l"
ESCAPE_SEQUENCE_TIMEOUT_SECONDS = 0.12
MAX_ESCAPE_SEQUENCE_BYTES = 8


def codux_command() -> str:
    return codux_cli_shell_command()


@app.callback(invoke_without_command=True)
def root_command(
    ctx: typer.Context,
    attach: bool = typer.Option(
        True,
        "--attach/--no-attach",
        help="Attach to this workdir's tmux session after preparing it.",
    ),
) -> None:
    if ctx.invoked_subcommand is None:
        start_dashboard(attach=attach)


def load_runtime() -> tuple[CoduxConfig, StateStore, TmuxController]:
    config, tmux = load_config_and_tmux()
    store = StateStore()
    store.ensure()
    return config, store, tmux


def load_config_and_tmux() -> tuple[CoduxConfig, TmuxController]:
    ensure_runtime_environment()
    config = ensure_config()
    return config, TmuxController(config.tmux_session)


def exit_for_busy_state_lock(exc: StateLockTimeout, session_name: str) -> None:
    console.print(f"[red]error[/red] Codux state is busy: {exc}")
    console.print(
        f"If the dashboard is already running, attach with `tmux attach -t {session_name}`."
    )
    raise typer.Exit(1)


def _with_runtime_lock(fn=None, *, wait: bool = False):
    def decorator(inner):
        @wraps(inner)
        def wrapped(*args, **kwargs):
            with runtime_lock(path=runtime_lock_path(), wait=wait) as acquired:
                if not acquired:
                    return None
                return inner(*args, **kwargs)

        return wrapped

    return decorator if fn is None else decorator(fn)


def runtime_lock_path():
    return default_runtime_lock_path(state_path())


def repair_and_render(
    config: CoduxConfig,
    store: StateStore,
    tmux: TmuxController,
    *,
    repaint_nav: bool = False,
) -> AppState:
    initial = store.read()
    repaired_state = repaired_runtime_state(config, initial, tmux)

    state = store.update(lambda current: repaired_state if current == initial else current)
    render_runtime(config, state, tmux)
    if repaint_nav:
        tmux.request_nav_repaint()
    return state


def repaired_runtime_state(
    config: CoduxConfig,
    state: AppState,
    tmux: TmuxController,
) -> AppState:
    repaired, _ = prune_stale_tabs(
        state,
        lambda tab: (
            tmux.has_session()
            and tmux.window_exists(tab.tmux_window_id)
            and tmux.pane_exists(tab.tmux_pane_id)
        ),
    )
    recovered_tabs = [
        tab
        for tab in tmux.recoverable_tabs(config)
        if tab.id not in {existing.id for existing in repaired.tabs}
    ]
    tabs: list[Tab] = []
    for tab in [*repaired.tabs, *recovered_tabs]:
        changes = {}
        if tab.column not in config.columns:
            changes["column"] = config.columns[0]
        tabs.append(tab.with_updates(**changes) if changes else tab)
    state_with_recovered_tabs = replace(repaired, tabs=tabs)
    state_with_recovered_tabs = state_with_live_codex_titles(
        state_with_recovered_tabs,
        lambda pane_id: tmux.pane_title(pane_id),
    )
    tabs = state_with_recovered_tabs.tabs
    tab_ids = {tab.id for tab in tabs}
    active_tab_id = state_with_recovered_tabs.active_tab_id
    if active_tab_id not in tab_ids:
        tmux_active_tab_id = tmux.active_tab_id_from_tmux()
        active_tab_id = tmux_active_tab_id if tmux_active_tab_id in tab_ids else None
    if active_tab_id is None and tabs:
        active_tab_id = tabs[0].id
    focus = "nav" if active_tab_id is None else state_with_recovered_tabs.focus
    return replace(state_with_recovered_tabs, active_tab_id=active_tab_id, focus=focus)


def render_runtime(config: CoduxConfig, state: AppState, tmux: TmuxController) -> None:
    if tmux.has_session():
        tmux.refresh_static_panes(config, state)


def generated_tab_title(tmux: TmuxController, tab: Tab) -> str | None:
    return normalize_codex_title(tmux.pane_title(tab.tmux_pane_id))


def is_transient_codex_title(title: str) -> bool:
    return title_helpers.is_transient_codex_title(title)


def select_active_or_empty(config: CoduxConfig, state: AppState, tmux: TmuxController) -> None:
    if state.active_tab is not None:
        tmux.remove_empty_windows()
        tmux.select_window(state.active_tab.tmux_window_id)
        if state.focus == "nav":
            focus_nav_for_window(tmux, state.active_tab.tmux_window_id)
        else:
            tmux.select_pane(state.active_tab.tmux_pane_id)
        return
    empty_window_id = prepare_empty_dashboard(config, state, tmux)
    tmux.select_window(empty_window_id)
    if state.focus == "nav":
        focus_nav_for_window(tmux, empty_window_id)
    else:
        focus_content_for_window(tmux, empty_window_id)


def prepare_empty_dashboard(config: CoduxConfig, state: AppState, tmux: TmuxController) -> str:
    empty_window_id = tmux.ensure_empty_window(config)
    tmux.refresh_window_frame_panes(config, state, empty_window_id)
    return empty_window_id


def focus_nav_for_window(tmux: TmuxController, window_id: str) -> None:
    pane_id = tmux.nav_pane_for_window(window_id)
    if pane_id:
        tmux.select_pane(pane_id)


def focus_content_for_window(tmux: TmuxController, window_id: str) -> None:
    pane_id = tmux.content_pane_for_window(window_id)
    if pane_id:
        tmux.select_pane(pane_id)


def current_tab_or_exit(state: AppState) -> Tab:
    tab = state.active_tab
    if tab is None:
        raise typer.BadParameter("no active Codex session")
    return tab


def default_config_for_current_workdir() -> CoduxConfig:
    return CoduxConfig.from_mapping({}, tmux_session_default=default_tmux_session())


def start_dashboard(*, attach: bool = True) -> None:
    config, tmux = load_config_and_tmux()
    if tmux.has_session():
        tmux.install_look_and_keys(config, codux_command())
        if attach:
            tmux.attach()
            return
    store = StateStore()
    try:
        store.ensure()
    except StateLockTimeout as exc:
        if attach and tmux.has_session():
            tmux.attach()
            return
        exit_for_busy_state_lock(exc, config.tmux_session)
    tmux.ensure_session(config)
    tmux.install_look_and_keys(config, codux_command())
    state = repair_and_render(config, store, tmux)
    tmux.ensure_spare_window(config, state)
    select_active_or_empty(config, state, tmux)
    if attach:
        tmux.attach()
    else:
        console.print(
            f"Prepared tmux session [bold]{config.tmux_session}[/bold] for {current_workdir()}."
        )
        console.print(f"Config: {config_path()}")


def new(title: str | None = typer.Argument(None, help="Optional tab title.")) -> None:
    """Create a new Codex tab."""
    config, store, tmux = load_runtime()
    had_session = tmux.has_session()
    tmux.ensure_session(config)
    if not had_session:
        tmux.install_look_and_keys(config, codux_command())
    tab_id = uuid.uuid4().hex[:8]
    title = title or CODEX_TITLE_TEMPLATE
    created = tmux.claim_spare_tab_window(config, store.read(), title, tab_id)
    created_at = now_iso()
    tab = Tab(
        id=tab_id,
        title=title,
        column=config.columns[0],
        tmux_session=config.tmux_session,
        tmux_window_id=created.window_id,
        tmux_pane_id=created.content_pane_id,
        created_at=created_at,
        updated_at=created_at,
    )

    def mutate(current: AppState) -> AppState:
        return AppState(
            tabs=[*current.tabs, tab],
            active_tab_id=tab.id,
            focus="codex",
        )

    state = store.update(mutate)
    tmux.refresh_window_frame_panes(config, state, tab.tmux_window_id)
    tmux.select_window(tab.tmux_window_id)
    tmux.select_pane(created.content_pane_id)
    tmux.refresh_window_frame_colors(config, state, tab.tmux_window_id)
    tmux.prepare_spare_window_async()
    tmux.remove_empty_windows()
    console.print(f"Created [bold]{tab.title}[/bold].")


def close(
    tab_id: str | None = typer.Argument(None, help="Tab id to close. Defaults to active."),
) -> None:
    """Close a Codex tab."""
    config, store, tmux = load_runtime()
    state = repair_and_render(config, store, tmux)
    target_id = tab_id or state.active_tab_id
    if target_id is None:
        console.print("No Codex tabs are open.")
        return
    target_index = next(
        (index for index, tab in enumerate(state.tabs) if tab.id == target_id), None
    )
    if target_index is None:
        raise typer.BadParameter(f"unknown tab id: {target_id}")
    target = state.tabs[target_index]

    def mutate(current: AppState) -> AppState:
        return state_after_closing_tab(current, target_id)

    state = store.update(mutate)
    if state.tabs:
        render_runtime(config, state, tmux)
    select_active_or_empty(config, state, tmux)
    tmux.kill_window(target.tmux_window_id)
    console.print(f"Closed [bold]{target.title}[/bold].")


def rename(title: str = typer.Argument(..., help="New active tab title.")) -> None:
    """Rename the active Codex tab."""
    rename_active_tab(title)
    console.print(f"Renamed tab to [bold]{title}[/bold].")


def rename_active_tab(title: str) -> AppState:
    config, store, tmux = load_runtime()
    state = store.read()
    target = current_tab_or_exit(state)

    def mutate(current: AppState) -> AppState:
        tabs = [
            tab.with_updates(title=title) if tab.id == target.id else tab for tab in current.tabs
        ]
        return replace(current, tabs=tabs)

    state = store.update(mutate)
    tmux.rename_window(target.tmux_window_id, title)
    if not title_uses_codex_placeholder(title):
        tmux.set_pane_title(target.tmux_pane_id, title)
    refresh_runtime_async()
    return state


@app.command()
def quit(
    kill: bool = typer.Option(
        False,
        "--kill",
        help="Kill the Codux tmux session instead of detaching clients.",
    ),
) -> None:
    """Detach or stop the Codux dashboard."""
    config, tmux = load_config_and_tmux()
    if not tmux.has_session():
        console.print(f"tmux session is not running: {config.tmux_session}")
        return
    if kill is True:
        tmux.kill_session()
    else:
        tmux.detach_clients()


@app.command("sessions")
def list_sessions_command() -> None:
    """List active Codux dashboard sessions."""
    config, _ = load_config_and_tmux()
    sessions = list_codux_sessions(config.tmux_session)
    if not sessions:
        console.print("No Codux sessions are running.")
        return
    table = Table(title="Codux Sessions")
    table.add_column("Current")
    table.add_column("Session")
    table.add_column("Workdir")
    table.add_column("Windows", justify="right")
    table.add_column("Clients", justify="right")
    for session in sessions:
        table.add_row(
            "*" if session.current else "",
            session.name,
            display_path(session.workdir),
            str(session.window_count),
            str(session.attached_clients),
        )
    console.print(table)


@app.command("delete-session")
def delete_session_command(
    session_name: str = typer.Argument(..., help="Exact tmux session name to delete."),
) -> None:
    """Delete a Codux dashboard session without confirmation."""
    if not kill_codux_session(session_name):
        console.print(f"tmux session not found: {session_name}")
        raise typer.Exit(1)
    console.print(f"Deleted tmux session: {session_name}")


@app.command("clear")
def clear_command() -> None:
    """Delete all Codux tmux sessions and saved workspaces after confirmation."""
    current_session = current_tmux_session()
    sessions = list_codux_sessions(current_session)
    workspaces = list_codux_workspaces()
    if not sessions and not workspaces:
        console.print("No Codux workspaces or sessions found.")
        return

    console.print("This will delete all Codux tmux sessions and workspace runtimes:")
    for session in sessions:
        current = " (current)" if session.current else ""
        console.print(f"- tmux session: {session.name}{current} {display_path(session.workdir)}")
    for workspace in workspaces:
        console.print(f"- workspace: {display_path(str(workspace))}")

    if not typer.confirm("Delete all Codux workspaces and sessions?", default=False):
        console.print("Delete canceled.")
        return

    failed_workspaces = [
        str(workspace) for workspace in workspaces if not delete_codux_workspace(workspace)
    ]
    failed_sessions = [
        session.name
        for session in sorted(sessions, key=lambda item: item.current)
        if not kill_codux_session(session.name)
    ]
    deleted_workspaces = len(workspaces) - len(failed_workspaces)
    deleted_sessions = len(sessions) - len(failed_sessions)
    console.print(
        f"Deleted {deleted_sessions} tmux session(s) and {deleted_workspaces} workspace(s)."
    )
    if failed_sessions or failed_workspaces:
        for name in failed_sessions:
            console.print(f"[red]failed[/red] tmux session: {name}")
        for path in failed_workspaces:
            console.print(f"[red]failed[/red] workspace: {path}")
        raise typer.Exit(1)


@config_app.command("info")
def config_info_command() -> None:
    """Show the workdir-scoped runtime paths and tmux session."""
    ensure_runtime_environment()
    path = config_path()
    config = load_config(path) if path.exists() else default_config_for_current_workdir()
    config_status = "exists" if path.exists() else "missing; run codux config init"
    console.out("Codux workdir runtime")
    console.out(f"Workdir: {current_workdir()}")
    console.out(f"Runtime dir: {app_dir()}")
    console.out(f"Config: {path} ({config_status})")
    console.out(f"State: {state_path()}")
    console.out(f"tmux session: {config.tmux_session}")
    console.out(f"Codex command: {config.codex_command}")


@config_app.command("path")
def config_path_command() -> None:
    """Print this workdir's config.toml path."""
    ensure_runtime_environment()
    console.out(str(config_path()))


@config_app.command("show")
def config_show_command() -> None:
    """Create the config if needed, then print config.toml."""
    ensure_runtime_environment()
    ensure_config()
    console.print(config_path().read_text(encoding="utf-8"), end="", markup=False)


@config_app.command("init")
def config_init_command(
    force: bool = typer.Option(
        False,
        "--force",
        "-f",
        help="Replace an existing config.toml with the generated default.",
    ),
) -> None:
    """Create the default config.toml for this workdir."""
    ensure_runtime_environment()
    path = config_path()
    force_enabled = force is True
    if path.exists() and not force_enabled:
        console.print(f"config already exists: {path}")
        console.print("Use `codux config show` to inspect it or `codux config init --force`.")
        raise typer.Exit(1)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(default_config_text(), encoding="utf-8")
    action = "Rewrote" if force_enabled else "Wrote"
    console.print(f"{action} config: {path}")


def move_left() -> None:
    """Move the active tab one column left."""
    move_active_column(-1)


def move_right() -> None:
    """Move the active tab one column right."""
    move_active_column(1)


def move_active_column(delta: int) -> None:
    config, store, tmux = load_runtime()
    state = repair_and_render(config, store, tmux)
    target = current_tab_or_exit(state)
    current_index = config.columns.index(target.column) if target.column in config.columns else 0
    next_index = max(0, min(len(config.columns) - 1, current_index + delta))
    next_column = config.columns[next_index]

    def mutate(current: AppState) -> AppState:
        tabs = [
            tab.with_updates(column=next_column) if tab.id == target.id else tab
            for tab in current.tabs
        ]
        return replace(current, tabs=tabs)

    state = store.update(mutate)
    render_runtime(config, state, tmux)
    select_active_or_empty(config, state, tmux)
    console.print(f"Moved [bold]{target.title}[/bold] to {next_column}.")


def next_tab() -> None:
    """Select the next tab."""
    select_relative(1)


def prev_tab() -> None:
    """Select the previous tab."""
    select_relative(-1)


def nav_up() -> None:
    """Select the visible tab above the active tab."""
    select_grid(delta_row=-1)


def nav_down() -> None:
    """Select the visible tab below the active tab."""
    select_grid(delta_row=1)


def nav_left() -> None:
    """Select the visible tab to the left of the active tab."""
    select_grid(delta_column=-1)


def nav_right() -> None:
    """Select the visible tab to the right of the active tab."""
    select_grid(delta_column=1)


def select_relative(delta: int) -> None:
    config, store, tmux = load_runtime()
    state = repair_and_render(config, store, tmux)
    if not state.tabs:
        select_active_or_empty(config, state, tmux)
        return
    active_index = next(
        (index for index, tab in enumerate(state.tabs) if tab.id == state.active_tab_id),
        0,
    )
    next_tab = state.tabs[(active_index + delta) % len(state.tabs)]
    state = store.update(lambda current: replace(current, active_tab_id=next_tab.id))
    render_runtime(config, state, tmux)
    select_active_or_empty(config, state, tmux)


def select_grid(delta_column: int = 0, delta_row: int = 0) -> None:
    config, store, tmux = load_runtime()
    state = repair_and_render(config, store, tmux)
    target = select_grid_tab(
        state.tabs,
        state.active_tab_id,
        config.columns,
        delta_column=delta_column,
        delta_row=delta_row,
    )
    if target is None:
        select_active_or_empty(config, state, tmux)
        return
    state = store.update(lambda current: replace(current, active_tab_id=target.id))
    render_runtime(config, state, tmux)
    select_active_or_empty(config, state, tmux)


def focus_nav() -> None:
    """Focus the nav pane."""
    set_focus("nav")


def focus_codex() -> None:
    """Focus the Codex pane."""
    set_focus("codex")


def toggle_focus() -> None:
    """Toggle focus between the nav and Codex panes."""
    config, store, tmux = load_runtime()
    state = store.read()
    active_window = state.active_tab.tmux_window_id if state.active_tab else tmux.empty_window_id()
    if active_window is None:
        active_window = tmux.ensure_empty_window(config)
    nav_pane_id = tmux.nav_pane_for_window(active_window)
    content_pane_id = tmux.content_pane_for_window(active_window)
    if nav_pane_id and nav_pane_id == content_pane_id:
        set_focus("codex" if state.focus == "nav" else "nav")
        return
    active_pane_id = tmux.active_pane_id()
    set_focus("codex" if active_pane_id == nav_pane_id else "nav")


def set_focus(focus: FocusTarget) -> None:
    config, store, tmux = load_runtime()
    state = store.update(lambda current: replace(current, focus=focus))
    tmux.refresh_frame_panes(config, state)
    select_active_or_empty(config, state, tmux)


@app.command("refresh")
@_with_runtime_lock(wait=True)
def refresh_dashboard_command() -> None:
    """Redraw the Codux dashboard and focus the nav pane."""
    config, store, tmux = load_runtime()
    if not tmux.has_session():
        console.print(f"tmux session is not running: {config.tmux_session}")
        return
    refresh_and_focus_nav(config, store, tmux)
    console.print("Refreshed Codux dashboard and focused nav.")


def refresh_and_focus_nav(
    config: CoduxConfig,
    store: StateStore,
    tmux: TmuxController,
) -> AppState:
    initial = store.read()
    repaired = repaired_runtime_state(config, initial, tmux)
    focused = replace(repaired, focus="nav")
    state = store.update(
        lambda current: focused if current == initial else replace(current, focus="nav")
    )
    render_runtime(config, state, tmux)
    select_active_or_empty(config, state, tmux)
    tmux.request_nav_repaint()
    return state


def status() -> None:
    """Print Codux state."""
    config, store, tmux = load_runtime()
    state = repair_and_render(config, store, tmux)
    table = Table(title="Codux")
    table.add_column("Active")
    table.add_column("ID")
    table.add_column("Title")
    table.add_column("Column")
    table.add_column("Window")
    for tab in state.tabs:
        table.add_row(
            "*" if tab.id == state.active_tab_id else "",
            tab.id,
            tab.title,
            tab.column,
            tab.tmux_window_id,
        )
    console.print(f"Config: {config_path()}")
    console.print(f"State: {state_path()}")
    console.print(f"tmux session: {config.tmux_session} ({'up' if tmux.has_session() else 'down'})")
    console.print(f"focus: {state.focus}")
    console.print(table)


@app.command()
def doctor() -> None:
    """Check local Codux dependencies and runtime files."""
    problems: list[str] = []
    warnings: list[str] = []
    try:
        ensure_runtime_environment()
        path = config_path()
        config = load_config(path) if path.exists() else default_config_for_current_workdir()
        store = StateStore()
        state = store.read() if state_path().exists() else AppState()
        tmux = TmuxController(config.tmux_session)
    except (ConfigError, StateError) as exc:
        console.print(f"[red]error[/red] {exc}")
        raise typer.Exit(1) from exc

    tmux_available = TmuxController.available()
    if not tmux_available:
        problems.append("tmux is not installed or not on PATH")
    else:
        console.print(f"[green]ok[/green] tmux: {TmuxController.version_text()}")
    codex_binary = shlex.split(config.codex_command)[0]
    if shutil.which(codex_binary) is None:
        warnings.append(f"Codex command is not on PATH: {config.codex_command}")
    else:
        console.print(f"[green]ok[/green] Codex command: {config.codex_command}")
    if tmux_available and state_path().exists():
        state = repair_and_render(config, store, tmux)
    console.print(f"[blue]info[/blue] workdir: {current_workdir()}")
    console.print(f"[blue]info[/blue] runtime dir: {app_dir()}")
    if path.exists():
        console.print(f"[green]ok[/green] config: {path}")
    else:
        console.print(f"[blue]info[/blue] config: {path} (created by running `codux`)")
    if state_path().exists():
        console.print(f"[green]ok[/green] state: {state_path()} ({len(state.tabs)} tabs)")
    else:
        console.print(f"[blue]info[/blue] state: {state_path()} (created by running `codux`)")
    console.print(
        "[blue]info[/blue] same workdir reattaches to this workspace; another "
        "workdir gets its own config, state, and tmux session."
    )
    console.print(
        "[blue]info[/blue] native tmux panes are used for NAV and direct Codex content; "
        "rounded frame panes provide the visible boxes."
    )
    console.print(
        "[blue]info[/blue] Codex panes run the configured command directly; Codux does "
        "not force a Codex theme or color palette."
    )
    console.print("[blue]info[/blue] Codux windows reset native tmux pane styles to default.")
    if not tmux_available:
        pass
    elif tmux.has_session():
        console.print(f"[green]ok[/green] tmux session: {config.tmux_session}")
    else:
        warnings.append(f"tmux session is not running: {config.tmux_session}")
    for warning in warnings:
        console.print(f"[yellow]warn[/yellow] {warning}")
    for problem in problems:
        console.print(f"[red]error[/red] {problem}")
    if problems:
        raise typer.Exit(1)


@app.command("_render-nav", hidden=True)
def render_nav_command() -> None:
    config, store, tmux = load_runtime()
    state = repair_and_render(config, store, tmux)
    console.print(render_nav(config, state), end="")


@app.command("_render-empty", hidden=True)
def render_empty_command() -> None:
    console.print(render_empty_state(), end="")


@app.command("_render-help", hidden=True)
def render_help_command() -> None:
    ensure_runtime_environment()
    config = ensure_config()
    console.print(render_help(config))


@app.command("_popup-help", hidden=True)
def popup_help_command() -> None:
    ensure_runtime_environment()
    config = ensure_config()
    write_terminal_control(f"\033[?25l{ENABLE_MOUSE_CAPTURE}\033[2J\033[H")
    try:
        console.file.write(render_help(config))
        console.file.flush()
        while True:
            key = read_single_key()
            if key in {"\x1b", ""}:
                break
    except EOFError:
        pass
    finally:
        write_terminal_control(f"{DISABLE_MOUSE_CAPTURE}\033[?25h")


@app.command("_popup-sessions", hidden=True)
def popup_sessions_command() -> None:
    config, _ = load_config_and_tmux()
    sessions = other_codux_sessions(config.tmux_session)
    selected_index = 0
    pending_kill: str | None = None
    status = ""

    def redraw() -> None:
        size = shutil.get_terminal_size((SESSIONS_POPUP_WIDTH, SESSIONS_POPUP_HEIGHT))
        lines = _sessions_popup_screen(
            sessions,
            selected_index,
            pending_kill,
            status,
            size.columns,
            size.lines,
        )
        console.file.write("\033[?25l\033[2J\033[H")
        console.file.write("\n".join(lines))
        console.file.flush()

    write_terminal_control(f"\033[?25l{ENABLE_MOUSE_CAPTURE}\033[2J\033[H")
    try:
        redraw()
        while True:
            key = read_single_key()
            if key in {"\x1b", ""}:
                break
            if pending_kill is not None:
                if key.lower() == "y":
                    killed = kill_codux_session(pending_kill)
                    status = f"Deleted {pending_kill}." if killed else f"Not found: {pending_kill}."
                    sessions = other_codux_sessions(config.tmux_session)
                    selected_index = min(selected_index, max(0, len(sessions) - 1))
                    pending_kill = None
                    redraw()
                elif key.lower() in {"n", "c"}:
                    status = "Delete canceled."
                    pending_kill = None
                    redraw()
                continue
            if key in {"\x1b[A", "\x1bOA"} and sessions:
                selected_index = max(0, selected_index - 1)
                redraw()
            elif key in {"\x1b[B", "\x1bOB"} and sessions:
                selected_index = min(len(sessions) - 1, selected_index + 1)
                redraw()
            elif key == "c" and sessions:
                pending_kill = sessions[selected_index].name
                status = ""
                redraw()
    except EOFError:
        pass
    finally:
        write_terminal_control(f"{DISABLE_MOUSE_CAPTURE}\033[?25h")


def _sessions_popup_screen(
    sessions: list[CoduxSession],
    selected_index: int,
    pending_kill: str | None,
    status: str,
    width: int,
    height: int,
) -> list[str]:
    lines = [
        "Other Codux sessions",
        "",
    ]
    if sessions:
        lines.extend(
            [
                f"{'':2} {'Session':<28} {'Clients':>7} {'Windows':>7}  Workdir",
                "",
            ]
        )
        for index, session in enumerate(sessions):
            marker = "›" if index == selected_index else " "
            lines.append(
                f"{marker} {session.name:<28.28} {session.attached_clients:>7} "
                f"{session.window_count:>7}  {display_path(session.workdir)}"
            )
    else:
        lines.append("No other Codux sessions are running.")

    footer = status or "↑/↓ select  c close  Esc cancel"
    if pending_kill is not None:
        footer = f"Delete {pending_kill}? y/N"
    if len(lines) + 1 < height:
        lines.extend([""] * (height - len(lines) - 1))
    lines.append(footer)
    return [_clip_popup_line(line, width) for line in lines[:height]]


def _clip_popup_line(line: str, width: int) -> str:
    return line[: max(0, width - 1)]


@app.command("_popup-rename", hidden=True)
def popup_rename_command() -> None:
    _, store, _ = load_runtime()
    state = store.read()
    target = state.active_tab
    if target is None:
        write_terminal_control("\033[?25l\033[2J\033[H")
        try:
            console.print("No active Codex session.\n\nPress Esc to close.", markup=False)
            wait_for_escape()
        finally:
            write_terminal_control("\033[?25h")
        return

    buffer = target.title
    cursor = len(buffer)
    status = ""

    def redraw() -> None:
        nonlocal status
        size = shutil.get_terminal_size((72, 10))
        lines, cursor_row, cursor_column = _rename_popup_screen(
            buffer,
            cursor,
            status,
            size.columns,
            size.lines,
        )
        console.file.write("\033[?25h\033[2J\033[H")
        for index, line in enumerate(lines, start=1):
            console.file.write(
                _render_popup_line(
                    line,
                    row=index,
                    cursor_row=cursor_row,
                    width=size.columns,
                )
            )
            if index < len(lines):
                console.file.write("\n")
        console.file.write(f"\033[{_visual_cursor_row(cursor_row)};{cursor_column}H")
        console.file.flush()
        status = ""

    try:
        redraw()
        while True:
            key = read_single_key()
            if key in {"\x1b", "\x03", ""}:
                break
            if key in {"\r", "\n"}:
                title = buffer.strip()
                if not title:
                    status = "Title cannot be empty."
                    redraw()
                    continue
                rename_active_tab(title)
                break
            buffer, cursor = _edit_rename_buffer(buffer, cursor, key)
            redraw()
    except EOFError:
        pass
    finally:
        write_terminal_control("\033[?25h")


def _rename_popup_screen(
    buffer: str,
    cursor: int,
    status: str,
    width: int,
    height: int,
) -> tuple[list[str], int, int]:
    visible_buffer, visible_cursor = _visible_rename_buffer(
        buffer,
        cursor,
        max(1, width - len(RENAME_INPUT_PREFIX) - 1),
    )
    input_line = f"{RENAME_INPUT_PREFIX}{visible_buffer}"
    lines_before_input = [
        "Rename active tab",
        "Enter a nav tab title template with live variables.",
        "",
        "Variables:",
        *[f"  {variable}  {definition}" for variable, definition in TITLE_TEMPLATE_VARIABLES],
        "",
    ]
    lines = [*lines_before_input, input_line, ""]
    footer = status or "Enter save  Esc cancel  Arrows move cursor  Ctrl-A/E ends"
    if len(lines) + 1 < height:
        lines.extend([""] * (height - len(lines) - 1))
    lines.append(footer)
    cursor_column = min(
        max(1, len(RENAME_INPUT_PREFIX) + visible_cursor + 1),
        max(1, width),
    )
    return lines, len(lines_before_input) + 1, cursor_column


def _render_popup_line(line: str, *, row: int, cursor_row: int, width: int) -> str:
    visible_line = line[: max(0, width - 1)]
    if row != cursor_row:
        return visible_line
    return f"{RENAME_INPUT_BACKGROUND}{visible_line}{CLEAR_TO_LINE_END}{RESET_TERMINAL_STYLE}"


def _visual_cursor_row(cursor_row: int) -> int:
    return max(1, cursor_row - 1)


def _visible_rename_buffer(buffer: str, cursor: int, width: int) -> tuple[str, int]:
    cursor = max(0, min(cursor, len(buffer)))
    if len(buffer) <= width:
        return buffer, cursor
    start = max(0, cursor - width)
    return buffer[start : start + width], cursor - start


def _edit_rename_buffer(buffer: str, cursor: int, key: str) -> tuple[str, int]:
    cursor = max(0, min(cursor, len(buffer)))
    if key in {"\x1b[D", "\x1bOD"}:
        return buffer, max(0, cursor - 1)
    if key in {"\x1b[C", "\x1bOC"}:
        return buffer, min(len(buffer), cursor + 1)
    if key in {"\x1b[H", "\x1bOH"}:
        return buffer, 0
    if key in {"\x1b[F", "\x1bOF"}:
        return buffer, len(buffer)
    if key in {"\x7f", "\b"}:
        if cursor == 0:
            return buffer, cursor
        return buffer[: cursor - 1] + buffer[cursor:], cursor - 1
    if key == "\x15":
        return "", 0
    if key == "\x01":
        return buffer, 0
    if key == "\x05":
        return buffer, len(buffer)
    if key.isprintable():
        return buffer[:cursor] + key + buffer[cursor:], cursor + len(key)
    return buffer, cursor


def wait_for_escape() -> None:
    try:
        while True:
            key = read_single_key()
            if key in {"\x1b", "\r", "\n", ""}:
                return
    except EOFError:
        return


def write_terminal_control(sequence: str) -> None:
    console.file.write(sequence)
    console.file.flush()


@app.command("_activate-window", hidden=True)
def activate_window_command(window_id: str) -> None:
    config, store, tmux = load_runtime()
    if not _active_tmux_window_matches(tmux, window_id):
        return

    def mutate(current: AppState) -> AppState:
        target = next((tab for tab in current.tabs if tab.tmux_window_id == window_id), None)
        if target is None:
            return current
        return replace(current, active_tab_id=target.id, focus="nav")

    state = store.update(mutate)
    try:
        if tmux.refresh_window_frame_colors(config, state, window_id) is False:
            tmux.refresh_window_frame_colors(config, state, window_id, repair_frame=True)
    except Exception:
        pass


@app.command("_focus-window", hidden=True)
@_with_runtime_lock(wait=True)
def focus_window_command(window_id: str, focus: FocusTarget) -> None:
    try:
        config, store, tmux = load_runtime()
        if not _active_tmux_window_matches(tmux, window_id):
            return

        def mutate(current: AppState) -> AppState:
            target = next((tab for tab in current.tabs if tab.tmux_window_id == window_id), None)
            active_tab_id = target.id if target is not None else current.active_tab_id
            return replace(current, active_tab_id=active_tab_id, focus=focus)

        state = store.update(mutate)
        if tmux.refresh_window_frame_colors(config, state, window_id) is False:
            tmux.refresh_window_frame_colors(config, state, window_id, repair_frame=True)
    except Exception:
        pass


def _active_tmux_window_matches(tmux: TmuxController, window_id: str) -> bool:
    active_window_id = tmux.active_window_id()
    return active_window_id is None or active_window_id == window_id


@app.command("_finish-close-window", hidden=True)
def finish_close_window_command(window_id: str) -> None:
    config, store, tmux = load_runtime()
    state = store.read()
    if not state.tabs:
        empty_window_id = prepare_empty_dashboard(config, state, tmux)
        tmux.select_window(empty_window_id)
        if state.focus == "nav":
            focus_nav_for_window(tmux, empty_window_id)
        else:
            focus_content_for_window(tmux, empty_window_id)
        if window_id != empty_window_id:
            tmux.kill_window(window_id)
        return
    tmux.kill_window(window_id)
    state = repair_and_render(config, store, tmux)
    select_active_or_empty(config, state, tmux)


@app.command("_prepare-spare-window", hidden=True)
def prepare_spare_window_command() -> None:
    config, store, tmux = load_runtime()
    state = store.read()
    tmux.ensure_spare_window(config, state)


@app.command("_loading-pane", hidden=True)
def loading_pane_command() -> None:
    frames = "|/-\\"
    index = 0
    try:
        while True:
            width, height = shutil.get_terminal_size((80, 24))
            message = f"{frames[index % len(frames)]} Starting Codex"
            index += 1
            row = max(1, height // 2)
            col = max(1, ((width - len(message)) // 2) + 1)
            console.file.write(f"\033[?25l\033[2J\033[{row};{col}H{message}")
            console.file.flush()
            time.sleep(0.08)
    except KeyboardInterrupt:
        return


@app.command("_frame-pane", hidden=True)
def frame_pane_command() -> None:
    stdin_fd = sys.stdin.fileno()
    old_termios = _disable_stdin_echo(stdin_fd) if sys.stdin.isatty() else None
    _mark_frame_pane_ready()
    try:
        for raw in sys.stdin.buffer:
            try:
                line = raw.decode("utf-8", errors="ignore").strip()
            except Exception:
                continue
            if not line.startswith("CODUX_FRAME:"):
                continue
            payload = line.removeprefix("CODUX_FRAME:")
            try:
                length_text, checksum_text, encoded_payload = payload.split(":", 2)
                expected_length = int(length_text)
                expected_checksum = int(checksum_text, 16)
                decoded = base64.b64decode(encoded_payload.encode("ascii"), validate=True)
                if len(decoded) != expected_length:
                    continue
                if (zlib.crc32(decoded) & 0xFFFFFFFF) != expected_checksum:
                    continue
                content = decoded.decode("utf-8")
            except Exception:
                continue
            console.file.write("\033[?25l\033[2J\033[H")
            console.file.write(content)
            console.file.flush()
    except KeyboardInterrupt:
        return
    finally:
        if old_termios is not None:
            termios.tcsetattr(stdin_fd, termios.TCSADRAIN, old_termios)


def _mark_frame_pane_ready() -> None:
    pane_id = os.environ.get("TMUX_PANE")
    if not pane_id:
        return
    try:
        subprocess.run(
            ["tmux", "set-option", "-p", "-t", pane_id, FRAME_HOST_OPTION, FRAME_HOST_VERSION],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
    except OSError:
        return


def _disable_stdin_echo(fd: int):
    old_attrs = termios.tcgetattr(fd)
    new_attrs = [*old_attrs]
    new_attrs[3] = new_attrs[3] & ~termios.ECHO
    termios.tcsetattr(fd, termios.TCSADRAIN, new_attrs)
    return old_attrs


def refresh_runtime_async() -> None:
    subprocess.Popen(
        codux_cli_args("_refresh"),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )


@app.command("_nav-pane", hidden=True)
def nav_pane_command() -> None:
    raise typer.Exit(run_nav_pane())


def read_single_key() -> str:
    if not sys.stdin.isatty():
        return sys.stdin.read(1)
    import termios
    import tty

    fd = sys.stdin.fileno()
    old_attrs = termios.tcgetattr(fd)
    try:
        tty.setcbreak(fd)
        return _read_key_from_tty(fd)
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old_attrs)


def _read_key_from_tty(fd: int) -> str:
    key = os.read(fd, 1)
    if key != b"\x1b":
        return key.decode(errors="ignore")
    return _read_escape_sequence(fd, key).decode(errors="ignore")


def _read_escape_sequence(fd: int, sequence: bytes) -> bytes:
    while len(sequence) < MAX_ESCAPE_SEQUENCE_BYTES:
        ready, _, _ = select.select(
            [fd],
            [],
            [],
            ESCAPE_SEQUENCE_TIMEOUT_SECONDS,
        )
        if not ready:
            break
        sequence += os.read(fd, 1)
        if _escape_sequence_complete(sequence):
            break
    return sequence


def _escape_sequence_complete(sequence: bytes) -> bool:
    if len(sequence) < 2 or not sequence.startswith(b"\x1b"):
        return True
    introducer = sequence[1:2]
    if introducer == b"[":
        return len(sequence) >= 3 and 0x40 <= sequence[-1] <= 0x7E
    if introducer == b"O":
        return len(sequence) >= 3
    return True


@app.command("_refresh", hidden=True)
@_with_runtime_lock
def refresh_command(
    nav_repaint: bool = typer.Option(
        False,
        "--nav-repaint",
        help="Ask long-running nav panes to repaint after tmux layout refresh.",
    ),
) -> None:
    config, store, tmux = load_runtime()
    repair_and_render(config, store, tmux, repaint_nav=nav_repaint)


if __name__ == "__main__":
    app()
