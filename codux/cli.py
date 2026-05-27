from __future__ import annotations

import shlex
import shutil
import subprocess
import sys
import time
import uuid
from dataclasses import replace
from pathlib import Path

import typer
from rich.console import Console
from rich.table import Table

from codux.config import ConfigError, CoduxConfig, config_path, ensure_config
from codux.navigation import select_grid_tab
from codux.nav_pane import run_nav_pane
from codux.render import render_empty_state, render_help, render_nav, write_render_files
from codux.state import (
    AppState,
    FocusTarget,
    StateError,
    StateStore,
    Tab,
    now_iso,
    prune_stale_tabs,
    state_after_closing_tab,
    state_path,
)
from codux.tmux import TmuxController


app = typer.Typer(help="Manage Codex sessions in a tmux-native tab UI.")
console = Console()
IGNORED_GENERATED_TITLES = {"", "CODEX", "NAV", "Codux Empty"}
PROJECT_ROOT = Path(__file__).resolve().parent.parent


def codux_command() -> str:
    return f"cd {shlex.quote(str(PROJECT_ROOT))} && {shlex.quote(sys.executable)} -m codux.cli"


def load_runtime() -> tuple[CoduxConfig, StateStore, TmuxController]:
    config = ensure_config()
    store = StateStore()
    store.ensure()
    return config, store, TmuxController(config.tmux_session)


def repair_and_render(
    config: CoduxConfig,
    store: StateStore,
    tmux: TmuxController,
) -> AppState:
    def mutate(state: AppState) -> AppState:
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
            generated_title = generated_tab_title(tmux, tab)
            if generated_title and generated_title != tab.title:
                changes["title"] = generated_title
            tabs.append(tab.with_updates(**changes) if changes else tab)
        tab_ids = {tab.id for tab in tabs}
        active_tab_id = repaired.active_tab_id
        if active_tab_id not in tab_ids:
            tmux_active_tab_id = tmux.active_tab_id_from_tmux()
            active_tab_id = tmux_active_tab_id if tmux_active_tab_id in tab_ids else None
        if active_tab_id is None and tabs:
            active_tab_id = tabs[0].id
        focus = "nav" if active_tab_id is None else repaired.focus
        return replace(repaired, tabs=tabs, active_tab_id=active_tab_id, focus=focus)

    state = store.update(mutate)
    render_runtime(config, state, tmux)
    return state


def render_runtime(config: CoduxConfig, state: AppState, tmux: TmuxController) -> None:
    write_render_files(config, state)
    if tmux.has_session():
        tmux.refresh_static_panes(config, state)


def generated_tab_title(tmux: TmuxController, tab: Tab) -> str | None:
    title = tmux.pane_title(tab.tmux_pane_id)
    if title is None:
        return None
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


def select_active_or_empty(config: CoduxConfig, state: AppState, tmux: TmuxController) -> None:
    if state.active_tab is not None:
        tmux.remove_empty_windows()
        tmux.select_window(state.active_tab.tmux_window_id)
        if state.focus == "nav":
            focus_nav_for_window(tmux, state.active_tab.tmux_window_id)
        else:
            tmux.select_pane(state.active_tab.tmux_pane_id)
        return
    empty_window_id = tmux.ensure_empty_window(config)
    tmux.select_window(empty_window_id)
    if state.focus == "nav":
        focus_nav_for_window(tmux, empty_window_id)
    else:
        focus_content_for_window(tmux, empty_window_id)


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


@app.command()
def start(
    attach: bool = typer.Option(
        True,
        "--attach/--no-attach",
        help="Attach to the tmux session after preparing it.",
    ),
) -> None:
    """Create or attach to the Codux tmux session."""
    config, store, tmux = load_runtime()
    tmux.ensure_session(config)
    tmux.install_look_and_keys(config, codux_command())
    state = repair_and_render(config, store, tmux)
    tmux.ensure_spare_window(config, state)
    select_active_or_empty(config, state, tmux)
    if attach:
        tmux.attach()
    else:
        console.print(f"Prepared tmux session [bold]{config.tmux_session}[/bold].")


@app.command()
def new(title: str | None = typer.Argument(None, help="Optional tab title.")) -> None:
    """Create a new Codex tab."""
    config, store, tmux = load_runtime()
    had_session = tmux.has_session()
    tmux.ensure_session(config)
    if not had_session:
        tmux.install_look_and_keys(config, codux_command())
    tab_id = uuid.uuid4().hex[:8]
    title = title or "New Codex"
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
    tmux.remove_empty_windows()
    tmux.refresh_window_frame_panes(config, state, tab.tmux_window_id)
    tmux.select_window(tab.tmux_window_id)
    tmux.select_pane(created.content_pane_id)
    tmux.prepare_spare_window_async()
    refresh_runtime_async()
    console.print(f"Created [bold]{tab.title}[/bold].")


@app.command()
def close(
    tab_id: str | None = typer.Argument(None, help="Tab id to close. Defaults to active."),
) -> None:
    """Close a Codex tab."""
    config, store, tmux = load_runtime()
    state = repair_and_render(config, store, tmux)
    target_id = tab_id or state.active_tab_id
    if target_id is None:
        console.print("No Codex sessions are open.")
        return
    target_index = next(
        (index for index, tab in enumerate(state.tabs) if tab.id == target_id), None
    )
    if target_index is None:
        raise typer.BadParameter(f"unknown tab id: {target_id}")
    target = state.tabs[target_index]
    if len(state.tabs) == 1:
        tmux.ensure_empty_window(config)

    def mutate(current: AppState) -> AppState:
        return state_after_closing_tab(current, target_id)

    state = store.update(mutate)
    render_runtime(config, state, tmux)
    select_active_or_empty(config, state, tmux)
    tmux.kill_window(target.tmux_window_id)
    console.print(f"Closed [bold]{target.title}[/bold].")


@app.command()
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
    tmux.set_pane_title(target.tmux_pane_id, title)
    write_render_files(config, state)
    refresh_runtime_async()
    return state


@app.command()
def quit() -> None:
    """Detach the dashboard and leave Codex sessions running."""
    config, _, tmux = load_runtime()
    if tmux.has_session():
        tmux.detach_clients()
    else:
        console.print(f"tmux session is not running: {config.tmux_session}")


@app.command("move-left")
def move_left() -> None:
    """Move the active tab one column left."""
    move_active_column(-1)


@app.command("move-right")
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


@app.command("next")
def next_tab() -> None:
    """Select the next tab."""
    select_relative(1)


@app.command("prev")
def prev_tab() -> None:
    """Select the previous tab."""
    select_relative(-1)


@app.command("nav-up", hidden=True)
def nav_up() -> None:
    """Select the visible tab above the active tab."""
    select_grid(delta_row=-1)


@app.command("nav-down", hidden=True)
def nav_down() -> None:
    """Select the visible tab below the active tab."""
    select_grid(delta_row=1)


@app.command("nav-left", hidden=True)
def nav_left() -> None:
    """Select the visible tab to the left of the active tab."""
    select_grid(delta_column=-1)


@app.command("nav-right", hidden=True)
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


@app.command("focus-nav")
def focus_nav() -> None:
    """Focus the nav pane."""
    set_focus("nav")


@app.command("focus-codex")
def focus_codex() -> None:
    """Focus the Codex pane."""
    set_focus("codex")


@app.command("toggle-focus")
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


@app.command()
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
        config = ensure_config()
        store = StateStore()
        state = store.ensure()
        tmux = TmuxController(config.tmux_session)
    except (ConfigError, StateError) as exc:
        console.print(f"[red]error[/red] {exc}")
        raise typer.Exit(1) from exc

    if not TmuxController.available():
        problems.append("tmux is not installed or not on PATH")
    else:
        console.print(f"[green]ok[/green] tmux: {TmuxController.version_text()}")
    codex_binary = shlex.split(config.codex_command)[0]
    if shutil.which(codex_binary) is None:
        warnings.append(f"Codex command is not on PATH: {config.codex_command}")
    else:
        console.print(f"[green]ok[/green] Codex command: {config.codex_command}")
    if TmuxController.available():
        state = repair_and_render(config, store, tmux)
    console.print(f"[green]ok[/green] config: {config_path()}")
    console.print(f"[green]ok[/green] state: {state_path()} ({len(state.tabs)} tabs)")
    console.print(
        "[blue]info[/blue] native tmux panes are used for NAV and direct Codex content; "
        "rounded frame panes provide the visible boxes."
    )
    console.print(
        "[blue]info[/blue] Codex panes run the configured command directly; Codux does "
        "not force a Codex theme or color palette."
    )
    console.print("[blue]info[/blue] Codux windows reset native tmux pane styles to default.")
    if not TmuxController.available():
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
    config = ensure_config()
    console.print(render_help(config))


@app.command("_popup-help", hidden=True)
def popup_help_command() -> None:
    config = ensure_config()
    write_terminal_control("\033[?25l\033[2J\033[H")
    try:
        console.print(render_help(config))
        console.print("\nPress Esc to close.", end="")
        while True:
            key = read_single_key()
            if key in {"\x1b", ""}:
                break
    except EOFError:
        pass
    finally:
        write_terminal_control("\033[?25h")


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
    status = ""

    def redraw() -> None:
        nonlocal status
        width = shutil.get_terminal_size((72, 10)).columns
        input_line = f"> {buffer}"
        lines = [
            "Rename active tab",
            "",
            "New title:",
            input_line,
            "",
            status or "Enter save  Esc cancel",
        ]
        console.file.write("\033[?25l\033[2J\033[H")
        for line in lines:
            console.file.write(line[: max(0, width - 1)] + "\n")
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
            if key in {"\x7f", "\b"}:
                buffer = buffer[:-1]
            elif key == "\x15":
                buffer = ""
            elif key.isprintable():
                buffer += key
            redraw()
    except EOFError:
        pass
    finally:
        write_terminal_control("\033[?25h")


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
    _, store, _ = load_runtime()

    def mutate(current: AppState) -> AppState:
        target = next((tab for tab in current.tabs if tab.tmux_window_id == window_id), None)
        if target is None:
            return current
        return replace(current, active_tab_id=target.id, focus="nav")

    store.update(mutate)


@app.command("_focus-window", hidden=True)
def focus_window_command(window_id: str, focus: FocusTarget) -> None:
    try:
        config, store, tmux = load_runtime()

        def mutate(current: AppState) -> AppState:
            target = next((tab for tab in current.tabs if tab.tmux_window_id == window_id), None)
            active_tab_id = target.id if target is not None else current.active_tab_id
            return replace(current, active_tab_id=active_tab_id, focus=focus)

        state = store.update(mutate)
        tmux.refresh_window_frame_colors(config, state, window_id)
    except Exception:
        pass


@app.command("_finish-close-window", hidden=True)
def finish_close_window_command(window_id: str) -> None:
    config, store, tmux = load_runtime()
    state = store.read()
    if not state.tabs:
        tmux.ensure_empty_window(config)
    tmux.kill_window(window_id)
    state = repair_and_render(config, store, tmux)
    select_active_or_empty(config, state, tmux)


@app.command("_prepare-spare-window", hidden=True)
def prepare_spare_window_command() -> None:
    config, store, tmux = load_runtime()
    state = repair_and_render(config, store, tmux)
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


def refresh_runtime_async() -> None:
    subprocess.Popen(
        [sys.executable, "-m", "codux.cli", "_refresh"],
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
        return sys.stdin.read(1)
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old_attrs)


@app.command("_refresh", hidden=True)
def refresh_command() -> None:
    config, store, tmux = load_runtime()
    repair_and_render(config, store, tmux)


if __name__ == "__main__":
    app()
