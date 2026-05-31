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
	weftversion "github.com/edwmurph/weft/internal/version"
	"github.com/muesli/termenv"
)

func TestWorkspaceNavWidthShrinksWorkspacesFirst(t *testing.T) {
	st := layoutState("/tmp/project")
	if got := workspaceNavFrameWidth(st, 140); got != fixedWorkspacePaneWidth+defaultAgentsPaneWidth {
		t.Fatalf("wide nav width = %d", got)
	}
	if got := workspaceNavFrameWidth(st, minThreePaneWidth); got != minTwoPaneNavWidth {
		t.Fatalf("minimum three-pane nav width = %d", got)
	}
	if got := workspaceNavFrameWidth(st, 100); got != 100 {
		t.Fatalf("medium nav width = %d, want nav-only dashboard", got)
	}
	if got := workspaceNavFrameWidth(st, 70); got != 42 {
		t.Fatalf("narrow nav width = %d", got)
	}
	st.NavOpen = false
	if got := workspaceNavFrameWidth(st, 140); got != 0 {
		t.Fatalf("collapsed nav width = %d", got)
	}
}

func TestWeftLogoGraphShape(t *testing.T) {
	if len(emptyWeftLogo) != 6 {
		t.Fatalf("logo height = %d, want 6", len(emptyWeftLogo))
	}
	joined := strings.Join(emptyWeftLogo, "\n")
	if got := strings.Count(joined, "●"); got != 3 {
		t.Fatalf("logo graph input count = %d, want 3:\n%s", got, joined)
	}
	if got := strings.Count(joined, "────▶"); got != 1 {
		t.Fatalf("logo graph output count = %d, want 1:\n%s", got, joined)
	}
	for index, wantPrefix := range []string{
		"●─────╮",
		"      │",
		"●─────┼────▶",
		"      │",
		"●─────╯",
		"       ",
	} {
		if !strings.HasPrefix(emptyWeftLogo[index], wantPrefix) {
			t.Fatalf("logo row %d prefix = %q, want %q", index, emptyWeftLogo[index], wantPrefix)
		}
	}
}

func TestRenderWorkspaceShowsWorkspacesAgentsAndAgentPreview(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TitleTemplate = "{title}"
	st := layoutState("/tmp/project")
	output := "output from a selected agent that is intentionally long enough to be cropped by the preview lens"

	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", output, 140, 24, "", minTwoPaneNavWidth, 1)

	for _, expected := range []string{
		"Workspaces",
		"Agents",
		"Agent Live Preview",
		"▾ inbox (1)",
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
	stripped := ansi.Strip(got)
	lines := strings.Split(stripped, "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "Agent Live Preview") || !strings.Contains(lines[0], "alpha") {
		t.Fatalf("preview top border should include pane title and agent title:\n%s", stripped)
	}
	if strings.Contains(lines[len(lines)-1], "alpha") {
		t.Fatalf("preview bottom border should not include agent title:\n%s", stripped)
	}
	if strings.Contains(stripped, "● Live") || strings.Contains(stripped, " Live─") {
		t.Fatalf("preview should not render live indicator text:\n%s", stripped)
	}
	if !strings.Contains(stripped, " … │") {
		t.Fatalf("wide preview should reserve a right-edge crop marker with right padding:\n%s", stripped)
	}
	if strings.Contains(stripped, "cropped by the preview lens…") {
		t.Fatalf("preview should not attach generic clipping ellipsis to agent text:\n%s", stripped)
	}
	if strings.Contains(got, "ready") {
		t.Fatalf("agent rows should not render fixed status tags unless template asks for them:\n%s", got)
	}
}

func TestRenderWorkspaceShowsAllPanesAtWideTerminalWidth(t *testing.T) {
	cfg := config.DefaultConfig()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(home, "code", "personal", "weft", ".worktrees", "ideal-architecture")
	expectedPath := "~" + strings.TrimPrefix(workspace, home)
	st := layoutState(workspace)

	got := renderWorkspace(cfg, st, "Codex", "No Codex agent open.", minThreePaneWidth, 24, "", workspace)

	for _, expected := range []string{"Workspaces", "Agents", "No Codex agent open", expectedPath} {
		if !strings.Contains(got, expected) {
			t.Fatalf("wide dashboard missing %q:\n%s", expected, got)
		}
	}
}

func TestRenderWorkspaceKeepsFixedWorkspacePaneAtMediumWidth(t *testing.T) {
	cfg := config.DefaultConfig()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(home, "code", "personal", "weft", ".worktrees", "ideal-architecture")
	expectedPath := "~" + strings.TrimPrefix(workspace, home)
	st := layoutState(workspace)

	got := renderWorkspace(cfg, st, "Codex", "No Codex agent open.", 100, 24, "", workspace)

	for _, expected := range []string{"Workspaces", "Agents", expectedPath} {
		if !strings.Contains(got, expected) {
			t.Fatalf("medium dashboard missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "No Codex agent open") || strings.Contains(got, "Agent Live Preview") {
		t.Fatalf("medium dashboard should hide Agent Live Preview before clipping fixed Workspaces:\n%s", got)
	}
}

func TestRenderWorkspaceCardsUseDefaultPathAndTitleOverride(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	st := layoutState(filepath.Join(home, "code", "personal", "weft"))
	st.Focus = state.FocusWorkspaces

	got := ansi.Strip(strings.Join(renderWorkspacesPane(cfg, st, 78, 8), "\n"))
	if !strings.Contains(got, "~/code/personal/weft") {
		t.Fatalf("workspace card should use default display path title:\n%s", got)
	}

	st.Workspaces[0].Title = "Main Weft"
	got = ansi.Strip(strings.Join(renderWorkspacesPane(cfg, st, 78, 8), "\n"))
	if !strings.Contains(got, "Main Weft") || strings.Contains(got, "~/code/personal/weft") {
		t.Fatalf("workspace card should use manual title override:\n%s", got)
	}
}

func TestRenderWorkspaceCardFlagsMissingPath(t *testing.T) {
	cfg := config.DefaultConfig()
	stalePath := filepath.Join(t.TempDir(), "stale-worktree")
	if err := os.Mkdir(stalePath, 0o700); err != nil {
		t.Fatal(err)
	}
	st := layoutState(stalePath)
	if err := os.Remove(stalePath); err != nil {
		t.Fatal(err)
	}

	got := ansi.Strip(strings.Join(renderWorkspacesPane(cfg, st, 78, 9), "\n"))
	if !strings.Contains(got, "path missing; press d to remove") {
		t.Fatalf("missing workspace path should be visible and actionable:\n%s", got)
	}
}

func TestRenderWorkspacesPaneShowsUpgradeFooterAtBottom(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")

	got := ansi.Strip(strings.Join(renderWorkspacesPaneWithOptions(cfg, st, 60, 12, workspaceRenderOptions{
		workspaceFooterText: "Upgrade ready: client 7.5.5, supervisor 7.4.0.\nPress U to upgrade and resume 1 idle agent.",
	}), "\n"))
	for _, expected := range []string{"Upgrade ready: client 7.5.5, supervisor 7.4.0.", "Press U to upgrade and resume 1 idle agent."} {
		if !strings.Contains(got, expected) {
			t.Fatalf("upgrade footer missing %q:\n%s", expected, got)
		}
	}
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[len(lines)-3], "Upgrade ready") || !strings.Contains(lines[len(lines)-2], "Press U") {
		t.Fatalf("upgrade footer should be pinned to pane bottom:\n%s", got)
	}
}

func TestRenderWorkspacesPaneShowsVersionHeaderAboveWorkspaceCards(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState(t.TempDir())

	got := ansi.Strip(strings.Join(renderWorkspacesPaneWithOptions(cfg, st, 60, 12, workspaceRenderOptions{
		workspaceInfoText: "Weft\nCLI        7.13.6\nSupervisor 7.13.6",
	}), "\n"))
	for _, expected := range []string{"Weft", "CLI        7.13.6", "Supervisor 7.13.6"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("version header missing %q:\n%s", expected, got)
		}
	}
	for _, expected := range []string{"┌", "┐", "└", "┘"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("version header box missing %q:\n%s", expected, got)
		}
	}
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[1], "┌") ||
		!strings.Contains(lines[2], "Weft") ||
		!strings.Contains(lines[3], "CLI        7.13.6") ||
		!strings.Contains(lines[4], "Supervisor 7.13.6") ||
		!strings.Contains(lines[5], "└") {
		t.Fatalf("version header should be pinned to pane top:\n%s", got)
	}
	if strings.TrimSpace(strings.Trim(lines[6], " │")) != "" {
		t.Fatalf("version header should leave one blank line before workspace cards:\n%s", got)
	}
	if !strings.Contains(lines[7], "╭") {
		t.Fatalf("workspace cards should render below version header spacer:\n%s", got)
	}
	brandLine := lines[2]
	if strings.Index(brandLine, "Weft") <= strings.Index(brandLine, "│")+1 {
		t.Fatalf("brand title should be horizontally centered in the header box:\n%s", got)
	}
}

func TestRenderWorkspacesPaneOmitsVersionHeaderWhenCardsFillPane(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState(t.TempDir())
	now := state.NowISO()
	for index := 1; index < 4; index++ {
		st.Workspaces = append(st.Workspaces, state.Workspace{
			ID:        fmtInt(index),
			Path:      t.TempDir(),
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	got := ansi.Strip(strings.Join(renderWorkspacesPaneWithOptions(cfg, st, 60, 12, workspaceRenderOptions{
		workspaceInfoText: "Weft\nCLI        7.13.6\nSupervisor 7.13.6",
	}), "\n"))
	if strings.Contains(got, "CLI        7.13.6") || strings.Contains(got, "Supervisor 7.13.6") {
		t.Fatalf("version header should not hide workspace cards:\n%s", got)
	}
}

func TestRenderWorkspaceCardsShowOnlyReconciledCounts(t *testing.T) {
	cfg := config.DefaultConfig()
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Agents: []state.Agent{
			{ID: "starting", WorkspaceID: "w", Title: "Starting", Status: state.StatusStarting, CreatedAt: now, UpdatedAt: now},
			{ID: "running", WorkspaceID: "w", Title: "Running", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now},
			{ID: "working", WorkspaceID: "w", Title: "Working", Status: state.StatusRunning, CodexTitle: "Codex Working", CreatedAt: now, UpdatedAt: now},
			{ID: "shipping", WorkspaceID: "w", Title: "Shipping", Status: state.StatusShipping, CreatedAt: now, UpdatedAt: now},
			{ID: "ready", WorkspaceID: "w", Title: "Ready", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "live-ready", WorkspaceID: "w", Title: "Live Ready", Status: state.StatusRunning, CodexTitle: "Codex Ready", CreatedAt: now, UpdatedAt: now},
			{ID: "failed", WorkspaceID: "w", Title: "Failed", Status: state.StatusError, CreatedAt: now, UpdatedAt: now},
			{ID: "killed", WorkspaceID: "w", Title: "Killed", Status: state.StatusKilled, CreatedAt: now, UpdatedAt: now},
		},
	}

	counts := workspaceCardCountsForWorkspace(st, "w")
	if counts.total != 8 {
		t.Fatalf("counts = %#v", counts)
	}
	if counts.active+counts.needsAttention+counts.silenced != counts.total {
		t.Fatalf("counts = %#v", counts)
	}
	got := strings.ToLower(ansi.Strip(strings.Join(renderWorkspacesPane(cfg, st, 78, 8), "\n")))
	for _, expected := range []string{"8 total", "0 silenced", "needs attention", "╭ /tmp/project", "│", "╰"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("workspace card missing %q:\n%s", expected, got)
		}
	}
	for _, forbidden := range []string{"parked", "stopped", "killed", "quiet", "error"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("workspace card should not render %q label:\n%s", forbidden, got)
		}
	}
}

func TestRenderWorkspaceCardCountsColorOnlyNonzeroValues(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	zeroActive := renderWorkspaceCardCounts(workspaceCardCounts{total: 1, active: 0, needsAttention: 1, silenced: 0}, 72)
	if !strings.Contains(zeroActive, workspaceCountMutedStyle.Render("0 active")) {
		t.Fatalf("zero active should use muted style:\n%q", zeroActive)
	}
	if strings.Contains(zeroActive, workspaceCountActiveStyle.Render("0 active")) {
		t.Fatalf("zero active should not use active color:\n%q", zeroActive)
	}
	if !strings.Contains(zeroActive, workspaceCountNeedsAttentionStyle.Render("1 needs attention")) {
		t.Fatalf("nonzero needs attention should use amber style:\n%q", zeroActive)
	}

	zeroNeedsAttention := renderWorkspaceCardCounts(workspaceCardCounts{total: 1, active: 1, needsAttention: 0, silenced: 0}, 72)
	if !strings.Contains(zeroNeedsAttention, workspaceCountActiveStyle.Render("1 active")) {
		t.Fatalf("nonzero active should use active color:\n%q", zeroNeedsAttention)
	}
	if !strings.Contains(zeroNeedsAttention, workspaceCountMutedStyle.Render("0 needs attention")) {
		t.Fatalf("zero needs attention should use muted style:\n%q", zeroNeedsAttention)
	}
	if strings.Contains(zeroNeedsAttention, workspaceCountNeedsAttentionStyle.Render("0 needs attention")) {
		t.Fatalf("zero needs attention should not use amber color:\n%q", zeroNeedsAttention)
	}
}

func TestWorkspaceCardCountsSilenceIdleAgentsInSilentGroups(t *testing.T) {
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "g", WorkspaceID: "w", Path: "release", Silent: true, CreatedAt: now, UpdatedAt: now},
		},
		Agents: []state.Agent{
			{ID: "ready", WorkspaceID: "w", GroupID: "g", Title: "Ready", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "stopped", WorkspaceID: "w", GroupID: "g", Title: "Stopped", Status: state.StatusStopped, CreatedAt: now, UpdatedAt: now},
			{ID: "error", WorkspaceID: "w", GroupID: "g", Title: "Error", Status: state.StatusError, CreatedAt: now, UpdatedAt: now},
		},
	}

	counts := workspaceCardCountsForWorkspace(st, "w")
	if counts.total != 3 || counts.silenced != 2 || counts.needsAttention != 1 {
		t.Fatalf("counts = %#v", counts)
	}

	rendered := ansi.Strip(renderWorkspaceCardCounts(counts, 72))
	if !strings.Contains(rendered, "2 silenced") {
		t.Fatalf("missing silenced count:\n%s", rendered)
	}
	if !strings.Contains(rendered, "1 needs attention") {
		t.Fatalf("missing needs attention count:\n%s", rendered)
	}
}

func TestRenderWorkspaceFallsBackToSingleNavPane(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusWorkspaces

	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 70, 16, "", 32, 0)

	if !strings.Contains(got, "Workspaces") {
		t.Fatalf("narrow workspace focus should show workspaces pane:\n%s", got)
	}
	if strings.Contains(got, "Agents") {
		t.Fatalf("narrow nav should use one pane, got agents too:\n%s", got)
	}
}

func TestRenderAgentsPaneShowsGroupCountInline(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TitleTemplate = "{title}"
	st := layoutState("/tmp/project")
	st.Agents = append(st.Agents, state.Agent{
		ID:          "b",
		WorkspaceID: "w",
		GroupID:     "f",
		Title:       "beta",
		Status:      state.StatusReady,
		CreatedAt:   state.NowISO(),
		UpdatedAt:   state.NowISO(),
	})

	got := ansi.Strip(strings.Join(renderGroupsPane(cfg, st, 40, 12, 0), "\n"))
	if !strings.Contains(got, "▾ inbox (2)") {
		t.Fatalf("group count should render inline after the title:\n%s", got)
	}
	if strings.Contains(got, "▾ inbox                 2") {
		t.Fatalf("group count should not render as a far-right bare count:\n%s", got)
	}
}

func TestRenderAgentsPaneShowsTopLevelAgentsAndEmptyState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TitleTemplate = "{title}"
	st := layoutState("/tmp/project")
	st.SelectedGroupID = ""
	st.Groups = nil
	st.Agents[0].GroupID = ""

	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 100, 18, "", 60, 0)
	if !strings.Contains(got, "Agents") || !strings.Contains(got, "● alpha") || strings.Contains(got, "▾") {
		t.Fatalf("top-level agent rendering mismatch:\n%s", got)
	}

	st.Agents = nil
	st.ActiveAgentID = ""
	got = renderWorkspaceWithNavWidth(cfg, st, "Codex", "", 100, 18, "", 60, 0)
	if !strings.Contains(got, "No agents") || !strings.Contains(got, "Press n to create one.") {
		t.Fatalf("empty agents pane missing empty state:\n%s", got)
	}

	st = state.Repair(state.Empty(), "/tmp/project")
	got = strings.Join(renderGroupsPane(cfg, st, 40, 12, 0), "\n")
	if !strings.Contains(got, "No workspace selected") || !strings.Contains(got, "Press w to add one.") || strings.Contains(got, "Press n to create one.") {
		t.Fatalf("no-workspace agents pane should explain workspace requirement:\n%s", got)
	}
}

func TestRenderAgentsPaneScrollsSelectedBottomGroupAgentIntoView(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TitleTemplate = "{title}"
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "shipit",
		ActiveAgentID:       "ship",
		Focus:               state.FocusAgents,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "alpha", WorkspaceID: "w", Path: "alpha", CreatedAt: now, UpdatedAt: now},
			{ID: "beta", WorkspaceID: "w", Path: "beta", CreatedAt: now, UpdatedAt: now},
			{ID: "gamma", WorkspaceID: "w", Path: "gamma", CreatedAt: now, UpdatedAt: now},
			{ID: "delta", WorkspaceID: "w", Path: "delta", CreatedAt: now, UpdatedAt: now},
			{ID: "shipit", WorkspaceID: "w", Path: "shipit", CreatedAt: now, UpdatedAt: now},
		},
		Agents: []state.Agent{{ID: "ship", WorkspaceID: "w", GroupID: "shipit", Title: "Ship Agent", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now}},
	}

	groupSelected := ansi.Strip(strings.Join(renderGroupsPane(cfg, st, 32, 11, 4), "\n"))
	if !strings.Contains(groupSelected, "shipit") || strings.Contains(groupSelected, "Ship Agent") {
		t.Fatalf("shipit group should sit at the bottom before moving into its hidden child:\n%s", groupSelected)
	}

	agentSelected := ansi.Strip(strings.Join(renderGroupsPane(cfg, st, 32, 11, 5), "\n"))
	if !strings.Contains(agentSelected, "shipit") || !strings.Contains(agentSelected, "Ship Agent") {
		t.Fatalf("selected bottom group agent should scroll into view:\n%s", agentSelected)
	}
}

func TestRenderAgentsPaneAnimatesLoadingRowsAndColorsStatuses(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	cfg := config.DefaultConfig()
	cfg.TitleTemplate = "{title}"
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Agents: []state.Agent{
			{ID: "loading", WorkspaceID: "w", Title: "Booting", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now},
			{ID: "working", WorkspaceID: "w", Title: "Review", Status: state.StatusRunning, CodexTitle: "Codex Working", CreatedAt: now, UpdatedAt: now},
			{ID: "ready", WorkspaceID: "w", Title: "Respond", Status: state.StatusRunning, CodexTitle: "Codex Ready", CreatedAt: now, UpdatedAt: now},
			{ID: "failed", WorkspaceID: "w", Title: "Broken", Status: state.StatusError, CreatedAt: now, UpdatedAt: now},
			{ID: "stopped", WorkspaceID: "w", Title: "Paused", Status: state.StatusStopped, CreatedAt: now, UpdatedAt: now},
			{ID: "killed", WorkspaceID: "w", Title: "Killed", Status: state.StatusKilled, CreatedAt: now, UpdatedAt: now},
		},
	}

	got := strings.Join(renderGroupsPaneWithOptions(cfg, st, 42, 12, 99, workspaceRenderOptions{
		loadingFrame:  "⠼",
		loadingAgents: map[string]bool{"loading": true},
	}), "\n")
	stripped := ansi.Strip(got)
	if !strings.Contains(stripped, "⠼ Booting") || strings.Contains(stripped, "• Booting") {
		t.Fatalf("loading row should replace the static marker with the spinner:\n%s", stripped)
	}
	if !strings.Contains(stripped, "⠼ Review") || strings.Contains(stripped, "• Review") {
		t.Fatalf("working row should use the animated marker:\n%s", stripped)
	}
	if !strings.Contains(stripped, "● Respond") || strings.Contains(stripped, "⠼ Respond") {
		t.Fatalf("ready row should use the attention marker instead of the spinner:\n%s", stripped)
	}
	if !strings.Contains(stripped, "! Broken") {
		t.Fatalf("error row should use the error marker:\n%s", stripped)
	}
	if !strings.Contains(stripped, "◦ Paused") {
		t.Fatalf("stopped row should use the attention marker:\n%s", stripped)
	}
	if !strings.Contains(stripped, "! Killed") {
		t.Fatalf("killed row should use the attention marker:\n%s", stripped)
	}
	for _, expected := range []string{
		agentRunningStyle.Render("⠼ Booting"),
		agentWorkingStyle.Render("⠼ Review"),
		agentReadyStyle.Render("● Respond"),
		agentErrorStyle.Render("! Broken"),
		agentAttentionStyle.Render("◦ Paused"),
		agentAttentionStyle.Render("! Killed"),
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("agents pane missing styled row %q:\n%s", expected, got)
		}
	}
	for _, forbidden := range []string{"running", "working", "error"} {
		if strings.Contains(strings.ToLower(stripped), forbidden) {
			t.Fatalf("agent rows should not render fixed status text %q:\n%s", forbidden, stripped)
		}
	}
}

func TestRenderWorkspacesPaneEmptyStateIsCenteredHelp(t *testing.T) {
	cfg := config.DefaultConfig()
	st := state.Repair(state.Empty(), "/tmp/project")

	got := strings.Join(renderWorkspacesPane(cfg, st, fixedWorkspacePaneWidth, 12), "\n")

	if !strings.Contains(got, "No workspaces") || !strings.Contains(got, "Press w to add one.") {
		t.Fatalf("empty workspaces pane missing help:\n%s", got)
	}
	for _, line := range strings.Split(ansi.Strip(got), "\n") {
		if strings.Contains(line, "No workspaces") && strings.Trim(line, " │") != "No workspaces" {
			t.Fatalf("empty workspace help should be centered, got line %q\n%s", line, got)
		}
	}
}

func TestRenderWorkspaceEmptyDashboardShowsNewHint(t *testing.T) {
	cfg := config.DefaultConfig()
	st := state.Repair(state.Empty(), "/tmp/project")

	got := renderWorkspace(cfg, st, "Codex", "No Codex agent open.", 80, 24, "", "/tmp/project")

	if strings.Contains(got, "Press n to create one.") || !strings.Contains(got, "Add a workspace first.") {
		t.Fatalf("workspace should not advertise agent creation before a workspace exists:\n%s", got)
	}

	st = layoutState("/tmp/project")
	st.Agents = nil
	st.ActiveAgentID = ""
	st.Focus = state.FocusAgents
	st.NavOpen = true
	got = renderWorkspace(cfg, st, "Codex", "No Codex agent open.", 80, 24, "", "/tmp/project")
	if !strings.Contains(got, "Press n to create one.") {
		t.Fatalf("workspace missing agent creation hint:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	if strings.Contains(lines[len(lines)-1], "Codex") {
		t.Fatalf("empty dashboard should not render default codex title in bottom border:\n%s", got)
	}

	got = renderWorkspaceWithNavWidth(cfg, st, "Codex", "No Codex agent open.", 100, 24, "", 0, 0)
	stripped := ansi.Strip(got)
	logoIndex := strings.Index(stripped, `●─────┼────▶  ██║ █╗ ██║ █████╗   █████╗      ██║`)
	hintIndex := strings.Index(stripped, "No Codex agent open")
	if logoIndex < 0 {
		t.Fatalf("empty dashboard missing Weft wordmark:\n%s", stripped)
	}
	if hintIndex < 0 || logoIndex > hintIndex {
		t.Fatalf("empty dashboard should render wordmark above existing hint:\n%s", stripped)
	}
	if !strings.Contains(stripped, weftversion.Label()) {
		t.Fatalf("empty dashboard missing version label:\n%s", stripped)
	}
	if strings.Contains(stripped, "Agent Live Preview") || strings.Contains(stripped, " … │") {
		t.Fatalf("empty command center should not show cropped preview styling:\n%s", stripped)
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
	if got := ansi.Strip(content[logoStart+len(emptyWeftLogo)+1]); !strings.HasPrefix(got, expectedLeft+centerVisual(weftversion.Label(), logoWidth)) {
		t.Fatalf("empty version should align inside centered logo block, got %q", got)
	}
	if got := ansi.Strip(content[logoStart+len(emptyWeftLogo)+3]); !strings.HasPrefix(got, expectedLeft+centerVisual("No Codex agent open", logoWidth)) {
		t.Fatalf("empty title should align inside centered logo block, got %q", got)
	}
	if got := ansi.Strip(content[logoStart+len(emptyWeftLogo)+4]); !strings.HasPrefix(got, expectedLeft+centerVisual("Press n to create one.", logoWidth)) {
		t.Fatalf("empty hint should align inside centered logo block, got %q", got)
	}
}

func TestRenderWorkspaceLoadingStateIsCentered(t *testing.T) {
	cfg := config.DefaultConfig()
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
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusCodex
	st.NavOpen = false

	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 80, 24, "", 0, 0)

	if strings.Contains(got, "●") {
		t.Fatalf("active dot indicator should not render:\n%s", got)
	}
	if !strings.Contains(got, "C-b dashboard") {
		t.Fatalf("collapsed codex top toolbar missing drawer shortcuts:\n%s", got)
	}
	if strings.Contains(got, "WEFT") {
		t.Fatalf("collapsed codex top toolbar should not include WEFT branding:\n%s", got)
	}
	if strings.Contains(got, "C-c") {
		t.Fatalf("collapsed codex top toolbar should not advertise C-c:\n%s", got)
	}
	if !strings.Contains(got, "Agent Console") {
		t.Fatalf("focused codex pane should render Agent Console title:\n%s", got)
	}
	if strings.Contains(got, "Agent Live Preview") || strings.Contains(got, "● Live") {
		t.Fatalf("focused codex pane should not render live preview UI:\n%s", got)
	}
	st.Agents[0].Status = state.StatusRunning
	st.Agents[0].CodexTitle = "Fake Codex Working"
	got = renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 80, 24, "", 0, 0)
	if !strings.Contains(got, "C-b dashboard") || strings.Contains(got, "WEFT") || strings.Contains(got, "C-c") {
		t.Fatalf("working codex toolbar should only advertise dashboard return:\n%s", got)
	}
}

func TestAgentConsoleReadyIndicatorCountsOtherGlobalReadyAgents(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	cfg := config.DefaultConfig()
	now := state.NowISO()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusCodex
	st.NavOpen = false
	st.Agents = append(st.Agents,
		state.Agent{ID: "b", WorkspaceID: "w", Title: "beta", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
		state.Agent{ID: "c", WorkspaceID: "w2", Title: "gamma", Status: state.StatusRunning, CodexTitle: "Codex Ready", CreatedAt: now, UpdatedAt: now},
		state.Agent{ID: "d", WorkspaceID: "w", Title: "delta", Status: state.StatusRunning, CodexTitle: "Codex Working", CreatedAt: now, UpdatedAt: now},
	)

	got := renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 100, 18, "", 0, 0)
	if !strings.Contains(ansi.Strip(got), "2 other agents ready") {
		t.Fatalf("console should show ready indicator for other global agents:\n%s", got)
	}
	if !strings.Contains(got, workspaceCountNeedsAttentionStyle.Render("2 other agents ready")) {
		t.Fatalf("ready indicator should use needs-attention styling:\n%q", got)
	}

	st.Agents = st.Agents[:1]
	got = renderWorkspaceWithNavWidth(cfg, st, "alpha", "output", 100, 18, "", 0, 0)
	if strings.Contains(ansi.Strip(got), "other agent") {
		t.Fatalf("console should hide ready indicator when no other agents are ready:\n%s", got)
	}
}

func TestCodexLeftPaddingStaysBeforeLeadingANSIStyle(t *testing.T) {
	cfg := config.DefaultConfig()
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

	cfg := config.DefaultConfig()
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

func layoutState(workspace string) state.State {
	now := state.NowISO()
	return state.State{
		Version:             state.Version,
		ActiveAgentID:       "a",
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "f",
		Focus:               state.FocusAgents,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: workspace, CreatedAt: now, UpdatedAt: now}},
		Groups:              []state.Group{{ID: "f", WorkspaceID: "w", Path: "inbox", CreatedAt: now, UpdatedAt: now}},
		Agents:              []state.Agent{{ID: "a", WorkspaceID: "w", GroupID: "f", Title: "alpha", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now}},
	}
}
