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

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/ptyx"
	"github.com/edwmurph/weft/internal/state"
	weftversion "github.com/edwmurph/weft/internal/version"
	"github.com/muesli/termenv"
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

func TestNewTaskModalRendersAndTogglesSilentCheckbox(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	cfg := config.DefaultConfig()
	input := textinput.New()
	index := defaultTaskTypeIndex(cfg)
	configureNewTaskTitleInput(&input, cfg, index)

	raw := renderNewTaskModal(cfg, index, input, 60, 2, false, false)
	rendered := ansi.Strip(raw)
	if !strings.Contains(rendered, "Title") || !strings.Contains(rendered, "[ ] Silent") || !strings.Contains(rendered, "Variables") || !strings.Contains(rendered, "{live}") {
		t.Fatalf("new task modal missing title, variables, or silent checkbox:\n%s", rendered)
	}
	if strings.Index(rendered, "[ ] Silent") > strings.Index(rendered, "Title") {
		t.Fatalf("new task modal should render silent before title:\n%s", rendered)
	}
	if !strings.Contains(raw, "\x1b[38;5;117m╭") {
		t.Fatalf("focused title input should use blue border:\n%s", raw)
	}
	if strings.Contains(rendered, "Space choices") || strings.Contains(rendered, "Space toggle") {
		t.Fatalf("title field actions should not advertise unrelated Space actions:\n%s", rendered)
	}
	if !strings.Contains(rendered, "↑/↓ move") || strings.Contains(rendered, "Up/Down move") {
		t.Fatalf("new task modal should use arrow glyphs for field movement:\n%s", rendered)
	}

	result := handleNewTaskKey(cfg, index, input, 2, false, false, tea.KeyMsg{Type: tea.KeyUp})
	if result.field != 1 || result.silent {
		t.Fatalf("up should focus silent checkbox without toggling: %#v", result)
	}
	raw = renderNewTaskModal(cfg, result.index, result.input, 60, result.field, result.silent, result.typeOpen)
	if !strings.Contains(raw, modalKeyStyle.Render("[ ]")+" "+modalValueStyle.Render("Silent")) {
		t.Fatalf("focused silent checkbox should color only the checkbox glyph:\n%s", raw)
	}
	if rendered := ansi.Strip(raw); !strings.Contains(rendered, "Space toggle") || strings.Contains(rendered, "Space choices") {
		t.Fatalf("silent field actions should advertise only checkbox toggling:\n%s", rendered)
	}

	result = handleNewTaskKey(cfg, result.index, result.input, result.field, result.silent, result.typeOpen, tea.KeyMsg{Type: tea.KeySpace})
	if result.field != 1 || !result.silent {
		t.Fatalf("space should toggle silent checkbox: %#v", result)
	}

	rendered = ansi.Strip(renderNewTaskModal(cfg, result.index, result.input, 60, result.field, result.silent, result.typeOpen))
	if !strings.Contains(rendered, "[x] Silent") {
		t.Fatalf("new task modal should render checked checkbox:\n%s", rendered)
	}

	result = handleNewTaskKey(cfg, result.index, result.input, result.field, result.silent, result.typeOpen, tea.KeyMsg{Type: tea.KeyUp})
	if result.field != 0 || !result.silent {
		t.Fatalf("up should move to type while keeping checkbox state: %#v", result)
	}
	raw = renderNewTaskModal(cfg, result.index, result.input, 60, result.field, result.silent, result.typeOpen)
	if rendered := ansi.Strip(raw); !strings.Contains(rendered, "Space choices") || !strings.Contains(rendered, "←/→ type") || strings.Contains(rendered, "Space toggle") {
		t.Fatalf("type field actions should advertise only type controls:\n%s", rendered)
	}
	result = handleNewTaskKey(cfg, result.index, result.input, result.field, result.silent, result.typeOpen, tea.KeyMsg{Type: tea.KeySpace})
	if !result.typeOpen {
		t.Fatalf("space on type should open dropdown: %#v", result)
	}
	raw = renderNewTaskModal(cfg, result.index, result.input, 60, result.field, result.silent, result.typeOpen)
	if !strings.Contains(raw, "\x1b[38;5;117m╭") {
		t.Fatalf("focused type field should use blue border:\n%s", raw)
	}
	result = handleNewTaskKey(cfg, result.index, result.input, result.field, result.silent, result.typeOpen, tea.KeyMsg{Type: tea.KeyDown})
	if result.index == index || result.field != 0 || !result.silent || !result.typeOpen {
		t.Fatalf("down in dropdown should move task type while keeping field and checkbox state: %#v", result)
	}
	rendered = ansi.Strip(renderNewTaskModal(cfg, result.index, result.input, 60, result.field, result.silent, result.typeOpen))
	if !strings.Contains(rendered, "> Shell") {
		t.Fatalf("new task modal should render open type dropdown:\n%s", rendered)
	}
}

func TestPromptActionFootersUseArrowGlyphs(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	ctx := promptContext{prompt: promptMoveTask, state: st, selectedTask: &st.Tasks[0]}
	input := textinput.New()
	input.SetValue("in")

	closed := ansi.Strip(renderPromptActions(ctx, input, false))
	if !strings.Contains(closed, "↓ suggestions") || strings.Contains(closed, "Down suggestions") {
		t.Fatalf("closed autocomplete footer should use down arrow glyph:\n%s", closed)
	}

	open := ansi.Strip(renderPromptActions(ctx, input, true))
	if !strings.Contains(open, "↑/↓ move") || strings.Contains(open, "Up/Down move") {
		t.Fatalf("open autocomplete footer should use arrow glyphs:\n%s", open)
	}

	silent := ansi.Strip(renderSilentPromptActions(promptEditTask, true))
	if !strings.Contains(silent, "↑/↓ move") || strings.Contains(silent, "Up/Down move") {
		t.Fatalf("silent prompt footer should use arrow glyphs:\n%s", silent)
	}
}

func TestEditTaskModalRendersAndTogglesSilentCheckbox(t *testing.T) {
	st := testStateWithTask(t.TempDir())
	ctx := promptContext{prompt: promptEditTask, pendingID: "a", state: st, selectedTask: &st.Tasks[0]}
	input := textinput.New()
	configurePromptInput(&input, ctx, "Codex {status}")

	extra := renderPromptExtraForState(config.DefaultConfig(), st, promptEditTask, &st.Tasks[0], input, 60)
	rendered := ansi.Strip(renderSilentPromptModal(ctx, input, 60, 20, 1, true, extra))
	if !strings.Contains(rendered, "Title") || !strings.Contains(rendered, "Codex {status}") || !strings.Contains(rendered, "[x] Silent") || !strings.Contains(rendered, "Variables") {
		t.Fatalf("edit task modal missing title, template, or checkbox:\n%s", rendered)
	}
	if strings.Index(rendered, "[x] Silent") > strings.Index(rendered, "Title") {
		t.Fatalf("edit task modal should render silent before title:\n%s", rendered)
	}

	result := handleSilentPromptInputKey(input, ctx, 1, true, tea.KeyMsg{Type: tea.KeyTab})
	result = handleSilentPromptInputKey(result.input, ctx, result.field, result.silent, tea.KeyMsg{Type: tea.KeySpace})
	if result.field != 0 || result.silent {
		t.Fatalf("space should clear existing silent checkbox: %#v", result)
	}

	result = handleSilentPromptInputKey(result.input, ctx, result.field, result.silent, tea.KeyMsg{Type: tea.KeyEnter})
	if result.action != promptInputSubmit || result.value != "Codex {status}" || result.silent {
		t.Fatalf("edit task submit should preserve title variables and unchecked state: %#v", result)
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
	if got := model.state.Tasks[0].LiveTitle; got != "Fake Codex Ready" {
		t.Fatalf("LiveTitle = %q", got)
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
	if task.LiveStatus != "Ready" || task.Status != state.StatusReady {
		t.Fatalf("task status = %s/%q, want ready/Ready", task.Status, task.LiveStatus)
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
	if task == nil || task.LiveStatus != "Running" || task.Status != state.StatusRunning {
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
			if task.LiveStatus != "Ready" || task.Status != state.StatusReady {
				t.Fatalf("prompt status = %s/%q, want ready/Ready", task.Status, task.LiveStatus)
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

	model.state.Tasks[0].LiveTitle = "Fake Codex Ready"
	model.state.Tasks[0].LiveStatus = "Ready"
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
	if got, ok := snapshot.TaskOperationDurations["a"]; !ok || got < 12*time.Second {
		t.Fatalf("ready task completed operation duration = %v/%t, want at least 12s", got, ok)
	}

	model.state.Tasks[0].LiveTitle = "Fake Codex Waiting"
	model.state.Tasks[0].LiveStatus = "Waiting"
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
	if _, ok := ready.TaskOperationDurations["a"]; !ok {
		t.Fatalf("ready Codex task should expose completed operation duration: %#v", ready.TaskOperationDurations)
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
	if task.Status != state.StatusReady || task.LiveStatus != "Ready" {
		t.Fatalf("setup task status = %s/%q, want ready/Ready", task.Status, task.LiveStatus)
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
	if task.Status != state.StatusRunning || task.LiveStatus != string(state.StatusRunning) {
		t.Fatalf("submitted ready prompt should become running, got %s/%q", task.Status, task.LiveStatus)
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
			task: state.Task{ID: "a", Title: "Codex", Status: state.StatusRunning, LiveTitle: "Fake Codex Waiting", LiveStatus: "Waiting"},
			want: true,
		},
		{
			name: "unlisted codex activity title",
			task: state.Task{ID: "a", Title: "Codex", Status: state.StatusRunning, LiveTitle: "Fake Codex Crafting", LiveStatus: "Crafting"},
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
			task: state.Task{ID: "a", Title: "Codex", Status: state.StatusRunning, LiveTitle: "Fake Codex Ready", LiveStatus: "Ready"},
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

func TestSnapshotLaunchWorkspaceFieldDoesNotReselectWorkspace(t *testing.T) {
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

	response, _ := model.handleIPC(ipc.Request{Command: "snapshot", LaunchWorkspace: launch})

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

func TestClientCommandMenuOpensFromHelpAndCanRepaint(t *testing.T) {
	rt := testRuntime(t)
	model := NewClientModel(rt, config.DefaultConfig())
	model.mode = modeHelp

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	model = updated.(ClientModel)

	if model.mode != modeCommand || model.commandMenuReturnMode != modeHelp {
		t.Fatalf("command menu mode/return = %s/%s, want command/help", model.mode, model.commandMenuReturnMode)
	}
	if cmd != nil {
		t.Fatal("opening command menu should not run a command")
	}
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "Task Tools") || !strings.Contains(view, "Task Notes") || !strings.Contains(view, "Repaint") || !strings.Contains(view, "Copy full task console") {
		t.Fatalf("command menu missing expected actions:\n%s", view)
	}
	if !strings.Contains(view, "↑/↓") || !strings.Contains(view, "Select") || strings.Contains(view, "Up/Down select") {
		t.Fatalf("command menu should use arrow glyphs in footer actions:\n%s", view)
	}

	updated, cmd = model.handleCommandMenuKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ClientModel)

	if model.mode != modeHelp {
		t.Fatalf("repaint action should return to help, mode=%s", model.mode)
	}
	if cmd == nil {
		t.Fatal("repaint action should force clear-screen and refresh")
	}
	msg := cmd()
	if got := fmt.Sprintf("%T", msg); got != "tea.sequenceMsg" {
		t.Fatalf("repaint action command = %T, want tea.sequenceMsg", msg)
	}
	sequence := reflect.ValueOf(msg)
	if sequence.Len() != 2 {
		t.Fatalf("repaint action scheduled %d commands, want clear-screen and refresh", sequence.Len())
	}
	first := sequence.Index(0).Interface().(tea.Cmd)
	if got := fmt.Sprintf("%T", first()); got != "tea.clearScreenMsg" {
		t.Fatalf("first repaint action command = %s, want tea.clearScreenMsg", got)
	}
}

func TestClientHelpCtrlRRepaintsScreen(t *testing.T) {
	rt := testRuntime(t)
	model := NewClientModel(rt, config.DefaultConfig())
	model.mode = modeHelp

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})
	model = updated.(ClientModel)

	if model.mode != modeHelp {
		t.Fatalf("C-r should keep help open, mode=%s", model.mode)
	}
	if cmd == nil {
		t.Fatal("C-r should force clear-screen and refresh")
	}
	msg := cmd()
	if got := fmt.Sprintf("%T", msg); got != "tea.sequenceMsg" {
		t.Fatalf("C-r command = %T, want tea.sequenceMsg", msg)
	}
	sequence := reflect.ValueOf(msg)
	if sequence.Len() != 2 {
		t.Fatalf("C-r scheduled %d commands, want clear-screen and refresh", sequence.Len())
	}
	first := sequence.Index(0).Interface().(tea.Cmd)
	if got := fmt.Sprintf("%T", first()); got != "tea.clearScreenMsg" {
		t.Fatalf("first C-r command = %s, want tea.clearScreenMsg", got)
	}
}

func TestClientCommandMenuShowsTaskContextDetail(t *testing.T) {
	rt := testRuntime(t)
	model := NewClientModel(rt, config.DefaultConfig())
	st := state.Empty()
	st.Workspaces = []state.Workspace{{ID: "w", Path: rt.Workspace}}
	st.Tasks = []state.Task{{ID: "t", WorkspaceID: "w", TypeID: "codex", Title: "Codex", Status: state.StatusReady}}
	st.ActiveTaskID = "t"
	st.SelectedWorkspaceID = "w"
	st.SelectedTaskID = "t"
	st.Focus = state.FocusConsole
	st.NavOpen = false
	model.applyResponse(ipc.Response{OK: true, Snapshot: &ipc.Snapshot{
		State:             st,
		ActiveTaskContext: &ipc.TaskContext{TaskID: "t", Heading: "Waiting on CI", Detail: "Run: https://github.com/example/repo/actions/runs/123\nCheck release notes."},
	}})
	model.startCommandMenu()

	view := ansi.Strip(model.View())
	for _, expected := range []string{"Task Tools", "Task Notes", "Waiting on CI", "Run: https://github.com/example", "Check release notes.", "Console Commands", "Copy full task console"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("task notes missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "Context") || strings.Contains(view, "Heading") || strings.Contains(view, "Detail") || strings.Contains(view, "Shortcuts") {
		t.Fatalf("task notes should not spend context space on inferred labels:\n%s", view)
	}

	model.applyResponse(ipc.Response{OK: true, Snapshot: &ipc.Snapshot{
		State:             st,
		ActiveTaskContext: &ipc.TaskContext{TaskID: "t", Preview: "CI wait"},
	}})
	model.startCommandMenu()
	view = ansi.Strip(model.View())
	if !strings.Contains(view, "Task Notes") || !strings.Contains(view, "CI wait") || strings.Contains(view, "No task notes set.") {
		t.Fatalf("task tools should show preview-only task notes as fallback:\n%s", view)
	}
}

func TestClientTaskBriefModalFitsTerminalWidth(t *testing.T) {
	rt := testRuntime(t)
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 120
	model.height = 32
	st := state.Empty()
	st.Workspaces = []state.Workspace{{ID: "w", Path: rt.Workspace}}
	st.Tasks = []state.Task{{ID: "t", WorkspaceID: "w", TypeID: "codex", Title: "Codex", Status: state.StatusReady}}
	st.ActiveTaskID = "t"
	st.SelectedWorkspaceID = "w"
	st.SelectedTaskID = "t"
	st.Focus = state.FocusConsole
	model.applyResponse(ipc.Response{OK: true, Snapshot: &ipc.Snapshot{
		State:             st,
		ActiveTaskContext: &ipc.TaskContext{TaskID: "t", Heading: "Waiting on CI", Detail: "First detail line\nSecond detail line"},
	}})
	model.startCommandMenu()

	view := model.View()
	lines := strings.Split(view, "\n")
	if len(lines) > model.height {
		t.Fatalf("task notes line count = %d, want <= %d:\n%s", len(lines), model.height, ansi.Strip(view))
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > model.width-2 {
			t.Fatalf("task notes line width = %d, want <= %d:\n%q\n%s", width, model.width-2, ansi.Strip(line), ansi.Strip(view))
		}
	}
}

func TestClientCommandMenuCopiesTaskPaneContent(t *testing.T) {
	rt := testRuntime(t)
	model := NewClientModel(rt, config.DefaultConfig())
	st := state.Empty()
	st.Workspaces = []state.Workspace{{ID: "w", Path: rt.Workspace}}
	st.Tasks = []state.Task{{ID: "t", WorkspaceID: "w", TypeID: "codex", Title: "Codex", Status: state.StatusReady}}
	st.ActiveTaskID = "t"
	st.SelectedWorkspaceID = "w"
	st.SelectedTaskID = "t"
	st.Focus = state.FocusConsole
	st.NavOpen = false
	model.applyResponse(ipc.Response{OK: true, Snapshot: &ipc.Snapshot{
		State:                st,
		CodexScrollbackLines: []string{"alpha   ", "", "beta\t", ""},
	}})
	model.startCommandMenu()

	oldWriteClipboard := writeClipboard
	var copied string
	writeClipboard = func(value string) error {
		copied = value
		return nil
	}
	defer func() { writeClipboard = oldWriteClipboard }()

	updated, cmd := model.handleCommandMenuKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	model = updated.(ClientModel)

	if copied != "alpha\n\nbeta" {
		t.Fatalf("copied pane content = %q", copied)
	}
	if model.mode != modeNormal {
		t.Fatalf("copy action should return to normal mode, got %s", model.mode)
	}
	if !strings.Contains(model.toastText, "Copied 11 characters") {
		t.Fatalf("copy action missing toast, got %q", model.toastText)
	}
	if cmd == nil {
		t.Fatal("copy action should schedule toast clear")
	}
}

func TestClientCommandMenuHandlesEnhancedKeyboardActionFromConsole(t *testing.T) {
	rt := testRuntime(t)
	model := NewClientModel(rt, config.DefaultConfig())
	st := state.Empty()
	st.Workspaces = []state.Workspace{{ID: "w", Path: rt.Workspace}}
	st.Tasks = []state.Task{{ID: "t", WorkspaceID: "w", TypeID: "codex", Title: "Codex", Status: state.StatusReady}}
	st.ActiveTaskID = "t"
	st.SelectedWorkspaceID = "w"
	st.SelectedTaskID = "t"
	st.Focus = state.FocusConsole
	st.NavOpen = false
	model.applyResponse(ipc.Response{OK: true, Snapshot: &ipc.Snapshot{State: st}})
	model.startCommandMenu()

	input, ok := enhancedKeyboardInputFromMsg(testCSIMessage(unknownCSIString("\x1b[114u")))
	if !ok {
		t.Fatal("expected enhanced keyboard input for r")
	}
	updated, cmd := model.handleEnhancedKeyboardInput(input)
	model = updated.(ClientModel)

	if model.mode != modeNormal {
		t.Fatalf("enhanced command action should close palette, got mode %s", model.mode)
	}
	if cmd == nil {
		t.Fatal("enhanced repaint action should run repaint command")
	}
}
func TestConfirmShortcutsUseEnterAndEsc(t *testing.T) {
	for _, confirm := range []confirmKind{confirmAddLaunchWorkspace, confirmDeleteWorkspace, confirmDeleteGroup, confirmUpgradeResume, confirmScheduleUpgrade} {
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
	if confirmKeyCancels(confirmDeleteTask, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}) {
		t.Fatal("delete task should not cancel with n")
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
			LiveTitle:           "Task",
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

func TestClientRequestMetadataOnlySelectsLaunchWorkspaceOnAttach(t *testing.T) {
	rt := config.Runtime{Workspace: "/tmp/project"}

	attach := clientRequest(rt, "client-1", 120, 40, "attach_client", nil)
	if attach.ClientID != "client-1" || attach.Width != 120 || attach.Height != 40 {
		t.Fatalf("attach metadata = %#v", attach)
	}
	if attach.LaunchWorkspace != rt.Workspace {
		t.Fatalf("attach launch workspace = %q", attach.LaunchWorkspace)
	}

	nav := clientRequest(rt, "client-1", 120, 40, "nav_move", map[string]string{"delta": "1"})
	if nav.ClientID != "client-1" || nav.Width != 120 || nav.Height != 40 || nav.Args["delta"] != "1" {
		t.Fatalf("nav request = %#v", nav)
	}
	if nav.LaunchWorkspace != "" {
		t.Fatalf("nav request should not reselect launch workspace: %#v", nav)
	}

	upgrade := clientRequest(rt, "client-1", 120, 40, "upgrade_resume", nil)
	if upgrade.ClientID != "client-1" || upgrade.ClientExecutable == "" {
		t.Fatalf("upgrade request = %#v", upgrade)
	}
	if upgrade.LaunchWorkspace != "" {
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
	st.Tasks[0].LiveTitle = "Fake Codex Ready"
	st.Tasks[0].LiveStatus = "Ready"
	st.Tasks[0].ResumeID = "session-alpha"
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, LiveTitle: "alpha", CodexContent: "output", NavWidth: 92},
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

func TestClientUpgradeBridgePausesEditsButAllowsUpgrade(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].LiveTitle = "Fake Codex Ready"
	st.Tasks[0].LiveStatus = "Ready"
	st.Tasks[0].ResumeID = "session-alpha"
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, LiveTitle: "alpha", CodexContent: "output", NavWidth: 92},
		Upgrade:           testClientUpgrade("3.9.0", 1),
		ProtocolVersion:   ipc.UpgradeBridgeMinProtocolVersion,
		SupervisorVersion: "3.9.0",
	})

	if !model.protocolBridgeActive() {
		t.Fatalf("expected protocol bridge for supervisor protocol %d", model.supervisorProtocol)
	}
	got := ansi.Strip(model.View())
	if !strings.Contains(got, "Upgrade bridge: dashboard edits resume after restart.") {
		t.Fatalf("bridge footer missing:\n%s", got)
	}
	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeNormal || !strings.Contains(model.message, "Upgrade bridge is active") {
		t.Fatalf("bridge edit should be paused mode=%s message=%q cmd=%v", model.mode, model.message, cmd)
	}
	updated, cmd = model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmUpgradeResume {
		t.Fatalf("bridge upgrade confirm state mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
}

func TestClientUpgradeWaitsUntilTaskIsIdleAndResumable(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].LiveTitle = "Fake Codex Working"
	st.Tasks[0].LiveStatus = "Working"
	st.Tasks[0].InputSubmitted = true
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, LiveTitle: "alpha", CodexContent: "output", NavWidth: 92},
		Upgrade:           testClientUpgrade("3.9.0", 1),
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: "3.9.0",
	})

	got := ansi.Strip(model.View())
	for _, expected := range []string{"Upgrade pending", "Wait for 1 Codex task(s) to become idle", "Press U to schedule auto-upgrade when ready"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("upgrade wait copy missing %q:\n%s", expected, got)
		}
	}
	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmScheduleUpgrade {
		t.Fatalf("blocked upgrade should open schedule confirm, mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
	got = ansi.Strip(model.View())
	for _, expected := range []string{
		"Upgrade when ready?",
		"supervisor 3.9.0",
		"Enter schedule",
		"keep checking blockers",
		"Keep this dashboard open",
		"upgrade automatically",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("schedule confirm missing %q:\n%s", expected, got)
		}
	}
	updated, cmd = model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeNormal || model.scheduledUpgrade == nil {
		t.Fatalf("schedule confirm should arm without command, mode=%s scheduled=%#v cmd=%v", model.mode, model.scheduledUpgrade, cmd)
	}
	if !strings.Contains(model.message, "Auto-upgrade scheduled") {
		t.Fatalf("schedule message = %q", model.message)
	}
	got = ansi.Strip(model.View())
	for _, expected := range []string{"Auto-upgrade scheduled in this dashboard"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("scheduled footer missing %q:\n%s", expected, got)
		}
	}
	updated, cmd = model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.scheduledUpgrade != nil || model.message != "Scheduled upgrade canceled." {
		t.Fatalf("second U should cancel schedule, scheduled=%#v message=%q cmd=%v", model.scheduledUpgrade, model.message, cmd)
	}
}

func TestScheduledUpgradeRequestsResumeWhenTasksBecomeReady(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].LiveTitle = "Fake Codex Working"
	st.Tasks[0].LiveStatus = "Working"
	st.Tasks[0].InputSubmitted = true
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160
	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, LiveTitle: "alpha", CodexContent: "output", NavWidth: 92},
		Upgrade:           testClientUpgrade("3.9.0", 1),
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: "3.9.0",
	})
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	updated, cmd := model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ClientModel)
	if cmd != nil || model.scheduledUpgrade == nil {
		t.Fatalf("schedule setup failed, scheduled=%#v cmd=%v", model.scheduledUpgrade, cmd)
	}

	st.Tasks[0].Status = state.StatusReady
	st.Tasks[0].LiveTitle = "Fake Codex Ready"
	st.Tasks[0].LiveStatus = "Ready"
	st.Tasks[0].ResumeID = "session-alpha"
	updatedModel, cmd := model.Update(clientResponseMsg{
		command: "snapshot",
		response: ipc.Response{
			OK:                true,
			Snapshot:          &ipc.Snapshot{State: st, LiveTitle: "alpha", CodexContent: "output", NavWidth: 92},
			Upgrade:           testClientUpgrade("3.9.0", 1),
			ProtocolVersion:   ipc.ProtocolVersion,
			SupervisorVersion: "3.9.0",
		},
	})
	model = updatedModel.(ClientModel)
	if cmd == nil || !model.autoUpgradeRequesting {
		t.Fatalf("ready scheduled upgrade should request upgrade_resume, requesting=%t cmd=%v", model.autoUpgradeRequesting, cmd)
	}
	if !strings.Contains(model.message, "Scheduled upgrade is ready") {
		t.Fatalf("ready scheduled message = %q", model.message)
	}
}

func TestClientUpgradeAllowsFreshCodexWithoutSession(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].LiveTitle = "Fake Codex Ready"
	st.Tasks[0].LiveStatus = "Ready"
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, LiveTitle: "alpha", CodexContent: "output", NavWidth: 92},
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

func TestClientUpgradeBlocksForegroundShellTaskWithReadyStatus(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].TypeID = config.DefaultTaskTypeShell
	st.Tasks[0].Title = "Server"
	st.Tasks[0].Status = state.StatusReady
	st.Workspaces[0].Title = "Core"
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK: true,
		Snapshot: &ipc.Snapshot{
			State:                     st,
			NavWidth:                  92,
			TerminalForegroundTaskIDs: []string{st.Tasks[0].ID},
		},
		Upgrade:           testClientUpgrade("3.9.0", 1),
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: "3.9.0",
	})

	got := ansi.Strip(model.View())
	for _, expected := range []string{"Upgrade pending", "Wait for 1 shell task(s) to become idle", "Blocking:", "- workspace: Core", "  task: Server"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("foreground shell footer missing %q:\n%s", expected, got)
		}
	}
	if !strings.Contains(got, "Press U to schedule auto-upgrade when ready") {
		t.Fatalf("foreground shell upgrade should offer scheduling:\n%s", got)
	}
}

func TestClientUpgradeBlocksRunningShellTask(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].TypeID = config.DefaultTaskTypeShell
	st.Tasks[0].Title = "Shell"
	st.Tasks[0].Status = state.StatusRunning
	st.Workspaces[0].Title = "Core"
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
	for _, expected := range []string{"Upgrade pending", "Wait for 1 shell task(s) to become idle", "Blocking:", "- workspace: Core", "  task: Shell"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("running shell footer missing %q:\n%s", expected, got)
		}
	}
	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmScheduleUpgrade {
		t.Fatalf("running shell upgrade should open schedule confirm, mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
}

func TestClientUpgradeBlockerResolvesTaskTitleTemplate(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].TypeID = config.DefaultTaskTypeCodex
	st.Tasks[0].Title = "{status} {auto}"
	st.Tasks[0].AutoTitle = "Fix config"
	st.Tasks[0].Status = state.StatusRunning
	st.Tasks[0].LiveTitle = "Fake Codex Working"
	st.Tasks[0].LiveStatus = "Working"
	st.Workspaces[0].Title = "Core"
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
	for _, expected := range []string{"Upgrade pending", "Wait for 1 Codex task(s) to become idle", "Blocking:", "- workspace: Core", "  task: Working Fix config"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("running Codex footer missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "{status}") || strings.Contains(got, "{auto}") {
		t.Fatalf("running Codex footer leaked title template:\n%s", got)
	}
	if !strings.Contains(got, "Press U to schedule auto-upgrade when ready") {
		t.Fatalf("running Codex upgrade should offer scheduling:\n%s", got)
	}
}

func TestClientConfigDriftFooterUsesUpgradePathAndListsBlocker(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithTask(rt.Workspace)
	st.Tasks[0].TypeID = config.DefaultTaskTypeShell
	st.Tasks[0].Title = "Shell"
	st.Tasks[0].Status = state.StatusRunning
	st.Workspaces[0].Title = "Core"
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:       true,
		Snapshot: &ipc.Snapshot{State: st, NavWidth: 92},
		Upgrade: &ipc.Upgrade{
			ClientVersion:     weftversion.Version,
			SupervisorVersion: weftversion.Version,
			Reason:            ipc.UpgradeReasonConfig,
			Compatible:        true,
			RestartRequired:   true,
			RunningTasks:      1,
			Message:           "Config changed.",
		},
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: weftversion.Version,
		ConfigFingerprint: config.Fingerprint(config.DefaultConfig()),
	})

	got := ansi.Strip(model.View())
	for _, expected := range []string{"Config pending", "config.toml changed", "Wait for 1 shell task(s) to become idle", "Blocking:", "- workspace: Core", "  task: Shell"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("config drift footer missing %q:\n%s", expected, got)
		}
	}
	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmScheduleUpgrade {
		t.Fatalf("blocked config reload should open schedule confirm, mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
}

func TestClientReloadsConfigAfterSupervisorFingerprintChanges(t *testing.T) {
	rt := testRuntime(t)
	changed := strings.Replace(config.DefaultConfigText(), `new_task = "n"`, `new_task = "t"`, 1)
	if err := os.WriteFile(rt.ConfigPath, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	changedCfg, err := config.LoadConfig(rt.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	model := NewClientModel(rt, config.DefaultConfig())

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: state.Empty(), NavWidth: 92},
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: weftversion.Version,
		ConfigFingerprint: config.Fingerprint(changedCfg),
	})

	if model.cfg.KeyBindings.NewTask != "t" {
		t.Fatalf("client config did not reload: %#v", model.cfg.KeyBindings)
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

func TestTerminalUpgradeSnapshotRestoresSavedScreenWidth(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	st := state.State{
		Version:      state.Version,
		ActiveTaskID: "shell",
		Focus:        state.FocusConsole,
		Workspaces: []state.Workspace{{
			ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now,
		}},
		Tasks: []state.Task{{
			ID: "shell", WorkspaceID: "w", TypeID: config.DefaultTaskTypeShell,
			Title: "Shell", Status: state.StatusReady,
			CreatedAt: now, UpdatedAt: now,
		}},
	}
	wideLine := strings.Repeat("L", 120)
	screen := NewTerminalScreen(140, 8)
	screen.Write(wideLine + "\r\n$ ")
	model := Model{
		runtime: rt, cfg: config.DefaultConfig(), state: st, width: 160, height: 12,
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

	restoredScreen := restored.screens["shell"]
	if restoredScreen == nil {
		t.Fatal("snapshot screen was not restored")
	}
	if restoredScreen.cols != 140 {
		t.Fatalf("restored screen cols = %d, want saved width 140", restoredScreen.cols)
	}
	lines := restoredScreen.ScrollbackPlainLines()
	foundWideLine := false
	for _, line := range lines {
		if strings.TrimSpace(line) == wideLine {
			foundWideLine = true
			break
		}
	}
	if !foundWideLine {
		t.Fatalf("restored snapshot should keep the wide row unwrapped:\n%s", restoredScreen.ScrollbackString())
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
	st.Tasks[0].LiveTitle = "fork/exec /missing/zsh: no such file or directory"

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
	model.state.Tasks[0].LiveTitle = "Fake Codex Ready"
	model.state.Tasks[0].LiveStatus = "Ready"

	model.applyPTYData(ptyx.Data{TaskID: "a", Err: os.ErrClosed})

	task := state.TaskByID(model.state, "a")
	if task == nil || task.Status != state.StatusStopped || task.LiveTitle != "Codex exited" {
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
	if task == nil || task.Status != state.StatusKilled || task.LiveTitle != "Codex killed" {
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
	if task == nil || task.Status != state.StatusKilled || task.LiveTitle != "Shell killed" {
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
	if task == nil || task.Status != state.StatusKilled || task.LiveTitle != "Shell killed" {
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

func TestTerminalScreenResizeDoesNotShrinkForPreview(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.width = 160
	model.height = 32
	model.state.Focus = state.FocusTasks
	model.state.NavOpen = true
	model.state.Tasks[0].TypeID = config.DefaultTaskTypeShell
	model.navWidth = model.targetNavWidth()
	taskID := model.state.ActiveTaskID
	model.screens[taskID] = NewTerminalScreen(120, 10)
	targetWidth := model.ptyWidth()
	if targetWidth >= 120 {
		t.Fatalf("test setup expected narrower preview width, got %d", targetWidth)
	}

	model.resizeScreens()

	if got := model.screens[taskID].cols; got != 120 {
		t.Fatalf("terminal preview resize should preserve screen width, got %d", got)
	}
	if got, want := model.screens[taskID].rows, model.ptyHeight(); got != want {
		t.Fatalf("terminal preview resize rows = %d, want %d", got, want)
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
	if task := state.TaskByID(model.state, "a"); task == nil || !task.AutoTitleAttempted || !task.InputSubmitted {
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
	screen.Write("› prompt")
	model.screens["a"] = screen

	output := model.activeOutput()
	if !strings.Contains(output, "48;2;255;255;255") {
		t.Fatalf("codex-focused output should paint terminal cursor:\n%q", output)
	}
	if !strings.Contains(output, "48;2;60;66;71") {
		t.Fatalf("codex-focused output should paint Codex input guide:\n%q", output)
	}

	model.state.Focus = state.FocusTasks
	model.state.NavOpen = true
	output = model.activeOutput()
	if strings.Contains(output, "48;2;255;255;255") {
		t.Fatalf("nav-focused output should not paint Codex cursor:\n%q", output)
	}
	if !strings.Contains(output, "48;2;60;66;71") {
		t.Fatalf("nav-focused Codex preview should keep input guide without cursor:\n%q", output)
	}
}

func TestActiveOutputDoesNotPaintCodexInputGuideForShellTask(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks[0].TypeID = config.DefaultTaskTypeShell
	screen := NewTerminalScreen(20, 3)
	screen.Write("› shell prompt")
	model.screens["a"] = screen

	output := model.activeOutput()

	if strings.Contains(output, "48;2;60;66;71") {
		t.Fatalf("shell task output should not paint Codex input guide:\n%q", output)
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

func TestFocusTasksSyncsCursorBeforeNextNavMove(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	model := NewModel(rt, config.DefaultConfig(), state.State{
		Version:             state.Version,
		ActiveTaskID:        "later-one",
		SelectedTaskID:      "later-one",
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "later",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "earlier", WorkspaceID: "w", Path: "earlier", CreatedAt: now, UpdatedAt: now},
			{ID: "later", WorkspaceID: "w", Path: "later", CreatedAt: now, UpdatedAt: now},
		},
		Tasks: []state.Task{
			{ID: "later-one", WorkspaceID: "w", GroupID: "later", TypeID: config.DefaultTaskTypeCodex, Title: "Later One", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "later-two", WorkspaceID: "w", GroupID: "later", TypeID: config.DefaultTaskTypeCodex, Title: "Later Two", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
		},
	})
	model.groupCursor = 0
	model.groupCursorPinned = false

	response, _ := model.HandleSupervisorRequest(ipc.Request{Command: "focus", Args: map[string]string{"target": string(state.FocusTasks)}})
	if !response.OK {
		t.Fatalf("focus response failed: %#v", response)
	}
	response, _ = model.HandleSupervisorRequest(ipc.Request{Command: "nav_move", Args: map[string]string{"delta": "1"}})
	if !response.OK {
		t.Fatalf("nav response failed: %#v", response)
	}
	if model.state.SelectedTaskID != "later-two" {
		t.Fatalf("down after focus selected %q, want later-two", model.state.SelectedTaskID)
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

func TestIPCNewCreatesSilentTask(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks = nil
	model.state.ActiveTaskID = ""

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{"silent": "true"}})
	defer killPTYs(model)

	if !response.OK || cmd == nil {
		t.Fatalf("new response/cmd = %#v/%v", response, cmd)
	}
	if len(model.state.Tasks) != 1 || !model.state.Tasks[0].Silent {
		t.Fatalf("silent task was not created: %#v", model.state.Tasks)
	}
}

func TestIPCNewRejectsInvalidRequestsWithoutCreatingTask(t *testing.T) {
	tests := []struct {
		name            string
		args            map[string]string
		code            string
		messageContains string
	}{
		{
			name: "invalid silent",
			args: map[string]string{"silent": "sometimes"},
			code: "invalid_silent",
		},
		{
			name:            "transport metadata",
			args:            map[string]string{"client_id": "dashboard-1"},
			code:            "unsupported_arg",
			messageContains: "client_id",
		},
		{
			name:            "unknown task type",
			args:            map[string]string{"type": "ghost"},
			code:            "task_type_not_found",
			messageContains: "ghost",
		},
		{
			name:            "unsupported argument",
			args:            map[string]string{"unexpected": config.DefaultTaskTypeShell},
			code:            "unsupported_arg",
			messageContains: "unexpected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := testModelWithTask(t)
			defer killPTYs(model)
			model.state.Tasks = nil
			model.state.ActiveTaskID = ""

			response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: tt.args})
			if response.OK || cmd != nil {
				t.Fatalf("new response/cmd = %#v/%v", response, cmd)
			}
			if response.Error == nil || response.Error.Code != tt.code || (tt.messageContains != "" && !strings.Contains(response.Message, tt.messageContains)) {
				t.Fatalf("expected %s error containing %q: %#v", tt.code, tt.messageContains, response)
			}
			if len(model.state.Tasks) != 0 {
				t.Fatalf("tasks should not be created: %#v", model.state.Tasks)
			}
		})
	}
}

func TestIPCRenameCanSetAndPreserveTaskSilent(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	response, _ := model.handleIPC(ipc.Request{Command: "rename", Args: map[string]string{"id": "a", "title": "Beta", "silent": "true"}})
	if !response.OK {
		t.Fatalf("rename response = %#v", response)
	}
	if task := state.TaskByID(model.state, "a"); task == nil || task.Title != "Beta" || !task.Silent {
		t.Fatalf("silent rename task = %#v", task)
	}

	response, _ = model.handleIPC(ipc.Request{Command: "rename", Args: map[string]string{"id": "a", "title": "Gamma"}})
	if !response.OK {
		t.Fatalf("rename response = %#v", response)
	}
	if task := state.TaskByID(model.state, "a"); task == nil || task.Title != "Gamma" || !task.Silent {
		t.Fatalf("title-only rename should preserve silence: %#v", task)
	}

	response, _ = model.handleIPC(ipc.Request{Command: "rename", Args: map[string]string{"id": "a", "title": "Delta", "silent": "false"}})
	if !response.OK {
		t.Fatalf("rename response = %#v", response)
	}
	if task := state.TaskByID(model.state, "a"); task == nil || task.Title != "Delta" || task.Silent {
		t.Fatalf("unsilent rename task = %#v", task)
	}
}

func TestIPCRenameRejectsInvalidSilent(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	response, _ := model.handleIPC(ipc.Request{Command: "rename", Args: map[string]string{"id": "a", "title": "Beta", "silent": "sometimes"}})

	if response.OK {
		t.Fatalf("rename response = %#v", response)
	}
	if response.Error == nil || response.Error.Code != "invalid_silent" {
		t.Fatalf("expected invalid silent error: %#v", response)
	}
	if task := state.TaskByID(model.state, "a"); task == nil || task.Title != "alpha" || task.Silent {
		t.Fatalf("invalid silent should not mutate task: %#v", task)
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

func TestIPCTaskContextSetShowClearForActiveCodexTask(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	response, cmd := model.handleIPC(ipc.Request{Command: "task_context_set", Args: map[string]string{"content": "Review PR 123"}})
	if !response.OK || cmd != nil {
		t.Fatalf("set response/cmd = %#v/%v", response, cmd)
	}
	if response.TaskContext == nil || response.TaskContext.TaskID != "a" || response.TaskContext.Heading != "Review PR 123" || response.TaskContext.Summary != "Review PR 123" {
		t.Fatalf("set task notes = %#v", response.TaskContext)
	}
	response, cmd = model.handleIPC(ipc.Request{Command: "task_context_set", Args: map[string]string{"kind": "preview", "content": "CI wait"}})
	if !response.OK || cmd != nil {
		t.Fatalf("set preview response/cmd = %#v/%v", response, cmd)
	}
	if response.TaskContext == nil || response.TaskContext.Preview != "CI wait" || response.TaskContext.Heading != "Review PR 123" || response.TaskContext.Summary != "Review PR 123" {
		t.Fatalf("set preview task notes = %#v", response.TaskContext)
	}
	response, cmd = model.handleIPC(ipc.Request{Command: "task_context_set", Args: map[string]string{"kind": "detail", "content": "next line\nmore detail"}})
	if !response.OK || cmd != nil {
		t.Fatalf("set detail response/cmd = %#v/%v", response, cmd)
	}
	if response.TaskContext == nil || response.TaskContext.Preview != "CI wait" || response.TaskContext.Heading != "Review PR 123" || response.TaskContext.Detail != "next line\nmore detail" {
		t.Fatalf("set detail task notes = %#v", response.TaskContext)
	}
	snapshot := model.Snapshot()
	if snapshot.ActiveTaskContext == nil || snapshot.ActiveTaskContext.Preview != "CI wait" || snapshot.ActiveTaskContext.Heading != "Review PR 123" || snapshot.ActiveTaskContext.Detail != "next line\nmore detail" {
		t.Fatalf("snapshot active notes = %#v", snapshot.ActiveTaskContext)
	}

	response, cmd = model.handleIPC(ipc.Request{Command: "task_context_show", Args: map[string]string{}})
	if !response.OK || cmd != nil || response.TaskContext == nil || response.TaskContext.Preview != "CI wait" || response.TaskContext.Heading != "Review PR 123" || response.TaskContext.Detail != "next line\nmore detail" {
		t.Fatalf("show response/cmd = %#v/%v", response, cmd)
	}

	response, cmd = model.handleIPC(ipc.Request{Command: "task_context_clear", Args: map[string]string{}})
	if !response.OK || cmd != nil {
		t.Fatalf("clear response/cmd = %#v/%v", response, cmd)
	}
	if snapshot := model.Snapshot(); snapshot.ActiveTaskContext == nil || snapshot.ActiveTaskContext.Preview != "CI wait" || snapshot.ActiveTaskContext.Heading != "" || snapshot.ActiveTaskContext.Detail != "next line\nmore detail" {
		t.Fatalf("clearing short note should preserve detail in snapshot: %#v", snapshot.ActiveTaskContext)
	}

	response, cmd = model.handleIPC(ipc.Request{Command: "task_context_clear", Args: map[string]string{"kind": "preview"}})
	if !response.OK || cmd != nil {
		t.Fatalf("clear preview response/cmd = %#v/%v", response, cmd)
	}
	if snapshot := model.Snapshot(); snapshot.ActiveTaskContext == nil || snapshot.ActiveTaskContext.Preview != "" || snapshot.ActiveTaskContext.Detail != "next line\nmore detail" {
		t.Fatalf("clearing preview note should preserve detail in snapshot: %#v", snapshot.ActiveTaskContext)
	}

	response, cmd = model.handleIPC(ipc.Request{Command: "task_context_clear", Args: map[string]string{"kind": "detail"}})
	if !response.OK || cmd != nil {
		t.Fatalf("clear detail response/cmd = %#v/%v", response, cmd)
	}
	if snapshot := model.Snapshot(); snapshot.ActiveTaskContext != nil {
		t.Fatalf("cleared notes should disappear from snapshot: %#v", snapshot.ActiveTaskContext)
	}
}

func TestIPCTaskContextRejectsShellTasksAndDisabledConfig(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)
	model.state.Tasks[0].TypeID = config.DefaultTaskTypeShell

	response, _ := model.handleIPC(ipc.Request{Command: "task_context_set", Args: map[string]string{"content": "note"}})
	if response.OK || response.Error == nil || response.Error.Code != "task_context_not_supported" {
		t.Fatalf("shell task notes response = %#v", response)
	}

	model.state.Tasks[0].TypeID = config.DefaultTaskTypeCodex
	model.cfg.TaskContext.Enabled = false
	response, _ = model.handleIPC(ipc.Request{Command: "task_context_set", Args: map[string]string{"content": "note"}})
	if response.OK || response.Error == nil || response.Error.Code != "task_context_disabled" {
		t.Fatalf("disabled task notes response = %#v", response)
	}
}

func TestTaskContextClearsWhenTaskCloses(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	response, _ := model.handleIPC(ipc.Request{Command: "task_context_set", Args: map[string]string{"content": "handoff"}})
	if !response.OK {
		t.Fatalf("set response = %#v", response)
	}
	response, _ = model.handleIPC(ipc.Request{Command: "close", Args: map[string]string{"id": "a"}})
	if !response.OK {
		t.Fatalf("close response = %#v", response)
	}
	if _, ok, err := model.taskContextStore.Show("a"); err != nil || ok {
		t.Fatalf("closed task notes should be removed, ok=%t err=%v", ok, err)
	}
}

func TestTaskEnvForCodexTaskOnly(t *testing.T) {
	model := testModelWithTask(t)
	defer killPTYs(model)

	env := model.taskEnvForTask("a")
	if env[config.AppDirEnv] != model.runtime.Dir || env["WEFT_TASK_ID"] != "a" || env["WEFT_TASK_TYPE_ID"] != config.DefaultTaskTypeCodex || env["WEFT_TASK_KIND"] != config.TaskKindCodex {
		t.Fatalf("codex task env = %#v", env)
	}

	model.state.Tasks[0].TypeID = config.DefaultTaskTypeShell
	if env := model.taskEnvForTask("a"); len(env) != 0 {
		t.Fatalf("shell task should not receive Weft task env: %#v", env)
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
	if _, ok := snapshot.TaskOperationDurations["a"]; !ok {
		t.Fatalf("ready terminal command should expose completed operation duration: %#v", snapshot.TaskOperationDurations)
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

	cmd := model.newTaskWithSilent("Grouped", false)
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

	cmd := model.newTaskWithSilent("Top-level", false)
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
