package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/pathx"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/titles"
)

const (
	appTitle                = "WEFT"
	codexLeftPadding        = 1
	navHorizontalPadding    = 1
	fixedWorkspacePaneWidth = 64
	minAgentsPaneWidth      = 28
	defaultAgentsPaneWidth  = 48
	minCodexPaneWidth       = 28
	minTwoPaneNavWidth      = fixedWorkspacePaneWidth + minAgentsPaneWidth
	minThreePaneWidth       = minTwoPaneNavWidth + minCodexPaneWidth
	borderHorizontal        = "─"
	borderVertical          = "│"
	borderTopLeft           = "╭"
	borderTopRight          = "╮"
	borderBottomLeft        = "╰"
	borderBottomRight       = "╯"
)

var (
	mutedStyle                        = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	headerStyle                       = lipgloss.NewStyle().Underline(true)
	activeAgentStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("117"))
	activePaneStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	groupHeaderStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	modalStyle                        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("117")).Padding(1, 2)
	modalInputStyle                   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("244")).Padding(0, 1)
	modalTitleStyle                   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	modalLabelStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true)
	modalValueStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	modalTokenStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	modalKeyStyle                     = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	modalSuccessStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	modalWarningStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	modalErrorStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	modalSuggestionSelectedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("117")).Bold(true)
	workspaceCardBorderStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	workspaceCardSelectedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	workspaceCardSelectedFocusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	workspaceCountMutedStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	workspaceCountActiveStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	workspaceCountNeedsAttentionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	workspacePathWarningStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	emptyLogoStyle                    = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	previewCropMarkerStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

type framePalette struct {
	border lipgloss.Style
}

var (
	activePalette   = framePalette{border: lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)}
	inactivePalette = framePalette{border: lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Bold(true)}
)

var (
	emptyWeftLogo = []string{
		`             ██╗    ██╗ ███████╗ ███████╗ █████████╗`,
		`●──╮         ██║    ██║ ██╔════╝ ██╔════╝ ╚══██╔══╝`,
		`●──┼──▶      ██║ █╗ ██║ █████╗   █████╗      ██║`,
		`●──╯         ██║███╗██║ ██╔══╝   ██╔══╝      ██║`,
		`             ╚███╔███╔╝ ███████╗ ██║         ██║`,
		`              ╚══╝╚══╝  ╚══════╝ ╚═╝         ╚═╝`,
	}
)

func WeftLogoLines() []string {
	return append([]string(nil), emptyWeftLogo...)
}

func workspaceNavFrameWidth(st state.State, width int) int {
	if !st.NavOpen {
		return 0
	}
	if width < 42 {
		return width
	}
	if width < minTwoPaneNavWidth {
		return min(width-minCodexPaneWidth, 44)
	}
	if width < minThreePaneWidth {
		return width
	}
	agentsWidth := min(defaultAgentsPaneWidth, width-fixedWorkspacePaneWidth-minCodexPaneWidth)
	return fixedWorkspacePaneWidth + max(minAgentsPaneWidth, agentsWidth)
}

func renderWorkspace(
	cfg config.Config,
	st state.State,
	codexTitle string,
	codexContent string,
	width int,
	height int,
	message string,
	_ string,
) string {
	return renderWorkspaceWithNavWidth(cfg, st, codexTitle, codexContent, width, height, message, workspaceNavFrameWidth(st, width), 0)
}

func renderWorkspaceWithNavWidth(
	cfg config.Config,
	st state.State,
	codexTitle string,
	codexContent string,
	width int,
	height int,
	message string,
	navWidth int,
	groupCursor int,
) string {
	return renderWorkspaceView(cfg, st, codexTitle, codexContent, width, height, message, navWidth, groupCursor, "")
}

func renderLoadingWorkspaceWithNavWidth(
	cfg config.Config,
	st state.State,
	codexTitle string,
	loadingText string,
	width int,
	height int,
	message string,
	navWidth int,
	groupCursor int,
) string {
	return renderWorkspaceView(cfg, st, codexTitle, "", width, height, message, navWidth, groupCursor, loadingText)
}

func renderWorkspaceView(
	cfg config.Config,
	st state.State,
	codexTitle string,
	codexContent string,
	width int,
	height int,
	message string,
	navWidth int,
	groupCursor int,
	loadingText string,
) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	navWidth = min(max(0, navWidth), width)
	codexWidth := width - navWidth
	navOnly := navWidth >= width
	if !navOnly && codexWidth < minCodexPaneWidth && navWidth > 0 {
		codexWidth = min(width, minCodexPaneWidth)
		navWidth = width - codexWidth
	}
	if navWidth <= 0 {
		return strings.Join(renderCodexFrame(cfg, st, codexTitle, codexContent, width, height, st.Focus == state.FocusCodex, message, true, loadingText), "\n")
	}
	if codexWidth <= 0 {
		return strings.Join(renderNavSection(cfg, st, navWidth, height, groupCursor), "\n")
	}
	nav := renderNavSection(cfg, st, navWidth, height, groupCursor)
	codex := renderCodexFrame(cfg, st, codexTitle, codexContent, codexWidth, height, false, message, false, loadingText)
	lines := make([]string, 0, height)
	for index := 0; index < height; index++ {
		left := lineAt(nav, index, navWidth)
		right := lineAt(codex, index, codexWidth)
		lines = append(lines, clip(left+right, width))
	}
	return strings.Join(lines, "\n")
}

func renderNavSection(cfg config.Config, st state.State, width int, height int, groupCursor int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	if width >= minTwoPaneNavWidth {
		workspaceWidth := min(fixedWorkspacePaneWidth, max(0, width-minAgentsPaneWidth))
		groupWidth := width - workspaceWidth
		workspaces := renderWorkspacesPane(cfg, st, workspaceWidth, height)
		groups := renderGroupsPane(cfg, st, groupWidth, height, groupCursor)
		lines := make([]string, 0, height)
		for index := 0; index < height; index++ {
			lines = append(lines, lineAt(workspaces, index, workspaceWidth)+lineAt(groups, index, groupWidth))
		}
		return lines
	}
	if st.Focus == state.FocusWorkspaces {
		return renderWorkspacesPane(cfg, st, width, height)
	}
	return renderGroupsPane(cfg, st, width, height, groupCursor)
}

func renderWorkspacesPane(cfg config.Config, st state.State, width int, height int) []string {
	content := []string{}
	cardWidth := max(2, width-2-(navHorizontalPadding*2))
	for _, workspace := range st.Workspaces {
		selected := workspace.ID == st.SelectedWorkspaceID
		card := renderWorkspaceCard(cfg, st, workspace, cardWidth, selected, st.Focus == state.FocusWorkspaces)
		for _, line := range card {
			content = append(content, strings.Repeat(" ", navHorizontalPadding)+line)
		}
	}
	if len(content) == 0 {
		content = renderCenteredPaneHelp(width, height, "No workspaces", "Press "+cfg.KeyBindings.NewWorkspace+" to add one.")
	}
	return renderPaneFrame("Workspaces", "", width, height, st.Focus == state.FocusWorkspaces, content)
}

type workspaceCardCounts struct {
	total          int
	active         int
	needsAttention int
}

func renderWorkspaceCard(cfg config.Config, st state.State, workspace state.Workspace, width int, selected bool, focused bool) []string {
	if width < 2 {
		return []string{""}
	}
	borderStyle := workspaceCardBorderStyle
	if selected && focused {
		borderStyle = workspaceCardSelectedFocusedStyle
	} else if selected {
		borderStyle = workspaceCardSelectedStyle
	}
	innerWidth := max(0, width-2)
	title := workspaceCardTitle(workspace)
	top := borderStyle.Render(workspaceCardTopLine(title, width))
	counts := workspaceCardCountsForWorkspace(st, workspace.ID)
	body := borderStyle.Render(borderVertical) + renderWorkspaceCardCounts(counts, innerWidth) + borderStyle.Render(borderVertical)
	lines := []string{top, body}
	if warning := workspaceCardPathWarning(cfg, workspace, innerWidth); warning != "" {
		lines = append(lines, borderStyle.Render(borderVertical)+warning+borderStyle.Render(borderVertical))
	}
	bottom := borderStyle.Render(workspaceCardBottomLine(width))
	lines = append(lines, bottom)
	return lines
}

func workspaceCardTopLine(title string, width int) string {
	if width < 2 {
		return ""
	}
	contentWidth := max(0, width-2)
	label := " " + strings.TrimSpace(title) + " "
	label = clip(label, contentWidth)
	padding := max(0, contentWidth-lipgloss.Width(label))
	return borderTopLeft + label + strings.Repeat(borderHorizontal, padding) + borderTopRight
}

func workspaceCardBottomLine(width int) string {
	if width < 2 {
		return ""
	}
	return borderBottomLeft + strings.Repeat(borderHorizontal, max(0, width-2)) + borderBottomRight
}

func workspaceCardTitle(workspace state.Workspace) string {
	if title := strings.TrimSpace(workspace.Title); title != "" {
		return title
	}
	return pathx.Display(workspace.Path)
}

func workspaceCardPathWarning(cfg config.Config, workspace state.Workspace, width int) string {
	issue := workspacePathIssue(workspace.Path)
	if issue == "" || width <= 0 {
		return ""
	}
	message := issue + "; press " + cfg.KeyBindings.Delete + " to remove"
	return workspacePathWarningStyle.Render(padVisual(clip(" "+message, width), width))
}

func workspacePathIssue(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "path missing"
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "path missing"
		}
		return "path unreadable"
	}
	if !info.IsDir() {
		return "path is not a directory"
	}
	return ""
}

func workspaceCardCountsForWorkspace(st state.State, workspaceID string) workspaceCardCounts {
	counts := workspaceCardCounts{}
	for _, agent := range st.Agents {
		if agent.WorkspaceID != workspaceID {
			continue
		}
		counts.total++
		if workspaceCardAgentActive(agent) {
			counts.active++
		}
	}
	counts.needsAttention = counts.total - counts.active
	return counts
}

func workspaceCardAgentActive(agent state.Agent) bool {
	switch titles.RenderStatus(agent) {
	case string(state.StatusStarting), string(state.StatusRunning), "working", string(state.StatusShipping):
		return true
	default:
		return false
	}
}

func renderWorkspaceCardCounts(counts workspaceCardCounts, width int) string {
	if width <= 0 {
		return ""
	}
	labels := workspaceCardCountLabels(counts)
	styles := []lipgloss.Style{
		workspaceCountMutedStyle,
		workspaceActiveStyle(counts),
		workspaceNeedsAttentionStyle(counts),
	}
	sum := 0
	for _, label := range labels {
		sum += lipgloss.Width(label)
	}
	const (
		leftPadding  = 1
		rightPadding = 1
	)
	gapTotal := width - leftPadding - rightPadding - sum
	if gapTotal < 4 {
		return workspaceCountMutedStyle.Render(padVisual(clip(" "+strings.Join(labels, "  ")+" ", width), width))
	}
	gap := max(2, gapTotal/(len(labels)-1))
	remainder := gapTotal - gap*(len(labels)-1)
	var builder strings.Builder
	builder.WriteString(" ")
	for index, label := range labels {
		if index > 0 {
			spaces := gap
			if remainder > 0 {
				spaces++
				remainder--
			}
			builder.WriteString(strings.Repeat(" ", spaces))
		}
		builder.WriteString(styles[index].Render(label))
	}
	return padVisual(builder.String(), width)
}

func workspaceActiveStyle(counts workspaceCardCounts) lipgloss.Style {
	if counts.active == 0 {
		return workspaceCountMutedStyle
	}
	return workspaceCountActiveStyle
}

func workspaceNeedsAttentionStyle(counts workspaceCardCounts) lipgloss.Style {
	if counts.needsAttention == 0 {
		return workspaceCountMutedStyle
	}
	return workspaceCountNeedsAttentionStyle
}

func workspaceCardCountLabels(counts workspaceCardCounts) []string {
	return []string{
		fmtInt(counts.total) + " total",
		fmtInt(counts.active) + " active",
		fmtInt(counts.needsAttention) + " needs attention",
	}
}

func renderGroupsPane(cfg config.Config, st state.State, width int, height int, groupCursor int) []string {
	content := []string{}
	rowIndex := 0
	for _, agent := range state.UngroupedAgentsForWorkspace(st, st.SelectedWorkspaceID) {
		title := renderAgentTitleForState(cfg, st, agent)
		agentRow := "• " + title
		agentRow = clip(agentRow, max(0, width-2-(navHorizontalPadding*2)))
		if rowIndex == groupCursor && st.Focus == state.FocusAgents {
			agentRow = activeAgentStyle.Render(padVisual(agentRow, max(0, width-2-(navHorizontalPadding*2))))
		} else if agent.ID == st.ActiveAgentID {
			agentRow = activePaneStyle.Render(agentRow)
		}
		content = append(content, strings.Repeat(" ", navHorizontalPadding)+agentRow)
		rowIndex++
	}
	for _, group := range state.GroupsForWorkspace(st, st.SelectedWorkspaceID) {
		if rowIndex > 0 {
			content = append(content, "")
		}
		indicator := "▾ "
		if state.IsGroupCollapsed(st, group.ID) {
			indicator = "▸ "
		}
		groupRow := rowLine(indicator+group.Path, fmtInt(state.AgentCountForGroup(st, group.ID)), max(0, width-2-(navHorizontalPadding*2)))
		if rowIndex == groupCursor && st.Focus == state.FocusAgents {
			groupRow = activeAgentStyle.Render(padVisual(groupRow, max(0, width-2-(navHorizontalPadding*2))))
		} else {
			groupRow = groupHeaderStyle.Render(groupRow)
		}
		content = append(content, strings.Repeat(" ", navHorizontalPadding)+groupRow)
		rowIndex++
		if state.IsGroupCollapsed(st, group.ID) {
			continue
		}
		for _, agent := range state.AgentsForGroup(st, group.ID) {
			title := renderAgentTitleForState(cfg, st, agent)
			agentRow := "  • " + title
			agentRow = clip(agentRow, max(0, width-2-(navHorizontalPadding*2)))
			if rowIndex == groupCursor && st.Focus == state.FocusAgents {
				agentRow = activeAgentStyle.Render(padVisual(agentRow, max(0, width-2-(navHorizontalPadding*2))))
			} else if agent.ID == st.ActiveAgentID {
				agentRow = activePaneStyle.Render(agentRow)
			}
			content = append(content, strings.Repeat(" ", navHorizontalPadding)+agentRow)
			rowIndex++
		}
	}
	if len(content) == 0 {
		if state.ActiveWorkspace(st) == nil {
			content = renderCenteredPaneHelp(width, height, "No workspace selected", "Press "+cfg.KeyBindings.NewWorkspace+" to add one.")
		} else {
			content = renderCenteredPaneHelp(width, height, "No agents", "Press "+cfg.KeyBindings.NewAgent+" to create one.")
		}
	}
	return renderPaneFrame("Agents", "", width, height, st.Focus == state.FocusAgents, content)
}

func renderCenteredPaneHelp(width int, height int, lines ...string) []string {
	contentHeight := max(0, height-2)
	innerWidth := max(0, width-2)
	if contentHeight == 0 {
		return nil
	}
	help := make([]string, 0, len(lines))
	for index, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		style := mutedStyle
		if index == 0 {
			style = modalValueStyle
		}
		help = append(help, centerVisual(style.Render(line), innerWidth))
	}
	if len(help) == 0 {
		return make([]string, contentHeight)
	}
	topPadding := max(0, (contentHeight-len(help))/2)
	content := make([]string, 0, contentHeight)
	for len(content) < topPadding {
		content = append(content, "")
	}
	content = append(content, help...)
	for len(content) < contentHeight {
		content = append(content, "")
	}
	return content[:contentHeight]
}

func renderPaneFrame(title string, right string, width int, height int, active bool, content []string) []string {
	if width < 2 || height <= 0 {
		return nil
	}
	palette := paletteFor(active)
	innerWidth := max(0, width-2)
	lines := []string{
		palette.border.Render(cornerLine(borderTopLeft, borderTopRight, borderTextLine(title, right, max(0, innerWidth-2)), innerWidth)),
	}
	contentHeight := max(0, height-2)
	for len(content) < contentHeight {
		content = append(content, "")
	}
	for _, line := range content[:contentHeight] {
		lines = append(lines, palette.border.Render(borderVertical)+padVisual(clip(line, innerWidth), innerWidth)+palette.border.Render(borderVertical))
	}
	lines = append(lines, palette.border.Render(cornerLine(borderBottomLeft, borderBottomRight, "", innerWidth)))
	return lines
}

func renderCodexFrame(
	cfg config.Config,
	st state.State,
	title string,
	content string,
	width int,
	height int,
	active bool,
	_ string,
	navCollapsed bool,
	loadingText string,
) []string {
	if width < 2 || height <= 0 {
		return nil
	}
	palette := paletteFor(active)
	innerWidth := max(0, width-2)
	agentActive := state.ActiveAgent(st) != nil
	previewMode := !navCollapsed
	topLabel := "Agent Preview"
	topRightLabel := ""
	if navCollapsed && active {
		topLabel = "Agent Console  " + codexCollapsedTopShortcuts(cfg)
	} else if navCollapsed {
		topLabel = "Agent Console"
	} else if agentActive {
		topRightLabel = "live · cropped"
	}
	lines := []string{
		palette.border.Render(cornerLine(borderTopLeft, borderTopRight, borderTextLine(topLabel, topRightLabel, max(0, innerWidth-2)), innerWidth)),
	}
	contentHeight := max(0, height-2)
	empty := !agentActive
	contentLines := renderCodexContent(content, max(0, innerWidth-codexLeftPadding), contentHeight, empty, len(st.Workspaces) > 0, loadingText)
	for len(contentLines) < contentHeight {
		contentLines = append(contentLines, "")
	}
	for _, line := range contentLines[:contentHeight] {
		contentWidth := codexLineContentWidth(innerWidth)
		renderedLine := padVisual(clip(line, contentWidth), contentWidth)
		if previewMode && !empty {
			renderedLine = previewClipLine(line, contentWidth)
		}
		lines = append(lines, palette.border.Render(borderVertical)+codexLeftPad(innerWidth)+renderedLine+palette.border.Render(borderVertical))
	}
	rightLabel := ""
	if agentActive {
		rightLabel = title
	}
	lines = append(lines, palette.border.Render(cornerLine(borderBottomLeft, borderBottomRight, borderTextLine("", rightLabel, max(0, innerWidth-2)), innerWidth)))
	return lines
}

func renderCodexContent(content string, width int, height int, empty bool, canCreateAgent bool, loadingText string) []string {
	if height <= 0 {
		return nil
	}
	if strings.TrimSpace(loadingText) != "" {
		return renderCenteredCodexContent([]string{loadingText}, width, height)
	}
	if empty {
		return renderEmptyCodexContent(width, height, canCreateAgent)
	}
	lines := lastLines(content, height)
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func renderEmptyCodexContent(width int, height int, canCreateAgent bool) []string {
	hint := "Press n to create one."
	if !canCreateAgent {
		hint = "Add a workspace first."
	}
	content := []string{}
	if logoFits(emptyWeftLogo, width, height) {
		logoWidth := maxVisualWidth(emptyWeftLogo)
		for _, line := range emptyWeftLogo {
			content = append(content, emptyLogoStyle.Render(padVisual(line, logoWidth)))
		}
		content = append(content, "")
		content = append(content, centerVisual("No Codex agent open", logoWidth), centerVisual(hint, logoWidth))
		return renderCenteredCodexBlockContent(content, width, height, logoWidth)
	}
	content = append(content, "No Codex agent open", hint)
	return renderCenteredCodexContent(content, width, height)
}

func logoFits(logo []string, width int, height int) bool {
	if len(logo) == 0 || height < len(logo)+3 {
		return false
	}
	for _, line := range logo {
		if lipgloss.Width(line) > width {
			return false
		}
	}
	return true
}

func maxVisualWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		width = max(width, lipgloss.Width(line))
	}
	return width
}

func renderCenteredCodexBlockContent(content []string, width int, height int, blockWidth int) []string {
	topPadding := max(0, (height-len(content))/2)
	leftPadding := max(0, (width-blockWidth)/2)
	lines := make([]string, 0, height)
	for len(lines) < topPadding {
		lines = append(lines, "")
	}
	for _, line := range content {
		lines = append(lines, strings.Repeat(" ", leftPadding)+padVisual(clip(line, blockWidth), blockWidth))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines[:height]
}

func renderCenteredCodexContent(content []string, width int, height int) []string {
	topPadding := max(0, (height-len(content))/2)
	lines := make([]string, 0, height)
	for len(lines) < topPadding {
		lines = append(lines, "")
	}
	for _, line := range content {
		lines = append(lines, centerVisual(line, width))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines[:height]
}

func codexCollapsedTopShortcuts(cfg config.Config) string {
	return appTitle + "  " + cfg.KeyBindings.Drawer + " dashboard  " + cfg.KeyBindings.Quit + " to Codex"
}

func paletteFor(active bool) framePalette {
	if active {
		return activePalette
	}
	return inactivePalette
}

func cornerLine(leftCorner string, rightCorner string, content string, innerWidth int) string {
	if innerWidth <= 0 {
		return leftCorner + rightCorner
	}
	if innerWidth == 1 {
		return leftCorner + borderHorizontal + rightCorner
	}
	contentWidth := innerWidth - 2
	return leftCorner + borderHorizontal + padVisual(clip(content, contentWidth), contentWidth) + borderHorizontal + rightCorner
}

func borderTextLine(left string, right string, width int) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if width <= 0 {
		return ""
	}
	if right != "" {
		right = " " + right
	}
	if left != "" {
		left += " "
	}
	left = clip(left, width)
	remaining := width - lipgloss.Width(left) - lipgloss.Width(right)
	if remaining < 0 {
		right = clip(right, max(0, width-lipgloss.Width(left)))
		remaining = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if remaining < 0 {
		left = clip(left, width)
		right = ""
		remaining = width - lipgloss.Width(left)
	}
	return left + strings.Repeat(" ", max(0, remaining)) + right
}

func rowLine(left string, right string, width int) string {
	return borderTextLine(left, right, width)
}

func centerVisual(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = clip(value, width)
	padding := max(0, width-lipgloss.Width(value))
	left := padding / 2
	return strings.Repeat(" ", left) + value
}

func lastLines(content string, maxLines int) []string {
	content = strings.ReplaceAll(content, "\r", "")
	lines := strings.Split(content, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

func padVisual(value string, width int) string {
	if width <= 0 {
		return ""
	}
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func codexLeftPad(width int) string {
	if width <= 0 {
		return ""
	}
	return strings.Repeat(" ", min(codexLeftPadding, width))
}

func codexLineContentWidth(width int) int {
	if width <= 0 {
		return 0
	}
	return max(0, width-min(codexLeftPadding, width))
}

func previewClipLine(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return padVisual(value, width)
	}
	marker := previewCropMarkerStyle.Render(" …")
	markerWidth := lipgloss.Width(marker)
	contentWidth := max(0, width-markerWidth)
	return padVisual(clipPlain(value, contentWidth), contentWidth) + marker
}

func clipPlain(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

func clip(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width-1 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func lineAt(lines []string, index int, width int) string {
	if index < 0 || index >= len(lines) {
		return strings.Repeat(" ", width)
	}
	return padVisual(clip(lines[index], width), width)
}

func workspaceForRender(st state.State, agent state.Agent) state.Workspace {
	if workspace := state.WorkspaceForAgent(st, agent); workspace != nil {
		return *workspace
	}
	return state.Workspace{}
}

func fmtInt(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
