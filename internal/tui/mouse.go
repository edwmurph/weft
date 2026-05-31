package tui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/weft/internal/state"
)

const (
	ansiReverseStart = "\x1b[7m"
	ansiReverseEnd   = "\x1b[27m"

	maxConsoleSelectionMargin = 8
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
	active := state.ActiveAgent(m.snapshot.State)
	if m.snapshot.State.Focus != state.FocusCodex || active == nil {
		m.mouseSelection = consoleSelection{}
		return m, nil
	}
	contentArea, ok := m.codexContentArea()
	if !ok {
		m.mouseSelection = consoleSelection{}
		return m, nil
	}
	event := tea.MouseEvent(msg)
	switch event.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown, tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight:
		point, ok := consolePointFromMouse(event, contentArea)
		if !ok {
			return m, nil
		}
		args := codexMouseInputArgs(event, point)
		return m.enqueueCodexInput(args)
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

func (m ClientModel) codexContentArea() (consoleArea, bool) {
	return codexContentAreaFor(m.snapshot.State, m.width, m.height, m.snapshot.NavWidth, m.messageText())
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

func (m ClientModel) codexPlainLines() []string {
	if len(m.snapshot.CodexPlainLines) > 0 {
		return m.snapshot.CodexPlainLines
	}
	if strings.TrimSpace(m.snapshot.CodexContent) == "" {
		return nil
	}
	return strings.Split(ansi.Strip(m.snapshot.CodexContent), "\n")
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

func codexMouseInputArgs(event tea.MouseEvent, point consolePoint) map[string]string {
	button := 64
	switch event.Button {
	case tea.MouseButtonWheelDown:
		button = 65
	case tea.MouseButtonWheelLeft:
		button = 66
	case tea.MouseButtonWheelRight:
		button = 67
	}
	if event.Shift {
		button += 4
	}
	if event.Alt {
		button += 8
	}
	if event.Ctrl {
		button += 16
	}
	return map[string]string{
		"input":   "mouse",
		"encoded": fmt.Sprintf("\x1b[<%d;%d;%dM", button, point.col+1, point.row+1),
	}
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
	return string(runes[:left]) + ansiReverseStart + string(runes[left:right+1]) + ansiReverseEnd + string(runes[right+1:])
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
		style.Reverse(true)
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
