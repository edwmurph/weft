package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRejectsLegacyStateWithoutArchiving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 2,
  "active_tab_id": "abc",
  "focus": "codex",
  "tabs": [{
    "id": "abc",
    "title": "{codex}",
    "column": "inbox",
    "tmux_window_id": "@1",
    "tmux_pane_id": "%1",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-01T00:00:00Z"
  }]
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := NewStore(path, workspace).Ensure()
	if err == nil {
		t.Fatal("expected legacy state error")
	}
	for _, expected := range []string{"strict v4 state", "run `weft clear` to reset"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error missing %q: %v", expected, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "state.legacy.json")); !os.IsNotExist(err) {
		t.Fatalf("state.legacy.json should not be written, err=%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"active_tab_id"`) {
		t.Fatalf("legacy state file should be left intact:\n%s", raw)
	}
}

func TestStoreRejectsUnknownV4StateWithoutArchiving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 4,
  "focus": "agents",
  "nav_open": true,
  "workspaces": [{"id": "w", "path": "/tmp/project", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"}],
  "groups": [],
  "agents": [{"id": "a", "workspace_id": "w", "group_id": "", "title": "Alpha", "status": "running", "tmux_pane_id": "%1", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"}],
  "tabs": []
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := NewStore(path, workspace).Ensure()
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	for _, expected := range []string{`unknown field`, "run `weft clear` to reset"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error missing %q: %v", expected, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "state.legacy.json")); !os.IsNotExist(err) {
		t.Fatalf("state.legacy.json should not be written, err=%v", err)
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

func TestCloseLastAgentInWorkspaceStaysInCurrentWorkspace(t *testing.T) {
	st := testState(t)
	now := NowISO()
	otherWorkspace := Workspace{ID: "w2", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	otherGroup := Group{ID: "g2", WorkspaceID: otherWorkspace.ID, Path: "inbox", CreatedAt: now, UpdatedAt: now}
	st.Workspaces = append(st.Workspaces, otherWorkspace)
	st.Groups = append(st.Groups, otherGroup)
	st.Agents = []Agent{
		{ID: "only", WorkspaceID: "w", GroupID: "f", Title: "Only", Status: StatusRunning, CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "other", WorkspaceID: "w2", GroupID: "g2", Title: "Other", Status: StatusRunning, CreatedAt: "2026-01-01T00:01:00Z"},
	}
	st.ActiveAgentID = "only"

	st = CloseAgent(st, "only")

	if st.SelectedWorkspaceID != "w" || st.ActiveAgentID != "" {
		t.Fatalf("state switched away from current workspace: %#v", st)
	}
}

func TestWorkspaceCanHaveNoGroupsAndUngroupedAgents(t *testing.T) {
	st := stateWithWorkspace(t)
	if len(st.Workspaces) != 1 || len(st.Groups) != 0 {
		t.Fatalf("workspace state = %#v", st)
	}

	next, agent, err := AddAgent(st, "a", st.SelectedWorkspaceID, "", "Codex", NowISO())
	if err != nil {
		t.Fatal(err)
	}
	if agent.GroupID != "" {
		t.Fatalf("new agent should be ungrouped: %#v", agent)
	}
	if ungrouped := UngroupedAgentsForWorkspace(next, st.SelectedWorkspaceID); len(ungrouped) != 1 {
		t.Fatalf("ungrouped agents = %#v", ungrouped)
	}
	next = CloseAgent(next, agent.ID)
	if len(next.Agents) != 0 || next.SelectedWorkspaceID != st.SelectedWorkspaceID || next.SelectedGroupID != "" {
		t.Fatalf("closed ungrouped agent state = %#v", next)
	}
}

func TestWorkspaceTitleOverrideCanBeSetAndCleared(t *testing.T) {
	st := testState(t)

	next, err := SetWorkspaceTitle(st, "w", "  Trading Engine  ")
	if err != nil {
		t.Fatal(err)
	}
	if got := next.Workspaces[0].Title; got != "Trading Engine" {
		t.Fatalf("title override = %q", got)
	}

	next, err = SetWorkspaceTitle(next, "w", " ")
	if err != nil {
		t.Fatal(err)
	}
	if got := next.Workspaces[0].Title; got != "" {
		t.Fatalf("blank title should clear override, got %q", got)
	}
}

func TestAddAgentDefaultsTitleToCodexTemplate(t *testing.T) {
	st := stateWithWorkspace(t)

	_, agent, err := AddAgent(st, "a", st.SelectedWorkspaceID, "", "", NowISO())
	if err != nil {
		t.Fatal(err)
	}
	if agent.Title != DefaultAgentTitle {
		t.Fatalf("agent title = %q", agent.Title)
	}
	if agent.TypeID != DefaultAgentTypeID {
		t.Fatalf("agent type = %q", agent.TypeID)
	}
}

func TestAddAgentWithTypeStoresTaskType(t *testing.T) {
	st := stateWithWorkspace(t)
	now := NowISO()

	_, agent, err := AddAgentWithType(st, "", st.SelectedWorkspaceID, "", "shell", "Shell", now)
	if err != nil {
		t.Fatal(err)
	}
	if agent.TypeID != "shell" || agent.Title != "Shell" {
		t.Fatalf("agent = %#v", agent)
	}
	if want := StableID("agent", st.SelectedWorkspaceID, "", "shell", now, "Shell"); agent.ID != want {
		t.Fatalf("typed agent id = %q, want %q", agent.ID, want)
	}
}

func TestRepairDefaultsLegacyAgentsToCodexType(t *testing.T) {
	st := testState(t)
	st.Agents[0].TypeID = ""

	repaired := Repair(st, t.TempDir())

	if got := repaired.Agents[0].TypeID; got != DefaultAgentTypeID {
		t.Fatalf("repaired type = %q", got)
	}
	if got := AgentTypeID(Agent{ID: "legacy"}); got != DefaultAgentTypeID {
		t.Fatalf("legacy helper type = %q", got)
	}
}

func TestRepairAllowsEmptyWorkspaces(t *testing.T) {
	st := Repair(Empty(), t.TempDir())

	if len(st.Workspaces) != 0 || st.SelectedWorkspaceID != "" || st.Focus != FocusWorkspaces || !st.NavOpen {
		t.Fatalf("empty repaired state = %#v", st)
	}
}

func TestRemoveLastWorkspaceLeavesEmptyState(t *testing.T) {
	st := stateWithWorkspace(t)

	next, removed, err := RemoveWorkspace(st, st.SelectedWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}

	if len(removed) != 0 {
		t.Fatalf("removed agents = %#v", removed)
	}
	if len(next.Workspaces) != 0 || next.SelectedWorkspaceID != "" || next.Focus != FocusWorkspaces || !next.NavOpen {
		t.Fatalf("state should allow no workspaces: %#v", next)
	}
}

func TestGroupValidationAndMoveAgent(t *testing.T) {
	st := testState(t)

	if _, _, err := AddGroup(st, "bad", st.SelectedWorkspaceID, "bad/path", NowISO()); err == nil {
		t.Fatal("expected slash validation error")
	}
	next, group, err := AddGroup(st, "ideas", st.SelectedWorkspaceID, "ideas", NowISO())
	if err != nil {
		t.Fatal(err)
	}
	next, err = MoveAgent(next, "a", group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if agent := AgentByID(next, "a"); agent == nil || agent.GroupID != group.ID {
		t.Fatalf("agent not moved: %#v", agent)
	}
	if _, err := DeleteGroup(next, group.ID); err == nil {
		t.Fatal("expected non-empty group delete error")
	}

	next, err = MoveAgent(next, "a", "")
	if err != nil {
		t.Fatal(err)
	}
	if agent := AgentByID(next, "a"); agent == nil || agent.GroupID != "" {
		t.Fatalf("agent not moved to top-level: %#v", agent)
	}
	next, err = DeleteGroup(next, group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Groups) != 1 {
		t.Fatalf("group should be deleted: %#v", next.Groups)
	}
}

func TestEditGroupPersistsSilentAndPath(t *testing.T) {
	st := stateWithWorkspace(t)
	now := NowISO()
	st.Groups = []Group{{ID: "g", WorkspaceID: "w", Path: "release", Silent: false, CreatedAt: now, UpdatedAt: now}}

	next, err := EditGroup(st, "g", "release", true)
	if err != nil {
		t.Fatal(err)
	}
	group := GroupByID(next, "g")
	if group == nil || group.Path != "release" || !group.Silent {
		t.Fatalf("group not updated: %#v", group)
	}

	next, err = EditGroup(next, "g", "shipping", true)
	if err != nil {
		t.Fatal(err)
	}
	group = GroupByID(next, "g")
	if group == nil || group.Path != "shipping" || !group.Silent {
		t.Fatalf("group not renamed+silent: %#v", group)
	}

	next, err = RenameGroup(next, "g", "shipping")
	if err != nil {
		t.Fatal(err)
	}
	group = GroupByID(next, "g")
	if group == nil || !group.Silent {
		t.Fatalf("rename should preserve silent: %#v", group)
	}
}

func TestRepairDefaultsGroupSilentFalse(t *testing.T) {
	now := NowISO()
	st := State{
		Version:    Version,
		Focus:      FocusAgents,
		NavOpen:    true,
		Workspaces: []Workspace{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Groups:     []Group{{ID: "g", WorkspaceID: "w", Path: "release", CreatedAt: now, UpdatedAt: now}},
		Agents:     []Agent{},
	}
	repaired := Repair(st, "/tmp/project")
	group := GroupByID(repaired, "g")
	if group == nil || group.Silent {
		t.Fatalf("silent should default false: %#v", group)
	}
}

func TestAddGroupWithSilentSetsSilent(t *testing.T) {
	st := testState(t)
	next, group, err := AddGroupWithSilent(st, "release", st.SelectedWorkspaceID, "release", NowISO(), true)
	if err != nil {
		t.Fatal(err)
	}
	if group.ID != "release" || !group.Silent {
		t.Fatalf("group = %#v", group)
	}
	if got := GroupByID(next, "release"); got == nil || !got.Silent {
		t.Fatalf("silent not persisted: %#v", got)
	}
}

func TestReorderAgentStaysWithinGroupOrUngroupedArea(t *testing.T) {
	st := stateWithWorkspace(t)
	now := NowISO()
	st.Groups = []Group{
		{ID: "release", WorkspaceID: "w", Path: "release", CreatedAt: now, UpdatedAt: now},
		{ID: "review", WorkspaceID: "w", Path: "review", CreatedAt: now, UpdatedAt: now},
	}
	st.Agents = []Agent{
		{ID: "top-a", WorkspaceID: "w", GroupID: "", Title: "Top A", Status: StatusRunning, CreatedAt: "2026-01-01T00:03:00Z", UpdatedAt: now},
		{ID: "release-a", WorkspaceID: "w", GroupID: "release", Title: "Release A", Status: StatusRunning, CreatedAt: "2026-01-01T00:02:00Z", UpdatedAt: now},
		{ID: "review-a", WorkspaceID: "w", GroupID: "review", Title: "Review A", Status: StatusRunning, CreatedAt: "2026-01-01T00:01:00Z", UpdatedAt: now},
		{ID: "release-b", WorkspaceID: "w", GroupID: "release", Title: "Release B", Status: StatusRunning, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: now},
		{ID: "top-b", WorkspaceID: "w", GroupID: "", Title: "Top B", Status: StatusRunning, CreatedAt: "2026-01-01T00:04:00Z", UpdatedAt: now},
	}

	next, moved, err := ReorderAgent(st, "release-b", -1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected grouped agent to move")
	}
	if got, want := agentIDs(AgentsForGroup(next, "release")), []string{"release-b", "release-a"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("release group order = %#v, want %#v", got, want)
	}
	if got, want := agentIDs(UngroupedAgentsForWorkspace(next, "w")), []string{"top-a", "top-b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ungrouped order changed = %#v, want %#v", got, want)
	}
	if next.ActiveAgentID != "release-b" || next.SelectedGroupID != "release" {
		t.Fatalf("selection did not follow moved grouped agent: %#v", next)
	}

	next, moved, err = ReorderAgent(next, "top-b", -1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected ungrouped agent to move")
	}
	if got, want := agentIDs(UngroupedAgentsForWorkspace(next, "w")), []string{"top-b", "top-a"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ungrouped order = %#v, want %#v", got, want)
	}

	next, moved, err = ReorderAgent(next, "top-b", -1)
	if err != nil {
		t.Fatal(err)
	}
	if moved {
		t.Fatal("first ungrouped agent should not move above its area")
	}
}

func testState(t *testing.T) State {
	t.Helper()
	dir := t.TempDir()
	workspaceID := "w"
	groupID := "f"
	now := NowISO()
	return State{
		Version:             Version,
		ActiveAgentID:       "a",
		SelectedWorkspaceID: workspaceID,
		SelectedGroupID:     groupID,
		Focus:               FocusAgents,
		NavOpen:             true,
		Workspaces:          []Workspace{{ID: workspaceID, Path: dir, CreatedAt: now, UpdatedAt: now}},
		Groups:              []Group{{ID: groupID, WorkspaceID: workspaceID, Path: "inbox", CreatedAt: now, UpdatedAt: now}},
		Agents: []Agent{
			{ID: "a", WorkspaceID: workspaceID, GroupID: groupID, Title: "A", Status: StatusRunning, CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "b", WorkspaceID: workspaceID, GroupID: groupID, Title: "B", Status: StatusRunning, CreatedAt: "2026-01-01T00:01:00Z"},
			{ID: "c", WorkspaceID: workspaceID, GroupID: groupID, Title: "C", Status: StatusRunning, CreatedAt: "2026-01-01T00:02:00Z"},
		},
	}
}

func stateWithWorkspace(t *testing.T) State {
	t.Helper()
	st, _, err := AddWorkspace(Empty(), "w", t.TempDir(), NowISO())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func agentIDs(agents []Agent) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}
	return ids
}
