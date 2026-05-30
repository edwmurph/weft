package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/codux/internal/config"
	"github.com/edwmurph/codux/internal/ipc"
	"github.com/edwmurph/codux/internal/state"
	"github.com/edwmurph/codux/internal/titles"
)

func TestEmptyCommandCenterStartsInAgentsFocus(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("codux-test")

	model := NewModel(rt, cfg, state.Empty())

	if model.state.Focus != state.FocusFolders || !model.state.NavOpen {
		t.Fatalf("focus/nav = %s/%t", model.state.Focus, model.state.NavOpen)
	}
	if len(model.state.Workdirs) != 1 || len(model.state.Folders) != 0 {
		t.Fatalf("seeded state = %#v", model.state)
	}
}

func TestNewAgentKeyStartsAgentAndFocusesCodex(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("codux-test")
	cfg.CodexCommand = "cat"
	model := NewModel(rt, cfg, state.Empty())
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
	if model.state.Agents[0].Title != titles.CodexTemplate {
		t.Fatalf("new agent title = %q", model.state.Agents[0].Title)
	}
	if model.state.Agents[0].FolderID != "" {
		t.Fatalf("new agent should be top-level: %#v", model.state.Agents[0])
	}
	if model.state.Focus != state.FocusCodex || model.state.NavOpen {
		t.Fatalf("focus/nav = %s/%t", model.state.Focus, model.state.NavOpen)
	}
}

func TestCodexFocusOnlyHandlesGlobalShortcuts(t *testing.T) {
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("s")},
		{Type: tea.KeyRunes, Runes: []rune("?")},
		{Type: tea.KeyRunes, Runes: []rune("n")},
		{Type: tea.KeyShiftRight},
		{Type: tea.KeyCtrlD},
		{Type: tea.KeyCtrlQ},
	} {
		model := testModelWithAgent(t)
		defer killPTYs(model)

		updated, cmd := model.handleKey(msg)
		model = updated.(Model)

		if cmd != nil {
			t.Fatalf("%s should not start command center command in codex focus", msg.String())
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
	if model.state.Focus != state.FocusFolders || !model.state.NavOpen {
		t.Fatalf("C-b should open command center, got %s/%t", model.state.Focus, model.state.NavOpen)
	}

	model.state.Focus = state.FocusCodex
	model.state.NavOpen = false
	model.state.Agents[0].CodexTitle = "Fake Codex Working"
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(Model)
	if model.message != "" {
		t.Fatalf("C-c should forward while Codex is running, message=%q", model.message)
	}

	model.state.Agents[0].CodexTitle = "Fake Codex Ready"
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(Model)
	if model.message != "closed Codux clients" {
		t.Fatalf("C-c should close Codux clients when Codex is ready, message=%q", model.message)
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

	model.state.Focus = state.FocusFolders
	model.state.NavOpen = true
	model.folderCursor = 1
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

	model.state.Focus = state.FocusFolders
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
	model.state.Focus = state.FocusFolders
	model.state.NavOpen = true
	model.folderCursor = 1
	model.prompt = promptRenameAgent
	model.mode = modeInput
	model.pendingID = "a"
	model.input.SetValue("{codex}")

	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Rename agent",
		"Preview",
		"Fake Codex Ready",
		"Auto title unavailable",
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

func TestWorkdirRenamePromptSetsAndClearsTitleOverride(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("codux-test")
	model := NewModel(rt, cfg, state.Empty())
	model.state.Focus = state.FocusWorkdirs
	model.state.NavOpen = true
	model.lastNavFocus = state.FocusWorkdirs

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	model = updated.(Model)
	if cmd != nil {
		t.Fatalf("rename prompt should not start command, got %#v", cmd)
	}
	if model.mode != modeInput || model.prompt != promptWorkdirTitle || model.pendingID != model.state.SelectedWorkdirID {
		t.Fatalf("prompt state = mode:%s prompt:%s pending:%s selected:%s", model.mode, model.prompt, model.pendingID, model.state.SelectedWorkdirID)
	}

	model.input.SetValue("Trading Engine")
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeNormal {
		t.Fatalf("mode after save = %s", model.mode)
	}
	if got := model.state.Workdirs[0].Title; got != "Trading Engine" {
		t.Fatalf("title override = %q", got)
	}

	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	model = updated.(Model)
	model.input.SetValue("")
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if got := model.state.Workdirs[0].Title; got != "" {
		t.Fatalf("blank input should clear title override, got %q", got)
	}
	if model.message != "cleared workdir title" {
		t.Fatalf("message = %q", model.message)
	}
}

func TestIPCNewDefaultsToCodexTemplate(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Agents = nil
	model.state.ActiveAgentID = ""

	response, cmd := model.handleIPC(ipc.Request{Command: "new", Args: map[string]string{}})
	defer killPTYs(model)

	if !response.OK || cmd == nil {
		t.Fatalf("new response/cmd = %#v/%v", response, cmd)
	}
	if len(model.state.Agents) != 1 || model.state.Agents[0].Title != titles.CodexTemplate {
		t.Fatalf("agents = %#v", model.state.Agents)
	}
}

func TestEnterOnGroupTogglesCollapse(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusFolders
	model.state.NavOpen = true
	model.folderCursor = 0

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !state.IsGroupCollapsed(model.state, "f") {
		t.Fatalf("group should collapse: %#v", model.state.CollapsedGroupIDs)
	}
	if rows := model.folderRows(); len(rows) != 1 {
		t.Fatalf("collapsed group should hide agents, rows=%#v", rows)
	}

	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if state.IsGroupCollapsed(model.state, "f") {
		t.Fatalf("group should reopen: %#v", model.state.CollapsedGroupIDs)
	}
}

func TestNewAgentUsesCurrentGroupOnlyWhenCursorIsGrouped(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.state.Focus = state.FocusFolders
	model.state.NavOpen = true
	model.state.ActiveAgentID = ""
	model.folderCursor = 0

	cmd := model.newAgent("Grouped")
	defer killPTYs(model)
	if cmd == nil {
		t.Fatal("expected PTY start command")
	}
	if got := model.state.Agents[len(model.state.Agents)-1].FolderID; got != "f" {
		t.Fatalf("group row should create grouped agent, got folder %q", got)
	}

	ungrouped := model.state.Agents[len(model.state.Agents)-1]
	ungrouped.ID = "ungrouped"
	ungrouped.FolderID = ""
	model.state.Agents = append([]state.Agent{ungrouped}, model.state.Agents...)
	model.state.ActiveAgentID = "ungrouped"
	model.state.NavOpen = true
	model.state.Focus = state.FocusFolders
	model.syncFolderCursor()

	cmd = model.newAgent("Top-level")
	defer killPTYs(model)
	if cmd == nil {
		t.Fatal("expected PTY start command")
	}
	if got := model.state.Agents[len(model.state.Agents)-1].FolderID; got != "" {
		t.Fatalf("top-level agent row should create ungrouped agent, got folder %q", got)
	}
}

func TestIPCFocusAcceptsGroupsAlias(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)

	response, _ := model.handleIPC(ipc.Request{Command: "focus", Args: map[string]string{"target": "groups"}})

	if !response.OK {
		t.Fatalf("focus groups failed: %#v", response)
	}
	if model.state.Focus != state.FocusFolders || !model.state.NavOpen {
		t.Fatalf("focus/nav = %s/%t", model.state.Focus, model.state.NavOpen)
	}
}

func TestNavWidthAnimatesOnDrawerToggle(t *testing.T) {
	model := testModelWithAgent(t)
	defer killPTYs(model)
	model.width = 120
	model.height = 32
	model.state.Focus = state.FocusFolders
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
	if got := model.View(); strings.Contains(got, "Workdirs") || !strings.Contains(got, "CODUX  C-b command center  C-c interrupt/close") {
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
	cfg := config.DefaultConfig("codux-test")
	cfg.CodexCommand = "cat"
	st := testStateWithAgent(rt.Workdir)
	return NewModel(rt, cfg, st)
}

func testStateWithAgent(workdir string) state.State {
	now := state.NowISO()
	return state.State{
		Version:           state.Version,
		ActiveAgentID:     "a",
		SelectedWorkdirID: "w",
		SelectedFolderID:  "f",
		Focus:             state.FocusCodex,
		NavOpen:           false,
		Workdirs:          []state.Workdir{{ID: "w", Path: workdir, CreatedAt: now, UpdatedAt: now}},
		Folders:           []state.Folder{{ID: "f", WorkdirID: "w", Path: "inbox", CreatedAt: now, UpdatedAt: now}},
		Agents:            []state.Agent{{ID: "a", WorkdirID: "w", FolderID: "f", Title: "alpha", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now}},
	}
}

func killPTYs(model Model) {
	for _, pty := range model.ptys {
		pty.Kill()
	}
}

func testRuntime(t *testing.T) config.Runtime {
	t.Helper()
	dir := t.TempDir()
	return config.Runtime{
		Workdir:    dir,
		Dir:        dir,
		ConfigPath: filepath.Join(dir, "config.toml"),
		StatePath:  filepath.Join(dir, "state.json"),
		SocketPath: filepath.Join(dir, "codux.sock"),
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
