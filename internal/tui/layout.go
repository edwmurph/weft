package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/codux/internal/config"
	"github.com/edwmurph/codux/internal/sessions"
	"github.com/edwmurph/codux/internal/state"
	"github.com/edwmurph/codux/internal/titles"
)

const (
	appTitle             = "CODUX"
	codexLeftPadding     = 1
	navHorizontalPadding = 2
	navColumnGap         = 2
	borderHorizontal     = "─"
	borderVertical       = "│"
	borderTopLeft        = "╭"
	borderTopRight       = "╮"
	borderBottomLeft     = "╰"
	borderBottomRight    = "╯"
)

var (
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	headerStyle    = lipgloss.NewStyle().Underline(true)
	activeTabStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("117"))
	modalStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("117")).Padding(1, 2)
)

type framePalette struct {
	border lipgloss.Style
}

var (
	activePalette   = framePalette{border: lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)}
	inactivePalette = framePalette{border: lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Bold(true)}
)

func workspaceNavFrameHeight(cfg config.Config, st state.State, height int) int {
	if height < 8 {
		return max(2, height/3)
	}
	wanted := navContentHeight(cfg, st) + 1
	if wanted < 3 {
		wanted = 3
	}
	limit := max(3, height/3)
	if limit > 10 {
		limit = 10
	}
	return min(wanted, limit)
}

func navFrameHeight(height int, tabCount int) int {
	wanted := 3 + tabCount/3
	if wanted > 10 {
		wanted = 10
	}
	if height < 18 {
		return max(3, height/3)
	}
	return min(wanted, max(3, height/3))
}

func renderWorkspace(
	cfg config.Config,
	st state.State,
	codexTitle string,
	codexContent string,
	width int,
	height int,
	message string,
	workdir string,
) string {
	return renderWorkspaceWithNavHeight(cfg, st, codexTitle, codexContent, width, height, message, workdir, workspaceNavFrameHeight(cfg, st, height))
}

func renderWorkspaceWithNavHeight(
	cfg config.Config,
	st state.State,
	codexTitle string,
	codexContent string,
	width int,
	height int,
	message string,
	workdir string,
	navHeight int,
) string {
	return renderWorkspaceView(cfg, st, codexTitle, codexContent, width, height, message, workdir, navHeight, "")
}

func renderLoadingWorkspaceWithNavHeight(
	cfg config.Config,
	st state.State,
	codexTitle string,
	loadingText string,
	width int,
	height int,
	message string,
	workdir string,
	navHeight int,
) string {
	return renderWorkspaceView(cfg, st, codexTitle, "", width, height, message, workdir, navHeight, loadingText)
}

func renderWorkspaceView(
	cfg config.Config,
	st state.State,
	codexTitle string,
	codexContent string,
	width int,
	height int,
	message string,
	workdir string,
	navHeight int,
	loadingText string,
) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	navHeight = max(0, navHeight)
	if navHeight > height-3 {
		navHeight = max(0, height-3)
	}
	codexHeight := height - navHeight
	if codexHeight < 3 {
		codexHeight = 3
		navHeight = max(0, height-codexHeight)
	}

	lines := []string{}
	if navHeight > 0 {
		lines = append(lines, renderNavFrame(cfg, st, width, navHeight, st.Focus == state.FocusNav, workdir)...)
	}
	lines = append(lines, renderCodexFrame(cfg, st, codexTitle, codexContent, width, codexHeight, st.Focus == state.FocusCodex, message, navHeight == 0, workdir, loadingText)...)
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func renderNavFrame(cfg config.Config, st state.State, width int, height int, active bool, workdir string) []string {
	if width < 2 || height <= 0 {
		return nil
	}
	palette := paletteFor(active)
	innerWidth := max(0, width-2)
	topLabel := appTitle + "  " + focusHintLabel(cfg)
	if active {
		topLabel = appTitle + "  " + navShortcuts(cfg)
	}
	lines := []string{
		palette.border.Render(cornerLine(borderTopLeft, borderTopRight, borderTextLine(topLabel, sessions.DisplayPath(workdir), max(0, innerWidth-2)), innerWidth)),
	}
	contentHeight := max(0, height-1)
	content := renderNavContent(cfg, st, innerWidth, contentHeight)
	for len(content) < contentHeight {
		content = append(content, "")
	}
	for _, line := range content[:contentHeight] {
		lines = append(lines, palette.border.Render(borderVertical)+padVisual(clip(line, innerWidth), innerWidth)+palette.border.Render(borderVertical))
	}
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
	message string,
	navCollapsed bool,
	workdir string,
	loadingText string,
) []string {
	if width < 2 || height <= 0 {
		return nil
	}
	palette := paletteFor(active)
	innerWidth := max(0, width-2)
	topLabel := ""
	if navCollapsed && active {
		topLabel = codexCollapsedTopShortcuts(cfg)
	}
	contentHeight := max(0, height-1)
	if topLabel != "" {
		contentHeight = max(0, height-2)
	}
	lines := []string{}
	if topLabel != "" {
		lines = append(lines, palette.border.Render(cornerLine(borderTopLeft, borderTopRight, borderTextLine(topLabel, sessions.DisplayPath(workdir), max(0, innerWidth-2)), innerWidth)))
	}
	contentLines := renderCodexContent(content, max(0, innerWidth-codexLeftPadding), contentHeight, len(st.Tabs) == 0, loadingText)
	for len(contentLines) < contentHeight {
		contentLines = append(contentLines, "")
	}
	for index, line := range contentLines[:contentHeight] {
		left, right := borderVertical, borderVertical
		lineWidth := innerWidth
		if index == 0 && topLabel == "" {
			left, right = borderTopLeft+borderHorizontal, borderHorizontal+borderTopRight
			lineWidth = max(0, innerWidth-2)
		}
		contentWidth := codexLineContentWidth(lineWidth)
		lines = append(lines, palette.border.Render(left)+codexLeftPad(lineWidth)+padVisual(clip(line, contentWidth), contentWidth)+palette.border.Render(right))
	}

	leftLabel := ""
	rightLabel := ""
	if state.ActiveTab(st) != nil {
		rightLabel = title
	}
	if strings.TrimSpace(message) != "" {
		leftLabel = message
	}
	lines = append(lines, palette.border.Render(cornerLine(borderBottomLeft, borderBottomRight, borderTextLine(leftLabel, rightLabel, max(0, innerWidth-2)), innerWidth)))
	return lines
}

func renderNavContent(cfg config.Config, st state.State, width int, height int) []string {
	if len(cfg.Columns) == 0 || width <= 0 || height <= 0 {
		return nil
	}
	contentWidth := max(1, width-(navHorizontalPadding*2))
	columnWidths := navColumnWidths(len(cfg.Columns), contentWidth, navColumnGap)
	tabsByColumn := map[string][]state.Tab{}
	for _, column := range cfg.Columns {
		tabsByColumn[column] = nil
	}
	for _, tab := range st.Tabs {
		tabsByColumn[tab.Column] = append(tabsByColumn[tab.Column], tab)
	}

	lines := make([]string, 0, height)
	headerParts := make([]string, 0, len(cfg.Columns))
	for index, column := range cfg.Columns {
		headerParts = append(headerParts, padVisual(headerStyle.Render(clip(strings.ToUpper(column), columnWidths[index])), columnWidths[index]))
	}
	lines = append(lines, strings.Repeat(" ", navHorizontalPadding)+strings.Join(headerParts, strings.Repeat(" ", navColumnGap))+strings.Repeat(" ", navHorizontalPadding))

	maxRows := 1
	for _, column := range cfg.Columns {
		if len(tabsByColumn[column]) > maxRows {
			maxRows = len(tabsByColumn[column])
		}
	}
	for row := 0; row < maxRows && len(lines) < height; row++ {
		parts := make([]string, 0, len(cfg.Columns))
		for columnIndex, column := range cfg.Columns {
			cell := strings.Repeat(" ", columnWidths[columnIndex])
			tabs := tabsByColumn[column]
			if row < len(tabs) {
				tab := tabs[row]
				label := clip(titles.Render(tab), columnWidths[columnIndex])
				cell = padVisual(label, columnWidths[columnIndex])
				if tab.ID == st.ActiveTabID {
					cell = activeTabStyle.Render(cell)
				}
			}
			parts = append(parts, cell)
		}
		lines = append(lines, strings.Repeat(" ", navHorizontalPadding)+strings.Join(parts, strings.Repeat(" ", navColumnGap))+strings.Repeat(" ", navHorizontalPadding))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	for index := range lines {
		lines[index] = clip(lines[index], width)
	}
	return lines
}

func navContentHeight(cfg config.Config, st state.State) int {
	maxRows := 1
	for _, column := range cfg.Columns {
		count := 0
		for _, tab := range st.Tabs {
			if tab.Column == column {
				count++
			}
		}
		if count > maxRows {
			maxRows = count
		}
	}
	return 1 + maxRows
}

func navColumnWidths(count int, width int, gap int) []int {
	if count <= 0 {
		return nil
	}
	available := max(0, width-gap*(count-1))
	base, remainder := available/count, available%count
	widths := make([]int, count)
	for index := range widths {
		widths[index] = base
		if index < remainder {
			widths[index]++
		}
	}
	return widths
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
	return renderCenteredCodexContent([]string{"No Codex tabs open", "Press n to create one."}, width, height)
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
	return "←↑↓→ select  S-←/→ move  Enter  " +
		cfg.KeyBindings.New + " new  " +
		cfg.KeyBindings.Close + " close  " +
		cfg.KeyBindings.CloseCodux + " close codux"
}

func codexCollapsedTopShortcuts(cfg config.Config) string {
	return appTitle + "  " + cfg.KeyBindings.FocusToggle + " focus nav  " + cfg.KeyBindings.CloseCodux + " interrupt/close"
}

func focusHintLabel(cfg config.Config) string {
	return cfg.KeyBindings.FocusToggle + " focus nav"
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
