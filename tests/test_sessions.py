from __future__ import annotations

from types import SimpleNamespace

import codux.sessions as sessions_module


def test_list_codux_sessions_parses_tmux_metadata(monkeypatch):
    stdout = "\n".join(
        [
            "codux-current\t2\t100\t1\t/Users/me/current\t/tmp/current\t/repo",
            "notes\t1\t101\t0\t\t\t",
            "codux-other\t3\t102\t0\t/Users/me/other\t/tmp/other\t/repo",
        ]
    )
    calls: list[tuple[str, ...]] = []

    def fake_run_tmux(args, *, check=True):
        calls.append(tuple(args))
        return SimpleNamespace(returncode=0, stdout=stdout)

    monkeypatch.setattr(sessions_module, "run_tmux", fake_run_tmux)

    sessions = sessions_module.list_codux_sessions("codux-current")

    assert calls[0][:2] == ("list-sessions", "-F")
    assert [session.name for session in sessions] == ["codux-current", "codux-other"]
    assert sessions[0].current
    assert sessions[0].window_count == 2
    assert sessions[0].attached_clients == 1
    assert sessions[1].workdir == "/Users/me/other"


def test_other_codux_session_count_excludes_current(monkeypatch):
    monkeypatch.setattr(
        sessions_module,
        "list_codux_sessions",
        lambda current: [
            sessions_module.CoduxSession("codux-current", "", "", "", 1, 1, 1, True),
            sessions_module.CoduxSession("codux-other", "", "", "", 1, 1, 0, False),
        ],
    )

    assert sessions_module.other_codux_session_count("codux-current") == 1


def test_kill_codux_session_targets_exact_tmux_session(monkeypatch):
    calls: list[tuple[str, ...]] = []

    def fake_run_tmux(args, *, check=True):
        calls.append(tuple(args))
        return SimpleNamespace(returncode=0, stdout="")

    monkeypatch.setattr(sessions_module, "run_tmux", fake_run_tmux)

    assert sessions_module.kill_codux_session("codux-old")
    assert calls == [("kill-session", "-t", "codux-old")]
