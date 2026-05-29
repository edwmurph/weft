package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func bindingMatches(binding string, msg tea.KeyMsg) bool {
	return normalizeBinding(binding) == strings.ToLower(msg.String())
}

func normalizeBinding(binding string) string {
	value := strings.TrimSpace(binding)
	value = strings.ReplaceAll(value, "C-", "ctrl+")
	value = strings.ReplaceAll(value, "c-", "ctrl+")
	value = strings.ReplaceAll(value, "S-", "shift+")
	value = strings.ReplaceAll(value, "s-", "shift+")
	return strings.ToLower(value)
}

func encodeKey(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyRunes:
		return []byte(string(msg.Runes))
	case tea.KeyEnter:
		return []byte("\r")
	case tea.KeySpace:
		return []byte(" ")
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyTab:
		return []byte("\t")
	case tea.KeyEsc:
		return []byte{0x1b}
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	}
	key := strings.ToLower(msg.String())
	if strings.HasPrefix(key, "ctrl+") && len(key) == len("ctrl+x") {
		ch := key[len(key)-1]
		if ch >= 'a' && ch <= 'z' {
			return []byte{ch - 'a' + 1}
		}
	}
	return nil
}
