package tui

import (
	"os"
	"strings"
	"time"

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
	minTasksPaneWidth        = 28
	defaultTasksPaneWidth    = 54
	minCodexPaneWidth        = 28
	minTwoPaneNavWidth       = fixedWorkspacePaneWidth + minTasksPaneWidth
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
	activeTaskStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("117"))
	activePaneStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	attentionHighlightStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	taskReadyHighlightStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	groupHeaderStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	modalStyle                        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("117")).Padding(1, 2)
	modalInputStyle                   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("244")).Padding(0, 1)
	modalInputFocusedStyle            = modalInputStyle.BorderForeground(lipgloss.Color("117"))
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
	workspaceCountNeedsAttentionStyle = taskReadyHighlightStyle
	workspacePathWarningStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	workspaceUpgradeFooterStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	workspaceInfoBrandStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true).Underline(true)
	workspaceInfoFooterStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	workspaceInfoBoxBorderStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	emptyLogoStyle                    = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	emptyLogoAccentStyle              = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	newWorkspaceCardHintStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	newTaskRowStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	previewCropMarkerStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	taskReadyStyle                    = taskReadyHighlightStyle
	taskReadySelectedStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("220")).Bold(true)
	taskRunningStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	taskWorkingStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	taskLoadingStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	taskShippingStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	taskAttentionStyle                = attentionHighlightStyle
	taskErrorStyle                    = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
)

type workspaceRenderOptions struct {
	loadingText              string
	loadingFrame             string
	previewHeaderAnimation   string
	emptyArtFrame            int
	loadingTasks             map[string]bool
	taskOperationStartedAt   map[string]time.Time
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
		`◆━━━━━┓       ██╗    ██╗ ███████╗ ███████╗ █████████╗`,
		`      ┃       ██║    ██║ ██╔════╝ ██╔════╝ ╚══██╔══╝`,
		`◆━━━━━╋━━━━━➤ ██║ █╗ ██║ █████╗   █████╗      ██║`,
		`      ┃       ██║███╗██║ ██╔══╝   ██╔══╝      ██║`,
		`◆━━━━━┛       ╚███╔███╔╝ ███████╗ ██║         ██║`,
		`               ╚══╝╚══╝  ╚══════╝ ╚═╝         ╚═╝`,
	}
	previewEmptyWeftLogo = emptyWeftLogo
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
	tasksWidth := min(defaultTasksPaneWidth, width-fixedWorkspacePaneWidth-minCodexPaneWidth)
	return fixedWorkspacePaneWidth + max(minTasksPaneWidth, tasksWidth)
}

func previewPaneVisible(navOpen bool, width int, navWidth int) bool {
	return navOpen && width > 0 && navWidth > 0 && navWidth < width
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
		return strings.Join(renderCodexFrame(cfg, codexState, codexTitle, codexContent, width, height, st.Focus == state.FocusConsole, message, true, options.loadingText, options.codexToastText, options.previewHeaderAnimation, options.emptyArtFrame), "\n")
	}
	if codexWidth <= 0 {
		return strings.Join(renderNavSection(cfg, st, navWidth, height, groupCursor, options), "\n")
	}
	codexState := codexFrameStateForSelection(st, groupCursor)
	nav := renderNavSection(cfg, st, navWidth, height, groupCursor, options)
	codex := renderCodexFrame(cfg, codexState, codexTitle, codexContent, codexWidth, height, false, message, false, options.loadingText, options.codexToastText, options.previewHeaderAnimation, options.emptyArtFrame)
	lines := make([]string, 0, height)
	for index := 0; index < height; index++ {
		left := lineAt(nav, index, navWidth)
		right := lineAt(codex, index, codexWidth)
		lines = append(lines, clip(left+right, width))
	}
	return strings.Join(lines, "\n")
}

func codexFrameStateForSelection(st state.State, groupCursor int) state.State {
	if st.Focus == state.FocusConsole {
		return st
	}
	if st.Focus == state.FocusWorkspaces && st.SelectedWorkspaceID != "" {
		return st
	}
	if st.Focus == state.FocusTasks {
		row := currentGroupRowForState(st, groupCursor)
		if row.kind == groupRowTask {
			if st.ActiveTaskID == row.taskID {
				return st
			}
			st.ActiveTaskID = ""
			return st
		}
	}
	st.ActiveTaskID = ""
	return st
}

func renderNavSection(cfg config.Config, st state.State, width int, height int, groupCursor int, options workspaceRenderOptions) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	if width >= minTwoPaneNavWidth {
		workspaceWidth := min(fixedWorkspacePaneWidth, max(0, width-minTasksPaneWidth))
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
		workspaceWidth := min(fixedWorkspacePaneWidth, max(0, navWidth-minTasksPaneWidth))
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
		workspaceWidth := min(fixedWorkspacePaneWidth, max(0, navWidth-minTasksPaneWidth))
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

func renderWorkspacesPaneWithOptions(cfg config.Config, st state.State, width int, height int, options workspaceRenderOptions) []string {
	content := buildWorkspacesPaneContent(cfg, st, width, height, options)
	return renderPaneFrame("Workspaces", "", width, height, st.Focus == state.FocusWorkspaces, content.lines)
}

func buildWorkspacesPaneContent(cfg config.Config, st state.State, width int, height int, options workspaceRenderOptions) workspacePaneContent {
	cards := []string{}
	selectedLine := -1
	selectedHeight := 1
	cardWidth := max(2, width-2-(navHorizontalPadding*2))
	for _, workspace := range st.Workspaces {
		selected := workspace.ID == st.SelectedWorkspaceID
		card := renderWorkspaceCard(cfg, st, workspace, cardWidth, selected, st.Focus == state.FocusWorkspaces)
		if selected {
			selectedLine = len(cards)
			selectedHeight = len(card)
		}
		for _, line := range card {
			cards = append(cards, strings.Repeat(" ", navHorizontalPadding)+line)
		}
	}
	hasWorkspaceCards := len(cards) > 0
	content := workspacePaneContent{lines: cards, newWorkspaceStart: -1}
	contentHeight := max(0, height-2)
	header := renderWorkspaceInfoHeader(options.workspaceInfoText, width, height)
	footer := renderWorkspaceFooter(options.workspaceFooterText, width, height, workspaceUpgradeFooterStyle)
	newWorkspaceCardHeight := len(renderNewWorkspaceTemplateCard(cfg, cardWidth, options.newWorkspaceCardSelected, st.Focus == state.FocusWorkspaces))
	minBodyHeight := 1
	if hasWorkspaceCards {
		minBodyHeight = newWorkspaceCardHeight
	}
	headerBlock := workspaceInfoHeaderBlock(header, contentHeight, len(footer), minBodyHeight)
	bodyHeight := max(0, contentHeight-len(headerBlock)-len(footer))
	if hasWorkspaceCards {
		if bodyHeight >= newWorkspaceCardHeight {
			content = appendNewWorkspaceTemplateCard(content, cfg, cardWidth, options.newWorkspaceCardSelected, st.Focus == state.FocusWorkspaces)
			if options.newWorkspaceCardSelected {
				selectedLine = content.newWorkspaceStart
				selectedHeight = newWorkspaceCardHeight
			}
		}
		scrollOffset := 0
		content.lines, scrollOffset = scrollPaneContentToRangeWithOffset(content.lines, selectedLine, selectedHeight, bodyHeight)
		if content.newWorkspaceStart >= 0 {
			content.newWorkspaceStart -= scrollOffset
			if content.newWorkspaceStart < 0 || content.newWorkspaceStart+content.newWorkspaceHeight > bodyHeight {
				content.newWorkspaceStart = -1
				content.newWorkspaceHeight = 0
			}
		}
	} else {
		content.lines = renderCenteredPaneHelpContent(max(0, width-2), bodyHeight, "No workspaces", "Press "+cfg.KeyBindings.NewWorkspace+" to add one.")
	}
	content.lines = fitPaneContent(content.lines, bodyHeight)
	if content.newWorkspaceStart >= 0 && len(headerBlock) > 0 {
		content.newWorkspaceStart += len(headerBlock)
	}
	content.lines = append(append([]string{}, headerBlock...), content.lines...)
	content.lines = append(content.lines, footer...)
	return content
}

func workspaceInfoHeaderBlock(header []string, contentHeight int, footerHeight int, minBodyHeight int) []string {
	if len(header) == 0 || len(header)+footerHeight > contentHeight {
		return nil
	}
	bodyHeight := contentHeight - len(header) - footerHeight
	if bodyHeight <= 0 {
		return append([]string{}, header...)
	}
	block := append([]string{}, header...)
	if bodyHeight > minBodyHeight {
		block = append(block, "")
	}
	if len(block)+footerHeight > contentHeight {
		return append([]string{}, header...)
	}
	return block
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

func fitPaneContent(content []string, height int) []string {
	if height <= 0 {
		return nil
	}
	next := append([]string{}, content...)
	if len(next) > height {
		next = next[:height]
	}
	for len(next) < height {
		next = append(next, "")
	}
	return next
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
	top := workspaceCardTopLineWithTitleStyle("+ New workspace", width, borderStyle, borderStyle.Italic(true))
	hint := " Press " + cfg.KeyBindings.NewWorkspace + " to create "
	body := borderStyle.Render(borderVertical) + newWorkspaceCardHintStyle.Render(padVisual(clip(hint, innerWidth), innerWidth)) + borderStyle.Render(borderVertical)
	bottom := borderStyle.Render(workspaceCardBottomLine(width))
	return []string{top, body, bottom}
}

func appendNewWorkspaceTemplateCard(content workspacePaneContent, cfg config.Config, width int, selected bool, focused bool) workspacePaneContent {
	card := renderNewWorkspaceTemplateCard(cfg, width, selected, focused)
	if len(content.lines) == 0 || len(card) == 0 {
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

func workspaceCardTopLineWithTitleStyle(title string, width int, borderStyle lipgloss.Style, titleStyle lipgloss.Style) string {
	if width < 2 {
		return ""
	}
	contentWidth := max(0, width-2)
	label := " " + strings.TrimSpace(title) + " "
	label = clip(label, contentWidth)
	padding := max(0, contentWidth-lipgloss.Width(label))
	return borderStyle.Render(borderTopLeft) + titleStyle.Render(label) + borderStyle.Render(strings.Repeat(borderHorizontal, padding)+borderTopRight)
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
	for _, task := range st.Tasks {
		if task.WorkspaceID != workspaceID {
			continue
		}
		counts.total++
		if workspaceCardTaskActive(task) {
			counts.active++
			continue
		}
		if workspaceCardTaskSilenceEnabled(st, task) && workspaceCardTaskSilenced(task) {
			counts.silenced++
			continue
		}
		counts.needsAttention++
	}
	return counts
}

func workspaceCardTaskActive(task state.Task) bool {
	return taskStatusIndicatesActivity(task)
}

func workspaceCardTaskSilenceEnabled(st state.State, task state.Task) bool {
	if task.Silent {
		return true
	}
	group := state.GroupByID(st, task.GroupID)
	return group != nil && group.Silent
}

func workspaceCardTaskSilenced(task state.Task) bool {
	switch titles.ConsolidatedStatus(task) {
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

func renderGroupsPaneWithOptions(cfg config.Config, st state.State, width int, height int, groupCursor int, options workspaceRenderOptions) []string {
	content := []string{}
	rowIndex := 0
	selectedLine := -1
	rowWidth := max(0, width-2-(navHorizontalPadding*2))
	if state.ActiveWorkspace(st) != nil {
		selected := (rowIndex == groupCursor && st.Focus == state.FocusTasks) || options.newTaskRowSelected
		taskRow := renderNewTaskTemplateRow(cfg, rowWidth, selected, st.Focus == state.FocusTasks)
		if rowIndex == groupCursor {
			selectedLine = len(content)
		}
		content = append(content, strings.Repeat(" ", navHorizontalPadding)+taskRow)
		content = append(content, "")
		rowIndex++
	}
	for _, task := range state.UngroupedTasksForWorkspace(st, st.SelectedWorkspaceID) {
		taskRow := renderTaskRow(cfg, st, task, rowWidth, false, rowIndex == groupCursor && st.Focus == state.FocusTasks, options)
		if rowIndex == groupCursor {
			selectedLine = len(content)
		}
		content = append(content, strings.Repeat(" ", navHorizontalPadding)+taskRow)
		rowIndex++
	}
	for _, group := range state.GroupsForWorkspace(st, st.SelectedWorkspaceID) {
		if rowIndex > 0 && (len(content) == 0 || content[len(content)-1] != "") {
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
		if collapsed && groupHasLoadingTask(st, group.ID, options.loadingTasks) {
			loadingMarker = loadingFrameForRender(options.loadingFrame) + " "
		}
		groupRow := rowLine(indicator+loadingMarker+silentMarker+group.Path+" ("+fmtInt(state.TaskCountForGroup(st, group.ID))+")", "", rowWidth)
		if rowIndex == groupCursor && st.Focus == state.FocusTasks {
			groupRow = activeTaskStyle.Render(padVisual(groupRow, rowWidth))
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
		for _, task := range state.TasksForGroup(st, group.ID) {
			taskRow := renderTaskRow(cfg, st, task, rowWidth, true, rowIndex == groupCursor && st.Focus == state.FocusTasks, options)
			if rowIndex == groupCursor {
				selectedLine = len(content)
			}
			content = append(content, strings.Repeat(" ", navHorizontalPadding)+taskRow)
			rowIndex++
		}
	}
	if len(content) == 0 && state.ActiveWorkspace(st) == nil {
		content = renderCenteredPaneHelp(width, height, "No workspace selected", "Press "+cfg.KeyBindings.NewWorkspace+" to add one.")
	}
	content = scrollPaneContentToLine(content, selectedLine, max(0, height-2))
	return renderPaneFrame("Tasks", "", width, height, st.Focus == state.FocusTasks, content)
}

func renderNewTaskTemplateRow(cfg config.Config, width int, selected bool, focused bool) string {
	if width <= 0 {
		return ""
	}
	row := "+ New task  Press " + cfg.KeyBindings.NewTask + " to create"
	if selected && focused {
		return activeTaskStyle.Italic(true).Render(padVisual(clip(row, width), width))
	}
	if selected {
		return activePaneStyle.Italic(true).Render(padVisual(clip(row, width), width))
	}
	return newTaskRowStyle.Render(padVisual(clip(row, width), width))
}

func groupHasLoadingTask(st state.State, groupID string, loadingTasks map[string]bool) bool {
	for _, task := range state.TasksForGroup(st, groupID) {
		if taskIsLoadingForRender(task, loadingTasks) {
			return true
		}
	}
	return false
}

func scrollPaneContentToLine(content []string, selectedLine int, visibleLines int) []string {
	scrolled, _ := scrollPaneContentToLineWithOffset(content, selectedLine, visibleLines)
	return scrolled
}

func scrollPaneContentToLineWithOffset(content []string, selectedLine int, visibleLines int) ([]string, int) {
	return scrollPaneContentToRangeWithOffset(content, selectedLine, 1, visibleLines)
}

func scrollPaneContentToRangeWithOffset(content []string, selectedLine int, selectedHeight int, visibleLines int) ([]string, int) {
	if visibleLines <= 0 || selectedLine < 0 || len(content) <= visibleLines {
		return content, 0
	}
	selectedHeight = max(1, selectedHeight)
	selectedEnd := selectedLine + selectedHeight - 1
	start := 0
	if selectedEnd >= visibleLines {
		start = selectedEnd - visibleLines + 1
	}
	if selectedLine < start {
		start = selectedLine
	}
	start = min(start, len(content)-visibleLines)
	return content[start:], start
}

func renderTaskRow(cfg config.Config, st state.State, task state.Task, width int, nested bool, selected bool, options workspaceRenderOptions) string {
	title := renderTaskTitleForState(cfg, st, task)
	marker := taskMarkerForRender(task, options.loadingFrame, options.loadingTasks)
	prefix := marker + " " + taskSilentMarkerForRender(task) + taskTypeBadgeCellForTask(cfg, task) + " "
	if duration := taskOperationDurationSuffix(task, options); duration != "" {
		prefix += duration + " "
	}
	if nested {
		prefix = "  " + prefix
	}
	row := clip(prefix+title, width)
	if selected {
		return selectedTaskRowStyle(st, task, options.loadingTasks).Render(padVisual(row, width))
	}
	if task.ID == st.ActiveTaskID {
		return activeTaskRowStyle(st, task, options.loadingTasks).Render(row)
	}
	return taskRowStyle(st, task, options.loadingTasks).Render(row)
}

func taskOperationDurationSuffix(task state.Task, options workspaceRenderOptions) string {
	if !taskIsLoadingForRender(task, options.loadingTasks) {
		return ""
	}
	if len(options.taskOperationStartedAt) == 0 {
		return ""
	}
	started, ok := options.taskOperationStartedAt[task.ID]
	if !ok || started.IsZero() {
		return ""
	}
	return formatTaskOperationDuration(time.Since(started))
}

func taskSilentMarkerForRender(task state.Task) string {
	if task.Silent {
		return "⊘ "
	}
	return ""
}

func formatTaskOperationDuration(elapsed time.Duration) string {
	seconds := int(elapsed / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	if seconds < 60 {
		return fmtInt(seconds) + "s"
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmtInt(minutes) + "m"
	}
	hours := minutes / 60
	remainder := minutes % 60
	if remainder == 0 {
		return fmtInt(hours) + "h"
	}
	return fmtInt(hours) + "h" + fmtInt(remainder) + "m"
}

func taskPaneWidthForTitleHook(st state.State, terminalWidth int) int {
	if terminalWidth <= 0 {
		return defaultTasksPaneWidth
	}
	st.NavOpen = true
	st.Focus = state.FocusTasks
	navWidth := workspaceNavFrameWidth(st, terminalWidth)
	if navWidth <= 0 {
		return defaultTasksPaneWidth
	}
	if navWidth >= minTwoPaneNavWidth {
		workspaceWidth := min(fixedWorkspacePaneWidth, max(0, navWidth-minTasksPaneWidth))
		return max(0, navWidth-workspaceWidth)
	}
	return navWidth
}

func taskTitleColumnWidth(cfg config.Config, st state.State, task state.Task, terminalWidth int) int {
	paneWidth := taskPaneWidthForTitleHook(st, terminalWidth)
	rowWidth := max(0, paneWidth-2-(navHorizontalPadding*2))
	return max(0, rowWidth-taskRowPrefixWidth(cfg, task, task.GroupID != ""))
}

func autoTitleMaxColumns(cfg config.Config, st state.State, task state.Task, terminalWidth int) int {
	columnWidth := taskTitleColumnWidth(cfg, st, task, terminalWidth)
	slots := strings.Count(task.Title, titles.AutoTemplate)
	if slots <= 0 || columnWidth <= 0 {
		return columnWidth
	}
	const sentinel = "X"
	probe := task
	probe.AutoTitle = sentinel
	probe.AutoTitleAttempted = false
	probe.AutoTitleError = ""
	rendered := renderTaskTitleForState(cfg, st, probe)
	fixedWidth := max(0, lipgloss.Width(rendered)-(slots*lipgloss.Width(sentinel)))
	return max(1, (columnWidth-fixedWidth)/slots)
}

func taskRowStyle(st state.State, task state.Task, loadingTasks map[string]bool) lipgloss.Style {
	status := titles.ConsolidatedStatus(task)
	if taskIsLoadingForRender(task, loadingTasks) {
		return taskLoadingStyleForStatus(status)
	}
	if taskRowSuppressedBySilence(st, task, loadingTasks) {
		return mutedStyle
	}
	switch status {
	case string(state.StatusReady):
		return taskReadyStyle
	case string(state.StatusRunning):
		return taskRunningStyle
	case "working":
		return taskWorkingStyle
	case string(state.StatusShipping):
		return taskShippingStyle
	case string(state.StatusError):
		return taskErrorStyle
	case string(state.StatusStopped), string(state.StatusKilled), string(state.StatusSitting):
		return taskAttentionStyle
	default:
		return taskAttentionStyle
	}
}

func selectedTaskRowStyle(st state.State, task state.Task, loadingTasks map[string]bool) lipgloss.Style {
	if taskRowSuppressedBySilence(st, task, loadingTasks) {
		return activeTaskStyle
	}
	if taskRowIsReady(task, loadingTasks) {
		return taskReadySelectedStyle
	}
	return activeTaskStyle
}

func activeTaskRowStyle(st state.State, task state.Task, loadingTasks map[string]bool) lipgloss.Style {
	if taskRowSuppressedBySilence(st, task, loadingTasks) {
		return mutedStyle
	}
	if taskRowIsReady(task, loadingTasks) {
		return taskReadyStyle
	}
	return activePaneStyle
}

func taskRowSuppressedBySilence(st state.State, task state.Task, loadingTasks map[string]bool) bool {
	return !taskIsLoadingForRender(task, loadingTasks) &&
		workspaceCardTaskSilenceEnabled(st, task) &&
		workspaceCardTaskSilenced(task)
}

func taskRowIsReady(task state.Task, loadingTasks map[string]bool) bool {
	return !taskIsLoadingForRender(task, loadingTasks) && titles.ConsolidatedStatus(task) == string(state.StatusReady)
}

func taskLoadingStyleForStatus(status string) lipgloss.Style {
	switch status {
	case string(state.StatusRunning):
		return taskRunningStyle
	case "waiting":
		return taskWorkingStyle
	case "working":
		return taskWorkingStyle
	case string(state.StatusShipping):
		return taskShippingStyle
	default:
		return taskLoadingStyle
	}
}

func taskMarkerForRender(task state.Task, loadingFrame string, loadingTasks map[string]bool) string {
	if taskIsLoadingForRender(task, loadingTasks) {
		return loadingFrameForRender(loadingFrame)
	}
	switch titles.ConsolidatedStatus(task) {
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

func taskIsLoadingForRender(task state.Task, loadingTasks map[string]bool) bool {
	return taskStatusShowsLoadingIndicator(task) || loadingTasks[task.ID]
}

func loadingFrameForRender(frame string) string {
	if strings.TrimSpace(frame) != "" {
		return frame
	}
	return loadingFrames[0]
}

func loadingTaskSet(ids []string) map[string]bool {
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
	emptyArtFrame int,
) []string {
	if width < 2 || height <= 0 {
		return nil
	}
	palette := paletteFor(active)
	innerWidth := max(0, width-2)
	taskActive := state.ActiveTask(st) != nil
	if !taskActive {
		loadingText = ""
	}
	previewMode := !navCollapsed
	topLabel := "Task Live Preview"
	topRightLabel := ""
	if previewMode && taskActive {
		topLabel = previewTopLabel(previewHeaderAnimation)
	}
	if navCollapsed && active {
		topLabel = "Task Console  " + codexCollapsedTopShortcuts(cfg)
		topRightLabel = codexConsoleTopRightLabel(st, toastText)
	} else if navCollapsed {
		topLabel = "Task Console"
	} else if taskActive {
		topRightLabel = previewTopRightLabel(title, toastText)
	}
	lines := []string{
		palette.border.Render(cornerLine(borderTopLeft, borderTopRight, borderTextLine(topLabel, topRightLabel, max(0, innerWidth-2)), innerWidth)),
	}
	contentHeight := max(0, height-2)
	empty := !taskActive
	contentWidth := codexLineContentWidth(innerWidth, previewMode)
	messageLines := renderStatusBanner(message, contentWidth, min(3, contentHeight))
	contentLines := renderCodexContent(content, contentWidth, max(0, contentHeight-len(messageLines)), empty, len(st.Workspaces) > 0, loadingText, codexEmptyTitle(previewMode), previewMode && empty, emptyArtFrame)
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
	if taskActive && navCollapsed {
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

func previewTopRightLabel(title string, toastText string) string {
	toastText = strings.TrimSpace(toastText)
	if toastText != "" {
		return toastText
	}
	return title
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
			var segment string
			segment, current = splitPlainAtWidth(word, width)
			lines = append(lines, segment)
		}
		if len(lines) == maxLines {
			return lines
		}
		for current != "" && lipgloss.Width(current) > width {
			var segment string
			segment, current = splitPlainAtWidth(current, width)
			lines = append(lines, segment)
			if len(lines) == maxLines {
				return lines
			}
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

func splitPlainAtWidth(value string, width int) (string, string) {
	if width <= 0 || value == "" {
		return "", value
	}
	runes := []rune(value)
	end := 0
	for end < len(runes) && lipgloss.Width(string(runes[:end+1])) <= width {
		end++
	}
	if end == 0 {
		end = 1
	}
	return string(runes[:end]), string(runes[end:])
}

func codexEmptyTitle(previewMode bool) string {
	if previewMode {
		return "No task selected"
	}
	return "No task open"
}

func renderCodexContent(content string, width int, height int, empty bool, canCreateTask bool, loadingText string, emptyTitle string, previewEmpty bool, emptyArtFrame int) []string {
	if height <= 0 {
		return nil
	}
	if strings.TrimSpace(loadingText) != "" {
		return renderCenteredCodexContent([]string{loadingText}, width, height)
	}
	if empty {
		return renderEmptyCodexContentWithFrame(width, height, canCreateTask, emptyTitle, previewEmpty, emptyArtFrame)
	}
	lines := lastLines(content, height)
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func renderEmptyCodexContentWithFrame(width int, height int, canCreateTask bool, title string, previewEmpty bool, emptyArtFrame int) []string {
	emptyTitle := "No task open"
	if strings.TrimSpace(title) != "" {
		emptyTitle = strings.TrimSpace(title)
	}
	hint := "Press n to create one."
	if !canCreateTask {
		hint = "Add a workspace first."
	}
	content := []string{}
	logo := emptyWeftLogo
	if previewEmpty {
		logo = previewEmptyWeftLogo
	}
	if logoFits(logo, width, height) {
		logoWidth := maxVisualWidth(logo)
		for index, line := range logo {
			content = append(content, renderEmptyLogoLine(line, logoWidth, previewEmpty, emptyArtFrame, index))
		}
		content = append(content, "")
		content = append(content, centerVisual(emptyTitle, logoWidth), centerVisual(hint, logoWidth))
		return renderCenteredCodexBlockContent(content, width, height, logoWidth)
	}
	content = append(content, emptyTitle, hint)
	return renderCenteredCodexContent(content, width, height)
}

func renderEmptyLogoLine(line string, logoWidth int, animated bool, frame int, row int) string {
	line = padVisual(line, logoWidth)
	if !animated {
		return emptyLogoStyle.Render(line)
	}
	return renderPreviewLogoLine(line, row, frame)
}

type logoRange struct {
	start int
	end   int
}

const (
	previewLogoAccentWidth  = 2
	previewLogoAccentHold   = 4
	previewLogoGraphWidth   = 14
	previewLogoAccentSteps  = previewLogoGraphWidth / previewLogoAccentWidth
	previewLogoActiveFrames = previewLogoAccentSteps * previewLogoAccentHold
	previewLogoPauseFrames  = 36
	previewLogoCycleFrames  = previewLogoActiveFrames + previewLogoPauseFrames
)

func renderPreviewLogoLine(line string, row int, frame int) string {
	ranges := previewLogoAccentRanges(row, frame)
	if len(ranges) == 0 {
		return emptyLogoStyle.Render(line)
	}
	runes := []rune(line)
	var builder strings.Builder
	cursor := 0
	for _, r := range ranges {
		start := min(max(r.start, cursor), len(runes))
		end := min(max(r.end, start), len(runes))
		if start > cursor {
			builder.WriteString(emptyLogoStyle.Render(string(runes[cursor:start])))
		}
		if end > start {
			builder.WriteString(emptyLogoAccentStyle.Render(string(runes[start:end])))
		}
		cursor = end
	}
	if cursor < len(runes) {
		builder.WriteString(emptyLogoStyle.Render(string(runes[cursor:])))
	}
	return builder.String()
}

func previewLogoAccentRanges(row int, frame int) []logoRange {
	if frame < 0 {
		frame = 0
	}
	frame %= previewLogoCycleFrames
	if frame >= previewLogoActiveFrames {
		return nil
	}
	start := ((frame / previewLogoAccentHold) % previewLogoAccentSteps) * previewLogoAccentWidth
	end := start + previewLogoAccentWidth
	switch row {
	case 0, 4:
		if start <= 6 {
			return []logoRange{{start: start, end: end}}
		}
	case 1, 3:
		if start == 6 {
			return []logoRange{{start: start, end: end}}
		}
	case 2:
		return []logoRange{{start: start, end: end}}
	}
	return nil
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
	return cfg.KeyBindings.Drawer + " dashboard  " + cfg.KeyBindings.Repaint + " repaint"
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
	count := otherReadyTaskCount(st)
	if count == 0 {
		return ""
	}
	noun := "task"
	if count != 1 {
		noun = "tasks"
	}
	return workspaceCountNeedsAttentionStyle.Render(fmtInt(count) + " other " + noun + " ready")
}

func otherReadyTaskCount(st state.State) int {
	active := state.ActiveTask(st)
	if active == nil {
		return 0
	}
	count := 0
	for _, task := range st.Tasks {
		if task.ID == active.ID {
			continue
		}
		if titles.ConsolidatedStatus(task) == string(state.StatusReady) {
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
