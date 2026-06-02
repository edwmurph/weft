package tui

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
)

const terminalUpgradeSnapshotDir = "terminal-upgrade-snapshots"

type terminalUpgradeSnapshot struct {
	TaskID    string `json:"task_id"`
	CWD       string `json:"cwd,omitempty"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

func (m *Model) terminalForegroundProcessActive(taskID string) bool {
	pty := m.ptys[taskID]
	return pty != nil && pty.ForegroundProcessActive()
}

func (m *Model) terminalForegroundTaskIDs() []string {
	ids := make([]string, 0, len(m.ptys))
	for _, task := range m.state.Tasks {
		if taskUsesCodexIntegration(m.cfg, task) || !m.terminalForegroundProcessActive(task.ID) {
			continue
		}
		ids = append(ids, task.ID)
	}
	return ids
}

func (m *Model) PrepareTerminalUpgradeSnapshots(taskIDs []string) error {
	if len(taskIDs) == 0 {
		return nil
	}
	dir := filepath.Join(m.runtime.Dir, terminalUpgradeSnapshotDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, taskID := range taskIDs {
		task := state.TaskByID(m.state, taskID)
		if task == nil {
			continue
		}
		screen := m.screens[taskID]
		text := terminalUpgradeSnapshotText(screen)
		cwd := terminalTaskCWD(m.cfg, *task)
		snapshot := terminalUpgradeSnapshot{
			TaskID:    taskID,
			CWD:       cwd,
			Text:      text,
			CreatedAt: state.NowISO(),
		}
		raw, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(terminalUpgradeSnapshotPath(m.runtime.Dir, taskID), append(raw, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) restoreTerminalUpgradeSnapshots() {
	dir := filepath.Join(m.runtime.Dir, terminalUpgradeSnapshotDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var snapshot terminalUpgradeSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			_ = os.Remove(path)
			continue
		}
		if state.TaskByID(m.state, snapshot.TaskID) != nil && strings.TrimSpace(snapshot.Text) != "" {
			screen := NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
			screen.Write(snapshot.Text)
			m.screens[snapshot.TaskID] = screen
			m.visible[snapshot.TaskID] = true
		}
		_ = os.Remove(path)
	}
}

func terminalUpgradeSnapshotPath(runtimeDir string, taskID string) string {
	name := hex.EncodeToString([]byte(taskID)) + ".json"
	return filepath.Join(runtimeDir, terminalUpgradeSnapshotDir, name)
}

func terminalUpgradeSnapshotText(screen *TerminalScreen) string {
	var lines []string
	if screen != nil {
		for _, line := range strings.Split(strings.ReplaceAll(screen.ScrollbackString(), "\r", ""), "\n") {
			lines = append(lines, strings.TrimRight(line, " "))
		}
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
	}
	lines = append(lines,
		"",
		"[Weft restarted this idle shell task with saved history/cwd.]",
		"[Jobs, env mutations, shell variables, and unsubmitted input were not preserved.]",
		"",
	)
	return strings.Join(lines, "\r\n")
}

func terminalTaskCWD(cfg config.Config, task state.Task) string {
	taskType, ok := cfg.TaskType(state.TaskTypeID(task))
	if !ok || taskType.Kind != config.TaskKindTerminal {
		return ""
	}
	cwd := strings.TrimSpace(task.TerminalCWD)
	if cwd == "" {
		return ""
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return ""
	}
	return cwd
}

func TerminalTaskIDs(tasks []state.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if strings.TrimSpace(task.ID) != "" {
			ids = append(ids, task.ID)
		}
	}
	return ids
}
