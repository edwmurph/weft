package titles

import (
	"testing"

	"github.com/edwmurph/codux/internal/state"
)

func TestRenderCodexTemplateUsesLiveTitle(t *testing.T) {
	tab := state.Tab{ID: "abc", Title: "{codex}", Column: "inbox", CodexTitle: "Plan Ready", Status: state.StatusRunning}

	if got := Render(tab); got != "Plan Ready" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderCodexTemplateFallsBackToPending(t *testing.T) {
	tab := state.Tab{ID: "abc", Title: "{codex}", Column: "inbox"}

	if got := Render(tab); got != PendingTitle {
		t.Fatalf("got %q", got)
	}
}

func TestRenderStatusTemplateFallsBackToTabStatus(t *testing.T) {
	tab := state.Tab{ID: "abc", Title: StatusTemplate, Column: "inbox", Status: state.StatusStopped}

	if got := Render(tab); got != string(state.StatusStopped) {
		t.Fatalf("got %q", got)
	}
}

func TestRenderStatusTemplateUsesCodexActivityStatus(t *testing.T) {
	tab := state.Tab{ID: "abc", Title: StatusTemplate, Column: "inbox", CodexTitle: "Fake Codex Ready", Status: state.StatusRunning}

	if got := Render(tab); got != "ready" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderFixedTitleWithStatusTemplate(t *testing.T) {
	tab := state.Tab{ID: "abc", Title: "Codex " + StatusTemplate, Column: "inbox", CodexTitle: "Fake Codex Working", Status: state.StatusRunning}

	if got := Render(tab); got != "Codex working" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderFixedTitleWithStatusTemplateFallsBackToRunning(t *testing.T) {
	tab := state.Tab{ID: "abc", Title: "Codex " + StatusTemplate, Column: "inbox", Status: state.StatusRunning}

	if got := Render(tab); got != "Codex running" {
		t.Fatalf("got %q", got)
	}
}

func TestTemplateVariablesListsSupportedPlaceholders(t *testing.T) {
	got := TemplateVariables()
	want := []string{CodexTitleTemplate, StatusTemplate}
	if len(got) != len(want) {
		t.Fatalf("got %d variables, want %d: %#v", len(got), len(want), got)
	}
	for index, wantName := range want {
		if got[index].Name != wantName {
			t.Fatalf("variable %d = %q, want %q", index, got[index].Name, wantName)
		}
	}
}
