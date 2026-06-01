package tui

import (
	"image/color"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/state"
)

func TestSelectedConsoleTextCopiesOnlyDraggedCells(t *testing.T) {
	lines := []string{
		"alpha beta  ",
		"gamma delta ",
	}
	selection := consoleSelection{
		active: true,
		start:  consolePoint{col: 6, row: 0},
		end:    consolePoint{col: 4, row: 1},
	}

	got := selectedConsoleText(lines, selection, 12)

	if got != "beta\ngamma" {
		t.Fatalf("selected text = %q", got)
	}
}

func TestSelectedConsoleTextStartsAfterCodexMargin(t *testing.T) {
	lines := []string{
		"   gofmt -w cmd internal tests",
	}
	selection := consoleSelection{
		active:    true,
		start:     consolePoint{col: 0, row: 0},
		end:       consolePoint{col: 26, row: 0},
		colOffset: 3,
	}

	got := selectedConsoleText(lines, selection, 29)

	if got != "gofmt -w cmd internal tests" {
		t.Fatalf("selected text = %q", got)
	}
}

func TestSelectedConsoleTextUsesShiftedSelectionAreaForContinuationRows(t *testing.T) {
	lines := []string{
		"  The dashboard frames a living scene:",
		"  A task, a timer, in-between.",
		"  A status word that won't sit still,",
		"  A pulse of work, a stubborn will.",
	}
	selection := consoleSelection{
		active:    true,
		start:     consolePoint{col: 0, row: 0},
		end:       consolePoint{col: 33, row: 3},
		colOffset: 2,
	}

	got := selectedConsoleText(lines, selection, 38)

	want := strings.Join([]string{
		"The dashboard frames a living scene:",
		"A task, a timer, in-between.",
		"A status word that won't sit still,",
		"A pulse of work, a stubborn will.",
	}, "\n")
	if got != want {
		t.Fatalf("selected text = %q", got)
	}
}

func TestSelectedCodexContentHighlightsShiftedSelectionArea(t *testing.T) {
	lines := []string{
		"  The dashboard frames a living scene:",
		"  A task, a timer, in-between.",
	}
	selection := consoleSelection{
		active:    true,
		start:     consolePoint{col: 0, row: 0},
		end:       consolePoint{col: 28, row: 1},
		colOffset: 2,
	}

	got := selectedCodexContent(lines, selection, 38)

	if !strings.Contains(got, "  "+ansiReverseStart+"The dashboard frames a living") ||
		!strings.Contains(got, "\n  "+ansiReverseStart+"A task, a timer, in-between.") {
		t.Fatalf("highlighted content should leave margin unhighlighted:\n%q", got)
	}
}

func TestSelectedConsoleTextKeepsCodeIndentInsideShiftedArea(t *testing.T) {
	lines := []string{
		"   if err != nil {",
		"       return err",
		"   }",
	}
	selection := consoleSelection{
		active:    true,
		start:     consolePoint{col: 0, row: 0},
		end:       consolePoint{col: 14, row: 2},
		colOffset: 3,
	}

	got := selectedConsoleText(lines, selection, 21)

	if got != "if err != nil {\n    return err\n}" {
		t.Fatalf("selected text = %q", got)
	}
}

func TestCodexSelectableMarginIgnoresChromeAtColumnZero(t *testing.T) {
	lines := []string{
		"╭──────────────────╮",
		"│ Codex header      │",
		"  The dashboard frames a living scene:",
		"  A task, a timer, in-between.",
	}

	if got := codexSelectableMargin(lines); got != 2 {
		t.Fatalf("selectable margin = %d", got)
	}
}

func TestSelectedCodexContentHighlightsDraggedCells(t *testing.T) {
	selection := consoleSelection{
		active: true,
		start:  consolePoint{col: 1, row: 0},
		end:    consolePoint{col: 3, row: 0},
	}

	got := selectedCodexContent([]string{"abcdef"}, selection, 6)

	if !strings.Contains(got, "a"+ansiReverseStart+"bcd"+ansiReverseEnd+"ef") {
		t.Fatalf("highlighted content = %q", got)
	}
}

func TestSelectedStyledCodexContentPreservesCodexColors(t *testing.T) {
	content := "\x1b[38;2;196;42;42mred \x1b[48;2;40;40;49mblue\x1b[0m"
	selection := consoleSelection{
		active: true,
		start:  consolePoint{col: 1, row: 0},
		end:    consolePoint{col: 6, row: 0},
	}

	got := selectedStyledCodexContent(content, selection, 8)

	if stripped := ansi.Strip(got); stripped != "red blue" {
		t.Fatalf("highlighted content should keep visible text, got %q", stripped)
	}
	screen := NewTerminalScreen(8, 1)
	screen.Write(got)
	assertStyleRGB(t, screen.cells[0][0].style.Fg, color.RGBA{R: 196, G: 42, B: 42, A: 0xff})
	if screen.cells[0][0].style.Attrs.Contains(cellbuf.ReverseAttr) {
		t.Fatalf("unselected cell should not be reversed: %#v", screen.cells[0][0].style)
	}
	assertStyleRGB(t, screen.cells[0][1].style.Fg, color.RGBA{R: 196, G: 42, B: 42, A: 0xff})
	if !screen.cells[0][1].style.Attrs.Contains(cellbuf.ReverseAttr) {
		t.Fatalf("selected foreground-colored cell should be reversed: %#v", screen.cells[0][1].style)
	}
	assertStyleRGB(t, screen.cells[0][4].style.Bg, color.RGBA{R: 40, G: 40, B: 49, A: 0xff})
	if !screen.cells[0][4].style.Attrs.Contains(cellbuf.ReverseAttr) {
		t.Fatalf("selected background-colored cell should be reversed: %#v", screen.cells[0][4].style)
	}
}

func TestClientMouseDragCopiesConsoleSelection(t *testing.T) {
	oldWriteClipboard := writeClipboard
	var copied string
	writeClipboard = func(value string) error {
		copied = value
		return nil
	}
	defer func() { writeClipboard = oldWriteClipboard }()

	model := ClientModel{
		cfg:    config.DefaultConfig(),
		width:  80,
		height: 8,
		snapshot: ipc.Snapshot{
			State: state.State{
				Focus:         state.FocusCodex,
				ActiveAgentID: "a",
				Workspaces:    []state.Workspace{{ID: "w", Path: "/tmp/project"}},
				Agents:        []state.Agent{{ID: "a", WorkspaceID: "w"}},
			},
			CodexTitle:      "Codex",
			CodexContent:    "alpha beta",
			CodexPlainLines: []string{"alpha beta                                                                      "},
		},
	}
	area, ok := model.codexContentArea()
	if !ok {
		t.Fatal("expected codex content area")
	}

	updated, _ := model.handleMouse(tea.MouseMsg{
		X:      area.x + 6,
		Y:      area.y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)
	updated, _ = model.handleMouse(tea.MouseMsg{
		X:      area.x + 9,
		Y:      area.y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	model = updated.(ClientModel)
	updated, _ = model.handleMouse(tea.MouseMsg{
		X:      area.x + 9,
		Y:      area.y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	model = updated.(ClientModel)

	if copied != "beta" {
		t.Fatalf("copied = %q", copied)
	}
	if model.toastText != "Copied 4 characters" {
		t.Fatalf("toast = %q", model.toastText)
	}
	if !strings.Contains(model.View(), "Copied 4 characters") {
		t.Fatalf("toast did not render:\n%s", model.View())
	}
	if model.mouseSelection.active {
		t.Fatal("selection should clear after copy")
	}
}

func TestClientMouseWheelScrollsConsoleScrollback(t *testing.T) {
	model := ClientModel{
		cfg:    config.DefaultConfig(),
		width:  80,
		height: 8,
		snapshot: ipc.Snapshot{
			State: state.State{
				Focus:         state.FocusCodex,
				ActiveAgentID: "a",
				Workspaces:    []state.Workspace{{ID: "w", Path: "/tmp/project"}},
				Agents:        []state.Agent{{ID: "a", WorkspaceID: "w"}},
			},
			CodexTitle:           "Codex",
			CodexContent:         strings.Join([]string{"history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"}, "\n"),
			CodexPlainLines:      []string{"history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"},
			CodexScrollback:      strings.Join([]string{"history line 01", "history line 02", "history line 03", "history line 04", "history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"}, "\n"),
			CodexScrollbackLines: []string{"history line 01", "history line 02", "history line 03", "history line 04", "history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"},
		},
	}
	area, ok := model.codexFrameArea()
	if !ok {
		t.Fatal("expected codex frame area")
	}

	updated, cmd := model.handleMouse(tea.MouseMsg{
		X:      area.x,
		Y:      area.y,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)

	if cmd != nil {
		t.Fatal("mouse wheel scrollback should not forward input to Codex")
	}
	if model.codexScrollOffset != 3 {
		t.Fatalf("scroll offset = %d, want 3", model.codexScrollOffset)
	}
	view := model.View()
	if !strings.Contains(view, "history line 02") || strings.Contains(view, "history line 10") {
		t.Fatalf("view should show older scrollback instead of bottom:\n%s", view)
	}

	updated, _ = model.handleMouse(tea.MouseMsg{
		X:      area.x,
		Y:      area.y,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)
	if model.codexScrollOffset != 0 {
		t.Fatalf("scroll offset after wheel down = %d, want 0", model.codexScrollOffset)
	}
}

func TestClientMouseWheelScrollsTaskPreviewScrollback(t *testing.T) {
	model := ClientModel{
		cfg:    config.DefaultConfig(),
		width:  140,
		height: 8,
		snapshot: ipc.Snapshot{
			State: state.State{
				Focus:               state.FocusAgents,
				NavOpen:             true,
				ActiveAgentID:       "a",
				SelectedAgentID:     "a",
				SelectedWorkspaceID: "w",
				Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project"}},
				Agents:              []state.Agent{{ID: "a", WorkspaceID: "w"}},
			},
			NavWidth:             minTwoPaneNavWidth,
			CodexTitle:           "Codex",
			CodexContent:         strings.Join([]string{"history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"}, "\n"),
			CodexPlainLines:      []string{"history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"},
			CodexScrollback:      strings.Join([]string{"history line 01", "history line 02", "history line 03", "history line 04", "history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"}, "\n"),
			CodexScrollbackLines: []string{"history line 01", "history line 02", "history line 03", "history line 04", "history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"},
		},
	}
	area, ok := model.codexFrameArea()
	if !ok {
		t.Fatal("expected task preview frame area")
	}

	updated, cmd := model.handleMouse(tea.MouseMsg{
		X:      area.x,
		Y:      area.y,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)

	if cmd != nil {
		t.Fatal("preview mouse wheel scrollback should not forward input to Codex")
	}
	if model.codexScrollOffset != 3 {
		t.Fatalf("scroll offset = %d, want 3", model.codexScrollOffset)
	}
	view := model.View()
	if !strings.Contains(view, "history line 02") || strings.Contains(view, "history line 10") {
		t.Fatalf("preview should show older scrollback instead of bottom:\n%s", view)
	}
	if model.snapshot.State.Focus != state.FocusAgents || !model.snapshot.State.NavOpen {
		t.Fatalf("preview scroll should keep dashboard focus/nav, got %s/%t", model.snapshot.State.Focus, model.snapshot.State.NavOpen)
	}
}

func TestClientMouseSelectsNewWorkspaceCardAndEnterOpensPrompt(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	st := testStateWithAgent(rt.Workspace)
	st.Focus = state.FocusWorkspaces
	st.NavOpen = true
	model := NewClientModel(rt, cfg)
	model.width = 120
	model.height = 16
	model.snapshot = ipc.Snapshot{
		State:        st,
		CodexTitle:   "alpha",
		CodexContent: "last task output",
		NavWidth:     workspaceNavFrameWidth(st, model.width),
	}
	area, ok := model.newWorkspaceCardArea()
	if !ok {
		t.Fatal("expected new workspace card hit area")
	}

	updated, cmd := model.handleMouse(tea.MouseMsg{
		X:      area.x + 1,
		Y:      area.y + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)

	if cmd == nil {
		t.Fatal("clicking the new workspace card should focus the Workspaces pane")
	}
	if !model.newWorkspaceCardSelected || model.snapshot.State.Focus != state.FocusWorkspaces {
		t.Fatalf("new workspace card should be selected, selected=%t focus=%s", model.newWorkspaceCardSelected, model.snapshot.State.Focus)
	}
	renderState := model.dashboardState()
	if renderState.SelectedWorkspaceID != "" {
		t.Fatalf("new workspace card render state should clear selected workspace, got %q", renderState.SelectedWorkspaceID)
	}
	got := ansi.Strip(model.View())
	if !strings.Contains(got, "No workspace selected") || !strings.Contains(got, "No task selected") || strings.Contains(got, "alpha") || strings.Contains(got, "last task output") {
		t.Fatalf("new workspace card selection should empty the Tasks pane and preview:\n%s", got)
	}

	updated, cmd = model.handleNavKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ClientModel)

	if cmd != nil {
		t.Fatal("enter on the new workspace card should open the local prompt")
	}
	if model.mode != modeInput || model.prompt != promptWorkspace {
		t.Fatalf("enter should open workspace prompt, mode=%s prompt=%s", model.mode, model.prompt)
	}
	if !model.newWorkspaceCardSelected {
		t.Fatal("new workspace card selection should persist while its prompt is open")
	}

	updated, cmd = model.handleInputKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(ClientModel)

	if cmd != nil {
		t.Fatal("escaping the workspace prompt should not call the supervisor")
	}
	if model.mode != modeNormal {
		t.Fatalf("esc should close workspace prompt, mode=%s", model.mode)
	}
	if !model.newWorkspaceCardSelected {
		t.Fatal("escaping the workspace prompt should return to the new workspace card")
	}
	got = ansi.Strip(model.View())
	if !strings.Contains(got, "No workspace selected") || !strings.Contains(got, "No task selected") || strings.Contains(got, "alpha") || strings.Contains(got, "last task output") {
		t.Fatalf("escaping back to the new workspace card should keep Tasks and preview empty:\n%s", got)
	}
}

func TestClientHoverSelectsNewWorkspaceCard(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	st := testStateWithAgent(rt.Workspace)
	st.Focus = state.FocusAgents
	st.NavOpen = true
	model := NewClientModel(rt, cfg)
	model.width = 120
	model.height = 16
	model.snapshot = ipc.Snapshot{
		State:        st,
		CodexTitle:   "alpha",
		CodexContent: "last task output",
		NavWidth:     workspaceNavFrameWidth(st, model.width),
	}
	area, ok := model.newWorkspaceCardArea()
	if !ok {
		t.Fatal("expected new workspace card hit area")
	}

	updated, cmd := model.handleMouse(tea.MouseMsg{
		X:      area.x + 1,
		Y:      area.y + 1,
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionMotion,
	})
	model = updated.(ClientModel)

	if cmd == nil {
		t.Fatal("hovering the new workspace card should focus the Workspaces pane")
	}
	if !model.newWorkspaceCardSelected || model.snapshot.State.Focus != state.FocusWorkspaces {
		t.Fatalf("hover should select the new workspace card, selected=%t focus=%s", model.newWorkspaceCardSelected, model.snapshot.State.Focus)
	}
	got := ansi.Strip(model.View())
	if !strings.Contains(got, "No workspace selected") || !strings.Contains(got, "No task selected") || strings.Contains(got, "alpha") || strings.Contains(got, "last task output") {
		t.Fatalf("hovering the new workspace card should empty the Tasks pane and preview:\n%s", got)
	}

	updated, cmd = model.handleMouse(tea.MouseMsg{
		X:      area.x,
		Y:      area.y + area.height + 1,
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionMotion,
	})
	model = updated.(ClientModel)

	if cmd != nil {
		t.Fatal("leaving the new workspace card should not call the supervisor")
	}
	if model.newWorkspaceCardSelected {
		t.Fatal("leaving the new workspace card should restore the real workspace selection")
	}
	got = ansi.Strip(model.View())
	if !strings.Contains(got, "alpha") || strings.Contains(got, "No workspace selected") {
		t.Fatalf("leaving hover should restore the real workspace Tasks pane:\n%s", got)
	}
}

func TestClientDownFromLastWorkspaceSelectsNewWorkspaceCard(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	st := testStateWithWorkspace(t, rt.Workspace)
	st.Focus = state.FocusWorkspaces
	st.NavOpen = true
	model := NewClientModel(rt, cfg)
	model.width = 120
	model.height = 16
	model.snapshot = ipc.Snapshot{
		State:        st,
		CodexTitle:   "Codex",
		CodexContent: "No task open.",
		NavWidth:     workspaceNavFrameWidth(st, model.width),
	}

	updated, cmd := model.handleNavKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(ClientModel)

	if cmd != nil {
		t.Fatal("down from the last workspace should select the local new workspace card")
	}
	if !model.newWorkspaceCardSelected {
		t.Fatal("down from the last workspace should select the new workspace card")
	}
}

func assertStyleRGB(t *testing.T, got color.Color, want color.RGBA) {
	t.Helper()
	if got == nil {
		t.Fatalf("color = nil, want %#v", want)
	}
	r, g, b, a := got.RGBA()
	actual := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
	if actual != want {
		t.Fatalf("color = %#v, want %#v", actual, want)
	}
}
