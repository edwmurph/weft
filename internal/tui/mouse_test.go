package tui

import (
	"image/color"
	"strings"
	"testing"
	"time"

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

	if !strings.Contains(got, "  "+consoleSelectionANSIStart+"The dashboard frames a living") ||
		!strings.Contains(got, "\n  "+consoleSelectionANSIStart+"A task, a timer, in-between.") {
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

func TestCodexSelectionColumnOffsetByTaskType(t *testing.T) {
	tests := []struct {
		name     string
		taskID   string
		taskType string
		want     int
	}{
		{name: "terminal starts at zero", taskID: "shell", taskType: config.DefaultTaskTypeShell, want: 0},
		{name: "codex keeps shared margin", taskID: "codex", taskType: config.DefaultTaskTypeCodex, want: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := ClientModel{
				cfg:    config.DefaultConfig(),
				width:  80,
				height: 8,
				snapshot: ipc.Snapshot{
					State: state.State{
						Focus:        state.FocusConsole,
						ActiveTaskID: tt.taskID,
						Workspaces:   []state.Workspace{{ID: "w", Path: "/tmp/project"}},
						Tasks:        []state.Task{{ID: tt.taskID, WorkspaceID: "w", TypeID: tt.taskType}},
					},
					CodexPlainLines: []string{"        content"},
				},
			}

			if got := model.codexSelectionColumnOffset(); got != tt.want {
				t.Fatalf("selection offset = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPreviewPlainLinesUseProjectedShellPromptForCopy(t *testing.T) {
	st := layoutState("/tmp/project")
	st.Focus = state.FocusTasks
	st.NavOpen = true
	st.Tasks[0].TypeID = config.DefaultTaskTypeShell
	line := "0s 5:30:15 console-right-padding⟩ echo asdf" + strings.Repeat(" ", 60) + "± ● v0.20.3^0"
	model := ClientModel{
		cfg:    config.DefaultConfig(),
		width:  140,
		height: 10,
		snapshot: ipc.Snapshot{
			State:           st,
			NavWidth:        minTwoPaneNavWidth,
			GroupCursor:     2,
			CodexPlainLines: []string{line},
		},
	}

	got := strings.Join(model.codexPlainLines(), "\n")

	if !strings.Contains(got, "± ● v0.20.3^0") {
		t.Fatalf("preview copy lines should preserve the right prompt tail, got %q", got)
	}
	if strings.Contains(got, "…") {
		t.Fatalf("preview copy lines should not contain crop chrome, got %q", got)
	}
}

func TestSelectedCodexContentHighlightsDraggedCells(t *testing.T) {
	selection := consoleSelection{
		active: true,
		start:  consolePoint{col: 1, row: 0},
		end:    consolePoint{col: 3, row: 0},
	}

	got := selectedCodexContent([]string{"abcdef"}, selection, 6)

	if !strings.Contains(got, "a"+consoleSelectionANSIStart+"bcd"+consoleSelectionANSIEnd+"ef") {
		t.Fatalf("highlighted content = %q", got)
	}
}

func TestSelectedStyledCodexContentAppliesConsistentSelectionColors(t *testing.T) {
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
	assertStyleRGB(t, screen.cells[0][1].style.Fg, consoleSelectionForeground)
	assertStyleRGB(t, screen.cells[0][1].style.Bg, consoleSelectionBackground)
	if screen.cells[0][1].style.Attrs.Contains(cellbuf.ReverseAttr) {
		t.Fatalf("selected foreground-colored cell should not be reversed: %#v", screen.cells[0][1].style)
	}
	assertStyleRGB(t, screen.cells[0][4].style.Fg, consoleSelectionForeground)
	assertStyleRGB(t, screen.cells[0][4].style.Bg, consoleSelectionBackground)
	if screen.cells[0][4].style.Attrs.Contains(cellbuf.ReverseAttr) {
		t.Fatalf("selected background-colored cell should not be reversed: %#v", screen.cells[0][4].style)
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
				Focus:        state.FocusConsole,
				ActiveTaskID: "a",
				Workspaces:   []state.Workspace{{ID: "w", Path: "/tmp/project"}},
				Tasks:        []state.Task{{ID: "a", WorkspaceID: "w"}},
			},
			LiveTitle:       "Codex",
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

func TestClientMouseDragCopiesTaskNotesSelection(t *testing.T) {
	oldWriteClipboard := writeClipboard
	var copied string
	writeClipboard = func(value string) error {
		copied = value
		return nil
	}
	defer func() { writeClipboard = oldWriteClipboard }()

	model := ClientModel{
		cfg:    config.DefaultConfig(),
		width:  120,
		height: 34,
		mode:   modeCommand,
		snapshot: ipc.Snapshot{
			State: state.State{
				Focus:        state.FocusConsole,
				ActiveTaskID: "a",
				Workspaces:   []state.Workspace{{ID: "w", Path: "/tmp/project"}},
				Tasks:        []state.Task{{ID: "a", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex}},
			},
			ActiveTaskContext: &ipc.TaskContext{
				TaskID:  "a",
				Heading: "Waiting on CI",
				Detail:  "First detail line\nSecond detail line",
			},
		},
	}
	area, ok := model.taskPanelContextArea()
	if !ok {
		t.Fatal("expected task notes area")
	}

	updated, _ := model.handleMouse(tea.MouseMsg{
		X:      area.x,
		Y:      area.y + 2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)
	updated, _ = model.handleMouse(tea.MouseMsg{
		X:      area.x + len("Second detail line") - 1,
		Y:      area.y + 3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	model = updated.(ClientModel)
	if !model.mouseSelection.active {
		t.Fatal("task notes drag should keep a visible selection active before release")
	}
	if !strings.Contains(model.View(), consoleSelectionANSIStart) {
		t.Fatalf("task notes drag should render the selection highlight:\n%s", model.View())
	}
	updated, _ = model.handleMouse(tea.MouseMsg{
		X:      area.x + len("Second detail line") - 1,
		Y:      area.y + 3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	model = updated.(ClientModel)

	if copied != "First detail line\nSecond detail line" {
		t.Fatalf("copied = %q", copied)
	}
	if model.mode != modeCommand {
		t.Fatalf("task notes copy should keep notes open, mode=%s", model.mode)
	}
	if model.toastText != "Copied 36 characters" {
		t.Fatalf("toast = %q", model.toastText)
	}
	if model.mouseSelection.active {
		t.Fatal("selection should clear after copy")
	}
}

func TestClientMouseDragIgnoresTaskBriefConsoleCommands(t *testing.T) {
	oldWriteClipboard := writeClipboard
	var copied string
	writeClipboard = func(value string) error {
		copied = value
		return nil
	}
	defer func() { writeClipboard = oldWriteClipboard }()

	model := ClientModel{
		cfg:    config.DefaultConfig(),
		width:  120,
		height: 34,
		mode:   modeCommand,
		snapshot: ipc.Snapshot{
			State: state.State{
				Focus:        state.FocusConsole,
				ActiveTaskID: "a",
				Workspaces:   []state.Workspace{{ID: "w", Path: "/tmp/project"}},
				Tasks:        []state.Task{{ID: "a", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex}},
			},
			ActiveTaskContext: &ipc.TaskContext{TaskID: "a", Detail: "Notes only"},
		},
	}
	layout := model.taskPanelLayout()
	area, ok := model.taskPanelContextArea()
	if !ok {
		t.Fatal("expected task notes area")
	}
	shortcutX := area.x + layout.contextWidth + 2

	updated, _ := model.handleMouse(tea.MouseMsg{
		X:      shortcutX,
		Y:      area.y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)
	updated, _ = model.handleMouse(tea.MouseMsg{
		X:      shortcutX + 6,
		Y:      area.y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	model = updated.(ClientModel)

	if copied != "" {
		t.Fatalf("console command drag copied %q", copied)
	}
	if model.mouseSelection.active {
		t.Fatal("console command drag should not start a context selection")
	}
}

func TestClientMouseDragCopiesTaskPreviewSelection(t *testing.T) {
	oldWriteClipboard := writeClipboard
	var copied string
	writeClipboard = func(value string) error {
		copied = value
		return nil
	}
	defer func() { writeClipboard = oldWriteClipboard }()

	model := ClientModel{
		cfg:    config.DefaultConfig(),
		width:  140,
		height: 8,
		snapshot: ipc.Snapshot{
			State: state.State{
				Focus:               state.FocusTasks,
				NavOpen:             true,
				ActiveTaskID:        "a",
				SelectedTaskID:      "a",
				SelectedWorkspaceID: "w",
				Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project"}},
				Tasks:               []state.Task{{ID: "a", WorkspaceID: "w"}},
			},
			NavWidth:        minTwoPaneNavWidth,
			LiveTitle:       "Codex",
			CodexContent:    "alpha beta",
			CodexPlainLines: []string{"alpha beta"},
			CodexScrollback: "alpha beta",
			LoadingTaskIDs:  nil,
			CodexScrollbackLines: []string{
				"alpha beta",
			},
			GroupCursor: 1,
		},
	}
	area, ok := model.codexContentArea()
	if !ok {
		t.Fatal("expected task preview content area")
	}

	updated, cmd := model.handleMouse(tea.MouseMsg{
		X:      area.x + 6,
		Y:      area.y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)
	if cmd != nil {
		t.Fatal("preview selection should not send a focus request")
	}
	updated, _ = model.handleMouse(tea.MouseMsg{
		X:      area.x + 9,
		Y:      area.y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	model = updated.(ClientModel)
	if !model.mouseSelection.active {
		t.Fatal("preview drag should keep a visible selection active before release")
	}
	if !strings.Contains(model.View(), consoleSelectionANSIStart) {
		t.Fatalf("preview drag should render the selection highlight:\n%s", model.View())
	}
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
	if model.snapshot.State.Focus != state.FocusTasks || !model.snapshot.State.NavOpen {
		t.Fatalf("preview copy should keep dashboard focus/nav, got %s/%t", model.snapshot.State.Focus, model.snapshot.State.NavOpen)
	}
	if model.toastText != "Copied 4 characters" {
		t.Fatalf("toast = %q", model.toastText)
	}
	if !strings.Contains(model.View(), "Copied 4 characters") {
		t.Fatalf("preview copy toast did not render:\n%s", model.View())
	}
}

func TestClientMouseWheelScrollsConsoleScrollback(t *testing.T) {
	model := ClientModel{
		cfg:    config.DefaultConfig(),
		width:  80,
		height: 8,
		snapshot: ipc.Snapshot{
			State: state.State{
				Focus:        state.FocusConsole,
				ActiveTaskID: "a",
				Workspaces:   []state.Workspace{{ID: "w", Path: "/tmp/project"}},
				Tasks:        []state.Task{{ID: "a", WorkspaceID: "w", TypeID: config.DefaultTaskTypeShell}},
			},
			LiveTitle:            "Codex",
			CodexContent:         strings.Join([]string{"history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"}, "\n"),
			CodexPlainLines:      []string{"history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"},
			CodexScrollback:      strings.Join([]string{"history line 01", "history line 02", "history line 03", "history line 04", "history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"}, "\n"),
			CodexScrollbackLines: []string{"history line 01", "history line 02", "history line 03", "history line 04", "history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"},
			GroupCursor:          1,
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

func TestClientMouseWheelForwardsFocusedTerminalAlternateScreen(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	st := state.State{
		Focus:        state.FocusConsole,
		ActiveTaskID: "a",
		Workspaces:   []state.Workspace{{ID: "w", Path: rt.Workspace}},
		Tasks:        []state.Task{{ID: "a", WorkspaceID: "w", TypeID: config.DefaultTaskTypeShell}},
	}
	requests := make(chan ipc.Request, 1)
	stop, err := ipc.Serve(rt.SocketPath, func(request ipc.Request) ipc.Response {
		requests <- request
		snapshot := ipc.Snapshot{State: st, ActiveTaskInAlternateScreen: true}
		return ipc.Response{OK: true, Snapshot: &snapshot}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	model := ClientModel{
		cfg:      cfg,
		runtime:  rt,
		clientID: "client",
		width:    80,
		height:   8,
		snapshot: ipc.Snapshot{
			State:                       st,
			LiveTitle:                   "Shell",
			CodexContent:                "pager page 1",
			CodexPlainLines:             []string{"pager page 1"},
			CodexScrollback:             "pager page 1",
			CodexScrollbackLines:        []string{"pager page 1"},
			ActiveTaskInAlternateScreen: true,
		},
	}
	area, ok := model.codexFrameArea()
	if !ok {
		t.Fatal("expected codex frame area")
	}

	updated, cmd := model.handleMouse(tea.MouseMsg{
		X:      area.x + 6,
		Y:      area.y + 6,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)

	if cmd == nil {
		t.Fatal("alternate-screen terminal wheel should forward input")
	}
	if model.codexScrollOffset != 0 {
		t.Fatalf("forwarded wheel should not move Weft scrollback, got offset %d", model.codexScrollOffset)
	}
	if response, ok := cmd().(clientResponseMsg); !ok || response.err != nil {
		t.Fatalf("client command response = %#v", response)
	}
	select {
	case request := <-requests:
		if request.Command != "task_input" {
			t.Fatalf("request command = %q, want task_input", request.Command)
		}
		if got, want := request.Args["encoded"], "\x1b[<64;7;7M"; got != want {
			t.Fatalf("encoded wheel = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for forwarded wheel request")
	}
}

func TestTerminalMouseWheelInputEncodesSGRWheel(t *testing.T) {
	got, ok := terminalMouseWheelInput(tea.MouseEvent{
		X:      11,
		Y:      4,
		Ctrl:   true,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	if !ok {
		t.Fatal("expected encoded terminal wheel input")
	}
	if want := "\x1b[<81;12;5M"; got != want {
		t.Fatalf("encoded terminal wheel = %q, want %q", got, want)
	}
}

func TestClientMouseWheelScrollsTaskPreviewScrollback(t *testing.T) {
	model := ClientModel{
		cfg:    config.DefaultConfig(),
		width:  140,
		height: 8,
		snapshot: ipc.Snapshot{
			State: state.State{
				Focus:               state.FocusTasks,
				NavOpen:             true,
				ActiveTaskID:        "a",
				SelectedTaskID:      "a",
				SelectedWorkspaceID: "w",
				Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project"}},
				Tasks:               []state.Task{{ID: "a", WorkspaceID: "w", TypeID: config.DefaultTaskTypeShell}},
			},
			NavWidth:                    minTwoPaneNavWidth,
			LiveTitle:                   "Codex",
			CodexContent:                strings.Join([]string{"history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"}, "\n"),
			CodexPlainLines:             []string{"history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"},
			CodexScrollback:             strings.Join([]string{"history line 01", "history line 02", "history line 03", "history line 04", "history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"}, "\n"),
			CodexScrollbackLines:        []string{"history line 01", "history line 02", "history line 03", "history line 04", "history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"},
			GroupCursor:                 1,
			ActiveTaskInAlternateScreen: true,
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
		t.Fatal("preview mouse wheel should not forward input to Codex")
	}
	if model.codexScrollOffset != 3 {
		t.Fatalf("scroll offset = %d, want 3", model.codexScrollOffset)
	}
	view := model.View()
	if !strings.Contains(view, "history line 02") || strings.Contains(view, "history line 10") {
		t.Fatalf("preview should show older scrollback after wheel input:\n%s", view)
	}
	if model.snapshot.State.Focus != state.FocusTasks || !model.snapshot.State.NavOpen {
		t.Fatalf("preview wheel should keep dashboard focus/nav, got %s/%t", model.snapshot.State.Focus, model.snapshot.State.NavOpen)
	}

	updated, _ = model.handleMouse(tea.MouseMsg{
		X:      area.x,
		Y:      area.y,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)
	if model.codexScrollOffset != 0 {
		t.Fatalf("scroll offset after preview wheel down = %d, want 0", model.codexScrollOffset)
	}
}

func TestClientMouseWheelIgnoredWhenTaskPreviewHasNoSelectedTask(t *testing.T) {
	model := ClientModel{
		cfg:    config.DefaultConfig(),
		width:  140,
		height: 8,
		snapshot: ipc.Snapshot{
			State: state.State{
				Focus:               state.FocusWorkspaces,
				NavOpen:             true,
				ActiveTaskID:        "a",
				SelectedTaskID:      "",
				SelectedWorkspaceID: "w",
				Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project"}},
				Tasks:               []state.Task{{ID: "a", WorkspaceID: "w", TypeID: config.DefaultTaskTypeShell}},
			},
			NavWidth:             minTwoPaneNavWidth,
			LiveTitle:            "Codex",
			CodexContent:         strings.Join([]string{"history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"}, "\n"),
			CodexPlainLines:      []string{"history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"},
			CodexScrollback:      strings.Join([]string{"history line 01", "history line 02", "history line 03", "history line 04", "history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"}, "\n"),
			CodexScrollbackLines: []string{"history line 01", "history line 02", "history line 03", "history line 04", "history line 05", "history line 06", "history line 07", "history line 08", "history line 09", "history line 10"},
			GroupCursor:          1,
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
		t.Fatal("preview mouse wheel without selected task should not forward input to Codex")
	}
	if model.codexScrollOffset != 0 {
		t.Fatalf("scroll offset = %d, want 0", model.codexScrollOffset)
	}
	if view := model.View(); !strings.Contains(view, "No task selected") || strings.Contains(view, "history line 02") {
		t.Fatalf("non-task preview wheel should keep the empty preview state:\n%s", view)
	}
}

func TestClientMouseIgnoresNewWorkspaceCard(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	st := testStateWithTask(rt.Workspace)
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, cfg)
	model.width = 120
	model.height = 16
	model.snapshot = ipc.Snapshot{
		State:        st,
		LiveTitle:    "alpha",
		CodexContent: "last task output",
		NavWidth:     workspaceNavFrameWidth(st, model.width),
	}
	area, ok := model.newWorkspaceCardArea()
	if !ok {
		t.Fatal("expected new workspace card hit area")
	}
	originalFocus := model.snapshot.State.Focus
	originalWorkspace := model.snapshot.State.SelectedWorkspaceID

	updated, cmd := model.handleMouse(tea.MouseMsg{
		X:      area.x + 1,
		Y:      area.y + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)

	if cmd != nil {
		t.Fatal("clicking the new workspace card should not call the supervisor")
	}
	if model.newWorkspaceCardSelected || model.snapshot.State.Focus != originalFocus || model.snapshot.State.SelectedWorkspaceID != originalWorkspace {
		t.Fatalf("click should leave dashboard selection unchanged, selected=%t focus=%s workspace=%q", model.newWorkspaceCardSelected, model.snapshot.State.Focus, model.snapshot.State.SelectedWorkspaceID)
	}
	updated, cmd = model.handleMouse(tea.MouseMsg{
		X:      area.x + 1,
		Y:      area.y + 1,
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionMotion,
	})
	model = updated.(ClientModel)
	if cmd != nil {
		t.Fatal("moving over the new workspace card should not call the supervisor")
	}
	if model.newWorkspaceCardSelected || model.snapshot.State.Focus != originalFocus || model.snapshot.State.SelectedWorkspaceID != originalWorkspace {
		t.Fatalf("motion should leave dashboard selection unchanged, selected=%t focus=%s workspace=%q", model.newWorkspaceCardSelected, model.snapshot.State.Focus, model.snapshot.State.SelectedWorkspaceID)
	}
	got := ansi.Strip(model.View())
	if !strings.Contains(got, "alpha") || strings.Contains(got, "No workspace selected") {
		t.Fatalf("mouse input should not empty the Tasks pane or preview:\n%s", got)
	}
}

func TestClientKeyboardNewWorkspaceCardEnterOpensPrompt(t *testing.T) {
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
		LiveTitle:    "Codex",
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

func TestClientMouseIgnoresNewTaskRow(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	st := testStateWithWorkspace(t, rt.Workspace)
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, cfg)
	model.width = 120
	model.height = 16
	model.snapshot = ipc.Snapshot{
		State:        st,
		LiveTitle:    "Codex",
		CodexContent: "No task open.",
		NavWidth:     workspaceNavFrameWidth(st, model.width),
	}
	area, ok := model.newTaskRowArea()
	if !ok {
		t.Fatal("expected new task row hit area")
	}

	updated, cmd := model.handleMouse(tea.MouseMsg{
		X:      area.x + 1,
		Y:      area.y,
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionMotion,
	})
	model = updated.(ClientModel)

	if cmd != nil {
		t.Fatal("moving over the new task row should not call the supervisor")
	}
	if model.newTaskRowSelected || model.snapshot.State.Focus != state.FocusTasks || model.snapshot.GroupCursor != 0 {
		t.Fatalf("motion should leave new task row state unchanged, selected=%t focus=%s cursor=%d", model.newTaskRowSelected, model.snapshot.State.Focus, model.snapshot.GroupCursor)
	}
	updated, cmd = model.handleMouse(tea.MouseMsg{
		X:      area.x + 1,
		Y:      area.y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(ClientModel)
	if cmd != nil {
		t.Fatal("clicking the new task row should not call the supervisor")
	}
	if model.newTaskRowSelected || model.snapshot.State.Focus != state.FocusTasks || model.snapshot.GroupCursor != 0 {
		t.Fatalf("click should leave new task row state unchanged, selected=%t focus=%s cursor=%d", model.newTaskRowSelected, model.snapshot.State.Focus, model.snapshot.GroupCursor)
	}
}

func TestClientKeyboardNewTaskRowEnterOpensMenu(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig()
	st := testStateWithWorkspace(t, rt.Workspace)
	st.Focus = state.FocusTasks
	st.NavOpen = true
	model := NewClientModel(rt, cfg)
	model.width = 120
	model.height = 16
	model.snapshot = ipc.Snapshot{
		State:        st,
		LiveTitle:    "Task",
		CodexContent: "No task open.",
		NavWidth:     workspaceNavFrameWidth(st, model.width),
	}
	got := ansi.Strip(model.View())
	if !strings.Contains(got, "+ New task") || !strings.Contains(got, "Press n to create") {
		t.Fatalf("new task row should be visible and actionable:\n%s", got)
	}

	updated, cmd := model.handleNavKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ClientModel)

	if cmd != nil {
		t.Fatal("enter on the new task row should open the local task menu")
	}
	if model.mode != modeNewTask {
		t.Fatalf("enter should open new task menu, mode=%s", model.mode)
	}
	if model.newTaskField != 0 || model.input.Focused() {
		t.Fatalf("new task menu should start on type field, field=%d input focused=%t", model.newTaskField, model.input.Focused())
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
