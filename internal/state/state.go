package state

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const Version = 3

const DefaultFolderPath = "inbox"
const DefaultAgentTitle = "{codex}"

type Focus string

const (
	FocusWorkdirs Focus = "workdirs"
	FocusFolders  Focus = "folders"
	FocusCodex    Focus = "codex"
)

type AgentStatus string

const (
	StatusStarting AgentStatus = "starting"
	StatusRunning  AgentStatus = "running"
	StatusReady    AgentStatus = "ready"
	StatusSitting  AgentStatus = "sitting"
	StatusShipping AgentStatus = "shipping"
	StatusStopped  AgentStatus = "stopped"
	StatusError    AgentStatus = "error"
)

type Workdir struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Title     string `json:"title,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Folder struct {
	ID        string `json:"id"`
	WorkdirID string `json:"workdir_id"`
	Path      string `json:"path"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Agent struct {
	ID                 string      `json:"id"`
	WorkdirID          string      `json:"workdir_id"`
	FolderID           string      `json:"folder_id"`
	Title              string      `json:"title"`
	AutoTitle          string      `json:"auto_title,omitempty"`
	AutoTitleAttempted bool        `json:"auto_title_attempted,omitempty"`
	AutoTitleError     string      `json:"auto_title_error,omitempty"`
	CodexTitle         string      `json:"codex_title,omitempty"`
	Status             AgentStatus `json:"status"`
	CreatedAt          string      `json:"created_at"`
	UpdatedAt          string      `json:"updated_at"`
}

type State struct {
	Version           int       `json:"version"`
	ActiveAgentID     string    `json:"active_agent_id,omitempty"`
	SelectedWorkdirID string    `json:"selected_workdir_id,omitempty"`
	SelectedFolderID  string    `json:"selected_folder_id,omitempty"`
	Focus             Focus     `json:"focus"`
	NavOpen           bool      `json:"nav_open"`
	Workdirs          []Workdir `json:"workdirs"`
	Folders           []Folder  `json:"folders"`
	Agents            []Agent   `json:"agents"`
	CollapsedGroupIDs []string  `json:"collapsed_group_ids,omitempty"`
}

type Store struct {
	Path     string
	LockPath string
	Workdir  string
}

type Migration struct {
	ArchivedPath string
	Message      string
}

func NewStore(path string, workdir ...string) *Store {
	current := ""
	if len(workdir) > 0 {
		current = workdir[0]
	}
	return &Store{Path: path, LockPath: path + ".lock", Workdir: current}
}

func Empty() State {
	return State{Version: Version, Focus: FocusWorkdirs, NavOpen: true, Workdirs: []Workdir{}, Folders: []Folder{}, Agents: []Agent{}, CollapsedGroupIDs: []string{}}
}

func NowISO() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func (s *Store) Ensure() (State, *Migration, error) {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return State{}, nil, err
	}
	if err := ensureLockFile(s.LockPath); err != nil {
		return State{}, nil, err
	}
	var loaded State
	var migration *Migration
	err := withFileLock(s.LockPath, func() error {
		if _, err := os.Stat(s.Path); errors.Is(err, os.ErrNotExist) {
			loaded = Repair(Empty(), s.Workdir)
			return writeJSONAtomic(s.Path, loaded)
		}
		raw, err := os.ReadFile(s.Path)
		if err != nil {
			return err
		}
		switch {
		case isLegacyTmuxState(raw):
			archived, err := archiveState(s.Path, "v1-tmux")
			if err != nil {
				return err
			}
			loaded = Repair(Empty(), s.Workdir)
			if err := writeJSONAtomic(s.Path, loaded); err != nil {
				return err
			}
			migration = &Migration{
				ArchivedPath: archived,
				Message:      fmt.Sprintf("archived old tmux-pane state to %s; starting with clean supervisor-owned PTYs", archived),
			}
			return nil
		case isTabState(raw):
			migrated, err := migrateTabState(raw, s.Workdir)
			if err != nil {
				return err
			}
			archived, err := archiveState(s.Path, "v2-tabs")
			if err != nil {
				return err
			}
			loaded = Repair(migrated, s.Workdir)
			if err := writeJSONAtomic(s.Path, loaded); err != nil {
				return err
			}
			migration = &Migration{
				ArchivedPath: archived,
				Message:      fmt.Sprintf("migrated old tabs/columns state; archived original to %s", archived),
			}
			return nil
		default:
			loaded, err = parseState(raw, s.Workdir)
			if err != nil {
				return err
			}
			repaired := Repair(loaded, s.Workdir)
			loaded = repaired
			return writeJSONAtomic(s.Path, repaired)
		}
	})
	return loaded, migration, err
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
			loaded = Repair(Empty(), s.Workdir)
			return nil
		}
		if err != nil {
			return err
		}
		var parseErr error
		if isTabState(raw) && !isLegacyTmuxState(raw) {
			loaded, parseErr = migrateTabState(raw, s.Workdir)
			loaded = Repair(loaded, s.Workdir)
			return parseErr
		}
		loaded, parseErr = parseState(raw, s.Workdir)
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
		return writeJSONAtomic(s.Path, Repair(next, s.Workdir))
	})
}

func (s *Store) Update(mutator func(State) State) (State, error) {
	var next State
	err := withFileLock(s.LockPath, func() error {
		current := Repair(Empty(), s.Workdir)
		raw, err := os.ReadFile(s.Path)
		if err == nil {
			current, err = parseState(raw, s.Workdir)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		next = Repair(mutator(current), s.Workdir)
		return writeJSONAtomic(s.Path, next)
	})
	return next, err
}

func parseState(raw []byte, fallbackWorkdir string) (State, error) {
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, fmt.Errorf("could not parse state: %w", err)
	}
	if st.Version != Version {
		return State{}, fmt.Errorf("unsupported state version %d", st.Version)
	}
	return Repair(st, fallbackWorkdir), nil
}

func Repair(st State, fallbackWorkdir string) State {
	st.Version = Version
	if st.Workdirs == nil {
		st.Workdirs = []Workdir{}
	}
	if st.Folders == nil {
		st.Folders = []Folder{}
	}
	if st.Agents == nil {
		st.Agents = []Agent{}
	}
	if st.CollapsedGroupIDs == nil {
		st.CollapsedGroupIDs = []string{}
	}
	workdirs := map[string]bool{}
	for index := range st.Workdirs {
		if strings.TrimSpace(st.Workdirs[index].ID) == "" {
			st.Workdirs[index].ID = StableID("workdir", st.Workdirs[index].Path)
		}
		st.Workdirs[index].Path = absolutePath(st.Workdirs[index].Path)
		st.Workdirs[index].Title = strings.TrimSpace(st.Workdirs[index].Title)
		if st.Workdirs[index].CreatedAt == "" {
			st.Workdirs[index].CreatedAt = NowISO()
		}
		if st.Workdirs[index].UpdatedAt == "" {
			st.Workdirs[index].UpdatedAt = st.Workdirs[index].CreatedAt
		}
		workdirs[st.Workdirs[index].ID] = true
	}

	for index := range st.Folders {
		if !workdirs[st.Folders[index].WorkdirID] && len(st.Workdirs) > 0 {
			st.Folders[index].WorkdirID = st.Workdirs[0].ID
		}
		if strings.TrimSpace(st.Folders[index].Path) == "" {
			st.Folders[index].Path = DefaultFolderPath
		}
		if strings.TrimSpace(st.Folders[index].ID) == "" {
			st.Folders[index].ID = StableID("folder", st.Folders[index].WorkdirID, st.Folders[index].Path)
		}
		if st.Folders[index].CreatedAt == "" {
			st.Folders[index].CreatedAt = NowISO()
		}
		if st.Folders[index].UpdatedAt == "" {
			st.Folders[index].UpdatedAt = st.Folders[index].CreatedAt
		}
	}

	folderIDs := map[string]Folder{}
	for _, folder := range st.Folders {
		folderIDs[folder.ID] = folder
	}
	st.CollapsedGroupIDs = validCollapsedGroupIDs(st.CollapsedGroupIDs, folderIDs)
	for index := range st.Agents {
		agent := &st.Agents[index]
		if strings.TrimSpace(agent.ID) == "" {
			agent.ID = StableID("agent", agent.WorkdirID, agent.FolderID, agent.CreatedAt, agent.Title)
		}
		if agent.FolderID != "" {
			if _, ok := folderIDs[agent.FolderID]; !ok {
				agent.FolderID = ""
			}
		}
		if folder, ok := folderIDs[agent.FolderID]; ok {
			agent.WorkdirID = folder.WorkdirID
		}
		if !workdirs[agent.WorkdirID] && len(st.Workdirs) > 0 {
			agent.WorkdirID = st.Workdirs[0].ID
			agent.FolderID = ""
		}
		if strings.TrimSpace(agent.Title) == "" {
			agent.Title = DefaultAgentTitle
		}
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
	if st.SelectedWorkdirID == "" || WorkdirByID(st, st.SelectedWorkdirID) == nil {
		if len(st.Workdirs) > 0 {
			st.SelectedWorkdirID = st.Workdirs[0].ID
		} else {
			st.SelectedWorkdirID = ""
		}
	}
	if st.SelectedFolderID != "" && (FolderByID(st, st.SelectedFolderID) == nil || folderWorkdir(st, st.SelectedFolderID) != st.SelectedWorkdirID) {
		st.SelectedFolderID = ""
	}

	if st.NavOpen {
		if st.Focus != FocusWorkdirs && st.Focus != FocusFolders {
			st.Focus = FocusFolders
		}
	} else {
		st.Focus = FocusCodex
	}
	if st.ActiveAgentID == "" {
		st.NavOpen = true
		if st.Focus == FocusCodex {
			st.Focus = FocusFolders
		}
	}
	if st.Focus == "" {
		if st.NavOpen {
			st.Focus = FocusFolders
		} else {
			st.Focus = FocusCodex
		}
	}
	return st
}

func isTabState(raw []byte) bool {
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	if _, ok := probe["tabs"]; ok {
		if _, newState := probe["workdirs"]; !newState {
			return true
		}
	}
	return false
}

func isLegacyTmuxState(raw []byte) bool {
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	tabs, ok := probe["tabs"].([]any)
	if !ok {
		return false
	}
	for _, item := range tabs {
		tab, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := tab["tmux_window_id"]; ok {
			return true
		}
		if _, ok := tab["tmux_pane_id"]; ok {
			return true
		}
	}
	return false
}

type oldTabState struct {
	Version     int      `json:"version"`
	ActiveTabID string   `json:"active_tab_id,omitempty"`
	Focus       Focus    `json:"focus"`
	Tabs        []oldTab `json:"tabs"`
}

type oldTab struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Column     string      `json:"column"`
	CreatedAt  string      `json:"created_at"`
	UpdatedAt  string      `json:"updated_at"`
	CodexTitle string      `json:"codex_title,omitempty"`
	Status     AgentStatus `json:"status"`
}

func migrateTabState(raw []byte, currentWorkdir string) (State, error) {
	var old oldTabState
	if err := json.Unmarshal(raw, &old); err != nil {
		return State{}, fmt.Errorf("could not parse tabs state: %w", err)
	}
	now := NowISO()
	workdirPath := absolutePath(currentWorkdir)
	workdirID := StableID("workdir", workdirPath)
	st := State{
		Version:           Version,
		SelectedWorkdirID: workdirID,
		Focus:             FocusFolders,
		NavOpen:           true,
		Workdirs: []Workdir{{
			ID: workdirID, Path: workdirPath, CreatedAt: now, UpdatedAt: now,
		}},
		Folders: []Folder{},
		Agents:  []Agent{},
	}
	if old.ActiveTabID != "" {
		st.ActiveAgentID = old.ActiveTabID
		st.NavOpen = old.Focus != FocusCodex
		if !st.NavOpen {
			st.Focus = FocusCodex
		}
	}
	folderIDsByPath := map[string]string{}
	for _, tab := range old.Tabs {
		path := strings.TrimSpace(tab.Column)
		if path == "" {
			path = DefaultFolderPath
		}
		if _, ok := folderIDsByPath[path]; !ok {
			folderID := StableID("folder", workdirID, path)
			folderIDsByPath[path] = folderID
			st.Folders = append(st.Folders, Folder{
				ID: folderID, WorkdirID: workdirID, Path: path,
				CreatedAt: firstNonEmpty(tab.CreatedAt, now),
				UpdatedAt: firstNonEmpty(tab.UpdatedAt, tab.CreatedAt, now),
			})
		}
		agentID := tab.ID
		if agentID == "" {
			agentID = StableID("agent", workdirID, folderIDsByPath[path], tab.CreatedAt, tab.Title)
		}
		status := tab.Status
		if status == "" {
			status = StatusStopped
		}
		st.Agents = append(st.Agents, Agent{
			ID: agentID, WorkdirID: workdirID, FolderID: folderIDsByPath[path],
			Title: firstNonEmpty(tab.Title, "Codex"), CodexTitle: tab.CodexTitle, Status: status,
			CreatedAt: firstNonEmpty(tab.CreatedAt, now),
			UpdatedAt: firstNonEmpty(tab.UpdatedAt, tab.CreatedAt, now),
		})
	}
	if active := AgentByID(st, st.ActiveAgentID); active != nil {
		st.SelectedFolderID = active.FolderID
	} else if len(st.Folders) > 0 {
		st.SelectedFolderID = st.Folders[0].ID
	}
	return Repair(st, currentWorkdir), nil
}

func archiveState(path string, suffix string) (string, error) {
	target := filepath.Join(filepath.Dir(path), fmt.Sprintf("state.%s.json", suffix))
	if _, err := os.Stat(target); err == nil {
		target = filepath.Join(
			filepath.Dir(path),
			fmt.Sprintf("state.%s.%s.json", suffix, time.Now().UTC().Format("20060102T150405")),
		)
	}
	return target, os.Rename(path, target)
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

func WorkdirByID(st State, workdirID string) *Workdir {
	if workdirID == "" {
		return nil
	}
	for index := range st.Workdirs {
		if st.Workdirs[index].ID == workdirID {
			return &st.Workdirs[index]
		}
	}
	return nil
}

func WorkdirByPath(st State, path string) *Workdir {
	path = NormalizeWorkdirPath(path)
	for index := range st.Workdirs {
		if st.Workdirs[index].Path == path {
			return &st.Workdirs[index]
		}
	}
	return nil
}

func FolderByID(st State, folderID string) *Folder {
	if folderID == "" {
		return nil
	}
	for index := range st.Folders {
		if st.Folders[index].ID == folderID {
			return &st.Folders[index]
		}
	}
	return nil
}

func ActiveWorkdir(st State) *Workdir {
	return WorkdirByID(st, st.SelectedWorkdirID)
}

func ActiveFolder(st State) *Folder {
	return FolderByID(st, st.SelectedFolderID)
}

func WorkdirForAgent(st State, agent Agent) *Workdir {
	return WorkdirByID(st, agent.WorkdirID)
}

func FolderForAgent(st State, agent Agent) *Folder {
	return FolderByID(st, agent.FolderID)
}

func FoldersForWorkdir(st State, workdirID string) []Folder {
	var folders []Folder
	for _, folder := range st.Folders {
		if folder.WorkdirID == workdirID {
			folders = append(folders, folder)
		}
	}
	sort.SliceStable(folders, func(i, j int) bool {
		if folders[i].Path == folders[j].Path {
			return folders[i].CreatedAt < folders[j].CreatedAt
		}
		return folders[i].Path < folders[j].Path
	})
	return folders
}

func AgentsForFolder(st State, folderID string) []Agent {
	if folderID == "" {
		return nil
	}
	var agents []Agent
	for _, agent := range st.Agents {
		if agent.FolderID == folderID {
			agents = append(agents, agent)
		}
	}
	sort.SliceStable(agents, func(i, j int) bool {
		return agents[i].CreatedAt < agents[j].CreatedAt
	})
	return agents
}

func UngroupedAgentsForWorkdir(st State, workdirID string) []Agent {
	var agents []Agent
	for _, agent := range st.Agents {
		if agent.WorkdirID == workdirID && agent.FolderID == "" {
			agents = append(agents, agent)
		}
	}
	sort.SliceStable(agents, func(i, j int) bool {
		return agents[i].CreatedAt < agents[j].CreatedAt
	})
	return agents
}

func AgentCountForWorkdir(st State, workdirID string) int {
	count := 0
	for _, agent := range st.Agents {
		if agent.WorkdirID == workdirID {
			count++
		}
	}
	return count
}

func AgentCountForFolder(st State, folderID string) int {
	if folderID == "" {
		return 0
	}
	count := 0
	for _, agent := range st.Agents {
		if agent.FolderID == folderID {
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
	st.Agents = append(st.Agents[:index], st.Agents[index+1:]...)
	if st.ActiveAgentID != agentID {
		return st
	}
	st.ActiveAgentID = ""
	candidates := agentsForWorkdir(st, removed.WorkdirID)
	if len(candidates) > 0 {
		nextIndex := index
		for candidateIndex, agent := range candidates {
			if agent.CreatedAt > removed.CreatedAt || candidateIndex == len(candidates)-1 {
				nextIndex = candidateIndex
				break
			}
		}
		if nextIndex >= len(candidates) {
			nextIndex = len(candidates) - 1
		}
		next := candidates[nextIndex]
		st.ActiveAgentID = next.ID
		st.SelectedWorkdirID = next.WorkdirID
		st.SelectedFolderID = next.FolderID
	} else {
		st.ActiveAgentID = ""
		st.NavOpen = true
		st.Focus = FocusFolders
		st.SelectedWorkdirID = removed.WorkdirID
		st.SelectedFolderID = removed.FolderID
	}
	return st
}

func MoveAgent(st State, agentID string, folderID string) (State, error) {
	var target *Folder
	if folderID != "" {
		target = FolderByID(st, folderID)
		if target == nil {
			return st, fmt.Errorf("group not found")
		}
	}
	for index, agent := range st.Agents {
		if agent.ID != agentID {
			continue
		}
		if target != nil && agent.WorkdirID != target.WorkdirID {
			return st, fmt.Errorf("cross-workspace moves are not supported")
		}
		st.Agents[index].FolderID = folderID
		st.Agents[index].UpdatedAt = NowISO()
		st.SelectedFolderID = folderID
		return st, nil
	}
	return st, fmt.Errorf("agent not found")
}

func AddWorkdir(st State, id string, path string, now string) (State, Workdir, error) {
	path = NormalizeWorkdirPath(path)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, Workdir{}, fmt.Errorf("workspace path does not exist: %s", path)
		}
		return st, Workdir{}, fmt.Errorf("cannot read workspace path %s: %w", path, err)
	}
	if !info.IsDir() {
		return st, Workdir{}, fmt.Errorf("workspace path is not a directory: %s", path)
	}
	if workdir := WorkdirByPath(st, path); workdir != nil {
		st = SelectWorkdir(st, workdir.ID)
		return st, *workdir, nil
	}
	if id == "" {
		id = StableID("workdir", path)
	}
	if now == "" {
		now = NowISO()
	}
	workdir := Workdir{ID: id, Path: path, CreatedAt: now, UpdatedAt: now}
	st.Workdirs = append(st.Workdirs, workdir)
	st.SelectedWorkdirID = id
	st.SelectedFolderID = ""
	st.NavOpen = true
	st.Focus = FocusFolders
	return st, workdir, nil
}

func SelectWorkdir(st State, workdirID string) State {
	if WorkdirByID(st, workdirID) == nil {
		return st
	}
	st.SelectedWorkdirID = workdirID
	st.SelectedFolderID = ""
	if folders := FoldersForWorkdir(st, workdirID); len(folders) > 0 {
		st.SelectedFolderID = folders[0].ID
	}
	return st
}

func SelectWorkdirByPath(st State, path string) (State, bool) {
	workdir := WorkdirByPath(st, path)
	if workdir == nil {
		return st, false
	}
	return SelectWorkdir(st, workdir.ID), true
}

func RemoveWorkdir(st State, workdirID string) (State, []Agent, error) {
	if WorkdirByID(st, workdirID) == nil {
		return st, nil, fmt.Errorf("workspace not found")
	}
	var removed []Agent
	var agents []Agent
	for _, agent := range st.Agents {
		if agent.WorkdirID == workdirID {
			removed = append(removed, agent)
			continue
		}
		agents = append(agents, agent)
	}
	st.Agents = agents
	st.Folders = filterFolders(st.Folders, func(folder Folder) bool { return folder.WorkdirID != workdirID })
	st.Workdirs = filterWorkdirs(st.Workdirs, func(workdir Workdir) bool { return workdir.ID != workdirID })
	if st.ActiveAgentID != "" {
		if AgentByID(st, st.ActiveAgentID) == nil {
			st.ActiveAgentID = ""
		}
	}
	st.SelectedWorkdirID = ""
	st.SelectedFolderID = ""
	st.NavOpen = true
	st.Focus = FocusWorkdirs
	return Repair(st, ""), removed, nil
}

func SetWorkdirTitle(st State, workdirID string, title string) (State, error) {
	title = strings.TrimSpace(title)
	if WorkdirByID(st, workdirID) == nil {
		return st, fmt.Errorf("workspace not found")
	}
	for index := range st.Workdirs {
		if st.Workdirs[index].ID == workdirID {
			st.Workdirs[index].Title = title
			st.Workdirs[index].UpdatedAt = NowISO()
			return st, nil
		}
	}
	return st, fmt.Errorf("workspace not found")
}

func AddFolder(st State, id string, workdirID string, path string, now string) (State, Folder, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return st, Folder{}, fmt.Errorf("group name is required")
	}
	if strings.Contains(path, "/") {
		return st, Folder{}, fmt.Errorf("group names cannot contain /")
	}
	if WorkdirByID(st, workdirID) == nil {
		return st, Folder{}, fmt.Errorf("workspace not found")
	}
	for _, folder := range FoldersForWorkdir(st, workdirID) {
		if folder.Path == path {
			return st, Folder{}, fmt.Errorf("group name already exists")
		}
	}
	if id == "" {
		id = StableID("folder", workdirID, path)
	}
	if now == "" {
		now = NowISO()
	}
	folder := Folder{ID: id, WorkdirID: workdirID, Path: path, CreatedAt: now, UpdatedAt: now}
	st.Folders = append(st.Folders, folder)
	st.SelectedWorkdirID = workdirID
	st.SelectedFolderID = id
	st.NavOpen = true
	st.Focus = FocusFolders
	return st, folder, nil
}

func RenameFolder(st State, folderID string, path string) (State, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return st, fmt.Errorf("group name is required")
	}
	if strings.Contains(path, "/") {
		return st, fmt.Errorf("group names cannot contain /")
	}
	folder := FolderByID(st, folderID)
	if folder == nil {
		return st, fmt.Errorf("group not found")
	}
	for _, other := range FoldersForWorkdir(st, folder.WorkdirID) {
		if other.ID != folderID && other.Path == path {
			return st, fmt.Errorf("group name already exists")
		}
	}
	for index := range st.Folders {
		if st.Folders[index].ID == folderID {
			st.Folders[index].Path = path
			st.Folders[index].UpdatedAt = NowISO()
			return st, nil
		}
	}
	return st, fmt.Errorf("group not found")
}

func DeleteFolder(st State, folderID string) (State, error) {
	if FolderByID(st, folderID) == nil {
		return st, fmt.Errorf("group not found")
	}
	if AgentCountForFolder(st, folderID) > 0 {
		return st, fmt.Errorf("group is not empty")
	}
	st.Folders = filterFolders(st.Folders, func(folder Folder) bool { return folder.ID != folderID })
	st.CollapsedGroupIDs = removeString(st.CollapsedGroupIDs, folderID)
	if st.SelectedFolderID == folderID {
		st.SelectedFolderID = ""
	}
	return Repair(st, ""), nil
}

func IsGroupCollapsed(st State, folderID string) bool {
	for _, id := range st.CollapsedGroupIDs {
		if id == folderID {
			return true
		}
	}
	return false
}

func ToggleGroupCollapsed(st State, folderID string) State {
	if FolderByID(st, folderID) == nil {
		return st
	}
	if IsGroupCollapsed(st, folderID) {
		st.CollapsedGroupIDs = removeString(st.CollapsedGroupIDs, folderID)
		return st
	}
	st.CollapsedGroupIDs = append(st.CollapsedGroupIDs, folderID)
	return st
}

func AddAgent(st State, id string, workdirID string, folderID string, title string, now string) (State, Agent, error) {
	if WorkdirByID(st, workdirID) == nil {
		return st, Agent{}, fmt.Errorf("workspace not found")
	}
	if folderID != "" {
		folder := FolderByID(st, folderID)
		if folder == nil || folder.WorkdirID != workdirID {
			return st, Agent{}, fmt.Errorf("group not found")
		}
	}
	if strings.TrimSpace(title) == "" {
		title = DefaultAgentTitle
	}
	if id == "" {
		id = StableID("agent", workdirID, folderID, now, title)
	}
	if now == "" {
		now = NowISO()
	}
	agent := Agent{
		ID: id, WorkdirID: workdirID, FolderID: folderID,
		Title: title, Status: StatusStarting, CreatedAt: now, UpdatedAt: now,
	}
	st.Agents = append(st.Agents, agent)
	st.ActiveAgentID = id
	st.SelectedWorkdirID = workdirID
	st.SelectedFolderID = folderID
	st.Focus = FocusCodex
	st.NavOpen = false
	return st, agent, nil
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

func folderWorkdir(st State, folderID string) string {
	if folder := FolderByID(st, folderID); folder != nil {
		return folder.WorkdirID
	}
	return ""
}

func StableID(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:12]
}

func NormalizeWorkdirPath(path string) string {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func validCollapsedGroupIDs(ids []string, folders map[string]Folder) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		if _, ok := folders[id]; !ok {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func agentsForWorkdir(st State, workdirID string) []Agent {
	var agents []Agent
	for _, agent := range st.Agents {
		if agent.WorkdirID == workdirID {
			agents = append(agents, agent)
		}
	}
	sort.SliceStable(agents, func(i, j int) bool {
		return agents[i].CreatedAt < agents[j].CreatedAt
	})
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

func filterFolders(folders []Folder, keep func(Folder) bool) []Folder {
	out := folders[:0]
	for _, folder := range folders {
		if keep(folder) {
			out = append(out, folder)
		}
	}
	return out
}

func filterWorkdirs(workdirs []Workdir, keep func(Workdir) bool) []Workdir {
	out := workdirs[:0]
	for _, workdir := range workdirs {
		if keep(workdir) {
			out = append(out, workdir)
		}
	}
	return out
}
