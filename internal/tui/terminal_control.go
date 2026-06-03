package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

const terminalDashboardTitle = "Weft"

var writeTerminalSequence = func(sequence string) error {
	_, err := os.Stdout.WriteString(sequence)
	return err
}

func terminalSequenceCommand(sequence string) tea.Cmd {
	if sequence == "" {
		return nil
	}
	return func() tea.Msg {
		_ = writeTerminalSequence(sequence)
		return nil
	}
}

func terminalTitleCommand() tea.Cmd {
	return terminalSequenceCommand(terminalSetTitleSequence(terminalDashboardTitle))
}

func terminalSetTitleSequence(title string) string {
	title = sanitizeOSCText(title)
	if title == "" {
		return ""
	}
	return "\x1b]0;" + title + "\a" + "\x1b]1;" + title + "\a" + "\x1b]2;" + title + "\a"
}
