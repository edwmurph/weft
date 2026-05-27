from __future__ import annotations

from collections.abc import Callable
from dataclasses import replace

from codux.state import AppState
from codux.titles import CODEX_TITLE_TEMPLATE, normalize_codex_title


def state_with_live_codex_titles(
    state: AppState,
    pane_title: Callable[[str], str | None],
) -> AppState:
    changed = False
    tabs = []
    for tab in state.tabs:
        changes = {}
        if not tab.title.strip():
            changes["title"] = CODEX_TITLE_TEMPLATE
        live_title = normalize_codex_title(pane_title(tab.tmux_pane_id))
        if live_title is not None and live_title != tab.codex_title:
            changes["codex_title"] = live_title
        if changes:
            changed = True
            tabs.append(tab.with_updates(**changes))
        else:
            tabs.append(tab)
    if not changed:
        return state
    return replace(state, tabs=tabs)
