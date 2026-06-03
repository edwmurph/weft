package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
	detail string
	action commandAction
}

func commandMenuItems() []commandMenuItem {
	return []commandMenuItem{
		{key: "r", label: "Repaint", detail: "Refresh and redraw the dashboard", action: commandActionRepaint},
		{key: "c", label: "Copy pane content", detail: "Copy plain task output for debugging", action: commandActionCopyPane},
	}
}

func renderCommandMenu(selected int) string {
	items := commandMenuItems()
	selected = clampCommandMenuIndex(selected, len(items))
	lines := []string{modalTitleStyle.Render("Command palette"), ""}
	for index, item := range items {
		text := fmt.Sprintf("%s  %s", item.key, item.label)
		if item.detail != "" {
			text += "  " + item.detail
		}
		if index == selected {
			lines = append(lines, modalSuggestionSelectedStyle.Render(text))
			continue
		}
		lines = append(lines, modalKeyStyle.Render(item.key)+"  "+modalValueStyle.Render(item.label)+"  "+modalLabelStyle.Render(item.detail))
	}
	lines = append(lines, "", modalKeyStyle.Render("Enter")+" run  "+modalKeyStyle.Render("↑/↓")+" move  "+modalKeyStyle.Render("Esc")+" close")
	return strings.Join(lines, "\n")
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
