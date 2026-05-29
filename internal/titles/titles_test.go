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
