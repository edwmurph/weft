package titles

import (
	"testing"

	"github.com/edwmurph/weft/internal/state"
)

func TestRenderAgentDefaultTemplateUsesConfiguredTitle(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Plan", CodexTitle: "Plan Ready", Status: state.StatusRunning}

	if got := RenderAgent(agent, state.Workdir{Path: "/tmp/project"}, state.Folder{Path: "inbox"}, "{title}"); got != "Plan" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAgentSupportsLiveVariables(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex", AutoTitle: "Fix Login", CodexTitle: "Fake Codex Working", Status: state.StatusRunning}

	got := RenderAgent(agent, state.Workdir{Path: "/tmp/project"}, state.Folder{Path: "ship"}, "{group}: {auto} {status} {codex}")

	if got != "ship: Fix Login working Fake Codex Working" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAgentKeepsLegacyVariablesInsideBaseTitle(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex {status}", CodexTitle: "Fake Codex Ready", Status: state.StatusRunning}

	if got := RenderAgent(agent, state.Workdir{}, state.Folder{}, "{title}"); got != "Codex ready" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderStatusTemplateFallsBackToAgentStatus(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex", Status: state.StatusStopped}

	if got := RenderAgent(agent, state.Workdir{}, state.Folder{}, StatusTemplate); got != string(state.StatusStopped) {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAutoTemplateFallsBackToPending(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex", Status: state.StatusRunning}

	if got := RenderAgent(agent, state.Workdir{}, state.Folder{}, AutoTemplate); got != AutoPending {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAutoTemplateShowsFailureState(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex", AutoTitleError: "OPENAI_API_KEY is required", Status: state.StatusRunning}

	if got := RenderAgent(agent, state.Workdir{}, state.Folder{}, AutoTemplate); got != AutoFailed {
		t.Fatalf("got %q", got)
	}
}

func TestTemplateVariablesListsSupportedPlaceholders(t *testing.T) {
	got := TemplateVariables()
	want := []string{TitleTemplate, AutoTemplate, CodexTemplate, StatusTemplate, WorkdirTemplate, GroupTemplate, FolderTemplate}
	if len(got) != len(want) {
		t.Fatalf("got %d variables, want %d: %#v", len(got), len(want), got)
	}
	for index, wantName := range want {
		if got[index].Name != wantName {
			t.Fatalf("variable %d = %q, want %q", index, got[index].Name, wantName)
		}
	}
}
