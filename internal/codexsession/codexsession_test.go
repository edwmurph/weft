package codexsession

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
)

func TestPrepareResumeStateAssignsMatchingSessionIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	writeSessionMeta(t, home, "rollout-alpha.jsonl", "session-alpha", workspace, now)
	writeSessionMeta(t, home, "rollout-beta.jsonl", "session-beta", workspace, now.Add(time.Second))

	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		Workspaces:          []state.Workspace{{ID: "w", Path: workspace, CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)}},
		Tasks: []state.Task{
			{ID: "a", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "Alpha", Status: state.StatusRunning, CodexTitle: "Codex Ready", CodexInputSubmitted: true, CreatedAt: now.Add(-30 * time.Second).Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)},
			{ID: "b", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "Beta", Status: state.StatusRunning, CodexTitle: "Codex Ready", CodexInputSubmitted: true, CreatedAt: now.Add(-20 * time.Second).Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)},
		},
	}

	next, report := PrepareResumeState(st, workspace)
	if !report.CanUpgrade() || report.Assigned != 2 || report.Ready != 2 {
		t.Fatalf("report = %#v", report)
	}
	if got := next.Tasks[0].CodexSessionID; got != "session-alpha" {
		t.Fatalf("first session id = %q", got)
	}
	if got := next.Tasks[1].CodexSessionID; got != "session-beta" {
		t.Fatalf("second session id = %q", got)
	}
}

func TestBuildReportBlocksBusyOrMissingSessions(t *testing.T) {
	now := state.NowISO()
	st := state.State{
		Version: state.Version,
		Tasks: []state.Task{
			{ID: "busy", TypeID: config.DefaultTaskTypeCodex, Title: "Busy", Status: state.StatusRunning, CodexTitle: "Codex Working", CodexInputSubmitted: true, CreatedAt: now, UpdatedAt: now},
			{ID: "waiting", TypeID: config.DefaultTaskTypeCodex, Title: "Waiting", Status: state.StatusRunning, CodexTitle: "Codex Waiting", CodexInputSubmitted: true, CreatedAt: now, UpdatedAt: now},
			{ID: "missing", TypeID: config.DefaultTaskTypeCodex, Title: "Missing", Status: state.StatusRunning, CodexTitle: "Codex Ready", CodexInputSubmitted: true, CreatedAt: now, UpdatedAt: now},
			{ID: "ready", TypeID: config.DefaultTaskTypeCodex, Title: "Ready", Status: state.StatusRunning, CodexTitle: "Codex Ready", CodexSessionID: "session-ready", CreatedAt: now, UpdatedAt: now},
			{ID: "prompt-ready", TypeID: config.DefaultTaskTypeCodex, Title: "Prompt Ready", Status: state.StatusRunning, CodexTitle: "Codex Waiting", CodexStatus: "Ready", CodexSessionID: "session-prompt", CreatedAt: now, UpdatedAt: now},
			{ID: "fresh", TypeID: config.DefaultTaskTypeCodex, Title: "Fresh", Status: state.StatusRunning, CodexTitle: "Codex Ready", CreatedAt: now, UpdatedAt: now},
		},
	}

	report := BuildReport(st)
	if report.CanUpgrade() || report.Ready != 2 || report.Fresh != 1 || len(report.Busy) != 2 || len(report.Missing) != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestBuildReportAllowsFreshUnsubmittedCodexWithoutSession(t *testing.T) {
	now := state.NowISO()
	st := state.State{
		Version: state.Version,
		Tasks: []state.Task{
			{ID: "fresh", TypeID: config.DefaultTaskTypeCodex, Title: "Fresh", Status: state.StatusRunning, CodexTitle: "Codex Ready", CreatedAt: now, UpdatedAt: now},
		},
	}

	report := BuildReport(st)
	if !report.CanUpgrade() || report.Fresh != 1 || report.Ready != 0 || len(report.Busy) != 0 || len(report.Missing) != 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestBuildUpgradeReportClassifiesTerminalActivity(t *testing.T) {
	now := state.NowISO()
	cfg := config.DefaultConfig()
	cfg.TaskTypes["logs"] = config.TaskType{
		ID:            "logs",
		Label:         "Logs",
		Kind:          config.TaskKindTerminal,
		Command:       "tail -f app.log",
		Badge:         "[logs]",
		TitleTemplate: "Logs",
	}
	cfg.TaskTypes["watch"] = config.TaskType{
		ID:            "watch",
		Label:         "Watch",
		Kind:          config.TaskKindTerminal,
		Command:       "watch make test",
		Badge:         "[watch]",
		TitleTemplate: "Watch",
	}
	st := state.State{
		Version: state.Version,
		Tasks: []state.Task{
			{ID: "shell", TypeID: config.DefaultTaskTypeShell, Title: "Shell", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "shell-running", TypeID: config.DefaultTaskTypeShell, Title: "Shell", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now},
			{ID: "logs", TypeID: "logs", Title: "Logs", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "watch", TypeID: "watch", Title: "Watch", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "foreground", TypeID: config.DefaultTaskTypeShell, Title: "Shell", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
		},
	}

	report := BuildUpgradeReport(st, cfg, func(taskID string) bool {
		return taskID == "foreground"
	})

	if report.CanUpgrade() {
		t.Fatalf("busy terminal tasks should block upgrade: %#v", report)
	}
	if len(report.TerminalReady) != 3 || report.TerminalReady[0].ID != "shell" || report.TerminalReady[1].ID != "logs" || report.TerminalReady[2].ID != "watch" {
		t.Fatalf("idle terminals = %#v", report.TerminalReady)
	}
	if len(report.TerminalBusy) != 2 {
		t.Fatalf("busy terminals = %#v", report.TerminalBusy)
	}
}

func TestResumeCommandDefaultsCodexAndQuotesSession(t *testing.T) {
	got := ResumeCommand("", "session-1")
	if want := "codex resume 'session-1'"; got != want {
		t.Fatalf("resume command = %q, want %q", got, want)
	}
}

func writeSessionMeta(t *testing.T, home string, name string, id string, cwd string, ts time.Time) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "05", "31")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	line := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q,"timestamp":%q}}`+"\n", id, cwd, ts.Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}
