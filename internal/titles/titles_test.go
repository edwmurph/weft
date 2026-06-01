package titles

import (
	"testing"

	"github.com/edwmurph/weft/internal/state"
)

func TestRenderTaskDefaultTemplateUsesConfiguredTitle(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Plan", CodexTitle: "Plan Ready", Status: state.StatusRunning}

	if got := RenderTask(task, state.Workspace{Path: "/tmp/project"}, state.Group{Path: "inbox"}, "{title}"); got != "Plan" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderTaskSupportsLiveVariables(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", AutoTitle: "Fix Login", CodexTitle: "Fake Codex Working", Status: state.StatusRunning}

	got := RenderTask(task, state.Workspace{Path: "/tmp/project"}, state.Group{Path: "ship"}, "{workspace} {group}: {auto} {status} {codex}")

	if got != "/tmp/project ship: Fix Login Working Fake Codex Working" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderTaskUsesWorkspaceVariable(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", Status: state.StatusRunning}

	got := RenderTask(task, state.Workspace{Path: "/tmp/project"}, state.Group{}, "{workspace}")

	if got != "/tmp/project" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderTaskDoesNotSupportLegacyWorkspaceVariable(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", Status: state.StatusRunning}

	got := RenderTask(task, state.Workspace{Path: "/tmp/project"}, state.Group{Path: "ship"}, "{workdir} {folder}")

	if got != "{workdir} {folder}" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderTaskRendersVariablesInsideBaseTitle(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex {status}", CodexTitle: "Fake Codex Ready", Status: state.StatusRunning}

	if got := RenderTask(task, state.Workspace{}, state.Group{}, "{title}"); got != "Codex Ready" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderStatusPreservesCodexTokenCase(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", CodexTitle: "Fake Codex Working", Status: state.StatusRunning}

	if got := RenderStatus(task); got != "Working" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalStatus(task); got != "working" {
		t.Fatalf("canonical status = %q", got)
	}

	task.CodexTitle = "Fake Codex Waiting"
	if got := RenderStatus(task); got != "Waiting" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalStatus(task); got != "waiting" {
		t.Fatalf("canonical status = %q", got)
	}

	task.CodexTitle = "Fake Codex Crafting"
	if got := RenderStatus(task); got != "Crafting" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalStatus(task); got != "crafting" {
		t.Fatalf("canonical status = %q", got)
	}

	task.CodexTitle = "Exploring"
	if got := RenderStatus(task); got != "Exploring" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalStatus(task); got != "exploring" {
		t.Fatalf("canonical status = %q", got)
	}
}

func TestRenderStatusUsesScreenDerivedCodexStatus(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", CodexTitle: "Fake Codex Running", CodexStatus: "Ready", Status: state.StatusRunning}

	if got := RenderStatus(task); got != "Ready" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalStatus(task); got != "ready" {
		t.Fatalf("canonical status = %q", got)
	}

	task.CodexTitle = "Fake Codex Working"
	if got := RenderStatus(task); got != "Working" {
		t.Fatalf("live title status should win, got %q", got)
	}
}

func TestStatusIndicatesActivityForUnlistedCodexStatus(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", CodexTitle: "Fake Codex Crafting", Status: state.StatusRunning}

	if !StatusIndicatesActivity(task) {
		t.Fatal("unlisted live Codex status should be active")
	}

	task.CodexTitle = "Fake Codex Waiting"
	if !StatusIndicatesActivity(task) {
		t.Fatal("waiting Codex status should be active")
	}

	task.CodexTitle = "Fake Codex Ready"
	if StatusIndicatesActivity(task) {
		t.Fatal("ready Codex status should not be active")
	}

	task.CodexTitle = "Fake Codex Running"
	task.CodexStatus = "Ready"
	if StatusIndicatesActivity(task) {
		t.Fatal("screen-derived ready status should not be active")
	}

	task.CodexTitle = "Fake Codex Waiting"
	task.CodexStatus = "Ready"
	if StatusIndicatesActivity(task) {
		t.Fatal("screen-derived ready status should still override waiting title status")
	}
}

func TestRenderStatusTemplateFallsBackToTaskStatus(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", Status: state.StatusStopped}

	if got := RenderTask(task, state.Workspace{}, state.Group{}, StatusTemplate); got != string(state.StatusStopped) {
		t.Fatalf("got %q", got)
	}

	task.Status = state.StatusKilled
	if got := RenderTask(task, state.Workspace{}, state.Group{}, StatusTemplate); got != string(state.StatusKilled) {
		t.Fatalf("got %q", got)
	}

	task.Status = state.StatusReady
	if got := RenderTask(task, state.Workspace{}, state.Group{}, StatusTemplate); got != string(state.StatusReady) {
		t.Fatalf("got %q", got)
	}

	task.Status = state.StatusRunning
	task.CodexTitle = "Codex"
	if got := RenderTask(task, state.Workspace{}, state.Group{}, StatusTemplate); got != string(state.StatusRunning) {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAutoTemplateFallsBackToPending(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", Status: state.StatusRunning}

	if got := RenderTask(task, state.Workspace{}, state.Group{}, AutoTemplate); got != AutoPending {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAutoTemplateShowsFailureState(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", AutoTitleError: "OPENAI_API_KEY is required", Status: state.StatusRunning}

	if got := RenderTask(task, state.Workspace{}, state.Group{}, AutoTemplate); got != AutoFailed {
		t.Fatalf("got %q", got)
	}
}

func TestTemplateVariablesListsSupportedPlaceholders(t *testing.T) {
	got := TemplateVariables()
	want := []string{TitleTemplate, AutoTemplate, CodexTemplate, StatusTemplate, WorkspaceTemplate, GroupTemplate}
	if len(got) != len(want) {
		t.Fatalf("got %d variables, want %d: %#v", len(got), len(want), got)
	}
	for index, wantName := range want {
		if got[index].Name != wantName {
			t.Fatalf("variable %d = %q, want %q", index, got[index].Name, wantName)
		}
	}
}
