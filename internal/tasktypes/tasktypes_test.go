package tasktypes

import (
	"testing"

	"github.com/edwmurph/weft/internal/state"
)

func TestRegistryExposesBuiltInDefinitions(t *testing.T) {
	codex, ok := ForKind(KindCodex)
	if !ok {
		t.Fatal("codex definition missing")
	}
	if codex.ConfiguredTypeID() != DefaultCodexID || codex.InputMode() != InputModeCodex {
		t.Fatalf("codex definition = %#v/%s", codex, codex.InputMode())
	}
	if policy := codex.StartPolicy(); policy.Status != state.StatusRunning || !policy.TrackOperation || policy.Visible {
		t.Fatalf("codex start policy = %#v", policy)
	}
	if got := codex.Command("", state.Task{ResumeID: "session-1"}); got != "codex resume 'session-1'" {
		t.Fatalf("codex resume command = %q", got)
	}
	if got := codex.ScreenStatus("Allow Codex to edit files?\nAllow this request\nDeny\nEnter to submit"); got != "Ready" {
		t.Fatalf("codex screen status = %q", got)
	}

	terminal, ok := ForKind(KindTerminal)
	if !ok {
		t.Fatal("terminal definition missing")
	}
	if terminal.ConfiguredTypeID() != "" || terminal.InputMode() != InputModeTerminal {
		t.Fatalf("terminal definition = %#v/%s", terminal, terminal.InputMode())
	}
	if policy := terminal.StartPolicy(); policy.Status != state.StatusReady || !policy.Visible || policy.TrackOperation {
		t.Fatalf("terminal start policy = %#v", policy)
	}
	if !terminal.TracksTerminalCWD() || !terminal.TracksForegroundCommands() || !terminal.RestartableTerminal() {
		t.Fatalf("terminal capabilities missing")
	}
}

func TestCodexLoadingUsesActiveScreenVisibility(t *testing.T) {
	codex, ok := ForKind(KindCodex)
	if !ok {
		t.Fatal("codex definition missing")
	}
	ready := codex.ApplyPTYTitle(state.Task{ID: "a", Status: state.StatusRunning}, "Fake Codex Ready", "")
	if codex.Loading(ready, LoadingContext{Active: false}) {
		t.Fatal("inactive ready codex task should not load")
	}
	if !codex.Loading(ready, LoadingContext{Active: true, ScreenVisible: false}) {
		t.Fatal("active ready codex task should load until visible content arrives")
	}
	if codex.Loading(ready, LoadingContext{Active: true, ScreenVisible: true}) {
		t.Fatal("active ready codex task with visible screen should not load")
	}
}

func TestCodexDefinitionTranslatesLiveTitleIntoLiveStatus(t *testing.T) {
	codex, ok := ForKind(KindCodex)
	if !ok {
		t.Fatal("codex definition missing")
	}
	task := codex.ApplyPTYTitle(state.Task{ID: "a", Status: state.StatusRunning}, "Fake Codex Crafting", "Ready")
	if task.LiveStatus != "Crafting" || task.Status != state.StatusRunning {
		t.Fatalf("busy codex status = %s/%q", task.Status, task.LiveStatus)
	}
	task = codex.ApplyPTYTitle(state.Task{ID: "a", Status: state.StatusRunning}, "Fake Codex Running", "Ready")
	if task.LiveStatus != "Ready" || task.Status != state.StatusReady {
		t.Fatalf("ready codex status = %s/%q", task.Status, task.LiveStatus)
	}
}
