from __future__ import annotations

import shlex
from types import SimpleNamespace

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

    assert command == "exec codex --foo"
    assert "_codex-proxy" not in command
    assert "tui.theme" not in command
    assert "CODUX_" not in command


def test_codex_shell_command_preserves_user_codex_command():
    controller = TmuxController("codux")
    command = controller._codex_shell_command(
        CoduxConfig(codex_command='codex -c tui.theme="dark" --foo')
    )

    assert command == 'exec codex -c tui.theme="dark" --foo'


def test_codex_shell_command_does_not_rewrite_custom_launcher():
    controller = TmuxController("codux")
    command = controller._codex_shell_command(CoduxConfig(codex_command="my-codex --foo"))

    assert command == "exec my-codex --foo"


def test_tmux_internal_shell_commands_run_from_project_root(monkeypatch):
    controller = TmuxController("codux")
    monkeypatch.setattr(tmux_module.sys, "executable", "/tmp/codux python")
    root = shlex.quote(str(tmux_module.PROJECT_ROOT))
    python = shlex.quote("/tmp/codux python")

    assert controller._nav_shell_command("%1") == (
        f"cd {root} && env TMUX_PANE=%1 {python} -m codux.cli _nav-pane"
    )
    assert controller._loading_shell_command() == (
        f"cd {root} && {python} -m codux.cli _loading-pane"
    )
    assert (
        controller._frame_pane_shell_command() == f"cd {root} && {python} -m codux.cli _frame-pane"
    )
    assert controller._codux_cli_command() == f"cd {root} && {python} -m codux.cli"


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


def test_create_tab_window_uses_native_nav_and_codex_panes(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []
    selected_windows: list[str] = []

    def fake_tmux(args, check=True):
        commands.append(tuple(args))
        if args and args[0] == "new-window":
            return "@9\t%9"
        return ""

    monkeypatch.setattr(controller, "ensure_session", lambda config: None)
    monkeypatch.setattr(controller, "_tmux", fake_tmux)
    monkeypatch.setattr(controller, "_split_nav_pane", lambda pane_id: "%10")
    monkeypatch.setattr(controller, "select_window", selected_windows.append)

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
    monkeypatch.setattr(controller, "_border_panes", lambda window_id, snapshot: {})

    controller.refresh_window_frame_panes(CoduxConfig(), state, "@2")

    assert events == ["native", "frame", "nav:%nav", "resize:3"]


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
        lambda pane_id: events.append(("split", pane_id)) or "%new-nav",
    )
    monkeypatch.setattr(controller, "_kill_duplicate_managed_panes", lambda *args, **kwargs: False)
    snapshot = SimpleNamespace()
    assert controller._ensure_native_window("@1", snapshot) == ("%new-nav", "%nav")

    assert ("kill-borders", "@1", tmux_module.BORDER_ROLES) in events
    assert ("role", "%nav", "CODEX") in events
    assert ("split", "%nav") in events


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
    monkeypatch.setattr(controller, "_split_nav_pane", lambda pane_id: "%10")

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
    assert calls[1] == ("send-keys", "-t", "%1", "Enter")


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
    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    controller._install_session_environment()

    assert ("set-environment", "-t", "codux", "-u", "CODUX_BG_RGB") in commands
    assert ("set-environment", "-t", "codux", "COLORTERM", "truecolor") in commands
    assert ("set-environment", "-t", "codux", "COLORFGBG", "15;0") in commands
    assert ("set-environment", "-t", "codux", "ITERM_PROFILE", "Default") in commands


def test_session_environment_unsets_missing_terminal_metadata(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    monkeypatch.delenv("COLORTERM", raising=False)
    monkeypatch.delenv("ITERM_PROFILE", raising=False)
    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    controller._install_session_environment()

    assert ("set-environment", "-t", "codux", "-u", "COLORTERM") in commands
    assert ("set-environment", "-t", "codux", "-u", "ITERM_PROFILE") in commands


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
