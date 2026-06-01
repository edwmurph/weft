package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestEmptyDashboardStartsInAgentsFocus(t *testing.T) {
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

func TestLoadingTickContinuesOnlyWhenLivePreviewHasTask(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	model := NewModel(rt, cfg, state.Empty())

	updated, cmd := model.Update(loadingTick{})
	model = updated.(Model)

	if model.loading != 0 {
		t.Fatalf("empty preview loading frame index = %d, want 0", model.loading)
	}
	if cmd != nil {
		t.Fatal("empty live preview should not keep the loading ticker active")
	}

	activeState := testStateWithAgent(rt.Workspace)
	activeState.NavOpen = true
	model = NewModel(rt, cfg, activeState)
	updated, cmd = model.Update(loadingTick{})
	model = updated.(Model)

	if model.loading != 1 {
		t.Fatalf("loading frame index = %d, want 1", model.loading)
	}
	if cmd == nil {
		t.Fatal("live preview animation should keep the loading ticker active")
	}
}

func TestNewTaskKeyOpensTypeMenuAndCreatesDefaultTask(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	cfg.DefaultTaskType = config.DefaultTaskTypeShell
	model := NewModel(rt, cfg, testStateWithWorkspace(t, rt.Workspace))
	defer killPTYs(model)

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model = updated.(Model)

	if cmd != nil {
		t.Fatalf("new task menu should not start command immediately, got %#v", cmd)
	}
	if model.mode != modeNewTask {
		t.Fatalf("mode = %s, want new task menu", model.mode)
	}
	got := ansi.Strip(model.View())
	for _, expected := range []string{"New task", "Codex", "Shell", "Enter create", "Up/Down move", "Esc cancel"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("new task menu missing %q:\n%s", expected, got)
		}
	}
	for _, unexpected := range []string{"[codex] Codex", "[shell] Shell"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("new task menu should not render %q:\n%s", unexpected, got)
		}
	}

	updated, cmd = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	defer killPTYs(model)
	if cmd == nil {
		t.Fatal("expected PTY start command")
	}
	if len(model.state.Agents) != 1 {
		t.Fatalf("agents = %#v", model.state.Agents)
	}
	if model.state.Agents[0].TypeID != config.DefaultTaskTypeShell {
		t.Fatalf("new task type = %q", model.state.Agents[0].TypeID)
	}
	if model.state.Agents[0].Title != "Shell" {
		t.Fatalf("new task title = %q", model.state.Agents[0].Title)
	}
	if model.state.Agents[0].GroupID != "" {
		t.Fatalf("new task should be top-level: %#v", model.state.Agents[0])
	}
	if model.state.Focus != state.FocusCodex || model.state.NavOpen {
		t.Fatalf("focus/nav = %s/%t", model.state.Focus, model.state.NavOpen)
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

func TestNewAgentRequiresWorkspace(t *testing.T) {
	model := NewModel(testRuntime(t), config.DefaultConfig(), state.Empty())

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model = updated.(Model)

	if cmd != nil || len(model.state.Agents) != 0 {
		t.Fatalf("new agent should be blocked without workspace, cmd=%v agents=%#v", cmd, model.state.Agents)
	}
	if model.message != "add a workspace first" {
		t.Fatalf("message = %q", model.message)
	}

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{}})
	if response.OK || response.Message != "add a workspace first" || cmd != nil {
		t.Fatalf("ipc new should be blocked without workspace, response=%#v cmd=%v", response, cmd)
	}
}

func TestApplyPTYDataUsesAgentID(t *testing.T) {
	model := testModelWithAgent(t)

	model.applyPTYData(ptyx.Data{AgentID: "a", Text: "hello\n", Title: "Fake Codex Ready"})

	if screen := model.screens["a"]; screen == nil || !screen.HasVisibleContent() {
		t.Fatalf("agent screen was not updated: %#v", model.screens)
	}
	if !model.visible["a"] {
		t.Fatalf("agent should be marked visible: %#v", model.visible)
	}
	if got := model.state.Agents[0].CodexTitle; got != "Fake Codex Ready" {
		t.Fatalf("CodexTitle = %q", got)
	}
}

func TestApplyPTYDataMarksRequestUserInputScreenReady(t *testing.T) {
	model := testModelWithAgent(t)

	model.applyPTYData(ptyx.Data{AgentID: "a", Title: "Fake Codex Running"})
	model.applyPTYData(ptyx.Data{AgentID: "a", Text: "\033[2J\033[HQuestion 1\nPick a path\n1 unanswered question\nEnter to submit answer\n"})

	agent := state.AgentByID(model.state, "a")
	if agent == nil {
		t.Fatal("agent missing")
	}
	if agent.CodexStatus != "Ready" || agent.Status != state.StatusReady {
		t.Fatalf("agent status = %s/%q, want ready/Ready", agent.Status, agent.CodexStatus)
	}
	if got := model.renderAgentTitle(*agent); got != "alpha" {
		t.Fatalf("configured title should remain unchanged, got %q", got)
	}

	agent.Title = "{status}"
	if got := renderAgentTitleForState(model.cfg, model.state, *agent); got != "Ready" {
		t.Fatalf("status title = %q, want Ready", got)
	}

	model.applyPTYData(ptyx.Data{AgentID: "a", Text: "\033[2J\033[Hworking again\n"})
	agent = state.AgentByID(model.state, "a")
	if agent == nil || agent.CodexStatus != "" || agent.Status != state.StatusRunning {
		t.Fatalf("agent status after clearing prompt = %#v", agent)
	}
}

func TestSnapshotMarksActiveAgentsLoadingUntilReady(t *testing.T) {
	st := testStateWithAgent(t.TempDir())
	model := Model{
		cfg:     config.DefaultConfig(),
		state:   st,
		screens: map[string]*TerminalScreen{"a": NewTerminalScreen(80, 24)},
		visible: map[string]bool{},
	}

	snapshot := model.Snapshot()
	if len(snapshot.LoadingAgentIDs) != 1 || snapshot.LoadingAgentIDs[0] != "a" {
		t.Fatalf("loading agent ids = %#v", snapshot.LoadingAgentIDs)
	}

	model.state.Agents[0].CodexTitle = "Fake Codex Ready"
	snapshot = model.Snapshot()
	if len(snapshot.LoadingAgentIDs) != 1 || snapshot.LoadingAgentIDs[0] != "a" {
		t.Fatalf("ready agent should keep loading until visible content: %#v", snapshot.LoadingAgentIDs)
	}

	model.screens["a"].Write("ready\n")
	snapshot = model.Snapshot()
	if len(snapshot.LoadingAgentIDs) != 0 {
		t.Fatalf("ready agent should not be marked loading: %#v", snapshot.LoadingAgentIDs)
	}

	model.state.Agents[0].CodexTitle = "Fake Codex Waiting"
	snapshot = model.Snapshot()
	if len(snapshot.LoadingAgentIDs) != 1 || snapshot.LoadingAgentIDs[0] != "a" {
		t.Fatalf("waiting agent should be marked loading: %#v", snapshot.LoadingAgentIDs)
	}

	model.state.Agents[0].CodexTitle = "Fake Codex Working"
	snapshot = model.Snapshot()
	if len(snapshot.LoadingAgentIDs) != 1 || snapshot.LoadingAgentIDs[0] != "a" {
		t.Fatalf("working agent should be marked loading: %#v", snapshot.LoadingAgentIDs)
	}
}

func TestLoadingIndicatorCoversNonIdleTaskStates(t *testing.T) {
	for _, tt := range []struct {
		name  string
		agent state.Agent
		want  bool
	}{
		{
			name:  "codex waiting title",
			agent: state.Agent{ID: "a", Title: "Codex", Status: state.StatusRunning, CodexTitle: "Fake Codex Waiting"},
			want:  true,
		},
		{
			name:  "terminal waiting status",
			agent: state.Agent{ID: "a", TypeID: config.DefaultTaskTypeShell, Title: "Shell", Status: state.AgentStatus("waiting")},
			want:  true,
		},
		{
			name:  "ready",
			agent: state.Agent{ID: "a", Title: "Codex", Status: state.StatusRunning, CodexTitle: "Fake Codex Ready"},
			want:  false,
		},
		{
			name:  "idle",
			agent: state.Agent{ID: "a", Title: "Codex", Status: state.AgentStatus("idle")},
			want:  false,
		},
		{
			name:  "killed",
			agent: state.Agent{ID: "a", Title: "Codex", Status: state.StatusKilled},
			want:  false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentStatusShowsLoadingIndicator(tt.agent); got != tt.want {
				t.Fatalf("loading indicator = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestIPCLaunchWorkspaceSelectsExistingWorkspace(t *testing.T) {
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
	if model.state.SelectedWorkspaceID != "launch" {
		t.Fatalf("selected workspace = %q, want launch", model.state.SelectedWorkspaceID)
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
	for _, unexpected := range []string{"Y yes", "N no", "Y/Enter yes"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("launch workspace prompt should not include %q:\n%s", unexpected, got)
		}
	}
	if strings.Contains(got, "New agents will start from this directory.") {
		t.Fatalf("launch workspace prompt should not include agent-start explanation:\n%s", got)
	}
}

func TestLaunchWorkspaceConfirmationEnterAddsWorkspace(t *testing.T) {
	rt := testRuntime(t)
	model := NewModel(rt, config.DefaultConfig(), state.Empty())
	model.mode = modeConfirm
	model.confirm = confirmAddLaunchWorkspace
	model.pendingID = rt.Workspace

	updated, cmd := model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)

	if cmd != nil {
		t.Fatalf("launch workspace confirm should not start command, got %#v", cmd)
	}
	if model.mode != modeNormal {
		t.Fatalf("mode = %s, want normal", model.mode)
	}
	if workspace := state.WorkspaceByPath(model.state, rt.Workspace); workspace == nil {
		t.Fatalf("workspace was not added: %#v", model.state.Workspaces)
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

func TestLaunchWorkspaceConfirmationIgnoresYAndN(t *testing.T) {
	rt := testRuntime(t)
	model := NewModel(rt, config.DefaultConfig(), state.Empty())
	model.mode = modeConfirm
	model.confirm = confirmAddLaunchWorkspace
	model.pendingID = rt.Workspace

	updated, cmd := model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	model = updated.(Model)
	if cmd != nil || model.mode != modeConfirm {
		t.Fatalf("y should be ignored for launch workspace confirm, mode=%s cmd=%#v", model.mode, cmd)
	}

	updated, cmd = model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model = updated.(Model)
	if cmd != nil || model.mode != modeConfirm {
		t.Fatalf("n should be ignored for launch workspace confirm, mode=%s cmd=%#v", model.mode, cmd)
	}

	updated, cmd = model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if cmd != nil || model.mode != modeNormal {
		t.Fatalf("esc should cancel launch workspace confirm, mode=%s cmd=%#v", model.mode, cmd)
	}
	if workspace := state.WorkspaceByPath(model.state, rt.Workspace); workspace != nil {
		t.Fatalf("workspace should not be added after ignored keys and esc: %#v", model.state.Workspaces)
	}
}

func TestDeleteAgentConfirmationEnterSubmitsYIgnoredAndNCancels(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.mode = modeConfirm
	model.confirm = confirmDeleteAgent
	model.pendingID = "a"

	updated, cmd := model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	model = updated.(Model)
	if cmd != nil || model.mode != modeConfirm || len(model.state.Agents) != 1 {
		t.Fatalf("y should be ignored for delete confirm, mode=%s cmd=%#v agents=%d", model.mode, cmd, len(model.state.Agents))
	}

	updated, cmd = model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model = updated.(Model)
	if cmd != nil || model.mode != modeNormal || len(model.state.Agents) != 1 {
		t.Fatalf("n should cancel delete confirm, mode=%s cmd=%#v agents=%d", model.mode, cmd, len(model.state.Agents))
	}

	model.mode = modeConfirm
	model.confirm = confirmDeleteAgent
	model.pendingID = "a"
	updated, cmd = model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeNormal {
		t.Fatalf("enter should submit delete confirm, mode=%s cmd=%#v", model.mode, cmd)
	}
	if len(model.state.Agents) != 0 {
		t.Fatalf("agent should be removed after enter: %#v", model.state.Agents)
	}
}

func TestDeleteAgentConfirmationExplainsStopAndDelete(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	model = updated.(Model)

	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmDeleteAgent || model.pendingID != "a" {
		t.Fatalf("delete confirm state mode=%s confirm=%s pending=%s cmd=%v", model.mode, model.confirm, model.pendingID, cmd)
	}
	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Delete task",
		"Task",
		"alpha",
		"Stops the terminal, then removes this task from Weft.",
		"Enter stop and delete",
		"N Esc",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("delete agent confirm missing %q:\n%s", expected, got)
		}
	}
	for _, unexpected := range []string{"Y stop and delete", "N cancel", "Esc cancel"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("delete agent confirm should not show %q:\n%s", unexpected, got)
		}
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
	if !confirmKeySubmits(confirmDeleteAgent, tea.KeyMsg{Type: tea.KeyEnter}) {
		t.Fatal("delete agent should submit with enter")
	}
	if confirmKeySubmits(confirmDeleteAgent, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}) {
		t.Fatal("delete agent should not submit with y")
	}
	if !confirmKeyCancels(confirmDeleteAgent, tea.KeyMsg{Type: tea.KeyEsc}) {
		t.Fatal("delete agent should cancel with esc")
	}
	if !confirmKeyCancels(confirmDeleteAgent, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}) {
		t.Fatal("delete agent should cancel with n")
	}
}

func TestDefaultDeleteShortcutNoLongerUsesD(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	model = updated.(Model)

	if cmd != nil || model.mode == modeConfirm || model.confirm == confirmDeleteAgent {
		t.Fatalf("d should not start delete confirmation mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
}

func TestBackspaceDeleteShortcutAcceptsCtrlHSequence(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlH})
	model = updated.(Model)

	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmDeleteAgent || model.pendingID != "a" {
		t.Fatalf("ctrl+h backspace confirm state mode=%s confirm=%s pending=%s cmd=%v", model.mode, model.confirm, model.pendingID, cmd)
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

func testClientUpgrade(supervisorVersion string, runningAgents int) *ipc.Upgrade {
	return &ipc.Upgrade{
		ClientVersion:     weftversion.Version,
		SupervisorVersion: supervisorVersion,
		Compatible:        true,
		RestartRequired:   true,
		RunningAgents:     runningAgents,
		Message:           fmt.Sprintf("Upgrade pending: client %s is newer than supervisor %s.", weftversion.Version, supervisorVersion),
	}
}

func TestClientUpgradeBannerOpensUpgradeResumeConfirm(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithAgent(rt.Workspace)
	st.Agents[0].CodexTitle = "Fake Codex Ready"
	st.Agents[0].CodexSessionID = "session-alpha"
	st.Focus = state.FocusAgents
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

	if model.upgrade == nil || model.upgrade.RunningAgents != 1 {
		t.Fatalf("upgrade = %#v", model.upgrade)
	}
	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Upgrade: ready",
		"can restart",
		"1 idle Codex task",
		"Press U",
		"supervisor 3.9.0 → " + weftversion.Version,
		"Press U to upgrade and resume 1 idle Codex task",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("upgrade banner missing %q:\n%s", expected, got)
		}
	}

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmUpgradeResume {
		t.Fatalf("upgrade confirm state mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
	got = ansi.Strip(model.View())
	for _, expected := range []string{
		"Upgrade supervisor and resume Codex tasks?",
		"supervisor 3.9.0 → " + weftversion.Version,
		"Enter upgrade and resume",
		"tasks. Running commands",
		"unsubmitted text are not preserved, so",
		"finish important work first",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("upgrade confirm missing %q:\n%s", expected, got)
		}
	}
	for _, unexpected := range []string{"Y upgrade and resume", "N cancel", "Esc cancel"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("upgrade confirm should not show %q:\n%s", unexpected, got)
		}
	}
}

func TestClientUpgradeWaitsUntilAgentIsIdleAndResumable(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithAgent(rt.Workspace)
	st.Agents[0].CodexTitle = "Fake Codex Working"
	st.Focus = state.FocusAgents
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
		t.Fatalf("upgrade action should not show while agent is working:\n%s", got)
	}
	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode == modeConfirm {
		t.Fatalf("blocked upgrade should not open confirm, mode=%s cmd=%v", model.mode, cmd)
	}
}

func TestSnapshotShowsActiveAgentStartError(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	st := testStateWithAgent(rt.Workspace)
	st.Agents[0].Status = state.StatusError
	st.Agents[0].CodexTitle = "fork/exec /missing/zsh: no such file or directory"

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

func TestCodexFocusOnlyHandlesGlobalShortcuts(t *testing.T) {
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("s")},
		{Type: tea.KeyRunes, Runes: []rune("?")},
		{Type: tea.KeyRunes, Runes: []rune("n")},
		{Type: tea.KeyShiftRight},
		{Type: tea.KeyShiftTab},
		{Type: tea.KeyCtrlD},
		{Type: tea.KeyCtrlQ},
	} {
		model := testModelWithAgent(t)
		defer killPTYs(model)

		updated, cmd := model.handleKey(msg)
		model = updated.(Model)

		if cmd != nil {
			t.Fatalf("%s should not start dashboard command in codex focus", msg.String())
		}
		if model.mode != modeNormal {
			t.Fatalf("%s changed mode to %s", msg.String(), model.mode)
		}
		if model.state.Focus != state.FocusCodex {
			t.Fatalf("%s changed focus to %s", msg.String(), model.state.Focus)
		}
		if len(model.state.Agents) != 1 {
			t.Fatalf("%s changed agents: %#v", msg.String(), model.state.Agents)
		}
	}

	model := testModelWithAgent(t)
	defer killPTYs(model)
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	model = updated.(Model)
	if model.state.Focus != state.FocusAgents || !model.state.NavOpen {
		t.Fatalf("C-b should open dashboard, got %s/%t", model.state.Focus, model.state.NavOpen)
	}

	model.state.Focus = state.FocusCodex
	model.state.NavOpen = false
	model.state.Agents[0].CodexTitle = "Fake Codex Working"
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(Model)
	if model.message != "" {
		t.Fatalf("C-c should forward while Codex has focus, message=%q", model.message)
	}

	model.state.Agents[0].CodexTitle = "Fake Codex Ready"
	model.screens[model.state.Agents[0].ID] = NewTerminalScreen(model.ptyWidth(), model.ptyHeight())
	model.screens[model.state.Agents[0].ID].Write("ready\n")
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(Model)
	if model.message != "" || model.state.Focus != state.FocusCodex {
		t.Fatalf("C-c should still forward while Codex is ready, message=%q focus=%s", model.message, model.state.Focus)
	}
}

func TestActivePTYExitReturnsToAgentsPane(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.width = 120
	model.navWidth = 0
	model.state.Focus = state.FocusCodex
	model.state.NavOpen = false
	model.state.Agents[0].CodexTitle = "Fake Codex Ready"

	model.applyPTYData(ptyx.Data{AgentID: "a", Err: os.ErrClosed})

	agent := state.AgentByID(model.state, "a")
	if agent == nil || agent.Status != state.StatusStopped || agent.CodexTitle != "Codex exited" {
		t.Fatalf("agent after PTY exit = %#v", agent)
	}
	if model.state.Focus != state.FocusAgents || !model.state.NavOpen {
		t.Fatalf("PTY exit should recover to Tasks pane, focus/nav=%s/%t", model.state.Focus, model.state.NavOpen)
	}
	if model.ptys["a"] != nil {
		t.Fatal("dead PTY should be removed from live PTY map")
	}
	if model.navWidth == 0 {
		t.Fatal("dashboard nav should be visible after active PTY exit")
	}
}

func TestRecentCtrlCPTYExitMarksAgentKilled(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.width = 120
	model.navWidth = 0
	model.state.Focus = state.FocusCodex
	model.state.NavOpen = false
	model.recordAgentInterrupt("a")

	model.applyPTYData(ptyx.Data{AgentID: "a", Err: os.ErrClosed})

	agent := state.AgentByID(model.state, "a")
	if agent == nil || agent.Status != state.StatusKilled || agent.CodexTitle != "Codex killed" {
		t.Fatalf("agent after interrupted PTY exit = %#v", agent)
	}
	if model.state.Focus != state.FocusAgents || !model.state.NavOpen {
		t.Fatalf("interrupted PTY exit should recover to Tasks pane, focus/nav=%s/%t", model.state.Focus, model.state.NavOpen)
	}
}

func TestPTYWidthMatchesVisibleCodexContentWidth(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.width = 100
	model.navWidth = 0

	if got, want := model.ptyWidth(), 97; got != want {
		t.Fatalf("focused pty width = %d, want visible content width %d", got, want)
	}

	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.navWidth = 60
	if got, want := model.ptyWidth(), 36; got != want {
		t.Fatalf("split pty width = %d, want visible content width %d", got, want)
	}
}

func TestPTYHeightMatchesVisibleCodexContentHeight(t *testing.T) {
	model := testModelWithAgent(t)
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
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.width = 160
	model.height = 32
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.navWidth = model.targetNavWidth()
	agentID := model.state.ActiveAgentID
	model.screens[agentID] = NewTerminalScreen(model.ptyWidth(), model.ptyHeight())

	splitWidth := model.screens[agentID].cols
	if splitWidth >= 80 {
		t.Fatalf("test setup expected narrow split screen, got %d", splitWidth)
	}

	response, _ := model.handleIPC(ipc.Request{Command: "focus", Args: map[string]string{"target": string(state.FocusCodex)}})
	if !response.OK {
		t.Fatalf("focus response failed: %#v", response)
	}

	if got, want := model.navWidth, 0; got != want {
		t.Fatalf("codex focus nav width = %d, want %d", got, want)
	}
	if got, want := model.screens[agentID].cols, 157; got != want {
		t.Fatalf("focused screen width = %d, want visible console width %d", got, want)
	}
}

func TestTitleHookCapturesFirstSubmittedLine(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Generated title\\n'"

	cmd := model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix ")})
	if cmd != nil {
		t.Fatal("hook should not run before Enter")
	}
	model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("loginx")})
	model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyBackspace})
	cmd = model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected title hook command")
	}
	if agent := state.AgentByID(model.state, "a"); agent == nil || !agent.AutoTitleAttempted {
		t.Fatalf("agent should be marked attempted: %#v", agent)
	}

	msg := titleHookMessageFromCmd(t, cmd)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	model.applyTitleHook(msg)
	if got := state.AgentByID(model.state, "a").AutoTitle; got != "Generated title" {
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
	model := testModelWithAgent(t)
	defer killPTYs(model)

	model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix loginx")})
	model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	if got, want := string(model.codexInputBuffers[model.state.Agents[0].ID]), "fix "; got != want {
		t.Fatalf("direct capture buffer = %q, want %q", got, want)
	}

	model.codexInputBuffers[model.state.Agents[0].ID] = []rune("fix loginx")
	model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyCtrlH, Alt: true})
	if got, want := string(model.codexInputBuffers[model.state.Agents[0].ID]), "fix "; got != want {
		t.Fatalf("direct alt ctrl-h capture buffer = %q, want %q", got, want)
	}

	model.codexInputBuffers[model.state.Agents[0].ID] = []rune("fix loginx")
	model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": "alt+backspace"})
	if got, want := string(model.codexInputBuffers[model.state.Agents[0].ID]), "fix "; got != want {
		t.Fatalf("forwarded capture buffer = %q, want %q", got, want)
	}
}

func TestTitleHookCapturesSupervisorForwardedInput(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	model.cfg.TitleTemplate = "{status} {auto}"
	model.state.Agents[0].Title = model.cfg.TitleTemplate
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Generated title\\n'"

	model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": "text", "text": "fix"})
	model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": "space"})
	model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": "text", "text": "login"})
	cmd := model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": "enter"})
	if cmd == nil {
		t.Fatal("expected title hook command")
	}
	msg := titleHookMessageFromCmd(t, cmd)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	model.applyTitleHook(msg)

	agent := state.AgentByID(model.state, "a")
	if agent == nil || agent.AutoTitle != "Generated title" {
		t.Fatalf("auto title = %#v", agent)
	}
	if got := model.renderAgentTitle(*agent); got != "running Generated title" {
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
	model := testModelWithAgent(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	model.cfg.TitleTemplate = "{status} {auto}"
	model.state.Agents[0].Title = model.cfg.TitleTemplate
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Generated title\\n'"

	sendRaw := func(raw string) tea.Cmd {
		t.Helper()
		return model.captureCodexInputArgs(model.state.Agents[0], map[string]string{
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
	if agent := state.AgentByID(model.state, "a"); agent == nil || !agent.AutoTitleAttempted {
		t.Fatalf("agent should be marked attempted: %#v", agent)
	}
	msg := cmd().(titleHookMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	model.applyTitleHook(msg)

	agent := state.AgentByID(model.state, "a")
	if agent == nil || agent.AutoTitle != "Generated title" {
		t.Fatalf("auto title = %#v", agent)
	}
	if got := model.renderAgentTitle(*agent); got != "running Generated title" {
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
	model := testModelWithAgent(t)
	defer killPTYs(model)

	model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": codexInputRaw, "encoded": "fix login"})
	model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": codexInputRaw, "encoded": "\x1b"})
	model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": codexInputRaw, "encoded": "\x1bb"})
	model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": codexInputRaw, "encoded": " now"})

	if got, want := string(model.codexInputBuffers[model.state.Agents[0].ID]), "fix login now"; got != want {
		t.Fatalf("raw capture buffer = %q, want %q", got, want)
	}
}

func TestTitleHookBuffersShiftEnterUntilSubmit(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Generated title\\n'"

	model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": "text", "text": "first"})
	cmd := model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": codexInputShiftEnter})
	if cmd != nil {
		t.Fatal("shift enter should not submit the first message")
	}
	model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": "text", "text": "second"})
	cmd = model.captureCodexInputArgs(model.state.Agents[0], map[string]string{"input": "enter"})
	if cmd == nil {
		t.Fatal("expected title hook command")
	}
	msg := cmd().(titleHookMsg)
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
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.cfg.TitleHookCommand = "false"

	model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first")})
	cmd := model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected first hook attempt")
	}
	if msg := cmd().(titleHookMsg); msg.err == nil {
		t.Fatal("expected hook error")
	}
	model.captureCodexInput(*state.AgentByID(model.state, "a"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second")})
	cmd = model.captureCodexInput(*state.AgentByID(model.state, "a"), tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("hook should not retry after first attempt")
	}
}

func TestRenameToAutoWithoutHookReportsConfigurationError(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.prompt = promptEditAgent
	model.pendingID = "a"

	cmd := model.applyPrompt("{auto}")

	if cmd != nil {
		t.Fatal("missing hook should not start hook command")
	}
	agent := state.AgentByID(model.state, "a")
	if agent == nil || agent.Title != "{auto}" {
		t.Fatalf("agent not renamed: %#v", agent)
	}
	if agent.AutoTitleError != "title_hook_command is not configured" {
		t.Fatalf("auto title error = %q", agent.AutoTitleError)
	}
	if !strings.Contains(model.message, "title_hook_command") {
		t.Fatalf("message = %q", model.message)
	}
}

func TestRenameToAutoUsesSavedAutoTitle(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Generated before rename\\n'"

	cmd := model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("summarize this bug")})
	if cmd != nil {
		t.Fatal("plain title should not run hook while capturing input")
	}
	cmd = model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("hook should run on first message even before {auto} is used")
	}
	msg := cmd().(titleHookMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	model.applyTitleHook(msg)

	model.prompt = promptEditAgent
	model.pendingID = "a"
	cmd = model.applyPrompt("{auto}")
	if cmd != nil {
		t.Fatal("rename to auto should only reveal saved auto title")
	}
	if got := state.AgentByID(model.state, "a").AutoTitle; got != "Generated before rename" {
		t.Fatalf("auto title = %q", got)
	}
	if got := model.renderAgentTitle(*state.AgentByID(model.state, "a")); got != "Generated before rename" {
		t.Fatalf("rendered title = %q", got)
	}
	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"first_message":"summarize this bug"`) {
		t.Fatalf("payload missing first submitted message:\n%s", raw)
	}
}

func TestTitleHookFailureIsReportedInFooterAndRenamePane(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.cfg.TitleHookCommand = "printf 'bad config' >&2; exit 2"

	model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first")})
	cmd := model.captureCodexInput(model.state.Agents[0], tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected title hook command")
	}
	msg := cmd().(titleHookMsg)
	if msg.err == nil {
		t.Fatal("expected hook error")
	}
	model.applyTitleHook(msg)

	agent := state.AgentByID(model.state, "a")
	if agent == nil || !strings.Contains(agent.AutoTitleError, "bad config") {
		t.Fatalf("agent auto title error = %#v", agent)
	}
	if !strings.Contains(model.message, "auto title hook failed") {
		t.Fatalf("message = %q", model.message)
	}

	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1
	model.prompt = promptEditAgent
	model.mode = modeInput
	model.pendingID = "a"
	model.input.SetValue("{auto}")

	got := ansi.Strip(model.View())
	if !strings.Contains(got, "Auto title error") || !strings.Contains(got, "bad config") {
		t.Fatalf("edit pane missing hook error:\n%s", got)
	}
}

func TestActiveOutputPreservesTerminalStyles(t *testing.T) {
	model := testModelWithAgent(t)
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
	model := testModelWithAgent(t)
	defer killPTYs(model)
	screen := NewTerminalScreen(20, 3)
	screen.Write("prompt")
	model.screens["a"] = screen

	output := model.activeOutput()
	if !strings.Contains(output, "48;2;255;255;255") {
		t.Fatalf("codex-focused output should paint terminal cursor:\n%q", output)
	}

	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	output = model.activeOutput()
	if strings.Contains(output, "48;2;255;255;255") {
		t.Fatalf("nav-focused output should not paint Codex cursor:\n%q", output)
	}
}

func TestEditAgentPromptPreviewsEditedTitle(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.cfg.TitleTemplate = "{auto}"
	model.state.Agents[0].CodexTitle = "Fake Codex Ready"
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1
	model.prompt = promptEditAgent
	model.mode = modeInput
	model.pendingID = "a"
	model.input.SetValue("{codex}")

	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Edit task",
		"Preview",
		"Fake Codex Ready",
		"Variables",
		"{auto}: generated title",
		"Enter save",
		"Esc cancel",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("edit modal missing %q:\n%s", expected, got)
		}
	}
}

func TestEditAgentPromptPrefillsStoredAgentTitleTemplate(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Agents[0].Title = "{status} {auto}"
	model.state.Agents[0].AutoTitle = "Fix login"
	model.state.Agents[0].CodexTitle = "Fake Codex Ready"
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	model = updated.(Model)

	if cmd != nil {
		t.Fatalf("rename prompt should not start command, got %#v", cmd)
	}
	if model.mode != modeInput || model.prompt != promptEditAgent || model.pendingID != "a" {
		t.Fatalf("prompt state = mode:%s prompt:%s pending:%s", model.mode, model.prompt, model.pendingID)
	}
	if got, want := model.input.Value(), "{status} {auto}"; got != want {
		t.Fatalf("edit prompt value = %q, want stored title template %q", got, want)
	}
	if got := model.renderAgentTitle(model.state.Agents[0]); got != "Ready Fix login" {
		t.Fatalf("agent row title = %q", got)
	}
}

func TestWorkspaceRenamePromptSetsAndClearsTitleOverride(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	model := NewModel(rt, cfg, testStateWithWorkspace(t, rt.Workspace))
	model.state.Focus = state.FocusWorkspaces
	model.state.NavOpen = true
	model.lastNavFocus = state.FocusWorkspaces

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	model = updated.(Model)
	if cmd != nil {
		t.Fatalf("rename prompt should not start command, got %#v", cmd)
	}
	if model.mode != modeInput || model.prompt != promptWorkspaceTitle || model.pendingID != model.state.SelectedWorkspaceID {
		t.Fatalf("prompt state = mode:%s prompt:%s pending:%s selected:%s", model.mode, model.prompt, model.pendingID, model.state.SelectedWorkspaceID)
	}

	model.input.SetValue("Trading Engine")
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeNormal {
		t.Fatalf("mode after save = %s", model.mode)
	}
	if got := model.state.Workspaces[0].Title; got != "Trading Engine" {
		t.Fatalf("title override = %q", got)
	}

	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	model = updated.(Model)
	model.input.SetValue("")
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if got := model.state.Workspaces[0].Title; got != "" {
		t.Fatalf("blank input should clear title override, got %q", got)
	}
	if model.message != "cleared workspace title" {
		t.Fatalf("message = %q", model.message)
	}
}

func TestDashboardEditShortcutIgnoresLegacyRenameKey(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	model = updated.(Model)
	if cmd != nil {
		t.Fatalf("r should not start command, got %#v", cmd)
	}
	if model.mode != modeNormal {
		t.Fatalf("mode = %s, want normal", model.mode)
	}
}

func TestEditGroupPromptTogglesSilentAndSaves(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "g",
		Focus:               state.FocusAgents,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now}},
		Groups:              []state.Group{{ID: "g", WorkspaceID: "w", Path: "release", Silent: false, CreatedAt: now, UpdatedAt: now}},
		Agents:              []state.Agent{},
	}
	model := NewModel(rt, cfg, st)
	defer killPTYs(model)
	model.groupCursor = 0

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	model = updated.(Model)
	if model.mode != modeInput || model.prompt != promptEditGroup || model.pendingID != "g" {
		t.Fatalf("prompt state = mode:%s prompt:%s pending:%s", model.mode, model.prompt, model.pendingID)
	}

	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	model = updated.(Model)
	if !model.editGroupSilent {
		t.Fatalf("expected silent toggle, got %#v", model.editGroupSilent)
	}

	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeNormal {
		t.Fatalf("mode after save = %s", model.mode)
	}
	group := state.GroupByID(model.state, "g")
	if group == nil || !group.Silent {
		t.Fatalf("silent not persisted: %#v", group)
	}
}

func TestCreateGroupPromptCanCreateSilentGroup(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		Focus:               state.FocusAgents,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now}},
		Groups:              []state.Group{},
		Agents:              []state.Agent{},
	}
	model := NewModel(rt, cfg, st)
	defer killPTYs(model)

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	model = updated.(Model)
	if model.mode != modeInput || model.prompt != promptGroup {
		t.Fatalf("prompt state = mode:%s prompt:%s", model.mode, model.prompt)
	}

	model.input.SetValue("release")
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	model = updated.(Model)
	if !model.editGroupSilent {
		t.Fatalf("expected silent toggle, got %#v", model.editGroupSilent)
	}
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)

	if model.mode != modeNormal {
		t.Fatalf("mode after save = %s", model.mode)
	}
	group := state.GroupByID(model.state, model.state.SelectedGroupID)
	if group == nil || group.Path != "release" || !group.Silent {
		t.Fatalf("group not created silent: %#v", group)
	}
}

func TestNewWorkspacePromptPrefillsSelectedParentAndShowsPathStatus(t *testing.T) {
	parent := t.TempDir()
	current := filepath.Join(parent, "current")
	if err := os.Mkdir(current, 0o700); err != nil {
		t.Fatal(err)
	}
	rt := testRuntime(t)
	rt.Workspace = current
	cfg := config.DefaultConfig()
	model := NewModel(rt, cfg, state.Empty())
	model.state.Focus = state.FocusWorkspaces
	model.state.NavOpen = true

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	model = updated.(Model)
	if cmd != nil {
		t.Fatalf("workspace prompt should not start command, got %#v", cmd)
	}
	if model.mode != modeInput || model.prompt != promptWorkspace {
		t.Fatalf("prompt state = mode:%s prompt:%s", model.mode, model.prompt)
	}
	if want := withTrailingSeparator(displayPathForPrompt(parent)); model.input.Value() != want {
		t.Fatalf("prompt value = %q, want %q", model.input.Value(), want)
	}

	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Add workspace",
		"Path",
		"✓ ",
		"> current",
		"Enter choose",
		"Tab choose",
		"Up/Down move",
		"Esc close suggestions",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("workspace modal missing %q:\n%s", expected, got)
		}
	}
	if status := inspectWorkspacePromptPath(model.state, model.input.Value()).message; status != "✓ "+parent {
		t.Fatalf("path status = %q", status)
	}
	if strings.Count(got, "╭") < 2 || strings.Count(got, "╰") < 2 {
		t.Fatalf("workspace modal should render a bordered input box:\n%s", got)
	}
	if got := model.input.MatchedSuggestions(); len(got) != 1 || got[0] != withTrailingSeparator(current) {
		t.Fatalf("matched suggestions = %#v", got)
	}
}

func TestTextEntryPromptsUseSharedFormChromeAndStatefulActions(t *testing.T) {
	rt := testRuntime(t)
	model := NewModel(rt, config.DefaultConfig(), testStateWithWorkspace(t, rt.Workspace))

	model.startPrompt(promptGroup, "")
	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Create group",
		"Group",
		"Group required",
		"Esc cancel",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("group prompt missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "Flat and unique in this workspace.") {
		t.Fatalf("group prompt should not show hint copy:\n%s", got)
	}
	if strings.Contains(got, "Enter create") {
		t.Fatalf("empty required prompt should not advertise submit:\n%s", got)
	}
	if strings.Count(got, "╭") < 2 || strings.Count(got, "╰") < 2 {
		t.Fatalf("group prompt should render a bordered input box:\n%s", got)
	}

	model.input.SetValue("release")
	got = ansi.Strip(model.View())
	for _, expected := range []string{"Enter create", "Esc cancel"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("valid group prompt missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "Ready") {
		t.Fatalf("valid group prompt should not show Ready status:\n%s", got)
	}

	model.startPrompt(promptWorkspaceTitle, "")
	got = ansi.Strip(model.View())
	for _, expected := range []string{"Rename workspace", "Blank uses path title", "Enter clear", "Esc cancel"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("blank title prompt missing %q:\n%s", expected, got)
		}
	}
	model.input.SetValue("Trading Engine")
	got = ansi.Strip(model.View())
	if !strings.Contains(got, "Enter save") {
		t.Fatalf("non-empty title prompt should advertise save:\n%s", got)
	}
}

func TestWorkspacePromptSuggestionMenuSupportsArrowSelection(t *testing.T) {
	parent := t.TempDir()
	alpha := filepath.Join(parent, "alpha-project")
	beta := filepath.Join(parent, "beta-project")
	if err := os.Mkdir(alpha, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(beta, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(beta, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	model := NewModel(testRuntime(t), config.DefaultConfig(), state.Empty())
	model.startPrompt(promptWorkspace, withTrailingSeparator(parent))

	got := ansi.Strip(model.View())
	if got, want := promptCurrentSuggestion(model.promptContext(), model.input.Value(), model.promptSuggestionIndex), withTrailingSeparator(alpha); got != want {
		t.Fatalf("initial suggestion = %q, want %q", got, want)
	}
	for _, expected := range []string{"> alpha-project", "beta-project", "Enter choose", "Tab choose", "Up/Down move", "Esc close suggestions"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("initial suggestion state missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "alpha-project/") || strings.Contains(got, "beta-project/") {
		t.Fatalf("suggestion labels should not render trailing slashes:\n%s", got)
	}

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got, want := promptCurrentSuggestion(model.promptContext(), model.input.Value(), model.promptSuggestionIndex), withTrailingSeparator(beta); got != want {
		t.Fatalf("down arrow suggestion = %q, want %q", got, want)
	}
	if got := ansi.Strip(model.View()); !strings.Contains(got, "> beta-project") {
		t.Fatalf("down arrow should highlight beta:\n%s", got)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeInput {
		t.Fatalf("enter on highlighted suggestion should keep prompt open, mode=%s", model.mode)
	}
	if want := beta; model.input.Value() != want {
		t.Fatalf("selected suggestion = %q, want %q", model.input.Value(), want)
	}
	got = ansi.Strip(model.View())
	if strings.Contains(got, "> nested") || !strings.Contains(got, "Enter add") {
		t.Fatalf("selection should close nested suggestions and show submit action:\n%s", got)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeNormal {
		t.Fatalf("second enter should add workspace, mode=%s", model.mode)
	}
	if model.state.SelectedWorkspaceID == "" || model.state.Workspaces[len(model.state.Workspaces)-1].Path != beta {
		t.Fatalf("workspace was not added/selected: %#v", model.state.Workspaces)
	}
}

func TestMoveAgentPromptAutocompletesKnownGroupsAndKeepsInvalidInputOpen(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.state.Agents[0].GroupID = ""
	model.groupCursor = 0
	now := state.NowISO()
	model.state.Groups = append(model.state.Groups, state.Group{ID: "release", WorkspaceID: "w", Path: "release", CreatedAt: now, UpdatedAt: now})

	model.startPrompt(promptMoveAgent, "")
	got := ansi.Strip(model.View())
	highlighted := promptCurrentSuggestion(model.promptContext(), model.input.Value(), model.promptSuggestionIndex)
	for _, expected := range []string{"Move task", "Top-level task", "> " + highlighted, "Enter choose", "Tab choose", "Esc close suggestions"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("blank move prompt missing %q:\n%s", expected, got)
		}
	}

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeInput || model.promptSuggestionOpen {
		t.Fatalf("choosing from blank move prompt should keep prompt open with menu closed: mode=%s open=%t", model.mode, model.promptSuggestionOpen)
	}
	if got := model.input.Value(); got != highlighted {
		t.Fatalf("blank move prompt enter chose %q, want %q", got, highlighted)
	}
	if agent := state.AgentByID(model.state, "a"); agent == nil || agent.GroupID != "" {
		t.Fatalf("choosing a suggestion should not submit the move: %#v", agent)
	}
	got = ansi.Strip(model.View())
	if !strings.Contains(got, "Enter move") || strings.Contains(got, "Esc close suggestions") {
		t.Fatalf("chosen blank suggestion should close suggestions and advertise submit:\n%s", got)
	}

	model.startPrompt(promptMoveAgent, "rel")
	if got, want := model.input.MatchedSuggestions(), []string{"release"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("matched groups = %#v, want %#v", got, want)
	}
	got = ansi.Strip(model.View())
	for _, expected := range []string{"> release", "Enter choose", "Tab choose", "Esc close suggestions"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("group suggestion menu missing %q:\n%s", expected, got)
		}
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeInput || model.promptSuggestionOpen {
		t.Fatalf("choosing a group should keep prompt open with menu closed: mode=%s open=%t", model.mode, model.promptSuggestionOpen)
	}
	if got := model.input.Value(); got != "release" {
		t.Fatalf("chosen group = %q", got)
	}
	got = ansi.Strip(model.View())
	if strings.Count(got, "> release") > 1 || !strings.Contains(got, "Enter move") {
		t.Fatalf("chosen group should close suggestions and advertise submit:\n%s", got)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeNormal {
		t.Fatalf("valid move should close prompt, mode=%s", model.mode)
	}
	if agent := state.AgentByID(model.state, "a"); agent == nil || agent.GroupID != "release" {
		t.Fatalf("agent was not moved to release: %#v", agent)
	}

	model.groupCursor = 2
	model.startPrompt(promptMoveAgent, "missing")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeInput || model.message != "Group not found" {
		t.Fatalf("invalid move should stay open with message, mode=%s message=%q", model.mode, model.message)
	}
}

func TestMoveAgentPromptAutocompletesGroupSubstring(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1
	now := state.NowISO()
	model.state.Groups = append(model.state.Groups,
		state.Group{ID: "feature", WorkspaceID: "w", Path: "release-feature", CreatedAt: now, UpdatedAt: now},
		state.Group{ID: "rollout", WorkspaceID: "w", Path: "release-rollout", CreatedAt: now, UpdatedAt: now},
	)

	model.startPrompt(promptMoveAgent, "out")
	if got, want := promptMatchedSuggestions(model.promptContext(), model.input.Value()), []string{"release-rollout"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("substring matched groups = %#v, want %#v", got, want)
	}
	got := ansi.Strip(model.View())
	if !strings.Contains(got, "> release-rollout") || !strings.Contains(got, "Enter choose") || !strings.Contains(got, "Tab choose") {
		t.Fatalf("substring group suggestion menu missing match:\n%s", got)
	}

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeInput || model.promptSuggestionOpen {
		t.Fatalf("choosing substring group should keep prompt open with menu closed: mode=%s open=%t", model.mode, model.promptSuggestionOpen)
	}
	if got := model.input.Value(); got != "release-rollout" {
		t.Fatalf("chosen substring group = %q", got)
	}
	got = ansi.Strip(model.View())
	if !strings.Contains(got, "Enter move") || strings.Count(got, "> release-rollout") > 1 {
		t.Fatalf("chosen substring group should close suggestions and advertise submit:\n%s", got)
	}
}

func TestShiftUpDownReordersSelectedAgentInAgentsPane(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	model := NewModel(rt, config.DefaultConfig(), state.State{
		Version:             state.Version,
		ActiveAgentID:       "b",
		SelectedWorkspaceID: "w",
		Focus:               state.FocusAgents,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now}},
		Agents: []state.Agent{
			{ID: "a", WorkspaceID: "w", Title: "Alpha", Status: state.StatusReady, CreatedAt: "2026-01-01T00:01:00Z", UpdatedAt: now},
			{ID: "b", WorkspaceID: "w", Title: "Beta", Status: state.StatusReady, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: now},
		},
	})
	model.groupCursor = 1

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyShiftUp})
	model = updated.(Model)
	if got, want := tuiAgentIDs(state.UngroupedAgentsForWorkspace(model.state, "w")), []string{"b", "a"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("shift up order = %#v, want %#v", got, want)
	}
	if model.groupCursor != 0 || model.state.ActiveAgentID != "b" {
		t.Fatalf("selection should follow reordered agent: cursor=%d active=%q", model.groupCursor, model.state.ActiveAgentID)
	}

	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	model = updated.(Model)
	if got, want := tuiAgentIDs(state.UngroupedAgentsForWorkspace(model.state, "w")), []string{"a", "b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("shift down order = %#v, want %#v", got, want)
	}
	if model.groupCursor != 1 || model.state.ActiveAgentID != "b" {
		t.Fatalf("selection should follow reordered agent: cursor=%d active=%q", model.groupCursor, model.state.ActiveAgentID)
	}
}

func TestGroupCursorSyncDoesNotSnapGroupRowsBackToActiveAgent(t *testing.T) {
	rt := testRuntime(t)
	now := state.NowISO()
	model := NewModel(rt, config.DefaultConfig(), state.State{
		Version:             state.Version,
		ActiveAgentID:       "planning-agent",
		SelectedAgentID:     "planning-agent",
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "planning",
		Focus:               state.FocusAgents,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "in-progress", WorkspaceID: "w", Path: "in progress", CreatedAt: now, UpdatedAt: now},
			{ID: "shipit", WorkspaceID: "w", Path: "shipit", CreatedAt: now, UpdatedAt: now},
			{ID: "planning", WorkspaceID: "w", Path: "planning", CreatedAt: now, UpdatedAt: now},
		},
		Agents: []state.Agent{
			{ID: "progress-a", WorkspaceID: "w", GroupID: "in-progress", Title: "Progress A", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "progress-b", WorkspaceID: "w", GroupID: "in-progress", Title: "Progress B", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "planning-agent", WorkspaceID: "w", GroupID: "planning", Title: "Planning", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now},
		},
	})

	if row := model.currentGroupRow(); row.kind != groupRowAgent || row.agentID != "planning-agent" {
		t.Fatalf("initial cursor row = %#v, want planning agent", row)
	}

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if row := model.currentGroupRow(); row.kind != groupRowGroup || row.groupID != "planning" {
		t.Fatalf("up should move to planning group row, got %#v", row)
	}
	model.syncGroupCursor()
	if row := model.currentGroupRow(); row.kind != groupRowGroup || row.groupID != "planning" {
		t.Fatalf("sync should keep planning group row, got %#v", row)
	}

	model.syncGroupCursorToAgent("planning-agent")
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
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
		ActiveAgentID:       "planning-agent",
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "planning",
		Focus:               state.FocusAgents,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: rt.Workspace, CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "in-progress", WorkspaceID: "w", Path: "in progress", CreatedAt: now, UpdatedAt: now},
			{ID: "planning", WorkspaceID: "w", Path: "planning", CreatedAt: now, UpdatedAt: now},
		},
		Agents: []state.Agent{
			{ID: "progress-a", WorkspaceID: "w", GroupID: "in-progress", Title: "Progress A", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "planning-agent", WorkspaceID: "w", GroupID: "planning", Title: "Planning", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now},
		},
	})

	if row := model.currentGroupRow(); row.kind != groupRowGroup || row.groupID != "planning" {
		t.Fatalf("persisted group row should win after model start, got %#v", row)
	}
}

func TestWorkspacePromptSuggestionMenuScrollsWithSelection(t *testing.T) {
	parent := t.TempDir()
	for index := 0; index < 12; index++ {
		if err := os.Mkdir(filepath.Join(parent, fmt.Sprintf("project-%02d", index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	model := NewModel(testRuntime(t), config.DefaultConfig(), state.Empty())
	model.height = 32
	model.startPrompt(promptWorkspace, withTrailingSeparator(parent))

	var updated tea.Model
	for index := 0; index < 9; index++ {
		updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(Model)
	}

	if got, want := promptCurrentSuggestion(model.promptContext(), model.input.Value(), model.promptSuggestionIndex), withTrailingSeparator(filepath.Join(parent, "project-09")); got != want {
		t.Fatalf("current suggestion = %q, want %q", got, want)
	}
	got := ansi.Strip(model.View())
	if !strings.Contains(got, "> project-09") {
		t.Fatalf("selected suggestion should remain visible after scrolling:\n%s", got)
	}
	if strings.Contains(got, "project-00") {
		t.Fatalf("menu should scroll past the first row:\n%s", got)
	}
}

func TestWorkspacePromptTabChoosesVisibleDirectory(t *testing.T) {
	parent := t.TempDir()
	alpha := filepath.Join(parent, "alpha-project")
	if err := os.Mkdir(alpha, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(parent, "beta-project"), 0o700); err != nil {
		t.Fatal(err)
	}
	model := NewModel(testRuntime(t), config.DefaultConfig(), state.Empty())
	model.startPrompt(promptWorkspace, filepath.Join(parent, "alp"))

	if got, want := model.input.MatchedSuggestions(), []string{withTrailingSeparator(alpha)}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("matched suggestions = %#v, want %#v", got, want)
	}
	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if want := alpha; model.input.Value() != want {
		t.Fatalf("completed path = %q, want %q", model.input.Value(), want)
	}
	if model.promptSuggestionOpen {
		t.Fatal("tab should close suggestions after choosing")
	}
	if status := inspectWorkspacePromptPath(model.state, model.input.Value()).message; status != "✓ "+alpha {
		t.Fatalf("completed path status = %q", status)
	}
}

func TestWorkspacePromptAutocompletesDirectorySubstring(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "client-suffix")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(parent, "other-project"), 0o700); err != nil {
		t.Fatal(err)
	}
	model := NewModel(testRuntime(t), config.DefaultConfig(), state.Empty())
	model.startPrompt(promptWorkspace, filepath.Join(parent, "suffix"))

	if got, want := promptMatchedSuggestions(model.promptContext(), model.input.Value()), []string{withTrailingSeparator(target)}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("substring matched directories = %#v, want %#v", got, want)
	}
	if !model.promptSuggestionOpen {
		t.Fatal("substring suggestions should open on prompt init")
	}
	if got := ansi.Strip(model.View()); !strings.Contains(got, "> client-suffix") {
		t.Fatalf("substring suggestion should render target directory:\n%s", got)
	}

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.input.Value() != target {
		t.Fatalf("completed substring path = %q, want %q", model.input.Value(), target)
	}
	if model.promptSuggestionOpen {
		t.Fatal("tab should close substring suggestions after choosing")
	}
}

func TestPromptInputSupportsOptionWordEditing(t *testing.T) {
	model := NewModel(testRuntime(t), config.DefaultConfig(), state.Empty())
	model.startPrompt(promptWorkspaceTitle, "/alpha-beta/gamma_delta")

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	model = updated.(Model)
	if got, want := model.input.Position(), len("/alpha-beta/gamma_"); got != want {
		t.Fatalf("option-left cursor = %d, want %d", got, want)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	model = updated.(Model)
	if got, want := model.input.Position(), len("/alpha-beta/gamma_delta"); got != want {
		t.Fatalf("option-right cursor = %d, want %d", got, want)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
		t.Fatalf("option-backspace value = %q, want %q", got, want)
	}
}

func TestPromptInputSupportsTerminalOptionWordSequences(t *testing.T) {
	model := NewModel(testRuntime(t), config.DefaultConfig(), state.Empty())
	model.startPrompt(promptWorkspaceTitle, "/alpha-beta/gamma_delta")

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true})
	model = updated.(Model)
	if got, want := model.input.Position(), len("/alpha-beta/gamma_"); got != want {
		t.Fatalf("alt-b cursor = %d, want %d", got, want)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f"), Alt: true})
	model = updated.(Model)
	if got, want := model.input.Position(), len("/alpha-beta/gamma_delta"); got != want {
		t.Fatalf("alt-f cursor = %d, want %d", got, want)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyCtrlH, Alt: true})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
		t.Fatalf("alt-ctrl-h value = %q, want %q", got, want)
	}

	model.startPrompt(promptWorkspaceTitle, "/alpha-beta/gamma_delta")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x7f}, Alt: true})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
		t.Fatalf("alt-del rune value = %q, want %q", got, want)
	}

	model.startPrompt(promptWorkspaceTitle, "/alpha-beta/gamma_delta")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\b'}, Alt: true})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
		t.Fatalf("alt-backspace rune value = %q, want %q", got, want)
	}

	for _, r := range []rune{'⌫', '←'} {
		model.startPrompt(promptWorkspaceTitle, "/alpha-beta/gamma_delta")
		updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(Model)
		if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
			t.Fatalf("option-backspace glyph %q value = %q, want %q", r, got, want)
		}
	}

	model.startPrompt(promptWorkspaceTitle, "/alpha-beta/gamma_delta")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyCtrlH})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
		t.Fatalf("ctrl-h value = %q, want %q", got, want)
	}

	for _, prompt := range []promptKind{promptWorkspace, promptGroup, promptWorkspaceTitle, promptEditGroup, promptEditAgent, promptMoveAgent} {
		model.startPrompt(prompt, "/alpha-beta/gamma_delta")
		updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'➜'}})
		model = updated.(Model)
		if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
			t.Fatalf("%s option-backspace arrow glyph value = %q, want %q", prompt, got, want)
		}
	}

	model.startPrompt(promptWorkspaceTitle, "/alpha-beta/gamma_delta")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'∂'}})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_delta∂"; got != want {
		t.Fatalf("sanity glyph insert value = %q, want %q", got, want)
	}
}

func TestWorkspacePromptShowsInvalidPathStatus(t *testing.T) {
	parent := t.TempDir()
	filePath := filepath.Join(parent, "notes.txt")
	if err := os.WriteFile(filePath, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(testRuntime(t), config.DefaultConfig(), state.Empty())

	model.startPrompt(promptWorkspace, filePath)
	if status := inspectWorkspacePromptPath(model.state, model.input.Value()).message; status != "! Not a directory: "+filePath {
		t.Fatalf("file path status = %q", status)
	}

	missing := filepath.Join(parent, "missing")
	model.startPrompt(promptWorkspace, missing)
	if status := inspectWorkspacePromptPath(model.state, model.input.Value()).message; status != "! Parent exists: "+parent {
		t.Fatalf("missing path status = %q", status)
	}

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeInput {
		t.Fatalf("invalid path should keep prompt open, mode=%s", model.mode)
	}
	if model.message != "! Parent exists: "+parent {
		t.Fatalf("message = %q", model.message)
	}
}

func TestIPCNewCopiesConfiguredTitleTemplate(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.cfg.TitleTemplate = "{status} {auto}"
	model.state.Agents = nil
	model.state.ActiveAgentID = ""

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{}})
	defer killPTYs(model)

	if !response.OK || cmd == nil {
		t.Fatalf("new response/cmd = %#v/%v", response, cmd)
	}
	if len(model.state.Agents) != 1 || model.state.Agents[0].Title != model.cfg.TaskTypes[config.DefaultTaskTypeCodex].TitleTemplate {
		t.Fatalf("agents = %#v", model.state.Agents)
	}
}

func TestIPCNewCreatesRequestedTaskType(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Agents = nil
	model.state.ActiveAgentID = ""

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{"type": config.DefaultTaskTypeShell}})
	defer killPTYs(model)

	if !response.OK || cmd == nil {
		t.Fatalf("new response/cmd = %#v/%v", response, cmd)
	}
	if len(model.state.Agents) != 1 {
		t.Fatalf("agents = %#v", model.state.Agents)
	}
	agent := model.state.Agents[0]
	if agent.TypeID != config.DefaultTaskTypeShell || agent.Title != "Shell" {
		t.Fatalf("shell task = %#v", agent)
	}
}

func TestTerminalTaskInputBypassesCodexCapture(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Agents[0].TypeID = config.DefaultTaskTypeShell
	model.state.Focus = state.FocusCodex
	model.state.NavOpen = false

	cmd := model.applyCodexInput(map[string]string{"input": "text", "text": "hello", "encoded": "hello"})

	if cmd != nil {
		t.Fatalf("terminal input should not start title hook command, got %#v", cmd)
	}
	if got := string(model.codexInputBuffers[model.state.Agents[0].ID]); got != "hello" {
		t.Fatalf("terminal input should track the pending command line without starting a title hook, got %q", got)
	}
}

func TestTerminalTaskCommandShowsLoadingUntilForegroundReturns(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Agents[0].TypeID = config.DefaultTaskTypeShell
	model.state.Agents[0].Status = state.StatusReady
	model.state.Focus = state.FocusCodex
	model.state.NavOpen = false

	if cmd := model.applyTaskInput(map[string]string{"input": codexInputRaw, "encoded": "sleep 10"}); cmd != nil {
		t.Fatal("command text should not start loading before Enter")
	}
	cmd := model.applyTaskInput(map[string]string{"input": codexInputRaw, "encoded": "\r"})
	if cmd == nil {
		t.Fatal("terminal command should start loading tick")
	}
	agent := state.AgentByID(model.state, "a")
	if agent == nil || agent.Status != state.StatusRunning {
		t.Fatalf("terminal command should mark task running: %#v", agent)
	}
	snapshot := model.Snapshot()
	if len(snapshot.LoadingAgentIDs) != 1 || snapshot.LoadingAgentIDs[0] != "a" {
		t.Fatalf("terminal command loading ids = %#v", snapshot.LoadingAgentIDs)
	}

	model.terminalCommands["a"] = time.Now().Add(-terminalCommandLoadingFloor - time.Millisecond)
	model.refreshTerminalTaskActivity()
	agent = state.AgentByID(model.state, "a")
	if agent == nil || agent.Status != state.StatusReady {
		t.Fatalf("terminal command should become ready when no foreground job is active: %#v", agent)
	}
}

func TestTerminalTaskAutoTitleCapturesFirstCommandWhenOptedIn(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Shell title\\n'"
	model.state.Agents[0].TypeID = config.DefaultTaskTypeShell
	model.state.Agents[0].Title = "{auto}"
	model.state.Agents[0].Status = state.StatusReady
	model.state.Focus = state.FocusCodex
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
	agent := state.AgentByID(model.state, "a")
	if agent == nil || agent.AutoTitle != "Shell title" {
		t.Fatalf("auto title = %#v", agent)
	}
	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type_id":"shell"`) || !strings.Contains(string(raw), `"first_message":"echo hello"`) {
		t.Fatalf("payload missing shell command context:\n%s", raw)
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

func TestTerminalTaskClearResetsVisibleScreen(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Agents[0].TypeID = config.DefaultTaskTypeShell
	model.state.Focus = state.FocusCodex
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
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Agents[0].TypeID = config.DefaultTaskTypeShell
	model.state.Agents[0].Status = state.StatusReady

	model.applyPTYData(ptyx.Data{AgentID: "a", Title: "zsh", Text: "prompt"})

	agent := state.AgentByID(model.state, "a")
	if agent == nil || agent.Status != state.StatusReady {
		t.Fatalf("terminal task should stay ready after shell title output: %#v", agent)
	}
	if model.agentLoading("a") {
		t.Fatalf("terminal task should not show loading after PTY title")
	}
}

func TestEnterOnGroupTogglesCollapse(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 0

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !state.IsGroupCollapsed(model.state, "f") {
		t.Fatalf("group should collapse: %#v", model.state.CollapsedGroupIDs)
	}
	if rows := model.groupRows(); len(rows) != 1 {
		t.Fatalf("collapsed group should hide agents, rows=%#v", rows)
	}

	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if state.IsGroupCollapsed(model.state, "f") {
		t.Fatalf("group should reopen: %#v", model.state.CollapsedGroupIDs)
	}
}

func TestNewAgentAlwaysCreatesTopLevelWhenGroupSelected(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.state.ActiveAgentID = ""
	model.groupCursor = 0

	cmd := model.newAgent("Grouped")
	defer killPTYs(model)
	if cmd == nil {
		t.Fatal("expected PTY start command")
	}
	if got := model.state.Agents[len(model.state.Agents)-1].GroupID; got != "" {
		t.Fatalf("group row should create top-level agent, got group %q", got)
	}
}

func TestNewAgentAlwaysCreatesTopLevelWhenAgentSelected(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.NavOpen = true
	model.state.Focus = state.FocusAgents
	model.syncGroupCursor()

	cmd := model.newAgent("Top-level")
	defer killPTYs(model)
	if cmd == nil {
		t.Fatal("expected PTY start command")
	}
	if got := model.state.Agents[len(model.state.Agents)-1].GroupID; got != "" {
		t.Fatalf("grouped agent row should create top-level agent, got group %q", got)
	}
}

func TestIPCFocusRejectsGroupsAlias(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)

	response, _ := model.handleIPC(ipc.Request{Command: "focus", Args: map[string]string{"target": "groups"}})

	if response.OK || response.Message != "focus target must be workspaces, tasks, or codex" {
		t.Fatalf("focus groups response = %#v", response)
	}
}

func TestNavWidthAnimatesOnDrawerToggle(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.width = 120
	model.height = 32
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.navWidth = model.targetNavWidth()

	expanded := model.navWidth
	if expanded <= 0 {
		t.Fatalf("expanded nav width = %d", expanded)
	}
	cmd := model.setCodexFocus()
	if cmd == nil {
		t.Fatal("expected collapse animation command")
	}
	for model.navWidth != 0 {
		model.stepNavAnimation()
	}
	if got := model.View(); strings.Contains(got, "Workspaces") || !strings.Contains(got, "C-b dashboard") || strings.Contains(got, "WEFT") || strings.Contains(got, "C-c") {
		t.Fatalf("codex focus should collapse nav pane:\n%s", got)
	}

	cmd = model.openNav()
	if cmd == nil {
		t.Fatal("expected expand animation command")
	}
	model.stepNavAnimation()
	if model.navWidth <= 0 || model.navWidth >= expanded {
		t.Fatalf("expected partial expansion, width=%d expanded=%d", model.navWidth, expanded)
	}
}

func testModelWithAgent(t *testing.T) Model {
	t.Helper()
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	cfg.CodexCommand = "cat"
	codexType := cfg.TaskTypes[config.DefaultTaskTypeCodex]
	codexType.Command = "cat"
	cfg.TaskTypes[config.DefaultTaskTypeCodex] = codexType
	st := testStateWithAgent(rt.Workspace)
	return NewModel(rt, cfg, st)
}

func testStateWithAgent(workspace string) state.State {
	now := state.NowISO()
	return state.State{
		Version:             state.Version,
		ActiveAgentID:       "a",
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "f",
		Focus:               state.FocusCodex,
		NavOpen:             false,
		Workspaces:          []state.Workspace{{ID: "w", Path: workspace, CreatedAt: now, UpdatedAt: now}},
		Groups:              []state.Group{{ID: "f", WorkspaceID: "w", Path: "inbox", CreatedAt: now, UpdatedAt: now}},
		Agents:              []state.Agent{{ID: "a", WorkspaceID: "w", GroupID: "f", Title: "alpha", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now}},
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

func tuiAgentIDs(agents []state.Agent) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
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
