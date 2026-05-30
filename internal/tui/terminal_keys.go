package tui

import (
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/edwmurph/weft/internal/state"
)

const (
	terminalKeyboardEnable     = "\x1b[>4;2m\x1b[>29u"
	terminalKeyboardDisable    = "\x1b[>4;0m\x1b[<1u"
	terminalKeyboardShiftEnter = "\x1b[13;2u"

	codexInputRaw        = "raw"
	codexInputShiftEnter = "shift+enter"
)

type enhancedKeyboardInput struct {
	encoded []byte
	input   string
	key     tea.KeyMsg
	hasKey  bool
}

type csiKeyboardEvent struct {
	keyCode   int
	modifiers int
	eventType int
	final     byte
	text      []rune
}

func enableTerminalKeyboardReporting() {
	if terminalKeyboardReportingDisabled() {
		return
	}
	_, _ = os.Stdout.WriteString(terminalKeyboardEnable)
}

func disableTerminalKeyboardReporting() {
	if terminalKeyboardReportingDisabled() {
		return
	}
	_, _ = os.Stdout.WriteString(terminalKeyboardDisable)
}

func terminalKeyboardReportingDisabled() bool {
	if os.Getenv("WEFT_HEADLESS") == "1" {
		return true
	}
	info, err := os.Stdout.Stat()
	return err != nil || info.Mode()&os.ModeCharDevice == 0
}

func enhancedKeyboardInputFromMsg(msg tea.Msg) (enhancedKeyboardInput, bool) {
	raw, ok := unknownCSISequenceBytes(msg)
	if !ok {
		return enhancedKeyboardInput{}, false
	}
	event, ok := parseCSIKeyboardEvent(raw)
	if !ok {
		return enhancedKeyboardInput{}, false
	}
	if event.isRelease() {
		return enhancedKeyboardInput{}, false
	}
	if event.isShiftEnter() {
		return enhancedKeyboardInput{encoded: raw, input: codexInputShiftEnter}, true
	}
	if key, ok := event.keyMsg(); ok {
		return enhancedKeyboardInput{key: key, hasKey: true}, true
	}
	return enhancedKeyboardInput{encoded: raw, input: codexInputRaw}, true
}

func (input enhancedKeyboardInput) codexInputArgs() map[string]string {
	kind := input.input
	if kind == "" {
		kind = codexInputRaw
	}
	return map[string]string{"encoded": string(input.encoded), "input": kind}
}

func (m Model) handleEnhancedKeyboardInput(input enhancedKeyboardInput) (tea.Model, tea.Cmd) {
	if input.hasKey {
		return m.handleKey(input.key)
	}
	if m.state.Focus != state.FocusCodex {
		return m, nil
	}
	active := state.ActiveAgent(m.state)
	if active == nil {
		return m, nil
	}
	if pty := m.ptys[active.ID]; pty != nil {
		_ = pty.Write(input.encoded)
	}
	return m, m.captureCodexInputArgs(*active, input.codexInputArgs())
}

func (m ClientModel) handleEnhancedKeyboardInput(input enhancedKeyboardInput) (tea.Model, tea.Cmd) {
	if input.hasKey {
		return m.handleKey(input.key)
	}
	if m.snapshot.State.Focus == state.FocusCodex && state.ActiveAgent(m.snapshot.State) != nil {
		return m, m.request("codex_input", input.codexInputArgs())
	}
	return m, nil
}

func unknownCSISequenceBytes(msg tea.Msg) ([]byte, bool) {
	stringer, ok := msg.(interface{ String() string })
	if !ok {
		return nil, false
	}
	return parseUnknownCSIString(stringer.String())
}

func parseUnknownCSIString(value string) ([]byte, bool) {
	if !strings.HasPrefix(value, "?CSI[") || !strings.HasSuffix(value, "]?") {
		return nil, false
	}
	fields := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(value, "?CSI["), "]?"))
	if len(fields) == 0 {
		return nil, false
	}
	raw := []byte{0x1b, '['}
	for _, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil || value < 0 || value > 255 {
			return nil, false
		}
		raw = append(raw, byte(value))
	}
	return raw, true
}

func parseCSIKeyboardEvent(raw []byte) (csiKeyboardEvent, bool) {
	if len(raw) < 4 || raw[0] != 0x1b || raw[1] != '[' {
		return csiKeyboardEvent{}, false
	}
	final := raw[len(raw)-1]
	body := string(raw[2 : len(raw)-1])
	if body == "" || strings.ContainsAny(body[:1], "<=>?") {
		return csiKeyboardEvent{}, false
	}
	switch final {
	case 'u':
		return parseCSIUKeyboardEvent(body, final)
	case '~':
		return parseCSITildeKeyboardEvent(body, final)
	}
	return csiKeyboardEvent{}, false
}

func parseCSIUKeyboardEvent(body string, final byte) (csiKeyboardEvent, bool) {
	fields := strings.Split(body, ";")
	keyCode, ok := parseCSIInt(fields[0])
	if !ok {
		return csiKeyboardEvent{}, false
	}
	event := csiKeyboardEvent{keyCode: keyCode, final: final}
	modifiers := 0
	if len(fields) > 1 && fields[1] != "" {
		encoded, eventType, ok := parseCSIModifierEvent(fields[1])
		if !ok {
			return csiKeyboardEvent{}, false
		}
		modifiers = max(0, encoded-1)
		event.eventType = eventType
	}
	event.modifiers = modifiers
	if len(fields) > 2 {
		event.text = parseCSIText(fields[2])
	}
	return event, true
}

func parseCSITildeKeyboardEvent(body string, final byte) (csiKeyboardEvent, bool) {
	fields := strings.Split(body, ";")
	if len(fields) >= 3 && fields[0] == "27" {
		modifier, ok := parseCSIInt(fields[1])
		if !ok {
			return csiKeyboardEvent{}, false
		}
		keyCode, ok := parseCSIInt(fields[2])
		if !ok {
			return csiKeyboardEvent{}, false
		}
		return csiKeyboardEvent{keyCode: keyCode, modifiers: max(0, modifier-1), final: final}, true
	}
	if len(fields) >= 2 {
		keyCode, ok := parseCSIInt(fields[0])
		if !ok {
			return csiKeyboardEvent{}, false
		}
		modifier, ok := parseCSIInt(fields[1])
		if !ok {
			return csiKeyboardEvent{}, false
		}
		return csiKeyboardEvent{keyCode: keyCode, modifiers: max(0, modifier-1), final: final}, true
	}
	return csiKeyboardEvent{}, false
}

func parseCSIInt(value string) (int, bool) {
	value = strings.Split(value, ":")[0]
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	return parsed, err == nil
}

func parseCSIModifierEvent(value string) (int, int, bool) {
	parts := strings.Split(value, ":")
	modifier, ok := parseCSIInt(parts[0])
	if !ok {
		return 0, 0, false
	}
	eventType := 0
	if len(parts) > 1 && parts[1] != "" {
		parsed, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
		eventType = parsed
	}
	return modifier, eventType, true
}

func parseCSIText(value string) []rune {
	parts := strings.Split(value, ":")
	text := make([]rune, 0, len(parts))
	for _, part := range parts {
		codepoint, ok := parseCSIInt(part)
		if !ok {
			return nil
		}
		text = append(text, rune(codepoint))
	}
	return text
}

func (event csiKeyboardEvent) isShiftEnter() bool {
	return event.keyCode == 13 && event.modifiers&1 != 0
}

func (event csiKeyboardEvent) isRelease() bool {
	return event.eventType == 3
}

func (event csiKeyboardEvent) keyMsg() (tea.KeyMsg, bool) {
	if event.final != 'u' && event.final != '~' {
		return tea.KeyMsg{}, false
	}
	switch event.keyCode {
	case 9:
		if event.modifiers&1 != 0 {
			return tea.KeyMsg{Type: tea.KeyShiftTab}, true
		}
		return tea.KeyMsg{Type: tea.KeyTab}, true
	case 13:
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case 27:
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	case 32:
		if event.modifiers&6 == 0 {
			return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}, true
		}
	case 127:
		return tea.KeyMsg{Type: tea.KeyBackspace}, true
	}
	if event.modifiers&4 != 0 && event.keyCode >= 'a' && event.keyCode <= 'z' {
		return tea.KeyMsg{Type: tea.KeyType(event.keyCode - 'a' + 1)}, true
	}
	if event.modifiers&6 == 0 && len(event.text) > 0 {
		if len(event.text) == 1 && event.text[0] == ' ' {
			return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}, true
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: event.text}, true
	}
	return tea.KeyMsg{}, false
}
