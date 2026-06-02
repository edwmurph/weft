package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/ptyx"
	"github.com/edwmurph/weft/internal/state"
	weftversion "github.com/edwmurph/weft/internal/version"
)

func TestEmptyModelStartsInWorkspacesFocus(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()

	model := NewModel(rt, cfg, state.Empty())

	if model.state.Focus != state.FocusWorkspaces || !model.state.NavOpen {
		t.Fatalf("focus/nav = %s/%t", model.state.Focus, model.state.NavOpen)
	}
	if len(model.state.Workspaces) != 0 || len(model.state.Groups) != 0 {
		t.Fatalf("empty state = %#v", model.state)
	}
}

func TestTaskTypeBadgeCellUsesConfiguredColumnWidth(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TaskTypes["logs"] = config.TaskType{
		ID:            "logs",
		Label:         "Logs",
		Kind:          config.TaskKindTerminal,
		Command:       "tail -f app.log",
		Badge:         "[logs]",
		TitleTemplate: "Logs",
	}

	tests := []struct {
		taskType config.TaskType
		want     string
	}{
		{taskType: cfg.TaskTypes["codex"], want: "[codex]"},
		{taskType: cfg.TaskTypes["shell"], want: "[shell]"},
		{taskType: cfg.TaskTypes["logs"], want: "[logs] "},
	}

	for _, tt := range tests {
		if got := taskTypeBadgeCell(cfg, tt.taskType); got != tt.want {
			t.Fatalf("taskTypeBadgeCell(%q) = %q, want %q", tt.taskType.ID, got, tt.want)
		}
	}
}

func TestTaskTypeBadgeDoesNotSynthesizeMissingTypes(t *testing.T) {
	cfg := config.DefaultConfig()
	task := state.Task{ID: "a", TypeID: "missing"}

	if got := strings.TrimSpace(taskTypeBadgeCellForTask(cfg, task)); got != "" {
		t.Fatalf("missing task type badge = %q, want empty configured badge", got)
	}
	if got := taskTypeBadgeColumnWidth(config.Config{TaskTypes: map[string]config.TaskType{}}); got != 0 {
		t.Fatalf("empty configured task type badge width = %d, want 0", got)
	}
}

func TestNewTaskRequiresWorkspace(t *testing.T) {
	model := NewModel(testRuntime(t), config.DefaultConfig(), state.Empty())

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{}})
	if response.OK || response.Message != "add a workspace first" || cmd != nil {
		t.Fatalf("ipc new should be blocked without workspace, response=%#v cmd=%v", response, cmd)
	}
	if len(model.state.Tasks) != 0 {
		t.Fatalf("new task should not mutate tasks: %#v", model.state.Tasks)
	}
}

func TestApplyPTYDataUsesTaskID(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	model.applyPTYData(ptyx.Data{TaskID: "a", Text: "hello\n", Title: "Fake Codex Ready"})

	if screen := model.screens["a"]; screen == nil || !screen.HasVisibleContent() {
		t.Fatalf("task screen was not updated: %#v", model.screens)
	}
	if !model.visible["a"] {
		t.Fatalf("task should be marked visible: %#v", model.visible)
	}
	if got := model.state.Tasks[0].CodexTitle; got != "Fake Codex Ready" {
		t.Fatalf("CodexTitle = %q", got)
	}
}

func TestApplyPTYDataMarksRequestUserInputScreenReady(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	model.applyPTYData(ptyx.Data{TaskID: "a", Title: "Fake Codex Running"})
	model.applyPTYData(ptyx.Data{TaskID: "a", Text: "\033[2J\033[HQuestion 1\nPick a path\n1 unanswered question\nEnter to submit answer\n"})

	task := state.TaskByID(model.state, "a")
	if task == nil {
		t.Fatal("task missing")
	}
	if task.CodexStatus != "Ready" || task.Status != state.StatusReady {
		t.Fatalf("task status = %s/%q, want ready/Ready", task.Status, task.CodexStatus)
	}
	if got := model.renderTaskTitle(*task); got != "alpha" {
		t.Fatalf("configured title should remain unchanged, got %q", got)
	}

	task.Title = "{status}"
	if got := renderTaskTitleForState(model.cfg, model.state, *task); got != "Ready" {
		t.Fatalf("status title = %q, want Ready", got)
	}

	model.applyPTYData(ptyx.Data{TaskID: "a", Text: "\033[2J\033[Hworking again\n"})
	task = state.TaskByID(model.state, "a")
	if task == nil || task.CodexStatus != "" || task.Status != state.StatusRunning {
		t.Fatalf("task status after clearing prompt = %#v", task)
	}
}

func TestApplyPTYDataMarksPermissionScreensReady(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{
			name: "tool permission",
			text: "\033[2J\033[HField 1/1\nAllow Codex to use ChatGPT Atlas?\n› 1. Allow         Allow this request and continue.\n  2. Always allow  Allow this request and remember this choice for future requests.\n  3. Deny          Decline this request and continue.\n  4. Cancel        Cancel this request\nenter to submit | esc to cancel\n",
		},
		{
			name: "command approval",
			text: "\033[2J\033[HWould you like to run the following comman\nd?\n\nReason: Do you want to remove temporary generated QA frames and the unused package lock\nfrom the demo video project?\n\n$ rm -f package-lock.json .hyperframes/frame-check/frame-01.png\n\n1. Yes, proceed (y)\n2. Yes, and don't ask again for commands that start with `rm -f` (p)\n3. No, and tell Codex what to do differently (esc)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := testModelWithTask(t)
			defer killPTYs(model)

			model.applyPTYData(ptyx.Data{TaskID: "a", Title: "Fake Codex Running"})
			model.applyPTYData(ptyx.Data{TaskID: "a", Text: tt.text})

			task := state.TaskByID(model.state, "a")
			if task == nil {
				t.Fatal("task missing")
			}
			if task.CodexStatus != "Ready" || task.Status != state.StatusReady {
				t.Fatalf("prompt status = %s/%q, want ready/Ready", task.Status, task.CodexStatus)
			}
			if model.taskLoading("a") {
				t.Fatalf("prompt should not show as loading")
			}
		})
	}
}

func TestSnapshotMarksActiveTasksLoadingUntilReady(t *testing.T) {
	st := testStateWithTask(t.TempDir())
	started := time.Now().Add(-12 * time.Second)
	model := Model{
		cfg:             config.DefaultConfig(),
		state:           st,
		screens:         map[string]*TerminalScreen{"a": NewTerminalScreen(80, 24)},
		visible:         map[string]bool{},
		operationStarts: testOperationStarts(map[string]time.Time{"a": started}),
	}

	snapshot := model.Snapshot()
	if len(snapshot.LoadingTaskIDs) != 1 || snapshot.LoadingTaskIDs[0] != "a" {
		t.Fatalf("loading task ids = %#v", snapshot.LoadingTaskIDs)
	}
	if got := snapshot.TaskOperationStartedAt["a"]; !got.Equal(started) {
		t.Fatalf("loading task operation start = %v, want %v", got, started)
	}

	model.state.Tasks[0].CodexTitle = "Fake Codex Ready"
	snapshot = model.Snapshot()
	if len(snapshot.LoadingTaskIDs) != 1 || snapshot.LoadingTaskIDs[0] != "a" {
		t.Fatalf("ready task should keep loading until visible content: %#v", snapshot.LoadingTaskIDs)
	}
	if got := snapshot.TaskOperationStartedAt["a"]; !got.Equal(started) {
		t.Fatalf("ready task without content should keep operation start = %v, want %v", got, started)
	}

	model.screens["a"].Write("ready\n")
	snapshot = model.Snapshot()
	if len(snapshot.LoadingTaskIDs) != 0 {
		t.Fatalf("ready task should not be marked loading: %#v", snapshot.LoadingTaskIDs)
	}
	if len(snapshot.TaskOperationStartedAt) != 0 {
		t.Fatalf("ready task should not expose operation starts: %#v", snapshot.TaskOperationStartedAt)
	}

	model.state.Tasks[0].CodexTitle = "Fake Codex Waiting"
	model.operationStarts = testOperationStarts(map[string]time.Time{"a": started})
	snapshot = model.Snapshot()
	if len(snapshot.LoadingTaskIDs) != 1 || snapshot.LoadingTaskIDs[0] != "a" {
		t.Fatalf("waiting task should be marked loading: %#v", snapshot.LoadingTaskIDs)
	}
	if got := snapshot.TaskOperationStartedAt["a"]; !got.Equal(started) {
		t.Fatalf("waiting task operation start = %v, want %v", got, started)
	}
}

func TestCodexOperationStartTracksStartupAndSubmittedPrompt(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	startup := model.Snapshot()
	if _, ok := startup.TaskOperationStartedAt["a"]; !ok {
		t.Fatalf("codex startup snapshot missing operation start: %#v", startup.TaskOperationStartedAt)
	}

	model.applyPTYData(ptyx.Data{TaskID: "a", Title: "Fake Codex Ready", Text: "ready\n"})
	ready := model.Snapshot()
	if len(ready.TaskOperationStartedAt) != 0 {
		t.Fatalf("ready Codex task should clear operation start: %#v", ready.TaskOperationStartedAt)
	}

	task := state.TaskByID(model.state, "a")
	if task == nil {
		t.Fatal("task missing")
	}
	model.codexInputBuffers["a"] = []rune("hello")
	model.submitCodexInputBuffer(*task)
	if _, ok := modelOperationStart(model, "a"); !ok {
		t.Fatalf("submitted Codex prompt should start operation timing: %#v", model.operationStarts)
	}
	submitted := model.Snapshot()
	if len(submitted.LoadingTaskIDs) != 1 || submitted.LoadingTaskIDs[0] != "a" {
		t.Fatalf("submitted Codex prompt should render as loading before title changes: %#v", submitted.LoadingTaskIDs)
	}
	if _, ok := submitted.TaskOperationStartedAt["a"]; !ok {
		t.Fatalf("submitted Codex prompt snapshot missing operation start: %#v", submitted.TaskOperationStartedAt)
	}

	model.applyPTYData(ptyx.Data{TaskID: "a", Title: "Fake Codex Working"})
	working := model.Snapshot()
	if _, ok := working.TaskOperationStartedAt["a"]; !ok {
		t.Fatalf("working Codex snapshot missing prompt operation start: %#v", working.TaskOperationStartedAt)
	}
}

func TestSubmittingReadyCodexPromptRestartsOperationTiming(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	model.applyPTYData(ptyx.Data{TaskID: "a", Title: "Fake Codex Running"})
	model.applyPTYData(ptyx.Data{TaskID: "a", Text: "\033[2J\033[HQuestion 1\nPick a path\n1 unanswered question\nEnter to submit answer\n"})

	task := state.TaskByID(model.state, "a")
	if task == nil {
		t.Fatal("task missing")
	}
	if task.Status != state.StatusReady || task.CodexStatus != "Ready" {
		t.Fatalf("setup task status = %s/%q, want ready/Ready", task.Status, task.CodexStatus)
	}
	ready := model.Snapshot()
	if len(ready.TaskOperationStartedAt) != 0 {
		t.Fatalf("ready prompt should not expose operation start before answer: %#v", ready.TaskOperationStartedAt)
	}

	model.codexInputBuffers["a"] = []rune("accept plan")
	model.submitCodexInputBuffer(*task)

	task = state.TaskByID(model.state, "a")
	if task == nil {
		t.Fatal("task missing after submit")
	}
	if task.Status != state.StatusRunning || task.CodexStatus != string(state.StatusRunning) {
		t.Fatalf("submitted ready prompt should become running, got %s/%q", task.Status, task.CodexStatus)
	}
	submitted := model.Snapshot()
	if len(submitted.LoadingTaskIDs) != 1 || submitted.LoadingTaskIDs[0] != "a" {
		t.Fatalf("submitted ready prompt should render as loading: %#v", submitted.LoadingTaskIDs)
	}
	if _, ok := submitted.TaskOperationStartedAt["a"]; !ok {
		t.Fatalf("submitted ready prompt snapshot missing operation start: %#v", submitted.TaskOperationStartedAt)
	}
}

func TestLoadingIndicatorCoversNonIdleTaskStates(t *testing.T) {
	for _, tt := range []struct {
		name string
		task state.Task
		want bool
	}{
		{
			name: "codex waiting title",
			task: state.Task{ID: "a", Title: "Codex", Status: state.StatusRunning, CodexTitle: "Fake Codex Waiting"},
			want: true,
		},
		{
			name: "unlisted codex activity title",
			task: state.Task{ID: "a", Title: "Codex", Status: state.StatusRunning, CodexTitle: "Fake Codex Crafting"},
			want: true,
		},
		{
			name: "terminal waiting status",
			task: state.Task{ID: "a", TypeID: config.DefaultTaskTypeShell, Title: "Shell", Status: state.TaskStatus("waiting")},
			want: true,
		},
		{
			name: "unlisted terminal activity status",
			task: state.Task{ID: "a", TypeID: config.DefaultTaskTypeShell, Title: "Shell", Status: state.TaskStatus("deploying")},
			want: true,
		},
		{
			name: "ready",
			task: state.Task{ID: "a", Title: "Codex", Status: state.StatusRunning, CodexTitle: "Fake Codex Ready"},
			want: false,
		},
		{
			name: "idle",
			task: state.Task{ID: "a", Title: "Codex", Status: state.TaskStatus("idle")},
			want: false,
		},
		{
			name: "killed",
			task: state.Task{ID: "a", Title: "Codex", Status: state.StatusKilled},
			want: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskStatusShowsLoadingIndicator(tt.task); got != tt.want {
				t.Fatalf("loading indicator = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestApplyLaunchWorkspaceSelectsExistingWorkspace(t *testing.T) {
	rt := testRuntime(t)
	other := t.TempDir()
	launch := t.TempDir()
	st := testStateWithWorkspace(t, other)
	next, _, err := state.AddWorkspace(st, "launch", launch, state.NowISO())
	if err != nil {
		t.Fatal(err)
	}
	next.SelectedWorkspaceID = "w"
	model := NewModel(rt, config.DefaultConfig(), next)

	model.ApplyLaunchWorkspace(launch)

	if model.state.SelectedWorkspaceID != "launch" {
		t.Fatalf("selected workspace = %q, want launch", model.state.SelectedWorkspaceID)
	}
}

func TestSnapshotLaunchWorkspaceArgDoesNotReselectWorkspace(t *testing.T) {
	rt := testRuntime(t)
	other := t.TempDir()
	launch := t.TempDir()
	st := testStateWithWorkspace(t, other)
	next, _, err := state.AddWorkspace(st, "launch", launch, state.NowISO())
	if err != nil {
		t.Fatal(err)
	}
	next.SelectedWorkspaceID = "w"
	model := NewModel(rt, config.DefaultConfig(), next)

	response, _ := model.handleIPC(ipc.Request{Command: "snapshot", Args: map[string]string{"launch_workspace": launch}})

	if !response.OK {
		t.Fatalf("snapshot response = %#v", response)
	}
	if model.state.SelectedWorkspaceID != "w" {
		t.Fatalf("snapshot reselected launch workspace: %#v", model.state)
	}
}

func TestNewModelPreservesPersistedWorkspaceSelection(t *testing.T) {
	rt := testRuntime(t)
	launch := rt.Workspace
	other := t.TempDir()
	now := state.NowISO()
	model := NewModel(rt, config.DefaultConfig(), state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "other",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces: []state.Workspace{
			{ID: "launch", Path: launch, CreatedAt: now, UpdatedAt: now},
			{ID: "other", Path: other, CreatedAt: now, UpdatedAt: now},
		},
	})

	if model.state.SelectedWorkspaceID != "other" {
		t.Fatalf("supervisor start reselected launch workspace: %#v", model.state)
	}
}

func TestClientPromptsToAddMissingLaunchWorkspace(t *testing.T) {
	rt := testRuntime(t)
	model := NewClientModel(rt, config.DefaultConfig())

	model.applyResponse(ipc.Response{OK: true, Snapshot: &ipc.Snapshot{State: state.Empty()}})

	if model.mode != modeConfirm || model.confirm != confirmAddLaunchWorkspace || model.pendingID != rt.Workspace {
		t.Fatalf("prompt state = mode:%s confirm:%s pending:%q", model.mode, model.confirm, model.pendingID)
	}
	got := ansi.Strip(model.View())
	for _, expected := range []string{"Add this workspace to Weft?", "Current directory", "Enter yes", "Esc no"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("launch workspace prompt missing %q:\n%s", expected, got)
		}
	}
	for _, unexpected := range []string{"Y yes", "N no", "Y/Enter yes", "New tasks will start from this directory."} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("launch workspace prompt should not include %q:\n%s", unexpected, got)
		}
	}
}

func TestClientLaunchWorkspaceConfirmationEnterSubmits(t *testing.T) {
	rt := testRuntime(t)
	model := NewClientModel(rt, config.DefaultConfig())
	model.applyResponse(ipc.Response{OK: true, Snapshot: &ipc.Snapshot{State: state.Empty()}})

	updated, cmd := model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ClientModel)

	if cmd == nil {
		t.Fatal("enter should submit launch workspace confirmation")
	}
	if model.mode != modeNormal {
		t.Fatalf("mode = %s, want normal", model.mode)
	}
}

func TestClientRepaintShortcutWorksFromHelp(t *testing.T) {
	rt := testRuntime(t)
	model := NewClientModel(rt, config.DefaultConfig())
	model.mode = modeHelp

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	model = updated.(ClientModel)

	if model.mode != modeHelp {
		t.Fatalf("repaint shortcut should not close help, mode=%s", model.mode)
	}
	if cmd == nil {
		t.Fatal("repaint shortcut should force clear-screen and refresh")
	}
	msg := cmd()
	if got := fmt.Sprintf("%T", msg); got != "tea.sequenceMsg" {
		t.Fatalf("repaint shortcut command = %T, want tea.sequenceMsg", msg)
	}
	sequence := reflect.ValueOf(msg)
	if sequence.Len() != 2 {
		t.Fatalf("repaint shortcut scheduled %d commands, want clear-screen and refresh", sequence.Len())
	}
	first := sequence.Index(0).Interface().(tea.Cmd)
	if got := fmt.Sprintf("%T", first()); got != "tea.clearScreenMsg" {
		t.Fatalf("first repaint shortcut command = %s, want tea.clearScreenMsg", got)
	}
}
func TestConfirmShortcutsUseEnterAndEsc(t *testing.T) {
	for _, confirm := range []confirmKind{confirmAddLaunchWorkspace, confirmDeleteWorkspace, confirmDeleteGroup, confirmUpgradeResume} {
		if !confirmKeySubmits(confirm, tea.KeyMsg{Type: tea.KeyEnter}) {
			t.Fatalf("%s should submit with enter", confirm)
		}
		if confirmKeySubmits(confirm, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}) {
			t.Fatalf("%s should not submit with y", confirm)
		}
		if !confirmKeyCancels(confirm, tea.KeyMsg{Type: tea.KeyEsc}) {
			t.Fatalf("%s should cancel with esc", confirm)
		}
		if confirmKeyCancels(confirm, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}) {
			t.Fatalf("%s should not cancel with n", confirm)
		}
	}
	if !confirmKeySubmits(confirmDeleteTask, tea.KeyMsg{Type: tea.KeyEnter}) {
		t.Fatal("delete task should submit with enter")
	}
	if confirmKeySubmits(confirmDeleteTask, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}) {
		t.Fatal("delete task should not submit with y")
	}
	if !confirmKeyCancels(confirmDeleteTask, tea.KeyMsg{Type: tea.KeyEsc}) {
		t.Fatal("delete task should cancel with esc")
	}
	if !confirmKeyCancels(confirmDeleteTask, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}) {
		t.Fatal("delete task should cancel with n")
	}
}

func TestClientDoesNotPromptForExistingLaunchWorkspace(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithWorkspace(t, rt.Workspace)
	model := NewClientModel(rt, config.DefaultConfig())

	model.applyResponse(ipc.Response{OK: true, Snapshot: &ipc.Snapshot{State: st}})

	if model.mode != modeNormal {
		t.Fatalf("existing launch workspace should not prompt, mode=%s", model.mode)
	}
}

func TestClientWorkspaceFooterShowsVersionInfo(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithWorkspace(t, rt.Workspace)
	st.Focus = state.FocusWorkspaces
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 120
	model.height = 20

	model.applyResponse(ipc.Response{
		OK: true,
		Snapshot: &ipc.Snapshot{
			State:               st,
			CodexTitle:          "Task",
			CodexContent:        "No task open.",
			NavWidth:            minTwoPaneNavWidth,
			ActiveClientVersion: weftversion.Version,
		},
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: weftversion.Version,
	})

	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Weft",
		"CLI        " + weftversion.Version,
		"Supervisor " + weftversion.Version,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("workspace version footer missing %q:\n%s", expected, got)
		}
	}
}

func TestClientRetriesTransportAttachFailures(t *testing.T) {
	model := NewClientModel(testRuntime(t), config.DefaultConfig())

	updated, cmd := model.Update(clientResponseMsg{command: "attach_client", err: errors.New("i/o timeout")})
	model = updated.(ClientModel)

	if cmd == nil {
		t.Fatal("transport attach failure should schedule an attach retry")
	}
	if model.message != "i/o timeout" {
		t.Fatalf("message = %q", model.message)
	}
}

func TestClientRequestArgsOnlySelectLaunchWorkspaceOnAttach(t *testing.T) {
	rt := config.Runtime{Workspace: "/tmp/project"}

	attach := clientRequestArgs(rt, "client-1", "attach_client", nil)
	if attach["client_id"] != "client-1" {
		t.Fatalf("attach client_id = %q", attach["client_id"])
	}
	if attach["launch_workspace"] != rt.Workspace {
		t.Fatalf("attach launch workspace = %q", attach["launch_workspace"])
	}

	nav := clientRequestArgs(rt, "client-1", "nav_move", map[string]string{"delta": "1"})
	if nav["client_id"] != "client-1" || nav["delta"] != "1" {
		t.Fatalf("nav args = %#v", nav)
	}
	if _, ok := nav["launch_workspace"]; ok {
		t.Fatalf("nav request should not reselect launch workspace: %#v", nav)
	}

	upgrade := clientRequestArgs(rt, "client-1", "upgrade_resume", nil)
	if upgrade["client_id"] != "client-1" || upgrade["client_executable"] == "" {
		t.Fatalf("upgrade args = %#v", upgrade)
	}
	if _, ok := upgrade["launch_workspace"]; ok {
		t.Fatalf("upgrade request should not reselect launch workspace: %#v", upgrade)
	}
}

func testClientUpgrade(supervisorVersion string, runningTasks int) *ipc.Upgrade {
	return &ipc.Upgrade{
		ClientVersion:     weftversion.Version,
		SupervisorVersion: supervisorVersion,
		Compatible:        true,
		RestartRequired:   true,
		RunningTasks:      runningTasks,
		Message:           fmt.Sprintf("Upgrade pending: client %s is newer than supervisor %s.", weftversion.Version, supervisorVersion),
	}
}

func TestClientUpgradeBannerOpensUpgradeResumeConfirm(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].CodexTitle = "Fake Codex Ready"
	st.Tasks[0].CodexSessionID = "session-alpha"
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, CodexTitle: "alpha", CodexContent: "output", NavWidth: 92},
		Upgrade:           testClientUpgrade("3.9.0", 1),
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: "3.9.0",
	})

	if model.upgrade == nil || model.upgrade.RunningTasks != 1 {
		t.Fatalf("upgrade = %#v", model.upgrade)
	}
	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Weft",
		"CLI        " + weftversion.Version,
		"Supervisor 3.9.0",
		"Upgrade ready: supervisor 3.9.0",
		weftversion.Version,
		"Press U to upgrade and resume 1 idle Codex task",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("upgrade footer missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "Upgrade: ready") {
		t.Fatalf("task console should not duplicate upgrade status banner:\n%s", got)
	}

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmUpgradeResume {
		t.Fatalf("upgrade confirm state mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
	got = ansi.Strip(model.View())
	for _, expected := range []string{
		"Upgrade supervisor?",
		"supervisor 3.9.0",
		weftversion.Version,
		"Enter upgrade",
		"saved session IDs",
		"fresh Codex tasks without one",
		"restarts idle shell task(s) with saved history/cwd",
		"Shell jobs, env",
		"mutations",
		"unsubmitted input",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("upgrade confirm missing %q:\n%s", expected, got)
		}
	}
}

func TestClientUpgradeWaitsUntilTaskIsIdleAndResumable(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].CodexTitle = "Fake Codex Working"
	st.Tasks[0].CodexInputSubmitted = true
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, CodexTitle: "alpha", CodexContent: "output", NavWidth: 92},
		Upgrade:           testClientUpgrade("3.9.0", 1),
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: "3.9.0",
	})

	got := ansi.Strip(model.View())
	for _, expected := range []string{"Upgrade pending", "Wait for 1 Codex task(s) to become idle"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("upgrade wait copy missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "Press U to upgrade") {
		t.Fatalf("upgrade action should not show while task is working:\n%s", got)
	}
	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode == modeConfirm {
		t.Fatalf("blocked upgrade should not open confirm, mode=%s cmd=%v", model.mode, cmd)
	}
}

func TestClientUpgradeAllowsFreshCodexWithoutSession(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].CodexTitle = "Fake Codex Ready"
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, CodexTitle: "alpha", CodexContent: "output", NavWidth: 92},
		Upgrade:           testClientUpgrade("3.9.0", 1),
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: "3.9.0",
	})

	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Upgrade ready",
		"supervisor 3.9.0",
		weftversion.Version,
		"Press U to upgrade and start 1 fresh Codex task",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("fresh upgrade footer missing %q:\n%s", expected, got)
		}
	}
	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmUpgradeResume {
		t.Fatalf("fresh upgrade confirm state mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
}

func TestClientUpgradeAllowsIdleShellRestartWithSavedHistoryCWD(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].TypeID = config.DefaultTaskTypeShell
	st.Tasks[0].Title = "Shell"
	st.Tasks[0].Status = state.StatusReady
	st.Tasks[0].TerminalCWD = rt.Workspace
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, NavWidth: 92},
		Upgrade:           testClientUpgrade("3.9.0", 1),
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: "3.9.0",
	})

	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Upgrade ready",
		"Press U to upgrade and restart 1 idle shell task(s) with",
		"saved history/cwd",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("shell restart footer missing %q:\n%s", expected, got)
		}
	}
	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmUpgradeResume {
		t.Fatalf("shell restart confirm state mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
	got = ansi.Strip(model.View())
	for _, expected := range []string{
		"restarts idle shell task(s) with saved history/cwd",
		"Shell jobs, env",
		"mutations",
		"shell variables",
		"unsubmitted input",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("shell restart confirm missing %q:\n%s", expected, got)
		}
	}
}

func TestClientUpgradeBlocksRunningShellTask(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].TypeID = config.DefaultTaskTypeShell
	st.Tasks[0].Title = "Shell"
	st.Tasks[0].Status = state.StatusRunning
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, NavWidth: 92},
		Upgrade:           testClientUpgrade("3.9.0", 1),
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: "3.9.0",
	})

	got := ansi.Strip(model.View())
	for _, expected := range []string{"Upgrade pending", "Wait for 1 shell task(s) to become idle"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("running shell footer missing %q:\n%s", expected, got)
		}
	}
	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode == modeConfirm {
		t.Fatalf("running shell upgrade should not open confirm, mode=%s cmd=%v", model.mode, cmd)
	}
}

func TestTerminalUpgradeSnapshotRestoresHistoryBeforeRestart(t *testing.T) {
	rt := testRuntime(t)
	cwd := t.TempDir()
	now := state.NowISO()
	st := state.State{
		Version: state.Version,
		Workspaces: []state.Workspace{{
			ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now,
		}},
		Tasks: []state.Task{{
			ID: "shell", WorkspaceID: "w", TypeID: config.DefaultTaskTypeShell,
			Title: "Shell", Status: state.StatusReady, TerminalCWD: cwd,
			CreatedAt: now, UpdatedAt: now,
		}},
	}
	screen := NewTerminalScreen(80, 8)
	screen.Write("history-before-upgrade\r\n$ ")
	model := Model{
		runtime: rt, cfg: config.DefaultConfig(), state: st, width: 80, height: 12,
		screens: map[string]*TerminalScreen{"shell": screen},
		visible: map[string]bool{"shell": true},
	}

	if err := model.PrepareTerminalUpgradeSnapshots([]string{"shell"}); err != nil {
		t.Fatal(err)
	}

	restored := Model{
		runtime: rt, cfg: config.DefaultConfig(), state: st, width: 80, height: 12,
		screens: map[string]*TerminalScreen{},
		visible: map[string]bool{},
	}
	restored.restoreTerminalUpgradeSnapshots()

	got := restored.screens["shell"].ScrollbackString()
	for _, expected := range []string{
		"history-before-upgrade",
		"restarted this idle shell task with saved history/cwd",
		"shell variables",
		"unsubmitted input",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("restored snapshot missing %q:\n%s", expected, got)
		}
	}
	if _, err := os.Stat(terminalUpgradeSnapshotPath(rt.Dir, "shell")); !os.IsNotExist(err) {
		t.Fatalf("snapshot file should be consumed, stat err = %v", err)
	}
}

func TestTerminalOSC7UpdatesTaskCWD(t *testing.T) {
	rt := testRuntime(t)
	cwd := t.TempDir()
	now := state.NowISO()
	st := state.State{
		Version: state.Version,
		Workspaces: []state.Workspace{{
			ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now,
		}},
		Tasks: []state.Task{{
			ID: "shell", WorkspaceID: "w", TypeID: config.DefaultTaskTypeShell,
			Title: "Shell", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now,
		}},
	}
	model := Model{
		runtime: rt, store: state.NewStore(rt.StatePath), cfg: config.DefaultConfig(), state: st, width: 80, height: 12,
		screens: map[string]*TerminalScreen{},
		visible: map[string]bool{},
	}

	model.applyPTYData(ptyx.Data{TaskID: "shell", Text: "\x1b]7;file://localhost" + cwd + "\x07$ "})

	task := state.TaskByID(model.state, "shell")
	if task == nil || task.TerminalCWD != cwd {
		t.Fatalf("terminal cwd = %#v, want %q", task, cwd)
	}
}

func TestSnapshotShowsActiveTaskStartError(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].Status = state.StatusError
	st.Tasks[0].CodexTitle = "fork/exec /missing/zsh: no such file or directory"

	model := NewModel(rt, cfg, state.Empty())
	model.state = st
	defer killPTYs(model)

	snapshot := model.Snapshot()
	if strings.Contains(snapshot.CodexContent, "No task open") {
		t.Fatalf("snapshot showed empty state for active error:\n%s", snapshot.CodexContent)
	}
	if !strings.Contains(snapshot.CodexContent, "Codex failed to start") || !strings.Contains(snapshot.CodexContent, "/missing/zsh") {
		t.Fatalf("snapshot missing start error:\n%s", snapshot.CodexContent)
	}
}

func TestActivePTYExitReturnsToTasksPane(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.width = 120
	model.navWidth = 0
	model.state.Focus = state.FocusConsole
	model.state.NavOpen = false
	model.state.Tasks[0].CodexTitle = "Fake Codex Ready"

	model.applyPTYData(ptyx.Data{TaskID: "a", Err: os.ErrClosed})

	task := state.TaskByID(model.state, "a")
	if task == nil || task.Status != state.StatusStopped || task.CodexTitle != "Codex exited" {
		t.Fatalf("task after PTY exit = %#v", task)
	}
	if model.state.Focus != state.FocusTasks || !model.state.NavOpen {
		t.Fatalf("PTY exit should recover to Tasks pane, focus/nav=%s/%t", model.state.Focus, model.state.NavOpen)
	}
	if model.ptys["a"] != nil {
		t.Fatal("dead PTY should be removed from live PTY map")
	}
	if model.navWidth == 0 {
		t.Fatal("dashboard nav should be visible after active PTY exit")
	}
}

func TestRecentCtrlCPTYExitMarksTaskKilled(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.width = 120
	model.navWidth = 0
	model.state.Focus = state.FocusConsole
	model.state.NavOpen = false
	model.recordTaskInterrupt("a")

	model.applyPTYData(ptyx.Data{TaskID: "a", Err: os.ErrClosed})

	task := state.TaskByID(model.state, "a")
	if task == nil || task.Status != state.StatusKilled || task.CodexTitle != "Codex killed" {
		t.Fatalf("task after interrupted PTY exit = %#v", task)
	}
	if model.state.Focus != state.FocusTasks || !model.state.NavOpen {
		t.Fatalf("interrupted PTY exit should recover to Tasks pane, focus/nav=%s/%t", model.state.Focus, model.state.NavOpen)
	}
}

func TestIdleTerminalTaskCtrlCKillsTask(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.width = 120
	model.navWidth = 0
	model.state.Focus = state.FocusConsole
	model.state.NavOpen = false
	model.state.Tasks[0].TypeID = config.DefaultTaskTypeShell
	model.codexInputBuffers["a"] = []rune("stale")
	model.screens["a"] = NewTerminalScreen(80, 6)
	model.screens["a"].Write("terminal-ready\r\n$ ")
	model.visible["a"] = true

	_ = model.applyTaskInput(map[string]string{"input": "ctrl+c", "encoded": terminalKeyboardCtrlC})
	if got := string(model.codexInputBuffers["a"]); got != "" {
		t.Fatalf("terminal ctrl+c should clear pending input buffer, got %q", got)
	}
	task := state.TaskByID(model.state, "a")
	if task == nil || task.Status != state.StatusKilled || task.CodexTitle != "Shell killed" {
		t.Fatalf("idle terminal ctrl+c should kill task immediately: %#v", task)
	}
	if model.state.Focus != state.FocusTasks || !model.state.NavOpen {
		t.Fatalf("idle terminal ctrl+c should recover to Tasks pane, focus/nav=%s/%t", model.state.Focus, model.state.NavOpen)
	}
	if model.ptys["a"] != nil {
		t.Fatal("idle terminal ctrl+c should remove the killed PTY")
	}

	model.applyPTYData(ptyx.Data{TaskID: "a", Err: os.ErrClosed})
	task = state.TaskByID(model.state, "a")
	if task == nil || task.Status != state.StatusKilled || task.CodexTitle != "Shell killed" {
		t.Fatalf("delayed PTY exit should not downgrade killed task: %#v", task)
	}

	model.state.Focus = state.FocusConsole
	output := model.activeOutput()
	if !strings.Contains(output, "terminal-ready") || !strings.Contains(output, "Shell killed") || !strings.Contains(output, "Process exited.") {
		t.Fatalf("killed terminal output should preserve terminal history and append exited state:\n%s", output)
	}
	if strings.Index(output, "terminal-ready") > strings.Index(output, "Shell killed") {
		t.Fatalf("killed terminal output should render exited state below terminal history:\n%s", output)
	}
	if strings.Contains(output, "\x1b") {
		t.Fatalf("killed terminal output should not render terminal cursor or ANSI styles:\n%q", output)
	}
}

func TestPTYWidthMatchesVisibleCodexContentWidth(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.width = 100
	model.navWidth = 0

	if got, want := model.ptyWidth(), 97; got != want {
		t.Fatalf("focused pty width = %d, want visible content width %d", got, want)
	}

	model.state.Focus = state.FocusTasks
	model.state.NavOpen = true
	model.navWidth = 60
	if got, want := model.ptyWidth(), 36; got != want {
		t.Fatalf("split pty width = %d, want visible content width %d", got, want)
	}
}

func TestPTYHeightMatchesVisibleCodexContentHeight(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.height = 32

	if got, want := model.ptyHeight(), 30; got != want {
		t.Fatalf("pty height = %d, want visible content height %d", got, want)
	}

	model.height = 4
	if got, want := model.ptyHeight(), 5; got != want {
		t.Fatalf("minimum pty height = %d, want %d", got, want)
	}
}

func TestIPCCodexFocusResizesScreenToVisibleConsoleWidth(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.width = 160
	model.height = 32
	model.state.Focus = state.FocusTasks
	model.state.NavOpen = true
	model.navWidth = model.targetNavWidth()
	taskID := model.state.ActiveTaskID
	model.screens[taskID] = NewTerminalScreen(model.ptyWidth(), model.ptyHeight())

	splitWidth := model.screens[taskID].cols
	if splitWidth >= 80 {
		t.Fatalf("test setup expected narrow split screen, got %d", splitWidth)
	}

	response, _ := model.handleIPC(ipc.Request{Command: "focus", Args: map[string]string{"target": string(state.FocusConsole)}})
	if !response.OK {
		t.Fatalf("focus response failed: %#v", response)
	}

	if got, want := model.navWidth, 0; got != want {
		t.Fatalf("codex focus nav width = %d, want %d", got, want)
	}
	if got, want := model.screens[taskID].cols, 157; got != want {
		t.Fatalf("focused screen width = %d, want visible console width %d", got, want)
	}
}

func TestTitleHookCapturesFirstSubmittedLine(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Generated title\\n'"

	cmd := model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix ")})
	if cmd != nil {
		t.Fatal("hook should not run before Enter")
	}
	model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("loginx")})
	model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyBackspace})
	cmd = model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected title hook command")
	}
	if task := state.TaskByID(model.state, "a"); task == nil || !task.AutoTitleAttempted || !task.CodexInputSubmitted {
		t.Fatalf("task should be marked attempted and submitted: %#v", task)
	}

	msg := titleHookMessageFromCmd(t, cmd)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	model.applyTitleHook(msg)
	if got := state.TaskByID(model.state, "a").AutoTitle; got != "Generated title" {
		t.Fatalf("auto title = %q", got)
	}
	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"first_message":"fix login"`) {
		t.Fatalf("payload missing reconstructed message:\n%s", raw)
	}
}

func TestTitleHookCaptureTracksAltBackspaceAsPreviousTokenDelete(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix loginx")})
	model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	if got, want := string(model.codexInputBuffers[model.state.Tasks[0].ID]), "fix "; got != want {
		t.Fatalf("direct capture buffer = %q, want %q", got, want)
	}

	model.codexInputBuffers[model.state.Tasks[0].ID] = []rune("fix loginx")
	model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyCtrlH, Alt: true})
	if got, want := string(model.codexInputBuffers[model.state.Tasks[0].ID]), "fix "; got != want {
		t.Fatalf("direct alt ctrl-h capture buffer = %q, want %q", got, want)
	}

	model.codexInputBuffers[model.state.Tasks[0].ID] = []rune("fix loginx")
	model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": "alt+backspace"})
	if got, want := string(model.codexInputBuffers[model.state.Tasks[0].ID]), "fix "; got != want {
		t.Fatalf("forwarded capture buffer = %q, want %q", got, want)
	}
}

func TestTitleHookCapturesSupervisorForwardedInput(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	codexType := model.cfg.TaskTypes[config.DefaultTaskTypeCodex]
	codexType.TitleTemplate = "{status} {auto}"
	model.cfg.TaskTypes[config.DefaultTaskTypeCodex] = codexType
	model.state.Tasks[0].Title = codexType.TitleTemplate
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Generated title\\n'"

	model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": "text", "text": "fix"})
	model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": "space"})
	model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": "text", "text": "login"})
	cmd := model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": "enter"})
	if cmd == nil {
		t.Fatal("expected title hook command")
	}
	msg := titleHookMessageFromCmd(t, cmd)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	model.applyTitleHook(msg)

	task := state.TaskByID(model.state, "a")
	if task == nil || task.AutoTitle != "Generated title" {
		t.Fatalf("auto title = %#v", task)
	}
	if got := model.renderTaskTitle(*task); got != "running Generated title" {
		t.Fatalf("rendered title = %q", got)
	}
	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"first_message":"fix login"`) {
		t.Fatalf("payload missing forwarded first message:\n%s", raw)
	}
}

func TestTitleHookCapturesRawKeyboardProtocolInput(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	codexType := model.cfg.TaskTypes[config.DefaultTaskTypeCodex]
	codexType.TitleTemplate = "{status} {auto}"
	model.cfg.TaskTypes[config.DefaultTaskTypeCodex] = codexType
	model.state.Tasks[0].Title = codexType.TitleTemplate
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Generated title\\n'"

	sendRaw := func(raw string) tea.Cmd {
		t.Helper()
		return model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{
			"encoded": raw,
			"input":   codexInputRaw,
		})
	}
	for _, raw := range []string{
		"\x1b[102u", "\x1b[105u", "\x1b[120u", "\x1b[32u", "\x1b[108u",
		"\x1b[111u", "\x1b[103u", "\x1b[105u", "\x1b[110u",
	} {
		if cmd := sendRaw(raw); cmd != nil {
			t.Fatalf("hook should not run before Enter for %q", raw)
		}
	}
	cmd := sendRaw("\x1b[13u")
	if cmd == nil {
		t.Fatal("expected title hook command")
	}
	if task := state.TaskByID(model.state, "a"); task == nil || !task.AutoTitleAttempted {
		t.Fatalf("task should be marked attempted: %#v", task)
	}
	msg := cmd().(titleHookMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	model.applyTitleHook(msg)

	task := state.TaskByID(model.state, "a")
	if task == nil || task.AutoTitle != "Generated title" {
		t.Fatalf("auto title = %#v", task)
	}
	if got := model.renderTaskTitle(*task); got != "running Generated title" {
		t.Fatalf("rendered title = %q", got)
	}
	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"first_message":"fix login"`) {
		t.Fatalf("payload missing raw keyboard first message:\n%s", raw)
	}
}

func TestTitleHookRawCaptureIgnoresVimEscapeAndMetaCommands(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": codexInputRaw, "encoded": "fix login"})
	model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": codexInputRaw, "encoded": "\x1b"})
	model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": codexInputRaw, "encoded": "\x1bb"})
	model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": codexInputRaw, "encoded": " now"})

	if got, want := string(model.codexInputBuffers[model.state.Tasks[0].ID]), "fix login now"; got != want {
		t.Fatalf("raw capture buffer = %q, want %q", got, want)
	}
}

func TestTitleHookBuffersShiftEnterUntilSubmit(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Generated title\\n'"

	model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": "text", "text": "first"})
	cmd := model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": codexInputShiftEnter})
	if cmd != nil {
		t.Fatal("shift enter should not submit the first message")
	}
	model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": "text", "text": "second"})
	cmd = model.captureCodexInputArgs(model.state.Tasks[0], map[string]string{"input": "enter"})
	if cmd == nil {
		t.Fatal("expected title hook command")
	}
	msg := titleHookMessageFromCmd(t, cmd)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"first_message":"first\nsecond"`) {
		t.Fatalf("payload missing multiline first message:\n%s", raw)
	}
}

func TestTitleHookDoesNotRetryAfterAttempt(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.cfg.TitleHookCommand = "false"

	model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first")})
	cmd := model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected first hook attempt")
	}
	if msg := cmd().(titleHookMsg); msg.err == nil {
		t.Fatal("expected hook error")
	}
	model.captureCodexInput(*state.TaskByID(model.state, "a"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second")})
	cmd = model.captureCodexInput(*state.TaskByID(model.state, "a"), tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("hook should not retry after first attempt")
	}
}

func TestTitleHookFailureRecordsFullError(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	detail := "curl: (56) HTTP/2 stream 1 was reset while reading the OpenAI response after the request was accepted; retryable receive failure with request id req-title-transport-56 and status text requested URL returned incomplete body"
	model.cfg.TitleHookCommand = "printf '%s\\n' " + shellQuote(detail) + " >&2; exit 56"

	model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first")})
	cmd := model.captureCodexInput(model.state.Tasks[0], tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected title hook command")
	}
	msg := cmd().(titleHookMsg)
	if msg.err == nil {
		t.Fatal("expected hook error")
	}
	model.applyTitleHook(msg)

	task := state.TaskByID(model.state, "a")
	if task == nil || !strings.Contains(task.AutoTitleError, detail) || strings.Contains(task.AutoTitleError, "...") {
		t.Fatalf("task auto title error = %#v", task)
	}
	if !strings.Contains(model.message, "auto title hook failed") || !strings.Contains(model.message, "returned incomplete body") {
		t.Fatalf("message = %q", model.message)
	}
}

func TestActiveOutputPreservesTerminalStyles(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	screen := NewTerminalScreen(20, 3)
	screen.Write("\x1b[48;2;1;2;3m input \x1b[0m")
	model.screens["a"] = screen

	output := model.activeOutput()

	if !strings.Contains(output, "\x1b[48;2;1;2;3m input \x1b[m") {
		t.Fatalf("active output should preserve terminal styling:\n%q", output)
	}
	if stripped := ansi.Strip(output); !strings.Contains(stripped, " input ") {
		t.Fatalf("styled active output should strip to visible screen content:\nplain  %q\nstyled %q", screen.String(), stripped)
	}
}

func TestActiveOutputPaintsCursorOnlyWhenCodexFocused(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	screen := NewTerminalScreen(20, 3)
	screen.Write("prompt")
	model.screens["a"] = screen

	output := model.activeOutput()
	if !strings.Contains(output, "48;2;255;255;255") {
		t.Fatalf("codex-focused output should paint terminal cursor:\n%q", output)
	}

	model.state.Focus = state.FocusTasks
	model.state.NavOpen = true
	output = model.activeOutput()
	if strings.Contains(output, "48;2;255;255;255") {
		t.Fatalf("nav-focused output should not paint Codex cursor:\n%q", output)
	}
}

func TestMovingWorkspaceSelectionClearsStaleGroup(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	model := NewModel(rt, config.DefaultConfig(), state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "alpha",
		SelectedGroupID:     "alpha-group",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces: []state.Workspace{
			{ID: "alpha", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now},
			{ID: "beta", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now},
		},
		Groups: []state.Group{{ID: "alpha-group", WorkspaceID: "alpha", Path: "alpha", CreatedAt: now, UpdatedAt: now}},
	})

	model.moveSelection(1)

	if model.state.SelectedWorkspaceID != "beta" || model.state.SelectedGroupID != "" || model.state.SelectedTaskID != "" {
		t.Fatalf("workspace move should clear stale task/group selection: %#v", model.state)
	}
}

func TestClientShiftUpOnSelectedWorkspaceRequestsReorderWorkspace(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "beta",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces: []state.Workspace{
			{ID: "alpha", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now},
			{ID: "beta", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now},
		},
	}
	requests := make(chan ipc.Request, 1)
	stop, err := ipc.Serve(rt.SocketPath, func(request ipc.Request) ipc.Response {
		requests <- request
		snapshot := ipc.Snapshot{State: st}
		return ipc.Response{OK: true, Snapshot: &snapshot}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	model := NewClientModel(rt, config.DefaultConfig())
	model.snapshot = ipc.Snapshot{State: st}
	_, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyShiftUp})
	if cmd == nil {
		t.Fatal("expected client reorder command")
	}
	msg := cmd()
	if response, ok := msg.(clientResponseMsg); !ok || response.err != nil {
		t.Fatalf("client command response = %#v", msg)
	}
	select {
	case request := <-requests:
		if request.Command != "reorder_workspace" || request.Args["id"] != "beta" || request.Args["delta"] != "-1" {
			t.Fatalf("client request = %#v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for client request")
	}
}

func TestSupervisorReorderWorkspaceRequestMovesSelectedWorkspace(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	model := NewModel(rt, config.DefaultConfig(), state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "beta",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces: []state.Workspace{
			{ID: "alpha", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now},
			{ID: "beta", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now},
		},
	})
	response, _ := model.HandleSupervisorRequest(ipc.Request{Command: "reorder_workspace", Args: map[string]string{"id": "beta", "delta": "-1"}})
	if !response.OK {
		t.Fatalf("reorder_workspace response = %#v", response)
	}
	if got, want := tuiWorkspaceIDs(model.state.Workspaces), []string{"beta", "alpha"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("supervisor workspace order = %#v, want %#v", got, want)
	}
	if model.state.SelectedWorkspaceID != "beta" || model.state.Focus != state.FocusWorkspaces {
		t.Fatalf("supervisor workspace selection = %#v", model.state)
	}
}

func TestClientShiftUpOnSelectedGroupRequestsReorderGroup(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "beta",
		Focus:               state.FocusTasks,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "alpha", WorkspaceID: "w", Path: "alpha", CreatedAt: now, UpdatedAt: now},
			{ID: "beta", WorkspaceID: "w", Path: "beta", CreatedAt: now, UpdatedAt: now},
		},
	}
	requests := make(chan ipc.Request, 1)
	stop, err := ipc.Serve(rt.SocketPath, func(request ipc.Request) ipc.Response {
		requests <- request
		snapshot := ipc.Snapshot{State: st, GroupCursor: 2}
		return ipc.Response{OK: true, Snapshot: &snapshot}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	model := NewClientModel(rt, config.DefaultConfig())
	model.snapshot = ipc.Snapshot{State: st, GroupCursor: 2}
	_, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyShiftUp})
	if cmd == nil {
		t.Fatal("expected client reorder command")
	}
	msg := cmd()
	if response, ok := msg.(clientResponseMsg); !ok || response.err != nil {
		t.Fatalf("client command response = %#v", msg)
	}
	select {
	case request := <-requests:
		if request.Command != "reorder_group" || request.Args["id"] != "beta" || request.Args["delta"] != "-1" {
			t.Fatalf("client request = %#v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for client request")
	}
}

func TestSupervisorReorderGroupRequestMovesSelectedGroup(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	model := NewModel(rt, config.DefaultConfig(), state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "beta",
		Focus:               state.FocusTasks,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "alpha", WorkspaceID: "w", Path: "alpha", CreatedAt: now, UpdatedAt: now},
			{ID: "beta", WorkspaceID: "w", Path: "beta", CreatedAt: now, UpdatedAt: now},
		},
	})
	response, _ := model.HandleSupervisorRequest(ipc.Request{Command: "reorder_group", Args: map[string]string{"id": "beta", "delta": "-1"}})
	if !response.OK {
		t.Fatalf("reorder_group response = %#v", response)
	}
	if got, want := tuiGroupIDs(state.GroupsForWorkspace(model.state, "w")), []string{"beta", "alpha"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("supervisor group order = %#v, want %#v", got, want)
	}
	if row := model.currentGroupRow(); row.kind != groupRowGroup || row.groupID != "beta" {
		t.Fatalf("supervisor selection row = %#v", row)
	}
}

func TestGroupCursorSyncDoesNotSnapGroupRowsBackToActiveTask(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	model := NewModel(rt, config.DefaultConfig(), state.State{
		Version:             state.Version,
		ActiveTaskID:        "planning-task",
		SelectedTaskID:      "planning-task",
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "planning",
		Focus:               state.FocusTasks,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "in-progress", WorkspaceID: "w", Path: "in progress", CreatedAt: now, UpdatedAt: now},
			{ID: "planning", WorkspaceID: "w", Path: "planning", CreatedAt: now, UpdatedAt: now},
			{ID: "shipit", WorkspaceID: "w", Path: "shipit", CreatedAt: now, UpdatedAt: now},
		},
		Tasks: []state.Task{
			{ID: "progress-a", WorkspaceID: "w", GroupID: "in-progress", Title: "Progress A", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "progress-b", WorkspaceID: "w", GroupID: "in-progress", Title: "Progress B", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "planning-task", WorkspaceID: "w", GroupID: "planning", Title: "Planning", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now},
		},
	})

	if row := model.currentGroupRow(); row.kind != groupRowTask || row.taskID != "planning-task" {
		t.Fatalf("initial cursor row = %#v, want planning task", row)
	}

	model.moveSelection(-1)
	if row := model.currentGroupRow(); row.kind != groupRowGroup || row.groupID != "planning" {
		t.Fatalf("up should move to planning group row, got %#v", row)
	}
	model.syncGroupCursor()
	if row := model.currentGroupRow(); row.kind != groupRowGroup || row.groupID != "planning" {
		t.Fatalf("sync should keep planning group row, got %#v", row)
	}

	model.syncGroupCursorToTask("planning-task")
	model.moveSelection(1)
	if row := model.currentGroupRow(); row.kind != groupRowGroup || row.groupID != "shipit" {
		t.Fatalf("down should move to shipit group row, got %#v", row)
	}
	model.syncGroupCursor()
	if row := model.currentGroupRow(); row.kind != groupRowGroup || row.groupID != "shipit" {
		t.Fatalf("sync should keep shipit group row, got %#v", row)
	}
}

func TestGroupCursorSyncRestoresPersistedGroupRow(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	model := NewModel(rt, config.DefaultConfig(), state.State{
		Version:             state.Version,
		ActiveTaskID:        "planning-task",
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "planning",
		Focus:               state.FocusTasks,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "in-progress", WorkspaceID: "w", Path: "in progress", CreatedAt: now, UpdatedAt: now},
			{ID: "planning", WorkspaceID: "w", Path: "planning", CreatedAt: now, UpdatedAt: now},
		},
		Tasks: []state.Task{
			{ID: "progress-a", WorkspaceID: "w", GroupID: "in-progress", Title: "Progress A", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "planning-task", WorkspaceID: "w", GroupID: "planning", Title: "Planning", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now},
		},
	})

	if row := model.currentGroupRow(); row.kind != groupRowGroup || row.groupID != "planning" {
		t.Fatalf("persisted group row should win after model start, got %#v", row)
	}
}

func TestSnapshotSyncsGroupCursorToSelectedTask(t *testing.T) {
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		ActiveTaskID:        "release-task",
		SelectedTaskID:      "release-task",
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "release",
		Focus:               state.FocusTasks,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "shipit", WorkspaceID: "w", Path: "shipit", CreatedAt: now, UpdatedAt: now},
			{ID: "release", WorkspaceID: "w", Path: "release queue", CreatedAt: now, UpdatedAt: now},
		},
		Tasks: []state.Task{
			{ID: "ship-task", WorkspaceID: "w", GroupID: "shipit", Title: "ship", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "release-task", WorkspaceID: "w", GroupID: "release", Title: "release", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
		},
	}
	model := Model{
		state:             st,
		screens:           map[string]*TerminalScreen{},
		ptys:              map[string]*ptyx.Session{},
		visible:           map[string]bool{},
		codexInputBuffers: map[string][]rune{},
		terminalCommands:  map[string]time.Time{},
		taskInterrupts:    map[string]time.Time{},
		sessionCaptures:   map[string]time.Time{},
	}
	model.groupCursor = 2
	model.groupCursorPinned = true

	snapshot := model.Snapshot()
	row := currentGroupRowForState(snapshot.State, snapshot.GroupCursor)
	if row.kind != groupRowTask || row.taskID != "release-task" {
		t.Fatalf("snapshot cursor row = %#v, cursor=%d", row, snapshot.GroupCursor)
	}
}

func TestIPCNewCopiesConfiguredTitleTemplate(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks = nil
	model.state.ActiveTaskID = ""

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{}})
	defer killPTYs(model)

	if !response.OK || cmd == nil {
		t.Fatalf("new response/cmd = %#v/%v", response, cmd)
	}
	if len(model.state.Tasks) != 1 || model.state.Tasks[0].Title != model.cfg.TaskTypes[config.DefaultTaskTypeCodex].TitleTemplate {
		t.Fatalf("tasks = %#v", model.state.Tasks)
	}
}

func TestIPCNewCreatesRequestedTaskType(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks = nil
	model.state.ActiveTaskID = ""

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{"type": config.DefaultTaskTypeShell}})
	defer killPTYs(model)

	if !response.OK || cmd == nil {
		t.Fatalf("new response/cmd = %#v/%v", response, cmd)
	}
	if len(model.state.Tasks) != 1 {
		t.Fatalf("tasks = %#v", model.state.Tasks)
	}
	task := model.state.Tasks[0]
	if task.TypeID != config.DefaultTaskTypeShell || task.Title != "Shell" {
		t.Fatalf("shell task = %#v", task)
	}
}

func TestIPCNewAllowsTransportMetadata(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks = nil
	model.state.ActiveTaskID = ""

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{
		"client_id": "dashboard-1",
		"width":     "120",
		"height":    "40",
	}})
	defer killPTYs(model)

	if !response.OK || cmd == nil {
		t.Fatalf("new response/cmd = %#v/%v", response, cmd)
	}
	if len(model.state.Tasks) != 1 || model.state.Tasks[0].TypeID != config.DefaultTaskTypeCodex {
		t.Fatalf("tasks = %#v", model.state.Tasks)
	}
}

func TestIPCNewRejectsUnknownTaskType(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks = nil
	model.state.ActiveTaskID = ""

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{"type": "ghost"}})
	defer killPTYs(model)

	if response.OK || cmd != nil {
		t.Fatalf("new response/cmd = %#v/%v", response, cmd)
	}
	if response.Error == nil || response.Error.Code != "task_type_not_found" || !strings.Contains(response.Message, "ghost") {
		t.Fatalf("expected unknown task type error: %#v", response)
	}
	if len(model.state.Tasks) != 0 {
		t.Fatalf("tasks should not be created: %#v", model.state.Tasks)
	}
}

func TestIPCNewRejectsUnsupportedArgument(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks = nil
	model.state.ActiveTaskID = ""

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{"unexpected": config.DefaultTaskTypeShell}})
	defer killPTYs(model)

	if response.OK || cmd != nil {
		t.Fatalf("new response/cmd = %#v/%v", response, cmd)
	}
	if response.Error == nil || response.Error.Code != "unsupported_arg" || !strings.Contains(response.Message, "unexpected") {
		t.Fatalf("expected unsupported arg error: %#v", response)
	}
	if len(model.state.Tasks) != 0 {
		t.Fatalf("tasks should not be created: %#v", model.state.Tasks)
	}
}

func TestIPCMoveRejectsUnsupportedArgument(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	response, cmd := model.handleIPC(ipc.Request{Command: "move", Args: map[string]string{"unexpected": "true"}})
	if response.OK || cmd != nil {
		t.Fatalf("move response/cmd = %#v/%v", response, cmd)
	}
	if response.Error == nil || response.Error.Code != "unsupported_arg" || !strings.Contains(response.Message, "unexpected") {
		t.Fatalf("expected unsupported arg error: %#v", response)
	}
}

func TestIPCMoveAcceptsCurrentGroupPathArg(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	now := state.NowISO()
	model.state.Groups = append(model.state.Groups, state.Group{ID: "release", WorkspaceID: "w", Path: "release", CreatedAt: now, UpdatedAt: now})

	response, cmd := model.handleIPC(ipc.Request{Command: "move", Args: map[string]string{"group": "release"}})

	if !response.OK || cmd != nil {
		t.Fatalf("move response/cmd = %#v/%v", response, cmd)
	}
	if task := state.TaskByID(model.state, "a"); task == nil || task.GroupID != "release" {
		t.Fatalf("task not moved to release: %#v", task)
	}
}

func TestTerminalTaskInputBypassesCodexCapture(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks[0].TypeID = config.DefaultTaskTypeShell
	model.state.Focus = state.FocusConsole
	model.state.NavOpen = false

	cmd := model.applyCodexInput(map[string]string{"input": "text", "text": "hello", "encoded": "hello"})

	if cmd != nil {
		t.Fatalf("terminal input should not start title hook command, got %#v", cmd)
	}
	if got := string(model.codexInputBuffers[model.state.Tasks[0].ID]); got != "hello" {
		t.Fatalf("terminal input should track the pending command line without starting a title hook, got %q", got)
	}
}

func TestTerminalTaskCommandShowsLoadingUntilForegroundReturns(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks[0].TypeID = config.DefaultTaskTypeShell
	model.state.Tasks[0].Status = state.StatusReady
	model.state.Focus = state.FocusConsole
	model.state.NavOpen = false

	if cmd := model.applyTaskInput(map[string]string{"input": codexInputRaw, "encoded": "sleep 10"}); cmd != nil {
		t.Fatal("command text should not start loading before Enter")
	}
	cmd := model.applyTaskInput(map[string]string{"input": codexInputRaw, "encoded": "\r"})
	if cmd == nil {
		t.Fatal("terminal command should start loading tick")
	}
	task := state.TaskByID(model.state, "a")
	if task == nil || task.Status != state.StatusRunning {
		t.Fatalf("terminal command should mark task running: %#v", task)
	}
	snapshot := model.Snapshot()
	if len(snapshot.LoadingTaskIDs) != 1 || snapshot.LoadingTaskIDs[0] != "a" {
		t.Fatalf("terminal command loading ids = %#v", snapshot.LoadingTaskIDs)
	}
	if _, ok := snapshot.TaskOperationStartedAt["a"]; !ok {
		t.Fatalf("terminal command operation start missing: %#v", snapshot.TaskOperationStartedAt)
	}

	model.terminalCommands["a"] = time.Now().Add(-terminalCommandLoadingFloor - time.Millisecond)
	model.refreshTerminalTaskActivity()
	task = state.TaskByID(model.state, "a")
	if task == nil || task.Status != state.StatusReady {
		t.Fatalf("terminal command should become ready when no foreground job is active: %#v", task)
	}
	snapshot = model.Snapshot()
	if len(snapshot.TaskOperationStartedAt) != 0 {
		t.Fatalf("ready terminal command should clear operation start: %#v", snapshot.TaskOperationStartedAt)
	}
}

func TestTerminalTaskAutoTitleCapturesFirstCommandWhenOptedIn(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Shell title\\n'"
	model.state.Tasks[0].TypeID = config.DefaultTaskTypeShell
	model.state.Tasks[0].Title = "{auto}"
	model.state.Tasks[0].Status = state.StatusReady
	model.state.Focus = state.FocusConsole
	model.state.NavOpen = false

	if cmd := model.applyTaskInput(map[string]string{"input": codexInputRaw, "encoded": "echo stale"}); cmd != nil {
		t.Fatal("hook should not run before Enter")
	}
	if cmd := model.applyTaskInput(map[string]string{"input": codexInputRaw, "encoded": "\x15"}); cmd != nil {
		t.Fatal("hook should not run for C-u")
	}
	if cmd := model.applyTaskInput(map[string]string{"input": codexInputRaw, "encoded": "echo hello"}); cmd != nil {
		t.Fatal("hook should not run before Enter")
	}
	cmd := model.applyTaskInput(map[string]string{"input": codexInputRaw, "encoded": "\r"})
	if cmd == nil {
		t.Fatal("expected title hook command")
	}

	msg := titleHookMessageFromCmd(t, cmd)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	model.applyTitleHook(msg)
	task := state.TaskByID(model.state, "a")
	if task == nil || task.AutoTitle != "Shell title" {
		t.Fatalf("auto title = %#v", task)
	}
	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type_id":"shell"`) || !strings.Contains(string(raw), `"first_message":"echo hello"`) {
		t.Fatalf("payload missing shell command context:\n%s", raw)
	}
}

func TestTerminalTaskClearResetsVisibleScreen(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks[0].TypeID = config.DefaultTaskTypeShell
	model.state.Focus = state.FocusConsole
	model.state.NavOpen = false
	screen := NewTerminalScreen(20, 3)
	screen.Write("old output")
	model.screens["a"] = screen
	model.visible["a"] = true

	model.clearActiveTerminal()

	if strings.Contains(model.activeOutput(), "old output") {
		t.Fatalf("terminal clear should remove old output:\n%s", model.activeOutput())
	}
}

func TestTerminalTaskPTYTitleDoesNotBecomeLoading(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks[0].TypeID = config.DefaultTaskTypeShell
	model.state.Tasks[0].Status = state.StatusReady

	model.applyPTYData(ptyx.Data{TaskID: "a", Title: "zsh", Text: "prompt"})

	task := state.TaskByID(model.state, "a")
	if task == nil || task.Status != state.StatusReady {
		t.Fatalf("terminal task should stay ready after shell title output: %#v", task)
	}
	if model.taskLoading("a") {
		t.Fatalf("terminal task should not show loading after PTY title")
	}
}

func TestIPCOpenOnGroupTogglesCollapse(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusTasks
	model.state.NavOpen = true
	model.groupCursor = 1

	response, _ := model.handleIPC(ipc.Request{Command: "open"})
	if !response.OK {
		t.Fatalf("open response = %#v", response)
	}
	if !state.IsGroupCollapsed(model.state, "f") {
		t.Fatalf("group should collapse: %#v", model.state.CollapsedGroupIDs)
	}
	if rows := model.groupRows(); len(rows) != 2 {
		t.Fatalf("collapsed group should hide tasks, rows=%#v", rows)
	}

	response, _ = model.handleIPC(ipc.Request{Command: "open"})
	if !response.OK {
		t.Fatalf("open response = %#v", response)
	}
	if state.IsGroupCollapsed(model.state, "f") {
		t.Fatalf("group should reopen: %#v", model.state.CollapsedGroupIDs)
	}
}

func TestNewTaskAlwaysCreatesTopLevelWhenGroupSelected(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusTasks
	model.state.NavOpen = true
	model.state.ActiveTaskID = ""
	model.groupCursor = 1

	cmd := model.newTask("Grouped")
	defer killPTYs(model)
	if cmd == nil {
		t.Fatal("expected PTY start command")
	}
	if got := model.state.Tasks[len(model.state.Tasks)-1].GroupID; got != "" {
		t.Fatalf("group row should create top-level task, got group %q", got)
	}
}

func TestNewTaskAlwaysCreatesTopLevelWhenTaskSelected(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.NavOpen = true
	model.state.Focus = state.FocusTasks
	model.syncGroupCursor()

	cmd := model.newTask("Top-level")
	defer killPTYs(model)
	if cmd == nil {
		t.Fatal("expected PTY start command")
	}
	if got := model.state.Tasks[len(model.state.Tasks)-1].GroupID; got != "" {
		t.Fatalf("grouped task row should create top-level task, got group %q", got)
	}
}

func TestIPCFocusRejectsGroupsAlias(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	response, _ := model.handleIPC(ipc.Request{Command: "focus", Args: map[string]string{"target": "groups"}})

	if response.OK || response.Message != "focus target must be workspaces, tasks, or console" {
		t.Fatalf("focus groups response = %#v", response)
	}
}

func TestNavWidthSnapsOnDrawerToggle(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.width = 120
	model.height = 32
	model.state.Focus = state.FocusTasks
	model.state.NavOpen = true
	model.navWidth = model.targetNavWidth()

	expanded := model.navWidth
	if expanded <= 0 {
		t.Fatalf("expanded nav width = %d", expanded)
	}
	if cmd := model.setCodexFocus(); cmd != nil {
		t.Fatalf("codex focus should not start legacy nav animation, got %#v", cmd)
	}
	model.snapNavWidthToTarget()
	if model.navWidth != 0 {
		t.Fatalf("codex focus nav width = %d, want 0", model.navWidth)
	}

	if cmd := model.openNav(); cmd != nil {
		t.Fatalf("opening nav should not start legacy nav animation, got %#v", cmd)
	}
	model.snapNavWidthToTarget()
	if model.navWidth != expanded {
		t.Fatalf("nav width after open = %d, want %d", model.navWidth, expanded)
	}
}

func titleHookMessageFromCmd(t *testing.T, cmd tea.Cmd) titleHookMsg {
	t.Helper()
	msg := cmd()
	if hook, ok := msg.(titleHookMsg); ok {
		return hook
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, next := range batch {
			if hook, ok := next().(titleHookMsg); ok {
				return hook
			}
		}
	}
	t.Fatalf("command did not produce titleHookMsg: %#v", msg)
	return titleHookMsg{}
}

func testModelWithTask(t *testing.T) Model {
	t.Helper()
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	codexType := cfg.TaskTypes[config.DefaultTaskTypeCodex]
	codexType.Command = "cat"
	cfg.TaskTypes[config.DefaultTaskTypeCodex] = codexType
	st := testStateWithTask(rt.Workspace)
	return NewModel(rt, cfg, st)
}

func testStateWithTask(workspace string) state.State {
	now := state.NowISO()
	return state.State{
		Version:             state.Version,
		ActiveTaskID:        "a",
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "f",
		Focus:               state.FocusConsole,
		NavOpen:             false,
		Workspaces:          []state.Workspace{{ID: "w", Path: workspace, CreatedAt: now, UpdatedAt: now}},
		Groups:              []state.Group{{ID: "f", WorkspaceID: "w", Path: "inbox", CreatedAt: now, UpdatedAt: now}},
		Tasks:               []state.Task{{ID: "a", WorkspaceID: "w", GroupID: "f", TypeID: config.DefaultTaskTypeCodex, Title: "alpha", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now}},
	}
}

func testStateWithWorkspace(t *testing.T, workspace string) state.State {
	t.Helper()
	st, _, err := state.AddWorkspace(state.Empty(), "w", workspace, state.NowISO())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func killPTYs(model Model) {
	for _, pty := range model.ptys {
		pty.Kill()
	}
}

func testOperationStarts(entries map[string]time.Time) *sync.Map {
	starts := &sync.Map{}
	for taskID, started := range entries {
		starts.Store(taskID, started)
	}
	return starts
}

func modelOperationStart(model Model, taskID string) (time.Time, bool) {
	if model.operationStarts == nil {
		return time.Time{}, false
	}
	value, ok := model.operationStarts.Load(taskID)
	if !ok {
		return time.Time{}, false
	}
	switch typed := value.(type) {
	case taskOperationStart:
		return typed.startedAt, !typed.startedAt.IsZero()
	case time.Time:
		return typed, !typed.IsZero()
	default:
		return time.Time{}, false
	}
}

func tuiWorkspaceIDs(workspaces []state.Workspace) []string {
	ids := make([]string, 0, len(workspaces))
	for _, workspace := range workspaces {
		ids = append(ids, workspace.ID)
	}
	return ids
}

func tuiGroupIDs(groups []state.Group) []string {
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	return ids
}

func testRuntime(t *testing.T) config.Runtime {
	t.Helper()
	dir := t.TempDir()
	return config.Runtime{
		Workspace:  dir,
		Dir:        dir,
		ConfigPath: filepath.Join(dir, "config.toml"),
		StatePath:  filepath.Join(dir, "state.json"),
		SocketPath: filepath.Join(dir, "weft.sock"),
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
