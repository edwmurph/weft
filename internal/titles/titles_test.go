package titles

import (
	"testing"

	"github.com/edwmurph/weft/internal/state"
)

func TestRenderTaskDefaultTemplateUsesConfiguredTitle(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Plan", LiveTitle: "Plan Ready", Status: state.StatusRunning}

	if got := RenderTask(task, state.Workspace{Path: "/tmp/project"}, state.Group{Path: "inbox"}, "{title}"); got != "Plan" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderTaskSupportsLiveVariables(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", AutoTitle: "Fix Login", LiveTitle: "Fake Codex Working", LiveStatus: "Working", Status: state.StatusRunning}

	got := RenderTask(task, state.Workspace{Path: "/tmp/project"}, state.Group{Path: "ship"}, "{workspace} {group}: {auto} {status} {live}")

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

func TestRenderTaskRendersVariablesInsideBaseTitle(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex {status}", LiveTitle: "Fake Codex Ready", LiveStatus: "Ready", Status: state.StatusRunning}

	if got := RenderTask(task, state.Workspace{}, state.Group{}, "{title}"); got != "Codex Ready" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderStatusPreservesLiveStatusCase(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", LiveStatus: "Working", Status: state.StatusRunning}

	if got := RenderStatus(task); got != "Working" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalStatus(task); got != "working" {
		t.Fatalf("canonical status = %q", got)
	}

	task.LiveStatus = "Waiting"
	if got := RenderStatus(task); got != "Waiting" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalStatus(task); got != "waiting" {
		t.Fatalf("canonical status = %q", got)
	}

	task.LiveStatus = "Crafting"
	if got := RenderStatus(task); got != "Crafting" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalStatus(task); got != "crafting" {
		t.Fatalf("canonical status = %q", got)
	}

	task.LiveStatus = "Exploring"
	if got := RenderStatus(task); got != "Exploring" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalStatus(task); got != "exploring" {
		t.Fatalf("canonical status = %q", got)
	}
}

func TestConsolidatedStatusBucketsLiveStatuses(t *testing.T) {
	for _, tt := range []struct {
		name string
		task state.Task
		want string
	}{
		{
			name: "known live working",
			task: state.Task{ID: "abc", Title: "Codex", LiveStatus: "Working", Status: state.StatusRunning},
			want: "working",
		},
		{
			name: "unknown live status",
			task: state.Task{ID: "abc", Title: "Codex", LiveStatus: "Crafting", Status: state.StatusRunning},
			want: "working",
		},
		{
			name: "ready prompt",
			task: state.Task{ID: "abc", Title: "Codex", LiveTitle: "Fake Codex Running", LiveStatus: "Ready", Status: state.StatusReady},
			want: string(state.StatusReady),
		},
		{
			name: "submitted ready prompt",
			task: state.Task{ID: "abc", Title: "Codex", LiveTitle: "Fake Codex Ready", LiveStatus: "running", Status: state.StatusRunning},
			want: string(state.StatusRunning),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConsolidatedStatus(tt.task); got != tt.want {
				t.Fatalf("consolidated status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderStatusUsesScreenDerivedLiveStatus(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", LiveTitle: "Fake Codex Running", LiveStatus: "Ready", Status: state.StatusRunning}

	if got := RenderStatus(task); got != "Ready" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalStatus(task); got != "ready" {
		t.Fatalf("canonical status = %q", got)
	}

	task.LiveStatus = "Working"
	if got := RenderStatus(task); got != "Working" {
		t.Fatalf("live status should update, got %q", got)
	}
}

func TestStatusIndicatesActivityForUnlistedLiveStatus(t *testing.T) {
	task := state.Task{ID: "abc", Title: "Codex", LiveStatus: "Crafting", Status: state.StatusRunning}

	if !StatusIndicatesActivity(task) {
		t.Fatal("unlisted live status should be active")
	}

	task.LiveStatus = "Waiting"
	if !StatusIndicatesActivity(task) {
		t.Fatal("waiting live status should be active")
	}

	task.LiveStatus = "Ready"
	if StatusIndicatesActivity(task) {
		t.Fatal("ready live status should not be active")
	}

	if StatusIndicatesActivity(task) {
		t.Fatal("ready live status should still not be active")
	}

	task.LiveStatus = "running"
	if !StatusIndicatesActivity(task) {
		t.Fatal("running live status should be active")
	}
}

func TestCodexActivityStatusParsesTerminalTitle(t *testing.T) {
	for _, tt := range []struct {
		title string
		want  string
	}{
		{title: "Fake Codex Working", want: "Working"},
		{title: "Fake Codex Waiting", want: "Waiting"},
		{title: "Fake Codex Crafting", want: "Crafting"},
		{title: "Exploring", want: "Exploring"},
		{title: "Codex", want: ""},
	} {
		if got := CodexActivityStatus(tt.title); got != tt.want {
			t.Fatalf("CodexActivityStatus(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
	if !LiveStatusIndicatesActivity(CodexActivityStatus("Fake Codex Crafting")) {
		t.Fatal("crafting title should indicate Codex activity")
	}
	if LiveStatusIndicatesActivity(CodexActivityStatus("Fake Codex Ready")) {
		t.Fatal("ready title should not indicate Codex activity")
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
	task.LiveTitle = "Codex"
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
	want := []string{TitleTemplate, AutoTemplate, LiveTemplate, StatusTemplate, WorkspaceTemplate, GroupTemplate}
	if len(got) != len(want) {
		t.Fatalf("got %d variables, want %d: %#v", len(got), len(want), got)
	}
	for index, wantName := range want {
		if got[index].Name != wantName {
			t.Fatalf("variable %d = %q, want %q", index, got[index].Name, wantName)
		}
	}
}
