package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const Version = 5

type Focus string

const (
	FocusWorkspaces Focus = "workspaces"
	FocusTasks      Focus = "tasks"
	FocusConsole    Focus = "console"
)

type TaskStatus string

const (
	StatusStarting TaskStatus = "starting"
	StatusRunning  TaskStatus = "running"
	StatusReady    TaskStatus = "ready"
	StatusSitting  TaskStatus = "sitting"
	StatusShipping TaskStatus = "shipping"
	StatusStopped  TaskStatus = "stopped"
	StatusKilled   TaskStatus = "killed"
	StatusError    TaskStatus = "error"
)

type Workspace struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Title     string `json:"title,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Group struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Path        string `json:"path"`
	Silent      bool   `json:"silent,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Task struct {
	ID                  string     `json:"id"`
	WorkspaceID         string     `json:"workspace_id"`
	GroupID             string     `json:"group_id"`
	TypeID              string     `json:"type_id"`
	Title               string     `json:"title"`
	Silent              bool       `json:"silent,omitempty"`
	AutoTitle           string     `json:"auto_title,omitempty"`
	AutoTitleAttempted  bool       `json:"auto_title_attempted,omitempty"`
	AutoTitleError      string     `json:"auto_title_error,omitempty"`
	CodexTitle          string     `json:"codex_title,omitempty"`
	CodexStatus         string     `json:"codex_status,omitempty"`
	CodexSessionID      string     `json:"codex_session_id,omitempty"`
	CodexInputSubmitted bool       `json:"codex_input_submitted,omitempty"`
	TerminalCWD         string     `json:"terminal_cwd,omitempty"`
	Status              TaskStatus `json:"status"`
	CreatedAt           string     `json:"created_at"`
	UpdatedAt           string     `json:"updated_at"`
}

type State struct {
	Version             int         `json:"version"`
	ActiveTaskID        string      `json:"active_task_id,omitempty"`
	SelectedTaskID      string      `json:"selected_task_id,omitempty"`
	SelectedWorkspaceID string      `json:"selected_workspace_id,omitempty"`
	SelectedGroupID     string      `json:"selected_group_id,omitempty"`
	Focus               Focus       `json:"focus"`
	NavOpen             bool        `json:"nav_open"`
	Workspaces          []Workspace `json:"workspaces"`
	Groups              []Group     `json:"groups"`
	Tasks               []Task      `json:"tasks"`
	CollapsedGroupIDs   []string    `json:"collapsed_group_ids,omitempty"`
}

type Store struct {
	Path     string
	LockPath string
}

func NewStore(path string) *Store {
	return &Store{Path: path, LockPath: path + ".lock"}
}

func Empty() State {
	return State{Version: Version, Focus: FocusWorkspaces, NavOpen: true, Workspaces: []Workspace{}, Groups: []Group{}, Tasks: []Task{}, CollapsedGroupIDs: []string{}}
}

func NowISO() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func (s *Store) Ensure() (State, error) {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return State{}, err
	}
	if err := ensureLockFile(s.LockPath); err != nil {
		return State{}, err
	}
	var loaded State
	err := withFileLock(s.LockPath, func() error {
		if _, err := os.Stat(s.Path); errors.Is(err, os.ErrNotExist) {
			loaded = Empty()
			return writeJSONAtomic(s.Path, loaded)
		}
		raw, err := os.ReadFile(s.Path)
		if err != nil {
			return err
		}
		loaded, err = parseState(raw)
		if err != nil {
			return err
		}
		return nil
	})
	return loaded, err
}

func (s *Store) Read() (State, error) {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return State{}, err
	}
	if err := ensureLockFile(s.LockPath); err != nil {
		return State{}, err
	}
	var loaded State
	err := withFileLock(s.LockPath, func() error {
		raw, err := os.ReadFile(s.Path)
		if errors.Is(err, os.ErrNotExist) {
			loaded = Empty()
			return nil
		}
		if err != nil {
			return err
		}
		var parseErr error
		loaded, parseErr = parseState(raw)
		return parseErr
	})
	return loaded, err
}

func (s *Store) Write(next State) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	if err := ensureLockFile(s.LockPath); err != nil {
		return err
	}
	return withFileLock(s.LockPath, func() error {
		if err := ValidateCurrent(next); err != nil {
			return unsupportedStateError(err.Error())
		}
		return writeJSONAtomic(s.Path, next)
	})
}

func parseState(raw []byte) (State, error) {
	var st State
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&st); err != nil {
		return State{}, unsupportedStateError(fmt.Sprintf("could not parse strict v5 state: %v", err))
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return State{}, unsupportedStateError("could not parse strict v5 state: multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return State{}, unsupportedStateError(fmt.Sprintf("could not parse strict v5 state: %v", err))
	}
	if st.Version != Version {
		return State{}, unsupportedStateError(fmt.Sprintf("unsupported state version %d", st.Version))
	}
	if !validFocus(st.Focus) {
		return State{}, unsupportedStateError(fmt.Sprintf("unsupported focus value %q", st.Focus))
	}
	if err := ValidateCurrent(st); err != nil {
		return State{}, unsupportedStateError(err.Error())
	}
	return st, nil
}

func unsupportedStateError(reason string) error {
	return fmt.Errorf("%s; run `weft clear` to reset", reason)
}

func validFocus(focus Focus) bool {
	switch focus {
	case FocusWorkspaces, FocusTasks, FocusConsole:
		return true
	default:
		return false
	}
}

func ValidateCurrent(st State) error {
	if st.Version != Version {
		return fmt.Errorf("unsupported state version %d", st.Version)
	}
	if !validFocus(st.Focus) {
		return fmt.Errorf("unsupported focus value %q", st.Focus)
	}
	if st.Workspaces == nil {
		return fmt.Errorf("workspaces must be an array")
	}
	if st.Groups == nil {
		return fmt.Errorf("groups must be an array")
	}
	if st.Tasks == nil {
		return fmt.Errorf("tasks must be an array")
	}
	workspaceIDs := map[string]bool{}
	for index, workspace := range st.Workspaces {
		if strings.TrimSpace(workspace.ID) == "" {
			return fmt.Errorf("workspaces[%d].id is required", index)
		}
		if workspaceIDs[workspace.ID] {
			return fmt.Errorf("workspaces[%d].id %q is duplicated", index, workspace.ID)
		}
		workspaceIDs[workspace.ID] = true
		if strings.TrimSpace(workspace.Path) == "" {
			return fmt.Errorf("workspaces[%d].path is required", index)
		}
		if strings.TrimSpace(workspace.CreatedAt) == "" {
			return fmt.Errorf("workspaces[%d].created_at is required", index)
		}
		if strings.TrimSpace(workspace.UpdatedAt) == "" {
			return fmt.Errorf("workspaces[%d].updated_at is required", index)
		}
	}
	groupIDs := map[string]Group{}
	for index, group := range st.Groups {
		if strings.TrimSpace(group.ID) == "" {
			return fmt.Errorf("groups[%d].id is required", index)
		}
		if _, exists := groupIDs[group.ID]; exists {
			return fmt.Errorf("groups[%d].id %q is duplicated", index, group.ID)
		}
		if strings.TrimSpace(group.WorkspaceID) == "" {
			return fmt.Errorf("groups[%d].workspace_id is required", index)
		}
		if !workspaceIDs[group.WorkspaceID] {
			return fmt.Errorf("groups[%d].workspace_id %q is not defined", index, group.WorkspaceID)
		}
		if strings.TrimSpace(group.Path) == "" {
			return fmt.Errorf("groups[%d].path is required", index)
		}
		if strings.Contains(group.Path, "/") {
			return fmt.Errorf("groups[%d].path cannot contain /", index)
		}
		if strings.TrimSpace(group.CreatedAt) == "" {
			return fmt.Errorf("groups[%d].created_at is required", index)
		}
		if strings.TrimSpace(group.UpdatedAt) == "" {
			return fmt.Errorf("groups[%d].updated_at is required", index)
		}
		groupIDs[group.ID] = group
	}
	taskIDs := map[string]Task{}
	for index, task := range st.Tasks {
		if strings.TrimSpace(task.ID) == "" {
			return fmt.Errorf("tasks[%d].id is required", index)
		}
		if _, exists := taskIDs[task.ID]; exists {
			return fmt.Errorf("tasks[%d].id %q is duplicated", index, task.ID)
		}
		if strings.TrimSpace(task.WorkspaceID) == "" {
			return fmt.Errorf("tasks[%d].workspace_id is required", index)
		}
		if !workspaceIDs[task.WorkspaceID] {
			return fmt.Errorf("tasks[%d].workspace_id %q is not defined", index, task.WorkspaceID)
		}
		if strings.TrimSpace(task.TypeID) == "" {
			return fmt.Errorf("tasks[%d].type_id is required", index)
		}
		if strings.TrimSpace(task.Title) == "" {
			return fmt.Errorf("tasks[%d].title is required", index)
		}
		if !validTaskStatus(task.Status) {
			return fmt.Errorf("tasks[%d].status %q is not supported", index, task.Status)
		}
		if strings.TrimSpace(task.CreatedAt) == "" {
			return fmt.Errorf("tasks[%d].created_at is required", index)
		}
		if strings.TrimSpace(task.UpdatedAt) == "" {
			return fmt.Errorf("tasks[%d].updated_at is required", index)
		}
		if task.GroupID != "" {
			group, ok := groupIDs[task.GroupID]
			if !ok {
				return fmt.Errorf("tasks[%d].group_id %q is not defined", index, task.GroupID)
			}
			if group.WorkspaceID != task.WorkspaceID {
				return fmt.Errorf("tasks[%d].group_id %q belongs to workspace %q", index, task.GroupID, group.WorkspaceID)
			}
		}
		taskIDs[task.ID] = task
	}
	if st.ActiveTaskID != "" {
		if _, ok := taskIDs[st.ActiveTaskID]; !ok {
			return fmt.Errorf("active_task_id %q is not defined", st.ActiveTaskID)
		}
	}
	if st.SelectedTaskID != "" {
		task, ok := taskIDs[st.SelectedTaskID]
		if !ok {
			return fmt.Errorf("selected_task_id %q is not defined", st.SelectedTaskID)
		}
		if st.SelectedWorkspaceID != "" && task.WorkspaceID != st.SelectedWorkspaceID {
			return fmt.Errorf("selected_task_id %q belongs to workspace %q", st.SelectedTaskID, task.WorkspaceID)
		}
	}
	if st.SelectedWorkspaceID != "" && !workspaceIDs[st.SelectedWorkspaceID] {
		return fmt.Errorf("selected_workspace_id %q is not defined", st.SelectedWorkspaceID)
	}
	if st.SelectedGroupID != "" {
		group, ok := groupIDs[st.SelectedGroupID]
		if !ok {
			return fmt.Errorf("selected_group_id %q is not defined", st.SelectedGroupID)
		}
		if st.SelectedWorkspaceID != "" && group.WorkspaceID != st.SelectedWorkspaceID {
			return fmt.Errorf("selected_group_id %q belongs to workspace %q", st.SelectedGroupID, group.WorkspaceID)
		}
	}
	for index, groupID := range st.CollapsedGroupIDs {
		if _, ok := groupIDs[groupID]; !ok {
			return fmt.Errorf("collapsed_group_ids[%d] %q is not defined", index, groupID)
		}
	}
	if st.NavOpen {
		if st.Focus != FocusWorkspaces && st.Focus != FocusTasks {
			return fmt.Errorf("focus %q requires nav_open=false", st.Focus)
		}
	} else if st.Focus != FocusConsole {
		return fmt.Errorf("focus %q requires nav_open=true", st.Focus)
	}
	if st.ActiveTaskID == "" && !st.NavOpen {
		return fmt.Errorf("nav_open must be true when active_task_id is empty")
	}
	return nil
}

func validTaskStatus(status TaskStatus) bool {
	switch status {
	case StatusStarting, StatusRunning, StatusReady, StatusSitting, StatusShipping, StatusStopped, StatusKilled, StatusError:
		return true
	default:
		return false
	}
}

func ValidateTaskTypes(st State, hasTaskType func(string) bool) error {
	if hasTaskType == nil {
		return fmt.Errorf("active config task types are not available")
	}
	for index, task := range st.Tasks {
		typeID := strings.TrimSpace(task.TypeID)
		if !hasTaskType(typeID) {
			return fmt.Errorf("tasks[%d].type_id %q is not defined in active config", index, typeID)
		}
	}
	return nil
}

func ensureLockFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	return file.Close()
}

func withFileLock(path string, fn func() error) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn()
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func ActiveTask(st State) *Task {
	return TaskByID(st, st.ActiveTaskID)
}

func TaskByID(st State, taskID string) *Task {
	if taskID == "" {
		return nil
	}
	for index := range st.Tasks {
		if st.Tasks[index].ID == taskID {
			return &st.Tasks[index]
		}
	}
	return nil
}

func WorkspaceByID(st State, workspaceID string) *Workspace {
	if workspaceID == "" {
		return nil
	}
	for index := range st.Workspaces {
		if st.Workspaces[index].ID == workspaceID {
			return &st.Workspaces[index]
		}
	}
	return nil
}

func WorkspaceByPath(st State, path string) *Workspace {
	path = NormalizeWorkspacePath(path)
	for index := range st.Workspaces {
		if st.Workspaces[index].Path == path {
			return &st.Workspaces[index]
		}
	}
	return nil
}

func GroupByID(st State, groupID string) *Group {
	if groupID == "" {
		return nil
	}
	for index := range st.Groups {
		if st.Groups[index].ID == groupID {
			return &st.Groups[index]
		}
	}
	return nil
}

func ActiveWorkspace(st State) *Workspace {
	return WorkspaceByID(st, st.SelectedWorkspaceID)
}

func WorkspaceForTask(st State, task Task) *Workspace {
	return WorkspaceByID(st, task.WorkspaceID)
}

func GroupForTask(st State, task Task) *Group {
	return GroupByID(st, task.GroupID)
}

func GroupsForWorkspace(st State, workspaceID string) []Group {
	var groups []Group
	for _, group := range st.Groups {
		if group.WorkspaceID == workspaceID {
			groups = append(groups, group)
		}
	}
	return groups
}

func TasksForGroup(st State, groupID string) []Task {
	if groupID == "" {
		return nil
	}
	var tasks []Task
	for _, task := range st.Tasks {
		if task.GroupID == groupID {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func UngroupedTasksForWorkspace(st State, workspaceID string) []Task {
	var tasks []Task
	for _, task := range st.Tasks {
		if task.WorkspaceID == workspaceID && task.GroupID == "" {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func TaskCountForGroup(st State, groupID string) int {
	if groupID == "" {
		return 0
	}
	count := 0
	for _, task := range st.Tasks {
		if task.GroupID == groupID {
			count++
		}
	}
	return count
}

func WithUpdatedTask(st State, taskID string, update func(Task) Task) State {
	for index, task := range st.Tasks {
		if task.ID == taskID {
			updated := update(task)
			updated.UpdatedAt = NowISO()
			st.Tasks[index] = updated
			break
		}
	}
	return st
}

func CloseTask(st State, taskID string) State {
	index := -1
	removed := Task{}
	for i, task := range st.Tasks {
		if task.ID == taskID {
			index = i
			removed = task
			break
		}
	}
	if index < 0 {
		return st
	}
	workspaceIndex := 0
	for i, task := range st.Tasks {
		if i == index {
			break
		}
		if task.WorkspaceID == removed.WorkspaceID {
			workspaceIndex++
		}
	}
	st.Tasks = append(st.Tasks[:index], st.Tasks[index+1:]...)
	if st.SelectedTaskID == taskID {
		st.SelectedTaskID = ""
	}
	if st.ActiveTaskID != taskID {
		return st
	}
	st.ActiveTaskID = ""
	candidates := tasksForWorkspace(st, removed.WorkspaceID)
	if len(candidates) > 0 {
		nextIndex := workspaceIndex
		if nextIndex >= len(candidates) {
			nextIndex = len(candidates) - 1
		}
		next := candidates[nextIndex]
		st.ActiveTaskID = next.ID
		st.SelectedTaskID = next.ID
		st.SelectedWorkspaceID = next.WorkspaceID
		st.SelectedGroupID = next.GroupID
	} else {
		st.ActiveTaskID = ""
		st.SelectedTaskID = ""
		st.NavOpen = true
		st.Focus = FocusTasks
		st.SelectedWorkspaceID = removed.WorkspaceID
		st.SelectedGroupID = removed.GroupID
	}
	return st
}

func ReorderTask(st State, taskID string, delta int) (State, bool, error) {
	if delta == 0 {
		return st, false, nil
	}
	index := -1
	selected := Task{}
	for i, task := range st.Tasks {
		if task.ID == taskID {
			index = i
			selected = task
			break
		}
	}
	if index < 0 {
		return st, false, fmt.Errorf("task not found")
	}
	if delta < 0 {
		for i := index - 1; i >= 0; i-- {
			if sameTaskOrderArea(st.Tasks[i], selected) {
				return swapTasksAndSelect(st, index, i, selected), true, nil
			}
		}
	} else {
		for i := index + 1; i < len(st.Tasks); i++ {
			if sameTaskOrderArea(st.Tasks[i], selected) {
				return swapTasksAndSelect(st, index, i, selected), true, nil
			}
		}
	}
	return moveTaskToAdjacentArea(st, index, selected, delta)
}

func swapTasksAndSelect(st State, index int, target int, selected Task) State {
	st.Tasks[index], st.Tasks[target] = st.Tasks[target], st.Tasks[index]
	now := NowISO()
	st.Tasks[index].UpdatedAt = now
	st.Tasks[target].UpdatedAt = now
	st.ActiveTaskID = selected.ID
	st.SelectedTaskID = selected.ID
	st.SelectedWorkspaceID = selected.WorkspaceID
	st.SelectedGroupID = selected.GroupID
	return st
}

func sameTaskOrderArea(left Task, right Task) bool {
	return left.WorkspaceID == right.WorkspaceID && left.GroupID == right.GroupID
}

func moveTaskToAdjacentArea(st State, index int, selected Task, delta int) (State, bool, error) {
	areas := taskOrderAreas(st, selected.WorkspaceID)
	currentArea := areaIndex(areas, selected.GroupID)
	if currentArea < 0 {
		return st, false, fmt.Errorf("task group not found")
	}
	targetArea := currentArea
	if delta < 0 {
		targetArea--
	} else {
		targetArea++
	}
	if targetArea < 0 || targetArea >= len(areas) {
		return st, false, nil
	}
	targetGroupID := areas[targetArea]
	selected.GroupID = targetGroupID
	selected.UpdatedAt = NowISO()
	tasks := append([]Task{}, st.Tasks[:index]...)
	tasks = append(tasks, st.Tasks[index+1:]...)
	insertAt := taskAreaInsertIndex(tasks, selected.WorkspaceID, targetGroupID, areas, targetArea, delta > 0)
	tasks = append(tasks, Task{})
	copy(tasks[insertAt+1:], tasks[insertAt:])
	tasks[insertAt] = selected
	st.Tasks = tasks
	st.ActiveTaskID = selected.ID
	st.SelectedTaskID = selected.ID
	st.SelectedWorkspaceID = selected.WorkspaceID
	st.SelectedGroupID = selected.GroupID
	return st, true, nil
}

func taskOrderAreas(st State, workspaceID string) []string {
	areas := []string{""}
	for _, group := range GroupsForWorkspace(st, workspaceID) {
		areas = append(areas, group.ID)
	}
	return areas
}

func areaIndex(areas []string, groupID string) int {
	for index, areaGroupID := range areas {
		if areaGroupID == groupID {
			return index
		}
	}
	return -1
}

func taskAreaInsertIndex(tasks []Task, workspaceID string, targetGroupID string, areas []string, targetArea int, atStart bool) int {
	if atStart {
		for index, task := range tasks {
			if task.WorkspaceID == workspaceID && task.GroupID == targetGroupID {
				return index
			}
		}
	} else {
		for index := len(tasks) - 1; index >= 0; index-- {
			if tasks[index].WorkspaceID == workspaceID && tasks[index].GroupID == targetGroupID {
				return index + 1
			}
		}
	}
	insertAt := len(tasks)
	for index, task := range tasks {
		if task.WorkspaceID != workspaceID {
			continue
		}
		taskArea := areaIndex(areas, task.GroupID)
		if taskArea < 0 {
			continue
		}
		if taskArea > targetArea {
			return index
		}
		if taskArea < targetArea {
			insertAt = index + 1
		}
	}
	return insertAt
}

func ReorderGroup(st State, groupID string, delta int) (State, bool, error) {
	if delta == 0 {
		return st, false, nil
	}
	index := -1
	selected := Group{}
	for i, group := range st.Groups {
		if group.ID == groupID {
			index = i
			selected = group
			break
		}
	}
	if index < 0 {
		return st, false, fmt.Errorf("group not found")
	}
	target := -1
	if delta < 0 {
		for i := index - 1; i >= 0; i-- {
			if st.Groups[i].WorkspaceID == selected.WorkspaceID {
				target = i
				break
			}
		}
	} else {
		for i := index + 1; i < len(st.Groups); i++ {
			if st.Groups[i].WorkspaceID == selected.WorkspaceID {
				target = i
				break
			}
		}
	}
	if target < 0 {
		return st, false, nil
	}
	st.Groups[index], st.Groups[target] = st.Groups[target], st.Groups[index]
	now := NowISO()
	st.Groups[index].UpdatedAt = now
	st.Groups[target].UpdatedAt = now
	st.SelectedWorkspaceID = selected.WorkspaceID
	st.SelectedGroupID = selected.ID
	st.SelectedTaskID = ""
	return st, true, nil
}

func MoveTask(st State, taskID string, groupID string) (State, error) {
	var target *Group
	if groupID != "" {
		target = GroupByID(st, groupID)
		if target == nil {
			return st, fmt.Errorf("group not found")
		}
	}
	for index, task := range st.Tasks {
		if task.ID != taskID {
			continue
		}
		if target != nil && task.WorkspaceID != target.WorkspaceID {
			return st, fmt.Errorf("cross-workspace moves are not supported")
		}
		st.Tasks[index].GroupID = groupID
		st.Tasks[index].UpdatedAt = NowISO()
		st.SelectedTaskID = taskID
		st.SelectedGroupID = groupID
		return st, nil
	}
	return st, fmt.Errorf("task not found")
}

func AddWorkspace(st State, id string, path string, now string) (State, Workspace, error) {
	if strings.TrimSpace(id) == "" {
		return st, Workspace{}, fmt.Errorf("workspace id is required")
	}
	if strings.TrimSpace(now) == "" {
		return st, Workspace{}, fmt.Errorf("workspace timestamp is required")
	}
	path = NormalizeWorkspacePath(path)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, Workspace{}, fmt.Errorf("workspace path does not exist: %s", path)
		}
		return st, Workspace{}, fmt.Errorf("cannot read workspace path %s: %w", path, err)
	}
	if !info.IsDir() {
		return st, Workspace{}, fmt.Errorf("workspace path is not a directory: %s", path)
	}
	if workspace := WorkspaceByPath(st, path); workspace != nil {
		st = SelectWorkspace(st, workspace.ID)
		return st, *workspace, nil
	}
	workspace := Workspace{ID: id, Path: path, CreatedAt: now, UpdatedAt: now}
	st.Workspaces = append(st.Workspaces, workspace)
	st.SelectedWorkspaceID = id
	st.SelectedGroupID = ""
	st.NavOpen = true
	st.Focus = FocusTasks
	return st, workspace, nil
}

func SelectWorkspace(st State, workspaceID string) State {
	if WorkspaceByID(st, workspaceID) == nil {
		return st
	}
	if st.SelectedWorkspaceID == workspaceID {
		return st
	}
	st.SelectedWorkspaceID = workspaceID
	st.SelectedTaskID = ""
	st.SelectedGroupID = ""
	if active := TaskByID(st, st.ActiveTaskID); active != nil && active.WorkspaceID == workspaceID {
		st.SelectedTaskID = active.ID
		st.SelectedGroupID = active.GroupID
		return st
	}
	if groups := GroupsForWorkspace(st, workspaceID); len(groups) > 0 {
		st.SelectedGroupID = groups[0].ID
	}
	return st
}

func ReorderWorkspace(st State, workspaceID string, delta int) (State, bool, error) {
	if delta == 0 {
		return st, false, nil
	}
	index := -1
	for i, workspace := range st.Workspaces {
		if workspace.ID == workspaceID {
			index = i
			break
		}
	}
	if index < 0 {
		return st, false, fmt.Errorf("workspace not found")
	}
	target := index - 1
	if delta > 0 {
		target = index + 1
	}
	if target < 0 || target >= len(st.Workspaces) {
		return st, false, nil
	}
	st.Workspaces[index], st.Workspaces[target] = st.Workspaces[target], st.Workspaces[index]
	now := NowISO()
	st.Workspaces[index].UpdatedAt = now
	st.Workspaces[target].UpdatedAt = now
	if st.SelectedWorkspaceID != workspaceID {
		st = SelectWorkspace(st, workspaceID)
	}
	return st, true, nil
}

func SelectWorkspaceByPath(st State, path string) (State, bool) {
	workspace := WorkspaceByPath(st, path)
	if workspace == nil {
		return st, false
	}
	return SelectWorkspace(st, workspace.ID), true
}

func RemoveWorkspace(st State, workspaceID string) (State, []Task, error) {
	if WorkspaceByID(st, workspaceID) == nil {
		return st, nil, fmt.Errorf("workspace not found")
	}
	var removed []Task
	tasks := make([]Task, 0, len(st.Tasks))
	for _, task := range st.Tasks {
		if task.WorkspaceID == workspaceID {
			removed = append(removed, task)
			continue
		}
		tasks = append(tasks, task)
	}
	removedGroupIDs := map[string]bool{}
	for _, group := range st.Groups {
		if group.WorkspaceID == workspaceID {
			removedGroupIDs[group.ID] = true
		}
	}
	st.Tasks = tasks
	st.Groups = filterGroups(st.Groups, func(group Group) bool { return group.WorkspaceID != workspaceID })
	st.Workspaces = filterWorkspaces(st.Workspaces, func(workspace Workspace) bool { return workspace.ID != workspaceID })
	st.CollapsedGroupIDs = filterCollapsedGroupIDs(st.CollapsedGroupIDs, removedGroupIDs)
	if st.ActiveTaskID != "" {
		if TaskByID(st, st.ActiveTaskID) == nil {
			st.ActiveTaskID = ""
		}
	}
	if st.SelectedTaskID != "" && TaskByID(st, st.SelectedTaskID) == nil {
		st.SelectedTaskID = ""
	}
	if len(st.Workspaces) > 0 {
		st.SelectedWorkspaceID = st.Workspaces[0].ID
	} else {
		st.SelectedWorkspaceID = ""
	}
	st.SelectedGroupID = ""
	if selected := TaskByID(st, st.SelectedTaskID); selected != nil {
		if selected.WorkspaceID != st.SelectedWorkspaceID {
			st.SelectedTaskID = ""
		} else {
			st.SelectedGroupID = selected.GroupID
		}
	}
	st.NavOpen = true
	st.Focus = FocusWorkspaces
	return st, removed, nil
}

func SetWorkspaceTitle(st State, workspaceID string, title string) (State, error) {
	title = strings.TrimSpace(title)
	if WorkspaceByID(st, workspaceID) == nil {
		return st, fmt.Errorf("workspace not found")
	}
	for index := range st.Workspaces {
		if st.Workspaces[index].ID == workspaceID {
			st.Workspaces[index].Title = title
			st.Workspaces[index].UpdatedAt = NowISO()
			return st, nil
		}
	}
	return st, fmt.Errorf("workspace not found")
}

func AddGroupWithSilent(st State, id string, workspaceID string, path string, now string, silent bool) (State, Group, error) {
	if strings.TrimSpace(id) == "" {
		return st, Group{}, fmt.Errorf("group id is required")
	}
	if strings.TrimSpace(now) == "" {
		return st, Group{}, fmt.Errorf("group timestamp is required")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return st, Group{}, fmt.Errorf("group name is required")
	}
	if strings.Contains(path, "/") {
		return st, Group{}, fmt.Errorf("group names cannot contain /")
	}
	if WorkspaceByID(st, workspaceID) == nil {
		return st, Group{}, fmt.Errorf("workspace not found")
	}
	for _, group := range GroupsForWorkspace(st, workspaceID) {
		if group.Path == path {
			return st, Group{}, fmt.Errorf("group name already exists")
		}
	}
	group := Group{ID: id, WorkspaceID: workspaceID, Path: path, Silent: silent, CreatedAt: now, UpdatedAt: now}
	st.Groups = append(st.Groups, group)
	st.SelectedWorkspaceID = workspaceID
	st.SelectedGroupID = id
	st.NavOpen = true
	st.Focus = FocusTasks
	return st, group, nil
}

func EditGroup(st State, groupID string, path string, silent bool) (State, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return st, fmt.Errorf("group name is required")
	}
	if strings.Contains(path, "/") {
		return st, fmt.Errorf("group names cannot contain /")
	}
	group := GroupByID(st, groupID)
	if group == nil {
		return st, fmt.Errorf("group not found")
	}
	for _, other := range GroupsForWorkspace(st, group.WorkspaceID) {
		if other.ID != groupID && other.Path == path {
			return st, fmt.Errorf("group name already exists")
		}
	}
	for index := range st.Groups {
		if st.Groups[index].ID == groupID {
			st.Groups[index].Path = path
			st.Groups[index].Silent = silent
			st.Groups[index].UpdatedAt = NowISO()
			return st, nil
		}
	}
	return st, fmt.Errorf("group not found")
}

func DeleteGroup(st State, groupID string) (State, error) {
	if GroupByID(st, groupID) == nil {
		return st, fmt.Errorf("group not found")
	}
	if TaskCountForGroup(st, groupID) > 0 {
		return st, fmt.Errorf("group is not empty")
	}
	st.Groups = filterGroups(st.Groups, func(group Group) bool { return group.ID != groupID })
	st.CollapsedGroupIDs = removeString(st.CollapsedGroupIDs, groupID)
	if st.SelectedGroupID == groupID {
		st.SelectedGroupID = ""
	}
	if selected := TaskByID(st, st.SelectedTaskID); selected != nil && selected.GroupID == groupID {
		st.SelectedTaskID = ""
	} else if selected != nil && selected.WorkspaceID == st.SelectedWorkspaceID {
		st.SelectedGroupID = selected.GroupID
	}
	return st, nil
}

func IsGroupCollapsed(st State, groupID string) bool {
	for _, id := range st.CollapsedGroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}

func ToggleGroupCollapsed(st State, groupID string) State {
	if GroupByID(st, groupID) == nil {
		return st
	}
	if IsGroupCollapsed(st, groupID) {
		st.CollapsedGroupIDs = removeString(st.CollapsedGroupIDs, groupID)
		return st
	}
	st.CollapsedGroupIDs = append(st.CollapsedGroupIDs, groupID)
	return st
}

func AddTaskWithType(st State, id string, workspaceID string, groupID string, typeID string, title string, now string) (State, Task, error) {
	return AddTaskWithTypeAndSilent(st, id, workspaceID, groupID, typeID, title, now, false)
}

func AddTaskWithTypeAndSilent(st State, id string, workspaceID string, groupID string, typeID string, title string, now string, silent bool) (State, Task, error) {
	if strings.TrimSpace(id) == "" {
		return st, Task{}, fmt.Errorf("task id is required")
	}
	if strings.TrimSpace(now) == "" {
		return st, Task{}, fmt.Errorf("task timestamp is required")
	}
	if WorkspaceByID(st, workspaceID) == nil {
		return st, Task{}, fmt.Errorf("workspace not found")
	}
	if groupID != "" {
		group := GroupByID(st, groupID)
		if group == nil || group.WorkspaceID != workspaceID {
			return st, Task{}, fmt.Errorf("group not found")
		}
	}
	typeID = strings.TrimSpace(typeID)
	if typeID == "" {
		return st, Task{}, fmt.Errorf("task type is required")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return st, Task{}, fmt.Errorf("task title is required")
	}
	task := Task{
		ID: id, WorkspaceID: workspaceID, GroupID: groupID,
		TypeID: typeID, Title: title, Silent: silent, Status: StatusStarting, CreatedAt: now, UpdatedAt: now,
	}
	st.Tasks = append(st.Tasks, task)
	st.ActiveTaskID = id
	st.SelectedTaskID = id
	st.SelectedWorkspaceID = workspaceID
	st.SelectedGroupID = groupID
	st.Focus = FocusConsole
	st.NavOpen = false
	return st, task, nil
}

func TaskTypeID(task Task) string {
	return strings.TrimSpace(task.TypeID)
}

func RenameTask(st State, taskID string, title string) (State, error) {
	return editTask(st, taskID, title, nil)
}

func EditTask(st State, taskID string, title string, silent bool) (State, error) {
	return editTask(st, taskID, title, &silent)
}

func editTask(st State, taskID string, title string, silent *bool) (State, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return st, fmt.Errorf("title cannot be empty")
	}
	if TaskByID(st, taskID) == nil {
		return st, fmt.Errorf("task not found")
	}
	return WithUpdatedTask(st, taskID, func(task Task) Task {
		task.Title = title
		if silent != nil {
			task.Silent = *silent
		}
		return task
	}), nil
}

func NormalizeWorkspacePath(path string) string {
	return absolutePath(path)
}

func absolutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return "."
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, path[2:])
			}
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func filterCollapsedGroupIDs(ids []string, removed map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if removed[id] {
			continue
		}
		out = append(out, id)
	}
	return out
}

func tasksForWorkspace(st State, workspaceID string) []Task {
	var tasks []Task
	for _, task := range st.Tasks {
		if task.WorkspaceID == workspaceID {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func removeString(values []string, value string) []string {
	out := values[:0]
	for _, item := range values {
		if item != value {
			out = append(out, item)
		}
	}
	return out
}

func filterGroups(groups []Group, keep func(Group) bool) []Group {
	out := groups[:0]
	for _, group := range groups {
		if keep(group) {
			out = append(out, group)
		}
	}
	return out
}

func filterWorkspaces(workspaces []Workspace, keep func(Workspace) bool) []Workspace {
	out := workspaces[:0]
	for _, workspace := range workspaces {
		if keep(workspace) {
			out = append(out, workspace)
		}
	}
	return out
}
