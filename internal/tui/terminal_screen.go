package tui

import (
	"image/color"
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

type TerminalScreen struct {
	cols          int
	rows          int
	cells         [][]terminalCell
	row           int
	col           int
	cursorVisible bool
	savedRow      int
	savedCol      int
	style         cellbuf.Style
	defaults      cellbuf.Style
	parser        *ansi.Parser
}

func NewTerminalScreen(cols int, rows int) *TerminalScreen {
	screen := &TerminalScreen{cols: max(1, cols), rows: max(1, rows), cursorVisible: true}
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
	cols = max(1, cols)
	rows = max(1, rows)
	if cols == s.cols && rows == s.rows {
		return
	}
	old := s.cells
	s.cols = cols
	s.rows = rows
	s.cells = make([][]terminalCell, rows)
	for row := range s.cells {
		s.cells[row] = blankCells(cols, cellbuf.Style{})
		if row < len(old) {
			copy(s.cells[row], old[row])
		}
	}
	s.clampCursor()
}

func (s *TerminalScreen) Write(text string) {
	for _, b := range []byte(text) {
		s.parser.Advance(b)
	}
}

func (s *TerminalScreen) String() string {
	lines := make([]string, len(s.cells))
	for row, cells := range s.cells {
		lines[row] = strings.TrimRight(plainCells(cells), " ")
	}
	return strings.Join(lines, "\n")
}

func (s *TerminalScreen) ANSIString() string {
	return s.ansiString(false)
}

func (s *TerminalScreen) ANSIStringWithCursor(showCursor bool) string {
	return s.ansiString(showCursor && s.cursorVisible)
}

func (s *TerminalScreen) ansiString(showCursor bool) string {
	lines := make([]string, len(s.cells))
	for row, cells := range s.cells {
		cursorCol := -1
		if showCursor && row == s.row {
			cursorCol = s.col
		}
		lines[row] = styledCells(cells, s.defaults, cursorCol)
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
		s.row = max(0, s.row-param(0, 1))
	case 'B':
		s.row = min(s.rows-1, s.row+param(0, 1))
	case 'C':
		s.col = min(s.cols-1, s.col+param(0, 1))
	case 'D':
		s.col = max(0, s.col-param(0, 1))
	case 'E':
		s.row = min(s.rows-1, s.row+param(0, 1))
		s.col = 0
	case 'F':
		s.row = max(0, s.row-param(0, 1))
		s.col = 0
	case 'G':
		s.col = min(max(0, param(0, 1)-1), s.cols-1)
	case 'H', 'f':
		s.row = min(max(0, param(0, 1)-1), s.rows-1)
		s.col = min(max(0, param(1, 1)-1), s.cols-1)
	case 'd':
		s.row = min(max(0, param(0, 1)-1), s.rows-1)
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
				s.clearAll()
			}
			if paramsContain(params, 25) {
				s.cursorVisible = final == 'h'
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
	case 2, 3:
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
	s.row++
	if s.row >= s.rows {
		s.scrollUp(1)
		s.row = s.rows - 1
	}
}

func (s *TerminalScreen) reverseIndex() {
	if s.row == 0 {
		s.scrollDown(1)
		return
	}
	s.row--
}

func (s *TerminalScreen) scrollUp(count int) {
	count = min(max(1, count), s.rows)
	copy(s.cells, s.cells[count:])
	for row := s.rows - count; row < s.rows; row++ {
		s.cells[row] = blankCells(s.cols, s.style)
	}
}

func (s *TerminalScreen) scrollDown(count int) {
	count = min(max(1, count), s.rows)
	for row := s.rows - 1; row >= count; row-- {
		s.cells[row] = s.cells[row-count]
	}
	for row := 0; row < count; row++ {
		s.cells[row] = blankCells(s.cols, s.style)
	}
}

func (s *TerminalScreen) insertLines(count int) {
	count = min(max(1, count), s.rows-s.row)
	for row := s.rows - 1; row >= s.row+count; row-- {
		s.cells[row] = s.cells[row-count]
	}
	for row := s.row; row < s.row+count; row++ {
		s.cells[row] = blankCells(s.cols, s.style)
	}
}

func (s *TerminalScreen) deleteLines(count int) {
	count = min(max(1, count), s.rows-s.row)
	for row := s.row; row < s.rows-count; row++ {
		s.cells[row] = s.cells[row+count]
	}
	for row := s.rows - count; row < s.rows; row++ {
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
	s.row = min(max(0, s.row), s.rows-1)
	s.col = min(max(0, s.col), s.cols-1)
	s.savedRow = min(max(0, s.savedRow), s.rows-1)
	s.savedCol = min(max(0, s.savedCol), s.cols-1)
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

func styledCells(cells []terminalCell, defaults cellbuf.Style, cursorCol int) string {
	last := -1
	for index, cell := range cells {
		style := styleWithDefaults(cell.style, defaults)
		if index == cursorCol || cell.r != ' ' || !style.Clear() {
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
		if index == cursorCol {
			style = cursorCellStyle(style)
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
		if cell.r == 0 {
			builder.WriteRune(' ')
		} else {
			builder.WriteRune(cell.r)
		}
	}
	if !active.Empty() {
		builder.WriteString(ansi.ResetStyle)
	}
	return builder.String()
}

func cursorCellStyle(style cellbuf.Style) cellbuf.Style {
	style.Fg = color.Black
	style.Bg = color.White
	return style
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
