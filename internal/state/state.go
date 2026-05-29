package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

const Version = 2

type Focus string

const (
	FocusNav   Focus = "nav"
	FocusCodex Focus = "codex"
)

type TabStatus string

const (
	StatusStarting TabStatus = "starting"
	StatusRunning  TabStatus = "running"
	StatusStopped  TabStatus = "stopped"
	StatusError    TabStatus = "error"
)

type Tab struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Column     string    `json:"column"`
	CreatedAt  string    `json:"created_at"`
	UpdatedAt  string    `json:"updated_at"`
	CodexTitle string    `json:"codex_title,omitempty"`
	Status     TabStatus `json:"status"`
}

type State struct {
	Version     int    `json:"version"`
	ActiveTabID string `json:"active_tab_id,omitempty"`
	Focus       Focus  `json:"focus"`
	Tabs        []Tab  `json:"tabs"`
}

type Store struct {
	Path     string
	LockPath string
}

type Migration struct {
	ArchivedPath string
	Message      string
}

func NewStore(path string) *Store {
	return &Store{Path: path, LockPath: path + ".lock"}
}

func Empty() State {
	return State{Version: Version, Focus: FocusCodex, Tabs: []Tab{}}
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
			loaded = Empty()
			return writeJSONAtomic(s.Path, loaded)
		}
		raw, err := os.ReadFile(s.Path)
		if err != nil {
			return err
		}
		if isLegacyTmuxState(raw) {
			archived, err := archiveLegacyState(s.Path)
			if err != nil {
				return err
			}
			loaded = Empty()
			if err := writeJSONAtomic(s.Path, loaded); err != nil {
				return err
			}
			migration = &Migration{
				ArchivedPath: archived,
				Message:      fmt.Sprintf("archived old tmux-pane state to %s; starting with clean TUI-owned PTYs", archived),
			}
			return nil
		}
		loaded, err = parseState(raw)
		return err
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
		next.Version = Version
		if next.Focus == "" {
			next.Focus = FocusCodex
		}
		if next.Tabs == nil {
			next.Tabs = []Tab{}
		}
		return writeJSONAtomic(s.Path, next)
	})
}

func (s *Store) Update(mutator func(State) State) (State, error) {
	var next State
	err := withFileLock(s.LockPath, func() error {
		current := Empty()
		raw, err := os.ReadFile(s.Path)
		if err == nil {
			current, err = parseState(raw)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		next = mutator(current)
		next.Version = Version
		if next.Focus == "" {
			next.Focus = FocusCodex
		}
		if next.Tabs == nil {
			next.Tabs = []Tab{}
		}
		return writeJSONAtomic(s.Path, next)
	})
	return next, err
}

func parseState(raw []byte) (State, error) {
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, fmt.Errorf("could not parse state: %w", err)
	}
	if st.Version != Version {
		return State{}, fmt.Errorf("unsupported state version %d", st.Version)
	}
	if st.Focus != FocusNav && st.Focus != FocusCodex {
		st.Focus = FocusCodex
	}
	if st.Tabs == nil {
		st.Tabs = []Tab{}
	}
	validIDs := map[string]bool{}
	for index := range st.Tabs {
		validIDs[st.Tabs[index].ID] = true
		if st.Tabs[index].Status == "" {
			st.Tabs[index].Status = StatusStopped
		}
	}
	if st.ActiveTabID != "" && !validIDs[st.ActiveTabID] {
		st.ActiveTabID = ""
	}
	if st.ActiveTabID == "" && len(st.Tabs) > 0 {
		st.ActiveTabID = st.Tabs[0].ID
	}
	return st, nil
}

func isLegacyTmuxState(raw []byte) bool {
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	if version, ok := probe["version"]; ok {
		return version != float64(Version)
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
	return true
}

func archiveLegacyState(path string) (string, error) {
	target := filepath.Join(filepath.Dir(path), "state.v1-tmux.json")
	if _, err := os.Stat(target); err == nil {
		target = filepath.Join(
			filepath.Dir(path),
			fmt.Sprintf("state.v1-tmux.%s.json", time.Now().UTC().Format("20060102T150405")),
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

func ActiveTab(st State) *Tab {
	if st.ActiveTabID == "" {
		return nil
	}
	for index := range st.Tabs {
		if st.Tabs[index].ID == st.ActiveTabID {
			return &st.Tabs[index]
		}
	}
	return nil
}

func WithUpdatedTab(st State, tabID string, update func(Tab) Tab) State {
	for index, tab := range st.Tabs {
		if tab.ID == tabID {
			updated := update(tab)
			updated.UpdatedAt = NowISO()
			st.Tabs[index] = updated
			break
		}
	}
	return st
}

func CloseTab(st State, tabID string) State {
	index := -1
	for i, tab := range st.Tabs {
		if tab.ID == tabID {
			index = i
			break
		}
	}
	if index < 0 {
		return st
	}
	st.Tabs = append(st.Tabs[:index], st.Tabs[index+1:]...)
	if len(st.Tabs) == 0 {
		st.ActiveTabID = ""
		st.Focus = FocusNav
		return st
	}
	if st.ActiveTabID == tabID {
		nextIndex := index
		if nextIndex >= len(st.Tabs) {
			nextIndex = len(st.Tabs) - 1
		}
		st.ActiveTabID = st.Tabs[nextIndex].ID
	}
	return st
}

func MoveActiveColumn(st State, columns []string, delta int) State {
	active := ActiveTab(st)
	if active == nil {
		return st
	}
	current := 0
	for index, column := range columns {
		if active.Column == column {
			current = index
			break
		}
	}
	next := current + delta
	if next < 0 {
		next = 0
	}
	if next >= len(columns) {
		next = len(columns) - 1
	}
	return WithUpdatedTab(st, active.ID, func(tab Tab) Tab {
		tab.Column = columns[next]
		return tab
	})
}

func RepairColumns(st State, columns []string) State {
	if len(columns) == 0 {
		return st
	}
	allowed := map[string]bool{}
	for _, column := range columns {
		allowed[column] = true
	}
	for index := range st.Tabs {
		if !allowed[st.Tabs[index].Column] {
			st.Tabs[index].Column = columns[0]
			st.Tabs[index].UpdatedAt = NowISO()
		}
	}
	return st
}

func SortTabsByColumn(tabs []Tab, columns []string) []Tab {
	columnIndex := map[string]int{}
	for index, column := range columns {
		columnIndex[column] = index
	}
	out := append([]Tab(nil), tabs...)
	sort.SliceStable(out, func(i, j int) bool {
		left := columnIndex[out[i].Column]
		right := columnIndex[out[j].Column]
		if left == right {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return left < right
	})
	return out
}
