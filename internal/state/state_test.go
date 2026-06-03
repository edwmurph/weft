package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexTaskTypeID = "codex"

func TestStoreRejectsUnknownV6StateWithoutArchiving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 6,
  "focus": "tasks",
  "nav_open": true,
  "workspaces": [],
  "groups": [],
  "tasks": [],
  "collapsed_group_ids": [],
  "unexpected": true
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := NewStore(path).Ensure()
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	for _, expected := range []string{`unknown field "unexpected"`, "run `weft clear` to reset"} {
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
	if !strings.Contains(string(raw), `"unexpected"`) {
		t.Fatalf("unsupported state file should be left intact:\n%s", raw)
	}
}

func TestStoreRejectsUnsupportedStateVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 4,
  "focus": "tasks",
  "nav_open": true,
  "workspaces": [],
  "groups": [],
  "tasks": [],
  "collapsed_group_ids": []
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := NewStore(path).Ensure()
	if err == nil {
		t.Fatal("expected version error")
	}
	for _, expected := range []string{"unsupported state version 4", "run `weft clear` to reset"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error missing %q: %v", expected, err)
		}
	}
}

func TestStoreRejectsUnknownFocusValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 6,
  "focus": "sideways",
  "nav_open": true,
  "workspaces": [],
  "groups": [],
  "tasks": [],
  "collapsed_group_ids": []
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := NewStore(path).Ensure()
	if err == nil {
		t.Fatal("expected focus error")
	}
	for _, expected := range []string{`unsupported focus value "sideways"`, "run `weft clear` to reset"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error missing %q: %v", expected, err)
		}
	}
}

func TestStoreRejectsTaskMissingTypeID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 6,
  "focus": "tasks",
  "nav_open": true,
  "workspaces": [{"id": "w", "path": "/tmp/project", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"}],
  "groups": [],
  "tasks": [{"id": "a", "workspace_id": "w", "group_id": "", "title": "Alpha", "status": "running", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"}],
  "collapsed_group_ids": []
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := NewStore(path).Ensure()
	if err == nil {
		t.Fatal("expected task type error")
	}
	for _, expected := range []string{`tasks[0].type_id is required`, "run `weft clear` to reset"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error missing %q: %v", expected, err)
		}
	}
}

func TestStoreReadsStrictV6TaskState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 6,
  "active_task_id": "a",
  "selected_task_id": "a",
  "selected_workspace_id": "w",
  "focus": "console",
  "nav_open": false,
  "workspaces": [{"id": "w", "path": "/tmp/project", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"}],
  "groups": [],
  "tasks": [{"id": "a", "workspace_id": "w", "group_id": "", "type_id": "codex", "title": "Alpha", "status": "running", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"}],
  "collapsed_group_ids": []
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := NewStore(path).Ensure()
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != Version || st.ActiveTaskID != "a" || st.SelectedTaskID != "a" || st.Focus != FocusConsole || len(st.Tasks) != 1 {
		t.Fatalf("strict v6 state = %#v", st)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type_id": "codex"`) {
		t.Fatalf("strict v6 state should preserve task type:\n%s", raw)
	}
}

func TestCloseTaskSelectsNextTask(t *testing.T) {
	st := testState(t)
	st.ActiveTaskID = "b"

	st = CloseTask(st, "b")

	if st.ActiveTaskID != "c" {
		t.Fatalf("ActiveTaskID = %q", st.ActiveTaskID)
	}
	if len(st.Tasks) != 2 {
		t.Fatalf("tasks = %#v", st.Tasks)
	}
}

func TestCloseLastTaskInWorkspaceStaysInCurrentWorkspace(t *testing.T) {
	st := testState(t)
	now := NowISO()
	otherWorkspace := Workspace{ID: "w2", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	otherGroup := Group{ID: "g2", WorkspaceID: otherWorkspace.ID, Path: "inbox", CreatedAt: now, UpdatedAt: now}
	st.Workspaces = append(st.Workspaces, otherWorkspace)
	st.Groups = append(st.Groups, otherGroup)
	st.Tasks = []Task{
		{ID: "only", WorkspaceID: "w", GroupID: "f", TypeID: codexTaskTypeID, Title: "Only", Status: StatusRunning, CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "other", WorkspaceID: "w2", GroupID: "g2", TypeID: codexTaskTypeID, Title: "Other", Status: StatusRunning, CreatedAt: "2026-01-01T00:01:00Z"},
	}
	st.ActiveTaskID = "only"

	st = CloseTask(st, "only")

	if st.SelectedWorkspaceID != "w" || st.ActiveTaskID != "" {
		t.Fatalf("state switched away from current workspace: %#v", st)
	}
}

func TestWorkspaceCanHaveNoGroupsAndUngroupedTasks(t *testing.T) {
	st := stateWithWorkspace(t)
	if len(st.Workspaces) != 1 || len(st.Groups) != 0 {
		t.Fatalf("workspace state = %#v", st)
	}

	next, task, err := AddTaskWithType(st, "a", st.SelectedWorkspaceID, "", codexTaskTypeID, "Codex", NowISO())
	if err != nil {
		t.Fatal(err)
	}
	if task.GroupID != "" {
		t.Fatalf("new task should be ungrouped: %#v", task)
	}
	if ungrouped := UngroupedTasksForWorkspace(next, st.SelectedWorkspaceID); len(ungrouped) != 1 {
		t.Fatalf("ungrouped tasks = %#v", ungrouped)
	}
	next = CloseTask(next, task.ID)
	if len(next.Tasks) != 0 || next.SelectedWorkspaceID != st.SelectedWorkspaceID || next.SelectedGroupID != "" {
		t.Fatalf("closed ungrouped task state = %#v", next)
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

func TestAddTaskWithTypeRequiresTitle(t *testing.T) {
	st := stateWithWorkspace(t)

	_, task, err := AddTaskWithType(st, "a", st.SelectedWorkspaceID, "", codexTaskTypeID, "", NowISO())
	if err == nil {
		t.Fatal("expected title error")
	}
	if err.Error() != "task title is required" {
		t.Fatalf("error = %v", err)
	}
	if task.ID != "" {
		t.Fatalf("task should be empty on error: %#v", task)
	}
}

func TestAddTaskWithTypeStoresTaskType(t *testing.T) {
	st := stateWithWorkspace(t)
	now := NowISO()

	_, task, err := AddTaskWithType(st, "shell-task", st.SelectedWorkspaceID, "", "shell", "Shell", now)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "shell-task" || task.TypeID != "shell" || task.Title != "Shell" {
		t.Fatalf("task = %#v", task)
	}
}

func TestAddTaskWithTypeAndSilentSetsSilent(t *testing.T) {
	st := stateWithWorkspace(t)
	now := NowISO()

	next, task, err := AddTaskWithTypeAndSilent(st, "silent-task", st.SelectedWorkspaceID, "", codexTaskTypeID, "Codex", now, true)
	if err != nil {
		t.Fatal(err)
	}
	if !task.Silent {
		t.Fatalf("task should be silent: %#v", task)
	}
	if got := TaskByID(next, "silent-task"); got == nil || !got.Silent {
		t.Fatalf("persisted task should be silent: %#v", next.Tasks)
	}
}

func TestAddTaskWithTypeRequiresTaskType(t *testing.T) {
	st := stateWithWorkspace(t)

	_, task, err := AddTaskWithType(st, "a", st.SelectedWorkspaceID, "", " ", "Shell", NowISO())
	if err == nil {
		t.Fatal("expected task type error")
	}
	if err.Error() != "task type is required" {
		t.Fatalf("error = %v", err)
	}
	if task.ID != "" {
		t.Fatalf("task should be empty on error: %#v", task)
	}
}

func TestEditTaskPersistsSilentAndRenameTaskPreservesSilent(t *testing.T) {
	st := stateWithWorkspace(t)
	now := NowISO()
	st.Tasks = []Task{{ID: "a", WorkspaceID: st.SelectedWorkspaceID, TypeID: codexTaskTypeID, Title: "Codex", Status: StatusReady, Silent: false, CreatedAt: now, UpdatedAt: now}}

	next, err := EditTask(st, "a", "Codex", true)
	if err != nil {
		t.Fatal(err)
	}
	if task := TaskByID(next, "a"); task == nil || task.Title != "Codex" || !task.Silent {
		t.Fatalf("edited task = %#v", task)
	}

	next, err = RenameTask(next, "a", "Renamed")
	if err != nil {
		t.Fatal(err)
	}
	if task := TaskByID(next, "a"); task == nil || task.Title != "Renamed" || !task.Silent {
		t.Fatalf("renamed task should preserve silence: %#v", task)
	}

	next, err = EditTask(next, "a", "Unsilenced", false)
	if err != nil {
		t.Fatal(err)
	}
	if task := TaskByID(next, "a"); task == nil || task.Title != "Unsilenced" || task.Silent {
		t.Fatalf("unsilenced task = %#v", task)
	}
}

func TestAddWorkspaceRequiresExplicitIDAndTimestamp(t *testing.T) {
	st := Empty()
	workspace := t.TempDir()

	_, created, err := AddWorkspace(st, "", workspace, NowISO())
	if err == nil || err.Error() != "workspace id is required" {
		t.Fatalf("expected workspace id error, got workspace=%#v err=%v", created, err)
	}
	_, created, err = AddWorkspace(st, "w", workspace, "")
	if err == nil || err.Error() != "workspace timestamp is required" {
		t.Fatalf("expected workspace timestamp error, got workspace=%#v err=%v", created, err)
	}
}

func TestAddGroupWithSilentRequiresExplicitIDAndTimestamp(t *testing.T) {
	st := stateWithWorkspace(t)

	_, group, err := AddGroupWithSilent(st, "", st.SelectedWorkspaceID, "release", NowISO(), false)
	if err == nil || err.Error() != "group id is required" {
		t.Fatalf("expected group id error, got group=%#v err=%v", group, err)
	}
	_, group, err = AddGroupWithSilent(st, "release", st.SelectedWorkspaceID, "release", "", false)
	if err == nil || err.Error() != "group timestamp is required" {
		t.Fatalf("expected group timestamp error, got group=%#v err=%v", group, err)
	}
}

func TestAddTaskWithTypeRequiresExplicitIDAndTimestamp(t *testing.T) {
	st := stateWithWorkspace(t)

	_, task, err := AddTaskWithType(st, "", st.SelectedWorkspaceID, "", codexTaskTypeID, "Codex", NowISO())
	if err == nil || err.Error() != "task id is required" {
		t.Fatalf("expected task id error, got task=%#v err=%v", task, err)
	}
	_, task, err = AddTaskWithType(st, "a", st.SelectedWorkspaceID, "", codexTaskTypeID, "Codex", "")
	if err == nil || err.Error() != "task timestamp is required" {
		t.Fatalf("expected task timestamp error, got task=%#v err=%v", task, err)
	}
}

func TestEmptyStateIsStrictCurrentState(t *testing.T) {
	st := Empty()

	if len(st.Workspaces) != 0 || st.SelectedWorkspaceID != "" || st.Focus != FocusWorkspaces || !st.NavOpen {
		t.Fatalf("empty state = %#v", st)
	}
	if err := ValidateCurrent(st); err != nil {
		t.Fatalf("empty state should be strict current state: %v", err)
	}
}

func TestRemoveLastWorkspaceLeavesEmptyState(t *testing.T) {
	st := stateWithWorkspace(t)

	next, removed, err := RemoveWorkspace(st, st.SelectedWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}

	if len(removed) != 0 {
		t.Fatalf("removed tasks = %#v", removed)
	}
	if len(next.Workspaces) != 0 || next.SelectedWorkspaceID != "" || next.Focus != FocusWorkspaces || !next.NavOpen {
		t.Fatalf("state should allow no workspaces: %#v", next)
	}
	if err := ValidateCurrent(next); err != nil {
		t.Fatalf("removed last workspace state should be strict current state: %v", err)
	}
}

func TestRemoveWorkspaceMaintainsStrictCurrentState(t *testing.T) {
	st := testState(t)
	now := NowISO()
	otherWorkspace := Workspace{ID: "w2", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	otherGroup := Group{ID: "g2", WorkspaceID: otherWorkspace.ID, Path: "review", CreatedAt: now, UpdatedAt: now}
	otherTask := Task{ID: "other", WorkspaceID: otherWorkspace.ID, GroupID: otherGroup.ID, TypeID: codexTaskTypeID, Title: "Other", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}
	st.Workspaces = append(st.Workspaces, otherWorkspace)
	st.Groups = append(st.Groups, otherGroup)
	st.Tasks = append(st.Tasks, otherTask)
	st.ActiveTaskID = otherTask.ID
	st.SelectedWorkspaceID = otherWorkspace.ID
	st.SelectedGroupID = otherGroup.ID
	st.SelectedTaskID = otherTask.ID
	st.CollapsedGroupIDs = []string{"f", otherGroup.ID}

	next, removed, err := RemoveWorkspace(st, otherWorkspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := taskIDs(removed), []string{otherTask.ID}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("removed tasks = %#v, want %#v", got, want)
	}
	if WorkspaceByID(next, otherWorkspace.ID) != nil || GroupByID(next, otherGroup.ID) != nil || TaskByID(next, otherTask.ID) != nil {
		t.Fatalf("removed workspace contents remain: %#v", next)
	}
	if next.ActiveTaskID != "" || next.SelectedTaskID != "" || next.SelectedWorkspaceID != "w" || next.SelectedGroupID != "" {
		t.Fatalf("selection after workspace removal = %#v", next)
	}
	if IsGroupCollapsed(next, otherGroup.ID) || !IsGroupCollapsed(next, "f") {
		t.Fatalf("collapsed groups not pruned correctly: %#v", next.CollapsedGroupIDs)
	}
	if err := ValidateCurrent(next); err != nil {
		t.Fatalf("removed workspace state should be strict current state: %v", err)
	}
}

func TestGroupValidationAndMoveTask(t *testing.T) {
	st := testState(t)

	if _, _, err := AddGroupWithSilent(st, "bad", st.SelectedWorkspaceID, "bad/path", NowISO(), false); err == nil {
		t.Fatal("expected slash validation error")
	}
	next, group, err := AddGroupWithSilent(st, "ideas", st.SelectedWorkspaceID, "ideas", NowISO(), false)
	if err != nil {
		t.Fatal(err)
	}
	next, err = MoveTask(next, "a", group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task := TaskByID(next, "a"); task == nil || task.GroupID != group.ID {
		t.Fatalf("task not moved: %#v", task)
	}
	if _, err := DeleteGroup(next, group.ID); err == nil {
		t.Fatal("expected non-empty group delete error")
	}

	next, err = MoveTask(next, "a", "")
	if err != nil {
		t.Fatal(err)
	}
	if task := TaskByID(next, "a"); task == nil || task.GroupID != "" {
		t.Fatalf("task not moved to top-level: %#v", task)
	}
	next, err = DeleteGroup(next, group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Groups) != 1 {
		t.Fatalf("group should be deleted: %#v", next.Groups)
	}
	if err := ValidateCurrent(next); err != nil {
		t.Fatalf("deleted group state should be strict current state: %v", err)
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

	next, err = EditGroup(next, "g", "shipping", false)
	if err != nil {
		t.Fatal(err)
	}
	group = GroupByID(next, "g")
	if group == nil || group.Silent {
		t.Fatalf("edit should store explicit silent value: %#v", group)
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

func TestReorderTaskMovesWithinAndAcrossAdjacentAreas(t *testing.T) {
	st := reorderTaskTestState(t)

	next, moved, err := ReorderTask(st, "release-b", -1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected grouped task to move")
	}
	if got, want := taskIDs(TasksForGroup(next, "release")), []string{"release-b", "release-a"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("release group order = %#v, want %#v", got, want)
	}
	if got, want := taskIDs(UngroupedTasksForWorkspace(next, "w")), []string{"top-a", "top-b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ungrouped order changed = %#v, want %#v", got, want)
	}
	if next.ActiveTaskID != "release-b" || next.SelectedGroupID != "release" {
		t.Fatalf("selection did not follow moved grouped task: %#v", next)
	}

	next, moved, err = ReorderTask(next, "top-b", -1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected ungrouped task to move")
	}
	if got, want := taskIDs(UngroupedTasksForWorkspace(next, "w")), []string{"top-b", "top-a"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ungrouped order = %#v, want %#v", got, want)
	}

	next, moved, err = ReorderTask(next, "top-b", -1)
	if err != nil {
		t.Fatal(err)
	}
	if moved {
		t.Fatal("first ungrouped task should not move up")
	}

	st = reorderTaskTestState(t)
	next, moved, err = ReorderTask(st, "top-b", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected last ungrouped task to move into first group")
	}
	if got, want := taskIDs(UngroupedTasksForWorkspace(next, "w")), []string{"top-a"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ungrouped order after crossing down = %#v, want %#v", got, want)
	}
	if got, want := taskIDs(TasksForGroup(next, "release")), []string{"top-b", "release-a", "release-b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("release order after top-level crossing down = %#v, want %#v", got, want)
	}
	if task := TaskByID(next, "top-b"); task == nil || task.GroupID != "release" {
		t.Fatalf("task did not move into release: %#v", task)
	}
	if next.ActiveTaskID != "top-b" || next.SelectedGroupID != "release" {
		t.Fatalf("selection did not follow task into group: %#v", next)
	}

	next, moved, err = ReorderTask(next, "top-b", -1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected first grouped task to move back to top-level")
	}
	if got, want := taskIDs(UngroupedTasksForWorkspace(next, "w")), []string{"top-a", "top-b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ungrouped order after crossing up = %#v, want %#v", got, want)
	}
	if task := TaskByID(next, "top-b"); task == nil || task.GroupID != "" {
		t.Fatalf("task did not move back to top-level: %#v", task)
	}
	if next.SelectedGroupID != "" {
		t.Fatalf("selection should follow top-level task: %#v", next)
	}

	st = reorderTaskTestState(t)
	next, moved, err = ReorderTask(st, "release-b", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected last release task to move into next group")
	}
	if got, want := taskIDs(TasksForGroup(next, "release")), []string{"release-a"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("release order after crossing into next group = %#v, want %#v", got, want)
	}
	if got, want := taskIDs(TasksForGroup(next, "review")), []string{"release-b", "review-a"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("review order after crossing into next group = %#v, want %#v", got, want)
	}

	next, moved, err = ReorderTask(next, "release-b", -1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected first review task to move back into previous group")
	}
	if got, want := taskIDs(TasksForGroup(next, "release")), []string{"release-a", "release-b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("release order after crossing back up = %#v, want %#v", got, want)
	}
}

func reorderTaskTestState(t *testing.T) State {
	t.Helper()
	st := stateWithWorkspace(t)
	now := NowISO()
	st.Groups = []Group{
		{ID: "release", WorkspaceID: "w", Path: "release", CreatedAt: now, UpdatedAt: now},
		{ID: "review", WorkspaceID: "w", Path: "review", CreatedAt: now, UpdatedAt: now},
	}
	st.Tasks = []Task{
		{ID: "top-a", WorkspaceID: "w", GroupID: "", TypeID: codexTaskTypeID, Title: "Top A", Status: StatusRunning, CreatedAt: "2026-01-01T00:03:00Z", UpdatedAt: now},
		{ID: "release-a", WorkspaceID: "w", GroupID: "release", TypeID: codexTaskTypeID, Title: "Release A", Status: StatusRunning, CreatedAt: "2026-01-01T00:02:00Z", UpdatedAt: now},
		{ID: "review-a", WorkspaceID: "w", GroupID: "review", TypeID: codexTaskTypeID, Title: "Review A", Status: StatusRunning, CreatedAt: "2026-01-01T00:01:00Z", UpdatedAt: now},
		{ID: "release-b", WorkspaceID: "w", GroupID: "release", TypeID: codexTaskTypeID, Title: "Release B", Status: StatusRunning, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: now},
		{ID: "top-b", WorkspaceID: "w", GroupID: "", TypeID: codexTaskTypeID, Title: "Top B", Status: StatusRunning, CreatedAt: "2026-01-01T00:04:00Z", UpdatedAt: now},
	}
	return st
}

func TestReorderWorkspacePreservesSelectionAndContents(t *testing.T) {
	st := stateWithWorkspace(t)
	now := NowISO()
	otherPath := t.TempDir()
	st.Workspaces = append(st.Workspaces,
		Workspace{ID: "w2", Path: otherPath, Title: "Other", CreatedAt: now, UpdatedAt: now},
		Workspace{ID: "w3", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now},
	)
	st.Groups = append(st.Groups, Group{ID: "other-group", WorkspaceID: "w2", Path: "other", CreatedAt: now, UpdatedAt: now})
	st.Tasks = append(st.Tasks, Task{ID: "other-task", WorkspaceID: "w2", GroupID: "other-group", TypeID: codexTaskTypeID, Title: "Other", Status: StatusRunning, CreatedAt: now, UpdatedAt: now})
	st.SelectedWorkspaceID = "w2"
	st.SelectedGroupID = "other-group"
	st.SelectedTaskID = "other-task"
	st.ActiveTaskID = "a"
	st.Focus = FocusWorkspaces

	next, moved, err := ReorderWorkspace(st, "w2", -1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected workspace to move")
	}
	if got, want := workspaceIDs(next.Workspaces), []string{"w2", "w", "w3"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("workspace order = %#v, want %#v", got, want)
	}
	if next.SelectedWorkspaceID != "w2" || next.SelectedGroupID != "other-group" || next.SelectedTaskID != "other-task" || next.ActiveTaskID != "a" {
		t.Fatalf("selection did not stay on moved workspace without changing active task: %#v", next)
	}
	if workspace := WorkspaceByID(next, "w2"); workspace == nil || workspace.Path != otherPath || workspace.Title != "Other" {
		t.Fatalf("workspace contents changed: %#v", workspace)
	}
	if task := TaskByID(next, "other-task"); task == nil || task.WorkspaceID != "w2" || task.GroupID != "other-group" {
		t.Fatalf("workspace task changed during reorder: %#v", task)
	}

	next, moved, err = ReorderWorkspace(next, "w2", -1)
	if err != nil {
		t.Fatal(err)
	}
	if moved {
		t.Fatal("first workspace should not move up")
	}

	next, moved, err = ReorderWorkspace(next, "w2", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected workspace to move down")
	}
	if got, want := workspaceIDs(next.Workspaces), []string{"w", "w2", "w3"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("workspace order after move down = %#v, want %#v", got, want)
	}
}

func TestSelectWorkspaceRestoresActiveTaskInWorkspace(t *testing.T) {
	st := stateWithWorkspace(t)
	now := NowISO()
	st.Workspaces = append(st.Workspaces, Workspace{ID: "w2", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now})
	st.Groups = append(st.Groups,
		Group{ID: "first", WorkspaceID: "w2", Path: "first", CreatedAt: now, UpdatedAt: now},
		Group{ID: "active-group", WorkspaceID: "w2", Path: "active", CreatedAt: now, UpdatedAt: now},
	)
	st.Tasks = append(st.Tasks, Task{ID: "active", WorkspaceID: "w2", GroupID: "active-group", TypeID: codexTaskTypeID, Title: "Active", Status: StatusRunning, CreatedAt: now, UpdatedAt: now})
	st.ActiveTaskID = "active"

	next := SelectWorkspace(st, "w2")
	if next.SelectedTaskID != "active" || next.SelectedGroupID != "active-group" {
		t.Fatalf("workspace selection should restore active task, got selected task=%q group=%q", next.SelectedTaskID, next.SelectedGroupID)
	}
}

func TestReorderGroupStaysWithinWorkspaceAndPreservesSelection(t *testing.T) {
	st := stateWithWorkspace(t)
	now := NowISO()
	st.ActiveTaskID = "active"
	st.SelectedTaskID = "active"
	st.SelectedGroupID = "review"
	st.CollapsedGroupIDs = []string{"review"}
	st.Workspaces = append(st.Workspaces, Workspace{ID: "w2", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now})
	st.Groups = []Group{
		{ID: "release", WorkspaceID: "w", Path: "release", CreatedAt: now, UpdatedAt: now},
		{ID: "other-a", WorkspaceID: "w2", Path: "other a", CreatedAt: now, UpdatedAt: now},
		{ID: "review", WorkspaceID: "w", Path: "review", Silent: true, CreatedAt: now, UpdatedAt: now},
		{ID: "other-b", WorkspaceID: "w2", Path: "other b", CreatedAt: now, UpdatedAt: now},
		{ID: "qa", WorkspaceID: "w", Path: "qa", CreatedAt: now, UpdatedAt: now},
	}
	st.Tasks = []Task{
		{ID: "active", WorkspaceID: "w", GroupID: "review", TypeID: codexTaskTypeID, Title: "Active", Status: StatusRunning, CreatedAt: now, UpdatedAt: now},
		{ID: "other", WorkspaceID: "w2", GroupID: "other-a", TypeID: codexTaskTypeID, Title: "Other", Status: StatusRunning, CreatedAt: now, UpdatedAt: now},
	}

	next, moved, err := ReorderGroup(st, "review", -1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected group to move")
	}
	if got, want := groupIDs(GroupsForWorkspace(next, "w")), []string{"review", "release", "qa"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("workspace group order = %#v, want %#v", got, want)
	}
	if got, want := groupIDs(GroupsForWorkspace(next, "w2")), []string{"other-a", "other-b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("other workspace order = %#v, want %#v", got, want)
	}
	if next.SelectedWorkspaceID != "w" || next.SelectedGroupID != "review" || next.SelectedTaskID != "" || next.ActiveTaskID != "active" {
		t.Fatalf("selection did not stay on moved group without changing active task: %#v", next)
	}
	if !IsGroupCollapsed(next, "review") {
		t.Fatalf("collapsed group state was not preserved: %#v", next.CollapsedGroupIDs)
	}
	if task := TaskByID(next, "active"); task == nil || task.GroupID != "review" || task.WorkspaceID != "w" {
		t.Fatalf("task changed during group reorder: %#v", task)
	}

	next, moved, err = ReorderGroup(next, "review", -1)
	if err != nil {
		t.Fatal(err)
	}
	if moved {
		t.Fatal("first group in workspace should not move up")
	}

	next, moved, err = ReorderGroup(next, "review", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected group to move down")
	}
	if got, want := groupIDs(GroupsForWorkspace(next, "w")), []string{"release", "review", "qa"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("workspace group order after move down = %#v, want %#v", got, want)
	}

	next, moved, err = ReorderGroup(next, "qa", 1)
	if err != nil {
		t.Fatal(err)
	}
	if moved {
		t.Fatal("last group in workspace should not move down")
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
		ActiveTaskID:        "a",
		SelectedWorkspaceID: workspaceID,
		SelectedGroupID:     groupID,
		Focus:               FocusTasks,
		NavOpen:             true,
		Workspaces:          []Workspace{{ID: workspaceID, Path: dir, CreatedAt: now, UpdatedAt: now}},
		Groups:              []Group{{ID: groupID, WorkspaceID: workspaceID, Path: "inbox", CreatedAt: now, UpdatedAt: now}},
		Tasks: []Task{
			{ID: "a", WorkspaceID: workspaceID, GroupID: groupID, TypeID: codexTaskTypeID, Title: "A", Status: StatusRunning, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: now},
			{ID: "b", WorkspaceID: workspaceID, GroupID: groupID, TypeID: codexTaskTypeID, Title: "B", Status: StatusRunning, CreatedAt: "2026-01-01T00:01:00Z", UpdatedAt: now},
			{ID: "c", WorkspaceID: workspaceID, GroupID: groupID, TypeID: codexTaskTypeID, Title: "C", Status: StatusRunning, CreatedAt: "2026-01-01T00:02:00Z", UpdatedAt: now},
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

func taskIDs(tasks []Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func workspaceIDs(workspaces []Workspace) []string {
	ids := make([]string, 0, len(workspaces))
	for _, workspace := range workspaces {
		ids = append(ids, workspace.ID)
	}
	return ids
}

func groupIDs(groups []Group) []string {
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	return ids
}
