from __future__ import annotations

import os

from codux.codex_proxy import DEFAULT_BG_RGB, DEFAULT_FG_RGB, _ProbeProxy, _terminal_rgb


def test_probe_proxy_clears_loading_before_first_visible_output():
    stdout_read, stdout_write = os.pipe()
    child_read, child_write = os.pipe()
    cleared: list[bool] = []
    proxy = _ProbeProxy({})

    try:
        proxy.feed(
            b"hello world",
            stdout_write,
            child_write,
            before_stdout=lambda: cleared.append(True),
        )
        os.close(stdout_write)
        stdout_write = -1

        assert cleared == [True]
        assert os.read(stdout_read, 1024)
    finally:
        for fd in (stdout_read, stdout_write, child_read, child_write):
            if fd >= 0:
                os.close(fd)


def test_terminal_color_probe_defaults_to_profile_like_dark_background(monkeypatch):
    monkeypatch.delenv("CODUX_COLORFGBG", raising=False)
    monkeypatch.delenv("CODUX_FG_RGB", raising=False)
    monkeypatch.delenv("CODUX_BG_RGB", raising=False)

    assert _terminal_rgb({}) == (DEFAULT_FG_RGB, DEFAULT_BG_RGB)


def test_terminal_color_probe_allows_rgb_override():
    assert _terminal_rgb({"CODUX_FG_RGB": "232,235,237", "CODUX_BG_RGB": "29,38,42"}) == (
        (232, 235, 237),
        (29, 38, 42),
    )


def test_terminal_color_probe_allows_colorfgbg_override():
    assert _terminal_rgb({"CODUX_COLORFGBG": "0;15"}) == ((0, 0, 0), (255, 255, 255))
