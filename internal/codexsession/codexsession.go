package codexsession

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/tasktypes"
	"github.com/edwmurph/weft/internal/titles"
)

const assignmentLookback = 2 * time.Minute

type Session struct {
	ID        string
	CWD       string
	Timestamp time.Time
	Path      string
}

type Report struct {
	Total         int
	Ready         int
	Fresh         int
	Assigned      int
	Busy          []state.Task
	Missing       []state.Task
	TerminalReady []state.Task
	TerminalBusy  []state.Task
}

func (r Report) CanUpgrade() bool {
	return len(r.Busy) == 0 &&
		len(r.Missing) == 0 &&
		len(r.TerminalBusy) == 0
}

func PrepareResumeState(st state.State, fallbackWorkspace string) (state.State, Report) {
	next, assigned := assignMissingSessionIDs(st, fallbackWorkspace, true)
	report := BuildReport(next)
	report.Assigned = assigned
	return next, report
}

func AssignMissingSessionIDs(st state.State, fallbackWorkspace string) (state.State, int) {
	return assignMissingSessionIDs(st, fallbackWorkspace, false)
}

func assignMissingSessionIDs(st state.State, fallbackWorkspace string, skipFresh bool) (state.State, int) {
	earliest := earliestTaskCreatedAt(st)
	sessions := recentSessions(earliest.Add(-assignmentLookback))
	used := usedSessionIDs(st)
	assigned := 0
	for index := range st.Tasks {
		task := &st.Tasks[index]
		if state.TaskTypeID(*task) != tasktypes.DefaultCodexID {
			continue
		}
		if strings.TrimSpace(task.ResumeID) != "" {
			continue
		}
		if skipFresh && TaskFreshForUpgrade(*task) {
			continue
		}
		workspace := workspaceForTask(st, *task, fallbackWorkspace)
		createdAt := parseTime(task.CreatedAt)
		session, ok := matchSession(sessions, workspace, createdAt.Add(-assignmentLookback), used)
		if !ok {
			continue
		}
		task.ResumeID = session.ID
		task.UpdatedAt = state.NowISO()
		used[session.ID] = true
		assigned++
	}
	return st, assigned
}

func BuildReport(st state.State) Report {
	report := Report{}
	for _, task := range st.Tasks {
		if state.TaskTypeID(task) != tasktypes.DefaultCodexID {
			continue
		}
		report.Total++
		if TaskFreshForUpgrade(task) {
			report.Fresh++
			continue
		}
		if !TaskIdleForUpgrade(task) {
			report.Busy = append(report.Busy, task)
			continue
		}
		if strings.TrimSpace(task.ResumeID) == "" {
			report.Missing = append(report.Missing, task)
			continue
		}
		report.Ready++
	}
	return report
}

func BuildUpgradeReport(st state.State, cfg config.Config, terminalForegroundActive func(string) bool) Report {
	report := BuildReport(st)
	for _, task := range st.Tasks {
		if state.TaskTypeID(task) == tasktypes.DefaultCodexID {
			continue
		}
		if !taskLiveForRestart(task) {
			continue
		}
		taskType, ok := cfg.TaskType(state.TaskTypeID(task))
		definition, supported := tasktypes.ForKind(taskType.Kind)
		if !ok || !supported || !definition.RestartableTerminal() || !TerminalTaskIdleForUpgrade(task) || terminalTaskForegroundActive(task.ID, terminalForegroundActive) {
			report.TerminalBusy = append(report.TerminalBusy, task)
			continue
		}
		report.TerminalReady = append(report.TerminalReady, task)
	}
	return report
}

func taskLiveForRestart(task state.Task) bool {
	switch task.Status {
	case state.StatusStarting, state.StatusRunning, state.StatusReady, state.StatusSitting, state.StatusShipping:
		return true
	default:
		return false
	}
}

func TaskIdleForUpgrade(task state.Task) bool {
	switch titles.CanonicalStatus(task) {
	case string(state.StatusReady), "idle", string(state.StatusSitting), string(state.StatusStopped), string(state.StatusKilled):
		return true
	default:
		return false
	}
}

func TerminalTaskIdleForUpgrade(task state.Task) bool {
	return task.Status == state.StatusReady
}

func terminalTaskForegroundActive(taskID string, terminalForegroundActive func(string) bool) bool {
	return terminalForegroundActive != nil && terminalForegroundActive(taskID)
}

func TaskFreshForUpgrade(task state.Task) bool {
	if strings.TrimSpace(task.ResumeID) != "" {
		return false
	}
	if task.InputSubmitted || task.AutoTitleAttempted {
		return false
	}
	if strings.TrimSpace(task.AutoTitle) != "" || strings.TrimSpace(task.AutoTitleError) != "" {
		return false
	}
	return true
}

func ResumeCommand(codexCommand string, sessionID string) string {
	return tasktypes.CodexResumeCommand(codexCommand, sessionID)
}

func Home() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".codex")
	}
	return ".codex"
}

func recentSessions(since time.Time) []Session {
	root := filepath.Join(Home(), "sessions")
	var sessions []Session
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if !since.IsZero() && info.ModTime().Before(since) {
			return nil
		}
		session, ok := readSessionMeta(path)
		if ok {
			sessions = append(sessions, session)
		}
		return nil
	})
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].Timestamp.Before(sessions[j].Timestamp)
	})
	return sessions
}

func readSessionMeta(path string) (Session, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Session{}, false
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Session{}, false
	}
	var event struct {
		Type    string `json:"type"`
		Payload struct {
			ID        string `json:"id"`
			CWD       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &event); err != nil || event.Type != "session_meta" {
		return Session{}, false
	}
	if strings.TrimSpace(event.Payload.ID) == "" || strings.TrimSpace(event.Payload.CWD) == "" {
		return Session{}, false
	}
	return Session{
		ID:        strings.TrimSpace(event.Payload.ID),
		CWD:       canonicalWorkspace(event.Payload.CWD),
		Timestamp: parseTime(event.Payload.Timestamp),
		Path:      path,
	}, true
}

func matchSession(sessions []Session, workspace string, since time.Time, used map[string]bool) (Session, bool) {
	workspace = canonicalWorkspace(workspace)
	var fallback Session
	hasFallback := false
	for _, session := range sessions {
		if used[session.ID] || session.CWD != workspace {
			continue
		}
		if !session.Timestamp.IsZero() && !since.IsZero() && session.Timestamp.Before(since) {
			continue
		}
		if !hasFallback || session.Timestamp.Before(fallback.Timestamp) {
			fallback = session
			hasFallback = true
		}
	}
	return fallback, hasFallback
}

func canonicalWorkspace(path string) string {
	normalized := state.NormalizeWorkspacePath(path)
	if resolved, err := filepath.EvalSymlinks(normalized); err == nil && strings.TrimSpace(resolved) != "" {
		return state.NormalizeWorkspacePath(resolved)
	}
	return normalized
}

func earliestTaskCreatedAt(st state.State) time.Time {
	var earliest time.Time
	for _, task := range st.Tasks {
		if state.TaskTypeID(task) != tasktypes.DefaultCodexID {
			continue
		}
		created := parseTime(task.CreatedAt)
		if created.IsZero() {
			continue
		}
		if earliest.IsZero() || created.Before(earliest) {
			earliest = created
		}
	}
	if earliest.IsZero() {
		return time.Now()
	}
	return earliest
}

func usedSessionIDs(st state.State) map[string]bool {
	used := map[string]bool{}
	for _, task := range st.Tasks {
		if state.TaskTypeID(task) != tasktypes.DefaultCodexID {
			continue
		}
		if id := strings.TrimSpace(task.ResumeID); id != "" {
			used[id] = true
		}
	}
	return used
}

func workspaceForTask(st state.State, task state.Task, fallback string) string {
	if workspace := state.WorkspaceForTask(st, task); workspace != nil {
		return workspace.Path
	}
	return fallback
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err == nil {
		return parsed
	}
	parsed, _ = time.Parse(time.RFC3339, strings.TrimSpace(value))
	return parsed
}
