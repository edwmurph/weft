from __future__ import annotations

import json

from codux.state import AppState, StateStore, Tab, now_iso, prune_stale_tabs, state_path


def make_tab(tab_id: str, window_id: str = "@1", pane_id: str = "%1") -> Tab:
    created_at = now_iso()
    return Tab(
        id=tab_id,
        title=f"Tab {tab_id}",
        column="Backlog",
        tmux_session="codux",
        tmux_window_id=window_id,
        tmux_pane_id=pane_id,
        created_at=created_at,
        updated_at=created_at,
    )


def test_state_store_writes_and_reads_json(tmp_path):
    store = StateStore(state_path(tmp_path))
    tab = make_tab("abc")
    state = AppState(tabs=[tab], active_tab_id=tab.id, focus="nav")

    store.write(state)
    loaded = store.read()

    assert loaded == state
    raw = json.loads(state_path(tmp_path).read_text(encoding="utf-8"))
    assert raw["tabs"][0]["tmux_window_id"] == "@1"
    assert state_path(tmp_path).with_suffix(".lock").exists()


def test_state_store_update_is_persisted(tmp_path):
    store = StateStore(state_path(tmp_path))

    updated = store.update(lambda state: AppState(tabs=[make_tab("abc")], active_tab_id="abc"))

    assert updated.active_tab_id == "abc"
    assert store.read().active_tab_id == "abc"


def test_prune_stale_tabs_removes_missing_tab_and_repairs_active():
    first = make_tab("first", "@1", "%1")
    second = make_tab("second", "@2", "%2")
    state = AppState(tabs=[first, second], active_tab_id=second.id)

    repaired, changed = prune_stale_tabs(state, lambda tab: tab.id == "first")

    assert changed is True
    assert repaired.tabs == [first]
    assert repaired.active_tab_id == first.id


def test_prune_stale_tabs_keeps_valid_state_unchanged():
    first = make_tab("first", "@1", "%1")
    state = AppState(tabs=[first], active_tab_id=first.id)

    repaired, changed = prune_stale_tabs(state, lambda tab: True)

    assert changed is False
    assert repaired == state
