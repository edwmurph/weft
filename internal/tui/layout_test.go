package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
	"github.com/muesli/termenv"
)

func TestWorkspaceNavWidthShrinksWorkspacesFirst(t *testing.T) {
	st := layoutState("/tmp/project")
	if got := workspaceNavFrameWidth(st, 140); got != fixedWorkdirPaneWidth+defaultAgentsPaneWidth {
		t.Fatalf("wide nav width = %d", got)
	}
	if got := workspaceNavFrameWidth(st, minThreePaneWidth); got != minTwoPaneNavWidth {
		t.Fatalf("minimum three-pane nav width = %d", got)
	}
	if got := workspaceNavFrameWidth(st, 100); got != 100 {
		t.Fatalf("medium nav width = %d, want nav-only command center", got)
	}
	if got := workspaceNavFrameWidth(st, 70); got != 42 {
		t.Fatalf("narrow nav width = %d", got)
	}
	st.NavOpen = false
	if got := workspaceNavFrameWidth(st, 140); got != 0 {
		t.Fatalf("collapsed nav width = %d", got)
	}
}

func TestRenderWorkspaceShowsWorkspacesAgentsAndConsole(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	cfg.TitleTemplate = "{title}"
	st := layoutState("/tmp/project")

	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 140, 24, "", minTwoPaneNavWidth, 1)

	for _, expected := range []string{
		"Workspaces",
		"Agents",
		"Console",
		"▾ inbox",
		"╭ /tmp/project",
		"1 total",
		"0 active",
		"1 needs attention",
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
	if strings.Contains(got, "ready") {
		t.Fatalf("agent rows should not render fixed status tags unless template asks for them:\n%s", got)
	}
}

func TestRenderWorkspaceShowsAllPanesAtWideTerminalWidth(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	workdir := filepath.Join(home, "code", "personal", "weft", ".worktrees", "ideal-architecture")
	expectedPath := "~" + strings.TrimPrefix(workdir, home)
	st := layoutState(workdir)

	got := renderWorkspace(cfg, st, "Codex", "No Codex agent open.", minThreePaneWidth, 24, "", workdir)

	for _, expected := range []string{"Workspaces", "Agents", "No Codex agent open", expectedPath} {
		if !strings.Contains(got, expected) {
			t.Fatalf("wide dashboard missing %q:\n%s", expected, got)
		}
	}
}

func TestRenderWorkspaceKeepsFixedWorkdirPaneAtMediumWidth(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	workdir := filepath.Join(home, "code", "personal", "weft", ".worktrees", "ideal-architecture")
	expectedPath := "~" + strings.TrimPrefix(workdir, home)
	st := layoutState(workdir)

	got := renderWorkspace(cfg, st, "Codex", "No Codex agent open.", 100, 24, "", workdir)

	for _, expected := range []string{"Workspaces", "Agents", expectedPath} {
		if !strings.Contains(got, expected) {
			t.Fatalf("medium dashboard missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "No Codex agent open") {
		t.Fatalf("medium dashboard should hide Codex preview before clipping fixed Workdirs:\n%s", got)
	}
}

func TestRenderWorkdirCardsUseDefaultPathAndTitleOverride(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig("weft-test")
	st := layoutState(filepath.Join(home, "code", "personal", "weft"))
	st.Focus = state.FocusWorkdirs

	got := ansi.Strip(strings.Join(renderWorkdirsPane(cfg, st, 78, 8), "\n"))
	if !strings.Contains(got, "~/code/personal/weft") {
		t.Fatalf("workdir card should use default display path title:\n%s", got)
	}

	st.Workdirs[0].Title = "Main Weft"
	got = ansi.Strip(strings.Join(renderWorkdirsPane(cfg, st, 78, 8), "\n"))
	if !strings.Contains(got, "Main Weft") || strings.Contains(got, "~/code/personal/weft") {
		t.Fatalf("workdir card should use manual title override:\n%s", got)
	}
}

func TestRenderWorkdirCardsShowOnlyReconciledCounts(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	now := state.NowISO()
	st := state.State{
		Version:           state.Version,
		SelectedWorkdirID: "w",
		Focus:             state.FocusWorkdirs,
		NavOpen:           true,
		Workdirs:          []state.Workdir{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Agents: []state.Agent{
			{ID: "starting", WorkdirID: "w", Title: "Starting", Status: state.StatusStarting, CreatedAt: now, UpdatedAt: now},
			{ID: "running", WorkdirID: "w", Title: "Running", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now},
			{ID: "working", WorkdirID: "w", Title: "Working", Status: state.StatusRunning, CodexTitle: "Codex Working", CreatedAt: now, UpdatedAt: now},
			{ID: "shipping", WorkdirID: "w", Title: "Shipping", Status: state.StatusShipping, CreatedAt: now, UpdatedAt: now},
			{ID: "ready", WorkdirID: "w", Title: "Ready", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "live-ready", WorkdirID: "w", Title: "Live Ready", Status: state.StatusRunning, CodexTitle: "Codex Ready", CreatedAt: now, UpdatedAt: now},
			{ID: "failed", WorkdirID: "w", Title: "Failed", Status: state.StatusError, CreatedAt: now, UpdatedAt: now},
		},
	}

	counts := workdirCardCountsForWorkdir(st, "w")
	if counts.total != 7 || counts.active != 4 || counts.needsAttention != 3 {
		t.Fatalf("counts = %#v", counts)
	}
	if counts.active+counts.needsAttention != counts.total {
		t.Fatalf("counts should reconcile: %#v", counts)
	}
	got := strings.ToLower(ansi.Strip(strings.Join(renderWorkdirsPane(cfg, st, 78, 8), "\n")))
	for _, expected := range []string{"7 total", "4 active", "3 needs attention", "3 needs attention │", "╭ /tmp/project", "│", "╰"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("workdir card missing %q:\n%s", expected, got)
		}
	}
	for _, forbidden := range []string{"parked", "stopped", "quiet", "error"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("workdir card should not render %q label:\n%s", forbidden, got)
		}
	}
}

func TestRenderWorkdirCardCountsColorOnlyNonzeroValues(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	zeroActive := renderWorkdirCardCounts(workdirCardCounts{total: 1, active: 0, needsAttention: 1}, 72)
	if !strings.Contains(zeroActive, workdirCountMutedStyle.Render("0 active")) {
		t.Fatalf("zero active should use muted style:\n%q", zeroActive)
	}
	if strings.Contains(zeroActive, workdirCountActiveStyle.Render("0 active")) {
		t.Fatalf("zero active should not use active color:\n%q", zeroActive)
	}
	if !strings.Contains(zeroActive, workdirCountNeedsAttentionStyle.Render("1 needs attention")) {
		t.Fatalf("nonzero needs attention should use amber style:\n%q", zeroActive)
	}

	zeroNeedsAttention := renderWorkdirCardCounts(workdirCardCounts{total: 1, active: 1, needsAttention: 0}, 72)
	if !strings.Contains(zeroNeedsAttention, workdirCountActiveStyle.Render("1 active")) {
		t.Fatalf("nonzero active should use active color:\n%q", zeroNeedsAttention)
	}
	if !strings.Contains(zeroNeedsAttention, workdirCountMutedStyle.Render("0 needs attention")) {
		t.Fatalf("zero needs attention should use muted style:\n%q", zeroNeedsAttention)
	}
	if strings.Contains(zeroNeedsAttention, workdirCountNeedsAttentionStyle.Render("0 needs attention")) {
		t.Fatalf("zero needs attention should not use amber color:\n%q", zeroNeedsAttention)
	}
}

func TestRenderWorkspaceFallsBackToSingleNavPane(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	st := layoutState("/tmp/project")
	st.Focus = state.FocusWorkdirs

	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 70, 16, "", 32, 0)

	if !strings.Contains(got, "Workspaces") {
		t.Fatalf("narrow workspace focus should show workspaces pane:\n%s", got)
	}
	if strings.Contains(got, "Agents") {
		t.Fatalf("narrow nav should use one pane, got agents too:\n%s", got)
	}
}

func TestRenderAgentsPaneShowsTopLevelAgentsAndEmptyState(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	cfg.TitleTemplate = "{title}"
	st := layoutState("/tmp/project")
	st.SelectedFolderID = ""
	st.Folders = nil
	st.Agents[0].FolderID = ""

	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 100, 18, "", 60, 0)
	if !strings.Contains(got, "Agents") || !strings.Contains(got, "• alpha") || strings.Contains(got, "▾") {
		t.Fatalf("top-level agent rendering mismatch:\n%s", got)
	}

	st.Agents = nil
	st.ActiveAgentID = ""
	got = renderWorkspaceWithNavWidth(cfg, st, "Codex", "", 100, 18, "", 60, 0)
	if !strings.Contains(got, "No agents") || !strings.Contains(got, "Press n to create one.") {
		t.Fatalf("empty agents pane missing empty state:\n%s", got)
	}

	st = state.Repair(state.Empty(), "/tmp/project")
	got = strings.Join(renderFoldersPane(cfg, st, 40, 12, 0), "\n")
	if !strings.Contains(got, "No workspace selected") || !strings.Contains(got, "Press w to add one.") || strings.Contains(got, "Press n to create one.") {
		t.Fatalf("no-workspace agents pane should explain workspace requirement:\n%s", got)
	}
}

func TestRenderWorkspacesPaneEmptyStateIsCenteredHelp(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	st := state.Repair(state.Empty(), "/tmp/project")

	got := strings.Join(renderWorkdirsPane(cfg, st, 64, 12), "\n")

	if !strings.Contains(got, "No workspaces") || !strings.Contains(got, "Press w to add one.") {
		t.Fatalf("empty workspaces pane missing help:\n%s", got)
	}
	for _, line := range strings.Split(ansi.Strip(got), "\n") {
		if strings.Contains(line, "No workspaces") && strings.Trim(line, " │") != "No workspaces" {
			t.Fatalf("empty workspace help should be centered, got line %q\n%s", line, got)
		}
	}
}

func TestRenderWorkspaceEmptyCommandCenterShowsNewHint(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	st := state.Repair(state.Empty(), "/tmp/project")

	got := renderWorkspace(cfg, st, "Codex", "No Codex agent open.", 80, 24, "", "/tmp/project")

	if strings.Contains(got, "Press n to create one.") || !strings.Contains(got, "Add a workspace first.") {
		t.Fatalf("workspace should not advertise agent creation before a workspace exists:\n%s", got)
	}

	st = layoutState("/tmp/project")
	st.Agents = nil
	st.ActiveAgentID = ""
	st.Focus = state.FocusFolders
	st.NavOpen = true
	got = renderWorkspace(cfg, st, "Codex", "No Codex agent open.", 80, 24, "", "/tmp/project")
	if !strings.Contains(got, "Press n to create one.") {
		t.Fatalf("workspace missing agent creation hint:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	if strings.Contains(lines[len(lines)-1], "Codex") {
		t.Fatalf("empty command center should not render default codex title in bottom border:\n%s", got)
	}

	got = renderWorkspaceWithNavWidth(cfg, st, "Codex", "No Codex agent open.", 100, 24, "", 0, 0)
	stripped := ansi.Strip(got)
	logoIndex := strings.Index(stripped, `●──┼──▶      ██║ █╗ ██║ █████╗   █████╗      ██║`)
	hintIndex := strings.Index(stripped, "No Codex agent open")
	if logoIndex < 0 {
		t.Fatalf("empty command center missing Weft wordmark:\n%s", stripped)
	}
	if hintIndex < 0 || logoIndex > hintIndex {
		t.Fatalf("empty command center should render wordmark above existing hint:\n%s", stripped)
	}

	content := renderEmptyCodexContent(100, 24, true)
	logoWidth := maxVisualWidth(emptyWeftLogo)
	expectedLeft := strings.Repeat(" ", (100-logoWidth)/2)
	logoStart := -1
	for index, line := range content {
		if strings.Contains(ansi.Strip(line), "██╗    ██╗") {
			logoStart = index
			break
		}
	}
	if logoStart < 0 {
		t.Fatalf("empty content missing first logo row:\n%s", strings.Join(content, "\n"))
	}
	for index, want := range emptyWeftLogo {
		got := ansi.Strip(content[logoStart+index])
		if !strings.HasPrefix(got, expectedLeft+want) {
			t.Fatalf("logo row %d should preserve art spacing inside one centered block:\nwant prefix %q\ngot         %q", index, expectedLeft+want, got)
		}
	}
	if got := ansi.Strip(content[logoStart+len(emptyWeftLogo)+1]); !strings.HasPrefix(got, expectedLeft+centerVisual("No Codex agent open", logoWidth)) {
		t.Fatalf("empty title should align inside centered logo block, got %q", got)
	}
	if got := ansi.Strip(content[logoStart+len(emptyWeftLogo)+2]); !strings.HasPrefix(got, expectedLeft+centerVisual("Press n to create one.", logoWidth)) {
		t.Fatalf("empty hint should align inside centered logo block, got %q", got)
	}
}

func TestRenderWorkspaceLoadingStateIsCentered(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	st := layoutState("/tmp/project")
	st.Focus = state.FocusCodex
	st.NavOpen = false
	st.Agents[0].Status = state.StatusStarting

	got := renderLoadingWorkspaceWithNavWidth(cfg, st, "alpha", "⠋ Starting Codex", 80, 24, "", 0, 0)
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

func TestActiveCodexToolbarUsesDrawerBinding(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	st := layoutState("/tmp/project")
	st.Focus = state.FocusCodex
	st.NavOpen = false

	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 80, 24, "", 0, 0)

	if strings.Contains(got, "●") {
		t.Fatalf("active dot indicator should not render:\n%s", got)
	}
	if !strings.Contains(got, "WEFT  C-b command center  C-c to Codex") {
		t.Fatalf("collapsed codex top toolbar missing drawer shortcuts:\n%s", got)
	}
	if !strings.Contains(got, "Console") {
		t.Fatalf("codex pane should render Console title:\n%s", got)
	}
	if count := strings.Count(got, "C-c to Codex"); count != 1 {
		t.Fatalf("collapsed codex should render shortcuts only once, got %d:\n%s", count, got)
	}
}

func TestCodexLeftPaddingStaysBeforeLeadingANSIStyle(t *testing.T) {
	cfg := config.DefaultConfig("weft-test")
	st := layoutState("/tmp/project")
	st.Focus = state.FocusCodex
	st.NavOpen = false

	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", "\x1b[48;2;1;2;3mZ\x1b[m", 40, 8, "", 0, 0)

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

func TestFocusedCodexAndNavUseSeparateFocusColors(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	cfg := config.DefaultConfig("weft-test")
	st := layoutState("/tmp/project")
	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 100, 18, "", 60, 1)

	rawLines := strings.Split(got, "\n")
	strippedLines := strings.Split(ansi.Strip(got), "\n")
	foundCodex := false
	for index, line := range strippedLines {
		if strings.Contains(line, "output") && strings.Contains(rawLines[index], inactivePalette.border.Render(borderVertical)) {
			foundCodex = true
		}
	}
	if !foundCodex {
		t.Fatalf("expected inactive frame color for non-focused codex pane:\n%s", ansi.Strip(got))
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

func layoutState(workdir string) state.State {
	now := state.NowISO()
	return state.State{
		Version:           state.Version,
		ActiveAgentID:     "a",
		SelectedWorkdirID: "w",
		SelectedFolderID:  "f",
		Focus:             state.FocusFolders,
		NavOpen:           true,
		Workdirs:          []state.Workdir{{ID: "w", Path: workdir, CreatedAt: now, UpdatedAt: now}},
		Folders:           []state.Folder{{ID: "f", WorkdirID: "w", Path: "inbox", CreatedAt: now, UpdatedAt: now}},
		Agents:            []state.Agent{{ID: "a", WorkdirID: "w", FolderID: "f", Title: "alpha", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now}},
	}
}
