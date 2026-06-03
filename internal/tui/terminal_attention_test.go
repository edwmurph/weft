package tui

import (
	"strings"
	"testing"

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/state"
)

func TestGlobalNeedsAttentionCountMatchesWorkspaceSemantics(t *testing.T) {
	st := state.State{
		Version: state.Version,
		Groups: []state.Group{
			{ID: "silent-group", Silent: true},
		},
		Tasks: []state.Task{
			{ID: "running", Status: state.StatusRunning},
			{ID: "ready", Status: state.StatusReady},
			{ID: "live-ready", Status: state.StatusRunning, LiveStatus: "Ready"},
			{ID: "silent-ready", Status: state.StatusReady, Silent: true},
			{ID: "group-stopped", GroupID: "silent-group", Status: state.StatusStopped},
			{ID: "group-error", GroupID: "silent-group", Status: state.StatusError},
		},
	}

	if got := len(globalAttentionTasks(ipc.Snapshot{State: st})); got != 3 {
		t.Fatalf("global needs attention count = %d, want 3", got)
	}
}

func TestGlobalNeedsAttentionCountTreatsTerminalForegroundTasksAsActive(t *testing.T) {
	st := state.State{
		Version: state.Version,
		Tasks: []state.Task{
			{ID: "sleep", Status: state.StatusReady},
			{ID: "done", Status: state.StatusReady},
		},
	}

	if got := len(globalAttentionTasks(ipc.Snapshot{State: st, TerminalForegroundTaskIDs: []string{"sleep"}})); got != 1 {
		t.Fatalf("global needs attention count = %d, want only non-foreground ready task", got)
	}
}

func TestTerminalAttentionProviderForEnvDetectsItermOnly(t *testing.T) {
	for _, env := range [][]string{
		{"TERM_PROGRAM=iTerm.app"},
		{"LC_TERMINAL=iTerm2"},
		{"ITERM_SESSION_ID=w0t0p0"},
	} {
		if _, ok := terminalAttentionProviderForEnv(env).(itermAttentionProvider); !ok {
			t.Fatalf("env %#v did not select iTerm attention provider", env)
		}
	}
	if provider := terminalAttentionProviderForEnv([]string{"TERM_PROGRAM=Apple_Terminal"}); provider != nil {
		t.Fatalf("non-iTerm env selected provider %#v", provider)
	}
}

func TestTerminalAttentionSequenceSkipsUnsupportedProvider(t *testing.T) {
	cfg := config.DefaultConfig().TerminalAttention
	cfg.Enabled = true
	ready := ipc.Snapshot{State: state.State{Version: state.Version, Tasks: []state.Task{{ID: "task", Status: state.StatusReady}}}}

	next, sequence := terminalAttentionSequence(cfg, ready, terminalAttentionState{}, nil)
	if next.enabled || sequence != "" {
		t.Fatalf("unsupported provider enabled/sequence = %v/%q, want disabled and empty", next.enabled, sequence)
	}
}

func TestTerminalAttentionSequenceClearsLegacyBadgeAndRequestsAttentionOnTransition(t *testing.T) {
	cfg := config.DefaultConfig().TerminalAttention
	cfg.Enabled = true
	active := ipc.Snapshot{State: state.State{Version: state.Version, Tasks: []state.Task{{ID: "task", Title: "Review patch", Status: state.StatusRunning}}}}
	ready := ipc.Snapshot{State: state.State{Version: state.Version, Tasks: []state.Task{{ID: "task", Title: "Review patch", Status: state.StatusReady}}}}

	next, sequence := terminalAttentionSequence(cfg, active, terminalAttentionState{}, itermAttentionProvider{})
	if sequence != "\x1b]1337;SetBadgeFormat=\a" {
		t.Fatalf("initial clear sequence = %q", sequence)
	}

	next, sequence = terminalAttentionSequence(cfg, ready, next, itermAttentionProvider{})
	if strings.Contains(sequence, "SetBadgeFormat=") ||
		!strings.Contains(sequence, "\x1b]9;Review patch needs attention\a") ||
		!strings.Contains(sequence, "\x1b]1337;RequestAttention=once\a") {
		t.Fatalf("ready transition sequence = %q, want notification and one-shot attention without session badge", sequence)
	}

	next, sequence = terminalAttentionSequence(cfg, ready, next, itermAttentionProvider{})
	if sequence != "" {
		t.Fatalf("unchanged count sequence = %q, want empty", sequence)
	}

	_, sequence = terminalAttentionSequence(cfg, ipc.Snapshot{State: state.Empty()}, next, itermAttentionProvider{})
	if sequence != "\x1b]1337;RequestAttention=no\a" {
		t.Fatalf("clear sequence = %q, want attention cancel only", sequence)
	}
}

func TestTerminalAttentionSequenceNotifiesWhenShellForegroundCommandFinishes(t *testing.T) {
	cfg := config.DefaultConfig().TerminalAttention
	cfg.Enabled = true
	st := state.State{Version: state.Version, Tasks: []state.Task{{ID: "sleep", Title: "sleep 5", Status: state.StatusReady}}}
	initial := ipc.Snapshot{State: st, TerminalForegroundTaskIDs: []string{"sleep"}}
	complete := ipc.Snapshot{State: st}

	next, sequence := terminalAttentionSequence(cfg, initial, terminalAttentionState{}, itermAttentionProvider{})
	if sequence != "\x1b]1337;SetBadgeFormat=\a" || next.count != 0 {
		t.Fatalf("foreground initial sequence/count = %q/%d, want clear and zero", sequence, next.count)
	}

	_, sequence = terminalAttentionSequence(cfg, complete, next, itermAttentionProvider{})
	notificationSequence := "\x1b]9;sleep 5 needs attention\a"
	if !strings.Contains(sequence, notificationSequence) ||
		!strings.Contains(sequence, "\x1b]1337;RequestAttention=once\a") {
		t.Fatalf("foreground completion sequence = %q, want notification and attention request", sequence)
	}
}

func TestTerminalAttentionSequenceDoesNotNotifyForNewReadyTask(t *testing.T) {
	cfg := config.DefaultConfig().TerminalAttention
	cfg.Enabled = true
	ready := ipc.Snapshot{State: state.State{Version: state.Version, Tasks: []state.Task{{ID: "new", Status: state.StatusReady}}}}

	next, sequence := terminalAttentionSequence(cfg, ipc.Snapshot{State: state.Empty()}, terminalAttentionState{}, itermAttentionProvider{})
	if sequence != "\x1b]1337;SetBadgeFormat=\a" {
		t.Fatalf("initial clear sequence = %q", sequence)
	}

	next, sequence = terminalAttentionSequence(cfg, ready, next, itermAttentionProvider{})
	if sequence != "" || next.count != 1 {
		t.Fatalf("new ready task sequence/count = %q/%d, want no notification and one tracked attention task", sequence, next.count)
	}

	_, sequence = terminalAttentionSequence(cfg, ready, next, itermAttentionProvider{})
	if sequence != "" {
		t.Fatalf("unchanged new ready task sequence = %q, want empty", sequence)
	}
}

func TestTerminalAttentionSequenceDoesNotNotifyForNewTaskFirstReadyAfterLoading(t *testing.T) {
	cfg := config.DefaultConfig().TerminalAttention
	cfg.Enabled = true
	st := state.State{Version: state.Version, Tasks: []state.Task{{ID: "new", Title: "New task", Status: state.StatusReady}}}
	loading := ipc.Snapshot{State: st, LoadingTaskIDs: []string{"new"}}
	ready := ipc.Snapshot{State: st}

	next, sequence := terminalAttentionSequence(cfg, ipc.Snapshot{State: state.Empty()}, terminalAttentionState{}, itermAttentionProvider{})
	if sequence != "\x1b]1337;SetBadgeFormat=\a" {
		t.Fatalf("initial clear sequence = %q", sequence)
	}

	next, sequence = terminalAttentionSequence(cfg, loading, next, itermAttentionProvider{})
	if sequence != "" || !next.pendingNewTasks["new"] {
		t.Fatalf("new loading task sequence/pending = %q/%v, want no notification and pending new task", sequence, next.pendingNewTasks)
	}

	next, sequence = terminalAttentionSequence(cfg, ready, next, itermAttentionProvider{})
	if sequence != "" || next.pendingNewTasks["new"] {
		t.Fatalf("new task first ready sequence/pending = %q/%v, want no notification and cleared pending task", sequence, next.pendingNewTasks)
	}

	runningAgain := ipc.Snapshot{State: st, TerminalForegroundTaskIDs: []string{"new"}}
	next, sequence = terminalAttentionSequence(cfg, runningAgain, next, itermAttentionProvider{})
	if sequence != "" {
		t.Fatalf("known task foreground sequence = %q, want empty", sequence)
	}

	_, sequence = terminalAttentionSequence(cfg, ready, next, itermAttentionProvider{})
	if !strings.Contains(sequence, "\x1b]9;New task needs attention\a") {
		t.Fatalf("known task later completion sequence = %q, want notification", sequence)
	}
}

func TestTerminalAttentionSequenceDoesNotNotifyForFocusedConsoleTask(t *testing.T) {
	cfg := config.DefaultConfig().TerminalAttention
	cfg.Enabled = true
	running := ipc.Snapshot{State: state.State{
		Version:      state.Version,
		ActiveTaskID: "task",
		Focus:        state.FocusConsole,
		Tasks:        []state.Task{{ID: "task", Status: state.StatusReady}},
	}, TerminalForegroundTaskIDs: []string{"task"}}
	readyFocused := ipc.Snapshot{State: state.State{
		Version:      state.Version,
		ActiveTaskID: "task",
		Focus:        state.FocusConsole,
		Tasks:        []state.Task{{ID: "task", Status: state.StatusReady}},
	}}
	readyUnfocused := ipc.Snapshot{State: state.State{
		Version:      state.Version,
		ActiveTaskID: "task",
		Focus:        state.FocusTasks,
		Tasks:        []state.Task{{ID: "task", Status: state.StatusReady}},
	}}

	next, sequence := terminalAttentionSequence(cfg, running, terminalAttentionState{}, itermAttentionProvider{})
	if sequence != "\x1b]1337;SetBadgeFormat=\a" {
		t.Fatalf("initial focused running sequence = %q, want clear only", sequence)
	}

	next, sequence = terminalAttentionSequence(cfg, readyFocused, next, itermAttentionProvider{})
	if sequence != "" || next.count != 0 {
		t.Fatalf("focused ready sequence/count = %q/%d, want no notification and zero notification count", sequence, next.count)
	}

	_, sequence = terminalAttentionSequence(cfg, readyUnfocused, next, itermAttentionProvider{})
	if sequence != "" {
		t.Fatalf("unfocused already-seen task sequence = %q, want empty", sequence)
	}
}

func TestTerminalAttentionExitCommandSkipsUnsupportedTerminal(t *testing.T) {
	previous := terminalAttentionState{
		initialized:      true,
		enabled:          false,
		requestAttention: terminalAttentionOnce,
		count:            1,
		attentionTasks:   map[string]bool{"ready": true},
	}

	if cmd := terminalAttentionExitCommand(previous); cmd != nil {
		t.Fatal("unsupported terminal state should not emit exit cleanup")
	}
}

func TestTerminalAttentionSequenceNotifiesForNewAttentionTaskWhenCountAlreadyNonzero(t *testing.T) {
	cfg := config.DefaultConfig().TerminalAttention
	cfg.Enabled = true
	st := state.State{
		Version: state.Version,
		Tasks: []state.Task{
			{ID: "already-ready", Title: "Existing task", Status: state.StatusReady},
			{ID: "sleep", Title: "sleep 5", Status: state.StatusReady},
		},
	}
	initial := ipc.Snapshot{State: st, TerminalForegroundTaskIDs: []string{"sleep"}}
	complete := ipc.Snapshot{State: st}

	next, sequence := terminalAttentionSequence(cfg, initial, terminalAttentionState{}, itermAttentionProvider{})
	if sequence != "\x1b]1337;SetBadgeFormat=\a" || next.count != 1 {
		t.Fatalf("foreground initial sequence/count = %q/%d, want clear and one existing attention task", sequence, next.count)
	}

	_, sequence = terminalAttentionSequence(cfg, complete, next, itermAttentionProvider{})
	if !strings.Contains(sequence, "\x1b]9;sleep 5 needs attention\a") ||
		!strings.Contains(sequence, "\x1b]1337;RequestAttention=once\a") {
		t.Fatalf("foreground completion sequence = %q, want notification for new attention task", sequence)
	}
}

func TestTerminalAttentionSequenceNotifiesWithConciseMultiTaskTitle(t *testing.T) {
	cfg := config.DefaultConfig().TerminalAttention
	cfg.Enabled = true
	active := ipc.Snapshot{State: state.State{
		Version: state.Version,
		Tasks: []state.Task{
			{ID: "build", Title: "Build", Status: state.StatusRunning},
			{ID: "tests", Title: "Tests", Status: state.StatusRunning},
		},
	}}
	ready := ipc.Snapshot{State: state.State{
		Version: state.Version,
		Tasks: []state.Task{
			{ID: "build", Title: "Build", Status: state.StatusReady},
			{ID: "tests", Title: "Tests", Status: state.StatusReady},
		},
	}}

	next, _ := terminalAttentionSequence(cfg, active, terminalAttentionState{}, itermAttentionProvider{})
	_, sequence := terminalAttentionSequence(cfg, ready, next, itermAttentionProvider{})
	if !strings.Contains(sequence, "\x1b]9;Build and 1 more need attention\a") {
		t.Fatalf("multi-task notification sequence = %q", sequence)
	}
}

func TestTerminalAttentionNotificationTextUsesRenderedTaskTitle(t *testing.T) {
	st := state.State{
		Version: state.Version,
		Tasks: []state.Task{
			{ID: "codex", Title: "{status} {auto}", AutoTitle: "Release notes", Status: state.StatusRunning, LiveStatus: "Ready"},
		},
	}
	got := terminalAttentionNotificationText(ipc.Snapshot{State: st}, []string{"codex"})
	if got != "Ready Release notes needs attention" {
		t.Fatalf("notification text = %q", got)
	}
}

func TestTerminalAttentionNotificationTextPrefersStoredTitleOverLiveSessionPrefix(t *testing.T) {
	st := state.State{
		Version: state.Version,
		Tasks: []state.Task{
			{ID: "shell", Title: "sleep shell", LiveTitle: "Session weft sleep #2", Status: state.StatusReady},
		},
	}
	got := terminalAttentionNotificationText(ipc.Snapshot{State: st}, []string{"shell"})
	if got != "sleep shell needs attention" {
		t.Fatalf("notification text = %q", got)
	}
}

func TestClientApplyResponseEmitsTerminalAttentionCommandInIterm(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	cfg := config.DefaultConfig()
	cfg.TerminalAttention.Enabled = true
	model := NewClientModel(testRuntime(t), cfg)
	oldWrite := writeTerminalSequence
	var wrote strings.Builder
	writeTerminalSequence = func(sequence string) error {
		wrote.WriteString(sequence)
		return nil
	}
	defer func() { writeTerminalSequence = oldWrite }()

	active := state.State{Version: state.Version, Tasks: []state.Task{{ID: "task", Title: "Review patch", Status: state.StatusRunning}}}
	if cmd := model.applyResponse(ipc.Response{OK: true, Snapshot: &ipc.Snapshot{State: active}}); cmd != nil {
		_ = cmd()
	}
	wrote.Reset()

	ready := state.State{Version: state.Version, Tasks: []state.Task{{ID: "task", Title: "Review patch", Status: state.StatusReady}}}
	cmd := model.applyResponse(ipc.Response{OK: true, Snapshot: &ipc.Snapshot{State: ready}})
	if cmd == nil {
		t.Fatal("ready transition should emit terminal attention command")
	}
	_ = cmd()

	output := wrote.String()
	if !strings.Contains(output, "\x1b]9;Review patch needs attention\a") ||
		!strings.Contains(output, "RequestAttention=once") ||
		strings.Contains(output, "SetBadgeFormat=") {
		t.Fatalf("terminal attention output = %q", output)
	}
}
