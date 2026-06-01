package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
)

type testCSIMessage string

func (m testCSIMessage) String() string {
	return string(m)
}

func TestBindingMatchesWeftConfigSpelling(t *testing.T) {
	if !bindingMatches("C-c", tea.KeyMsg{Type: tea.KeyCtrlC}) {
		t.Fatal("C-c should match ctrl+c")
	}
	if !bindingMatches("C-]", tea.KeyMsg{Type: tea.KeyCtrlCloseBracket}) {
		t.Fatal("C-] should match ctrl+]")
	}
	if !bindingMatches("S-Left", tea.KeyMsg{Type: tea.KeyShiftLeft}) {
		t.Fatal("S-Left should match shift+left")
	}
}

func TestBindingTerminalSequencesIncludesCtrlCloseBracketCSIU(t *testing.T) {
	sequences := bindingTerminalSequences("C-]")
	for _, expected := range []string{"\x1d", "\x1b[93;5u", "\x1b[93;5:1u", "\x1b[27;5;93~"} {
		found := false
		for _, sequence := range sequences {
			if string(sequence) == expected {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing terminal sequence %q in %#v", expected, sequences)
		}
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
		args := input.codexInputArgs()
		if got := args["encoded"]; got != raw {
			t.Fatalf("encoded = %q, want %q", got, raw)
		}
		if got := args["input"]; got != codexInputShiftTab {
			t.Fatalf("input kind = %q", got)
		}
	}
}

func TestEnhancedKeyboardInputMapsPrintableAndEnterCSIUForCodexCapture(t *testing.T) {
	for _, tc := range []struct {
		raw       string
		wantInput string
		wantText  string
	}{
		{raw: "\x1b[102u", wantInput: "text", wantText: "f"},
		{raw: "\x1b[63u", wantInput: "text", wantText: "?"},
		{raw: "\x1b[13u", wantInput: "enter"},
	} {
		input, ok := enhancedKeyboardInputFromMsg(testCSIMessage(unknownCSIString(tc.raw)))
		if !ok {
			t.Fatalf("expected enhanced input for %q", tc.raw)
		}
		if !input.hasKey {
			t.Fatalf("expected parsed key for %q", tc.raw)
		}
		args := input.codexInputArgs()
		if got := args["encoded"]; got != tc.raw {
			t.Fatalf("encoded = %q, want %q", got, tc.raw)
		}
		if got := args["input"]; got != tc.wantInput {
			t.Fatalf("input = %q, want %q", got, tc.wantInput)
		}
		if got := args["text"]; got != tc.wantText {
			t.Fatalf("text = %q, want %q", got, tc.wantText)
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
		if got := string(input.encoded); got != raw {
			t.Fatalf("ctrl+c should keep raw encoded bytes, got %q want %q", got, raw)
		}
		if got := input.codexInputArgs()["input"]; got != "ctrl+c" {
			t.Fatalf("ctrl+c should request terminal interrupt, got input kind %q", got)
		}
		if got := input.codexInputArgs()["encoded"]; got != raw {
			t.Fatalf("ctrl+c should preserve enhanced terminal bytes, got encoded %q want %q", got, raw)
		}
		cfg := config.DefaultConfig()
		active := &state.Task{ID: "a"}
		if input.shouldHandleAsKey(cfg, state.FocusConsole, active) {
			t.Fatalf("ctrl+c should pass through to Codex in Codex focus for %q", raw)
		}
		if !input.shouldHandleAsKey(cfg, state.FocusTasks, active) {
			t.Fatalf("ctrl+c should remain a Weft key outside Codex focus for %q", raw)
		}
	}
}

func TestEnhancedKeyboardInputKeepsDrawerKeyForWeftInCodexFocus(t *testing.T) {
	input, ok := enhancedKeyboardInputFromMsg(testCSIMessage(unknownCSIString("\x1b[98;5u")))
	if !ok {
		t.Fatal("expected enhanced input for ctrl+b")
	}
	if !input.hasKey || input.key.Type != tea.KeyCtrlB {
		t.Fatalf("expected ctrl+b key, got %#v", input.key)
	}
	cfg := config.DefaultConfig()
	active := &state.Task{ID: "a"}
	if !input.shouldHandleAsKey(cfg, state.FocusConsole, active) {
		t.Fatal("configured drawer key should stay owned by Weft in Codex focus")
	}
}

func TestCodexInputArgsSendsPlainCtrlCAsEnhancedCodexKey(t *testing.T) {
	args := codexInputArgs(tea.KeyMsg{Type: tea.KeyCtrlC})
	if got := args["input"]; got != "ctrl+c" {
		t.Fatalf("input = %q, want ctrl+c", got)
	}
	if got := args["encoded"]; got != terminalKeyboardCtrlC {
		t.Fatalf("encoded = %q, want enhanced ctrl+c %q", got, terminalKeyboardCtrlC)
	}
}

func TestRouteCodexInputArgsSendsWorkingCtrlCAsInterruptKey(t *testing.T) {
	args := map[string]string{"input": "ctrl+c", "encoded": terminalKeyboardCtrlC}
	routed := routeCodexInputArgs(state.Task{CodexTitle: "Fake Codex Working", Status: state.StatusRunning}, args)
	if got := routed["encoded"]; got != terminalKeyboardInterrupt {
		t.Fatalf("working ctrl+c encoded = %q, want interrupt key %q", got, terminalKeyboardInterrupt)
	}
	if got := args["encoded"]; got != terminalKeyboardCtrlC {
		t.Fatalf("routeCodexInputArgs mutated original args, got %q", got)
	}

	crafting := routeCodexInputArgs(state.Task{CodexTitle: "Fake Codex Crafting", Status: state.StatusRunning}, args)
	if got := crafting["encoded"]; got != terminalKeyboardInterrupt {
		t.Fatalf("crafting ctrl+c encoded = %q, want interrupt key %q", got, terminalKeyboardInterrupt)
	}

	waiting := routeCodexInputArgs(state.Task{CodexTitle: "Fake Codex Waiting", Status: state.StatusRunning}, args)
	if got := waiting["encoded"]; got != terminalKeyboardInterrupt {
		t.Fatalf("waiting ctrl+c encoded = %q, want interrupt key %q", got, terminalKeyboardInterrupt)
	}

	ready := routeCodexInputArgs(state.Task{CodexTitle: "Fake Codex Ready", Status: state.StatusRunning}, args)
	if got := ready["encoded"]; got != terminalKeyboardCtrlC {
		t.Fatalf("ready ctrl+c encoded = %q, want original ctrl+c %q", got, terminalKeyboardCtrlC)
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
