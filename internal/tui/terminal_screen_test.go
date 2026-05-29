package tui

import (
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
