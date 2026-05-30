package titles

import (
	"testing"

	"github.com/edwmurph/weft/internal/state"
)

func TestRenderAgentDefaultTemplateUsesConfiguredTitle(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Plan", CodexTitle: "Plan Ready", Status: state.StatusRunning}

	if got := RenderAgent(agent, state.Workspace{Path: "/tmp/project"}, state.Group{Path: "inbox"}, "{title}"); got != "Plan" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAgentSupportsLiveVariables(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex", AutoTitle: "Fix Login", CodexTitle: "Fake Codex Working", Status: state.StatusRunning}

	got := RenderAgent(agent, state.Workspace{Path: "/tmp/project"}, state.Group{Path: "ship"}, "{workspace} {group}: {auto} {status} {codex}")

	if got != "/tmp/project ship: Fix Login working Fake Codex Working" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAgentUsesWorkspaceVariable(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex", Status: state.StatusRunning}

	got := RenderAgent(agent, state.Workspace{Path: "/tmp/project"}, state.Group{}, "{workspace}")

	if got != "/tmp/project" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAgentDoesNotSupportLegacyWorkspaceVariable(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex", Status: state.StatusRunning}

	got := RenderAgent(agent, state.Workspace{Path: "/tmp/project"}, state.Group{Path: "ship"}, "{workdir} {folder}")

	if got != "{workdir} {folder}" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAgentRendersVariablesInsideBaseTitle(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex {status}", CodexTitle: "Fake Codex Ready", Status: state.StatusRunning}

	if got := RenderAgent(agent, state.Workspace{}, state.Group{}, "{title}"); got != "Codex ready" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderStatusTemplateFallsBackToAgentStatus(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex", Status: state.StatusStopped}

	if got := RenderAgent(agent, state.Workspace{}, state.Group{}, StatusTemplate); got != string(state.StatusStopped) {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAutoTemplateFallsBackToPending(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex", Status: state.StatusRunning}

	if got := RenderAgent(agent, state.Workspace{}, state.Group{}, AutoTemplate); got != AutoPending {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAutoTemplateShowsFailureState(t *testing.T) {
	agent := state.Agent{ID: "abc", Title: "Codex", AutoTitleError: "OPENAI_API_KEY is required", Status: state.StatusRunning}

	if got := RenderAgent(agent, state.Workspace{}, state.Group{}, AutoTemplate); got != AutoFailed {
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
