package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func bindingMatches(binding string, msg tea.KeyMsg) bool {
	normalized := normalizeBinding(binding)
	if normalized == "backspace" && (msg.Type == tea.KeyBackspace || msg.Type == tea.KeyCtrlH) {
		return true
	}
	return normalized == strings.ToLower(msg.String())
}

func isCtrlCKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyCtrlC || strings.ToLower(msg.String()) == "ctrl+c"
}

func normalizeBinding(binding string) string {
	value := strings.TrimSpace(binding)
	value = strings.ReplaceAll(value, "C-", "ctrl+")
	value = strings.ReplaceAll(value, "c-", "ctrl+")
	value = strings.ReplaceAll(value, "S-", "shift+")
	value = strings.ReplaceAll(value, "s-", "shift+")
	return strings.ToLower(value)
}

func bindingRawSequence(binding string) []byte {
	switch value := normalizeBinding(binding); value {
	case "enter":
		return []byte("\r")
	case "tab":
		return []byte("\t")
	case "esc", "escape":
		return []byte{0x1b}
	case "space", " ":
		return []byte(" ")
	case "backspace":
		return []byte{0x7f}
	case "ctrl+@", "ctrl+`":
		return []byte{0}
	case "ctrl+[":
		return []byte{0x1b}
	case "ctrl+\\":
		return []byte{0x1c}
	case "ctrl+]":
		return []byte{0x1d}
	case "ctrl+^":
		return []byte{0x1e}
	case "ctrl+_":
		return []byte{0x1f}
	case "ctrl+?":
		return []byte{0x7f}
	default:
		if strings.HasPrefix(value, "ctrl+") && len(value) == len("ctrl+x") {
			ch := value[len(value)-1]
			if ch >= 'a' && ch <= 'z' {
				return []byte{ch - 'a' + 1}
			}
		}
	}
	return nil
}

func bindingTerminalSequences(binding string) [][]byte {
	raw := bindingRawSequence(binding)
	var sequences [][]byte
	if len(raw) > 0 {
		sequences = append(sequences, raw)
	}
	value := normalizeBinding(binding)
	if strings.HasPrefix(value, "ctrl+") && len(value) == len("ctrl+x") {
		ch := value[len(value)-1]
		if ch >= 'a' && ch <= 'z' {
			code := int(ch)
			sequences = append(sequences,
				[]byte("\x1b["+strconv.Itoa(code)+";5u"),
				[]byte("\x1b["+strconv.Itoa(code)+";5:1u"),
				[]byte("\x1b[27;5;"+strconv.Itoa(code)+"~"),
			)
		}
	}
	if value == "ctrl+]" {
		sequences = append(sequences,
			[]byte("\x1b[93;5u"),
			[]byte("\x1b[93;5:1u"),
			[]byte("\x1b[27;5;93~"),
		)
	}
	return sequences
}

func terminalInterruptSequences() [][]byte {
	return [][]byte{
		[]byte("\x03"),
		[]byte(terminalKeyboardCtrlC),
		[]byte("\x1b[99;5:1u"),
		[]byte("\x1b[27;5;99~"),
	}
}

func encodeKey(msg tea.KeyMsg) []byte {
	encoded := encodeKeyWithoutAlt(msg)
	if msg.Alt && len(encoded) > 0 {
		return append([]byte{0x1b}, encoded...)
	}
	return encoded
}

func encodeKeyWithoutAlt(msg tea.KeyMsg) []byte {
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
	case tea.KeyShiftTab:
		return []byte("\x1b[Z")
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
	msg.Alt = false
	key := strings.ToLower(msg.String())
	if strings.HasPrefix(key, "ctrl+") && len(key) == len("ctrl+x") {
		ch := key[len(key)-1]
		if ch >= 'a' && ch <= 'z' {
			return []byte{ch - 'a' + 1}
		}
	}
	return nil
}
