package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestNewAgentKeyStartsAgentAndFocusesCodex(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	cfg.CodexCommand = "cat"
	model := NewModel(rt, cfg, testStateWithWorkspace(t, rt.Workspace))
	defer killPTYs(model)

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model = updated.(Model)
	defer killPTYs(model)

	if cmd == nil {
		t.Fatal("expected PTY start command")
	}
	if len(model.state.Agents) != 1 {
		t.Fatalf("agents = %#v", model.state.Agents)
	}
	if model.state.Agents[0].Title != cfg.TitleTemplate {
		t.Fatalf("new agent title = %q", model.state.Agents[0].Title)
	}
	if model.state.Agents[0].GroupID != "" {
		t.Fatalf("new agent should be top-level: %#v", model.state.Agents[0])
	}
	if model.state.Focus != state.FocusCodex || model.state.NavOpen {
		t.Fatalf("focus/nav = %s/%t", model.state.Focus, model.state.NavOpen)
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

	model.state.Agents[0].CodexTitle = "Fake Codex Working"
	snapshot = model.Snapshot()
	if len(snapshot.LoadingAgentIDs) != 1 || snapshot.LoadingAgentIDs[0] != "a" {
		t.Fatalf("working agent should be marked loading: %#v", snapshot.LoadingAgentIDs)
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

func TestDeleteAgentConfirmationExplainsStopAndDelete(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	model = updated.(Model)

	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmDeleteAgent || model.pendingID != "a" {
		t.Fatalf("delete confirm state mode=%s confirm=%s pending=%s cmd=%v", model.mode, model.confirm, model.pendingID, cmd)
	}
	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Delete agent",
		"Agent",
		"alpha",
		"Stops the Codex terminal, then removes this agent from Weft.",
		"Y stop and delete",
		"N cancel",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("delete agent confirm missing %q:\n%s", expected, got)
		}
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

	restart := clientRequestArgs(rt, "client-1", "restart_when_idle", nil)
	if restart["client_id"] != "client-1" || restart["client_executable"] == "" {
		t.Fatalf("restart args = %#v", restart)
	}
	if _, ok := restart["launch_workspace"]; ok {
		t.Fatalf("restart request should not reselect launch workspace: %#v", restart)
	}
}

func TestClientUpgradeBannerOpensRestartWhenIdleConfirm(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithAgent(rt.Workspace)
	st.Focus = state.FocusAgents
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:                true,
		Snapshot:          &ipc.Snapshot{State: st, CodexTitle: "alpha", CodexContent: "output", NavWidth: 92},
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: "3.9.0",
	})

	if model.upgrade == nil || model.upgrade.RunningAgents != 1 {
		t.Fatalf("upgrade = %#v", model.upgrade)
	}
	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Upgrade: pending",
		"reopening alone is not enough",
		"Press U",
		"client " + weftversion.Version,
		"supervisor 3.9.0",
		"Press U to restart when idle",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("upgrade banner missing %q:\n%s", expected, got)
		}
	}

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmRestartWhenIdle {
		t.Fatalf("restart confirm state mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
	got = ansi.Strip(model.View())
	for _, expected := range []string{"Restart supervisor when idle?", "client ", "supervisor 3.9.0", "Y restart when idle"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("restart confirm missing %q:\n%s", expected, got)
		}
	}
}

func TestClientQueuedUpgradeFooterOpensCancelConfirm(t *testing.T) {
	rt := testRuntime(t)
	st := testStateWithAgent(rt.Workspace)
	st.Focus = state.FocusAgents
	st.NavOpen = true
	model := NewClientModel(rt, config.DefaultConfig())
	model.width = 160

	model.applyResponse(ipc.Response{
		OK:       true,
		Snapshot: &ipc.Snapshot{State: st, CodexTitle: "alpha", CodexContent: "output", NavWidth: 92},
		Upgrade: &ipc.Upgrade{
			ClientVersion:         weftversion.Version,
			SupervisorVersion:     "3.9.0",
			Compatible:            true,
			RestartRequired:       true,
			RestartWhenIdleQueued: true,
			RunningAgents:         1,
		},
	})

	got := ansi.Strip(model.View())
	for _, expected := range []string{"Upgrade queued", "client " + weftversion.Version, "supervisor 3.9.0", "Close agents to finish", "press U to cancel"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("queued upgrade footer missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "live Codex terminals stay running") {
		t.Fatalf("queued upgrade should not render the old Agent Console banner:\n%s", got)
	}

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	model = updated.(ClientModel)
	if cmd != nil || model.mode != modeConfirm || model.confirm != confirmCancelRestartIdle {
		t.Fatalf("cancel confirm state mode=%s confirm=%s cmd=%v", model.mode, model.confirm, cmd)
	}
	got = ansi.Strip(model.View())
	for _, expected := range []string{"Cancel queued supervisor restart?", "Y cancel restart", "N keep queued"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("cancel restart confirm missing %q:\n%s", expected, got)
		}
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
	if strings.Contains(snapshot.CodexContent, "No Codex agent open") {
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
		t.Fatalf("PTY exit should recover to Agents pane, focus/nav=%s/%t", model.state.Focus, model.state.NavOpen)
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
		t.Fatalf("interrupted PTY exit should recover to Agents pane, focus/nav=%s/%t", model.state.Focus, model.state.NavOpen)
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

	msg := cmd().(titleHookMsg)
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
		t.Fatalf("payload missing forwarded first message:\n%s", raw)
	}
}

func TestTitleHookCapturesEnhancedKeyboardProtocolInput(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	model.cfg.TitleTemplate = "{status} {auto}"
	model.state.Agents[0].Title = model.cfg.TitleTemplate
	model.cfg.TitleHookCommand = "cat > " + shellQuote(payloadPath) + "; printf 'Generated title\\n'"

	sendEnhanced := func(raw string) tea.Cmd {
		t.Helper()
		input, ok := enhancedKeyboardInputFromMsg(testCSIMessage(unknownCSIString(raw)))
		if !ok {
			t.Fatalf("expected enhanced input for %q", raw)
		}
		updated, cmd := model.handleEnhancedKeyboardInput(input)
		model = updated.(Model)
		return cmd
	}
	for _, raw := range []string{
		"\x1b[102u", "\x1b[105u", "\x1b[120u", "\x1b[32u", "\x1b[108u",
		"\x1b[111u", "\x1b[103u", "\x1b[105u", "\x1b[110u",
	} {
		if cmd := sendEnhanced(raw); cmd != nil {
			t.Fatalf("hook should not run before Enter for %q", raw)
		}
	}
	cmd := sendEnhanced("\x1b[13u")
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
		t.Fatalf("payload missing enhanced keyboard first message:\n%s", raw)
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
	model.prompt = promptRenameAgent
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

	model.prompt = promptRenameAgent
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
	model.prompt = promptRenameAgent
	model.mode = modeInput
	model.pendingID = "a"
	model.input.SetValue("{auto}")

	got := ansi.Strip(model.View())
	if !strings.Contains(got, "Auto title error") || !strings.Contains(got, "bad config") {
		t.Fatalf("rename pane missing hook error:\n%s", got)
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

func TestRenameAgentPromptPreviewsEditedTitle(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.cfg.TitleTemplate = "{auto}"
	model.state.Agents[0].CodexTitle = "Fake Codex Ready"
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1
	model.prompt = promptRenameAgent
	model.mode = modeInput
	model.pendingID = "a"
	model.input.SetValue("{codex}")

	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Rename agent",
		"Preview",
		"Fake Codex Ready",
		"Variables",
		"{auto}: generated title",
		"Enter save",
		"Esc cancel",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("rename modal missing %q:\n%s", expected, got)
		}
	}
}

func TestRenameAgentPromptPrefillsStoredAgentTitleTemplate(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Agents[0].Title = "{status} {auto}"
	model.state.Agents[0].AutoTitle = "Fix login"
	model.state.Agents[0].CodexTitle = "Fake Codex Ready"
	model.state.Focus = state.FocusAgents
	model.state.NavOpen = true
	model.groupCursor = 1

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	model = updated.(Model)

	if cmd != nil {
		t.Fatalf("rename prompt should not start command, got %#v", cmd)
	}
	if model.mode != modeInput || model.prompt != promptRenameAgent || model.pendingID != "a" {
		t.Fatalf("prompt state = mode:%s prompt:%s pending:%s", model.mode, model.prompt, model.pendingID)
	}
	if got, want := model.input.Value(), "{status} {auto}"; got != want {
		t.Fatalf("rename prompt value = %q, want stored title template %q", got, want)
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

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
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

	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
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
		"Enter add",
		"Down suggestions",
		"Esc cancel",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("workspace modal missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "> current") {
		t.Fatalf("workspace menu should start closed:\n%s", got)
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
		"Flat and unique in this workspace.",
		"Esc cancel",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("group prompt missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "Enter create") {
		t.Fatalf("empty required prompt should not advertise submit:\n%s", got)
	}
	if strings.Count(got, "╭") < 2 || strings.Count(got, "╰") < 2 {
		t.Fatalf("group prompt should render a bordered input box:\n%s", got)
	}

	model.input.SetValue("release")
	got = ansi.Strip(model.View())
	for _, expected := range []string{"Ready", "Enter create", "Esc cancel"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("valid group prompt missing %q:\n%s", expected, got)
		}
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
	for _, expected := range []string{"Down suggestions", "Enter add", "Esc cancel"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("closed suggestion state missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "> alpha-project") || strings.Contains(got, "beta-project") {
		t.Fatalf("suggestion menu should start closed:\n%s", got)
	}

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got, want := promptCurrentSuggestion(model.promptContext(), model.input.Value(), model.promptSuggestionIndex), withTrailingSeparator(alpha); got != want {
		t.Fatalf("opened suggestion = %q, want %q", got, want)
	}
	got = ansi.Strip(model.View())
	for _, expected := range []string{"> alpha-project", "beta-project", "Enter choose", "Up/Down move", "Esc close suggestions"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("open suggestion state missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "alpha-project/") || strings.Contains(got, "beta-project/") {
		t.Fatalf("suggestion labels should not render trailing slashes:\n%s", got)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
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
	model.groupCursor = 1
	now := state.NowISO()
	model.state.Groups = append(model.state.Groups, state.Group{ID: "release", WorkspaceID: "w", Path: "release", CreatedAt: now, UpdatedAt: now})

	model.startPrompt(promptMoveAgent, "")
	got := ansi.Strip(model.View())
	for _, expected := range []string{"Move agent", "Top-level agent", "Enter top-level", "Esc cancel"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("blank move prompt missing %q:\n%s", expected, got)
		}
	}

	model.startPrompt(promptMoveAgent, "rel")
	if got, want := model.input.MatchedSuggestions(), []string{"release"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("matched groups = %#v, want %#v", got, want)
	}
	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	got = ansi.Strip(model.View())
	for _, expected := range []string{"> release", "Enter choose", "Esc close suggestions"} {
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
	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	got := ansi.Strip(model.View())
	if !strings.Contains(got, "> release-rollout") || !strings.Contains(got, "Enter choose") {
		t.Fatalf("substring group suggestion menu missing match:\n%s", got)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyEnter})
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

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
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

func TestWorkspacePromptTabOpensAndChoosesDirectory(t *testing.T) {
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
	if !model.promptSuggestionOpen {
		t.Fatal("first tab should open suggestions")
	}
	if model.input.Value() != filepath.Join(parent, "alp") {
		t.Fatalf("first tab should not change input, got %q", model.input.Value())
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if want := alpha; model.input.Value() != want {
		t.Fatalf("completed path = %q, want %q", model.input.Value(), want)
	}
	if model.promptSuggestionOpen {
		t.Fatal("second tab should close suggestions after choosing")
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
	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if !model.promptSuggestionOpen {
		t.Fatal("first tab should open substring suggestions")
	}
	if got := ansi.Strip(model.View()); !strings.Contains(got, "> client-suffix") {
		t.Fatalf("substring suggestion should render target directory:\n%s", got)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.input.Value() != target {
		t.Fatalf("completed substring path = %q, want %q", model.input.Value(), target)
	}
	if model.promptSuggestionOpen {
		t.Fatal("second tab should close substring suggestions after choosing")
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

	model.startPrompt(promptWorkspace, "/alpha-beta/gamma_delta")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'➜'}})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
		t.Fatalf("option-backspace arrow glyph value = %q, want %q", got, want)
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
	if len(model.state.Agents) != 1 || model.state.Agents[0].Title != model.cfg.TitleTemplate {
		t.Fatalf("agents = %#v", model.state.Agents)
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

	if response.OK || response.Message != "focus target must be workspaces, agents, or codex" {
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
	if got := model.View(); strings.Contains(got, "Workspaces") || !strings.Contains(got, "WEFT  C-b dashboard") || strings.Contains(got, "C-c") {
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
