package tui

import (
	"strings"
	"testing"
)

func TestTerminalSetTitleSequenceUsesFixedWeftTitle(t *testing.T) {
	if got := terminalSetTitleSequence("Weft"); got != "\x1b]0;Weft\a\x1b]1;Weft\a\x1b]2;Weft\a" {
		t.Fatalf("terminal title sequence = %q", got)
	}
	if got := terminalSetTitleSequence(" bad\n\x1btitle\a "); got != "\x1b]0;bad title\a\x1b]1;bad title\a\x1b]2;bad title\a" {
		t.Fatalf("sanitized terminal title sequence = %q", got)
	}
}

func TestTerminalTitleCommandWritesFixedWeftTitle(t *testing.T) {
	oldWrite := writeTerminalSequence
	var wrote strings.Builder
	writeTerminalSequence = func(sequence string) error {
		wrote.WriteString(sequence)
		return nil
	}
	defer func() { writeTerminalSequence = oldWrite }()

	cmd := terminalTitleCommand()
	if cmd == nil {
		t.Fatal("terminal title command should not be nil")
	}
	_ = cmd()
	if got := wrote.String(); got != "\x1b]0;Weft\a\x1b]1;Weft\a\x1b]2;Weft\a" {
		t.Fatalf("terminal title command wrote %q", got)
	}
}
