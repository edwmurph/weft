from __future__ import annotations

import pytest

from codux.config import APP_DIR_ENV, WORKDIR_ENV


@pytest.fixture(autouse=True)
def isolate_codux_environment(monkeypatch):
    monkeypatch.delenv(APP_DIR_ENV, raising=False)
    monkeypatch.delenv(WORKDIR_ENV, raising=False)
