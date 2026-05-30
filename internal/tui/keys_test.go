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
