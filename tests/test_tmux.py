from __future__ import annotations

import base64
import shlex
import zlib
from types import SimpleNamespace

import codux.sessions as sessions_module
import codux.tmux as tmux_module
from codux.config import CoduxConfig
from codux.state import AppState, Tab, now_iso
from codux.tmux import TmuxController
from codux.tmux_snapshot import PaneSnapshot, TmuxSnapshot, WindowSnapshot


def tab(tab_id: str, window_id: str, column: str = "inbox") -> Tab:
    created_at = now_iso()
    return Tab(
        id=tab_id,
        title=tab_id,
        column=column,
        tmux_session="codux",
        tmux_window_id=window_id,
        tmux_pane_id=f"%{tab_id}",
        created_at=created_at,
        updated_at=created_at,
    )


def test_codex_shell_command_launches_direct_codex_without_proxy():
    controller = TmuxController("codux")
    command = controller._codex_shell_command(CoduxConfig(codex_command="codex --foo"))

    assert command == "unset NO_COLOR CODUX_HOME CODUX_WORKDIR; exec codex --foo"
    assert "_codex-proxy" not in command
    assert "tui.theme" not in command


def test_codex_shell_command_preserves_user_codex_command():
    controller = TmuxController("codux")
    command = controller._codex_shell_command(
        CoduxConfig(codex_command='codex -c tui.theme="dark" --foo')
    )

    assert command == (
        'unset NO_COLOR CODUX_HOME CODUX_WORKDIR; exec codex -c tui.theme="dark" --foo'
    )


def test_codex_shell_command_does_not_rewrite_custom_launcher():
    controller = TmuxController("codux")
    command = controller._codex_shell_command(CoduxConfig(codex_command="my-codex --foo"))

    assert command == "unset NO_COLOR CODUX_HOME CODUX_WORKDIR; exec my-codex --foo"


def test_respawn_codex_pane_preserves_terminal_title(monkeypatch):
    controller = TmuxController("codux")
    events: list[tuple[str, str] | tuple[str, tuple[str, ...]]] = []

    monkeypatch.setattr(
        controller,
        "_tmux",
        lambda args, check=True: events.append(("tmux", tuple(args))) or "",
    )
    monkeypatch.setattr(
        controller,
        "_set_pane_role",
        lambda pane_id, role: events.append(("role", pane_id)),
    )
    monkeypatch.setattr(
        controller,
        "_set_pane_title",
        lambda pane_id, title: events.append(("title", title)),
    )

    controller._respawn_codex_pane(CoduxConfig(), "%codex")

    assert ("role", "%codex") in events
    assert not any(event[0] == "title" for event in events)


def test_kill_session_targets_codux_session(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    monkeypatch.setattr(
        controller,
        "_tmux",
        lambda args, check=True: commands.append(tuple(args)) or "",
    )

    controller.kill_session()

    assert commands == [("kill-session", "-t", "codux")]


def test_tmux_internal_shell_commands_use_uv_project_root_without_cd():
    controller = TmuxController("codux")
    root = shlex.quote(str(tmux_module.PROJECT_ROOT))
    codux_command = f"uv --quiet --no-progress --directory {root} --project {root} run codux"

    commands = [
        controller._nav_shell_command("%1"),
        controller._loading_shell_command(),
        controller._frame_pane_shell_command(),
        controller._codux_cli_command(),
    ]

    assert commands == [
        f"env TMUX_PANE=%1 {codux_command} _nav-pane",
        f"{codux_command} _loading-pane",
        f"stty -echo 2>/dev/null || true; exec {codux_command} _frame-pane",
        codux_command,
    ]
    assert all("cd " not in command for command in commands)


def test_project_root_falls_back_to_legacy_refresh_hook(monkeypatch):
    controller = TmuxController("codux")
    old_root = "/Users/me/codux/.worktrees/old"
    hook = (
        'client-attached[0] run-shell -b "uv --directory '
        f'{old_root} --project {old_root} run codux _refresh >/dev/null 2>&1"'
    )

    def fake_tmux(args, check=True):
        if args[:2] == ["show-option", "-qv"]:
            return ""
        if args[:2] == ["show-hooks", "-t"]:
            return hook
        raise AssertionError(args)

    monkeypatch.setattr(controller, "has_session", lambda: True)
    monkeypatch.setattr(controller, "_tmux", fake_tmux)

    assert controller.project_root() == old_root


def test_resize_hooks_debounce_and_repaint_nav(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    controller._install_hooks("uv run codux")

    hooks = {
        command[3]: command[4] for command in commands if command[:3] == ("set-hook", "-t", "codux")
    }
    assert "_refresh --nav-repaint" in hooks["client-attached"]
    assert "list-panes -a -t codux" in hooks["client-resized"]
    assert "##{pane_id}" in hooks["client-resized"]
    assert "##{@codux-role}" in hooks["client-resized"]
    assert 'resize-pane -t "$pane_id" -x 3' in hooks["client-resized"]
    assert 'resize-pane -t "$pane_id" -y 1' in hooks["client-resized"]
    assert f"sleep {tmux_module.RESIZE_REFRESH_DELAY_SECONDS}" in hooks["client-resized"]
    assert "_refresh --nav-repaint" in hooks["client-resized"]
    assert "list-panes -a -t codux" in hooks["window-resized"]
    assert 'resize-pane -t "$pane_id" -x 3' in hooks["window-resized"]
    assert 'resize-pane -t "$pane_id" -y 1' in hooks["window-resized"]
    assert f"sleep {tmux_module.RESIZE_REFRESH_DELAY_SECONDS}" in hooks["window-resized"]
    assert "_refresh --nav-repaint" in hooks["window-resized"]


def test_install_records_current_project_root(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    def fake_tmux(args, check=True):
        commands.append(tuple(args))
        if args and args[0] == "show-options":
            return ""
        return ""

    monkeypatch.setattr(controller, "_tmux", fake_tmux)
    monkeypatch.setattr(controller, "_snapshot", lambda: SimpleNamespace())
    monkeypatch.setattr(controller, "_codux_windows", lambda snapshot: [])
    monkeypatch.setattr(controller, "repair_window_sizes", lambda: None)
    monkeypatch.setattr(tmux_module, "current_workdir", lambda: tmux_module.PROJECT_ROOT)
    monkeypatch.setattr(tmux_module, "app_dir", lambda: tmux_module.PROJECT_ROOT / ".codux-test")

    controller.install_look_and_keys(CoduxConfig(), "uv run codux")

    assert (
        "set-option",
        "-t",
        "codux",
        tmux_module.PROJECT_ROOT_OPTION,
        str(tmux_module.PROJECT_ROOT),
    ) in commands
    assert (
        "set-option",
        "-t",
        "codux",
        tmux_module.WORKDIR_OPTION,
        str(tmux_module.PROJECT_ROOT),
    ) in commands
    assert (
        "set-option",
        "-t",
        "codux",
        tmux_module.RUNTIME_DIR_OPTION,
        str(tmux_module.PROJECT_ROOT / ".codux-test"),
    ) in commands


def test_direct_nav_arrow_activates_state_after_selecting_window(monkeypatch):
    controller = TmuxController("codux")
    commands: list[list[str]] = []

    monkeypatch.setattr(
        controller,
        "_tmux",
        lambda args, check=True: commands.append(args) or "",
    )

    controller._bind_direct_nav_arrow("Left", "left", "#{nav}", "uv run codux")

    bound_command = commands[0][-2]
    assert "_activate-window" in bound_command
    assert bound_command.index("select-pane") < bound_command.index("_activate-window")
    assert "run-shell -b 'uv run codux _activate-window" not in bound_command


def test_nav_border_stays_active_across_tab_windows_when_focus_is_nav():
    controller = TmuxController("codux")
    first = tab("one", "@1")
    second = tab("two", "@2")
    state = AppState(tabs=[first, second], active_tab_id=first.id, focus="nav")

    assert controller._border_is_active(second.tmux_window_id, "NAV_TOP", state)
    assert not controller._border_is_active(second.tmux_window_id, "CODEX_TOP", state)


def test_codex_border_stays_active_across_tab_windows_when_focus_is_codex():
    controller = TmuxController("codux")
    first = tab("one", "@1")
    second = tab("two", "@2")
    state = AppState(tabs=[first, second], active_tab_id=first.id, focus="codex")

    assert controller._border_is_active(second.tmux_window_id, "CODEX_TOP", state)
    assert not controller._border_is_active(second.tmux_window_id, "NAV_TOP", state)


def test_nav_top_border_shows_session_workdir(monkeypatch, tmp_path):
    controller = TmuxController("codux")
    home = tmp_path / "home"
    workdir = home / "code" / "configs"
    workdir.mkdir(parents=True)
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.setattr(
        controller,
        "_session_option",
        lambda option: str(workdir) if option == tmux_module.WORKDIR_OPTION else "",
    )

    content = controller._border_content(
        CoduxConfig(),
        "@1",
        "NAV_TOP",
        AppState(focus="nav"),
        width=60,
        height=1,
    )

    assert "NAV " in content
    assert " ~/code/configs" in content
    assert " ~/code/configs " not in content


def test_inactive_nav_bottom_border_shows_focus_hint_on_left(monkeypatch):
    controller = TmuxController("codux")
    config = CoduxConfig()
    state = AppState(focus="codex")
    monkeypatch.setattr(sessions_module, "other_codux_session_count", lambda current: 2)

    content = controller._border_content(config, "@1", "NAV_BOTTOM", state, width=180, height=1)

    assert content.startswith("\033[38;5;244mC-d focus ")
    assert "C-q quit" not in content
    assert "? help" not in content
    assert "new tab" not in content
    assert "sessions (2)" not in content
    assert "focus codex pane" not in content
    assert "\033[38;5;244m" in content


def test_bottom_borders_show_focus_marker(monkeypatch):
    controller = TmuxController("codux")
    config = CoduxConfig()
    first = tab("one", "@1")
    second = tab("two", "@2")
    state = AppState(tabs=[first, second], active_tab_id=second.id, focus="codex")
    monkeypatch.setattr(sessions_module, "other_codux_session_count", lambda current: 0)

    nav_content = controller._border_content(config, "@1", "NAV_BOTTOM", state, width=120, height=1)
    codex_content = controller._border_content(
        config,
        "@1",
        "CODEX_BOTTOM",
        state,
        width=80,
        height=1,
    )

    assert nav_content.startswith("\033[38;5;244mC-d focus ")
    assert codex_content.startswith("\033[38;5;117mC-q quit ")
    assert codex_content.endswith(" ●\033[0m")
    assert "C-d focus" not in codex_content
    assert "focus nav pane" not in codex_content


def test_border_roles_for_focus_paints_complete_groups():
    controller = TmuxController("codux")

    assert controller._border_roles_for_focus("nav") == [
        "CODEX_TOP",
        "CODEX_BOTTOM",
        "CODEX_LEFT",
        "CODEX_RIGHT",
        "NAV_TOP",
        "NAV_LEFT",
        "NAV_RIGHT",
        "NAV_BOTTOM",
    ]
    assert controller._border_roles_for_focus("codex") == [
        "NAV_TOP",
        "NAV_BOTTOM",
        "NAV_LEFT",
        "NAV_RIGHT",
        "CODEX_TOP",
        "CODEX_LEFT",
        "CODEX_RIGHT",
        "CODEX_BOTTOM",
    ]


def test_refresh_window_frame_colors_repairs_then_repaints_complete_group(monkeypatch):
    controller = TmuxController("codux")
    events: list[tuple[str, object]] = []

    def pane(pane_id: str, role: str) -> PaneSnapshot:
        return PaneSnapshot(
            pane_id=pane_id,
            window_id="@1",
            role=role,
            title=role,
            top=0,
            left=0,
            width=40,
            height=1,
            current_command="python",
            start_command="_frame-pane",
            nav_host_version="",
            frame_host_version=tmux_module.FRAME_HOST_VERSION,
        )

    complete_panes = {
        f"%{role.lower()}": pane(f"%{role.lower()}", role)
        for role in sorted(tmux_module.BORDER_ROLES)
    }
    complete_snapshot = TmuxSnapshot(
        windows={},
        panes=complete_panes,
        panes_by_window={"@1": list(complete_panes.values())},
    )
    empty_snapshot = TmuxSnapshot(windows={}, panes={}, panes_by_window={"@1": []})
    snapshots = iter([empty_snapshot, empty_snapshot, empty_snapshot, complete_snapshot])

    monkeypatch.setattr(controller, "has_session", lambda: True)
    monkeypatch.setattr(controller, "window_exists", lambda window_id: True)
    monkeypatch.setattr(controller, "_snapshot", lambda: next(snapshots))
    monkeypatch.setattr(
        controller,
        "_ensure_native_window",
        lambda window_id, snapshot: events.append(("native", window_id)) or ("%nav", "%codex"),
    )
    monkeypatch.setattr(
        controller,
        "_ensure_window_frame",
        lambda window_id, snapshot: events.append(("frame", window_id)) or True,
    )
    monkeypatch.setattr(
        controller,
        "_resize_nav_frame",
        lambda window_id, pane_id, height, snapshot: events.append(("resize-nav", pane_id, height)),
    )
    monkeypatch.setattr(
        controller,
        "_border_content",
        lambda config, window_id, role, state, width, height: role,
    )
    monkeypatch.setattr(
        controller,
        "_render_frame_pane",
        lambda pane_id, content, snapshot: events.append(("render", content)),
    )

    controller.refresh_window_frame_colors(
        CoduxConfig(), AppState(focus="nav"), "@1", repair_frame=True
    )

    assert events == [
        ("native", "@1"),
        ("frame", "@1"),
        ("resize-nav", "%nav", 2),
        ("render", "CODEX_TOP"),
        ("render", "CODEX_BOTTOM"),
        ("render", "CODEX_LEFT"),
        ("render", "CODEX_RIGHT"),
        ("render", "NAV_TOP"),
        ("render", "NAV_LEFT"),
        ("render", "NAV_RIGHT"),
        ("render", "NAV_BOTTOM"),
    ]


def test_refresh_window_frame_colors_does_not_repair_layout_by_default(monkeypatch):
    controller = TmuxController("codux")
    events: list[tuple[str, object]] = []
    snapshot = TmuxSnapshot(windows={}, panes={}, panes_by_window={"@1": []})

    monkeypatch.setattr(controller, "has_session", lambda: True)
    monkeypatch.setattr(controller, "window_exists", lambda window_id: True)
    monkeypatch.setattr(controller, "_snapshot", lambda: snapshot)
    monkeypatch.setattr(
        controller,
        "_ensure_native_window",
        lambda window_id, snapshot: events.append(("native", window_id)) or ("%nav", "%codex"),
    )
    monkeypatch.setattr(
        controller,
        "_ensure_window_frame",
        lambda window_id, snapshot: events.append(("frame", window_id)) or True,
    )
    monkeypatch.setattr(
        controller,
        "_render_frame_pane",
        lambda pane_id, content, snapshot: events.append(("render", pane_id)),
    )

    controller.refresh_window_frame_colors(CoduxConfig(), AppState(focus="nav"), "@1")

    assert events == []


def test_refresh_window_title_frame_repaints_only_codex_top(monkeypatch):
    controller = TmuxController("codux")
    events: list[tuple[str, object]] = []
    codex_top = PaneSnapshot(
        pane_id="%codex-top",
        window_id="@1",
        role="CODEX_TOP",
        title="CODEX_TOP",
        top=0,
        left=0,
        width=80,
        height=1,
        current_command="python",
        start_command="_frame-pane",
        nav_host_version="",
        frame_host_version=tmux_module.FRAME_HOST_VERSION,
    )
    nav_bottom = PaneSnapshot(
        pane_id="%nav-bottom",
        window_id="@1",
        role="NAV_BOTTOM",
        title="NAV_BOTTOM",
        top=0,
        left=0,
        width=80,
        height=1,
        current_command="python",
        start_command="_frame-pane",
        nav_host_version="",
        frame_host_version=tmux_module.FRAME_HOST_VERSION,
    )
    snapshot = TmuxSnapshot(
        windows={},
        panes={pane.pane_id: pane for pane in (codex_top, nav_bottom)},
        panes_by_window={"@1": [codex_top, nav_bottom]},
    )

    monkeypatch.setattr(controller, "has_session", lambda: True)
    monkeypatch.setattr(controller, "window_exists", lambda window_id: True)
    monkeypatch.setattr(controller, "_snapshot", lambda: snapshot)
    monkeypatch.setattr(
        controller,
        "_border_content",
        lambda config, window_id, role, state, width, height: f"{role}:{width}",
    )
    monkeypatch.setattr(
        controller,
        "_render_frame_pane",
        lambda pane_id, content, snapshot: events.append((pane_id, content)),
    )

    refreshed = controller.refresh_window_title_frame(CoduxConfig(), AppState(focus="nav"), "@1")

    assert refreshed is True
    assert events == [("%codex-top", "CODEX_TOP:80")]


def test_codex_top_border_shows_live_codex_title():
    controller = TmuxController("codux")
    active = tab("one", "@1").with_updates(codex_title="Implement auth flow")
    state = AppState(tabs=[active], active_tab_id=active.id, focus="codex")

    content = controller._border_content(
        CoduxConfig(),
        "@1",
        "CODEX_TOP",
        state,
        width=80,
        height=1,
    )

    assert "CODEX " in content
    assert " Implement auth flow" in content


def test_codex_top_border_uses_pending_title_until_live_title_arrives():
    controller = TmuxController("codux")
    active = tab("one", "@1")
    state = AppState(tabs=[active], active_tab_id=active.id, focus="codex")

    content = controller._border_content(
        CoduxConfig(),
        "@1",
        "CODEX_TOP",
        state,
        width=80,
        height=1,
    )

    assert " ..." in content


def test_create_tab_window_uses_native_nav_and_codex_panes(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []
    selected_windows: list[str] = []
    workdir = tmux_module.PROJECT_ROOT / "example-workdir"

    def fake_tmux(args, check=True):
        commands.append(tuple(args))
        if args and args[0] == "new-window":
            return "@9\t%9"
        return ""

    monkeypatch.setattr(controller, "ensure_session", lambda config: None)
    monkeypatch.setattr(controller, "_tmux", fake_tmux)
    monkeypatch.setattr(controller, "_split_nav_pane", lambda pane_id, **kwargs: "%10")
    monkeypatch.setattr(controller, "select_window", selected_windows.append)
    monkeypatch.setattr(tmux_module, "current_workdir", lambda: workdir)

    created = controller.create_tab_window(CoduxConfig(), "New Codex", "tab123")

    assert created.window_id == "@9"
    assert created.content_pane_id == "%9"
    assert created.nav_pane_id == "%10"
    assert selected_windows == []
    assert ("set-option", "-w", "-t", "@9", "@codux-nav-pane", "%10") in commands
    assert ("set-option", "-w", "-t", "@9", "@codux-codex-pane", "%9") in commands
    assert ("set-option", "-p", "-t", "%10", "@codux-role", "NAV") in commands
    assert ("set-option", "-p", "-t", "%9", "@codux-role", "CODEX") in commands
    new_window_command = next(command for command in commands if command[0] == "new-window")
    assert "-d" in new_window_command
    assert ("-c", str(workdir)) == (
        new_window_command[new_window_command.index("-c")],
        new_window_command[new_window_command.index("-c") + 1],
    )
    assert "_codex-proxy" not in new_window_command[-1]


def test_claim_spare_tab_window_respawns_claimed_loading_pane(monkeypatch):
    controller = TmuxController("codux")
    created = SimpleNamespace(window_id="@9", content_pane_id="%9", nav_pane_id="%10")
    events: list[tuple[str, str] | tuple[str, str, str]] = []

    monkeypatch.setattr(controller, "spare_window", lambda: created)
    monkeypatch.setattr(
        controller,
        "_mark_tab_window",
        lambda claimed, title, tab_id: events.append(("mark", claimed.window_id, tab_id)),
    )
    monkeypatch.setattr(
        controller,
        "rename_window",
        lambda window_id, title: events.append(("rename", window_id, title)),
    )
    monkeypatch.setattr(
        controller,
        "_respawn_codex_pane",
        lambda config, pane_id: events.append(("respawn", pane_id)),
    )

    claimed = controller.claim_spare_tab_window(CoduxConfig(), AppState(), "New Codex", "tab123")

    assert claimed == created
    assert events == [
        ("mark", "@9", "tab123"),
        ("rename", "@9", "New Codex"),
        ("respawn", "%9"),
    ]


def test_ensure_empty_window_converts_spare_loading_window(monkeypatch):
    controller = TmuxController("codux")
    spare = SimpleNamespace(window_id="@9", content_pane_id="%9", nav_pane_id="%10")
    events: list[tuple[str, str] | tuple[str, str, str]] = []

    monkeypatch.setattr(controller, "empty_window_id", lambda: None)
    monkeypatch.setattr(controller, "spare_window", lambda: spare)
    monkeypatch.setattr(
        controller,
        "rename_window",
        lambda window_id, title: events.append(("rename", window_id, title)),
    )
    monkeypatch.setattr(
        controller,
        "_set_window_option",
        lambda window_id, option, value: events.append(("option", option, value)),
    )
    monkeypatch.setattr(
        controller,
        "_set_pane_title",
        lambda pane_id, title: events.append(("title", pane_id, title)),
    )
    monkeypatch.setattr(
        controller,
        "_new_empty_window",
        lambda config: (_ for _ in ()).throw(AssertionError("created empty window")),
    )

    assert controller.ensure_empty_window(CoduxConfig()) == "@9"

    assert events == [
        ("rename", "@9", "codux"),
        ("option", tmux_module.EMPTY_WINDOW_OPTION, "1"),
        ("option", tmux_module.SPARE_WINDOW_OPTION, "0"),
        ("option", tmux_module.TAB_ID_OPTION, ""),
        ("title", "%9", tmux_module.CODEX_PANE_TITLE),
        ("title", "%10", tmux_module.NAV_PANE_TITLE),
    ]


def test_refresh_window_frame_panes_resizes_nav_before_writing_render(monkeypatch):
    controller = TmuxController("codux")
    events: list[str] = []
    first = tab("one", "@1")
    second = tab("two", "@2")
    state = AppState(tabs=[first, second], active_tab_id=second.id)

    monkeypatch.setattr(controller, "has_session", lambda: True)
    monkeypatch.setattr(controller, "window_exists", lambda window_id: True)
    monkeypatch.setattr(
        controller,
        "_snapshot",
        lambda: SimpleNamespace(panes={}, windows={}, window=lambda _id: None),
    )
    monkeypatch.setattr(
        controller,
        "_ensure_native_window",
        lambda window_id, snapshot: events.append("native") or ("%nav", "%codex"),
    )
    monkeypatch.setattr(
        controller,
        "_ensure_window_frame",
        lambda window_id, snapshot: events.append("frame"),
    )
    monkeypatch.setattr(
        controller,
        "_ensure_nav_interactive_pane",
        lambda pane_id, snapshot: events.append(f"nav:{pane_id}"),
    )
    monkeypatch.setattr(
        controller,
        "_resize_nav_frame",
        lambda window_id, pane_id, height, snapshot: events.append(f"resize:{height}"),
    )
    monkeypatch.setattr(
        controller,
        "_refresh_navigation_targets",
        lambda config, state, snapshot: events.append("targets"),
    )
    monkeypatch.setattr(controller, "_border_panes", lambda window_id, snapshot: {})

    controller.refresh_window_frame_panes(CoduxConfig(), state, "@2")

    assert events == ["native", "frame", "nav:%nav", "resize:3", "targets"]


def test_refresh_window_frame_panes_respects_minimum_nav_height(monkeypatch):
    controller = TmuxController("codux")
    events: list[str] = []
    state = AppState()

    monkeypatch.setattr(controller, "has_session", lambda: True)
    monkeypatch.setattr(controller, "window_exists", lambda window_id: True)
    monkeypatch.setattr(
        controller,
        "_snapshot",
        lambda: SimpleNamespace(panes={}, windows={}, window=lambda _id: None),
    )
    monkeypatch.setattr(
        controller,
        "_ensure_native_window",
        lambda window_id, snapshot: ("%nav", "%codex"),
    )
    monkeypatch.setattr(controller, "_ensure_window_frame", lambda window_id, snapshot: None)
    monkeypatch.setattr(controller, "_ensure_nav_interactive_pane", lambda pane_id, snapshot: None)
    monkeypatch.setattr(
        controller,
        "_resize_nav_frame",
        lambda window_id, pane_id, height, snapshot: events.append(f"resize:{height}"),
    )
    monkeypatch.setattr(
        controller, "_refresh_navigation_targets", lambda config, state, snapshot: None
    )
    monkeypatch.setattr(controller, "_border_panes", lambda window_id, snapshot: {})

    controller.refresh_window_frame_panes(
        CoduxConfig(),
        state,
        "@2",
        min_nav_content_height=5,
    )

    assert events == ["resize:5"]


def test_ensure_window_frame_normalizes_expanded_side_edges(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    def pane(pane_id: str, role: str, width: int, height: int) -> PaneSnapshot:
        return PaneSnapshot(
            pane_id=pane_id,
            window_id="@1",
            role=role,
            title=role,
            top=0,
            left=0,
            width=width,
            height=height,
            current_command="python",
            start_command="",
            nav_host_version="",
            frame_host_version=tmux_module.FRAME_HOST_VERSION,
        )

    panes = {
        "%nav": pane("%nav", tmux_module.NAV_PANE_TITLE, 131, 2),
        "%codex": pane("%codex", tmux_module.CODEX_PANE_TITLE, 131, 35),
        "%nav-left": pane("%nav-left", "NAV_LEFT", 36, 6),
        "%nav-right": pane("%nav-right", "NAV_RIGHT", 35, 6),
        "%nav-top": pane("%nav-top", "NAV_TOP", 131, 1),
        "%nav-bottom": pane("%nav-bottom", "NAV_BOTTOM", 131, 1),
        "%codex-left": pane("%codex-left", "CODEX_LEFT", 36, 39),
        "%codex-right": pane("%codex-right", "CODEX_RIGHT", 35, 39),
        "%codex-top": pane("%codex-top", "CODEX_TOP", 131, 1),
        "%codex-bottom": pane("%codex-bottom", "CODEX_BOTTOM", 131, 1),
    }
    snapshot = TmuxSnapshot(
        windows={
            "@1": WindowSnapshot(
                window_id="@1",
                name="codux",
                width=204,
                height=46,
                empty="1",
                spare="0",
                tab_id="",
                nav_pane_configured="%nav",
                codex_pane_configured="%codex",
                frame_version=tmux_module.FRAME_LAYOUT_VERSION,
            )
        },
        panes=panes,
        panes_by_window={"@1": list(panes.values())},
    )

    monkeypatch.setattr(controller, "_install_window_options", lambda window_id: None)
    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    assert controller._ensure_window_frame("@1", snapshot)
    assert ("resize-pane", "-t", "%nav-left", "-x", str(tmux_module.FRAME_SIDE_WIDTH)) in commands
    assert ("resize-pane", "-t", "%nav-right", "-x", str(tmux_module.FRAME_SIDE_WIDTH)) in commands
    assert ("resize-pane", "-t", "%codex-left", "-x", str(tmux_module.FRAME_SIDE_WIDTH)) in commands
    assert (
        "resize-pane",
        "-t",
        "%codex-right",
        "-x",
        str(tmux_module.FRAME_SIDE_WIDTH),
    ) in commands
    assert not any(command[0] == "split-window" for command in commands)


def test_missing_codex_pane_rebuilds_from_existing_nav_after_killing_frames(monkeypatch):
    controller = TmuxController("codux")
    events: list[tuple[str, object]] = []

    monkeypatch.setattr(controller, "_install_window_options", lambda window_id: None)
    monkeypatch.setattr(
        controller, "_window_panes_from_snapshot", lambda window_id, snapshot: ("%nav", None)
    )
    monkeypatch.setattr(
        controller,
        "_kill_border_panes",
        lambda window_id, roles, snapshot: events.append(("kill-borders", window_id, roles)),
    )
    monkeypatch.setattr(
        controller,
        "_set_pane_role",
        lambda pane_id, role: events.append(("role", pane_id, role)),
    )
    monkeypatch.setattr(
        controller,
        "_set_pane_title",
        lambda pane_id, title: events.append(("title", pane_id, title)),
    )
    monkeypatch.setattr(
        controller,
        "_set_window_option",
        lambda window_id, option, value: events.append(("window-option", option, value)),
    )
    monkeypatch.setattr(
        controller,
        "_split_nav_pane",
        lambda pane_id, **kwargs: events.append(("split", pane_id)) or "%new-nav",
    )
    monkeypatch.setattr(controller, "_kill_duplicate_managed_panes", lambda *args, **kwargs: False)
    snapshot = SimpleNamespace()
    assert controller._ensure_native_window("@1", snapshot) == ("%new-nav", "%nav")

    assert ("kill-borders", "@1", tmux_module.BORDER_ROLES) in events
    assert ("role", "%nav", "CODEX") in events
    assert ("split", "%nav") in events


def test_missing_content_pane_during_nav_split_is_ignored(monkeypatch):
    controller = TmuxController("codux")
    events: list[tuple[str, object]] = []

    monkeypatch.setattr(controller, "_install_window_options", lambda window_id: None)
    monkeypatch.setattr(
        controller, "_window_panes_from_snapshot", lambda window_id, snapshot: (None, "%gone")
    )
    monkeypatch.setattr(
        controller,
        "_kill_border_panes",
        lambda window_id, roles, snapshot: events.append(("kill-borders", window_id, roles)),
    )
    monkeypatch.setattr(controller, "_kill_duplicate_managed_panes", lambda *args, **kwargs: False)
    monkeypatch.setattr(
        tmux_module,
        "_run_tmux",
        lambda args, check=True: SimpleNamespace(
            returncode=1,
            stdout="",
            stderr="can't find pane: %gone",
        ),
    )

    assert controller._ensure_native_window("@1", SimpleNamespace()) == (None, None)
    assert ("kill-borders", "@1", tmux_module.BORDER_ROLES) in events


def test_short_nav_pane_is_expanded_before_frame_creation(monkeypatch):
    controller = TmuxController("codux")
    events: list[tuple[str, object]] = []
    pane = PaneSnapshot(
        pane_id="%nav",
        window_id="@1",
        role="NAV",
        title="NAV",
        top=0,
        left=0,
        width=120,
        height=2,
        current_command="python3.14",
        start_command="python -m codux.cli _nav-pane",
        nav_host_version="13",
        frame_host_version="",
    )
    snapshot = TmuxSnapshot(
        windows={},
        panes={"%nav": pane},
        panes_by_window={"@1": [pane]},
    )

    monkeypatch.setattr(
        controller,
        "_tmux",
        lambda args, check=True: events.append(("tmux", tuple(args))) or "",
    )
    monkeypatch.setattr(controller, "pane_size", lambda pane_id: (120, 7))
    monkeypatch.setattr(
        controller,
        "_create_border_pane",
        lambda pane_id, role, split_args: events.append(("create", role)) or f"%{role}",
    )

    controller._ensure_pane_frame("@1", "%nav", tmux_module.NAV_PANE_TITLE, snapshot)

    assert (
        "tmux",
        ("resize-pane", "-t", "%nav", "-y", tmux_module.DEFAULT_NAV_FRAME_HEIGHT),
    ) in events
    assert ("create", "NAV_TOP") in events
    assert ("create", "NAV_BOTTOM") in events


def test_window_panes_prefers_configured_native_panes(monkeypatch):
    controller = TmuxController("codux")
    panes = {
        "%stale": PaneSnapshot(
            pane_id="%stale",
            window_id="@1",
            role="NAV",
            title="NAV",
            top=7,
            left=0,
            width=10,
            height=10,
            current_command="python",
            start_command="python -m codux.cli _nav-pane",
            nav_host_version="",
            frame_host_version="",
        ),
        "%nav": PaneSnapshot(
            pane_id="%nav",
            window_id="@1",
            role="NAV",
            title="NAV",
            top=2,
            left=0,
            width=10,
            height=10,
            current_command="python",
            start_command="python -m codux.cli _nav-pane",
            nav_host_version="",
            frame_host_version="",
        ),
        "%codex": PaneSnapshot(
            pane_id="%codex",
            window_id="@1",
            role="CODEX",
            title="Ready",
            top=18,
            left=0,
            width=10,
            height=10,
            current_command="codex",
            start_command="codex",
            nav_host_version="",
            frame_host_version="",
        ),
    }
    snapshot = TmuxSnapshot(
        windows={
            "@1": WindowSnapshot(
                window_id="@1",
                name="one",
                width=120,
                height=40,
                empty="0",
                spare="0",
                tab_id="",
                nav_pane_configured="%nav",
                codex_pane_configured="%codex",
                frame_version="",
            )
        },
        panes=panes,
        panes_by_window={
            "@1": list(panes.values()),
        },
    )
    monkeypatch.setattr(controller, "_snapshot", lambda: snapshot)

    assert controller._window_panes("@1") == ("%nav", "%codex")


def test_duplicate_managed_nav_pane_is_removed_and_frames_rebuilt(monkeypatch):
    controller = TmuxController("codux")
    events: list[tuple[str, object]] = []

    monkeypatch.setattr(controller, "_install_window_options", lambda window_id: None)
    monkeypatch.setattr(
        controller,
        "_window_panes_from_snapshot",
        lambda window_id, snapshot: ("%nav", "%codex"),
    )
    monkeypatch.setattr(
        controller,
        "_duplicate_managed_panes_from_snapshot",
        lambda window_id, keep, snapshot: ["%old"],
    )
    monkeypatch.setattr(
        controller,
        "_kill_border_panes",
        lambda window_id, roles, snapshot: events.append(("kill-borders", roles)),
    )
    monkeypatch.setattr(
        controller,
        "_configure_native_panes",
        lambda window_id, nav, codex: events.append(("configure", nav, codex)),
    )
    monkeypatch.setattr(
        controller,
        "_tmux",
        lambda args, check=True: events.append(("tmux", tuple(args))) or "",
    )

    snapshot = SimpleNamespace()
    assert controller._ensure_native_window("@1", snapshot) == ("%nav", "%codex")

    assert ("tmux", ("kill-pane", "-t", "%old")) in events
    assert ("kill-borders", tmux_module.BORDER_ROLES) in events
    assert ("configure", "%nav", "%codex") in events


def test_window_exists_is_scoped_to_codux_session(monkeypatch):
    controller = TmuxController("codux")

    def fake_run_tmux(args, check=True):
        assert args[:3] == ["list-windows", "-t", "codux"]
        return SimpleNamespace(returncode=0, stdout="@1\n@2\n")

    monkeypatch.setattr(tmux_module, "run_tmux", fake_run_tmux)

    assert controller.window_exists("@2")
    assert not controller.window_exists("@3")


def test_repair_window_sizes_expands_manual_windows_to_attached_client(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    def fake_tmux(args, check=True):
        commands.append(tuple(args))
        if args[:3] == ["list-clients", "-t", "codux"]:
            return "204\t46\n"
        return ""

    monkeypatch.setattr(controller, "_tmux", fake_tmux)
    monkeypatch.setattr(controller, "_snapshot", lambda: SimpleNamespace())
    monkeypatch.setattr(
        controller, "_codux_windows", lambda snapshot: [("@1", False), ("@2", False)]
    )
    monkeypatch.setattr(controller, "window_size", lambda window_id: (80, 24))

    controller.repair_window_sizes()

    assert ("resize-window", "-t", "@1", "-x", "204", "-y", "46") in commands
    assert ("resize-window", "-t", "@2", "-x", "204", "-y", "46") in commands
    assert ("set-window-option", "-t", "@1", "window-size", "latest") in commands
    assert ("set-window-option", "-t", "@2", "window-size", "latest") in commands


def test_repair_window_sizes_uses_largest_window_when_detached(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    def fake_tmux(args, check=True):
        commands.append(tuple(args))
        if args[:3] == ["list-clients", "-t", "codux"]:
            return ""
        if args[:3] == ["list-windows", "-t", "codux"]:
            return "80\t24\n204\t46\n"
        return ""

    monkeypatch.setattr(controller, "_tmux", fake_tmux)
    monkeypatch.setattr(controller, "_snapshot", lambda: SimpleNamespace())
    monkeypatch.setattr(
        controller, "_codux_windows", lambda snapshot: [("@1", False), ("@2", False)]
    )
    monkeypatch.setattr(controller, "window_size", lambda window_id: (80, 24))

    controller.repair_window_sizes()

    assert ("resize-window", "-t", "@1", "-x", "204", "-y", "46") in commands
    assert ("resize-window", "-t", "@2", "-x", "204", "-y", "46") in commands


def test_repair_window_sizes_skips_windows_already_at_target(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    def fake_tmux(args, check=True):
        commands.append(tuple(args))
        if args[:3] == ["list-clients", "-t", "codux"]:
            return "204\t46\n"
        return ""

    monkeypatch.setattr(controller, "_tmux", fake_tmux)
    monkeypatch.setattr(controller, "_snapshot", lambda: SimpleNamespace())
    monkeypatch.setattr(controller, "_codux_windows", lambda snapshot: [("@1", False)])
    monkeypatch.setattr(controller, "window_size", lambda window_id: (204, 46))

    controller.repair_window_sizes()

    assert not any(command[0] == "resize-window" for command in commands)


def test_repair_window_sizes_uses_launch_terminal_before_attached_client(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))
    monkeypatch.setattr(controller, "attached_client_size", lambda: None)
    monkeypatch.setattr(tmux_module, "launch_terminal_size", lambda: (132, 40))
    monkeypatch.setattr(
        controller,
        "largest_window_size",
        lambda: (_ for _ in ()).throw(AssertionError("used detached tmux size")),
    )
    monkeypatch.setattr(controller, "window_size", lambda window_id: (80, 24))

    controller._sync_window_to_attached_client("@1")

    assert ("resize-window", "-t", "@1", "-x", "132", "-y", "40") in commands
    assert ("set-window-option", "-t", "@1", "window-size", "latest") in commands


def test_request_nav_repaint_sends_ctrl_l_to_configured_nav_panes(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []
    snapshot = TmuxSnapshot(
        windows={
            "@1": WindowSnapshot(
                window_id="@1",
                name="one",
                width=120,
                height=30,
                empty="0",
                spare="0",
                tab_id="one",
                nav_pane_configured="%nav",
                codex_pane_configured="%codex",
                frame_version=tmux_module.FRAME_LAYOUT_VERSION,
            )
        },
        panes={
            "%nav": PaneSnapshot(
                pane_id="%nav",
                window_id="@1",
                role="",
                title="",
                top=0,
                left=0,
                width=120,
                height=3,
                current_command="python",
                start_command="_nav-pane",
                nav_host_version=tmux_module.NAV_HOST_VERSION,
                frame_host_version="",
            ),
            "%role-nav": PaneSnapshot(
                pane_id="%role-nav",
                window_id="@1",
                role=tmux_module.NAV_PANE_TITLE,
                title=tmux_module.NAV_PANE_TITLE,
                top=0,
                left=0,
                width=120,
                height=3,
                current_command="python",
                start_command="_nav-pane",
                nav_host_version=tmux_module.NAV_HOST_VERSION,
                frame_host_version="",
            ),
        },
        panes_by_window={"@1": []},
    )

    monkeypatch.setattr(controller, "has_session", lambda: True)
    monkeypatch.setattr(controller, "_snapshot", lambda: snapshot)
    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    controller.request_nav_repaint()

    assert commands == [
        ("send-keys", "-t", "%nav", "C-l"),
        ("send-keys", "-t", "%role-nav", "C-l"),
    ]


def test_rename_prompt_uses_popup():
    controller = TmuxController("codux")

    command = controller._rename_prompt_command("python -m codux.cli")

    assert "display-popup" in command
    assert " -E " in command
    assert f"-d {shlex.quote(str(tmux_module.PROJECT_ROOT))}" in command
    assert "-s fg=default,bg=default" in command
    assert "_popup-rename" in command
    assert "command-prompt" not in command


def test_create_tab_window_does_not_force_manual_window_size(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    def fake_tmux(args, check=True):
        commands.append(tuple(args))
        if args and args[0] == "new-window":
            return "@9\t%9"
        return ""

    monkeypatch.setattr(controller, "ensure_session", lambda config: None)
    monkeypatch.setattr(controller, "_tmux", fake_tmux)
    monkeypatch.setattr(controller, "_split_nav_pane", lambda pane_id, **kwargs: "%10")

    controller.create_tab_window(CoduxConfig(), "New Codex", "tab123")

    assert not any(command[0] == "resize-window" for command in commands)


def test_frame_rebuilds_when_border_roles_are_duplicated():
    controller = TmuxController("codux")
    snapshot = TmuxSnapshot(windows={}, panes={}, panes_by_window={})

    assert controller._frame_needs_rebuild(
        {
            "%1": "CODEX_TOP",
            "%2": "CODEX_TOP",
            "%3": "CODEX_BOTTOM",
            "%4": "CODEX_LEFT",
            "%5": "CODEX_RIGHT",
        },
        {"CODEX_TOP", "CODEX_BOTTOM", "CODEX_LEFT", "CODEX_RIGHT"},
        snapshot,
    )


def test_real_tab_loading_pane_is_respawned_as_codex(monkeypatch):
    controller = TmuxController("codux")
    snapshot = TmuxSnapshot(
        windows={
            "@1": WindowSnapshot(
                window_id="@1",
                name="one",
                width=120,
                height=40,
                empty="0",
                spare="0",
                tab_id="",
                nav_pane_configured="",
                codex_pane_configured="%1",
                frame_version="",
            )
        },
        panes={
            "%1": PaneSnapshot(
                pane_id="%1",
                window_id="@1",
                role="CODEX",
                title="CODEX",
                top=0,
                left=0,
                width=10,
                height=10,
                current_command="python3.14",
                start_command="/venv/bin/python3 -m codux.cli _loading-pane",
                nav_host_version="",
                frame_host_version="",
            )
        },
        panes_by_window={"@1": []},
    )
    snapshot = TmuxSnapshot(
        windows=snapshot.windows,
        panes=snapshot.panes,
        panes_by_window={"@1": list(snapshot.panes.values())},
    )

    assert controller._pane_needs_codex_respawn("@1", "%1", snapshot)


def test_nav_pane_process_is_respawned_as_codex(monkeypatch):
    controller = TmuxController("codux")
    snapshot = TmuxSnapshot(
        windows={
            "@1": WindowSnapshot(
                window_id="@1",
                name="one",
                width=120,
                height=40,
                empty="0",
                spare="0",
                tab_id="",
                nav_pane_configured="",
                codex_pane_configured="%1",
                frame_version="",
            )
        },
        panes={
            "%1": PaneSnapshot(
                pane_id="%1",
                window_id="@1",
                role="NAV",
                title="NAV",
                top=0,
                left=0,
                width=10,
                height=10,
                current_command="python3.14",
                start_command="/venv/bin/python3 -m codux.cli _nav-pane",
                nav_host_version="",
                frame_host_version="",
            )
        },
        panes_by_window={"@1": []},
    )
    snapshot = TmuxSnapshot(
        windows=snapshot.windows,
        panes=snapshot.panes,
        panes_by_window={"@1": list(snapshot.panes.values())},
    )

    assert controller._pane_needs_codex_respawn("@1", "%1", snapshot)


def test_spare_loading_pane_is_not_respawned_as_codex(monkeypatch):
    controller = TmuxController("codux")
    snapshot = TmuxSnapshot(
        windows={
            "@1": WindowSnapshot(
                window_id="@1",
                name="one",
                width=120,
                height=40,
                empty="0",
                spare="1",
                tab_id="",
                nav_pane_configured="",
                codex_pane_configured="%1",
                frame_version="",
            )
        },
        panes={
            "%1": PaneSnapshot(
                pane_id="%1",
                window_id="@1",
                role="CODEX",
                title="CODEX",
                top=0,
                left=0,
                width=10,
                height=10,
                current_command="python3.14",
                start_command="/venv/bin/python3 -m codux.cli _loading-pane",
                nav_host_version="",
                frame_host_version="",
            )
        },
        panes_by_window={"@1": []},
    )
    snapshot = TmuxSnapshot(
        windows=snapshot.windows,
        panes=snapshot.panes,
        panes_by_window={"@1": list(snapshot.panes.values())},
    )

    assert not controller._pane_needs_codex_respawn("@1", "%1", snapshot)


def test_render_frame_pane_sends_base64_payload(monkeypatch):
    controller = TmuxController("codux")
    calls: list[tuple[str, ...]] = []
    snapshot = TmuxSnapshot(windows={}, panes={}, panes_by_window={})

    monkeypatch.setattr(controller, "_ensure_frame_pane", lambda pane_id, snapshot: None)
    monkeypatch.setattr(
        controller, "_tmux", lambda args, check=True: calls.append(tuple(args)) or ""
    )

    controller._render_frame_pane("%1", "hello\nworld\n", snapshot)

    assert calls[0][:3] == ("send-keys", "-l", "-t")
    assert calls[0][3] == "%1"
    assert calls[0][4].startswith("CODUX_FRAME:")
    length_text, checksum_text, payload = calls[0][4].removeprefix("CODUX_FRAME:").split(":", 2)
    decoded = base64.b64decode(payload.encode("ascii"), validate=True)
    assert int(length_text) == len(decoded)
    assert int(checksum_text, 16) == zlib.crc32(decoded) & 0xFFFFFFFF
    assert decoded == b"hello\nworld\n"
    assert calls[1] == ("send-keys", "-t", "%1", "Enter")


def test_create_border_pane_ignores_missing_target_pane(monkeypatch):
    controller = TmuxController("codux")
    monkeypatch.setattr(
        tmux_module,
        "_run_tmux",
        lambda args, check=True: SimpleNamespace(
            returncode=1,
            stdout="",
            stderr="can't find pane: %gone",
        ),
    )
    monkeypatch.setattr(
        controller,
        "_set_pane_role",
        lambda pane_id, role: (_ for _ in ()).throw(AssertionError("role set")),
    )

    assert controller._create_border_pane("%gone", "CODEX_TOP", ["-v"]) is None


def test_ensure_frame_pane_waits_until_ready_before_render_input(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []
    option_values = iter(["", tmux_module.FRAME_HOST_VERSION])
    snapshot = TmuxSnapshot(
        windows={},
        panes={
            "%1": PaneSnapshot(
                pane_id="%1",
                window_id="@1",
                role="CODEX",
                title="CODEX",
                top=0,
                left=0,
                width=10,
                height=5,
                current_command="python",
                start_command="python -m codux.cli _frame-pane",
                nav_host_version="",
                frame_host_version="",
            )
        },
        panes_by_window={},
    )

    def fake_tmux(args, check=True):
        commands.append(tuple(args))
        if args[:3] == ["show-option", "-p", "-qv"]:
            return next(option_values)
        return ""

    monkeypatch.setattr(controller, "_tmux", fake_tmux)
    monkeypatch.setattr(tmux_module.time, "sleep", lambda seconds: None)

    controller._render_frame_pane("%1", "hello", snapshot)

    assert commands[0][:3] == ("respawn-pane", "-k", "-t")
    assert ("show-option", "-p", "-qv", "-t", "%1", tmux_module.FRAME_HOST_OPTION) in commands
    assert (
        "set-option",
        "-p",
        "-t",
        "%1",
        tmux_module.FRAME_HOST_OPTION,
        tmux_module.FRAME_HOST_VERSION,
    ) not in commands
    assert commands[-2][0] == "send-keys"


def test_empty_content_uses_pane_dimensions():
    controller = TmuxController("codux")
    snapshot = TmuxSnapshot(
        windows={},
        panes={
            "%empty": PaneSnapshot(
                pane_id="%empty",
                window_id="@1",
                role="CODEX",
                title="CODEX",
                top=0,
                left=0,
                width=30,
                height=6,
                current_command="python3.14",
                start_command="python -m codux.cli _frame-pane",
                nav_host_version="",
                frame_host_version=tmux_module.FRAME_HOST_VERSION,
            )
        },
        panes_by_window={"@1": []},
    )

    assert controller._empty_content("%empty", snapshot).splitlines() == [
        "",
        "",
        "      No Codex tabs open",
        "    Press n to create one.",
    ]


def test_terminal_options_enable_extended_keys_for_shift_enter(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    def fake_tmux(args, check=True):
        commands.append(tuple(args))
        if args[:4] == ["show-options", "-s", "-qv", "terminal-features"]:
            return ""
        return ""

    monkeypatch.setattr(controller, "_tmux", fake_tmux)

    controller._install_terminal_options()

    assert ("set-option", "-s", "extended-keys", "on") in commands
    assert ("set-option", "-s", "extended-keys-format", "xterm") in commands
    assert ("set-option", "-as", "terminal-features", ",*:extkeys") in commands


def test_window_options_neutralize_native_tmux_colors(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    controller._install_window_options("@1")

    assert ("set-window-option", "-t", "@1", "window-style", "fg=default,bg=default") in commands
    assert (
        "set-window-option",
        "-t",
        "@1",
        "window-active-style",
        "fg=default,bg=default",
    ) in commands
    assert (
        "set-window-option",
        "-t",
        "@1",
        "pane-border-style",
        "fg=default,bg=default",
    ) in commands


def test_session_environment_passes_terminal_metadata_without_codux_color_hints(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    monkeypatch.setenv("COLORTERM", "truecolor")
    monkeypatch.setenv("COLORFGBG", "15;0")
    monkeypatch.setenv("ITERM_PROFILE", "Default")
    monkeypatch.setenv("CODUX_BG_RGB", "29,38,42")
    monkeypatch.setenv("NO_COLOR", "1")
    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    controller._install_session_environment()

    assert ("set-environment", "-t", "codux", "-u", "CODUX_BG_RGB") in commands
    assert ("set-environment", "-t", "codux", "-u", "NO_COLOR") in commands
    assert ("set-environment", "-t", "codux", "COLORTERM", "truecolor") in commands
    assert ("set-environment", "-t", "codux", "COLORFGBG", "15;0") in commands
    assert ("set-environment", "-t", "codux", "ITERM_PROFILE", "Default") in commands


def test_session_environment_preserves_missing_terminal_metadata(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    monkeypatch.delenv("COLORTERM", raising=False)
    monkeypatch.delenv("ITERM_PROFILE", raising=False)
    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    controller._install_session_environment()

    assert ("set-environment", "-t", "codux", "-u", "COLORTERM") not in commands
    assert ("set-environment", "-t", "codux", "-u", "ITERM_PROFILE") not in commands


def test_enter_uses_direct_focus_from_nav_pane(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    controller._install_bindings(CoduxConfig(), "codux")

    enter_bindings = [
        command
        for command in commands
        if command[:3] == ("bind-key", "-n", "Enter") or command[:3] == ("bind-key", "-n", "C-m")
    ]
    assert len(enter_bindings) == 2
    assert all("@codux-codex-pane" in " ".join(command) for command in enter_bindings)
    assert all("_focus-window" in " ".join(command) for command in enter_bindings)
    assert all("|| true" in " ".join(command) for command in enter_bindings)


def test_new_key_is_left_for_native_nav_pane(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    controller._install_bindings(CoduxConfig(), "codux")

    assert ("unbind-key", "-n", "n") in commands
    assert not any(command[:3] == ("bind-key", "-n", "n") for command in commands)
