package tui

import (
	"image/color"
	"net/url"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/mattn/go-runewidth"
)

type terminalCell struct {
	r     rune
	style cellbuf.Style
}

type terminalCursorShape int

const (
	terminalCursorBlock terminalCursorShape = iota
	terminalCursorUnderline
	terminalCursorBar
)

type terminalCursorOverlay struct {
	col   int
	shape terminalCursorShape
}

const terminalScrollbackLimit = 2000

type TerminalScreen struct {
	cols          int
	rows          int
	cells         [][]terminalCell
	history       [][]terminalCell
	row           int
	col           int
	cursorVisible bool
	cursorShape   terminalCursorShape
	cwd           string
	savedRow      int
	savedCol      int
	scrollTop     int
	scrollBottom  int
	originMode    bool
	style         cellbuf.Style
	defaults      cellbuf.Style
	parser        *ansi.Parser
}

func NewTerminalScreen(cols int, rows int) *TerminalScreen {
	screen := &TerminalScreen{cols: max(1, cols), rows: max(1, rows), cursorVisible: true}
	screen.resetScrollRegion()
	screen.clearAll()
	parser := ansi.NewParser()
	parser.SetHandler(ansi.Handler{
		Print:     screen.print,
		Execute:   screen.execute,
		HandleCsi: screen.handleCSI,
		HandleEsc: screen.handleESC,
		HandleOsc: screen.handleOSC,
		HandleDcs: func(ansi.Cmd, ansi.Params, []byte) {},
		HandlePm:  func([]byte) {},
		HandleApc: func([]byte) {},
		HandleSos: func([]byte) {},
	})
	screen.parser = parser
	return screen
}

func (s *TerminalScreen) Resize(cols int, rows int) {
	s.resize(cols, rows, false)
}

func (s *TerminalScreen) ResizeTopAligned(cols int, rows int) {
	s.resize(cols, rows, true)
}

func (s *TerminalScreen) resize(cols int, rows int, topAligned bool) {
	cols = max(1, cols)
	rows = max(1, rows)
	if cols == s.cols && rows == s.rows {
		return
	}
	old := s.cells
	oldRows := s.rows
	rowsChanged := rows != s.rows
	s.cols = cols
	s.rows = rows
	s.cells = make([][]terminalCell, rows)
	sourceStart := max(0, oldRows-rows)
	destStart := max(0, rows-oldRows)
	if topAligned {
		sourceStart = 0
		destStart = 0
	}
	for row := range s.cells {
		s.cells[row] = blankCells(cols, cellbuf.Style{})
		sourceRow := sourceStart + row - destStart
		if sourceRow >= 0 && sourceRow < len(old) {
			copy(s.cells[row], old[sourceRow])
		}
	}
	if rowsChanged {
		rowDelta := destStart - sourceStart
		s.row += rowDelta
		s.savedRow += rowDelta
		s.resetScrollRegion()
	}
	s.clampCursor()
}

func (s *TerminalScreen) Clear() {
	s.history = nil
	s.resetScrollRegion()
	s.cursorHome()
	s.clearAll()
}

func (s *TerminalScreen) Write(text string) {
	for _, b := range []byte(text) {
		s.parser.Advance(b)
	}
}

func (s *TerminalScreen) String() string {
	return plainRowsString(s.cells)
}

func (s *TerminalScreen) ScrollbackString() string {
	return plainRowsString(s.scrollbackRows())
}

func (s *TerminalScreen) LastCWD() string {
	return s.cwd
}

func plainRowsString(rows [][]terminalCell) string {
	lines := make([]string, len(rows))
	for row, cells := range rows {
		lines[row] = strings.TrimRight(plainCells(cells), " ")
	}
	return strings.Join(lines, "\n")
}

func (s *TerminalScreen) PlainLines() []string {
	return plainRows(s.cells)
}

func (s *TerminalScreen) ScrollbackPlainLines() []string {
	return plainRows(s.scrollbackRows())
}

func plainRows(rows [][]terminalCell) []string {
	lines := make([]string, len(rows))
	for row, cells := range rows {
		lines[row] = plainCells(cells)
	}
	return lines
}

func (s *TerminalScreen) ANSIString() string {
	return s.ansiString(false)
}

func (s *TerminalScreen) ANSIStringWithCursor(showCursor bool) string {
	return s.ansiString(showCursor && s.cursorVisible)
}

func (s *TerminalScreen) ScrollbackANSIStringWithCursor(showCursor bool) string {
	rows := s.scrollbackRows()
	cursorRow := -1
	if showCursor && s.cursorVisible {
		cursorRow = len(s.history) + s.row
	}
	return ansiRowsString(rows, cursorRow, s.col, s.cursorShape, s.defaults)
}

func (s *TerminalScreen) ansiString(showCursor bool) string {
	cursorRow := -1
	if showCursor {
		cursorRow = s.row
	}
	return ansiRowsString(s.cells, cursorRow, s.col, s.cursorShape, s.defaults)
}

func ansiRowsString(rows [][]terminalCell, cursorRow int, cursorCol int, cursorShape terminalCursorShape, defaults cellbuf.Style) string {
	lines := make([]string, len(rows))
	for row, cells := range rows {
		cursor := terminalCursorOverlay{col: -1}
		if row == cursorRow {
			cursor = terminalCursorOverlay{col: cursorCol, shape: cursorShape}
		}
		lines[row] = styledCells(cells, defaults, cursor)
	}
	return strings.Join(lines, "\n")
}

func (s *TerminalScreen) HasVisibleContent() bool {
	for _, row := range s.cells {
		for _, cell := range row {
			if cell.r != 0 && cell.r != ' ' {
				return true
			}
		}
	}
	return false
}

func (s *TerminalScreen) print(r rune) {
	if r == 0 || r == '\uFFFD' {
		return
	}
	width := runewidth.RuneWidth(r)
	if width <= 0 {
		return
	}
	if s.col >= s.cols {
		s.col = 0
		s.newline()
	}
	if width > s.cols {
		return
	}
	if s.col+width > s.cols {
		s.col = 0
		s.newline()
	}
	s.cells[s.row][s.col] = terminalCell{r: r, style: s.style}
	for offset := 1; offset < width && s.col+offset < s.cols; offset++ {
		s.cells[s.row][s.col+offset] = terminalCell{r: ' ', style: s.style}
	}
	s.col += width
}

func (s *TerminalScreen) execute(b byte) {
	switch b {
	case '\r':
		s.col = 0
	case '\n', 0x0b, 0x0c:
		s.newline()
	case '\b':
		if s.col > 0 {
			s.col--
		}
	case '\t':
		next := ((s.col / 8) + 1) * 8
		s.col = min(next, s.cols-1)
	case 0x85:
		s.col = 0
		s.newline()
	}
}

func (s *TerminalScreen) handleCSI(cmd ansi.Cmd, params ansi.Params) {
	final := cmd.Final()
	prefix := cmd.Prefix()
	param := func(index int, def int) int {
		value, _, ok := params.Param(index, def)
		if !ok {
			return def
		}
		return value
	}

	switch final {
	case 'A':
		minRow, _ := s.verticalBounds()
		s.row = max(minRow, s.row-param(0, 1))
	case 'B':
		_, maxRow := s.verticalBounds()
		s.row = min(maxRow, s.row+param(0, 1))
	case 'C':
		s.col = min(s.cols-1, s.col+param(0, 1))
	case 'D':
		s.col = max(0, s.col-param(0, 1))
	case 'E':
		_, maxRow := s.verticalBounds()
		s.row = min(maxRow, s.row+param(0, 1))
		s.col = 0
	case 'F':
		minRow, _ := s.verticalBounds()
		s.row = max(minRow, s.row-param(0, 1))
		s.col = 0
	case 'G':
		s.col = min(max(0, param(0, 1)-1), s.cols-1)
	case 'H', 'f':
		s.row = s.cursorRow(param(0, 1))
		s.col = min(max(0, param(1, 1)-1), s.cols-1)
	case 'd':
		s.row = s.cursorRow(param(0, 1))
	case 'J':
		s.eraseDisplay(param(0, 0))
	case 'K':
		s.eraseLine(param(0, 0))
	case 'S':
		s.scrollUp(param(0, 1))
	case 'T':
		s.scrollDown(param(0, 1))
	case 'L':
		s.insertLines(param(0, 1))
	case 'M':
		s.deleteLines(param(0, 1))
	case 'P':
		s.deleteChars(param(0, 1))
	case '@':
		s.insertChars(param(0, 1))
	case 'r':
		if prefix == 0 {
			s.setScrollRegion(param(0, 1), param(1, s.rows))
		}
	case 'q':
		if prefix == 0 && cmd.Intermediate() == ' ' {
			s.cursorShape = cursorShapeFromMode(param(0, 0))
		}
	case 's':
		if prefix == 0 {
			s.savedRow, s.savedCol = s.row, s.col
		}
	case 'u':
		if prefix == 0 {
			s.row, s.col = s.savedRow, s.savedCol
		}
	case 'h', 'l':
		if prefix == '?' {
			if paramsContain(params, 47, 1047, 1049) {
				s.history = nil
				s.resetScrollRegion()
				s.clearAll()
			}
			if paramsContain(params, 25) {
				s.cursorVisible = final == 'h'
			}
			if paramsContain(params, 6) {
				s.originMode = final == 'h'
				s.cursorHome()
			}
		}
	case 'm':
		if prefix == 0 {
			cellbuf.ReadStyle(params, &s.style)
		}
	}
	s.clampCursor()
}

func (s *TerminalScreen) handleESC(cmd ansi.Cmd) {
	switch cmd.Final() {
	case 'c':
		s.history = nil
		s.resetScrollRegion()
		s.originMode = false
		s.cursorVisible = true
		s.cursorShape = terminalCursorBlock
		s.clearAll()
	case '7':
		s.savedRow, s.savedCol = s.row, s.col
	case '8':
		s.row, s.col = s.savedRow, s.savedCol
	case 'D', 'E':
		if cmd.Final() == 'E' {
			s.col = 0
		}
		s.newline()
	case 'M':
		s.reverseIndex()
	}
	s.clampCursor()
}

func (s *TerminalScreen) handleOSC(cmd int, data []byte) {
	switch cmd {
	case 10:
		if c, ok := parseOSCColor(oscPayload(cmd, data)); ok {
			s.defaults.Fg = c
		}
	case 11:
		if c, ok := parseOSCColor(oscPayload(cmd, data)); ok {
			s.defaults.Bg = c
		}
	case 7:
		if cwd, ok := parseOSC7CWD(oscPayload(cmd, data)); ok {
			s.cwd = cwd
		}
	case 110:
		s.defaults.Fg = nil
	case 111:
		s.defaults.Bg = nil
	}
}

func (s *TerminalScreen) eraseDisplay(mode int) {
	switch mode {
	case 0:
		s.eraseLineFrom(s.row, s.col)
		for row := s.row + 1; row < s.rows; row++ {
			s.clearRow(row)
		}
	case 1:
		for row := 0; row < s.row; row++ {
			s.clearRow(row)
		}
		for col := 0; col <= s.col && col < s.cols; col++ {
			s.cells[s.row][col] = s.blankCell()
		}
	case 2:
		s.clearAll()
	case 3:
		s.history = nil
		s.clearAll()
	}
}

func (s *TerminalScreen) eraseLine(mode int) {
	switch mode {
	case 0:
		s.eraseLineFrom(s.row, s.col)
	case 1:
		for col := 0; col <= s.col && col < s.cols; col++ {
			s.cells[s.row][col] = s.blankCell()
		}
	case 2:
		s.clearRow(s.row)
	}
}

func (s *TerminalScreen) eraseLineFrom(row int, col int) {
	if row < 0 || row >= s.rows {
		return
	}
	for index := max(0, col); index < s.cols; index++ {
		s.cells[row][index] = s.blankCell()
	}
}

func (s *TerminalScreen) clearAll() {
	s.cells = make([][]terminalCell, s.rows)
	for row := range s.cells {
		s.cells[row] = blankCells(s.cols, s.style)
	}
	s.row, s.col = 0, 0
}

func (s *TerminalScreen) clearRow(row int) {
	if row < 0 || row >= s.rows {
		return
	}
	for col := range s.cells[row] {
		s.cells[row][col] = s.blankCell()
	}
}

func (s *TerminalScreen) newline() {
	if s.row == s.scrollBottom {
		s.scrollUpRegion(s.scrollTop, s.scrollBottom, 1)
		return
	}
	if s.row < s.rows-1 {
		s.row++
		return
	}
	s.scrollUpRegion(0, s.rows-1, 1)
}

func (s *TerminalScreen) reverseIndex() {
	if s.row == s.scrollTop {
		s.scrollDownRegion(s.scrollTop, s.scrollBottom, 1)
		return
	}
	if s.row > 0 {
		s.row--
		return
	}
	s.scrollDownRegion(0, s.rows-1, 1)
}

func (s *TerminalScreen) scrollUp(count int) {
	s.scrollUpRegion(s.scrollTop, s.scrollBottom, count)
}

func (s *TerminalScreen) scrollUpRegion(top int, bottom int, count int) {
	if top < 0 || bottom >= s.rows || top > bottom {
		return
	}
	height := bottom - top + 1
	count = min(max(1, count), height)
	for row := top; row < top+count; row++ {
		s.appendHistoryRow(s.cells[row])
	}
	copy(s.cells[top:bottom+1], s.cells[top+count:bottom+1])
	for row := bottom - count + 1; row <= bottom; row++ {
		s.cells[row] = blankCells(s.cols, s.style)
	}
}

func (s *TerminalScreen) scrollDown(count int) {
	s.scrollDownRegion(s.scrollTop, s.scrollBottom, count)
}

func (s *TerminalScreen) scrollDownRegion(top int, bottom int, count int) {
	if top < 0 || bottom >= s.rows || top > bottom {
		return
	}
	height := bottom - top + 1
	count = min(max(1, count), height)
	for row := bottom; row >= top+count; row-- {
		s.cells[row] = s.cells[row-count]
	}
	for row := top; row < top+count; row++ {
		s.cells[row] = blankCells(s.cols, s.style)
	}
}

func (s *TerminalScreen) appendHistoryRow(row []terminalCell) {
	s.history = append(s.history, cloneTerminalCells(row))
	if overflow := len(s.history) - terminalScrollbackLimit; overflow > 0 {
		copy(s.history, s.history[overflow:])
		s.history = s.history[:len(s.history)-overflow]
	}
}

func (s *TerminalScreen) scrollbackRows() [][]terminalCell {
	rows := make([][]terminalCell, 0, len(s.history)+len(s.cells))
	rows = append(rows, s.history...)
	rows = append(rows, s.cells...)
	return rows
}

func (s *TerminalScreen) insertLines(count int) {
	if s.row < s.scrollTop || s.row > s.scrollBottom {
		return
	}
	count = min(max(1, count), s.scrollBottom-s.row+1)
	for row := s.scrollBottom; row >= s.row+count; row-- {
		s.cells[row] = s.cells[row-count]
	}
	for row := s.row; row < s.row+count; row++ {
		s.cells[row] = blankCells(s.cols, s.style)
	}
}

func (s *TerminalScreen) deleteLines(count int) {
	if s.row < s.scrollTop || s.row > s.scrollBottom {
		return
	}
	count = min(max(1, count), s.scrollBottom-s.row+1)
	for row := s.row; row <= s.scrollBottom-count; row++ {
		s.cells[row] = s.cells[row+count]
	}
	for row := s.scrollBottom - count + 1; row <= s.scrollBottom; row++ {
		s.cells[row] = blankCells(s.cols, s.style)
	}
}

func (s *TerminalScreen) insertChars(count int) {
	count = min(max(1, count), s.cols-s.col)
	row := s.cells[s.row]
	for col := s.cols - 1; col >= s.col+count; col-- {
		row[col] = row[col-count]
	}
	for col := s.col; col < s.col+count; col++ {
		row[col] = s.blankCell()
	}
}

func (s *TerminalScreen) deleteChars(count int) {
	count = min(max(1, count), s.cols-s.col)
	row := s.cells[s.row]
	for col := s.col; col < s.cols-count; col++ {
		row[col] = row[col+count]
	}
	for col := s.cols - count; col < s.cols; col++ {
		row[col] = s.blankCell()
	}
}

func (s *TerminalScreen) clampCursor() {
	s.scrollTop = min(max(0, s.scrollTop), s.rows-1)
	s.scrollBottom = min(max(s.scrollTop, s.scrollBottom), s.rows-1)
	s.row = min(max(0, s.row), s.rows-1)
	s.col = min(max(0, s.col), s.cols-1)
	s.savedRow = min(max(0, s.savedRow), s.rows-1)
	s.savedCol = min(max(0, s.savedCol), s.cols-1)
}

func (s *TerminalScreen) resetScrollRegion() {
	s.scrollTop = 0
	s.scrollBottom = max(0, s.rows-1)
}

func (s *TerminalScreen) setScrollRegion(top int, bottom int) {
	top = min(max(1, top), s.rows) - 1
	bottom = min(max(1, bottom), s.rows) - 1
	if top >= bottom {
		return
	}
	s.scrollTop = top
	s.scrollBottom = bottom
	s.cursorHome()
}

func (s *TerminalScreen) cursorHome() {
	s.row = 0
	if s.originMode {
		s.row = s.scrollTop
	}
	s.col = 0
}

func (s *TerminalScreen) cursorRow(line int) int {
	line = max(1, line) - 1
	if s.originMode {
		return min(max(s.scrollTop, s.scrollTop+line), s.scrollBottom)
	}
	return min(max(0, line), s.rows-1)
}

func (s *TerminalScreen) verticalBounds() (int, int) {
	if s.originMode {
		return s.scrollTop, s.scrollBottom
	}
	return 0, s.rows - 1
}

func (s *TerminalScreen) blankCell() terminalCell {
	return terminalCell{r: ' ', style: s.style}
}

func blankCells(width int, style cellbuf.Style) []terminalCell {
	row := make([]terminalCell, width)
	for index := range row {
		row[index] = terminalCell{r: ' ', style: style}
	}
	return row
}

func cloneTerminalCells(cells []terminalCell) []terminalCell {
	cloned := make([]terminalCell, len(cells))
	copy(cloned, cells)
	return cloned
}

func plainCells(cells []terminalCell) string {
	runes := make([]rune, len(cells))
	for index, cell := range cells {
		if cell.r == 0 {
			runes[index] = ' '
		} else {
			runes[index] = cell.r
		}
	}
	return string(runes)
}

func styledCells(cells []terminalCell, defaults cellbuf.Style, cursor terminalCursorOverlay) string {
	last := -1
	for index, cell := range cells {
		style := styleWithDefaults(cell.style, defaults)
		if index == cursor.col || cell.r != ' ' || !style.Clear() {
			last = index
		}
	}
	if last < 0 {
		return ""
	}

	var builder strings.Builder
	var active cellbuf.Style
	for index, cell := range cells[:last+1] {
		style := styleWithDefaults(cell.style, defaults)
		r := cell.r
		if index == cursor.col {
			style, r = cursorCell(style, cursor.shape, r)
		}
		if !style.Equal(&active) {
			if !active.Empty() {
				builder.WriteString(ansi.ResetStyle)
			}
			if !style.Empty() {
				builder.WriteString(style.Sequence())
			}
			active = style
		}
		if r == 0 {
			builder.WriteRune(' ')
		} else {
			builder.WriteRune(r)
		}
	}
	if !active.Empty() {
		builder.WriteString(ansi.ResetStyle)
	}
	return builder.String()
}

func cursorCell(style cellbuf.Style, shape terminalCursorShape, r rune) (cellbuf.Style, rune) {
	switch shape {
	case terminalCursorUnderline:
		style.Underline(true)
	case terminalCursorBar:
		style.Fg = color.White
		r = '▏'
	default:
		style.Fg = color.Black
		style.Bg = color.White
	}
	return style, r
}

func cursorShapeFromMode(mode int) terminalCursorShape {
	switch mode {
	case 3, 4:
		return terminalCursorUnderline
	case 5, 6:
		return terminalCursorBar
	default:
		return terminalCursorBlock
	}
}

func styleWithDefaults(style cellbuf.Style, defaults cellbuf.Style) cellbuf.Style {
	if style.Fg == nil {
		style.Fg = defaults.Fg
	}
	if style.Bg == nil {
		style.Bg = defaults.Bg
	}
	return style
}

func oscPayload(cmd int, data []byte) string {
	payload := strings.TrimSpace(string(data))
	prefix := strconv.Itoa(cmd) + ";"
	if strings.HasPrefix(payload, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(payload, prefix))
	}
	if before, after, ok := strings.Cut(payload, ";"); ok && digitsOnly(before) {
		return strings.TrimSpace(after)
	}
	return payload
}

func parseOSCColor(value string) (ansi.Color, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "?" {
		return nil, false
	}
	if strings.HasPrefix(value, "#") {
		return parseHexColor(strings.TrimPrefix(value, "#"))
	}
	if strings.HasPrefix(value, "rgb:") {
		return parseXRGBColor(strings.TrimPrefix(value, "rgb:"))
	}
	if strings.HasPrefix(value, "rgba:") {
		return parseXRGBColor(strings.TrimPrefix(value, "rgba:"))
	}
	return nil, false
}

func parseOSC7CWD(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "file" || strings.TrimSpace(parsed.Path) == "" {
		return "", false
	}
	return parsed.Path, true
}

func parseHexColor(value string) (ansi.Color, bool) {
	if len(value) == 3 {
		value = strings.Repeat(value[0:1], 2) + strings.Repeat(value[1:2], 2) + strings.Repeat(value[2:3], 2)
	}
	if len(value) != 6 {
		return nil, false
	}
	r, ok := parseHexByte(value[0:2])
	if !ok {
		return nil, false
	}
	g, ok := parseHexByte(value[2:4])
	if !ok {
		return nil, false
	}
	b, ok := parseHexByte(value[4:6])
	if !ok {
		return nil, false
	}
	return color.RGBA{R: r, G: g, B: b, A: 0xff}, true
}

func parseXRGBColor(value string) (ansi.Color, bool) {
	parts := strings.Split(value, "/")
	if len(parts) < 3 {
		return nil, false
	}
	r, ok := parseXColorComponent(parts[0])
	if !ok {
		return nil, false
	}
	g, ok := parseXColorComponent(parts[1])
	if !ok {
		return nil, false
	}
	b, ok := parseXColorComponent(parts[2])
	if !ok {
		return nil, false
	}
	return color.RGBA{R: r, G: g, B: b, A: 0xff}, true
}

func parseHexByte(value string) (uint8, bool) {
	parsed, err := strconv.ParseUint(value, 16, 8)
	if err != nil {
		return 0, false
	}
	return uint8(parsed), true
}

func parseXColorComponent(value string) (uint8, bool) {
	if len(value) == 0 || len(value) > 4 {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 16, 16)
	if err != nil {
		return 0, false
	}
	maxValue := uint64(1)<<(4*len(value)) - 1
	return uint8((parsed*0xff + maxValue/2) / maxValue), true
}

func digitsOnly(value string) bool {
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

func paramsContain(params ansi.Params, values ...int) bool {
	targets := map[int]bool{}
	for _, value := range values {
		targets[value] = true
	}
	found := false
	params.ForEach(0, func(_ int, param int, _ bool) {
		if targets[param] {
			found = true
		}
	})
	return found
}
