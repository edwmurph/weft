package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

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
