from __future__ import annotations

import fcntl
import shlex
from types import SimpleNamespace

import codux.nav_pane as nav_pane_module
from codux.config import CoduxConfig
from codux.nav_pane import NavPane, nav_keys
from codux.state import AppState, StateStore, Tab, now_iso
from codux.titles import CODEX_TITLE_TEMPLATE


def tab(tab_id: str, column: str = "inbox") -> Tab:
    created_at = now_iso()
    return Tab(
        id=tab_id,
        title=tab_id,
        column=column,
        tmux_session="codux",
        tmux_window_id=f"@{tab_id}",
        tmux_pane_id=f"%{tab_id}",
        created_at=created_at,
        updated_at=created_at,
    )


def test_nav_keys_use_ctrl_d_for_focus_toggle():
    assert nav_keys(b"\x04") == ["C-d"]
    assert nav_keys(b"\x01") == []


def test_nav_pane_closes_on_c_not_x():
    events: list[str] = []

    pane = NavPane.__new__(NavPane)
    pane.config = CoduxConfig()
    pane.skip_next_render = False
    pane.close_active_tab = lambda: events.append("close")
    pane.render = lambda *, force=False: events.append(f"render:{force}")

    pane.handle_input(b"x")
    pane.handle_input(b"c")

    assert events == ["render:True", "close", "render:True"]


def test_nav_pane_opens_sessions_popup_on_s():
    events: list[str] = []

    pane = NavPane.__new__(NavPane)
    pane.config = CoduxConfig()
    pane.skip_next_render = False
    pane.sessions_popup = lambda: events.append("sessions")
    pane.render = lambda *, force=False: events.append(f"render:{force}")

    pane.handle_input(b"s")

    assert events == ["sessions", "render:True"]


def test_nav_pane_cli_helpers_run_from_project_root(monkeypatch):
    calls: list[tuple[list[str], object]] = []

    def fake_run(args, **kwargs):
        calls.append((args, kwargs.get("cwd")))

    monkeypatch.setattr(nav_pane_module.subprocess, "run", fake_run)

    pane = NavPane.__new__(NavPane)
    pane.run_cli("_finish-close-window", "@1")

    assert calls == [
        (
            [
                "uv",
                "--quiet",
                "--no-progress",
                "--directory",
                str(nav_pane_module.PROJECT_ROOT),
                "--project",
                str(nav_pane_module.PROJECT_ROOT),
                "run",
                "codux",
                "_finish-close-window",
                "@1",
            ],
            None,
        )
    ]


def test_rename_popup_runs_from_project_root(monkeypatch):
    calls: list[list[str]] = []

    def fake_run(args, **kwargs):
        calls.append(args)

    monkeypatch.setattr(nav_pane_module.subprocess, "run", fake_run)

    pane = NavPane.__new__(NavPane)
    pane.rename_prompt()

    command = calls[0][-1]
    assert calls[0] == [
        "tmux",
        "display-popup",
        "-E",
        "-d",
        str(nav_pane_module.PROJECT_ROOT),
        "-w",
        "72",
        "-h",
        "10",
        "-s",
        "fg=default,bg=default",
        "-S",
        "fg=default,bg=default",
        "-T",
        "Rename",
        command,
    ]
    root = shlex.quote(str(nav_pane_module.PROJECT_ROOT))
    assert command == (
        f"uv --quiet --no-progress --directory {root} --project {root} run codux _popup-rename"
    )
    assert "cd " not in command


def test_help_popup_sizes_to_rendered_help(monkeypatch):
    calls: list[list[str]] = []

    def fake_run(args, **kwargs):
        calls.append(args)

    monkeypatch.setattr(nav_pane_module.subprocess, "run", fake_run)

    pane = NavPane.__new__(NavPane)
    pane.config = CoduxConfig()
    pane.help_popup()

    command = calls[0][-1]
    assert calls[0] == [
        "tmux",
        "display-popup",
        "-E",
        "-d",
        str(nav_pane_module.PROJECT_ROOT),
        "-w",
        str(nav_pane_module.HELP_POPUP_WIDTH),
        "-h",
        str(nav_pane_module.help_popup_height(pane.config)),
        "-s",
        "fg=default,bg=default",
        "-S",
        "fg=default,bg=default",
        "-T",
        "Codux",
        command,
    ]
    root = shlex.quote(str(nav_pane_module.PROJECT_ROOT))
    assert command == (
        f"uv --quiet --no-progress --directory {root} --project {root} run codux _popup-help"
    )


def test_sessions_popup_runs_from_project_root(monkeypatch):
    calls: list[list[str]] = []

    def fake_run(args, **kwargs):
        calls.append(args)

    monkeypatch.setattr(nav_pane_module.subprocess, "run", fake_run)

    pane = NavPane.__new__(NavPane)
    pane.sessions_popup()

    command = calls[0][-1]
    assert calls[0] == [
        "tmux",
        "display-popup",
        "-E",
        "-d",
        str(nav_pane_module.PROJECT_ROOT),
        "-w",
        str(nav_pane_module.SESSIONS_POPUP_WIDTH),
        "-h",
        str(nav_pane_module.SESSIONS_POPUP_HEIGHT),
        "-s",
        "fg=default,bg=default",
        "-S",
        "fg=default,bg=default",
        "-T",
        "Codux Sessions",
        command,
    ]
    root = shlex.quote(str(nav_pane_module.PROJECT_ROOT))
    assert command == (
        f"uv --quiet --no-progress --directory {root} --project {root} run codux _popup-sessions"
    )


def test_move_column_skips_frame_refresh_when_height_is_stable(tmp_path):
    config = CoduxConfig()
    active = tab("active")
    state = AppState(tabs=[active], active_tab_id=active.id, focus="nav")
    store = StateStore(tmp_path / "state.json")
    store.write(state)
    events: list[tuple[str, object]] = []

    class FakeTmux:
        def resize_nav_frame_for_window(self, config_arg, state_arg, window_id):
            raise AssertionError("stable-height moves should only repaint the nav pane")

        def refresh_window_frame_panes(
            self, config_arg, state_arg, window_id, *, min_nav_content_height=None
        ):
            raise AssertionError("stable-height moves should only repaint the nav pane")

        def refresh_window_frame_colors(self, config_arg, state_arg, window_id):
            raise AssertionError("move_column should not repaint borders twice")

    pane = NavPane.__new__(NavPane)
    pane.config = config
    pane.store = store
    pane.state = state
    pane.tmux = FakeTmux()
    pane.skip_next_render = False
    pane.render_snapshot = lambda state_arg: events.append(("render", state_arg.active_tab.column))
    pane.select_nav_for_window = lambda window_id: events.append(("select", window_id))

    pane.move_column(1)

    assert store.read().active_tab.column == "implement"
    assert events == [
        ("render", "implement"),
        ("select", active.tmux_window_id),
    ]
    assert pane.skip_next_render


def test_move_column_resizes_before_rendering_new_row(tmp_path):
    config = CoduxConfig()
    left = tab("left", "inbox")
    active = tab("active", "implement")
    state = AppState(tabs=[left, active], active_tab_id=active.id, focus="nav")
    store = StateStore(tmp_path / "state.json")
    store.write(state)
    events: list[tuple[str, object]] = []

    class FakeTmux:
        def resize_nav_frame_for_window(self, config_arg, state_arg, window_id):
            events.append(("resize", state_arg.active_tab.column, window_id))

        def refresh_window_frame_panes(
            self, config_arg, state_arg, window_id, *, min_nav_content_height=None
        ):
            events.append(("frame", state_arg.active_tab.column, min_nav_content_height))

        def refresh_window_frame_colors(self, config_arg, state_arg, window_id):
            raise AssertionError("move_column should not repaint borders twice")

    pane = NavPane.__new__(NavPane)
    pane.config = config
    pane.store = store
    pane.state = state
    pane.tmux = FakeTmux()
    pane.skip_next_render = False
    pane.render_snapshot = lambda state_arg: events.append(("render", state_arg.active_tab.column))
    pane.select_nav_for_window = lambda window_id: events.append(("select", window_id))

    pane.move_column(-1)

    assert store.read().active_tab.column == "inbox"
    assert events == [
        ("resize", "inbox", active.tmux_window_id),
        ("render", "inbox"),
        ("frame", "inbox", 3),
        ("render", "inbox"),
        ("select", active.tmux_window_id),
    ]


def test_move_column_avoids_shrinking_nav_during_move(tmp_path):
    config = CoduxConfig()
    top = tab("top", "inbox")
    active = tab("active", "inbox")
    state = AppState(tabs=[top, active], active_tab_id=active.id, focus="nav")
    store = StateStore(tmp_path / "state.json")
    store.write(state)
    events: list[tuple[str, object]] = []

    class FakeTmux:
        def resize_nav_frame_for_window(self, config_arg, state_arg, window_id):
            raise AssertionError("shrinking moves should render before resizing")

        def refresh_window_frame_panes(
            self, config_arg, state_arg, window_id, *, min_nav_content_height=None
        ):
            events.append(("frame", state_arg.active_tab.column, min_nav_content_height))

        def refresh_window_frame_colors(self, config_arg, state_arg, window_id):
            raise AssertionError("move_column should not repaint borders twice")

    pane = NavPane.__new__(NavPane)
    pane.config = config
    pane.store = store
    pane.state = state
    pane.tmux = FakeTmux()
    pane.skip_next_render = False
    pane.render_snapshot = lambda state_arg: events.append(("render", state_arg.active_tab.column))
    pane.select_nav_for_window = lambda window_id: events.append(("select", window_id))

    pane.move_column(1)

    assert store.read().active_tab.column == "implement"
    assert events == [
        ("render", "implement"),
        ("frame", "implement", 3),
        ("render", "implement"),
        ("select", active.tmux_window_id),
    ]


def test_move_column_uses_frame_refresh_as_nav_focus_repaint(tmp_path):
    config = CoduxConfig()
    first = tab("first", "implement")
    second = tab("second", "implement")
    active = tab("active", "inbox")
    state = AppState(tabs=[first, second, active], active_tab_id=active.id, focus="nav")
    store = StateStore(tmp_path / "state.json")
    store.write(state)
    events: list[tuple[str, object]] = []

    class FakeTmux:
        def resize_nav_frame_for_window(self, config_arg, state_arg, window_id):
            events.append(("resize", state_arg.active_tab.column, window_id))

        def refresh_window_frame_panes(
            self, config_arg, state_arg, window_id, *, min_nav_content_height=None
        ):
            events.append(("frame", state_arg.active_tab.column, min_nav_content_height, window_id))

        def refresh_window_frame_colors(self, config_arg, state_arg, window_id):
            raise AssertionError("move_column should not repaint borders twice")

    pane = NavPane.__new__(NavPane)
    pane.config = config
    pane.store = store
    pane.state = state
    pane.tmux = FakeTmux()
    pane.skip_next_render = False
    pane.render_snapshot = lambda state_arg: events.append(("render", state_arg.active_tab.column))
    pane.select_nav_for_window = lambda window_id: events.append(("select", window_id))

    pane.move_column(1)
    pane.move_column(1)

    assert store.read().active_tab.column == "ship"
    assert events[-3:] == [
        ("frame", "ship", 4, active.tmux_window_id),
        ("render", "ship"),
        ("select", active.tmux_window_id),
    ]


def test_nav_render_snapshot_clears_before_repainting(monkeypatch):
    active = tab("active")
    state = AppState(tabs=[active], active_tab_id=active.id, focus="nav")
    writes: list[str] = []

    pane = NavPane.__new__(NavPane)
    pane.config = CoduxConfig()
    pane.last_payload = ""

    monkeypatch.setattr(nav_pane_module, "terminal_size", lambda: (60, 3))
    monkeypatch.setattr(
        nav_pane_module.os,
        "write",
        lambda _fd, payload: writes.append(payload.decode("utf-8")) or len(payload),
    )

    pane.render_snapshot(state)

    assert writes
    assert writes[0].startswith(nav_pane_module.HIDE_CURSOR + "\033[2J\033[H")
    assert writes[0].count("INBOX") == 1


def test_close_last_tab_skips_redrawing_closing_nav_pane(tmp_path):
    config = CoduxConfig()
    active = tab("active")
    state = AppState(tabs=[active], active_tab_id=active.id, focus="nav")
    store = StateStore(tmp_path / "state.json")
    store.write(state)
    events: list[tuple[str, str]] = []

    pane = NavPane.__new__(NavPane)
    pane.config = config
    pane.store = store
    pane.state = state
    pane.tmux = object()
    pane.skip_next_render = False
    pane.current_state_for_input = lambda: store.read()
    pane.select_nav_for_window = lambda window_id: events.append(("select", window_id))
    pane.run_cli_async = lambda *args: events.append(("run", " ".join(args)))

    pane.close_active_tab()

    assert store.read() == AppState(tabs=[], active_tab_id=None, focus="nav")
    assert pane.skip_next_render
    assert events == [("run", f"_finish-close-window {active.tmux_window_id}")]


def test_nav_render_skips_busy_state_lock(tmp_path):
    store = StateStore(tmp_path / "state.json", lock_timeout=0.01, lock_poll_interval=0.001)
    store.write(AppState(focus="nav"))
    events: list[str] = []
    lock_file = store.lock_path.open("a+", encoding="utf-8")

    pane = NavPane.__new__(NavPane)
    pane.store = store
    pane.last_render = 0.0
    pane.state = AppState()
    pane.render_snapshot = lambda state: events.append(state.focus)

    try:
        fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
        pane.render(force=True)
    finally:
        fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)
        lock_file.close()

    assert events == []


def test_nav_resize_waits_for_settle_period():
    pane = NavPane.__new__(NavPane)
    pane.resize_pending_at = 10.0

    assert not pane.resize_ready(10.0 + nav_pane_module.RESIZE_RENDER_DELAY - 0.001)
    assert pane.resize_ready(10.0 + nav_pane_module.RESIZE_RENDER_DELAY + 0.001)


def test_new_tab_refreshes_codex_frame_colors_after_selecting_codex(tmp_path):
    config = CoduxConfig()
    store = StateStore(tmp_path / "state.json")
    store.write(AppState(focus="nav"))
    events: list[tuple[str, object]] = []
    frame_states: list[AppState] = []
    color_states: list[AppState] = []
    claimed_titles: list[str] = []

    class FakeTmux:
        def claim_spare_tab_window(self, config_arg, state_arg, title, tab_id):
            claimed_titles.append(title)
            return SimpleNamespace(
                window_id="@new",
                content_pane_id="%content",
                nav_pane_id="%nav",
            )

        def remove_empty_windows(self):
            events.append(("remove-empty", True))

        def refresh_window_frame_panes(self, config_arg, state_arg, window_id):
            events.append(("frame", window_id))
            frame_states.append(state_arg)

        def select_window(self, window_id):
            events.append(("window", window_id))

        def select_pane(self, pane_id):
            events.append(("pane", pane_id))

        def refresh_window_frame_colors(self, config_arg, state_arg, window_id):
            events.append(("colors", window_id))
            color_states.append(state_arg)

        def prepare_spare_window_async(self):
            events.append(("prepare-spare", True))

    pane = NavPane.__new__(NavPane)
    pane.config = config
    pane.store = store
    pane.state = store.read()
    pane.tmux = FakeTmux()
    pane.pane_id = "%nav"
    pane.window_id = "@old"
    pane.refresh_nav_pane_cache = lambda: events.append(("refresh-cache", True))
    pane.refresh_static_panes_async = lambda: (_ for _ in ()).throw(
        AssertionError("new tab should not run a broad async refresh")
    )
    pane.render_snapshot = lambda state: events.append(("snapshot-focus", state.focus))

    pane.new_tab()

    state = store.read()
    assert state.active_tab is not None
    assert state.active_tab.title == CODEX_TITLE_TEMPLATE
    assert state.focus == "codex"
    assert claimed_titles == [CODEX_TITLE_TEMPLATE]
    assert frame_states[-1].focus == "codex"
    assert color_states[-1].focus == "codex"
    assert events == [
        ("snapshot-focus", "codex"),
        ("frame", "@new"),
        ("refresh-cache", True),
        ("window", "@new"),
        ("pane", "%content"),
        ("colors", "@new"),
        ("prepare-spare", True),
        ("remove-empty", True),
    ]


def test_focus_codex_repaints_active_window_without_broad_refresh(tmp_path):
    config = CoduxConfig()
    active = tab("active")
    state = AppState(tabs=[active], active_tab_id=active.id, focus="nav")
    store = StateStore(tmp_path / "state.json")
    store.write(state)
    events: list[tuple[str, object]] = []

    class FakeTmux:
        def empty_window_id(self):
            raise AssertionError("active tab should provide the focus target")

        def content_pane_for_window(self, window_id):
            events.append(("content", window_id))
            return "%content"

        def select_pane(self, pane_id):
            events.append(("pane", pane_id))

        def refresh_window_frame_colors(self, config_arg, state_arg, window_id):
            events.append(("colors", state_arg.focus, window_id))
            return True

    pane = NavPane.__new__(NavPane)
    pane.config = config
    pane.store = store
    pane.tmux = FakeTmux()
    pane.refresh_static_panes_async = lambda: (_ for _ in ()).throw(
        AssertionError("focus should not run a broad async refresh when the frame is ready")
    )

    pane.focus_codex()

    assert store.read().focus == "codex"
    assert events == [
        ("content", active.tmux_window_id),
        ("pane", "%content"),
        ("colors", "codex", active.tmux_window_id),
    ]


def test_nav_pane_polls_live_codex_titles(tmp_path):
    active = tab("active").with_updates(title=CODEX_TITLE_TEMPLATE, codex_title="Working | 019e")
    state = AppState(tabs=[active], active_tab_id=active.id, focus="nav")
    store = StateStore(tmp_path / "state.json")
    store.write(state)
    events: list[tuple[str, str | None]] = []

    class FakeTmux:
        def pane_title(self, pane_id):
            return "Ready | 019e"

        def refresh_window_title_frame(self, config, state_arg, window_id):
            events.append(
                (window_id, state_arg.active_tab.codex_title if state_arg.active_tab else None)
            )
            return True

    pane = NavPane.__new__(NavPane)
    pane.config = CoduxConfig()
    pane.store = store
    pane.tmux = FakeTmux()
    pane.state = state
    pane.last_title_poll = 0.0

    assert pane.poll_live_titles(now=1.0)
    assert store.read().active_tab.codex_title == "Ready | 019e"
    assert events == [(active.tmux_window_id, "Ready | 019e")]


def test_title_frame_refresh_uses_latest_focus_state(tmp_path):
    previous_tab = tab("active").with_updates(
        title=CODEX_TITLE_TEMPLATE,
        codex_title="Working | 019e",
    )
    current_tab = previous_tab.with_updates(codex_title="Ready | 019e")
    previous = AppState(tabs=[previous_tab], active_tab_id=previous_tab.id, focus="codex")
    current = AppState(tabs=[current_tab], active_tab_id=current_tab.id, focus="codex")
    latest = AppState(tabs=[current_tab], active_tab_id=current_tab.id, focus="nav")
    store = StateStore(tmp_path / "state.json")
    store.write(latest)
    events: list[tuple[str, str, str | None]] = []

    class FakeTmux:
        def refresh_window_title_frame(self, config, state_arg, window_id):
            active_tab = state_arg.active_tab
            events.append(
                (
                    window_id,
                    state_arg.focus,
                    active_tab.codex_title if active_tab else None,
                )
            )
            return True

    pane = NavPane.__new__(NavPane)
    pane.config = CoduxConfig()
    pane.store = store
    pane.tmux = FakeTmux()
    pane.state = current

    pane.refresh_title_frames(previous, current)

    assert events == [(current_tab.tmux_window_id, "nav", "Ready | 019e")]
