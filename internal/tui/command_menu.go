package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/weft/internal/state"
)

type commandAction string

const (
	commandActionRepaint  commandAction = "repaint"
	commandActionCopyPane commandAction = "copy-pane"
)

type commandMenuItem struct {
	key    string
	label  string
	action commandAction
}

func commandMenuItems() []commandMenuItem {
	return []commandMenuItem{
		{key: "r", label: "Repaint", action: commandActionRepaint},
		{key: "c", label: "Copy full task console", action: commandActionCopyPane},
	}
}

func (m ClientModel) renderCommandMenu() string {
	layout := m.taskPanelLayout()
	contextLines := renderTaskPanelContextRows(layout.contextLines, layout.contextBodyWidth(), layout.contextHeadingRows)
	if m.mode == modeCommand && m.mouseSelection.active {
		contextLines = strings.Split(selectedStyledCodexContent(strings.Join(contextLines, "\n"), m.mouseSelection, layout.contextBodyWidth()), "\n")
	}
	return renderTaskPanel(m.commandMenuIndex, layout, contextLines)
}

func (m ClientModel) taskPanelLayout() taskPanelLayout {
	heading := ""
	detail := ""
	if context := m.snapshot.ActiveTaskContext; context != nil {
		heading = strings.TrimSpace(context.Heading)
		detail = strings.TrimSpace(context.Detail)
	}
	return newTaskPanelLayout(m.taskPanelContentWidth(), max(12, m.height-8), heading, detail)
}

func (m ClientModel) taskPanelContentWidth() int {
	return max(56, m.taskPanelModalWidth()-4)
}

func (m ClientModel) taskPanelModalWidth() int {
	return max(60, min(m.width-12, 126))
}

type taskPanelLayout struct {
	width              int
	height             int
	split              bool
	contextX           int
	contextY           int
	contextWidth       int
	contextHeight      int
	shortcutX          int
	shortcutY          int
	shortcutWidth      int
	shortcutHeight     int
	contextLines       []string
	contextHeadingRows int
}

func (l taskPanelLayout) contextBodyWidth() int {
	return max(0, l.contextWidth-2)
}

func (l taskPanelLayout) contextBodyHeight() int {
	return max(0, l.contextHeight-2)
}

func newTaskPanelLayout(width int, height int, heading string, detail string) taskPanelLayout {
	width = max(56, width)
	height = max(12, height)
	const sectionTop = 2
	availableHeight := max(8, height-sectionTop)
	split := width >= 86
	layout := taskPanelLayout{width: width, height: height, split: split}
	if split {
		layout.shortcutWidth = 30
		layout.contextWidth = max(46, width-layout.shortcutWidth-2)
		layout.contextHeight = max(10, min(availableHeight, 26))
		layout.shortcutHeight = layout.contextHeight
		layout.contextY = sectionTop
		layout.shortcutX = layout.contextWidth + 2
		layout.shortcutY = sectionTop
	} else {
		layout.contextWidth = width
		layout.contextHeight = max(8, min(availableHeight-9, 18))
		layout.shortcutWidth = width
		layout.shortcutHeight = 8
		layout.contextY = sectionTop
		layout.shortcutY = sectionTop + layout.contextHeight + 1
	}
	layout.contextLines = taskPanelContextPlainLines(heading, detail, layout.contextBodyWidth(), layout.contextBodyHeight())
	if normalizedHeading := strings.Join(strings.Fields(heading), " "); normalizedHeading != "" {
		layout.contextHeadingRows = len(wrapPlain(normalizedHeading, layout.contextBodyWidth(), layout.contextBodyHeight()))
	}
	return layout
}

func renderTaskPanel(selected int, layout taskPanelLayout, contextLines []string) string {
	lines := []string{modalTitleStyle.Render("Task Tools"), ""}
	contextBox := renderTaskPanelBox("Task Notes", contextLines, layout.contextWidth, layout.contextHeight)
	shortcutBox := renderTaskPanelBox("Console Commands", renderTaskPanelShortcutRows(selected, layout.shortcutWidth-2, layout.shortcutHeight-2), layout.shortcutWidth, layout.shortcutHeight)
	if layout.split {
		for index := 0; index < max(len(contextBox), len(shortcutBox)); index++ {
			lines = append(lines, lineAt(contextBox, index, layout.contextWidth)+"  "+lineAt(shortcutBox, index, layout.shortcutWidth))
		}
		return strings.Join(lines, "\n")
	}
	lines = append(lines, contextBox...)
	lines = append(lines, "")
	lines = append(lines, shortcutBox...)
	return strings.Join(lines, "\n")
}

func renderTaskPanelShortcutRows(selected int, width int, height int) []string {
	items := commandMenuItems()
	selected = clampCommandMenuIndex(selected, len(items))
	lines := make([]string, 0, height)
	for index, item := range items {
		text := fmt.Sprintf("%s  %s", item.key, item.label)
		if index == selected {
			lines = append(lines, modalSuggestionSelectedStyle.Render(padVisual(clip(text, width), width)))
			continue
		}
		lines = append(lines, modalKeyStyle.Render(item.key)+"  "+modalValueStyle.Render(item.label))
	}
	lines = append(lines, "")
	lines = append(lines, modalKeyStyle.Render("Enter")+"  "+modalValueStyle.Render("Run selected"))
	lines = append(lines, modalKeyStyle.Render("↑/↓")+"    "+modalValueStyle.Render("Select"))
	lines = append(lines, modalKeyStyle.Render("Esc")+"   "+modalValueStyle.Render("Close"))
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		return lines[:height]
	}
	return lines
}

func taskPanelContextPlainLines(heading string, detail string, width int, maxLines int) []string {
	heading = strings.Join(strings.Fields(heading), " ")
	detail = strings.TrimSpace(strings.ReplaceAll(detail, "\r", ""))
	if maxLines <= 0 {
		return nil
	}
	lines := []string{}
	if heading == "" && detail == "" {
		return fitTaskPanelRows([]string{"No task notes set."}, maxLines)
	}
	if heading != "" {
		lines = append(lines, wrapPlain(heading, width, maxLines)...)
	}
	if detail != "" {
		if heading != "" {
			lines = append(lines, "")
		}
		for _, raw := range strings.Split(detail, "\n") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				lines = append(lines, "")
				continue
			}
			lines = append(lines, wrapPlain(raw, width, maxLines)...)
		}
	}
	return fitTaskPanelRows(lines, maxLines)
}

func fitTaskPanelRows(lines []string, height int) []string {
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func renderTaskPanelContextRows(lines []string, width int, headingRows int) []string {
	rendered := make([]string, 0, len(lines))
	for index, line := range lines {
		switch line {
		case "No task notes set.":
			rendered = append(rendered, mutedStyle.Render(line))
		default:
			if index < headingRows && strings.TrimSpace(line) != "" {
				rendered = append(rendered, taskPanelLeadStyle.Render(clip(line, width)))
				continue
			}
			rendered = append(rendered, modalValueStyle.Render(clip(line, width)))
		}
	}
	return rendered
}

func renderTaskPanelBox(title string, body []string, width int, height int) []string {
	if width < 4 || height < 2 {
		return nil
	}
	innerWidth := max(0, width-2)
	lines := []string{renderTaskPanelBoxTop(title, width)}
	for index := 0; index < height-2; index++ {
		lines = append(lines, taskPanelBorderStyle.Render(borderVertical)+lineAt(body, index, innerWidth)+taskPanelBorderStyle.Render(borderVertical))
	}
	lines = append(lines, taskPanelBorderStyle.Render(borderBottomLeft+strings.Repeat(borderHorizontal, innerWidth)+borderBottomRight))
	return lines
}

func renderTaskPanelBoxTop(title string, width int) string {
	innerWidth := max(0, width-2)
	if strings.TrimSpace(title) == "" {
		return taskPanelBorderStyle.Render(borderTopLeft + strings.Repeat(borderHorizontal, innerWidth) + borderTopRight)
	}
	title = " " + strings.Join(strings.Fields(title), " ") + " "
	if lipgloss.Width(title)+2 >= innerWidth {
		return taskPanelBorderStyle.Render(borderTopLeft + strings.Repeat(borderHorizontal, innerWidth) + borderTopRight)
	}
	right := innerWidth - lipgloss.Width(title) - 1
	return taskPanelBorderStyle.Render(borderTopLeft+borderHorizontal) +
		taskPanelTitleStyle.Render(title) +
		taskPanelBorderStyle.Render(strings.Repeat(borderHorizontal, right)+borderTopRight)
}

func (m *ClientModel) startCommandMenu() {
	if m.mode != modeCommand {
		m.commandMenuReturnMode = m.mode
		m.commandMenuIndex = 0
	}
	m.mode = modeCommand
	m.commandMenuIndex = clampCommandMenuIndex(m.commandMenuIndex, len(commandMenuItems()))
	m.mouseSelection = consoleSelection{}
	if m.inputRouter != nil {
		m.inputRouter.SetTaskInputMode(taskInputNone)
	}
}

func (m *ClientModel) closeCommandMenu() {
	next := m.commandMenuReturnMode
	if next == modeCommand {
		next = modeNormal
	}
	m.mode = next
	m.commandMenuReturnMode = modeNormal
	m.syncInputRouter()
}

func (m ClientModel) handleCommandMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := commandMenuItems()
	switch {
	case msg.Type == tea.KeyEsc || strings.EqualFold(msg.String(), "q"):
		m.closeCommandMenu()
		return m, nil
	case msg.Type == tea.KeyUp || bindingMatches(m.cfg.KeyBindings.SelectPrev, msg):
		m.commandMenuIndex = clampCommandMenuIndex(m.commandMenuIndex-1, len(items))
		return m, nil
	case msg.Type == tea.KeyDown || bindingMatches(m.cfg.KeyBindings.SelectNext, msg):
		m.commandMenuIndex = clampCommandMenuIndex(m.commandMenuIndex+1, len(items))
		return m, nil
	case msg.Type == tea.KeyEnter:
		item := items[clampCommandMenuIndex(m.commandMenuIndex, len(items))]
		return m.runCommandMenuAction(item.action)
	}
	for _, item := range items {
		if strings.EqualFold(msg.String(), item.key) {
			return m.runCommandMenuAction(item.action)
		}
	}
	return m, nil
}

func (m ClientModel) runCommandMenuAction(action commandAction) (tea.Model, tea.Cmd) {
	m.closeCommandMenu()
	switch action {
	case commandActionRepaint:
		return m, m.repaintClient()
	case commandActionCopyPane:
		next, cmd := m.copyTaskPaneToClipboard()
		return next, cmd
	default:
		return m, nil
	}
}

func (m ClientModel) copyTaskPaneToClipboard() (ClientModel, tea.Cmd) {
	text := m.taskPaneClipboardText()
	if strings.TrimSpace(text) == "" {
		m.message = "no task pane content to copy"
		return m, nil
	}
	if err := writeClipboard(text); err != nil {
		m.message = "copy failed: " + err.Error()
		return m, nil
	}
	count := len([]rune(text))
	return m, m.setToast(fmt.Sprintf("Copied %d character%s", count, plural(count)))
}

func (m ClientModel) taskPaneClipboardText() string {
	st := codexFrameStateForSelection(m.dashboardState(), m.snapshot.GroupCursor)
	if state.ActiveTask(st) == nil {
		return ""
	}
	lines := m.codexScrollbackPlainLines()
	if len(lines) == 0 {
		return ""
	}
	return commandPaneClipboardText(lines)
}

func commandPaneClipboardText(lines []string) string {
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned = append(cleaned, strings.TrimRight(line, " \t"))
	}
	for len(cleaned) > 0 && cleaned[len(cleaned)-1] == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}
	return strings.Join(cleaned, "\n")
}

func clampCommandMenuIndex(index int, length int) int {
	if length <= 0 {
		return 0
	}
	if index < 0 {
		return length - 1
	}
	if index >= length {
		return 0
	}
	return index
}
