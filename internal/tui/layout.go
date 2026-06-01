package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/pathx"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/titles"
	"github.com/edwmurph/weft/internal/version"
)

const (
	codexLeftPadding         = 1
	codexPreviewRightPadding = 1
	navHorizontalPadding     = 1
	fixedWorkspacePaneWidth  = 60
	minAgentsPaneWidth       = 28
	defaultAgentsPaneWidth   = 48
	minCodexPaneWidth        = 28
	minTwoPaneNavWidth       = fixedWorkspacePaneWidth + minAgentsPaneWidth
	minThreePaneWidth        = minTwoPaneNavWidth + minCodexPaneWidth
	borderHorizontal         = "─"
	borderVertical           = "│"
	borderTopLeft            = "╭"
	borderTopRight           = "╮"
	borderBottomLeft         = "╰"
	borderBottomRight        = "╯"
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
	workspaceUpgradeFooterStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	workspaceInfoBrandStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true).Underline(true)
	workspaceInfoFooterStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	workspaceInfoBoxBorderStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	emptyLogoStyle                    = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	emptyVersionStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	newTaskRowStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	previewCropMarkerStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	agentReadyStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	agentRunningStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	agentWorkingStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	agentLoadingStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	agentShippingStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	agentAttentionStyle               = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	agentErrorStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
)

type workspaceRenderOptions struct {
	loadingText              string
	loadingFrame             string
	previewHeaderAnimation   string
	loadingAgents            map[string]bool
	workspaceFooterText      string
	workspaceInfoText        string
	newWorkspaceCardSelected bool
	newTaskRowSelected       bool
	codexToastText           string
}

type workspacePaneContent struct {
	lines              []string
	newWorkspaceStart  int
	newWorkspaceHeight int
}

type framePalette struct {
	border lipgloss.Style
}

var (
	activePalette   = framePalette{border: lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)}
	inactivePalette = framePalette{border: lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Bold(true)}
)

var (
	emptyWeftLogo = []string{
		`●─────╮       ██╗    ██╗ ███████╗ ███████╗ █████████╗`,
		`      │       ██║    ██║ ██╔════╝ ██╔════╝ ╚══██╔══╝`,
		`●─────┼────▶  ██║ █╗ ██║ █████╗   █████╗      ██║`,
		`      │       ██║███╗██║ ██╔══╝   ██╔══╝      ██║`,
		`●─────╯       ╚███╔███╔╝ ███████╗ ██║         ██║`,
		`               ╚══╝╚══╝  ╚══════╝ ╚═╝         ╚═╝`,
	}
)

func WeftLogoLines() []string {
	return append([]string(nil), emptyWeftLogo...)
}

func WeftLogoWithVersionLines() []string {
	logoWidth := maxVisualWidth(emptyWeftLogo)
	lines := WeftLogoLines()
	lines = append(lines, "", centerVisual(version.Label(), logoWidth))
	return lines
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
	return renderWorkspaceView(cfg, st, codexTitle, codexContent, width, height, message, navWidth, groupCursor, workspaceRenderOptions{})
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
	return renderWorkspaceView(cfg, st, codexTitle, "", width, height, message, navWidth, groupCursor, workspaceRenderOptions{loadingText: loadingText})
}

func renderWorkspaceWithNavWidthAndAgents(
	cfg config.Config,
	st state.State,
	codexTitle string,
	codexContent string,
	loadingFrame string,
	loadingAgents map[string]bool,
	width int,
	height int,
	message string,
	navWidth int,
	groupCursor int,
) string {
	return renderWorkspaceView(cfg, st, codexTitle, codexContent, width, height, message, navWidth, groupCursor, workspaceRenderOptions{
		loadingFrame:  loadingFrame,
		loadingAgents: loadingAgents,
	})
}

func renderLoadingWorkspaceWithNavWidthAndAgents(
	cfg config.Config,
	st state.State,
	codexTitle string,
	loadingText string,
	loadingFrame string,
	loadingAgents map[string]bool,
	width int,
	height int,
	message string,
	navWidth int,
	groupCursor int,
) string {
	return renderWorkspaceView(cfg, st, codexTitle, "", width, height, message, navWidth, groupCursor, workspaceRenderOptions{
		loadingText:   loadingText,
		loadingFrame:  loadingFrame,
		loadingAgents: loadingAgents,
	})
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
	options workspaceRenderOptions,
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
		codexState := codexFrameStateForSelection(st, groupCursor)
		return strings.Join(renderCodexFrame(cfg, codexState, codexTitle, codexContent, width, height, st.Focus == state.FocusCodex, message, true, options.loadingText, options.codexToastText, options.previewHeaderAnimation), "\n")
	}
	if codexWidth <= 0 {
		return strings.Join(renderNavSection(cfg, st, navWidth, height, groupCursor, options), "\n")
	}
	codexState := codexFrameStateForSelection(st, groupCursor)
	nav := renderNavSection(cfg, st, navWidth, height, groupCursor, options)
	codex := renderCodexFrame(cfg, codexState, codexTitle, codexContent, codexWidth, height, false, message, false, options.loadingText, options.codexToastText, options.previewHeaderAnimation)
	lines := make([]string, 0, height)
	for index := 0; index < height; index++ {
		left := lineAt(nav, index, navWidth)
		right := lineAt(codex, index, codexWidth)
		lines = append(lines, clip(left+right, width))
	}
	return strings.Join(lines, "\n")
}

func codexFrameStateForSelection(st state.State, groupCursor int) state.State {
	if st.Focus == state.FocusCodex {
		return st
	}
	if st.Focus == state.FocusWorkspaces && st.SelectedWorkspaceID != "" {
		return st
	}
	if st.Focus == state.FocusAgents {
		row := currentGroupRowForState(st, groupCursor)
		if row.kind == groupRowAgent {
			if st.ActiveAgentID == row.agentID {
				return st
			}
			st.ActiveAgentID = ""
			return st
		}
	}
	st.ActiveAgentID = ""
	return st
}

func renderNavSection(cfg config.Config, st state.State, width int, height int, groupCursor int, options workspaceRenderOptions) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	if width >= minTwoPaneNavWidth {
		workspaceWidth := min(fixedWorkspacePaneWidth, max(0, width-minAgentsPaneWidth))
		groupWidth := width - workspaceWidth
		workspaces := renderWorkspacesPaneWithOptions(cfg, st, workspaceWidth, height, options)
		groups := renderGroupsPaneWithOptions(cfg, st, groupWidth, height, groupCursor, options)
		lines := make([]string, 0, height)
		for index := 0; index < height; index++ {
			lines = append(lines, lineAt(workspaces, index, workspaceWidth)+lineAt(groups, index, groupWidth))
		}
		return lines
	}
	if st.Focus == state.FocusWorkspaces {
		return renderWorkspacesPaneWithOptions(cfg, st, width, height, options)
	}
	return renderGroupsPaneWithOptions(cfg, st, width, height, groupCursor, options)
}

func workspacesPaneAreaFor(st state.State, width int, height int, navWidth int) (consoleArea, bool) {
	if width <= 0 || height <= 0 || navWidth <= 0 {
		return consoleArea{}, false
	}
	navWidth = min(max(0, navWidth), width)
	if navWidth <= 0 {
		return consoleArea{}, false
	}
	if navWidth >= minTwoPaneNavWidth {
		workspaceWidth := min(fixedWorkspacePaneWidth, max(0, navWidth-minAgentsPaneWidth))
		if workspaceWidth <= 0 {
			return consoleArea{}, false
		}
		return consoleArea{x: 0, y: 0, width: workspaceWidth, height: height}, true
	}
	if st.Focus == state.FocusWorkspaces {
		return consoleArea{x: 0, y: 0, width: navWidth, height: height}, true
	}
	return consoleArea{}, false
}

func newWorkspaceTemplateCardAreaFor(cfg config.Config, st state.State, width int, height int, navWidth int, options workspaceRenderOptions) (consoleArea, bool) {
	area, ok := workspacesPaneAreaFor(st, width, height, navWidth)
	if !ok || area.width <= 2 || area.height <= 2 {
		return consoleArea{}, false
	}
	content := buildWorkspacesPaneContent(cfg, st, area.width, area.height, options)
	if content.newWorkspaceStart < 0 || content.newWorkspaceHeight <= 0 {
		return consoleArea{}, false
	}
	cardWidth := max(2, area.width-2-(navHorizontalPadding*2))
	return consoleArea{
		x:      area.x + 1 + navHorizontalPadding,
		y:      area.y + 1 + content.newWorkspaceStart,
		width:  cardWidth,
		height: content.newWorkspaceHeight,
	}, true
}

func tasksPaneAreaFor(st state.State, width int, height int, navWidth int) (consoleArea, bool) {
	if width <= 0 || height <= 0 || navWidth <= 0 {
		return consoleArea{}, false
	}
	navWidth = min(max(0, navWidth), width)
	if navWidth <= 0 {
		return consoleArea{}, false
	}
	if navWidth >= minTwoPaneNavWidth {
		workspaceWidth := min(fixedWorkspacePaneWidth, max(0, navWidth-minAgentsPaneWidth))
		groupWidth := navWidth - workspaceWidth
		if groupWidth <= 0 {
			return consoleArea{}, false
		}
		return consoleArea{x: workspaceWidth, y: 0, width: groupWidth, height: height}, true
	}
	if st.Focus != state.FocusWorkspaces {
		return consoleArea{x: 0, y: 0, width: navWidth, height: height}, true
	}
	return consoleArea{}, false
}

func newTaskTemplateRowAreaFor(cfg config.Config, st state.State, width int, height int, navWidth int) (consoleArea, bool) {
	area, ok := tasksPaneAreaFor(st, width, height, navWidth)
	if !ok || state.ActiveWorkspace(st) == nil || area.width <= 2 || area.height <= 2 {
		return consoleArea{}, false
	}
	rowWidth := max(2, area.width-2-(navHorizontalPadding*2))
	if rowWidth <= 0 || area.height <= 2 {
		return consoleArea{}, false
	}
	return consoleArea{
		x:      area.x + 1 + navHorizontalPadding,
		y:      area.y + 1,
		width:  rowWidth,
		height: 1,
	}, true
}

func renderWorkspacesPane(cfg config.Config, st state.State, width int, height int) []string {
	return renderWorkspacesPaneWithOptions(cfg, st, width, height, workspaceRenderOptions{})
}

func renderWorkspacesPaneWithOptions(cfg config.Config, st state.State, width int, height int, options workspaceRenderOptions) []string {
	content := buildWorkspacesPaneContent(cfg, st, width, height, options)
	return renderPaneFrame("Workspaces", "", width, height, st.Focus == state.FocusWorkspaces, content.lines)
}

func buildWorkspacesPaneContent(cfg config.Config, st state.State, width int, height int, options workspaceRenderOptions) workspacePaneContent {
	cards := []string{}
	cardWidth := max(2, width-2-(navHorizontalPadding*2))
	for _, workspace := range st.Workspaces {
		selected := workspace.ID == st.SelectedWorkspaceID
		card := renderWorkspaceCard(cfg, st, workspace, cardWidth, selected, st.Focus == state.FocusWorkspaces)
		for _, line := range card {
			cards = append(cards, strings.Repeat(" ", navHorizontalPadding)+line)
		}
	}
	hasWorkspaceCards := len(cards) > 0
	content := workspacePaneContent{lines: cards, newWorkspaceStart: -1}
	if !hasWorkspaceCards {
		content.lines = renderCenteredPaneHelp(width, height, "No workspaces", "Press "+cfg.KeyBindings.NewWorkspace+" to add one.")
	}
	contentHeight := max(0, height-2)
	if footer := renderWorkspaceFooter(options.workspaceFooterText, width, height, workspaceUpgradeFooterStyle); len(footer) > 0 {
		if hasWorkspaceCards {
			content = appendNewWorkspaceTemplateCardIfFits(content, cfg, cardWidth, max(0, contentHeight-len(footer)), options.newWorkspaceCardSelected, st.Focus == state.FocusWorkspaces)
		}
		content.lines = pinBottomPaneContent(content.lines, footer, contentHeight)
	} else if header := renderWorkspaceInfoHeader(options.workspaceInfoText, width, height); len(header) > 0 &&
		workspaceInfoHeaderFits(cards, header, contentHeight, hasWorkspaceCards) {
		if hasWorkspaceCards {
			headerWithSpacer := workspaceInfoHeaderWithSpacer(header)
			bodyHeight := max(0, contentHeight-len(headerWithSpacer))
			content = appendNewWorkspaceTemplateCardIfFits(content, cfg, cardWidth, bodyHeight, options.newWorkspaceCardSelected, st.Focus == state.FocusWorkspaces)
			content.lines = prependTopPaneContent(content.lines, headerWithSpacer, contentHeight)
			if content.newWorkspaceStart >= 0 {
				content.newWorkspaceStart += len(headerWithSpacer)
			}
		} else {
			content.lines = append(header, renderCenteredPaneHelpContent(max(0, width-2), max(0, contentHeight-len(header)), "No workspaces", "Press "+cfg.KeyBindings.NewWorkspace+" to add one.")...)
		}
	} else if hasWorkspaceCards {
		content = appendNewWorkspaceTemplateCardIfFits(content, cfg, cardWidth, contentHeight, options.newWorkspaceCardSelected, st.Focus == state.FocusWorkspaces)
	}
	return content
}

func renderWorkspaceFooter(message string, width int, height int, style lipgloss.Style) []string {
	message = strings.TrimSpace(message)
	if message == "" || width <= 0 || height <= 3 {
		return nil
	}
	rowWidth := max(0, width-2-(navHorizontalPadding*2))
	if rowWidth <= 0 {
		return nil
	}
	wrapped := []string{}
	for _, paragraph := range strings.Split(message, "\n") {
		paragraph = strings.Join(strings.Fields(paragraph), " ")
		if paragraph == "" {
			continue
		}
		wrapped = append(wrapped, wrapPlain(paragraph, rowWidth, 3-len(wrapped))...)
		if len(wrapped) >= 3 {
			break
		}
	}
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		line = padVisual(clip(line, rowWidth), rowWidth)
		lines = append(lines, strings.Repeat(" ", navHorizontalPadding)+style.Render(line))
	}
	return lines
}

func renderWorkspaceInfoHeader(message string, width int, height int) []string {
	message = strings.TrimSpace(message)
	if message == "" || width <= 0 || height <= 6 {
		return nil
	}
	rowWidth := max(0, width-2-(navHorizontalPadding*2))
	if rowWidth <= 0 {
		return nil
	}
	content := []string{}
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		content = append(content, line)
		if len(content) == 3 {
			break
		}
	}
	if len(content) == 0 {
		return nil
	}
	lines := renderWorkspaceInfoBox(content, rowWidth)
	for index := range lines {
		lines[index] = strings.Repeat(" ", navHorizontalPadding) + lines[index]
	}
	return lines
}

func renderWorkspaceInfoBox(content []string, rowWidth int) []string {
	if rowWidth < 4 {
		return nil
	}
	contentWidth := maxVisualWidth(content)
	contentWidth = min(contentWidth, max(0, rowWidth-4))
	boxWidth := contentWidth + 4
	top := workspaceInfoBoxBorderStyle.Render("┌" + strings.Repeat(borderHorizontal, max(0, boxWidth-2)) + "┐")
	bottom := workspaceInfoBoxBorderStyle.Render("└" + strings.Repeat(borderHorizontal, max(0, boxWidth-2)) + "┘")
	lines := []string{centerVisual(top, rowWidth)}
	for index, line := range content {
		style := workspaceInfoFooterStyle
		if index == 0 {
			style = workspaceInfoBrandStyle
			line = padVisual(centerVisual(style.Render(clip(line, contentWidth)), contentWidth), contentWidth)
		} else {
			line = style.Render(padVisual(clip(line, contentWidth), contentWidth))
		}
		lines = append(lines, centerVisual(workspaceInfoBoxBorderStyle.Render(borderVertical)+" "+line+" "+workspaceInfoBoxBorderStyle.Render(borderVertical), rowWidth))
	}
	lines = append(lines, centerVisual(bottom, rowWidth))
	return lines
}

func workspaceInfoHeaderFits(cards []string, header []string, contentHeight int, hasWorkspaceCards bool) bool {
	if contentHeight <= 0 || len(header) == 0 || len(header) > contentHeight {
		return false
	}
	if !hasWorkspaceCards {
		return len(header) < contentHeight
	}
	return len(cards)+len(workspaceInfoHeaderWithSpacer(header)) <= contentHeight
}

func workspaceInfoHeaderWithSpacer(header []string) []string {
	return append(append([]string{}, header...), "")
}

func prependTopPaneContent(content []string, header []string, contentHeight int) []string {
	if contentHeight <= 0 || len(header) == 0 {
		return content
	}
	next := append([]string{}, header...)
	for _, line := range content {
		if len(next) >= contentHeight {
			break
		}
		next = append(next, line)
	}
	return next
}

func pinBottomPaneContent(content []string, footer []string, contentHeight int) []string {
	if contentHeight <= 0 || len(footer) == 0 {
		return content
	}
	if len(footer) >= contentHeight {
		return footer[len(footer)-contentHeight:]
	}
	bodyHeight := contentHeight - len(footer)
	if len(content) > bodyHeight {
		content = content[:bodyHeight]
	}
	next := append([]string{}, content...)
	for len(next) < bodyHeight {
		next = append(next, "")
	}
	return append(next, footer...)
}

type workspaceCardCounts struct {
	total          int
	active         int
	needsAttention int
	silenced       int
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

func renderNewWorkspaceTemplateCard(cfg config.Config, width int, selected bool, focused bool) []string {
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
	title := "+ New workspace"
	top := borderStyle.Render(workspaceCardTopLine(title, width))
	hint := " Press " + cfg.KeyBindings.NewWorkspace + " to create "
	body := borderStyle.Render(borderVertical) + mutedStyle.Render(padVisual(clip(hint, innerWidth), innerWidth)) + borderStyle.Render(borderVertical)
	bottom := borderStyle.Render(workspaceCardBottomLine(width))
	return []string{top, body, bottom}
}

func appendNewWorkspaceTemplateCardIfFits(content workspacePaneContent, cfg config.Config, width int, availableHeight int, selected bool, focused bool) workspacePaneContent {
	card := renderNewWorkspaceTemplateCard(cfg, width, selected, focused)
	if len(content.lines) == 0 || len(content.lines)+len(card) > availableHeight {
		return content
	}
	content.newWorkspaceStart = len(content.lines)
	content.newWorkspaceHeight = len(card)
	next := append([]string{}, content.lines...)
	for _, line := range card {
		next = append(next, strings.Repeat(" ", navHorizontalPadding)+line)
	}
	content.lines = next
	return content
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
			continue
		}
		if group := state.GroupByID(st, agent.GroupID); group != nil && group.Silent && workspaceCardAgentSilenced(agent) {
			counts.silenced++
			continue
		}
		counts.needsAttention++
	}
	return counts
}

func workspaceCardAgentActive(agent state.Agent) bool {
	return agentStatusIndicatesActivity(agent)
}

func workspaceCardAgentSilenced(agent state.Agent) bool {
	switch titles.CanonicalStatus(agent) {
	case string(state.StatusReady), string(state.StatusSitting), string(state.StatusStopped):
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
		workspaceCountMutedStyle,
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
		fmtInt(counts.silenced) + " silenced",
	}
}

func renderGroupsPane(cfg config.Config, st state.State, width int, height int, groupCursor int) []string {
	return renderGroupsPaneWithOptions(cfg, st, width, height, groupCursor, workspaceRenderOptions{})
}

func renderGroupsPaneWithOptions(cfg config.Config, st state.State, width int, height int, groupCursor int, options workspaceRenderOptions) []string {
	content := []string{}
	rowIndex := 0
	selectedLine := -1
	rowWidth := max(0, width-2-(navHorizontalPadding*2))
	if state.ActiveWorkspace(st) != nil {
		selected := (rowIndex == groupCursor && st.Focus == state.FocusAgents) || options.newTaskRowSelected
		taskRow := renderNewTaskTemplateRow(cfg, rowWidth, selected, st.Focus == state.FocusAgents)
		if rowIndex == groupCursor {
			selectedLine = len(content)
		}
		content = append(content, strings.Repeat(" ", navHorizontalPadding)+taskRow)
		content = append(content, "")
		rowIndex++
	}
	for _, agent := range state.UngroupedAgentsForWorkspace(st, st.SelectedWorkspaceID) {
		agentRow := renderAgentRow(cfg, st, agent, rowWidth, false, rowIndex == groupCursor && st.Focus == state.FocusAgents, options)
		if rowIndex == groupCursor {
			selectedLine = len(content)
		}
		content = append(content, strings.Repeat(" ", navHorizontalPadding)+agentRow)
		rowIndex++
	}
	for _, group := range state.GroupsForWorkspace(st, st.SelectedWorkspaceID) {
		if rowIndex > 0 {
			content = append(content, "")
		}
		collapsed := state.IsGroupCollapsed(st, group.ID)
		indicator := "▾ "
		if collapsed {
			indicator = "▸ "
		}
		silentMarker := ""
		if group.Silent {
			silentMarker = "⊘ "
		}
		loadingMarker := ""
		if collapsed && groupHasLoadingAgent(st, group.ID, options.loadingAgents) {
			loadingMarker = loadingFrameForRender(options.loadingFrame) + " "
		}
		groupRow := rowLine(indicator+loadingMarker+silentMarker+group.Path+" ("+fmtInt(state.AgentCountForGroup(st, group.ID))+")", "", rowWidth)
		if rowIndex == groupCursor && st.Focus == state.FocusAgents {
			groupRow = activeAgentStyle.Render(padVisual(groupRow, rowWidth))
		} else {
			groupRow = groupHeaderStyle.Render(groupRow)
		}
		if rowIndex == groupCursor {
			selectedLine = len(content)
		}
		content = append(content, strings.Repeat(" ", navHorizontalPadding)+groupRow)
		rowIndex++
		if collapsed {
			continue
		}
		for _, agent := range state.AgentsForGroup(st, group.ID) {
			agentRow := renderAgentRow(cfg, st, agent, rowWidth, true, rowIndex == groupCursor && st.Focus == state.FocusAgents, options)
			if rowIndex == groupCursor {
				selectedLine = len(content)
			}
			content = append(content, strings.Repeat(" ", navHorizontalPadding)+agentRow)
			rowIndex++
		}
	}
	if len(content) == 0 && state.ActiveWorkspace(st) == nil {
		content = renderCenteredPaneHelp(width, height, "No workspace selected", "Press "+cfg.KeyBindings.NewWorkspace+" to add one.")
	}
	content = scrollPaneContentToLine(content, selectedLine, max(0, height-2))
	return renderPaneFrame("Tasks", "", width, height, st.Focus == state.FocusAgents, content)
}

func renderNewTaskTemplateRow(cfg config.Config, width int, selected bool, focused bool) string {
	if width <= 0 {
		return ""
	}
	row := "+ New task  Press " + cfg.KeyBindings.NewTask + " to create"
	if selected && focused {
		return activeAgentStyle.Italic(true).Render(padVisual(clip(row, width), width))
	}
	if selected {
		return activePaneStyle.Italic(true).Render(padVisual(clip(row, width), width))
	}
	return newTaskRowStyle.Render(padVisual(clip(row, width), width))
}

func groupHasLoadingAgent(st state.State, groupID string, loadingAgents map[string]bool) bool {
	for _, agent := range state.AgentsForGroup(st, groupID) {
		if agentIsLoadingForRender(agent, loadingAgents) {
			return true
		}
	}
	return false
}

func scrollPaneContentToLine(content []string, selectedLine int, visibleLines int) []string {
	if visibleLines <= 0 || selectedLine < 0 || len(content) <= visibleLines {
		return content
	}
	start := 0
	if selectedLine >= visibleLines {
		start = selectedLine - visibleLines + 1
	}
	start = min(start, len(content)-visibleLines)
	return content[start:]
}

func renderAgentRow(cfg config.Config, st state.State, agent state.Agent, width int, nested bool, selected bool, options workspaceRenderOptions) string {
	title := renderAgentTitleForState(cfg, st, agent)
	marker := agentMarkerForRender(agent, options.loadingFrame, options.loadingAgents)
	prefix := marker + " " + taskTypeBadgeCellForAgent(cfg, agent) + " "
	if nested {
		prefix = "  " + prefix
	}
	row := clip(prefix+title, width)
	if selected {
		return activeAgentStyle.Render(padVisual(row, width))
	}
	if agent.ID == st.ActiveAgentID {
		return activePaneStyle.Render(row)
	}
	return agentRowStyle(agent, options.loadingAgents).Render(row)
}

func agentRowStyle(agent state.Agent, loadingAgents map[string]bool) lipgloss.Style {
	status := titles.CanonicalStatus(agent)
	if agentIsLoadingForRender(agent, loadingAgents) {
		return agentLoadingStyleForStatus(status)
	}
	switch status {
	case string(state.StatusReady):
		return agentReadyStyle
	case string(state.StatusRunning):
		return agentRunningStyle
	case "working":
		return agentWorkingStyle
	case string(state.StatusShipping):
		return agentShippingStyle
	case string(state.StatusError):
		return agentErrorStyle
	case string(state.StatusStopped), string(state.StatusKilled), string(state.StatusSitting):
		return agentAttentionStyle
	default:
		return agentAttentionStyle
	}
}

func agentLoadingStyleForStatus(status string) lipgloss.Style {
	switch status {
	case string(state.StatusRunning):
		return agentRunningStyle
	case "waiting":
		return agentWorkingStyle
	case "working":
		return agentWorkingStyle
	case string(state.StatusShipping):
		return agentShippingStyle
	default:
		return agentLoadingStyle
	}
}

func agentMarkerForRender(agent state.Agent, loadingFrame string, loadingAgents map[string]bool) string {
	if agentIsLoadingForRender(agent, loadingAgents) {
		return loadingFrameForRender(loadingFrame)
	}
	switch titles.CanonicalStatus(agent) {
	case string(state.StatusError):
		return "!"
	case string(state.StatusKilled):
		return "!"
	case string(state.StatusReady):
		return "·"
	case string(state.StatusStopped), string(state.StatusSitting):
		return "◦"
	default:
		return "·"
	}
}

func agentIsLoadingForRender(agent state.Agent, loadingAgents map[string]bool) bool {
	return agentStatusShowsLoadingIndicator(agent) || loadingAgents[agent.ID]
}

func loadingFrameForRender(frame string) string {
	if strings.TrimSpace(frame) != "" {
		return frame
	}
	return loadingFrames[0]
}

func loadingAgentSet(ids []string) map[string]bool {
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func renderCenteredPaneHelp(width int, height int, lines ...string) []string {
	contentHeight := max(0, height-2)
	innerWidth := max(0, width-2)
	return renderCenteredPaneHelpContent(innerWidth, contentHeight, lines...)
}

func renderCenteredPaneHelpContent(innerWidth int, contentHeight int, lines ...string) []string {
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
	message string,
	navCollapsed bool,
	loadingText string,
	toastText string,
	previewHeaderAnimation string,
) []string {
	if width < 2 || height <= 0 {
		return nil
	}
	palette := paletteFor(active)
	innerWidth := max(0, width-2)
	agentActive := state.ActiveAgent(st) != nil
	if !agentActive {
		loadingText = ""
	}
	previewMode := !navCollapsed
	topLabel := "Task Live Preview"
	topRightLabel := ""
	if previewMode && agentActive {
		topLabel = previewTopLabel(previewHeaderAnimation)
	}
	if navCollapsed && active {
		topLabel = "Task Console  " + codexCollapsedTopShortcuts(cfg)
		topRightLabel = codexConsoleTopRightLabel(st, toastText)
	} else if navCollapsed {
		topLabel = "Task Console"
	} else if agentActive {
		topRightLabel = title
	}
	lines := []string{
		palette.border.Render(cornerLine(borderTopLeft, borderTopRight, borderTextLine(topLabel, topRightLabel, max(0, innerWidth-2)), innerWidth)),
	}
	contentHeight := max(0, height-2)
	empty := !agentActive
	contentWidth := codexLineContentWidth(innerWidth, previewMode)
	messageLines := renderStatusBanner(message, contentWidth, min(3, contentHeight))
	contentLines := renderCodexContent(content, contentWidth, max(0, contentHeight-len(messageLines)), empty, len(st.Workspaces) > 0, loadingText, codexEmptyTitle(previewMode))
	if len(messageLines) > 0 {
		contentLines = append(messageLines, contentLines...)
	}
	for len(contentLines) < contentHeight {
		contentLines = append(contentLines, "")
	}
	leftPadding := codexLeftPad(innerWidth)
	rightPadding := codexRightPad(innerWidth, previewMode)
	for _, line := range contentLines[:contentHeight] {
		contentWidth := codexLineContentWidth(innerWidth, previewMode)
		renderedLine := padVisual(clip(line, contentWidth), contentWidth)
		if previewMode && !empty {
			renderedLine = previewClipLine(line, contentWidth)
		}
		lines = append(lines, palette.border.Render(borderVertical)+leftPadding+renderedLine+rightPadding+palette.border.Render(borderVertical))
	}
	rightLabel := ""
	if agentActive && navCollapsed {
		rightLabel = title
	}
	lines = append(lines, palette.border.Render(cornerLine(borderBottomLeft, borderBottomRight, borderTextLine("", rightLabel, max(0, innerWidth-2)), innerWidth)))
	return lines
}

func previewTopLabel(animation string) string {
	animation = strings.TrimSpace(animation)
	if animation == "" {
		animation = livePreviewAnimationFrame(0)
	}
	return "Task Live Preview " + animation
}

func renderStatusBanner(message string, width int, maxLines int) []string {
	message = strings.Join(strings.Fields(message), " ")
	if message == "" || width <= 0 || maxLines <= 0 {
		return nil
	}
	if !strings.HasPrefix(message, "Upgrade") && !strings.HasPrefix(message, "Restart") {
		return nil
	}
	prefix := "Upgrade: "
	message = strings.TrimPrefix(message, "Upgrade ")
	wrapped := wrapPlain(prefix+message, width, maxLines)
	for index := range wrapped {
		wrapped[index] = padVisual(clip(wrapped[index], width), width)
	}
	return wrapped
}

func wrapPlain(value string, width int, maxLines int) []string {
	if width <= 0 || maxLines <= 0 {
		return nil
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return nil
	}
	lines := []string{}
	current := ""
	for _, word := range words {
		next := word
		if current != "" {
			next = current + " " + word
		}
		if lipgloss.Width(next) <= width {
			current = next
			continue
		}
		if current != "" {
			lines = append(lines, current)
			current = word
		} else {
			lines = append(lines, clipPlain(word, width))
			current = ""
		}
		if len(lines) == maxLines {
			return lines
		}
	}
	if current != "" && len(lines) < maxLines {
		lines = append(lines, current)
	}
	if len(lines) > maxLines {
		return lines[:maxLines]
	}
	return lines
}

func codexEmptyTitle(previewMode bool) string {
	if previewMode {
		return "No task selected"
	}
	return "No task open"
}

func renderCodexContent(content string, width int, height int, empty bool, canCreateAgent bool, loadingText string, emptyTitle string) []string {
	if height <= 0 {
		return nil
	}
	if strings.TrimSpace(loadingText) != "" {
		return renderCenteredCodexContent([]string{loadingText}, width, height)
	}
	if empty {
		return renderEmptyCodexContent(width, height, canCreateAgent, emptyTitle)
	}
	lines := lastLines(content, height)
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func renderEmptyCodexContent(width int, height int, canCreateAgent bool, titles ...string) []string {
	title := "No task open"
	if len(titles) > 0 && strings.TrimSpace(titles[0]) != "" {
		title = strings.TrimSpace(titles[0])
	}
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
		content = append(content, emptyVersionStyle.Render(centerVisual(version.Label(), logoWidth)))
		content = append(content, "")
		content = append(content, centerVisual(title, logoWidth), centerVisual(hint, logoWidth))
		return renderCenteredCodexBlockContent(content, width, height, logoWidth)
	}
	content = append(content, version.Label(), title, hint)
	return renderCenteredCodexContent(content, width, height)
}

func logoFits(logo []string, width int, height int) bool {
	if len(logo) == 0 || height < len(logo)+5 {
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
	return cfg.KeyBindings.Drawer + " dashboard"
}

func codexConsoleTopRightLabel(st state.State, toastText string) string {
	toastText = strings.TrimSpace(toastText)
	indicator := codexConsoleReadyIndicator(st)
	switch {
	case toastText != "" && indicator != "":
		return toastText + "  " + indicator
	case toastText != "":
		return toastText
	default:
		return indicator
	}
}

func codexConsoleReadyIndicator(st state.State) string {
	count := otherReadyAgentCount(st)
	if count == 0 {
		return ""
	}
	noun := "task"
	if count != 1 {
		noun = "tasks"
	}
	return workspaceCountNeedsAttentionStyle.Render(fmtInt(count) + " other " + noun + " ready")
}

func otherReadyAgentCount(st state.State) int {
	active := state.ActiveAgent(st)
	if active == nil {
		return 0
	}
	count := 0
	for _, agent := range st.Agents {
		if agent.ID == active.ID {
			continue
		}
		if titles.CanonicalStatus(agent) == string(state.StatusReady) {
			count++
		}
	}
	return count
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
	left = clipFrameLabel(left, width)
	remaining := width - lipgloss.Width(left) - lipgloss.Width(right)
	if remaining < 0 {
		right = clipFrameLabel(right, max(0, width-lipgloss.Width(left)))
		remaining = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if remaining < 0 {
		left = clipFrameLabel(left, width)
		right = ""
		remaining = width - lipgloss.Width(left)
	}
	return left + strings.Repeat(" ", max(0, remaining)) + right
}

func clipFrameLabel(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return ansi.Truncate(value, max(0, width-1), "…")
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

func codexRightPad(width int, previewMode bool) string {
	if width <= codexLeftPadding || !previewMode {
		return ""
	}
	return strings.Repeat(" ", min(codexPreviewRightPadding, width-codexLeftPadding))
}

func codexLineContentWidth(width int, previewMode bool) int {
	if width <= 0 {
		return 0
	}
	padding := min(codexLeftPadding, width)
	if previewMode {
		padding += min(codexPreviewRightPadding, max(0, width-padding))
	}
	return max(0, width-padding)
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
