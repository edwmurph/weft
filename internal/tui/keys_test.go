package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBindingMatchesCoduxConfigSpelling(t *testing.T) {
	if !bindingMatches("C-q", tea.KeyMsg{Type: tea.KeyCtrlQ}) {
		t.Fatal("C-q should match ctrl+q")
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
