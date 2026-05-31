package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestCodexMouseInputArgsEncodesWheelForPTYCoordinates(t *testing.T) {
	args := codexMouseInputArgs(tea.MouseEvent{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
		Shift:  true,
	}, consolePoint{col: 2, row: 3})

	if args["input"] != "mouse" || args["encoded"] != "\x1b[<69;3;4M" {
		t.Fatalf("mouse args = %#v", args)
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
