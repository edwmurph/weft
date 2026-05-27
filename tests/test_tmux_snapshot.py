from __future__ import annotations

from codux.tmux_snapshot import fetch_snapshot


def test_fetch_snapshot_parses_windows_and_panes():
    windows_out = "\n".join(
        [
            "@1\tone\t120\t40\t0\t0\ttab1\t%nav\t%codex\t6",
            "@2\ttwo\t80\t24\t1\t0\t\t%nav2\t%codex2\t6",
        ]
    )
    panes_out = "\n".join(
        [
            "%nav\t@1\tNAV\tNAV\t0\t0\t120\t7\tpython\tpython -m codux.cli _nav-pane\t13\t",
            "%codex\t@1\tCODEX\tCODEX\t7\t0\t120\t33\tcodex\tcodex\t\t1",
            "%top\t@1\tCODEX_TOP\tCODEX_TOP\t6\t0\t120\t1\tsleep\tsleep\t\t1",
        ]
    )

    def fake_tmux(args, check=False):
        if args[:2] == ["list-windows", "-t"]:
            return windows_out
        if args[:2] == ["list-panes", "-s"]:
            return panes_out
        raise AssertionError(f"unexpected tmux args: {args}")

    snapshot = fetch_snapshot(
        fake_tmux,
        session_name="codux",
        empty_window_option="@codux-empty",
        spare_window_option="@codux-spare",
        tab_id_option="@codux-tab-id",
        frame_version_option="@codux-frame-version",
        nav_host_option="@codux-nav-host",
        frame_host_option="@codux-frame-host",
    )

    assert snapshot.window("@1") is not None
    assert snapshot.window("@2") is not None
    assert snapshot.window("@2").empty == "1"
    assert snapshot.panes["%codex"].role == "CODEX"
    assert snapshot.panes["%top"].height == 1
    assert {pane.pane_id for pane in snapshot.window_panes("@1")} == {"%nav", "%codex", "%top"}
