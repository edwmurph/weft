package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreArchivesLegacyTmuxState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := map[string]any{
		"active_tab_id": "abc",
		"focus":         "codex",
		"tabs": []map[string]any{{
			"id": "abc", "title": "{codex}", "column": "inbox",
			"tmux_window_id": "@1", "tmux_pane_id": "%1",
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		}},
	}
	raw, _ := json.Marshal(legacy)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	st, migration, err := NewStore(path).Ensure()
	if err != nil {
		t.Fatal(err)
	}

	if migration == nil {
		t.Fatal("expected migration")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "state.v1-tmux.json")); err != nil {
		t.Fatal(err)
	}
	if st.Version != Version || len(st.Tabs) != 0 {
		t.Fatalf("state = %#v", st)
	}
}

func TestCloseTabSelectsNextTab(t *testing.T) {
	st := State{
		Version: Version, ActiveTabID: "b", Focus: FocusCodex,
		Tabs: []Tab{{ID: "a"}, {ID: "b"}, {ID: "c"}},
	}

	st = CloseTab(st, "b")

	if st.ActiveTabID != "c" {
		t.Fatalf("ActiveTabID = %q", st.ActiveTabID)
	}
	if len(st.Tabs) != 2 {
		t.Fatalf("tabs = %#v", st.Tabs)
	}
}

func TestRepairColumnsMovesRemovedColumnsToFirstConfiguredColumn(t *testing.T) {
	st := State{Version: Version, Tabs: []Tab{{ID: "a", Column: "old"}}}

	st = RepairColumns(st, []string{"inbox", "ship"})

	if st.Tabs[0].Column != "inbox" {
		t.Fatalf("column = %q", st.Tabs[0].Column)
	}
}
