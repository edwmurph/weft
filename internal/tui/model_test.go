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
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/titles"
)

func TestEmptyCommandCenterStartsInAgentsFocus(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("weft-test")

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
	cfg := config.DefaultConfig("weft-test")
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

func TestSnapshotShowsActiveAgentStartError(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("weft-test")
	st := testStateWithAgent(rt.Workdir)
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
		t.Fatalf("C-c should forward while Codex has focus, message=%q", model.message)
	}

	model.state.Agents[0].CodexTitle = "Fake Codex Ready"
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(Model)
	if model.message != "" {
		t.Fatalf("C-c should still forward after Codex is ready, message=%q", model.message)
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

	model.state.Focus = state.FocusFolders
	model.state.NavOpen = true
	model.navWidth = 60
	if got, want := model.ptyWidth(), 37; got != want {
		t.Fatalf("split pty width = %d, want visible content width %d", got, want)
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
	if got := model.renderAgentTitle(*state.AgentByID(model.state, "a")); got != "running Generated before rename" {
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
	cfg := config.DefaultConfig("weft-test")
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

func TestNewWorkdirPromptPrefillsSelectedParentAndShowsPathStatus(t *testing.T) {
	parent := t.TempDir()
	current := filepath.Join(parent, "current")
	if err := os.Mkdir(current, 0o700); err != nil {
		t.Fatal(err)
	}
	rt := testRuntime(t)
	rt.Workdir = current
	cfg := config.DefaultConfig("weft-test")
	model := NewModel(rt, cfg, state.Empty())
	model.state.Focus = state.FocusWorkdirs
	model.state.NavOpen = true

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	model = updated.(Model)
	if cmd != nil {
		t.Fatalf("workdir prompt should not start command, got %#v", cmd)
	}
	if model.mode != modeInput || model.prompt != promptWorkdir {
		t.Fatalf("prompt state = mode:%s prompt:%s", model.mode, model.prompt)
	}
	if want := withTrailingSeparator(displayPathForPrompt(parent)); model.input.Value() != want {
		t.Fatalf("prompt value = %q, want %q", model.input.Value(), want)
	}

	got := ansi.Strip(model.View())
	for _, expected := range []string{
		"Add workdir",
		"Path",
		"✓ ",
		"Enter add",
		"Down open options",
		"Esc cancel",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("workdir modal missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "> current") {
		t.Fatalf("workdir menu should start closed:\n%s", got)
	}
	if status := inspectWorkdirPromptPath(model.state, model.input.Value()).message; status != "✓ "+parent {
		t.Fatalf("path status = %q", status)
	}
	if strings.Count(got, "╭") < 2 || strings.Count(got, "╰") < 2 {
		t.Fatalf("workdir modal should render a bordered input box:\n%s", got)
	}
	if got := model.input.MatchedSuggestions(); len(got) != 1 || got[0] != withTrailingSeparator(current) {
		t.Fatalf("matched suggestions = %#v", got)
	}
}

func TestWorkdirPromptSuggestionMenuSupportsArrowSelection(t *testing.T) {
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
	model := NewModel(testRuntime(t), config.DefaultConfig("weft-test"), state.Empty())
	model.startPrompt(promptWorkdir, withTrailingSeparator(parent))

	got := ansi.Strip(model.View())
	for _, expected := range []string{"Down open options", "Enter add", "Esc cancel"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("closed suggestion state missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "> alpha-project") || strings.Contains(got, "beta-project") {
		t.Fatalf("suggestion menu should start closed:\n%s", got)
	}

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got, want := model.input.CurrentSuggestion(), withTrailingSeparator(alpha); got != want {
		t.Fatalf("opened suggestion = %q, want %q", got, want)
	}
	got = ansi.Strip(model.View())
	for _, expected := range []string{"> alpha-project", "beta-project", "Enter choose", "Up/Down move", "Esc close options"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("open suggestion state missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "alpha-project/") || strings.Contains(got, "beta-project/") {
		t.Fatalf("suggestion labels should not render trailing slashes:\n%s", got)
	}

	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got, want := model.input.CurrentSuggestion(), withTrailingSeparator(beta); got != want {
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
		t.Fatalf("second enter should add workdir, mode=%s", model.mode)
	}
	if model.state.SelectedWorkdirID == "" || model.state.Workdirs[len(model.state.Workdirs)-1].Path != beta {
		t.Fatalf("workdir was not added/selected: %#v", model.state.Workdirs)
	}
}

func TestWorkdirPromptSuggestionMenuScrollsWithSelection(t *testing.T) {
	parent := t.TempDir()
	for index := 0; index < 12; index++ {
		if err := os.Mkdir(filepath.Join(parent, fmt.Sprintf("project-%02d", index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	model := NewModel(testRuntime(t), config.DefaultConfig("weft-test"), state.Empty())
	model.height = 32
	model.startPrompt(promptWorkdir, withTrailingSeparator(parent))

	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	for index := 0; index < 9; index++ {
		updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(Model)
	}

	if got, want := model.input.CurrentSuggestion(), withTrailingSeparator(filepath.Join(parent, "project-09")); got != want {
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

func TestWorkdirPromptTabOpensAndChoosesDirectory(t *testing.T) {
	parent := t.TempDir()
	alpha := filepath.Join(parent, "alpha-project")
	if err := os.Mkdir(alpha, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(parent, "beta-project"), 0o700); err != nil {
		t.Fatal(err)
	}
	model := NewModel(testRuntime(t), config.DefaultConfig("weft-test"), state.Empty())
	model.startPrompt(promptWorkdir, filepath.Join(parent, "alp"))

	if got, want := model.input.MatchedSuggestions(), []string{withTrailingSeparator(alpha)}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("matched suggestions = %#v, want %#v", got, want)
	}
	updated, _ := model.handleInputKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if !model.workdirSuggestionOpen {
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
	if model.workdirSuggestionOpen {
		t.Fatal("second tab should close suggestions after choosing")
	}
	if status := inspectWorkdirPromptPath(model.state, model.input.Value()).message; status != "✓ "+alpha {
		t.Fatalf("completed path status = %q", status)
	}
}

func TestPromptInputSupportsOptionWordEditing(t *testing.T) {
	model := NewModel(testRuntime(t), config.DefaultConfig("weft-test"), state.Empty())
	model.startPrompt(promptWorkdirTitle, "/alpha-beta/gamma_delta")

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
	model := NewModel(testRuntime(t), config.DefaultConfig("weft-test"), state.Empty())
	model.startPrompt(promptWorkdirTitle, "/alpha-beta/gamma_delta")

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

	model.startPrompt(promptWorkdirTitle, "/alpha-beta/gamma_delta")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x7f}, Alt: true})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
		t.Fatalf("alt-del rune value = %q, want %q", got, want)
	}

	model.startPrompt(promptWorkdirTitle, "/alpha-beta/gamma_delta")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\b'}, Alt: true})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
		t.Fatalf("alt-backspace rune value = %q, want %q", got, want)
	}

	for _, r := range []rune{'⌫', '←'} {
		model.startPrompt(promptWorkdirTitle, "/alpha-beta/gamma_delta")
		updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(Model)
		if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
			t.Fatalf("option-backspace glyph %q value = %q, want %q", r, got, want)
		}
	}

	model.startPrompt(promptWorkdirTitle, "/alpha-beta/gamma_delta")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyCtrlH})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
		t.Fatalf("ctrl-h value = %q, want %q", got, want)
	}

	model.startPrompt(promptWorkdir, "/alpha-beta/gamma_delta")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'➜'}})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_"; got != want {
		t.Fatalf("option-backspace arrow glyph value = %q, want %q", got, want)
	}

	model.startPrompt(promptWorkdirTitle, "/alpha-beta/gamma_delta")
	updated, _ = model.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'∂'}})
	model = updated.(Model)
	if got, want := model.input.Value(), "/alpha-beta/gamma_delta∂"; got != want {
		t.Fatalf("sanity glyph insert value = %q, want %q", got, want)
	}
}

func TestWorkdirPromptShowsInvalidPathStatus(t *testing.T) {
	parent := t.TempDir()
	filePath := filepath.Join(parent, "notes.txt")
	if err := os.WriteFile(filePath, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(testRuntime(t), config.DefaultConfig("weft-test"), state.Empty())

	model.startPrompt(promptWorkdir, filePath)
	if status := inspectWorkdirPromptPath(model.state, model.input.Value()).message; status != "! Not a directory: "+filePath {
		t.Fatalf("file path status = %q", status)
	}

	missing := filepath.Join(parent, "missing")
	model.startPrompt(promptWorkdir, missing)
	if status := inspectWorkdirPromptPath(model.state, model.input.Value()).message; status != "! Parent exists: "+parent {
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
	if got := model.View(); strings.Contains(got, "Workdirs") || !strings.Contains(got, "WEFT  C-b command center  C-c to Codex") {
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
	cfg := config.DefaultConfig("weft-test")
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
		SocketPath: filepath.Join(dir, "weft.sock"),
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
