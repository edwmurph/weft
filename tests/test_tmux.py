from __future__ import annotations

import codux.tmux as tmux_module
from codux.config import CoduxConfig
from codux.state import AppState, Tab, now_iso
from codux.tmux import TmuxController
from types import SimpleNamespace


def tab(tab_id: str, window_id: str) -> Tab:
    created_at = now_iso()
    return Tab(
        id=tab_id,
        title=tab_id,
        column="inbox",
        tmux_session="codux",
        tmux_window_id=window_id,
        tmux_pane_id=f"%{tab_id}",
        created_at=created_at,
        updated_at=created_at,
    )


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


def test_create_tab_window_does_not_select_detached_window(monkeypatch):
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
    monkeypatch.setattr(controller, "_split_nav_pane", lambda config, pane_id: "%10")
    monkeypatch.setattr(controller, "select_window", selected_windows.append)

    created = controller.create_tab_window(CoduxConfig(), "New Codex", "tab123")

    assert created.window_id == "@9"
    assert created.content_pane_id == "%9"
    assert created.nav_pane_id == "%10"
    assert selected_windows == []
    new_window_command = next(command for command in commands if command[0] == "new-window")
    assert "-d" in new_window_command


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
        controller, "_ensure_window_frame", lambda window_id: events.append("frame")
    )
    monkeypatch.setattr(controller, "nav_pane_for_window", lambda window_id: "%nav")
    monkeypatch.setattr(
        controller,
        "_resize_nav_frame",
        lambda window_id, pane_id, height: events.append(f"resize:{height}"),
    )
    monkeypatch.setattr(
        tmux_module,
        "write_render_files",
        lambda config, current_state: events.append("write"),
    )
    monkeypatch.setattr(controller, "_border_panes", lambda window_id: {})

    controller.refresh_window_frame_panes(CoduxConfig(), state, "@2")

    assert events == ["frame", "resize:3", "write"]


def test_window_exists_is_scoped_to_codux_session(monkeypatch):
    controller = TmuxController("codux")

    def fake_run_tmux(args, check=True):
        assert args[:3] == ["list-windows", "-t", "codux"]
        return SimpleNamespace(returncode=0, stdout="@1\n@2\n")

    monkeypatch.setattr(tmux_module, "_run_tmux", fake_run_tmux)

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
    monkeypatch.setattr(controller, "_codux_windows", lambda: [("@1", False), ("@2", False)])
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
    monkeypatch.setattr(controller, "_codux_windows", lambda: [("@1", False), ("@2", False)])
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
    monkeypatch.setattr(controller, "_codux_windows", lambda: [("@1", False)])
    monkeypatch.setattr(controller, "window_size", lambda window_id: (204, 46))

    controller.repair_window_sizes()

    assert not any(command[0] == "resize-window" for command in commands)


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
    monkeypatch.setattr(controller, "_split_nav_pane", lambda config, pane_id: "%10")

    controller.create_tab_window(CoduxConfig(), "New Codex", "tab123")

    assert not any(command[0] == "resize-window" for command in commands)


def test_frame_rebuilds_when_border_roles_are_duplicated():
    controller = TmuxController("codux")

    assert controller._frame_needs_rebuild(
        {
            "%1": "CODEX_TOP",
            "%2": "CODEX_TOP",
            "%3": "CODEX_BOTTOM",
            "%4": "CODEX_LEFT",
            "%5": "CODEX_RIGHT",
        },
        {"CODEX_TOP", "CODEX_BOTTOM", "CODEX_LEFT", "CODEX_RIGHT"},
    )


def test_real_tab_loading_pane_is_respawned_as_codex(monkeypatch):
    controller = TmuxController("codux")
    monkeypatch.setattr(controller, "_window_option", lambda window_id, option: "0")
    monkeypatch.setattr(controller, "_pane_current_command", lambda pane_id: "python3.14")
    monkeypatch.setattr(
        controller,
        "_pane_start_command",
        lambda pane_id: "/venv/bin/python3 -m codux.cli _loading-pane",
    )

    assert controller._pane_needs_codex_respawn("@1", "%1")


def test_spare_loading_pane_is_not_respawned_as_codex(monkeypatch):
    controller = TmuxController("codux")
    monkeypatch.setattr(controller, "_window_option", lambda window_id, option: "1")
    monkeypatch.setattr(controller, "_pane_current_command", lambda pane_id: "python3.14")
    monkeypatch.setattr(
        controller,
        "_pane_start_command",
        lambda pane_id: "/venv/bin/python3 -m codux.cli _loading-pane",
    )

    assert not controller._pane_needs_codex_respawn("@1", "%1")


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


def test_new_binding_only_intercepts_empty_host_pane(monkeypatch):
    controller = TmuxController("codux")
    commands: list[tuple[str, ...]] = []

    monkeypatch.setattr(controller, "_tmux", lambda args, check=True: commands.append(tuple(args)))

    controller._install_bindings(CoduxConfig(), "codux")

    new_binding = next(command for command in commands if command[:3] == ("bind-key", "-n", "n"))
    command_text = " ".join(new_binding)
    assert "@codux-host" in command_text
    assert "codux new" in command_text
    assert "send-keys n" in command_text
