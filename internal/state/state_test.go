package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreArchivesLegacyTmuxState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	workdir := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workdir, 0o700); err != nil {
		t.Fatal(err)
	}
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

	st, migration, err := NewStore(path, workdir).Ensure()
	if err != nil {
		t.Fatal(err)
	}

	if migration == nil {
		t.Fatal("expected migration")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "state.v1-tmux.json")); err != nil {
		t.Fatal(err)
	}
	if st.Version != Version || len(st.Agents) != 0 || len(st.Workdirs) != 1 || len(st.Folders) != 0 {
		t.Fatalf("state = %#v", st)
	}
}

func TestStoreMigratesTabsAndColumnsToWorkdirsFoldersAgents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	workdir := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workdir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := map[string]any{
		"version":       2,
		"active_tab_id": "b",
		"focus":         "codex",
		"tabs": []map[string]any{
			{"id": "a", "title": "Alpha", "column": "inbox", "status": "running", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"},
			{"id": "b", "title": "Beta", "column": "ship", "status": "ready", "created_at": "2026-01-01T00:01:00Z", "updated_at": "2026-01-01T00:01:00Z"},
		},
	}
	raw, _ := json.Marshal(old)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	st, migration, err := NewStore(path, workdir).Ensure()
	if err != nil {
		t.Fatal(err)
	}

	if migration == nil {
		t.Fatal("expected migration")
	}
	if st.ActiveAgentID != "b" || st.Focus != FocusCodex || st.NavOpen {
		t.Fatalf("selection/focus not preserved: %#v", st)
	}
	if len(st.Workdirs) != 1 || st.Workdirs[0].Path != workdir {
		t.Fatalf("workdirs = %#v", st.Workdirs)
	}
	if len(st.Folders) != 2 || len(st.Agents) != 2 {
		t.Fatalf("folders/agents = %#v %#v", st.Folders, st.Agents)
	}
	if st.Agents[1].FolderID == st.Agents[0].FolderID {
		t.Fatalf("columns should become distinct folders: %#v", st)
	}
}

func TestCloseAgentSelectsNextAgent(t *testing.T) {
	st := testState(t)
	st.ActiveAgentID = "b"

	st = CloseAgent(st, "b")

	if st.ActiveAgentID != "c" {
		t.Fatalf("ActiveAgentID = %q", st.ActiveAgentID)
	}
	if len(st.Agents) != 2 {
		t.Fatalf("agents = %#v", st.Agents)
	}
}

func TestCloseLastAgentInWorkdirStaysInCurrentWorkdir(t *testing.T) {
	st := testState(t)
	now := NowISO()
	otherWorkdir := Workdir{ID: "w2", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	otherGroup := Folder{ID: "g2", WorkdirID: otherWorkdir.ID, Path: "inbox", CreatedAt: now, UpdatedAt: now}
	st.Workdirs = append(st.Workdirs, otherWorkdir)
	st.Folders = append(st.Folders, otherGroup)
	st.Agents = []Agent{
		{ID: "only", WorkdirID: "w", FolderID: "f", Title: "Only", Status: StatusRunning, CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "other", WorkdirID: "w2", FolderID: "g2", Title: "Other", Status: StatusRunning, CreatedAt: "2026-01-01T00:01:00Z"},
	}
	st.ActiveAgentID = "only"

	st = CloseAgent(st, "only")

	if st.SelectedWorkdirID != "w" || st.ActiveAgentID != "" {
		t.Fatalf("state switched away from current workdir: %#v", st)
	}
}

func TestWorkdirCanHaveNoGroupsAndUngroupedAgents(t *testing.T) {
	st := Repair(Empty(), t.TempDir())
	if len(st.Workdirs) != 1 || len(st.Folders) != 0 {
		t.Fatalf("seeded state = %#v", st)
	}

	next, agent, err := AddAgent(st, "a", st.SelectedWorkdirID, "", "Codex", NowISO())
	if err != nil {
		t.Fatal(err)
	}
	if agent.FolderID != "" {
		t.Fatalf("new agent should be ungrouped: %#v", agent)
	}
	if ungrouped := UngroupedAgentsForWorkdir(next, st.SelectedWorkdirID); len(ungrouped) != 1 {
		t.Fatalf("ungrouped agents = %#v", ungrouped)
	}
	next = CloseAgent(next, agent.ID)
	if len(next.Agents) != 0 || next.SelectedWorkdirID != st.SelectedWorkdirID || next.SelectedFolderID != "" {
		t.Fatalf("closed ungrouped agent state = %#v", next)
	}
}

func TestWorkdirTitleOverrideCanBeSetAndCleared(t *testing.T) {
	st := testState(t)

	next, err := SetWorkdirTitle(st, "w", "  Trading Engine  ")
	if err != nil {
		t.Fatal(err)
	}
	if got := next.Workdirs[0].Title; got != "Trading Engine" {
		t.Fatalf("title override = %q", got)
	}

	next, err = SetWorkdirTitle(next, "w", " ")
	if err != nil {
		t.Fatal(err)
	}
	if got := next.Workdirs[0].Title; got != "" {
		t.Fatalf("blank title should clear override, got %q", got)
	}
}

func TestFolderValidationAndMoveAgent(t *testing.T) {
	st := testState(t)

	if _, _, err := AddFolder(st, "bad", st.SelectedWorkdirID, "bad/path", NowISO()); err == nil {
		t.Fatal("expected slash validation error")
	}
	next, folder, err := AddFolder(st, "ideas", st.SelectedWorkdirID, "ideas", NowISO())
	if err != nil {
		t.Fatal(err)
	}
	next, err = MoveAgent(next, "a", folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if agent := AgentByID(next, "a"); agent == nil || agent.FolderID != folder.ID {
		t.Fatalf("agent not moved: %#v", agent)
	}
	if _, err := DeleteFolder(next, folder.ID); err == nil {
		t.Fatal("expected non-empty folder delete error")
	}

	next, err = MoveAgent(next, "a", "")
	if err != nil {
		t.Fatal(err)
	}
	if agent := AgentByID(next, "a"); agent == nil || agent.FolderID != "" {
		t.Fatalf("agent not moved to top-level: %#v", agent)
	}
	next, err = DeleteFolder(next, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Folders) != 1 {
		t.Fatalf("group should be deleted: %#v", next.Folders)
	}
}

func testState(t *testing.T) State {
	t.Helper()
	dir := t.TempDir()
	workdirID := "w"
	folderID := "f"
	now := NowISO()
	return State{
		Version:           Version,
		ActiveAgentID:     "a",
		SelectedWorkdirID: workdirID,
		SelectedFolderID:  folderID,
		Focus:             FocusFolders,
		NavOpen:           true,
		Workdirs:          []Workdir{{ID: workdirID, Path: dir, CreatedAt: now, UpdatedAt: now}},
		Folders:           []Folder{{ID: folderID, WorkdirID: workdirID, Path: "inbox", CreatedAt: now, UpdatedAt: now}},
		Agents: []Agent{
			{ID: "a", WorkdirID: workdirID, FolderID: folderID, Title: "A", Status: StatusRunning, CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "b", WorkdirID: workdirID, FolderID: folderID, Title: "B", Status: StatusRunning, CreatedAt: "2026-01-01T00:01:00Z"},
			{ID: "c", WorkdirID: workdirID, FolderID: folderID, Title: "C", Status: StatusRunning, CreatedAt: "2026-01-01T00:02:00Z"},
		},
	}
}
