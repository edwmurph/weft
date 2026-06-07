package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTerminalScreenAppliesCursorAddressingWithoutLeakingEscapes(t *testing.T) {
	screen := NewTerminalScreen(20, 5)

	screen.Write("\x1b[2J\x1b[Htop\x1b[3;6Hmiddle\x1b]10;rgb:aaaa/bbbb/cccc\x1b\\tail")
	rendered := screen.String()

	if strings.Contains(rendered, "\x1b") || strings.Contains(rendered, "]10;") {
		t.Fatalf("screen leaked escape sequence:\n%q", rendered)
	}
	if !strings.Contains(rendered, "top") || !strings.Contains(rendered, "     middletail") {
		t.Fatalf("screen did not apply cursor addressing:\n%q", rendered)
	}
}

func TestTerminalScreenTracksOSC7CWD(t *testing.T) {
	screen := NewTerminalScreen(40, 5)

	screen.Write("prompt\x1b]7;file://localhost/tmp/weft%20workspace\x07")

	if got, want := screen.LastCWD(), "/tmp/weft workspace"; got != want {
		t.Fatalf("LastCWD = %q, want %q", got, want)
	}
	if strings.Contains(screen.String(), "]7;") {
		t.Fatalf("screen leaked OSC 7 sequence:\n%q", screen.String())
	}
}

func TestTerminalScreenIgnoresKeyboardModeSequences(t *testing.T) {
	screen := NewTerminalScreen(20, 5)

	screen.Write("x\x1b[>1uy\x1b[>4;2mz")

	if got := strings.TrimSpace(screen.String()); got != "xyz" {
		t.Fatalf("keyboard mode sequences should not move the cursor or style output, got %q", got)
	}
}

func TestTerminalScreenAlternateScreenClearsPreviousContent(t *testing.T) {
	screen := NewTerminalScreen(20, 5)

	screen.Write("old content\n\x1b[?1049h\x1b[Hnew content")
	rendered := screen.String()

	if strings.Contains(rendered, "old content") {
		t.Fatalf("alternate screen did not clear previous content:\n%q", rendered)
	}
	if !strings.Contains(rendered, "new content") {
		t.Fatalf("screen missing new content:\n%q", rendered)
	}

	screen.Write("\x1b[?1049l")
	restored := screen.String()
	if !strings.Contains(restored, "old content") {
		t.Fatalf("normal screen was not restored after alternate screen exit:\n%q", restored)
	}
	if strings.Contains(restored, "new content") {
		t.Fatalf("alternate screen content leaked after exit:\n%q", restored)
	}
}

func TestTerminalScreenAlternateScreenRestoresScrollback(t *testing.T) {
	screen := NewTerminalScreen(12, 3)
	for row := 1; row <= 5; row++ {
		screen.Write(fmt.Sprintf("chat%02d\r\n", row))
	}

	screen.Write("\x1b[?1049h\x1b[Hdiff page\r\n")
	for row := 1; row <= 5; row++ {
		screen.Write(fmt.Sprintf("diff%02d\r\n", row))
	}
	if !screen.InAlternateScreen() {
		t.Fatal("screen should stay in alternate screen while full-screen content is active")
	}
	screen.Write("\x1b[?1049l")
	scrollback := strings.Join(screen.ScrollbackPlainLines(), "\n")

	if !strings.Contains(scrollback, "chat01") || !strings.Contains(scrollback, "chat05") {
		t.Fatalf("normal scrollback was not restored:\n%q", scrollback)
	}
	if strings.Contains(scrollback, "diff01") || strings.Contains(scrollback, "diff page") {
		t.Fatalf("alternate screen content leaked into normal scrollback:\n%q", scrollback)
	}
}

func TestTerminalScreenRepeatedAlternateScreenEntryDoesNotNestBuffers(t *testing.T) {
	screen := NewTerminalScreen(20, 5)

	screen.Write("shell prompt\x1b[?1049h\x1b[Hpage one")
	screen.Write("\x1b[?1049h\x1b[Hpage two")
	if rendered := screen.String(); strings.Contains(rendered, "page one") || !strings.Contains(rendered, "page two") {
		t.Fatalf("re-entering alternate screen should redraw the active alternate buffer:\n%q", rendered)
	}

	screen.Write("\x1b[?1049l")
	restored := screen.String()
	if !strings.Contains(restored, "shell prompt") {
		t.Fatalf("alternate screen exit should restore the original normal buffer:\n%q", restored)
	}
	if strings.Contains(restored, "page one") || strings.Contains(restored, "page two") {
		t.Fatalf("alternate screen page leaked after exit:\n%q", restored)
	}
}

func TestTerminalScreenAlternateScreenResizeKeepsRestoredBufferAligned(t *testing.T) {
	screen := NewTerminalScreen(12, 5)
	for row := 1; row <= 5; row++ {
		screen.Write(fmt.Sprintf("\x1b[%d;1Hchat%d", row, row))
	}

	screen.Write("\x1b[?1049h\x1b[Hdiff")
	screen.Resize(12, 3)
	screen.Write("\x1b[?1049l")
	rendered := screen.String()

	for _, expected := range []string{"chat3", "chat4", "chat5"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("restored resized buffer missing %q:\n%q", expected, rendered)
		}
	}
	for _, forbidden := range []string{"chat1", "chat2", "diff"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("restored resized buffer should not contain %q:\n%q", forbidden, rendered)
		}
	}
}

func TestTerminalScreenTracksAlternateScreenMode(t *testing.T) {
	screen := NewTerminalScreen(20, 5)

	screen.Write("\x1b[?1049h")
	if !screen.InAlternateScreen() {
		t.Fatal("screen should enter alternate screen on ?1049h")
	}

	screen.Write("\x1b[?1049l")
	if screen.InAlternateScreen() {
		t.Fatal("screen should exit alternate screen on ?1049l")
	}

	screen.Write("\x1b[?47h")
	if !screen.InAlternateScreen() {
		t.Fatal("screen should enter alternate screen on ?47h")
	}

	screen.Write("\x1bc")
	if screen.InAlternateScreen() {
		t.Fatal("full terminal reset should leave alternate screen mode")
	}
}

func TestTerminalScreenWrapsLongPrintableLines(t *testing.T) {
	screen := NewTerminalScreen(5, 3)

	screen.Write("abcdefghijklmnopqrstuvwxyz")
	rendered := screen.String()

	if strings.Contains(rendered, "\x1b") {
		t.Fatalf("screen leaked escape sequence:\n%q", rendered)
	}
	if !strings.Contains(rendered, "uvwxy\nz") {
		t.Fatalf("screen did not keep final wrapped line:\n%q", rendered)
	}
}

func TestTerminalScreenKeepsScrolledRowsInScrollback(t *testing.T) {
	screen := NewTerminalScreen(12, 3)

	for row := 1; row <= 5; row++ {
		screen.Write(fmt.Sprintf("line%02d\r\n", row))
	}
	scrollback := strings.Join(screen.ScrollbackPlainLines(), "\n")

	if !strings.Contains(scrollback, "line01") || !strings.Contains(scrollback, "line05") {
		t.Fatalf("scrollback should include old and current rows:\n%q", scrollback)
	}
	if strings.Contains(screen.String(), "line01") {
		t.Fatalf("visible screen should still show only the bottom rows:\n%q", screen.String())
	}
}

func TestTerminalScreenResizePreservesBottomRows(t *testing.T) {
	screen := NewTerminalScreen(12, 6)
	for row := 1; row <= 6; row++ {
		screen.Write(fmt.Sprintf("\x1b[%d;1Hline%d", row, row))
	}

	screen.Resize(12, 3)
	rendered := screen.String()

	for _, expected := range []string{"line4", "line5", "line6"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("resize should preserve bottom row %q:\n%q", expected, rendered)
		}
	}
	for _, forbidden := range []string{"line1", "line2", "line3"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("resize should drop top row %q before bottom rows:\n%q", forbidden, rendered)
		}
	}
}

func TestTerminalScreenResizeTopAlignedKeepsPromptAtTop(t *testing.T) {
	screen := NewTerminalScreen(20, 5)
	screen.Write("shell prompt")

	screen.ResizeTopAligned(20, 10)

	rendered := screen.String()
	if !strings.HasPrefix(rendered, "shell prompt") {
		t.Fatalf("top-aligned resize should keep prompt on first row:\n%q", rendered)
	}
}

func TestTerminalScreenClearRemovesScrollbackAndVisibleContent(t *testing.T) {
	screen := NewTerminalScreen(12, 2)
	screen.Write("old\r\ncontent")
	scrollback := strings.Join(screen.ScrollbackPlainLines(), "\n")
	if !strings.Contains(scrollback, "old") {
		t.Fatalf("test setup missing scrollback:\n%q", scrollback)
	}

	screen.Clear()

	scrollback = strings.Join(screen.ScrollbackPlainLines(), "\n")
	if strings.TrimSpace(screen.String()) != "" || strings.TrimSpace(scrollback) != "" {
		t.Fatalf("clear should remove visible content and scrollback:\nvisible=%q\nscrollback=%q", screen.String(), scrollback)
	}
}

func TestTerminalScreenScrollRegionKeepsFooterPinned(t *testing.T) {
	screen := NewTerminalScreen(20, 6)

	screen.Write("\x1b[6;1Hfooter")
	screen.Write("\x1b[2;5r\x1b[5;1Hline1\r\nline2\r\nline3")
	lines := strings.Split(screen.String(), "\n")

	if !strings.Contains(lines[5], "footer") {
		t.Fatalf("scrolling inside region should not consume footer row:\n%q", screen.String())
	}
	if !strings.Contains(lines[4], "line3") {
		t.Fatalf("scrolling region should keep the newest region content at the bottom:\n%q", screen.String())
	}
}

func TestTerminalScreenWidthResizePreservesScrollRegion(t *testing.T) {
	screen := NewTerminalScreen(20, 6)

	screen.Write("\x1b[6;1Hfooter")
	screen.Write("\x1b[2;5r")
	screen.Resize(24, 6)
	screen.Write("\x1b[5;1Hline1\r\nline2")
	lines := strings.Split(screen.String(), "\n")

	if !strings.Contains(lines[5], "footer") {
		t.Fatalf("width resize should keep scroll region from consuming footer row:\n%q", screen.String())
	}
}

func TestTerminalScreenPreservesBoxDrawingWithCarriageReturns(t *testing.T) {
	screen := NewTerminalScreen(64, 8)

	screen.Write("╭──────────────╮\r\n")
	screen.Write("│ >_ Codex     │\r\n")
	screen.Write("│ model: xhigh │\r\n")
	screen.Write("╰──────────────╯\r\n")
	rendered := screen.String()

	for _, expected := range []string{
		"╭──────────────╮",
		"│ >_ Codex     │",
		"│ model: xhigh │",
		"╰──────────────╯",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("screen drifted box line %q:\n%q", expected, rendered)
		}
	}
}

func TestTerminalScreenANSIStringPreservesBackgroundOnSpaces(t *testing.T) {
	screen := NewTerminalScreen(12, 2)

	screen.Write("\x1b[48;2;1;2;3m  \x1b[0mX")
	plain := screen.String()
	styled := screen.ANSIString()

	if strings.Contains(plain, "\x1b") {
		t.Fatalf("plain screen should not contain ANSI escapes:\n%q", plain)
	}
	if !strings.Contains(plain, "  X") {
		t.Fatalf("plain screen missing text:\n%q", plain)
	}
	if !strings.Contains(styled, "\x1b[48;2;1;2;3m  \x1b[mX") {
		t.Fatalf("styled screen should preserve truecolor background spaces:\n%q", styled)
	}
	if stripped := ansi.Strip(styled); stripped != plain {
		t.Fatalf("styled screen should strip to plain output:\nplain  %q\nstyled %q", plain, stripped)
	}
}

func TestTerminalScreenEraseUsesCurrentBackground(t *testing.T) {
	screen := NewTerminalScreen(4, 1)

	screen.Write("\x1b[48;5;240m\x1b[2K")
	styled := screen.ANSIString()

	if !strings.Contains(styled, "\x1b[48;5;240m    \x1b[m") {
		t.Fatalf("erase should preserve current background across blank cells:\n%q", styled)
	}
	if stripped := ansi.Strip(styled); stripped != strings.Repeat(" ", 4) {
		t.Fatalf("styled erase should strip to four spaces, got %q", stripped)
	}
}

func TestTerminalScreenOSCDefaultColorsFillBlankPane(t *testing.T) {
	screen := NewTerminalScreen(6, 2)

	screen.Write("\x1b]10;rgb:eded/efef/f1f1\x1b\\")
	screen.Write("\x1b]11;rgb:2828/3131/3838\x1b\\")
	styled := screen.ANSIString()

	if !strings.Contains(styled, "\x1b[38;2;237;239;241;48;2;40;49;56m") {
		t.Fatalf("styled screen should apply OSC 10/11 as default colors:\n%q", styled)
	}
	if stripped := ansi.Strip(styled); stripped != strings.Repeat(" ", 6)+"\n"+strings.Repeat(" ", 6) {
		t.Fatalf("default background should fill blank pane, got %q", stripped)
	}
	if screen.HasVisibleContent() {
		t.Fatalf("color-only terminal output should not count as visible content:\n%q", styled)
	}
}

func TestTerminalScreenDetectsVisibleContent(t *testing.T) {
	screen := NewTerminalScreen(6, 2)

	if screen.HasVisibleContent() {
		t.Fatal("empty screen should not have visible content")
	}
	screen.Write("\x1b[48;2;1;2;3m  \x1b[0m")
	if screen.HasVisibleContent() {
		t.Fatal("styled spaces should not count as visible content")
	}
	screen.Write("x")
	if !screen.HasVisibleContent() {
		t.Fatal("printed glyph should count as visible content")
	}
}

func TestTerminalScreenOSCDefaultBackgroundSurvivesResetAndClear(t *testing.T) {
	screen := NewTerminalScreen(5, 1)

	screen.Write("\x1b]11;#282838\x07")
	screen.Write("\x1b[48;2;1;2;3mX\x1b[0m\x1b[2K")
	styled := screen.ANSIString()

	if !strings.Contains(styled, "\x1b[48;2;40;40;56m") {
		t.Fatalf("SGR reset should return to OSC default background:\n%q", styled)
	}
	if stripped := ansi.Strip(styled); stripped != strings.Repeat(" ", 5) {
		t.Fatalf("default background clear should strip to spaces, got %q", stripped)
	}
}

func TestTerminalScreenANSIStringWithCursorPaintsWhiteCursor(t *testing.T) {
	screen := NewTerminalScreen(6, 2)

	screen.Write("abc")
	styled := screen.ANSIStringWithCursor(true)
	firstLine := strings.Split(ansi.Strip(styled), "\n")[0]

	if !strings.Contains(styled, "38;2;0;0;0") || !strings.Contains(styled, "48;2;255;255;255") {
		t.Fatalf("styled screen should paint a white cursor cell:\n%q", styled)
	}
	if firstLine != "abc " {
		t.Fatalf("cursor should occupy the current terminal cell, got %q", firstLine)
	}
}

func TestTerminalScreenCodexInputGuidePaintsPromptRowsWithPadding(t *testing.T) {
	screen := NewTerminalScreen(20, 5)

	screen.Write("intro\r\n\r\n› prior input\r\n\r\nstatus")
	styled := screen.CodexANSIStringWithCursorGuide(false)
	lines := strings.Split(styled, "\n")

	for _, row := range []int{1, 2, 3} {
		if !strings.Contains(lines[row], "48;2;60;66;71") {
			t.Fatalf("Codex input guide should paint prompt row and one-row padding, row %d:\n%q", row, lines[row])
		}
	}
	if got := strings.TrimSpace(ansi.Strip(lines[2])); got != "› prior input" {
		t.Fatalf("Codex input guide should preserve prompt text, got %q", got)
	}
	if strings.Contains(lines[0], "48;2;60;66;71") || strings.Contains(lines[4], "48;2;60;66;71") {
		t.Fatalf("Codex input guide should not paint unrelated rows:\n%q", styled)
	}
}

func TestTerminalScreenANSIStringWithCursorRespectsVisibilityMode(t *testing.T) {
	screen := NewTerminalScreen(6, 1)

	screen.Write("abc\x1b[?25l")
	hidden := screen.ANSIStringWithCursor(true)
	if strings.Contains(hidden, "48;2;255;255;255") {
		t.Fatalf("hidden terminal cursor should not be painted:\n%q", hidden)
	}

	screen.Write("\x1b[?25h")
	shown := screen.ANSIStringWithCursor(true)
	if !strings.Contains(shown, "48;2;255;255;255") {
		t.Fatalf("shown terminal cursor should be painted:\n%q", shown)
	}
}

func TestTerminalScreenANSIStringWithCursorPreservesCursorShape(t *testing.T) {
	screen := NewTerminalScreen(8, 1)

	screen.Write("abc\x1b[4 q")
	underline := screen.ANSIStringWithCursor(true)
	if !strings.Contains(underline, "\x1b[4") || strings.Contains(underline, "48;2;255;255;255") {
		t.Fatalf("underline cursor should render as an underlined cell, got %q", underline)
	}

	screen.Write("\x1b[6 q")
	bar := screen.ANSIStringWithCursor(true)
	if !strings.Contains(ansi.Strip(bar), "abc▏") || strings.Contains(bar, "48;2;255;255;255") {
		t.Fatalf("bar cursor should render as a vertical bar glyph, got %q", bar)
	}

	screen.Write("\x1b[2 q")
	block := screen.ANSIStringWithCursor(true)
	if !strings.Contains(block, "48;2;255;255;255") {
		t.Fatalf("block cursor should render as an inverted cell, got %q", block)
	}
}
