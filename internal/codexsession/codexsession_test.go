package codexsession

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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
		Agents: []state.Agent{
			{ID: "a", WorkspaceID: "w", Title: "Alpha", Status: state.StatusRunning, CodexTitle: "Codex Ready", CreatedAt: now.Add(-30 * time.Second).Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)},
			{ID: "b", WorkspaceID: "w", Title: "Beta", Status: state.StatusRunning, CodexTitle: "Codex Ready", CreatedAt: now.Add(-20 * time.Second).Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)},
		},
	}

	next, report := PrepareResumeState(st, workspace)
	if !report.CanUpgrade() || report.Assigned != 2 || report.Ready != 2 {
		t.Fatalf("report = %#v", report)
	}
	if got := next.Agents[0].CodexSessionID; got != "session-alpha" {
		t.Fatalf("first session id = %q", got)
	}
	if got := next.Agents[1].CodexSessionID; got != "session-beta" {
		t.Fatalf("second session id = %q", got)
	}
}

func TestBuildReportBlocksBusyOrMissingSessions(t *testing.T) {
	now := state.NowISO()
	st := state.State{
		Version: state.Version,
		Agents: []state.Agent{
			{ID: "busy", Title: "Busy", Status: state.StatusRunning, CodexTitle: "Codex Working", CreatedAt: now, UpdatedAt: now},
			{ID: "waiting", Title: "Waiting", Status: state.StatusRunning, CodexTitle: "Codex Waiting", CreatedAt: now, UpdatedAt: now},
			{ID: "missing", Title: "Missing", Status: state.StatusRunning, CodexTitle: "Codex Ready", CreatedAt: now, UpdatedAt: now},
			{ID: "ready", Title: "Ready", Status: state.StatusRunning, CodexTitle: "Codex Ready", CodexSessionID: "session-ready", CreatedAt: now, UpdatedAt: now},
			{ID: "prompt-ready", Title: "Prompt Ready", Status: state.StatusRunning, CodexTitle: "Codex Waiting", CodexStatus: "Ready", CodexSessionID: "session-prompt", CreatedAt: now, UpdatedAt: now},
		},
	}

	report := BuildReport(st)
	if report.CanUpgrade() || report.Ready != 2 || len(report.Busy) != 2 || len(report.Missing) != 1 {
		t.Fatalf("report = %#v", report)
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
