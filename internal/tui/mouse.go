package tui

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/weft/internal/state"
)

const (
	consoleSelectionANSIStart = "\x1b[38;2;0;0;0;48;2;135;215;255m"
	consoleSelectionANSIEnd   = "\x1b[0m"

	maxConsoleSelectionMargin = 8
)

var (
	consoleSelectionForeground = color.RGBA{R: 0, G: 0, B: 0, A: 0xff}
	consoleSelectionBackground = color.RGBA{R: 135, G: 215, B: 255, A: 0xff}
)

var writeClipboard = clipboard.WriteAll

type consolePoint struct {
	col int
	row int
}

type consoleArea struct {
	x         int
	y         int
	width     int
	height    int
	colOffset int
}

type consoleSelection struct {
	active    bool
	start     consolePoint
	end       consolePoint
	colOffset int
}

func (m ClientModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeNormal {
		return m, nil
	}
	event := tea.MouseEvent(msg)
	if next, cmd, handled := m.handleWorkspaceMouse(event); handled {
		if cmd != nil || next.newWorkspaceCardSelected || event.Action == tea.MouseActionPress {
			return next, cmd
		}
		m = next
	}
	if next, cmd, handled := m.handleTaskMouse(event); handled {
		return next, cmd
	}
	active := state.ActiveTask(m.snapshot.State)
	if active == nil {
		m.mouseSelection = consoleSelection{}
		return m, nil
	}
	switch event.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown, tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight:
		frameArea, ok := m.codexFrameArea()
		if !ok {
			return m, nil
		}
		if !mouseInConsoleArea(event, frameArea) {
			return m, nil
		}
		return m.scrollCodexHistory(event), nil
	}
	if !m.canSelectCodexContent() {
		m.mouseSelection = consoleSelection{}
		return m, nil
	}
	_, ok := m.codexContentArea()
	if !ok {
		m.mouseSelection = consoleSelection{}
		return m, nil
	}
	switch event.Button {
	case tea.MouseButtonLeft:
		switch event.Action {
		case tea.MouseActionPress:
			area, ok := m.codexSelectionArea()
			if !ok {
				return m, nil
			}
			point, ok := consolePointFromMouse(event, area)
			if !ok {
				return m, nil
			}
			m.mouseSelection = consoleSelection{active: true, start: point, end: point, colOffset: area.colOffset}
			return m, nil
		case tea.MouseActionMotion:
			if !m.mouseSelection.active {
				return m, nil
			}
			area, ok := m.codexSelectionAreaForOffset(m.mouseSelection.colOffset)
			if !ok {
				return m, nil
			}
			m.mouseSelection.end = clampConsolePoint(event, area)
			return m, nil
		}
	}
	if event.Action == tea.MouseActionMotion && m.mouseSelection.active {
		area, ok := m.codexSelectionAreaForOffset(m.mouseSelection.colOffset)
		if !ok {
			return m, nil
		}
		m.mouseSelection.end = clampConsolePoint(event, area)
		return m, nil
	}
	if event.Action != tea.MouseActionRelease || !m.mouseSelection.active {
		return m, nil
	}
	area, ok := m.codexSelectionAreaForOffset(m.mouseSelection.colOffset)
	if !ok {
		m.mouseSelection = consoleSelection{}
		return m, nil
	}
	m.mouseSelection.end = clampConsolePoint(event, area)
	text := selectedConsoleText(m.codexPlainLines(), m.mouseSelection, area.width)
	m.mouseSelection = consoleSelection{}
	if strings.TrimSpace(text) == "" {
		return m, nil
	}
	if err := writeClipboard(text); err != nil {
		m.message = "copy failed: " + err.Error()
		return m, nil
	}
	cmd := m.setToast(fmt.Sprintf("Copied %d character%s", len([]rune(text)), plural(len([]rune(text)))))
	return m, cmd
}

func (m ClientModel) handleWorkspaceMouse(event tea.MouseEvent) (ClientModel, tea.Cmd, bool) {
	if event.Action != tea.MouseActionPress && event.Action != tea.MouseActionMotion {
		return m, nil, false
	}
	area, ok := m.newWorkspaceCardArea()
	if ok && mouseInConsoleArea(event, area) {
		alreadyFocused := m.snapshot.State.Focus == state.FocusWorkspaces
		m.mouseSelection = consoleSelection{}
		m.newWorkspaceCardSelected = true
		m.snapshot.State.Focus = state.FocusWorkspaces
		m.snapshot.State.NavOpen = true
		if event.Action == tea.MouseActionPress || !alreadyFocused {
			return m, m.request("focus", map[string]string{"target": "workspaces"}), true
		}
		return m, nil, true
	}
	if event.Action == tea.MouseActionMotion && m.newWorkspaceCardSelected {
		m.newWorkspaceCardSelected = false
		return m, nil, true
	}
	if event.Action == tea.MouseActionPress {
		m.newWorkspaceCardSelected = false
	}
	return m, nil, false
}

func (m ClientModel) newWorkspaceCardArea() (consoleArea, bool) {
	return newWorkspaceTemplateCardAreaFor(m.cfg, m.dashboardState(), m.width, m.height, m.snapshot.NavWidth, m.workspaceRenderOptions())
}

func (m ClientModel) handleTaskMouse(event tea.MouseEvent) (ClientModel, tea.Cmd, bool) {
	if event.Action != tea.MouseActionPress && event.Action != tea.MouseActionMotion {
		return m, nil, false
	}
	area, ok := m.newTaskRowArea()
	if ok && mouseInConsoleArea(event, area) {
		alreadySelected := m.newTaskRowSelected && m.snapshot.State.Focus == state.FocusTasks
		m.mouseSelection = consoleSelection{}
		m.newWorkspaceCardSelected = false
		m.newTaskRowSelected = true
		m.snapshot.State.Focus = state.FocusTasks
		m.snapshot.State.NavOpen = true
		m.snapshot.GroupCursor = 0
		if event.Action == tea.MouseActionPress || !alreadySelected {
			return m, m.request("select_new_task", nil), true
		}
		return m, nil, true
	}
	if event.Action == tea.MouseActionMotion && m.newTaskRowSelected {
		m.newTaskRowSelected = false
		return m, nil, true
	}
	if event.Action == tea.MouseActionPress {
		m.newTaskRowSelected = false
	}
	return m, nil, false
}

func (m ClientModel) newTaskRowArea() (consoleArea, bool) {
	return newTaskTemplateRowAreaFor(m.cfg, m.dashboardState(), m.width, m.height, m.snapshot.NavWidth)
}

func (m ClientModel) codexContentArea() (consoleArea, bool) {
	return codexContentAreaFor(m.snapshot.State, m.width, m.height, m.snapshot.NavWidth, m.messageText())
}

func (m ClientModel) codexFrameArea() (consoleArea, bool) {
	return codexFrameAreaFor(m.width, m.height, m.snapshot.NavWidth)
}

func (m ClientModel) codexSelectionArea() (consoleArea, bool) {
	return m.codexSelectionAreaForOffset(codexSelectableMargin(m.codexPlainLines()))
}

func (m ClientModel) codexSelectionAreaForOffset(offset int) (consoleArea, bool) {
	area, ok := m.codexContentArea()
	if !ok {
		return consoleArea{}, false
	}
	offset = min(max(0, offset), max(0, area.width-1))
	area.x += offset
	area.width -= offset
	area.colOffset = offset
	if area.width <= 0 {
		return consoleArea{}, false
	}
	return area, true
}

func (m ClientModel) canSelectCodexContent() bool {
	st := codexFrameStateForSelection(m.dashboardState(), m.snapshot.GroupCursor)
	if state.ActiveTask(st) == nil {
		return false
	}
	return st.Focus == state.FocusConsole || st.NavOpen
}

func (m ClientModel) codexPlainLines() []string {
	return m.codexVisiblePlainLines()
}

func codexContentAreaFor(st state.State, width int, height int, navWidth int, message string) (consoleArea, bool) {
	if width <= 0 || height <= 2 {
		return consoleArea{}, false
	}
	navWidth = min(max(0, navWidth), width)
	codexWidth := width - navWidth
	navOnly := navWidth >= width
	if !navOnly && codexWidth < minCodexPaneWidth && navWidth > 0 {
		codexWidth = min(width, minCodexPaneWidth)
		navWidth = width - codexWidth
	}
	if codexWidth <= 2 {
		return consoleArea{}, false
	}
	frameX := navWidth
	navCollapsed := navWidth <= 0
	innerWidth := max(0, codexWidth-2)
	contentHeight := max(0, height-2)
	contentWidth := codexLineContentWidth(innerWidth, !navCollapsed)
	if contentWidth <= 0 || contentHeight <= 0 {
		return consoleArea{}, false
	}
	messageLines := renderStatusBanner(message, contentWidth, min(3, contentHeight))
	contentHeight = max(0, contentHeight-len(messageLines))
	if contentHeight <= 0 {
		return consoleArea{}, false
	}
	return consoleArea{
		x:      frameX + 1 + min(codexLeftPadding, innerWidth),
		y:      1 + len(messageLines),
		width:  contentWidth,
		height: contentHeight,
	}, true
}

func codexFrameAreaFor(width int, height int, navWidth int) (consoleArea, bool) {
	if width <= 0 || height <= 0 {
		return consoleArea{}, false
	}
	navWidth = min(max(0, navWidth), width)
	codexWidth := width - navWidth
	navOnly := navWidth >= width
	if !navOnly && codexWidth < minCodexPaneWidth && navWidth > 0 {
		codexWidth = min(width, minCodexPaneWidth)
		navWidth = width - codexWidth
	}
	if codexWidth <= 0 {
		return consoleArea{}, false
	}
	return consoleArea{x: navWidth, y: 0, width: codexWidth, height: height}, true
}

func mouseInConsoleArea(event tea.MouseEvent, area consoleArea) bool {
	_, ok := consolePointFromMouse(event, area)
	return ok
}

func consolePointFromMouse(event tea.MouseEvent, area consoleArea) (consolePoint, bool) {
	if event.X < area.x || event.X >= area.x+area.width || event.Y < area.y || event.Y >= area.y+area.height {
		return consolePoint{}, false
	}
	return consolePoint{col: event.X - area.x, row: event.Y - area.y}, true
}

func clampConsolePoint(event tea.MouseEvent, area consoleArea) consolePoint {
	return consolePoint{
		col: min(max(0, event.X-area.x), max(0, area.width-1)),
		row: min(max(0, event.Y-area.y), max(0, area.height-1)),
	}
}

func (m ClientModel) scrollCodexHistory(event tea.MouseEvent) ClientModel {
	delta := 3
	switch event.Button {
	case tea.MouseButtonWheelUp:
		m.codexScrollOffset = min(m.maxCodexScrollOffset(), m.codexScrollOffset+delta)
	case tea.MouseButtonWheelDown:
		m.codexScrollOffset = max(0, m.codexScrollOffset-delta)
	}
	return m
}

func (m ClientModel) maxCodexScrollOffset() int {
	_, height := m.codexVisibleSize()
	return max(0, len(m.codexScrollbackPlainLines())-height)
}

func (m ClientModel) codexVisibleContent() string {
	lines := strings.Split(m.codexScrollbackContent(), "\n")
	return strings.Join(codexViewportLines(lines, m.codexVisibleHeight(), m.codexScrollOffset), "\n")
}

func (m ClientModel) codexVisiblePlainLines() []string {
	return codexViewportLines(m.codexScrollbackPlainLines(), m.codexVisibleHeight(), m.codexScrollOffset)
}

func (m ClientModel) codexScrollbackContent() string {
	if m.snapshot.CodexScrollback != "" {
		return m.snapshot.CodexScrollback
	}
	return m.snapshot.CodexContent
}

func (m ClientModel) codexScrollbackPlainLines() []string {
	if len(m.snapshot.CodexScrollbackLines) > 0 {
		return m.snapshot.CodexScrollbackLines
	}
	if len(m.snapshot.CodexPlainLines) > 0 {
		return m.snapshot.CodexPlainLines
	}
	if strings.TrimSpace(m.snapshot.CodexContent) == "" {
		return nil
	}
	return strings.Split(ansi.Strip(m.snapshot.CodexContent), "\n")
}

func (m ClientModel) codexVisibleHeight() int {
	_, height := m.codexVisibleSize()
	return height
}

func (m ClientModel) codexVisibleSize() (int, int) {
	area, ok := m.codexContentArea()
	if !ok {
		return 0, 0
	}
	return area.width, area.height
}

func codexViewportLines(lines []string, height int, scrollOffset int) []string {
	if height <= 0 || len(lines) == 0 {
		return nil
	}
	scrollOffset = min(max(0, scrollOffset), max(0, len(lines)-height))
	end := len(lines) - scrollOffset
	start := max(0, end-height)
	return append([]string(nil), lines[start:end]...)
}

func selectedCodexContent(lines []string, selection consoleSelection, width int) string {
	if width <= 0 || len(lines) == 0 {
		return ""
	}
	start, end := normalizedSelection(selection)
	contentWidth := width + selection.colOffset
	rendered := make([]string, len(lines))
	for row, line := range lines {
		if row < start.row || row > end.row {
			rendered[row] = line
			continue
		}
		left := selection.colOffset
		right := selection.colOffset + width - 1
		if row == start.row {
			left = selection.colOffset + start.col
		}
		if row == end.row {
			right = selection.colOffset + end.col
		}
		rendered[row] = highlightedConsoleLine(line, left, right, contentWidth)
	}
	return strings.Join(rendered, "\n")
}

func selectedStyledCodexContent(content string, selection consoleSelection, width int) string {
	if width <= 0 || content == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r", ""), "\n")
	start, end := normalizedSelection(selection)
	contentWidth := width + selection.colOffset
	if contentWidth <= 0 {
		return content
	}
	rendered := make([]string, len(lines))
	for row, line := range lines {
		if row < start.row || row > end.row {
			rendered[row] = line
			continue
		}
		left := selection.colOffset
		right := selection.colOffset + width - 1
		if row == start.row {
			left = selection.colOffset + start.col
		}
		if row == end.row {
			right = selection.colOffset + end.col
		}
		rendered[row] = highlightedStyledConsoleLine(line, left, right, contentWidth)
	}
	return strings.Join(rendered, "\n")
}

func selectedConsoleText(lines []string, selection consoleSelection, width int) string {
	if width <= 0 || len(lines) == 0 {
		return ""
	}
	start, end := normalizedSelection(selection)
	if start == end {
		return ""
	}
	contentWidth := width + selection.colOffset
	parts := make([]string, 0, end.row-start.row+1)
	for row := start.row; row <= end.row && row < len(lines); row++ {
		line := paddedRunes(lineAtPlain(lines, row), contentWidth)
		left := selection.colOffset
		right := selection.colOffset + width - 1
		if row == start.row {
			left = selection.colOffset + start.col
		}
		if row == end.row {
			right = selection.colOffset + end.col
		}
		if left > right || left >= len(line) {
			parts = append(parts, "")
			continue
		}
		right = min(right, len(line)-1)
		parts = append(parts, strings.TrimRight(string(line[left:right+1]), " "))
	}
	for len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	for len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "\n")
}

func codexSelectableMargin(lines []string) int {
	margin := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		leading := leadingWhitespaceCells(line)
		if leading == 0 || leading > maxConsoleSelectionMargin {
			continue
		}
		if margin == 0 || leading < margin {
			margin = leading
		}
	}
	return margin
}

func leadingWhitespaceCells(value string) int {
	cells := 0
	for _, r := range value {
		if r != ' ' && r != '\t' {
			return cells
		}
		cells++
	}
	return cells
}

func normalizedSelection(selection consoleSelection) (consolePoint, consolePoint) {
	start := selection.start
	end := selection.end
	if start.row > end.row || (start.row == end.row && start.col > end.col) {
		start, end = end, start
	}
	return start, end
}

func highlightedConsoleLine(line string, left int, right int, width int) string {
	runes := paddedRunes(line, width)
	left = min(max(0, left), max(0, width-1))
	right = min(max(left, right), max(0, width-1))
	return string(runes[:left]) + consoleSelectionANSIStart + string(runes[left:right+1]) + consoleSelectionANSIEnd + string(runes[right+1:])
}

func highlightedStyledConsoleLine(line string, left int, right int, width int) string {
	if width <= 0 {
		return ""
	}
	screen := NewTerminalScreen(width, 1)
	screen.Write(line)
	left = min(max(0, left), max(0, width-1))
	right = min(max(left, right), max(0, width-1))
	for col := left; col <= right; col++ {
		style := screen.cells[0][col].style
		style.Fg = consoleSelectionForeground
		style.Bg = consoleSelectionBackground
		style.Reverse(false)
		screen.cells[0][col].style = style
	}
	return screen.ANSIString()
}

func lineAtPlain(lines []string, row int) string {
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

func paddedRunes(value string, width int) []rune {
	runes := []rune(value)
	if len(runes) > width {
		return runes[:width]
	}
	for len(runes) < width {
		runes = append(runes, ' ')
	}
	return runes
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
