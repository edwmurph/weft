package state

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
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

const Version = 4

const DefaultGroupPath = "inbox"
const DefaultAgentTitle = "{codex}"
const DefaultAgentTypeID = "codex"

type Focus string

const (
	FocusWorkspaces Focus = "workspaces"
	FocusAgents     Focus = "agents"
	FocusCodex      Focus = "codex"
)

type AgentStatus string

const (
	StatusStarting AgentStatus = "starting"
	StatusRunning  AgentStatus = "running"
	StatusReady    AgentStatus = "ready"
	StatusSitting  AgentStatus = "sitting"
	StatusShipping AgentStatus = "shipping"
	StatusStopped  AgentStatus = "stopped"
	StatusKilled   AgentStatus = "killed"
	StatusError    AgentStatus = "error"
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

type Agent struct {
	ID                  string      `json:"id"`
	WorkspaceID         string      `json:"workspace_id"`
	GroupID             string      `json:"group_id"`
	TypeID              string      `json:"type_id,omitempty"`
	Title               string      `json:"title"`
	AutoTitle           string      `json:"auto_title,omitempty"`
	AutoTitleAttempted  bool        `json:"auto_title_attempted,omitempty"`
	AutoTitleError      string      `json:"auto_title_error,omitempty"`
	CodexTitle          string      `json:"codex_title,omitempty"`
	CodexStatus         string      `json:"codex_status,omitempty"`
	CodexSessionID      string      `json:"codex_session_id,omitempty"`
	CodexInputSubmitted bool        `json:"codex_input_submitted,omitempty"`
	Status              AgentStatus `json:"status"`
	CreatedAt           string      `json:"created_at"`
	UpdatedAt           string      `json:"updated_at"`
}

type State struct {
	Version             int         `json:"version"`
	ActiveAgentID       string      `json:"active_agent_id,omitempty"`
	SelectedAgentID     string      `json:"selected_agent_id,omitempty"`
	SelectedWorkspaceID string      `json:"selected_workspace_id,omitempty"`
	SelectedGroupID     string      `json:"selected_group_id,omitempty"`
	Focus               Focus       `json:"focus"`
	NavOpen             bool        `json:"nav_open"`
	Workspaces          []Workspace `json:"workspaces"`
	Groups              []Group     `json:"groups"`
	Agents              []Agent     `json:"agents"`
	CollapsedGroupIDs   []string    `json:"collapsed_group_ids,omitempty"`
}

type Store struct {
	Path      string
	LockPath  string
	Workspace string
}

func NewStore(path string, workspace ...string) *Store {
	current := ""
	if len(workspace) > 0 {
		current = workspace[0]
	}
	return &Store{Path: path, LockPath: path + ".lock", Workspace: current}
}

func Empty() State {
	return State{Version: Version, Focus: FocusWorkspaces, NavOpen: true, Workspaces: []Workspace{}, Groups: []Group{}, Agents: []Agent{}, CollapsedGroupIDs: []string{}}
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
			loaded = Repair(Empty(), s.Workspace)
			return writeJSONAtomic(s.Path, loaded)
		}
		raw, err := os.ReadFile(s.Path)
		if err != nil {
			return err
		}
		loaded, err = parseState(raw, s.Workspace)
		if err != nil {
			return err
		}
		repaired := Repair(loaded, s.Workspace)
		loaded = repaired
		return writeJSONAtomic(s.Path, repaired)
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
			loaded = Repair(Empty(), s.Workspace)
			return nil
		}
		if err != nil {
			return err
		}
		var parseErr error
		loaded, parseErr = parseState(raw, s.Workspace)
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
		return writeJSONAtomic(s.Path, Repair(next, s.Workspace))
	})
}

func parseState(raw []byte, fallbackWorkspace string) (State, error) {
	var st State
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&st); err != nil {
		return State{}, unsupportedStateError(fmt.Sprintf("could not parse strict v4 state: %v", err))
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return State{}, unsupportedStateError("could not parse strict v4 state: multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return State{}, unsupportedStateError(fmt.Sprintf("could not parse strict v4 state: %v", err))
	}
	if st.Version != Version {
		return State{}, unsupportedStateError(fmt.Sprintf("unsupported state version %d", st.Version))
	}
	return Repair(st, fallbackWorkspace), nil
}

func unsupportedStateError(reason string) error {
	return fmt.Errorf("%s; run `weft clear` to reset", reason)
}

func Repair(st State, fallbackWorkspace string) State {
	st.Version = Version
	if st.Workspaces == nil {
		st.Workspaces = []Workspace{}
	}
	if st.Groups == nil {
		st.Groups = []Group{}
	}
	if st.Agents == nil {
		st.Agents = []Agent{}
	}
	if st.CollapsedGroupIDs == nil {
		st.CollapsedGroupIDs = []string{}
	}
	workspaces := map[string]bool{}
	for index := range st.Workspaces {
		if strings.TrimSpace(st.Workspaces[index].ID) == "" {
			st.Workspaces[index].ID = StableID("workspace", st.Workspaces[index].Path)
		}
		st.Workspaces[index].Path = absolutePath(st.Workspaces[index].Path)
		st.Workspaces[index].Title = strings.TrimSpace(st.Workspaces[index].Title)
		if st.Workspaces[index].CreatedAt == "" {
			st.Workspaces[index].CreatedAt = NowISO()
		}
		if st.Workspaces[index].UpdatedAt == "" {
			st.Workspaces[index].UpdatedAt = st.Workspaces[index].CreatedAt
		}
		workspaces[st.Workspaces[index].ID] = true
	}

	for index := range st.Groups {
		if !workspaces[st.Groups[index].WorkspaceID] && len(st.Workspaces) > 0 {
			st.Groups[index].WorkspaceID = st.Workspaces[0].ID
		}
		if strings.TrimSpace(st.Groups[index].Path) == "" {
			st.Groups[index].Path = DefaultGroupPath
		}
		if strings.TrimSpace(st.Groups[index].ID) == "" {
			st.Groups[index].ID = StableID("group", st.Groups[index].WorkspaceID, st.Groups[index].Path)
		}
		if st.Groups[index].CreatedAt == "" {
			st.Groups[index].CreatedAt = NowISO()
		}
		if st.Groups[index].UpdatedAt == "" {
			st.Groups[index].UpdatedAt = st.Groups[index].CreatedAt
		}
	}

	groupIDs := map[string]Group{}
	for _, group := range st.Groups {
		groupIDs[group.ID] = group
	}
	st.CollapsedGroupIDs = validCollapsedGroupIDs(st.CollapsedGroupIDs, groupIDs)
	for index := range st.Agents {
		agent := &st.Agents[index]
		if strings.TrimSpace(agent.ID) == "" {
			agent.ID = StableID("agent", agent.WorkspaceID, agent.GroupID, agent.CreatedAt, agent.Title)
		}
		if agent.GroupID != "" {
			if _, ok := groupIDs[agent.GroupID]; !ok {
				agent.GroupID = ""
			}
		}
		if group, ok := groupIDs[agent.GroupID]; ok {
			agent.WorkspaceID = group.WorkspaceID
		}
		if !workspaces[agent.WorkspaceID] && len(st.Workspaces) > 0 {
			agent.WorkspaceID = st.Workspaces[0].ID
			agent.GroupID = ""
		}
		if strings.TrimSpace(agent.Title) == "" {
			agent.Title = DefaultAgentTitle
		}
		if strings.TrimSpace(agent.TypeID) == "" {
			agent.TypeID = DefaultAgentTypeID
		}
		agent.CodexStatus = strings.TrimSpace(agent.CodexStatus)
		if agent.Status == "" {
			agent.Status = StatusStopped
		}
		if agent.CreatedAt == "" {
			agent.CreatedAt = NowISO()
		}
		if agent.UpdatedAt == "" {
			agent.UpdatedAt = agent.CreatedAt
		}
	}

	if st.ActiveAgentID != "" && AgentByID(st, st.ActiveAgentID) == nil {
		st.ActiveAgentID = ""
	}
	if st.SelectedAgentID != "" && AgentByID(st, st.SelectedAgentID) == nil {
		st.SelectedAgentID = ""
	}
	if st.SelectedWorkspaceID == "" || WorkspaceByID(st, st.SelectedWorkspaceID) == nil {
		if len(st.Workspaces) > 0 {
			st.SelectedWorkspaceID = st.Workspaces[0].ID
		} else {
			st.SelectedWorkspaceID = ""
		}
	}
	if st.SelectedGroupID != "" && (GroupByID(st, st.SelectedGroupID) == nil || groupWorkspace(st, st.SelectedGroupID) != st.SelectedWorkspaceID) {
		st.SelectedGroupID = ""
	}
	if selected := AgentByID(st, st.SelectedAgentID); selected != nil {
		if selected.WorkspaceID != st.SelectedWorkspaceID {
			st.SelectedAgentID = ""
		} else {
			st.SelectedGroupID = selected.GroupID
		}
	}

	if st.NavOpen {
		if st.Focus != FocusWorkspaces && st.Focus != FocusAgents {
			st.Focus = FocusAgents
		}
	} else {
		st.Focus = FocusCodex
	}
	if st.ActiveAgentID == "" {
		st.NavOpen = true
		if st.Focus == FocusCodex {
			st.Focus = FocusAgents
		}
	}
	if st.Focus == "" {
		if st.NavOpen {
			st.Focus = FocusAgents
		} else {
			st.Focus = FocusCodex
		}
	}
	if st.SelectedAgentID == "" && st.ActiveAgentID != "" {
		active := AgentByID(st, st.ActiveAgentID)
		if active != nil && active.WorkspaceID == st.SelectedWorkspaceID && (st.Focus == FocusCodex || !st.NavOpen || st.SelectedGroupID == "") {
			st.SelectedAgentID = active.ID
			st.SelectedGroupID = active.GroupID
		}
	}
	return st
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

func ActiveAgent(st State) *Agent {
	return AgentByID(st, st.ActiveAgentID)
}

func AgentByID(st State, agentID string) *Agent {
	if agentID == "" {
		return nil
	}
	for index := range st.Agents {
		if st.Agents[index].ID == agentID {
			return &st.Agents[index]
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

func WorkspaceForAgent(st State, agent Agent) *Workspace {
	return WorkspaceByID(st, agent.WorkspaceID)
}

func GroupForAgent(st State, agent Agent) *Group {
	return GroupByID(st, agent.GroupID)
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

func AgentsForGroup(st State, groupID string) []Agent {
	if groupID == "" {
		return nil
	}
	var agents []Agent
	for _, agent := range st.Agents {
		if agent.GroupID == groupID {
			agents = append(agents, agent)
		}
	}
	return agents
}

func UngroupedAgentsForWorkspace(st State, workspaceID string) []Agent {
	var agents []Agent
	for _, agent := range st.Agents {
		if agent.WorkspaceID == workspaceID && agent.GroupID == "" {
			agents = append(agents, agent)
		}
	}
	return agents
}

func AgentCountForGroup(st State, groupID string) int {
	if groupID == "" {
		return 0
	}
	count := 0
	for _, agent := range st.Agents {
		if agent.GroupID == groupID {
			count++
		}
	}
	return count
}

func WithUpdatedAgent(st State, agentID string, update func(Agent) Agent) State {
	for index, agent := range st.Agents {
		if agent.ID == agentID {
			updated := update(agent)
			updated.UpdatedAt = NowISO()
			st.Agents[index] = updated
			break
		}
	}
	return st
}

func CloseAgent(st State, agentID string) State {
	index := -1
	removed := Agent{}
	for i, agent := range st.Agents {
		if agent.ID == agentID {
			index = i
			removed = agent
			break
		}
	}
	if index < 0 {
		return st
	}
	workspaceIndex := 0
	for i, agent := range st.Agents {
		if i == index {
			break
		}
		if agent.WorkspaceID == removed.WorkspaceID {
			workspaceIndex++
		}
	}
	st.Agents = append(st.Agents[:index], st.Agents[index+1:]...)
	if st.SelectedAgentID == agentID {
		st.SelectedAgentID = ""
	}
	if st.ActiveAgentID != agentID {
		return st
	}
	st.ActiveAgentID = ""
	candidates := agentsForWorkspace(st, removed.WorkspaceID)
	if len(candidates) > 0 {
		nextIndex := workspaceIndex
		if nextIndex >= len(candidates) {
			nextIndex = len(candidates) - 1
		}
		next := candidates[nextIndex]
		st.ActiveAgentID = next.ID
		st.SelectedAgentID = next.ID
		st.SelectedWorkspaceID = next.WorkspaceID
		st.SelectedGroupID = next.GroupID
	} else {
		st.ActiveAgentID = ""
		st.SelectedAgentID = ""
		st.NavOpen = true
		st.Focus = FocusAgents
		st.SelectedWorkspaceID = removed.WorkspaceID
		st.SelectedGroupID = removed.GroupID
	}
	return st
}

func ReorderAgent(st State, agentID string, delta int) (State, bool, error) {
	if delta == 0 {
		return st, false, nil
	}
	index := -1
	selected := Agent{}
	for i, agent := range st.Agents {
		if agent.ID == agentID {
			index = i
			selected = agent
			break
		}
	}
	if index < 0 {
		return st, false, fmt.Errorf("agent not found")
	}
	if delta < 0 {
		for i := index - 1; i >= 0; i-- {
			if sameAgentOrderArea(st.Agents[i], selected) {
				return swapAgentsAndSelect(st, index, i, selected), true, nil
			}
		}
	} else {
		for i := index + 1; i < len(st.Agents); i++ {
			if sameAgentOrderArea(st.Agents[i], selected) {
				return swapAgentsAndSelect(st, index, i, selected), true, nil
			}
		}
	}
	return moveAgentToAdjacentArea(st, index, selected, delta)
}

func swapAgentsAndSelect(st State, index int, target int, selected Agent) State {
	st.Agents[index], st.Agents[target] = st.Agents[target], st.Agents[index]
	now := NowISO()
	st.Agents[index].UpdatedAt = now
	st.Agents[target].UpdatedAt = now
	st.ActiveAgentID = selected.ID
	st.SelectedAgentID = selected.ID
	st.SelectedWorkspaceID = selected.WorkspaceID
	st.SelectedGroupID = selected.GroupID
	return st
}

func sameAgentOrderArea(left Agent, right Agent) bool {
	return left.WorkspaceID == right.WorkspaceID && left.GroupID == right.GroupID
}

func moveAgentToAdjacentArea(st State, index int, selected Agent, delta int) (State, bool, error) {
	areas := agentOrderAreas(st, selected.WorkspaceID)
	currentArea := areaIndex(areas, selected.GroupID)
	if currentArea < 0 {
		return st, false, fmt.Errorf("agent group not found")
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
	agents := append([]Agent{}, st.Agents[:index]...)
	agents = append(agents, st.Agents[index+1:]...)
	insertAt := agentAreaInsertIndex(agents, selected.WorkspaceID, targetGroupID, areas, targetArea, delta > 0)
	agents = append(agents, Agent{})
	copy(agents[insertAt+1:], agents[insertAt:])
	agents[insertAt] = selected
	st.Agents = agents
	st.ActiveAgentID = selected.ID
	st.SelectedAgentID = selected.ID
	st.SelectedWorkspaceID = selected.WorkspaceID
	st.SelectedGroupID = selected.GroupID
	return st, true, nil
}

func agentOrderAreas(st State, workspaceID string) []string {
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

func agentAreaInsertIndex(agents []Agent, workspaceID string, targetGroupID string, areas []string, targetArea int, atStart bool) int {
	if atStart {
		for index, agent := range agents {
			if agent.WorkspaceID == workspaceID && agent.GroupID == targetGroupID {
				return index
			}
		}
	} else {
		for index := len(agents) - 1; index >= 0; index-- {
			if agents[index].WorkspaceID == workspaceID && agents[index].GroupID == targetGroupID {
				return index + 1
			}
		}
	}
	insertAt := len(agents)
	for index, agent := range agents {
		if agent.WorkspaceID != workspaceID {
			continue
		}
		agentArea := areaIndex(areas, agent.GroupID)
		if agentArea < 0 {
			continue
		}
		if agentArea > targetArea {
			return index
		}
		if agentArea < targetArea {
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
	st.SelectedAgentID = ""
	return st, true, nil
}

func MoveAgent(st State, agentID string, groupID string) (State, error) {
	var target *Group
	if groupID != "" {
		target = GroupByID(st, groupID)
		if target == nil {
			return st, fmt.Errorf("group not found")
		}
	}
	for index, agent := range st.Agents {
		if agent.ID != agentID {
			continue
		}
		if target != nil && agent.WorkspaceID != target.WorkspaceID {
			return st, fmt.Errorf("cross-workspace moves are not supported")
		}
		st.Agents[index].GroupID = groupID
		st.Agents[index].UpdatedAt = NowISO()
		st.SelectedAgentID = agentID
		st.SelectedGroupID = groupID
		return st, nil
	}
	return st, fmt.Errorf("agent not found")
}

func AddWorkspace(st State, id string, path string, now string) (State, Workspace, error) {
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
	if id == "" {
		id = StableID("workspace", path)
	}
	if now == "" {
		now = NowISO()
	}
	workspace := Workspace{ID: id, Path: path, CreatedAt: now, UpdatedAt: now}
	st.Workspaces = append(st.Workspaces, workspace)
	st.SelectedWorkspaceID = id
	st.SelectedGroupID = ""
	st.NavOpen = true
	st.Focus = FocusAgents
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
	st.SelectedAgentID = ""
	st.SelectedGroupID = ""
	if groups := GroupsForWorkspace(st, workspaceID); len(groups) > 0 {
		st.SelectedGroupID = groups[0].ID
	}
	return st
}

func SelectWorkspaceByPath(st State, path string) (State, bool) {
	workspace := WorkspaceByPath(st, path)
	if workspace == nil {
		return st, false
	}
	return SelectWorkspace(st, workspace.ID), true
}

func RemoveWorkspace(st State, workspaceID string) (State, []Agent, error) {
	if WorkspaceByID(st, workspaceID) == nil {
		return st, nil, fmt.Errorf("workspace not found")
	}
	var removed []Agent
	var agents []Agent
	for _, agent := range st.Agents {
		if agent.WorkspaceID == workspaceID {
			removed = append(removed, agent)
			continue
		}
		agents = append(agents, agent)
	}
	st.Agents = agents
	st.Groups = filterGroups(st.Groups, func(group Group) bool { return group.WorkspaceID != workspaceID })
	st.Workspaces = filterWorkspaces(st.Workspaces, func(workspace Workspace) bool { return workspace.ID != workspaceID })
	if st.ActiveAgentID != "" {
		if AgentByID(st, st.ActiveAgentID) == nil {
			st.ActiveAgentID = ""
		}
	}
	if st.SelectedAgentID != "" && AgentByID(st, st.SelectedAgentID) == nil {
		st.SelectedAgentID = ""
	}
	st.SelectedWorkspaceID = ""
	st.SelectedGroupID = ""
	st.NavOpen = true
	st.Focus = FocusWorkspaces
	return Repair(st, ""), removed, nil
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

func AddGroup(st State, id string, workspaceID string, path string, now string) (State, Group, error) {
	return AddGroupWithSilent(st, id, workspaceID, path, now, false)
}

func AddGroupWithSilent(st State, id string, workspaceID string, path string, now string, silent bool) (State, Group, error) {
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
	if id == "" {
		id = StableID("group", workspaceID, path)
	}
	if now == "" {
		now = NowISO()
	}
	group := Group{ID: id, WorkspaceID: workspaceID, Path: path, Silent: silent, CreatedAt: now, UpdatedAt: now}
	st.Groups = append(st.Groups, group)
	st.SelectedWorkspaceID = workspaceID
	st.SelectedGroupID = id
	st.NavOpen = true
	st.Focus = FocusAgents
	return st, group, nil
}

func RenameGroup(st State, groupID string, path string) (State, error) {
	group := GroupByID(st, groupID)
	if group == nil {
		return st, fmt.Errorf("group not found")
	}
	return EditGroup(st, groupID, path, group.Silent)
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
	if AgentCountForGroup(st, groupID) > 0 {
		return st, fmt.Errorf("group is not empty")
	}
	st.Groups = filterGroups(st.Groups, func(group Group) bool { return group.ID != groupID })
	st.CollapsedGroupIDs = removeString(st.CollapsedGroupIDs, groupID)
	if st.SelectedGroupID == groupID {
		st.SelectedGroupID = ""
	}
	if selected := AgentByID(st, st.SelectedAgentID); selected != nil && selected.GroupID == groupID {
		st.SelectedAgentID = ""
	}
	return Repair(st, ""), nil
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

func AddAgent(st State, id string, workspaceID string, groupID string, title string, now string) (State, Agent, error) {
	return AddAgentWithType(st, id, workspaceID, groupID, DefaultAgentTypeID, title, now)
}

func AddAgentWithType(st State, id string, workspaceID string, groupID string, typeID string, title string, now string) (State, Agent, error) {
	if WorkspaceByID(st, workspaceID) == nil {
		return st, Agent{}, fmt.Errorf("workspace not found")
	}
	if groupID != "" {
		group := GroupByID(st, groupID)
		if group == nil || group.WorkspaceID != workspaceID {
			return st, Agent{}, fmt.Errorf("group not found")
		}
	}
	if strings.TrimSpace(title) == "" {
		title = DefaultAgentTitle
	}
	typeID = strings.TrimSpace(typeID)
	if typeID == "" {
		typeID = DefaultAgentTypeID
	}
	if id == "" {
		id = StableID("agent", workspaceID, groupID, typeID, now, title)
	}
	if now == "" {
		now = NowISO()
	}
	agent := Agent{
		ID: id, WorkspaceID: workspaceID, GroupID: groupID,
		TypeID: typeID, Title: title, Status: StatusStarting, CreatedAt: now, UpdatedAt: now,
	}
	st.Agents = append(st.Agents, agent)
	st.ActiveAgentID = id
	st.SelectedAgentID = id
	st.SelectedWorkspaceID = workspaceID
	st.SelectedGroupID = groupID
	st.Focus = FocusCodex
	st.NavOpen = false
	return st, agent, nil
}

func AgentTypeID(agent Agent) string {
	if id := strings.TrimSpace(agent.TypeID); id != "" {
		return id
	}
	return DefaultAgentTypeID
}

func RenameAgent(st State, agentID string, title string) (State, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return st, fmt.Errorf("title cannot be empty")
	}
	if AgentByID(st, agentID) == nil {
		return st, fmt.Errorf("agent not found")
	}
	return WithUpdatedAgent(st, agentID, func(agent Agent) Agent {
		agent.Title = title
		return agent
	}), nil
}

func groupWorkspace(st State, groupID string) string {
	if group := GroupByID(st, groupID); group != nil {
		return group.WorkspaceID
	}
	return ""
}

func StableID(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:12]
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

func validCollapsedGroupIDs(ids []string, groups map[string]Group) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		if _, ok := groups[id]; !ok {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func agentsForWorkspace(st State, workspaceID string) []Agent {
	var agents []Agent
	for _, agent := range st.Agents {
		if agent.WorkspaceID == workspaceID {
			agents = append(agents, agent)
		}
	}
	return agents
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
