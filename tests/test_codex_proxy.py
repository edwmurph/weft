from __future__ import annotations

import os

from codux.codex_proxy import _ProbeProxy


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
