package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type testCSIMessage string

func (m testCSIMessage) String() string {
	return string(m)
}

func TestBindingMatchesWeftConfigSpelling(t *testing.T) {
	if !bindingMatches("C-c", tea.KeyMsg{Type: tea.KeyCtrlC}) {
		t.Fatal("C-c should match ctrl+c")
	}
	if !bindingMatches("S-Left", tea.KeyMsg{Type: tea.KeyShiftLeft}) {
		t.Fatal("S-Left should match shift+left")
	}
}

func TestEncodeKeyForwardsPrintableAndArrows(t *testing.T) {
	if got := string(encodeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})); got != "x" {
		t.Fatalf("printable = %q", got)
	}
	if got := string(encodeKey(tea.KeyMsg{Type: tea.KeyUp})); got != "\x1b[A" {
		t.Fatalf("up = %q", got)
	}
	if got := string(encodeKey(tea.KeyMsg{Type: tea.KeyShiftTab})); got != "\x1b[Z" {
		t.Fatalf("shift tab = %q", got)
	}
}

func TestEnhancedKeyboardInputRecognizesShiftEnter(t *testing.T) {
	for _, raw := range []string{"\x1b[13;2u", "\x1b[13;2:1u", "\x1b[27;2;13~"} {
		input, ok := enhancedKeyboardInputFromMsg(testCSIMessage(unknownCSIString(raw)))
		if !ok {
			t.Fatalf("expected enhanced input for %q", raw)
		}
		if input.hasKey {
			t.Fatalf("shift enter should be forwarded raw, got key %s", input.key.String())
		}
		if input.input != codexInputShiftEnter {
			t.Fatalf("input kind = %q", input.input)
		}
		if string(input.encoded) != raw {
			t.Fatalf("encoded = %q, want %q", input.encoded, raw)
		}
	}
}

func TestEnhancedKeyboardInputPreservesShiftTabForCodex(t *testing.T) {
	for _, raw := range []string{"\x1b[9;2u", "\x1b[9;2:1u", "\x1b[27;2;9~"} {
		input, ok := enhancedKeyboardInputFromMsg(testCSIMessage(unknownCSIString(raw)))
		if !ok {
			t.Fatalf("expected enhanced input for %q", raw)
		}
		if !input.hasKey || input.key.Type != tea.KeyShiftTab {
			t.Fatalf("shift tab should still be available as a key outside Codex focus, got %#v", input.key)
		}
		if !input.preserveForCodex {
			t.Fatalf("shift tab should preserve raw bytes in Codex focus")
		}
		args := input.codexInputArgs()
		if got := args["encoded"]; got != raw {
			t.Fatalf("encoded = %q, want %q", got, raw)
		}
		if got := args["input"]; got != codexInputShiftTab {
			t.Fatalf("input kind = %q", got)
		}
	}
}

func TestEnhancedKeyboardInputMapsCSIUCtrlC(t *testing.T) {
	for _, raw := range []string{"\x1b[99;5u", "\x1b[27;5;99~"} {
		input, ok := enhancedKeyboardInputFromMsg(testCSIMessage(unknownCSIString(raw)))
		if !ok {
			t.Fatalf("expected enhanced input for %q", raw)
		}
		if !input.hasKey || input.key.Type != tea.KeyCtrlC {
			t.Fatalf("expected ctrl+c key for %q, got %#v", raw, input.key)
		}
	}
}

func TestEnhancedKeyboardInputIgnoresReleaseEvents(t *testing.T) {
	if input, ok := enhancedKeyboardInputFromMsg(testCSIMessage(unknownCSIString("\x1b[98;5:1u"))); !ok || !input.hasKey || input.key.Type != tea.KeyCtrlB {
		t.Fatalf("expected ctrl+b press, got input=%#v ok=%t", input, ok)
	}
	if input, ok := enhancedKeyboardInputFromMsg(testCSIMessage(unknownCSIString("\x1b[98;5:3u"))); ok {
		t.Fatalf("release event should be ignored, got %#v", input)
	}
}

func TestEncodeKeyPreservesAltModifierForCodexPTY(t *testing.T) {
	if got, want := string(encodeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true})), "\x1bb"; got != want {
		t.Fatalf("alt-b = %q, want %q", got, want)
	}
	if got, want := string(encodeKey(tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})), "\x1b\x7f"; got != want {
		t.Fatalf("alt-backspace = %q, want %q", got, want)
	}
	if got, want := string(encodeKey(tea.KeyMsg{Type: tea.KeyCtrlH, Alt: true})), "\x1b\b"; got != want {
		t.Fatalf("alt-ctrl-h = %q, want %q", got, want)
	}
	if got, want := string(encodeKey(tea.KeyMsg{Type: tea.KeyDelete, Alt: true})), "\x1b\x1b[3~"; got != want {
		t.Fatalf("alt-delete = %q, want %q", got, want)
	}
}

func TestCodexInputArgsPreservesAltBackspace(t *testing.T) {
	args := codexInputArgs(tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	if got, want := args["encoded"], "\x1b\x7f"; got != want {
		t.Fatalf("encoded = %q, want %q", got, want)
	}
	if got, want := args["input"], "alt+backspace"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}

	args = codexInputArgs(tea.KeyMsg{Type: tea.KeyCtrlH, Alt: true})
	if got, want := args["encoded"], "\x1b\b"; got != want {
		t.Fatalf("alt ctrl-h encoded = %q, want %q", got, want)
	}
	if got, want := args["input"], "alt+backspace"; got != want {
		t.Fatalf("alt ctrl-h input = %q, want %q", got, want)
	}
}

func TestCodexInputArgsPreservesShiftTab(t *testing.T) {
	args := codexInputArgs(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got, want := args["encoded"], "\x1b[Z"; got != want {
		t.Fatalf("encoded = %q, want %q", got, want)
	}
	if got, want := args["input"], codexInputShiftTab; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
}

func unknownCSIString(raw string) string {
	var builder strings.Builder
	builder.WriteString("?CSI[")
	for index, b := range []byte(raw)[2:] {
		if index > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(strconv.Itoa(int(b)))
	}
	builder.WriteString("]?")
	return builder.String()
}
