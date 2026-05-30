package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/sessions"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/titles"
)

const (
	appTitle               = "WEFT"
	codexLeftPadding       = 1
	navHorizontalPadding   = 1
	fixedWorkdirPaneWidth  = 64
	minAgentsPaneWidth     = 28
	defaultAgentsPaneWidth = 48
	minCodexPaneWidth      = 28
	minTwoPaneNavWidth     = fixedWorkdirPaneWidth + minAgentsPaneWidth
	minThreePaneWidth      = minTwoPaneNavWidth + minCodexPaneWidth
	borderHorizontal       = "─"
	borderVertical         = "│"
	borderTopLeft          = "╭"
	borderTopRight         = "╮"
	borderBottomLeft       = "╰"
	borderBottomRight      = "╯"
)

var (
	mutedStyle                      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	headerStyle                     = lipgloss.NewStyle().Underline(true)
	activeTabStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("117"))
	activePaneStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	groupHeaderStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	modalStyle                      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("117")).Padding(1, 2)
	modalTitleStyle                 = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	modalLabelStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true)
	modalValueStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	modalTokenStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	modalKeyStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	workdirCardBorderStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	workdirCardSelectedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	workdirCardSelectedFocusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	workdirCountMutedStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	workdirCountActiveStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	workdirCountNeedsAttentionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	emptyLogoStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
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
	agentsWidth := min(defaultAgentsPaneWidth, width-fixedWorkdirPaneWidth-minCodexPaneWidth)
	return fixedWorkdirPaneWidth + max(minAgentsPaneWidth, agentsWidth)
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
	folderCursor int,
) string {
	return renderWorkspaceView(cfg, st, codexTitle, codexContent, width, height, message, navWidth, folderCursor, "")
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
	folderCursor int,
) string {
	return renderWorkspaceView(cfg, st, codexTitle, "", width, height, message, navWidth, folderCursor, loadingText)
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
	folderCursor int,
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
		return strings.Join(renderNavSection(cfg, st, navWidth, height, folderCursor), "\n")
	}
	nav := renderNavSection(cfg, st, navWidth, height, folderCursor)
	codex := renderCodexFrame(cfg, st, codexTitle, codexContent, codexWidth, height, false, message, false, loadingText)
	lines := make([]string, 0, height)
	for index := 0; index < height; index++ {
		left := lineAt(nav, index, navWidth)
		right := lineAt(codex, index, codexWidth)
		lines = append(lines, clip(left+right, width))
	}
	return strings.Join(lines, "\n")
}

func renderNavSection(cfg config.Config, st state.State, width int, height int, folderCursor int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	if width >= minTwoPaneNavWidth {
		workdirWidth := min(fixedWorkdirPaneWidth, max(0, width-minAgentsPaneWidth))
		folderWidth := width - workdirWidth
		workdirs := renderWorkdirsPane(cfg, st, workdirWidth, height)
		folders := renderFoldersPane(cfg, st, folderWidth, height, folderCursor)
		lines := make([]string, 0, height)
		for index := 0; index < height; index++ {
			lines = append(lines, lineAt(workdirs, index, workdirWidth)+lineAt(folders, index, folderWidth))
		}
		return lines
	}
	if st.Focus == state.FocusWorkdirs {
		return renderWorkdirsPane(cfg, st, width, height)
	}
	return renderFoldersPane(cfg, st, width, height, folderCursor)
}

func renderWorkdirsPane(_ config.Config, st state.State, width int, height int) []string {
	content := []string{}
	cardWidth := max(2, width-2-(navHorizontalPadding*2))
	for _, workdir := range st.Workdirs {
		selected := workdir.ID == st.SelectedWorkdirID
		card := renderWorkdirCard(st, workdir, cardWidth, selected, st.Focus == state.FocusWorkdirs)
		for _, line := range card {
			content = append(content, strings.Repeat(" ", navHorizontalPadding)+line)
		}
	}
	if len(content) == 0 {
		content = append(content, mutedStyle.Render("No workdirs"))
	}
	return renderPaneFrame("Workdirs", "", width, height, st.Focus == state.FocusWorkdirs, content)
}

type workdirCardCounts struct {
	total          int
	active         int
	needsAttention int
}

func renderWorkdirCard(st state.State, workdir state.Workdir, width int, selected bool, focused bool) []string {
	if width < 2 {
		return []string{""}
	}
	borderStyle := workdirCardBorderStyle
	if selected && focused {
		borderStyle = workdirCardSelectedFocusedStyle
	} else if selected {
		borderStyle = workdirCardSelectedStyle
	}
	innerWidth := max(0, width-2)
	title := workdirCardTitle(workdir)
	top := borderStyle.Render(workdirCardTopLine(title, width))
	counts := workdirCardCountsForWorkdir(st, workdir.ID)
	body := borderStyle.Render(borderVertical) + renderWorkdirCardCounts(counts, innerWidth) + borderStyle.Render(borderVertical)
	bottom := borderStyle.Render(workdirCardBottomLine(width))
	return []string{top, body, bottom}
}

func workdirCardTopLine(title string, width int) string {
	if width < 2 {
		return ""
	}
	contentWidth := max(0, width-2)
	label := " " + strings.TrimSpace(title) + " "
	label = clip(label, contentWidth)
	padding := max(0, contentWidth-lipgloss.Width(label))
	return borderTopLeft + label + strings.Repeat(borderHorizontal, padding) + borderTopRight
}

func workdirCardBottomLine(width int) string {
	if width < 2 {
		return ""
	}
	return borderBottomLeft + strings.Repeat(borderHorizontal, max(0, width-2)) + borderBottomRight
}

func workdirCardTitle(workdir state.Workdir) string {
	if title := strings.TrimSpace(workdir.Title); title != "" {
		return title
	}
	return sessions.DisplayPath(workdir.Path)
}

func workdirCardCountsForWorkdir(st state.State, workdirID string) workdirCardCounts {
	counts := workdirCardCounts{}
	for _, agent := range st.Agents {
		if agent.WorkdirID != workdirID {
			continue
		}
		counts.total++
		if workdirCardAgentActive(agent) {
			counts.active++
		}
	}
	counts.needsAttention = counts.total - counts.active
	return counts
}

func workdirCardAgentActive(agent state.Agent) bool {
	switch titles.RenderStatus(agent) {
	case string(state.StatusStarting), string(state.StatusRunning), "working", string(state.StatusShipping):
		return true
	default:
		return false
	}
}

func renderWorkdirCardCounts(counts workdirCardCounts, width int) string {
	if width <= 0 {
		return ""
	}
	labels := workdirCardCountLabels(counts)
	styles := []lipgloss.Style{
		workdirCountMutedStyle,
		workdirActiveStyle(counts),
		workdirNeedsAttentionStyle(counts),
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
		return workdirCountMutedStyle.Render(padVisual(clip(" "+strings.Join(labels, "  ")+" ", width), width))
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

func workdirActiveStyle(counts workdirCardCounts) lipgloss.Style {
	if counts.active == 0 {
		return workdirCountMutedStyle
	}
	return workdirCountActiveStyle
}

func workdirNeedsAttentionStyle(counts workdirCardCounts) lipgloss.Style {
	if counts.needsAttention == 0 {
		return workdirCountMutedStyle
	}
	return workdirCountNeedsAttentionStyle
}

func workdirCardCountLabels(counts workdirCardCounts) []string {
	return []string{
		fmtInt(counts.total) + " total",
		fmtInt(counts.active) + " active",
		fmtInt(counts.needsAttention) + " needs attention",
	}
}

func renderFoldersPane(cfg config.Config, st state.State, width int, height int, folderCursor int) []string {
	content := []string{}
	rowIndex := 0
	for _, agent := range state.UngroupedAgentsForWorkdir(st, st.SelectedWorkdirID) {
		title := titles.RenderAgent(agent, workdirForRender(st, agent), state.Folder{}, cfg.TitleTemplate)
		agentRow := "• " + title
		agentRow = clip(agentRow, max(0, width-2-(navHorizontalPadding*2)))
		if rowIndex == folderCursor && st.Focus == state.FocusFolders {
			agentRow = activeTabStyle.Render(padVisual(agentRow, max(0, width-2-(navHorizontalPadding*2))))
		} else if agent.ID == st.ActiveAgentID {
			agentRow = activePaneStyle.Render(agentRow)
		}
		content = append(content, strings.Repeat(" ", navHorizontalPadding)+agentRow)
		rowIndex++
	}
	for _, folder := range state.FoldersForWorkdir(st, st.SelectedWorkdirID) {
		if rowIndex > 0 {
			content = append(content, "")
		}
		indicator := "▾ "
		if state.IsGroupCollapsed(st, folder.ID) {
			indicator = "▸ "
		}
		folderRow := rowLine(indicator+folder.Path, fmtInt(state.AgentCountForFolder(st, folder.ID)), max(0, width-2-(navHorizontalPadding*2)))
		if rowIndex == folderCursor && st.Focus == state.FocusFolders {
			folderRow = activeTabStyle.Render(padVisual(folderRow, max(0, width-2-(navHorizontalPadding*2))))
		} else {
			folderRow = groupHeaderStyle.Render(folderRow)
		}
		content = append(content, strings.Repeat(" ", navHorizontalPadding)+folderRow)
		rowIndex++
		if state.IsGroupCollapsed(st, folder.ID) {
			continue
		}
		for _, agent := range state.AgentsForFolder(st, folder.ID) {
			title := titles.RenderAgent(agent, workdirForRender(st, agent), folder, cfg.TitleTemplate)
			agentRow := "  • " + title
			agentRow = clip(agentRow, max(0, width-2-(navHorizontalPadding*2)))
			if rowIndex == folderCursor && st.Focus == state.FocusFolders {
				agentRow = activeTabStyle.Render(padVisual(agentRow, max(0, width-2-(navHorizontalPadding*2))))
			} else if agent.ID == st.ActiveAgentID {
				agentRow = activePaneStyle.Render(agentRow)
			}
			content = append(content, strings.Repeat(" ", navHorizontalPadding)+agentRow)
			rowIndex++
		}
	}
	if len(content) == 0 {
		content = append(content, mutedStyle.Render("No agents"))
	}
	return renderPaneFrame("Agents", "", width, height, st.Focus == state.FocusFolders, content)
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
	topLabel := ""
	if navCollapsed && active {
		topLabel = "Agent  " + codexCollapsedTopShortcuts(cfg)
	} else {
		topLabel = "Agent"
	}
	lines := []string{
		palette.border.Render(cornerLine(borderTopLeft, borderTopRight, borderTextLine(topLabel, "", max(0, innerWidth-2)), innerWidth)),
	}
	contentHeight := max(0, height-2)
	empty := state.ActiveAgent(st) == nil
	contentLines := renderCodexContent(content, max(0, innerWidth-codexLeftPadding), contentHeight, empty, loadingText)
	for len(contentLines) < contentHeight {
		contentLines = append(contentLines, "")
	}
	for _, line := range contentLines[:contentHeight] {
		contentWidth := codexLineContentWidth(innerWidth)
		lines = append(lines, palette.border.Render(borderVertical)+codexLeftPad(innerWidth)+padVisual(clip(line, contentWidth), contentWidth)+palette.border.Render(borderVertical))
	}
	rightLabel := ""
	if state.ActiveAgent(st) != nil {
		rightLabel = title
	}
	lines = append(lines, palette.border.Render(cornerLine(borderBottomLeft, borderBottomRight, borderTextLine("", rightLabel, max(0, innerWidth-2)), innerWidth)))
	return lines
}

func renderCodexContent(content string, width int, height int, empty bool, loadingText string) []string {
	if height <= 0 {
		return nil
	}
	if strings.TrimSpace(loadingText) != "" {
		return renderCenteredCodexContent([]string{loadingText}, width, height)
	}
	if empty {
		return renderEmptyCodexContent(width, height)
	}
	lines := lastLines(content, height)
	for len(lines) < height {
		lines = append(lines, "")
	}
	for index := range lines {
		lines[index] = clip(lines[index], width)
	}
	return lines
}

func renderEmptyCodexContent(width int, height int) []string {
	content := []string{}
	if logoFits(emptyWeftLogo, width, height) {
		logoWidth := maxVisualWidth(emptyWeftLogo)
		for _, line := range emptyWeftLogo {
			content = append(content, emptyLogoStyle.Render(padVisual(line, logoWidth)))
		}
		content = append(content, "")
		content = append(content, centerVisual("No Codex agent open", logoWidth), centerVisual("Press n to create one.", logoWidth))
		return renderCenteredCodexBlockContent(content, width, height, logoWidth)
	}
	content = append(content, "No Codex agent open", "Press n to create one.")
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

func navShortcuts(cfg config.Config) string {
	return cfg.KeyBindings.FocusLeft + "/" + cfg.KeyBindings.FocusRight + " panes  " +
		cfg.KeyBindings.SelectPrev + "/" + cfg.KeyBindings.SelectNext + " select  " +
		cfg.KeyBindings.Open + " open  " +
		cfg.KeyBindings.NewWorkdir + " workdir  " +
		cfg.KeyBindings.NewGroup + " group  " +
		cfg.KeyBindings.NewAgent + " agent"
}

func codexCollapsedTopShortcuts(cfg config.Config) string {
	return appTitle + "  " + cfg.KeyBindings.Drawer + " command center  " + cfg.KeyBindings.Quit + " interrupt/close"
}

func focusHintLabel(cfg config.Config) string {
	return cfg.KeyBindings.Drawer + " command center"
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

func workdirForRender(st state.State, agent state.Agent) state.Workdir {
	if workdir := state.WorkdirForAgent(st, agent); workdir != nil {
		return *workdir
	}
	return state.Workdir{}
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

func desiredWorkdirPaneWidth(st state.State) int {
	return fixedWorkdirPaneWidth
}

func workdirCardCountPreferredWidth(counts workdirCardCounts) int {
	width := 1
	for _, label := range workdirCardCountLabels(counts) {
		width += lipgloss.Width(label)
	}
	width += 8
	return width + 2
}
