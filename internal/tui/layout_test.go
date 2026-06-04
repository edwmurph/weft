package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
	weftversion "github.com/edwmurph/weft/internal/version"
	"github.com/muesli/termenv"
)

func TestWorkspaceNavWidthShrinksWorkspacesFirst(t *testing.T) {
	st := layoutState("/tmp/project")
	if got := workspaceNavFrameWidth(st, 142); got != fixedWorkspacePaneWidth+defaultTasksPaneWidth {
		t.Fatalf("wide nav width = %d", got)
	}
	if got := workspaceNavFrameWidth(st, 140); got != 112 {
		t.Fatalf("wide nav width with minimum preview = %d", got)
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

func TestFormatTaskOperationDuration(t *testing.T) {
	for _, tt := range []struct {
		name    string
		elapsed time.Duration
		want    string
	}{
		{name: "instant", elapsed: 0, want: "1s"},
		{name: "seconds", elapsed: 12 * time.Second, want: "12s"},
		{name: "last second", elapsed: 59 * time.Second, want: "59s"},
		{name: "one minute", elapsed: time.Minute, want: "1m"},
		{name: "two minutes", elapsed: 2*time.Minute + 59*time.Second, want: "2m"},
		{name: "last minute", elapsed: 59*time.Minute + 59*time.Second, want: "59m"},
		{name: "one hour", elapsed: time.Hour, want: "1h"},
		{name: "hour minutes", elapsed: time.Hour + 2*time.Minute + 59*time.Second, want: "1h2m"},
		{name: "many hours", elapsed: 25*time.Hour + 4*time.Minute, want: "25h4m"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTaskOperationDuration(tt.elapsed); got != tt.want {
				t.Fatalf("duration = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAutoTitleMaxColumnsAccountsForTaskTypeBadges(t *testing.T) {
	cfg := config.DefaultConfig()
	custom := cfg.TaskTypes[config.DefaultTaskTypeShell]
	custom.ID = "wide"
	custom.Badge = "[widekind]"
	cfg.TaskTypes[custom.ID] = custom
	st := layoutState("/tmp/project")
	st.Tasks = []state.Task{{
		ID:          "a",
		WorkspaceID: "w",
		TypeID:      config.DefaultTaskTypeCodex,
		Title:       "{status} {auto}",
		LiveTitle:   "Fake Codex Ready",
		LiveStatus:  "Ready",
		Status:      state.StatusRunning,
		CreatedAt:   state.NowISO(),
		UpdatedAt:   state.NowISO(),
	}}

	got := autoTitleMaxColumns(cfg, st, st.Tasks[0], fixedWorkspacePaneWidth+defaultTasksPaneWidth+minCodexPaneWidth)

	if got != 31 {
		t.Fatalf("auto title columns = %d, want 31", got)
	}
}

func TestAutoTitleMaxColumnsAccountsForTaskSilentMarker(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Tasks[0].Title = "{status} {auto}"
	st.Tasks[0].LiveTitle = "Fake Codex Ready"
	st.Tasks[0].LiveStatus = "Ready"
	st.Tasks[0].Silent = true

	got := autoTitleMaxColumns(cfg, st, st.Tasks[0], fixedWorkspacePaneWidth+defaultTasksPaneWidth+minCodexPaneWidth)

	if got != 30 {
		t.Fatalf("auto title columns = %d, want 30", got)
	}
}

func TestWrapPlainSplitsLongWordsWithoutEllipsis(t *testing.T) {
	got := wrapPlain("error abcdefghijklmnopqrstuvwxyz done", 10, 10)
	joined := strings.Join(got, "")

	if strings.Contains(joined, "…") || strings.Contains(joined, "...") {
		t.Fatalf("wrapped text should not use ellipsis: %#v", got)
	}
	if !strings.Contains(joined, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("wrapped text dropped long word: %#v", got)
	}
	for _, line := range got {
		if lipgloss.Width(line) > 10 {
			t.Fatalf("line width = %d, want <= 10: %#v", lipgloss.Width(line), got)
		}
	}
}

func TestWorkspaceFooterKeepsBlockingTaskList(t *testing.T) {
	got := ansi.Strip(strings.Join(renderWorkspaceFooter(
		"Config pending: config.toml changed.\nWait for 1 shell task(s) to become idle.\nBlocking:\n- workspace: /Users/emurphy/code/personal/weft/.worktrees/config-drift-upgrade\n  task: SuperLongTaskTitleWithoutSpaces",
		32,
		12,
		workspaceUpgradeFooterStyle,
	), "\n"))

	for _, expected := range []string{"Blocking:", "- workspace:", "  task: SuperLong"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("blocking footer should include %q, got:\n%s", expected, got)
		}
	}
}

func TestStatusBannerPreservesBlockingTaskList(t *testing.T) {
	got := ansi.Strip(strings.Join(renderStatusBanner(
		"Upgrade waits until 1 shell task(s) are idle.\nBlocking:\n- workspace: Core\n  task: Shell",
		48,
		6,
	), "\n"))

	for _, expected := range []string{"Upgrade: waits until 1 shell", "Blocking:", "- workspace: Core", "  task: Shell"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("status banner should include %q, got:\n%s", expected, got)
		}
	}
}

func TestWeftLogoGraphShape(t *testing.T) {
	if len(emptyWeftLogo) != 6 {
		t.Fatalf("logo height = %d, want 6", len(emptyWeftLogo))
	}
	joined := strings.Join(emptyWeftLogo, "\n")
	if got := strings.Count(joined, "◆"); got != 3 {
		t.Fatalf("logo graph input count = %d, want 3:\n%s", got, joined)
	}
	if got := strings.Count(joined, "➤"); got != 1 {
		t.Fatalf("logo graph output count = %d, want 1:\n%s", got, joined)
	}
	if got := lipgloss.Width("➤"); got != 1 {
		t.Fatalf("logo arrowhead width = %d, want 1", got)
	}
	for index, wantPrefix := range []string{
		"◆━━━━━┓",
		"      ┃",
		"◆━━━━━╋━━━━━➤ ",
		"      ┃",
		"◆━━━━━┛",
		"       ",
	} {
		if !strings.HasPrefix(emptyWeftLogo[index], wantPrefix) {
			t.Fatalf("logo row %d prefix = %q, want %q", index, emptyWeftLogo[index], wantPrefix)
		}
	}
}

func TestPreviewEmptyWeftLogoGraphShape(t *testing.T) {
	if len(previewEmptyWeftLogo) != 6 {
		t.Fatalf("preview logo height = %d, want 6", len(previewEmptyWeftLogo))
	}
	joined := strings.Join(previewEmptyWeftLogo, "\n")
	if got := strings.Count(joined, "◆"); got != 3 {
		t.Fatalf("preview logo graph input count = %d, want 3:\n%s", got, joined)
	}
	if got := strings.Count(joined, "➤"); got != 1 {
		t.Fatalf("preview logo graph output count = %d, want 1:\n%s", got, joined)
	}
	if got := lipgloss.Width("➤"); got != 1 {
		t.Fatalf("preview logo arrowhead width = %d, want 1", got)
	}
	for index, wantPrefix := range []string{
		"◆━━━━━┓",
		"      ┃",
		"◆━━━━━╋━━━━━➤ ",
		"      ┃",
		"◆━━━━━┛",
		"       ",
	} {
		if !strings.HasPrefix(previewEmptyWeftLogo[index], wantPrefix) {
			t.Fatalf("preview logo row %d prefix = %q, want %q", index, previewEmptyWeftLogo[index], wantPrefix)
		}
	}
}

func TestRenderWorkspaceShowsWorkspacesTasksAndTaskPreview(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	output := "output from a selected task that is intentionally long enough to be cropped by the preview lens"

	got := renderWorkspaceView(cfg, st, "alpha", output, 140, 24, "", minTwoPaneNavWidth, 2, workspaceRenderOptions{})

	for _, expected := range []string{
		"Workspaces",
		"Tasks",
		"Task Live Preview",
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
	if len(lines) == 0 || !strings.Contains(lines[0], "Task Live Preview ·") || !strings.Contains(lines[0], "alpha") {
		t.Fatalf("preview top border should include pane title and task title:\n%s", stripped)
	}
	if strings.Contains(lines[len(lines)-1], "alpha") {
		t.Fatalf("preview bottom border should not include task title:\n%s", stripped)
	}
	if strings.Contains(stripped, "● Live") || strings.Contains(stripped, " Live─") {
		t.Fatalf("preview should not render live indicator text:\n%s", stripped)
	}
	if !strings.Contains(stripped, " … │") {
		t.Fatalf("wide preview should reserve a right-edge crop marker with right padding:\n%s", stripped)
	}
	if strings.Contains(stripped, "cropped by the preview lens…") {
		t.Fatalf("preview should not attach generic clipping ellipsis to task text:\n%s", stripped)
	}
	if strings.Contains(got, "ready") {
		t.Fatalf("task rows should not render fixed status tags unless template asks for them:\n%s", got)
	}
}

func TestRenderTaskPreviewRequiresFocusedTaskRow(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	output := "selected terminal output"

	got := ansi.Strip(renderWorkspaceView(cfg, st, "Preview Title", output, 140, 24, "", minTwoPaneNavWidth, 0, workspaceRenderOptions{
		previewHeaderAnimation: "●",
	}))
	if !strings.Contains(got, "No task selected") || !strings.Contains(got, "Select a task to preview.") || strings.Contains(got, output) || strings.Contains(got, "Preview Title") {
		t.Fatalf("group row focus should render an empty preview instead of the active task:\n%s", got)
	}
	if lines := strings.Split(got, "\n"); len(lines) == 0 || strings.Contains(lines[0], "Task Live Preview ●") {
		t.Fatalf("group row focus should not show active preview animation:\n%s", got)
	}

	st.Focus = state.FocusWorkspaces
	st.SelectedWorkspaceID = ""
	got = ansi.Strip(renderWorkspaceView(cfg, st, "Preview Title", output, 140, 24, "", minTwoPaneNavWidth, 1, workspaceRenderOptions{
		previewHeaderAnimation: "●",
	}))
	if !strings.Contains(got, "No task selected") || strings.Contains(got, output) || strings.Contains(got, "Preview Title") {
		t.Fatalf("no workspace selection should render an empty preview instead of the active task:\n%s", got)
	}
}

func TestRenderTaskPreviewEmptyStateUsesPreviewLogoAndAnimation(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.ActiveTaskID = ""
	st.SelectedTaskID = ""
	st.Tasks = nil

	got := renderWorkspaceView(cfg, st, "Task", "No task open.", 180, 24, "", minTwoPaneNavWidth, 1, workspaceRenderOptions{
		emptyArtFrame: 30,
	})
	stripped := ansi.Strip(got)
	if !strings.Contains(stripped, "No task selected") || !strings.Contains(stripped, "◆━━━━━╋━━━━━➤ ") {
		t.Fatalf("empty preview missing preview logo:\n%s", stripped)
	}
	if strings.Contains(stripped, weftversion.Label()) {
		t.Fatalf("empty preview should not render dashboard version text outside the Workspaces pane:\n%s", stripped)
	}
	if strings.Contains(stripped, "●─────┼────▶") {
		t.Fatalf("empty preview should use the shared diamond graph, not the old static logo arrow:\n%s", stripped)
	}

	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	line := renderEmptyLogoLine(previewEmptyWeftLogo[2], maxVisualWidth(previewEmptyWeftLogo), true, 30, 2)
	if !strings.Contains(line, "\x1b[") {
		t.Fatalf("animated preview logo line should contain accent styling, got %q", line)
	}
	if strippedLine := ansi.Strip(line); strippedLine != padVisual(previewEmptyWeftLogo[2], maxVisualWidth(previewEmptyWeftLogo)) {
		t.Fatalf("animated preview logo should preserve text layout:\nwant %q\ngot  %q", padVisual(previewEmptyWeftLogo[2], maxVisualWidth(previewEmptyWeftLogo)), strippedLine)
	}
	for frame := 0; frame < previewLogoActiveFrames; frame += previewLogoAccentHold {
		for row := range previewEmptyWeftLogo {
			for _, r := range previewLogoAccentRanges(row, frame) {
				if r.start >= 14 || r.end > 14 {
					t.Fatalf("preview animation should stay on arrow graph, frame=%d row=%d range=%#v", frame, row, r)
				}
				if width := r.end - r.start; width != previewLogoAccentWidth {
					t.Fatalf("preview animation chunks should be fixed width, frame=%d row=%d range=%#v width=%d", frame, row, r, width)
				}
			}
		}
	}
	pauseDuration := time.Duration(previewLogoPauseFrames) * loadingInterval
	if pauseDuration < 2*time.Second || pauseDuration > 4*time.Second {
		t.Fatalf("preview animation pause = %s, want between 2s and 4s", pauseDuration)
	}
	for _, frame := range []int{previewLogoActiveFrames, previewLogoActiveFrames + previewLogoPauseFrames/2, previewLogoCycleFrames - 1} {
		for row := range previewEmptyWeftLogo {
			if ranges := previewLogoAccentRanges(row, frame); len(ranges) != 0 {
				t.Fatalf("preview animation should pause between sweeps, frame=%d row=%d ranges=%#v", frame, row, ranges)
			}
		}
	}
	if ranges := previewLogoAccentRanges(2, previewLogoCycleFrames); len(ranges) != 1 || ranges[0].start != 0 {
		t.Fatalf("preview animation should restart after pause, ranges=%#v", ranges)
	}
	for index, frame := range []int{0, 4, 8, 12, 16, 20, 24} {
		start := 99
		for row := range previewEmptyWeftLogo {
			for _, r := range previewLogoAccentRanges(row, frame) {
				start = min(start, r.start)
			}
		}
		if start == 99 {
			t.Fatalf("preview animation frame %d has no highlighted graph range", frame)
		}
		if index > 0 {
			previousStart := []int{0, 2, 4, 6, 8, 10, 12}[index-1]
			if start < previousStart {
				t.Fatalf("preview animation should move left-to-right, frame=%d start=%d previous=%d", frame, start, previousStart)
			}
		}
	}
}

func TestRenderTaskPreviewRejectsMismatchedCursorContent(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Tasks = append(st.Tasks, state.Task{
		ID:          "b",
		WorkspaceID: "w",
		GroupID:     "f",
		Title:       "bravo",
		Status:      state.StatusReady,
		CreatedAt:   state.NowISO(),
		UpdatedAt:   state.NowISO(),
	})

	got := ansi.Strip(renderWorkspaceView(cfg, st, "alpha", "alpha output", 140, 24, "", minTwoPaneNavWidth, 3, workspaceRenderOptions{
		previewHeaderAnimation: "●",
	}))
	if !strings.Contains(got, "No task selected") || strings.Contains(got, "alpha output") || strings.Contains(got, "Task Live Preview ●") {
		t.Fatalf("mismatched cursor/content should render an empty preview instead of stale task output:\n%s", got)
	}
}

func TestRenderTaskPreviewHeaderUsesAnimationFrame(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")

	got := renderWorkspaceView(cfg, st, "alpha", "output", 140, 24, "", minTwoPaneNavWidth, 2, workspaceRenderOptions{
		previewHeaderAnimation: "●",
	})
	stripped := ansi.Strip(got)
	lines := strings.Split(stripped, "\n")

	if len(lines) == 0 || !strings.Contains(lines[0], "Task Live Preview ●") || !strings.Contains(lines[0], "alpha") {
		t.Fatalf("preview top border should include animation frame and task title:\n%s", stripped)
	}

	st.ActiveTaskID = ""
	st.SelectedTaskID = ""
	st.Tasks = nil
	got = renderWorkspaceView(cfg, st, "Task", "No task open.", 140, 24, "", minTwoPaneNavWidth, 1, workspaceRenderOptions{
		previewHeaderAnimation: "●",
	})
	stripped = ansi.Strip(got)
	lines = strings.Split(stripped, "\n")
	if len(lines) == 0 || strings.Contains(lines[0], "Task Live Preview ●") {
		t.Fatalf("empty preview should not include animation frame:\n%s", stripped)
	}
}

func TestLivePreviewAnimationFramePulsesDotSlowly(t *testing.T) {
	cases := []struct {
		index int
		want  string
	}{
		{index: -1, want: "·"},
		{index: 0, want: "·"},
		{index: 2, want: "·"},
		{index: 3, want: "∙"},
		{index: 6, want: "•"},
		{index: 9, want: "●"},
		{index: 12, want: "•"},
		{index: 15, want: "∙"},
		{index: 18, want: "·"},
	}
	for _, tt := range cases {
		if got := livePreviewAnimationFrame(tt.index); got != tt.want {
			t.Fatalf("animation frame index=%d = %q, want %q", tt.index, got, tt.want)
		}
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

	got := renderWorkspaceView(cfg, st, "Task", "No task open.", minThreePaneWidth, 24, "", workspaceNavFrameWidth(st, minThreePaneWidth), 0, workspaceRenderOptions{})

	for _, expected := range []string{"Workspaces", "Tasks", "No task selected", expectedPath} {
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

	got := renderWorkspaceView(cfg, st, "Task", "No task open.", 100, 24, "", workspaceNavFrameWidth(st, 100), 0, workspaceRenderOptions{})

	for _, expected := range []string{"Workspaces", "Tasks", expectedPath} {
		if !strings.Contains(got, expected) {
			t.Fatalf("medium dashboard missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "No task open") || strings.Contains(got, "Task Live Preview") {
		t.Fatalf("medium dashboard should hide Task Live Preview before clipping fixed Workspaces:\n%s", got)
	}
}

func TestRenderWorkspaceCardsUseDefaultPathAndTitleOverride(t *testing.T) {
	workspace, err := os.MkdirTemp("/tmp", "weft-layout-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workspace) })
	cfg := config.DefaultConfig()
	st := layoutState(workspace)
	st.Focus = state.FocusWorkspaces

	got := ansi.Strip(strings.Join(renderWorkspacesPaneWithOptions(cfg, st, 78, 8, workspaceRenderOptions{}), "\n"))
	if !strings.Contains(got, workspace) {
		t.Fatalf("workspace card should use default display path title:\n%s", got)
	}
	if !strings.Contains(strings.ToLower(got), "new workspace") || !strings.Contains(strings.ToLower(got), "press w to create") {
		t.Fatalf("workspaces pane should include new-workspace template card:\n%s", got)
	}

	st.Workspaces[0].Title = "Main Weft"
	got = ansi.Strip(strings.Join(renderWorkspacesPaneWithOptions(cfg, st, 78, 8, workspaceRenderOptions{}), "\n"))
	if !strings.Contains(got, "Main Weft") || strings.Contains(got, workspace) {
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

	got := ansi.Strip(strings.Join(renderWorkspacesPaneWithOptions(cfg, st, 78, 9, workspaceRenderOptions{}), "\n"))
	if !strings.Contains(got, "path missing; press Backspace to remove") {
		t.Fatalf("missing workspace path should be visible and actionable:\n%s", got)
	}
}

func TestRenderWorkspacesPaneShowsUpgradeFooterAtBottom(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")

	got := ansi.Strip(strings.Join(renderWorkspacesPaneWithOptions(cfg, st, 60, 12, workspaceRenderOptions{
		workspaceFooterText: "Upgrade ready: client 7.5.5, supervisor 7.4.0.\nPress U to upgrade and resume 1 idle task.",
	}), "\n"))
	for _, expected := range []string{"Upgrade ready: client 7.5.5, supervisor 7.4.0.", "Press U to upgrade and resume 1 idle task."} {
		if !strings.Contains(got, expected) {
			t.Fatalf("upgrade footer missing %q:\n%s", expected, got)
		}
	}
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[len(lines)-3], "Upgrade ready") || !strings.Contains(lines[len(lines)-2], "Press U") {
		t.Fatalf("upgrade footer should be pinned to pane bottom:\n%s", got)
	}
}

func TestRenderWorkspacesPaneShowsVersionHeaderWithUpgradeFooter(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState(t.TempDir())

	got := ansi.Strip(strings.Join(renderWorkspacesPaneWithOptions(cfg, st, 60, 12, workspaceRenderOptions{
		workspaceInfoText:   "Weft\nCLI        7.13.6\nSupervisor 7.13.5",
		workspaceFooterText: "Upgrade ready: supervisor 7.13.5 → 7.13.6.\nPress U to upgrade and resume 1 idle Codex task.",
	}), "\n"))
	for _, expected := range []string{
		"Weft",
		"CLI        7.13.6",
		"Supervisor 7.13.5",
		"Upgrade ready: supervisor 7.13.5 → 7.13.6.",
		"Press U to upgrade and resume 1 idle Codex task.",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("workspace pane missing %q:\n%s", expected, got)
		}
	}
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[1], "┌") || !strings.Contains(lines[2], "Weft") {
		t.Fatalf("version header should stay pinned to pane top:\n%s", got)
	}
	if !strings.Contains(lines[len(lines)-3], "Upgrade ready") || !strings.Contains(lines[len(lines)-2], "Press U") {
		t.Fatalf("upgrade footer should stay pinned to pane bottom:\n%s", got)
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

func TestRenderWorkspacesPaneScrollsWorkspaceCardsBelowVersionHeader(t *testing.T) {
	cfg := config.DefaultConfig()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w3",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
	}
	now := state.NowISO()
	for index := 0; index < 4; index++ {
		path := filepath.Join(t.TempDir(), "workspace")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		st.Workspaces = append(st.Workspaces, state.Workspace{
			ID:        "w" + fmtInt(index),
			Path:      path,
			Title:     "Workspace " + fmtInt(index),
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	got := ansi.Strip(strings.Join(renderWorkspacesPaneWithOptions(cfg, st, 60, 12, workspaceRenderOptions{
		workspaceInfoText:   "Weft\nCLI        7.13.6\nSupervisor 7.13.5",
		workspaceFooterText: "Upgrade ready: supervisor 7.13.5 → 7.13.6.\nPress U to upgrade and resume 1 idle Codex task.",
	}), "\n"))
	for _, expected := range []string{"Weft", "CLI        7.13.6", "Supervisor 7.13.5", "Workspace 3", "Upgrade ready"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("scrolling workspace pane missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "Workspace 0") {
		t.Fatalf("workspace card body should scroll instead of hiding the fixed header/footer:\n%s", got)
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
		Tasks: []state.Task{
			{ID: "starting", WorkspaceID: "w", Title: "Starting", Status: state.StatusStarting, CreatedAt: now, UpdatedAt: now},
			{ID: "running", WorkspaceID: "w", Title: "Running", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now},
			{ID: "waiting", WorkspaceID: "w", Title: "Waiting", Status: state.StatusRunning, LiveTitle: "Codex Waiting", LiveStatus: "Waiting", CreatedAt: now, UpdatedAt: now},
			{ID: "working", WorkspaceID: "w", Title: "Working", Status: state.StatusRunning, LiveTitle: "Codex Working", LiveStatus: "Working", CreatedAt: now, UpdatedAt: now},
			{ID: "shipping", WorkspaceID: "w", Title: "Shipping", Status: state.StatusShipping, CreatedAt: now, UpdatedAt: now},
			{ID: "ready", WorkspaceID: "w", Title: "Ready", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
			{ID: "live-ready", WorkspaceID: "w", Title: "Live Ready", Status: state.StatusRunning, LiveTitle: "Codex Ready", LiveStatus: "Ready", CreatedAt: now, UpdatedAt: now},
			{ID: "failed", WorkspaceID: "w", Title: "Failed", Status: state.StatusError, CreatedAt: now, UpdatedAt: now},
			{ID: "killed", WorkspaceID: "w", Title: "Killed", Status: state.StatusKilled, CreatedAt: now, UpdatedAt: now},
		},
	}

	counts := workspaceCardCountsForWorkspace(st, "w")
	if counts.total != 9 || counts.active != 5 || counts.needsAttention != 4 {
		t.Fatalf("counts = %#v", counts)
	}
	if counts.active+counts.needsAttention+counts.silenced != counts.total {
		t.Fatalf("counts = %#v", counts)
	}
	got := strings.ToLower(ansi.Strip(strings.Join(renderWorkspacesPaneWithOptions(cfg, st, 78, 8, workspaceRenderOptions{}), "\n")))
	for _, expected := range []string{"9 total", "5 active", "4 needs attention", "0 silenced", "╭ /tmp/project", "│", "╰"} {
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
	if got, want := workspaceCountNeedsAttentionStyle.Render("1 needs attention"), taskReadyStyle.Render("1 needs attention"); got != want {
		t.Fatalf("needs attention style should match the Tasks pane ready highlight/text style:\ngot  %q\nwant %q", got, want)
	}
	if !strings.Contains(zeroActive, workspaceCountNeedsAttentionStyle.Render("1 needs attention")) {
		t.Fatalf("nonzero needs attention should use the Tasks pane yellow style:\n%q", zeroActive)
	}
	if strings.Contains(zeroActive, activePaneStyle.Render("1 needs attention")) {
		t.Fatalf("nonzero needs attention should not use the blue focus style:\n%q", zeroActive)
	}
	if strings.Contains(zeroActive, taskAttentionStyle.Render("1 needs attention")) {
		t.Fatalf("nonzero needs attention should not use the orange attention style:\n%q", zeroActive)
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

func TestWorkspaceCardCountsSilenceIdleTasksInSilentGroups(t *testing.T) {
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
		Tasks: []state.Task{
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

func TestWorkspaceCardCountsSilenceIdleTasksWithTaskSilent(t *testing.T) {
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Tasks: []state.Task{
			{ID: "ready", WorkspaceID: "w", Title: "Ready", Status: state.StatusReady, Silent: true, CreatedAt: now, UpdatedAt: now},
			{ID: "running", WorkspaceID: "w", Title: "Running", Status: state.StatusRunning, Silent: true, CreatedAt: now, UpdatedAt: now},
			{ID: "killed", WorkspaceID: "w", Title: "Killed", Status: state.StatusKilled, Silent: true, CreatedAt: now, UpdatedAt: now},
			{ID: "error", WorkspaceID: "w", Title: "Error", Status: state.StatusError, Silent: true, CreatedAt: now, UpdatedAt: now},
		},
	}

	counts := workspaceCardCountsForWorkspace(st, "w")
	if counts.total != 4 || counts.active != 1 || counts.silenced != 1 || counts.needsAttention != 2 {
		t.Fatalf("counts = %#v", counts)
	}
}

func TestRenderWorkspaceFallsBackToSingleNavPane(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusWorkspaces

	got := renderWorkspaceView(cfg, st, "alpha", "output", 70, 16, "", 32, 0, workspaceRenderOptions{})

	if !strings.Contains(got, "Workspaces") {
		t.Fatalf("narrow workspace focus should show workspaces pane:\n%s", got)
	}
	if strings.Contains(got, "Tasks") {
		t.Fatalf("narrow nav should use one pane, got tasks too:\n%s", got)
	}
}

func TestRenderTasksPaneShowsGroupCountInline(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Tasks = append(st.Tasks, state.Task{
		ID:          "b",
		WorkspaceID: "w",
		GroupID:     "f",
		Title:       "beta",
		Status:      state.StatusReady,
		CreatedAt:   state.NowISO(),
		UpdatedAt:   state.NowISO(),
	})

	got := ansi.Strip(strings.Join(renderGroupsPaneWithOptions(cfg, st, 40, 12, 0, workspaceRenderOptions{}), "\n"))
	if !strings.Contains(got, "▾ inbox (2)") {
		t.Fatalf("group count should render inline after the title:\n%s", got)
	}
	if strings.Contains(got, "▾ inbox                 2") {
		t.Fatalf("group count should not render as a far-right bare count:\n%s", got)
	}

	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)
	styled := strings.Join(renderGroupsPaneWithOptions(cfg, st, 40, 12, 0, workspaceRenderOptions{}), "\n")
	if !strings.Contains(styled, groupHeaderStyle.Render("▾ inbox")+mutedStyle.Render(" (2)")) {
		t.Fatalf("group title should carry hierarchy while count stays muted:\n%s", styled)
	}
}

func TestRenderTasksPaneShowsCollapsedGroupLoadingChild(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.CollapsedGroupIDs = []string{"f"}
	st.Tasks[0].TypeID = config.DefaultTaskTypeShell
	st.Tasks[0].Status = state.StatusRunning

	got := ansi.Strip(strings.Join(renderGroupsPaneWithOptions(cfg, st, 40, 12, 0, workspaceRenderOptions{
		loadingFrame: "⠼",
		loadingTasks: map[string]bool{"a": true},
	}), "\n"))

	if !strings.Contains(got, "▸ ⠼ inbox (1)") {
		t.Fatalf("collapsed group should expose loading child marker:\n%s", got)
	}
	if strings.Contains(got, "[shell] alpha") {
		t.Fatalf("collapsed group should still hide child task rows:\n%s", got)
	}
}

func TestRenderTasksPaneShowsTopLevelTasksAndEmptyState(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.SelectedGroupID = ""
	st.Groups = nil
	st.Tasks[0].GroupID = ""

	got := renderWorkspaceView(cfg, st, "alpha", "output", 100, 18, "", 60, 0, workspaceRenderOptions{})
	stripped := ansi.Strip(got)
	if !strings.Contains(stripped, "Tasks") || !strings.Contains(stripped, "+ New task") || !strings.Contains(stripped, "· [codex] alpha") || strings.Contains(stripped, "▾") {
		t.Fatalf("top-level task rendering mismatch:\n%s", got)
	}
	if strings.Index(stripped, "+ New task") > strings.Index(stripped, "· [codex] alpha") {
		t.Fatalf("new task row should render above task rows:\n%s", stripped)
	}

	st.Tasks = nil
	st.ActiveTaskID = ""
	got = renderWorkspaceView(cfg, st, "Task", "", 100, 18, "", 60, 0, workspaceRenderOptions{})
	stripped = ansi.Strip(got)
	if !strings.Contains(stripped, "+ New task") || !strings.Contains(stripped, "Press n to create") || strings.Contains(stripped, "No tasks") {
		t.Fatalf("empty tasks pane missing new task row:\n%s", got)
	}

	st = state.Empty()
	got = strings.Join(renderGroupsPaneWithOptions(cfg, st, 40, 12, 0, workspaceRenderOptions{}), "\n")
	if !strings.Contains(got, "No workspace selected") || !strings.Contains(got, "Press w to add one.") || strings.Contains(got, "Press n to create") {
		t.Fatalf("no-workspace tasks pane should explain workspace requirement:\n%s", got)
	}
}

func TestRenderTasksPaneShowsTaskSilentMarker(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Tasks[0].Silent = true

	got := ansi.Strip(strings.Join(renderGroupsPaneWithOptions(cfg, st, 44, 12, 0, workspaceRenderOptions{}), "\n"))
	if !strings.Contains(got, "· ⊘ [codex] alpha") {
		t.Fatalf("silent task row missing marker:\n%s", got)
	}
}

func TestRenderTasksPaneMutesSilencedIdleTaskRows(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	cfg := config.DefaultConfig()
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Groups:              []state.Group{{ID: "g", WorkspaceID: "w", Path: "release", Silent: true, CreatedAt: now, UpdatedAt: now}},
		Tasks: []state.Task{
			{ID: "task-ready", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "Task Ready", Status: state.StatusReady, Silent: true, CreatedAt: now, UpdatedAt: now},
			{ID: "task-stopped", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "Task Stopped", Status: state.StatusStopped, Silent: true, CreatedAt: now, UpdatedAt: now},
			{ID: "task-running", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "Task Running", Status: state.StatusRunning, Silent: true, CreatedAt: now, UpdatedAt: now},
			{ID: "task-error", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "Task Error", Status: state.StatusError, Silent: true, CreatedAt: now, UpdatedAt: now},
			{ID: "group-ready", WorkspaceID: "w", GroupID: "g", TypeID: config.DefaultTaskTypeCodex, Title: "Group Ready", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
		},
	}

	got := strings.Join(renderGroupsPaneWithOptions(cfg, st, 52, 16, 99, workspaceRenderOptions{}), "\n")

	for _, expected := range []string{
		mutedStyle.Render("· ⊘ [codex] Task Ready"),
		mutedStyle.Render("◦ ⊘ [codex] Task Stopped"),
		mutedStyle.Render("  · [codex] Group Ready"),
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("silenced idle task row should use muted style %q:\n%s", expected, got)
		}
	}
	for _, forbidden := range []string{
		taskReadyStyle.Render("· ⊘ [codex] Task Ready"),
		taskAttentionStyle.Render("◦ ⊘ [codex] Task Stopped"),
		taskReadyStyle.Render("  · [codex] Group Ready"),
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("silenced idle task row should not use attention style %q:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(got, taskRunningStyle.Render("⠋ ⊘ [codex] Task Running")) {
		t.Fatalf("active silenced task should keep active styling:\n%s", got)
	}
	if !strings.Contains(got, taskErrorStyle.Render("! ⊘ [codex] Task Error")) {
		t.Fatalf("error silenced task should keep error styling:\n%s", got)
	}
}

func TestRenderTasksPaneUsesSingleGapBetweenNewTaskRowAndFirstGroup(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Tasks = nil
	st.ActiveTaskID = ""
	st.SelectedTaskID = ""

	got := ansi.Strip(strings.Join(renderGroupsPaneWithOptions(cfg, st, 40, 12, 0, workspaceRenderOptions{}), "\n"))
	lines := strings.Split(got, "\n")
	newTaskLine := -1
	groupLine := -1
	for index, line := range lines {
		if strings.Contains(line, "+ New task") {
			newTaskLine = index
		}
		if strings.Contains(line, "▾ inbox (0)") {
			groupLine = index
		}
	}

	if newTaskLine == -1 || groupLine == -1 {
		t.Fatalf("tasks pane missing new task row or first group:\n%s", got)
	}
	if gap := groupLine - newTaskLine; gap != 2 {
		t.Fatalf("new task row should leave exactly one blank line before first group, line gap=%d:\n%s", gap, got)
	}
}

func TestRenderTasksPaneScrollsSelectedBottomGroupTaskIntoView(t *testing.T) {
	cfg := config.DefaultConfig()
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "shipit",
		ActiveTaskID:        "ship",
		Focus:               state.FocusTasks,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Groups: []state.Group{
			{ID: "alpha", WorkspaceID: "w", Path: "alpha", CreatedAt: now, UpdatedAt: now},
			{ID: "beta", WorkspaceID: "w", Path: "beta", CreatedAt: now, UpdatedAt: now},
			{ID: "gamma", WorkspaceID: "w", Path: "gamma", CreatedAt: now, UpdatedAt: now},
			{ID: "delta", WorkspaceID: "w", Path: "delta", CreatedAt: now, UpdatedAt: now},
			{ID: "shipit", WorkspaceID: "w", Path: "shipit", CreatedAt: now, UpdatedAt: now},
		},
		Tasks: []state.Task{{ID: "ship", WorkspaceID: "w", GroupID: "shipit", TypeID: config.DefaultTaskTypeCodex, Title: "Ship Task", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now}},
	}

	groupSelected := ansi.Strip(strings.Join(renderGroupsPaneWithOptions(cfg, st, 32, 11, 5, workspaceRenderOptions{}), "\n"))
	if !strings.Contains(groupSelected, "shipit") || strings.Contains(groupSelected, "Ship Task") {
		t.Fatalf("shipit group should sit at the bottom before moving into its hidden child:\n%s", groupSelected)
	}

	taskSelected := ansi.Strip(strings.Join(renderGroupsPaneWithOptions(cfg, st, 32, 11, 6, workspaceRenderOptions{}), "\n"))
	if !strings.Contains(taskSelected, "shipit") || !strings.Contains(taskSelected, "Ship Task") {
		t.Fatalf("selected bottom group task should scroll into view:\n%s", taskSelected)
	}
}

func TestRenderTasksPaneAnimatesLoadingRowsAndColorsStatuses(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	cfg := config.DefaultConfig()
	now := state.NowISO()
	st := state.State{
		Version:             state.Version,
		SelectedWorkspaceID: "w",
		Focus:               state.FocusWorkspaces,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: "/tmp/project", CreatedAt: now, UpdatedAt: now}},
		Tasks: []state.Task{
			{ID: "loading", TypeID: config.DefaultTaskTypeCodex, WorkspaceID: "w", Title: "Booting", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now},
			{ID: "working", TypeID: config.DefaultTaskTypeCodex, WorkspaceID: "w", Title: "Review", Status: state.StatusRunning, LiveTitle: "Codex Working", LiveStatus: "Working", CreatedAt: now, UpdatedAt: now},
			{ID: "waiting", TypeID: config.DefaultTaskTypeCodex, WorkspaceID: "w", Title: "Approval", Status: state.StatusRunning, LiveTitle: "Codex Waiting", LiveStatus: "Waiting", CreatedAt: now, UpdatedAt: now},
			{ID: "terminal-waiting", TypeID: config.DefaultTaskTypeShell, WorkspaceID: "w", Title: "Shell Awaiting", Status: state.TaskStatus("waiting"), CreatedAt: now, UpdatedAt: now},
			{ID: "ready", TypeID: config.DefaultTaskTypeCodex, WorkspaceID: "w", Title: "Respond", Status: state.StatusRunning, LiveTitle: "Codex Ready", LiveStatus: "Ready", CreatedAt: now, UpdatedAt: now},
			{ID: "failed", TypeID: config.DefaultTaskTypeCodex, WorkspaceID: "w", Title: "Broken", Status: state.StatusError, CreatedAt: now, UpdatedAt: now},
			{ID: "stopped", TypeID: config.DefaultTaskTypeCodex, WorkspaceID: "w", Title: "Paused", Status: state.StatusStopped, CreatedAt: now, UpdatedAt: now},
			{ID: "killed", TypeID: config.DefaultTaskTypeCodex, WorkspaceID: "w", Title: "Killed", Status: state.StatusKilled, CreatedAt: now, UpdatedAt: now},
		},
	}

	got := strings.Join(renderGroupsPaneWithOptions(cfg, st, 42, 14, 99, workspaceRenderOptions{
		loadingFrame: "⠼",
		loadingTasks: map[string]bool{"loading": true},
		taskOperationStartedAt: map[string]time.Time{
			"loading":          time.Now().Add(-12 * time.Second),
			"working":          time.Now().Add(-2 * time.Minute),
			"waiting":          time.Now().Add(-(time.Hour + 2*time.Minute)),
			"terminal-waiting": time.Now().Add(-59 * time.Second),
			"ready":            time.Now().Add(-12 * time.Second),
			"failed":           time.Now().Add(-12 * time.Second),
			"stopped":          time.Now().Add(-12 * time.Second),
			"killed":           time.Now().Add(-12 * time.Second),
		},
	}), "\n")
	stripped := ansi.Strip(got)
	if !taskLineContains(stripped, "Booting", "⠼ [codex] 12s Booting") || strings.Contains(stripped, "· [codex] 12s Booting") {
		t.Fatalf("loading row should show operation duration after the badge:\n%s", stripped)
	}
	if !taskLineContains(stripped, "Review", "⠼ [codex] 2m Review") || strings.Contains(stripped, "· [codex] 2m Review") {
		t.Fatalf("working row should show operation duration after the badge:\n%s", stripped)
	}
	if !taskLineContains(stripped, "Approval", "⠼ [codex] 1h2m Approval") || strings.Contains(stripped, "· [codex] 1h2m Approval") {
		t.Fatalf("waiting Codex row should show operation duration after the badge:\n%s", stripped)
	}
	if !taskLineContains(stripped, "Shell Awaiting", "⠼ [shell] 59s Shell Awaiting") || strings.Contains(stripped, "· [shell] 59s Shell Awaiting") {
		t.Fatalf("waiting terminal row should show operation duration after the badge:\n%s", stripped)
	}
	if !strings.Contains(stripped, "· [codex] Respond") || strings.Contains(stripped, "⠼ [codex] Respond") {
		t.Fatalf("ready row should use the subtle ready marker instead of the spinner:\n%s", stripped)
	}
	if taskLineHasDurationToken(stripped, "Respond") {
		t.Fatalf("ready row should not show operation duration:\n%s", stripped)
	}
	if !strings.Contains(stripped, "! [codex] Broken") {
		t.Fatalf("error row should use the error marker:\n%s", stripped)
	}
	if taskLineHasDurationToken(stripped, "Broken") {
		t.Fatalf("error row should not show operation duration:\n%s", stripped)
	}
	if !strings.Contains(stripped, "◦ [codex] Paused") {
		t.Fatalf("stopped row should use the attention marker:\n%s", stripped)
	}
	if taskLineHasDurationToken(stripped, "Paused") {
		t.Fatalf("stopped row should not show operation duration:\n%s", stripped)
	}
	if !strings.Contains(stripped, "! [codex] Killed") {
		t.Fatalf("killed row should use the attention marker:\n%s", stripped)
	}
	if taskLineHasDurationToken(stripped, "Killed") {
		t.Fatalf("killed row should not show operation duration:\n%s", stripped)
	}
	for _, expected := range []string{
		taskRunningStyle.Render("⠼ [codex] 12s Booting"),
		taskWorkingStyle.Render("⠼ [codex] 2m Review"),
		taskLoadingStyle.Render("⠼ [codex] 1h2m Approval"),
		taskLoadingStyle.Render("⠼ [shell] 59s Shell Awaiting"),
		taskReadyStyle.Render("· [codex] Respond"),
		taskErrorStyle.Render("! [codex] Broken"),
		taskAttentionStyle.Render("◦ [codex] Paused"),
		taskAttentionStyle.Render("! [codex] Killed"),
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("tasks pane missing styled row %q:\n%s", expected, got)
		}
	}
	for _, forbidden := range []string{"running", "working", "error"} {
		if strings.Contains(strings.ToLower(stripped), forbidden) {
			t.Fatalf("task rows should not render fixed status text %q:\n%s", forbidden, stripped)
		}
	}
}

func taskLineContains(capture string, title string, expected string) bool {
	for _, line := range strings.Split(capture, "\n") {
		if strings.Contains(line, title) {
			return strings.Contains(line, expected)
		}
	}
	return false
}

func taskLineHasDurationToken(capture string, title string) bool {
	for _, line := range strings.Split(capture, "\n") {
		if !strings.Contains(line, title) {
			continue
		}
		fields := strings.Fields(strings.Trim(line, " │"))
		for _, field := range fields {
			if isTaskDurationToken(field) {
				return true
			}
		}
	}
	return false
}

func isTaskDurationToken(value string) bool {
	if value == "" {
		return false
	}
	if strings.HasSuffix(value, "s") || (strings.HasSuffix(value, "m") && !strings.Contains(value, "h")) {
		return allASCIIDigits(value[:len(value)-1])
	}
	if strings.HasSuffix(value, "h") {
		return allASCIIDigits(value[:len(value)-1])
	}
	if strings.Contains(value, "h") && strings.HasSuffix(value, "m") {
		parts := strings.SplitN(value, "h", 2)
		return len(parts) == 2 && allASCIIDigits(parts[0]) && allASCIIDigits(strings.TrimSuffix(parts[1], "m"))
	}
	return false
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func TestReadyTaskColorSurvivesSelectionAndActiveFallback(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	now := state.NowISO()
	st.Groups = append(st.Groups, state.Group{ID: "next", WorkspaceID: "w", Path: "next", CreatedAt: now, UpdatedAt: now})

	const width = 42
	rowWidth := width - 2 - (navHorizontalPadding * 2)
	readyRow := "  · [codex] alpha"

	selected := strings.Join(renderGroupsPaneWithOptions(cfg, st, width, 12, 2, workspaceRenderOptions{}), "\n")
	expectedSelected := taskReadySelectedStyle.Render(padVisual(readyRow, rowWidth))
	if !strings.Contains(selected, expectedSelected) {
		t.Fatalf("selected ready task should keep a high-contrast ready style:\n%s", selected)
	}
	if strings.Contains(selected, activeTaskStyle.Render(padVisual(readyRow, rowWidth))) {
		t.Fatalf("selected ready task should not fall back to generic selected styling:\n%s", selected)
	}

	groupSelected := strings.Join(renderGroupsPaneWithOptions(cfg, st, width, 12, 3, workspaceRenderOptions{}), "\n")
	if !strings.Contains(groupSelected, taskReadyStyle.Render(readyRow)) {
		t.Fatalf("active ready task should keep ready color when cursor moves to a group:\n%s", groupSelected)
	}
	if strings.Contains(groupSelected, activePaneStyle.Render(readyRow)) {
		t.Fatalf("active ready task should not fall back to generic active styling:\n%s", groupSelected)
	}
}

func TestRenderWorkspacesPaneEmptyStateIsCenteredHelp(t *testing.T) {
	cfg := config.DefaultConfig()
	st := state.Empty()

	got := strings.Join(renderWorkspacesPaneWithOptions(cfg, st, fixedWorkspacePaneWidth, 12, workspaceRenderOptions{}), "\n")

	if !strings.Contains(got, "No workspaces") || !strings.Contains(got, "Press w to add one.") {
		t.Fatalf("empty workspaces pane missing help:\n%s", got)
	}
	for _, line := range strings.Split(ansi.Strip(got), "\n") {
		if strings.Contains(line, "No workspaces") && strings.Trim(line, " │") != "No workspaces" {
			t.Fatalf("empty workspace help should be centered, got line %q\n%s", line, got)
		}
	}
}

func TestRenderNewWorkspaceTemplateCardUsesItalicTitleAndHint(t *testing.T) {
	cfg := config.DefaultConfig()
	width := 40

	got := strings.Join(renderNewWorkspaceTemplateCard(cfg, width, false, false), "\n")
	topLine := strings.Split(got, "\n")[0]
	titleLabel := " + New workspace "
	expectedTitle := workspaceCardBorderStyle.Italic(true).Render(titleLabel)
	expectedTopTail := workspaceCardBorderStyle.Render(strings.Repeat(borderHorizontal, width-2-lipgloss.Width(titleLabel)) + borderTopRight)
	expectedHint := newWorkspaceCardHintStyle.Render(padVisual(clip(" Press w to create ", width-2), width-2))

	if !strings.Contains(topLine, expectedTitle) {
		t.Fatalf("new workspace card should render italic title %q:\n%s", expectedTitle, got)
	}
	if !strings.Contains(topLine, expectedTopTail) {
		t.Fatalf("new workspace card top border should keep the border style after the title %q:\n%s", expectedTopTail, got)
	}
	if !strings.Contains(got, expectedHint) {
		t.Fatalf("new workspace card should render italic hint %q:\n%s", expectedHint, got)
	}
	if stripped := ansi.Strip(got); !strings.Contains(stripped, "+ New workspace") || !strings.Contains(stripped, "Press w to create") {
		t.Fatalf("new workspace card missing visible copy:\n%s", got)
	}
}

func TestRenderWorkspaceEmptyDashboardShowsNewHint(t *testing.T) {
	cfg := config.DefaultConfig()
	st := state.Empty()

	got := renderWorkspaceView(cfg, st, "Task", "No task open.", 80, 24, "", workspaceNavFrameWidth(st, 80), 0, workspaceRenderOptions{})

	if strings.Contains(got, "Press n to create one.") || !strings.Contains(got, "Add a workspace first.") {
		t.Fatalf("workspace should not advertise task creation before a workspace exists:\n%s", got)
	}

	st = layoutState("/tmp/project")
	st.Tasks = nil
	st.ActiveTaskID = ""
	st.Focus = state.FocusTasks
	st.NavOpen = true
	got = renderWorkspaceView(cfg, st, "Task", "No task open.", 80, 24, "", workspaceNavFrameWidth(st, 80), 0, workspaceRenderOptions{})
	if !strings.Contains(got, "Press n to create") {
		t.Fatalf("workspace missing task creation hint:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	if strings.Contains(lines[len(lines)-1], "Codex") {
		t.Fatalf("empty dashboard should not render default codex title in bottom border:\n%s", got)
	}

	got = renderWorkspaceView(cfg, st, "Task", "No task open.", 100, 24, "", 0, 0, workspaceRenderOptions{})
	stripped := ansi.Strip(got)
	logoIndex := strings.Index(stripped, `◆━━━━━╋━━━━━➤ ██║ █╗ ██║ █████╗   █████╗      ██║`)
	hintIndex := strings.Index(stripped, "No task open")
	if logoIndex < 0 {
		t.Fatalf("empty dashboard missing Weft wordmark:\n%s", stripped)
	}
	if hintIndex < 0 || logoIndex > hintIndex {
		t.Fatalf("empty dashboard should render wordmark above existing hint:\n%s", stripped)
	}
	if strings.Contains(stripped, weftversion.Label()) {
		t.Fatalf("empty task pane should not render dashboard version text outside the Workspaces pane:\n%s", stripped)
	}
	if strings.Contains(stripped, "Task Live Preview") || strings.Contains(stripped, " … │") {
		t.Fatalf("empty command center should not show cropped preview styling:\n%s", stripped)
	}

	content := renderEmptyCodexContentWithFrame(100, 24, "No task open", "Press n to create one.", false, 0)
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
	if got := ansi.Strip(content[logoStart+len(emptyWeftLogo)+1]); !strings.HasPrefix(got, expectedLeft+centerVisual("No task open", logoWidth)) {
		t.Fatalf("empty title should align inside centered logo block, got %q", got)
	}
	if got := ansi.Strip(content[logoStart+len(emptyWeftLogo)+2]); !strings.HasPrefix(got, expectedLeft+centerVisual("Press n to create one.", logoWidth)) {
		t.Fatalf("empty hint should align inside centered logo block, got %q", got)
	}
}

func TestRenderWorkspaceLoadingStateIsCentered(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusConsole
	st.NavOpen = false
	st.Tasks[0].Status = state.StatusStarting

	got := renderWorkspaceView(cfg, st, "alpha", "", 80, 24, "", 0, 0, workspaceRenderOptions{loadingText: "⠋ Starting Codex"})
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
	st.Focus = state.FocusConsole
	st.NavOpen = false

	got := renderWorkspaceView(cfg, st, "alpha", "output", 80, 24, "", 0, 0, workspaceRenderOptions{})

	if strings.Contains(got, "●") {
		t.Fatalf("active dot indicator should not render:\n%s", got)
	}
	if !strings.Contains(got, "C-b dashboard") {
		t.Fatalf("collapsed codex top toolbar missing drawer shortcuts:\n%s", got)
	}
	if !strings.Contains(got, "C-] tools") {
		t.Fatalf("collapsed codex top toolbar missing tools key:\n%s", got)
	}
	if strings.Contains(got, "WEFT") {
		t.Fatalf("collapsed codex top toolbar should not include WEFT branding:\n%s", got)
	}
	if strings.Contains(got, "C-c") {
		t.Fatalf("collapsed codex top toolbar should not advertise C-c:\n%s", got)
	}
	if !strings.Contains(got, "Task Console") {
		t.Fatalf("focused codex pane should render Task Console title:\n%s", got)
	}
	if strings.Contains(got, "Task Live Preview") || strings.Contains(got, "● Live") {
		t.Fatalf("focused codex pane should not render live preview UI:\n%s", got)
	}
	st.Tasks[0].Status = state.StatusRunning
	st.Tasks[0].LiveTitle = "Fake Codex Working"
	st.Tasks[0].LiveStatus = "Working"
	got = renderWorkspaceView(cfg, st, "alpha", "output", 80, 24, "", 0, 0, workspaceRenderOptions{})
	if !strings.Contains(got, "C-b dashboard") || !strings.Contains(got, "C-] tools") || strings.Contains(got, "WEFT") || strings.Contains(got, "C-c") {
		t.Fatalf("working codex toolbar should advertise only Weft-owned console shortcuts:\n%s", got)
	}
}

func TestTaskContextHeadingRendersOnlyFocusedCodexConsole(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusConsole
	st.NavOpen = false
	heading := "Investigate failing release workflow"

	got := ansi.Strip(renderWorkspaceView(cfg, st, "alpha", "output", 96, 10, "", 0, 0, workspaceRenderOptions{taskContextHeading: heading}))
	if count := strings.Count(got, " note "); count != 1 {
		t.Fatalf("focused Codex console should render one note label, count=%d:\n%s", count, got)
	}
	if !strings.Contains(got, heading) {
		t.Fatalf("focused Codex console should render the full note when border space is available:\n%s", got)
	}
	if strings.Contains(got, "["+heading+"]") {
		t.Fatalf("task note should not render centered in brackets:\n%s", got)
	}
	if strings.Contains(got, "Context:") {
		t.Fatalf("task note should not render as a body banner:\n%s", got)
	}
	if strings.Index(got, " note ") > strings.Index(got, "output") {
		t.Fatalf("task note should render in the top border before task output:\n%s", got)
	}

	st.Focus = state.FocusTasks
	st.NavOpen = true
	got = ansi.Strip(renderWorkspaceView(cfg, st, "alpha", "output", 140, 18, "", minTwoPaneNavWidth, 2, workspaceRenderOptions{taskContextHeading: heading}))
	if strings.Contains(got, heading) {
		t.Fatalf("Task Live Preview should not render task notes:\n%s", got)
	}

	st.Focus = state.FocusConsole
	st.NavOpen = false
	st.Tasks[0].TypeID = config.DefaultTaskTypeShell
	got = ansi.Strip(renderWorkspaceView(cfg, st, "alpha", "output", 96, 10, "", 0, 0, workspaceRenderOptions{taskContextHeading: heading}))
	if strings.Contains(got, heading) {
		t.Fatalf("shell task console should not render task notes:\n%s", got)
	}
}

func TestTaskContextHeadingUsesAvailableConsoleHeaderWidth(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusConsole
	st.NavOpen = false
	st.Tasks[0].Title = "A"
	heading := "Workflow: https://github.com/example/repo/actions/runs/1234567890"

	got := ansi.Strip(renderWorkspaceView(cfg, st, "A", "output", 132, 10, "", 0, 0, workspaceRenderOptions{taskContextHeading: heading}))
	if !strings.Contains(got, heading) {
		t.Fatalf("focused Codex console should use available header width before truncating:\n%s", got)
	}

	got = ansi.Strip(renderWorkspaceView(cfg, st, "A", "output", 78, 10, "", 0, 0, workspaceRenderOptions{taskContextHeading: heading}))
	if !strings.Contains(got, "Workflow: https://githu") || !strings.Contains(got, "…") {
		t.Fatalf("focused Codex console should truncate only when header space runs out:\n%s", got)
	}
}

func TestTaskContextHeadingPreservesStatusBanner(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusConsole
	st.NavOpen = false

	got := ansi.Strip(renderWorkspaceView(cfg, st, "alpha", "output", 90, 12, "Upgrade pending: wait for Codex task.", 0, 0, workspaceRenderOptions{taskContextHeading: "Review PR 123"}))

	for _, expected := range []string{"Upgrade: pending", " note ", "Review PR 123", "output"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("console missing %q:\n%s", expected, got)
		}
	}
}

func TestTaskConsoleReadyIndicatorCountsOtherGlobalReadyTasks(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	cfg := config.DefaultConfig()
	now := state.NowISO()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusConsole
	st.NavOpen = false
	st.Groups = append(st.Groups, state.Group{ID: "silent-group", WorkspaceID: "w", Path: "quiet", Silent: true, CreatedAt: now, UpdatedAt: now})
	st.Tasks = append(st.Tasks,
		state.Task{ID: "b", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "beta", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now},
		state.Task{ID: "c", WorkspaceID: "w2", TypeID: config.DefaultTaskTypeCodex, Title: "gamma", Status: state.StatusRunning, LiveTitle: "Codex Ready", LiveStatus: "Ready", CreatedAt: now, UpdatedAt: now},
		state.Task{ID: "d", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "delta", Status: state.StatusRunning, LiveTitle: "Codex Working", LiveStatus: "Working", CreatedAt: now, UpdatedAt: now},
		state.Task{ID: "e", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "silent", Status: state.StatusReady, Silent: true, CreatedAt: now, UpdatedAt: now},
		state.Task{ID: "f", WorkspaceID: "w", GroupID: "silent-group", TypeID: config.DefaultTaskTypeCodex, Title: "group silent", Status: state.StatusRunning, LiveTitle: "Codex Ready", LiveStatus: "Ready", CreatedAt: now, UpdatedAt: now},
	)

	got := renderWorkspaceView(cfg, st, "alpha", "output", 100, 18, "", 0, 0, workspaceRenderOptions{})
	rawLines := strings.Split(got, "\n")
	stripped := ansi.Strip(got)
	strippedLines := strings.Split(stripped, "\n")
	topLine := strippedLines[0]
	bottomLine := strippedLines[len(strippedLines)-1]
	if !strings.Contains(bottomLine, "2 other tasks ready") {
		t.Fatalf("console should show ready indicator for other global tasks:\n%s", got)
	}
	if strings.Contains(topLine, "other task") {
		t.Fatalf("ready indicator should render in the bottom border, not the top border:\n%s", stripped)
	}
	if !strings.Contains(rawLines[len(rawLines)-1], workspaceCountNeedsAttentionStyle.Render("2 other tasks ready")) {
		t.Fatalf("ready indicator should use needs-attention styling:\n%q", got)
	}

	st.Tasks = st.Tasks[:1]
	got = renderWorkspaceView(cfg, st, "alpha", "output", 100, 18, "", 0, 0, workspaceRenderOptions{})
	if strings.Contains(ansi.Strip(got), "other task") {
		t.Fatalf("console should hide ready indicator when no other tasks are ready:\n%s", got)
	}
}

func TestTaskConsoleChromePlacesTitleTopAndNoticesBottom(t *testing.T) {
	cfg := config.DefaultConfig()
	now := state.NowISO()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusConsole
	st.NavOpen = false
	st.Tasks = append(st.Tasks, state.Task{ID: "b", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "beta", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now})

	got := ansi.Strip(renderWorkspaceView(cfg, st, "Alpha Task", "output", 100, 18, "", 0, 0, workspaceRenderOptions{
		codexToastText: "Copied 4 characters",
	}))
	lines := strings.Split(got, "\n")
	topLine := lines[0]
	bottomLine := lines[len(lines)-1]

	if !strings.Contains(topLine, "Task Console") || !strings.Contains(topLine, "Alpha Task") {
		t.Fatalf("console top border should include pane title and task title:\n%s", got)
	}
	for _, unexpected := range []string{"Copied 4 characters", "other task"} {
		if strings.Contains(topLine, unexpected) {
			t.Fatalf("console top-right should show only the task title, but included %q:\n%s", unexpected, got)
		}
	}
	for _, expected := range []string{"Copied 4 characters", "1 other task ready"} {
		if !strings.Contains(bottomLine, expected) {
			t.Fatalf("console bottom border should include %q:\n%s", expected, got)
		}
	}
	if strings.Contains(bottomLine, "Alpha Task") {
		t.Fatalf("console bottom border should not include the task title:\n%s", got)
	}
}

func TestTaskConsoleBottomNoticeKeepsActiveBorderCornerStyle(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(previous)

	cfg := config.DefaultConfig()
	now := state.NowISO()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusConsole
	st.NavOpen = false
	st.Tasks = append(st.Tasks, state.Task{ID: "b", WorkspaceID: "w", TypeID: config.DefaultTaskTypeCodex, Title: "beta", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now})

	got := renderWorkspaceView(cfg, st, "Alpha Task", "output", 100, 18, "", 0, 0, workspaceRenderOptions{
		codexToastText: "Copied 4 characters",
	})
	lines := strings.Split(got, "\n")
	bottomLine := lines[len(lines)-1]

	if !strings.Contains(bottomLine, workspaceCountNeedsAttentionStyle.Render("1 other task ready")) {
		t.Fatalf("bottom notice should keep its own highlight style:\n%q", bottomLine)
	}
	if !strings.HasSuffix(bottomLine, activePalette.border.Render(borderHorizontal+borderBottomRight)) {
		t.Fatalf("bottom-right border corner should stay in the active pane border style:\n%q", bottomLine)
	}
}

func TestCodexLeftPaddingStaysBeforeLeadingANSIStyle(t *testing.T) {
	cfg := config.DefaultConfig()
	st := layoutState("/tmp/project")
	st.Focus = state.FocusConsole
	st.NavOpen = false

	got := renderWorkspaceView(cfg, st, "alpha", "\x1b[48;2;1;2;3mZ\x1b[m", 40, 8, "", 0, 0, workspaceRenderOptions{})

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
	got := renderWorkspaceView(cfg, st, "alpha", "output", 100, 18, "", 60, 2, workspaceRenderOptions{})

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
		got := ansi.Strip(renderFrameBorderLine(inactivePalette, borderTopLeft, borderTopRight, "", innerWidth))
		if got != want {
			t.Fatalf("renderFrameBorderLine(%d) = %q, want %q", innerWidth, got, want)
		}
	}
}

func layoutState(workspace string) state.State {
	now := state.NowISO()
	return state.State{
		Version:             state.Version,
		ActiveTaskID:        "a",
		SelectedWorkspaceID: "w",
		SelectedGroupID:     "f",
		Focus:               state.FocusTasks,
		NavOpen:             true,
		Workspaces:          []state.Workspace{{ID: "w", Path: workspace, CreatedAt: now, UpdatedAt: now}},
		Groups:              []state.Group{{ID: "f", WorkspaceID: "w", Path: "inbox", CreatedAt: now, UpdatedAt: now}},
		Tasks:               []state.Task{{ID: "a", WorkspaceID: "w", GroupID: "f", TypeID: config.DefaultTaskTypeCodex, Title: "alpha", Status: state.StatusReady, CreatedAt: now, UpdatedAt: now}},
	}
}
