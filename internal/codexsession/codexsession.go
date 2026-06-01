package codexsession

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/edwmurph/weft/internal/state"
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
	Total    int
	Ready    int
	Assigned int
	Busy     []state.Agent
	Missing  []state.Agent
}

func (r Report) CanUpgrade() bool {
	return len(r.Busy) == 0 && len(r.Missing) == 0
}

func PrepareResumeState(st state.State, fallbackWorkspace string) (state.State, Report) {
	next, assigned := AssignMissingSessionIDs(st, fallbackWorkspace)
	report := BuildReport(next)
	report.Assigned = assigned
	return next, report
}

func AssignMissingSessionIDs(st state.State, fallbackWorkspace string) (state.State, int) {
	earliest := earliestAgentCreatedAt(st)
	sessions := recentSessions(earliest.Add(-assignmentLookback))
	used := usedSessionIDs(st)
	assigned := 0
	for index := range st.Agents {
		agent := &st.Agents[index]
		if state.AgentTypeID(*agent) != state.DefaultAgentTypeID {
			continue
		}
		if strings.TrimSpace(agent.CodexSessionID) != "" {
			continue
		}
		workspace := workspaceForAgent(st, *agent, fallbackWorkspace)
		createdAt := parseTime(agent.CreatedAt)
		session, ok := matchSession(sessions, workspace, createdAt.Add(-assignmentLookback), used)
		if !ok {
			continue
		}
		agent.CodexSessionID = session.ID
		agent.UpdatedAt = state.NowISO()
		used[session.ID] = true
		assigned++
	}
	return st, assigned
}

func BuildReport(st state.State) Report {
	report := Report{}
	for _, agent := range st.Agents {
		if state.AgentTypeID(agent) != state.DefaultAgentTypeID {
			continue
		}
		report.Total++
		if !AgentIdleForUpgrade(agent) {
			report.Busy = append(report.Busy, agent)
			continue
		}
		if strings.TrimSpace(agent.CodexSessionID) == "" {
			report.Missing = append(report.Missing, agent)
			continue
		}
		report.Ready++
	}
	return report
}

func LiveNonCodexTaskCount(st state.State) int {
	count := 0
	for _, agent := range st.Agents {
		if state.AgentTypeID(agent) == state.DefaultAgentTypeID {
			continue
		}
		if agentLiveForRestart(agent) {
			count++
		}
	}
	return count
}

func agentLiveForRestart(agent state.Agent) bool {
	switch agent.Status {
	case state.StatusStarting, state.StatusRunning, state.StatusReady, state.StatusSitting, state.StatusShipping:
		return true
	default:
		return false
	}
}

func AgentIdleForUpgrade(agent state.Agent) bool {
	switch titles.CanonicalStatus(agent) {
	case string(state.StatusReady), "idle", string(state.StatusSitting), string(state.StatusStopped), string(state.StatusKilled):
		return true
	default:
		return false
	}
}

func ResumeCommand(codexCommand string, sessionID string) string {
	codexCommand = strings.TrimSpace(codexCommand)
	if codexCommand == "" {
		codexCommand = "codex"
	}
	return codexCommand + " resume " + shellQuote(sessionID)
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

func earliestAgentCreatedAt(st state.State) time.Time {
	var earliest time.Time
	for _, agent := range st.Agents {
		if state.AgentTypeID(agent) != state.DefaultAgentTypeID {
			continue
		}
		created := parseTime(agent.CreatedAt)
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
	for _, agent := range st.Agents {
		if state.AgentTypeID(agent) != state.DefaultAgentTypeID {
			continue
		}
		if id := strings.TrimSpace(agent.CodexSessionID); id != "" {
			used[id] = true
		}
	}
	return used
}

func workspaceForAgent(st state.State, agent state.Agent, fallback string) string {
	if workspace := state.WorkspaceForAgent(st, agent); workspace != nil {
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

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
