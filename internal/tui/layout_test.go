package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/codux/internal/config"
	"github.com/edwmurph/codux/internal/state"
	"github.com/muesli/termenv"
)

func TestNavFrameHeightStaysBounded(t *testing.T) {
	if got := navFrameHeight(30, 20); got > 10 {
		t.Fatalf("height = %d", got)
	}
	if got := navFrameHeight(12, 0); got < 3 {
		t.Fatalf("height = %d", got)
	}
}

func TestRenderWorkspaceKeepsNavAndCodexFrames(t *testing.T) {
	cfg := config.DefaultConfig("codux-test")
	st := state.State{
		Version: state.Version, ActiveTabID: "a", Focus: state.FocusNav,
		Tabs: []state.Tab{{ID: "a", Title: "alpha", Column: "inbox"}},
	}

	got := renderWorkspace(cfg, st, "alpha", "output", 80, 24, "", "/tmp/project")

	for _, expected := range []string{
		"CODUX",
		"←↑↓→ select",
		"S-←/→ move",
		"Enter",
		"n new",
		"c close",
		"C-c close codux",
		"alpha",
		"output",
		"╭─",
		"─╮",
		"╰─",
		"─╯",
		"│",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("workspace missing %q:\n%s", expected, got)
		}
	}
	lines := strings.Split(got, "\n")
	joinLine := -1
	for index, line := range lines {
		if strings.Contains(line, "output") {
			joinLine = index
			break
		}
	}
	if joinLine <= 0 || !strings.HasPrefix(lines[joinLine-1], "│") || !strings.HasPrefix(lines[joinLine], "╭") {
		t.Fatalf("nav vertical side should stop directly above codex rounded top border:\n%s", got)
	}
	if !strings.Contains(lines[joinLine], "╭─ output") {
		t.Fatalf("codex content should have one left padding column:\n%s", got)
	}
	if strings.Contains(got, "├─") || strings.Contains(got, "─┤") {
		t.Fatalf("stacked panes should not use shared sideways T connectors:\n%s", got)
	}
	if strings.HasPrefix(lines[joinLine-1], "╰") {
		t.Fatalf("nav should not render its own rounded bottom border above codex:\n%s", got)
	}
	if strings.Contains(got, "> alpha") {
		t.Fatalf("workspace should use old active-tab highlight without marker:\n%s", got)
	}
}

func TestCodexLeftPaddingStaysInsideFrame(t *testing.T) {
	cfg := config.DefaultConfig("codux-test")
	st := state.State{
		Version: state.Version, ActiveTabID: "a", Focus: state.FocusNav,
		Tabs: []state.Tab{{ID: "a", Title: "alpha", Column: "inbox"}},
	}

	got := renderWorkspace(cfg, st, "alpha", "top\nnext", 80, 24, "", "/tmp/project")
	lines := strings.Split(ansi.Strip(got), "\n")
	for _, line := range lines {
		if strings.Contains(line, "next") {
			if !strings.HasPrefix(line, "│ next") {
				t.Fatalf("codex content should start one column inside the left border:\n%s", got)
			}
			return
		}
	}
	t.Fatalf("missing codex content line:\n%s", got)
}

func TestRenderWorkspaceEmptyDashboardShowsNewHint(t *testing.T) {
	cfg := config.DefaultConfig("codux-test")
	st := state.State{Version: state.Version, Focus: state.FocusNav}

	got := renderWorkspace(cfg, st, "Codex", "No Codex tabs open.", 80, 24, "", "/tmp/project")

	if !strings.Contains(got, "Press n to create one.") {
		t.Fatalf("workspace missing empty hint:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	if strings.Contains(lines[len(lines)-1], "Codex") {
		t.Fatalf("empty dashboard should not render default codex title in bottom border:\n%s", got)
	}
}

func TestRenderWorkspaceLoadingStateIsCentered(t *testing.T) {
	cfg := config.DefaultConfig("codux-test")
	st := state.State{
		Version: state.Version, ActiveTabID: "a", Focus: state.FocusCodex,
		Tabs: []state.Tab{{ID: "a", Title: "alpha", Column: "inbox", Status: state.StatusStarting}},
	}

	got := renderLoadingWorkspaceWithNavHeight(cfg, st, "alpha", "⠋ Starting Codex", 80, 24, "", "/tmp/project", 0)
	lines := strings.Split(ansi.Strip(got), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Starting Codex") {
			if strings.HasPrefix(line, "│ ⠋ Starting Codex") {
				t.Fatalf("loading state should not render against the left edge:\n%s", got)
			}
			if !strings.Contains(line, "                              ⠋ Starting Codex") {
				t.Fatalf("loading state should be visually centered, got line %q:\n%s", line, got)
			}
			return
		}
	}
	t.Fatalf("missing loading state:\n%s", got)
}

func TestActiveCodexFooterDoesNotRenderDotIndicator(t *testing.T) {
	cfg := config.DefaultConfig("codux-test")
	st := state.State{
		Version: state.Version, ActiveTabID: "a", Focus: state.FocusCodex,
		Tabs: []state.Tab{{ID: "a", Title: "alpha", Column: "inbox"}},
	}

	got := renderWorkspaceWithNavHeight(cfg, st, "alpha", "output", 80, 24, "", "/tmp/project", 0)

	if strings.Contains(got, "●") {
		t.Fatalf("active dot indicator should not render:\n%s", got)
	}
	if !strings.Contains(got, "CODUX  C-g focus nav  C-c interrupt/close") {
		t.Fatalf("collapsed codex top toolbar missing focus shortcuts:\n%s", got)
	}
	if !strings.Contains(got, "/tmp/project") {
		t.Fatalf("collapsed codex top toolbar should keep workdir visible:\n%s", got)
	}
	if count := strings.Count(got, "C-c interrupt/close"); count != 1 {
		t.Fatalf("collapsed codex should render shortcuts only once, got %d:\n%s", count, got)
	}
	if !strings.Contains(got, "alpha") {
		t.Fatalf("workspace missing title:\n%s", got)
	}
}

func TestFocusedCodexAnimationFrameDoesNotRenderBottomShortcut(t *testing.T) {
	cfg := config.DefaultConfig("codux-test")
	st := state.State{
		Version: state.Version, ActiveTabID: "a", Focus: state.FocusCodex,
		Tabs: []state.Tab{{ID: "a", Title: "alpha", Column: "inbox"}},
	}

	got := renderWorkspaceWithNavHeight(cfg, st, "alpha", "output", 80, 24, "", "/tmp/project", 3)

	if strings.Contains(got, "C-c interrupt/close") {
		t.Fatalf("focused codex animation frame should not flash bottom shortcut:\n%s", got)
	}
	if !strings.Contains(got, "/tmp/project") {
		t.Fatalf("visible nav frame should keep workdir during codex focus animation:\n%s", got)
	}
}

func TestCodexLeftPaddingStaysBeforeLeadingANSIStyle(t *testing.T) {
	cfg := config.DefaultConfig("codux-test")
	st := state.State{
		Version: state.Version, ActiveTabID: "a", Focus: state.FocusCodex,
		Tabs: []state.Tab{{ID: "a", Title: "alpha", Column: "inbox"}},
	}

	got := renderWorkspaceWithNavHeight(cfg, st, "alpha", "\x1b[48;2;1;2;3mZ\x1b[m", 40, 8, "", "/tmp/project", 0)

	if !strings.Contains(got, "│ \x1b[48;2;1;2;3mZ") {
		t.Fatalf("padding should render before Codex ANSI styling:\n%q", got)
	}
	for _, line := range strings.Split(ansi.Strip(got), "\n") {
		if strings.Contains(line, "Z") {
			if !strings.HasPrefix(line, "│ Z") {
				t.Fatalf("padding should add one visible column, got %q", line)
			}
			return
		}
	}
	t.Fatalf("missing styled content line:\n%q", got)
}

func TestNavStopsAboveCodexRoundedTopWithSeparateFocusColors(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	cfg := config.DefaultConfig("codux-test")
	st := state.State{
		Version: state.Version, ActiveTabID: "a", Focus: state.FocusNav,
		Tabs: []state.Tab{{ID: "a", Title: "alpha", Column: "inbox"}},
	}

	got := renderWorkspace(cfg, st, "alpha", "output", 80, 24, "", "/tmp/project")

	rawLines := strings.Split(got, "\n")
	strippedLines := strings.Split(ansi.Strip(got), "\n")
	codexTopLine := -1
	for index, line := range strippedLines {
		if strings.Contains(line, "output") {
			codexTopLine = index
			break
		}
	}
	if codexTopLine <= 0 {
		t.Fatalf("missing stacked codex top line:\n%s", ansi.Strip(got))
	}

	wantNavSidePrefix := activePalette.border.Render(borderVertical)
	if !strings.HasPrefix(rawLines[codexTopLine-1], wantNavSidePrefix) {
		t.Fatalf("last nav row should keep nav active side color:\nwant prefix %q\ngot         %q", wantNavSidePrefix, rawLines[codexTopLine-1])
	}
	wantCodexTopPrefix := inactivePalette.border.Render(borderTopLeft + borderHorizontal)
	if !strings.HasPrefix(rawLines[codexTopLine], wantCodexTopPrefix) {
		t.Fatalf("codex top border should start with inactive codex rounded top color:\nwant prefix %q\ngot         %q", wantCodexTopPrefix, rawLines[codexTopLine])
	}
}

func TestCornerLinePreservesTinyWidths(t *testing.T) {
	cases := map[int]string{
		0: "╭╮",
		1: "╭─╮",
		2: "╭──╮",
	}
	for innerWidth, want := range cases {
		if got := cornerLine(borderTopLeft, borderTopRight, "", innerWidth); got != want {
			t.Fatalf("cornerLine(%d) = %q, want %q", innerWidth, got, want)
		}
	}
}
