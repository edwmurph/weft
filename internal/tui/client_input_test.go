package tui

import (
	"bytes"
	"io"
	"testing"
)

type sentClientCommand struct {
	command string
	args    map[string]string
}

type clientInputSendRecorder struct {
	entries []sentClientCommand
}

func newTestClientInputRouter(input io.Reader) *clientInputRouter {
	return &clientInputRouter{
		input:              input,
		drawer:             []byte{0x02},
		drawerSequences:    bindingTerminalSequences("C-b"),
		interruptSequences: terminalInterruptSequences(),
	}
}

func recordClientInputSends(router *clientInputRouter) *clientInputSendRecorder {
	recorder := &clientInputSendRecorder{}
	router.send = func(command string, args map[string]string) error {
		recorder.entries = append(recorder.entries, sentClientCommand{command: command, args: args})
		return nil
	}
	return recorder
}

func readClientInput(t *testing.T, router *clientInputRouter, size int) (string, error) {
	t.Helper()
	buf := make([]byte, size)
	n, err := router.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	return string(buf[:n]), err
}

func TestClientInputRouterForwardsRawCodexBytesUntilDrawer(t *testing.T) {
	router := newTestClientInputRouter(bytes.NewBufferString("ihello\x1b[200~paste\x1b[201~\x03\x02j"))
	router.SetCodexActive(true)
	sent := recordClientInputSends(router)

	if got, _ := readClientInput(t, router, 8); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent.entries) != 3 {
		t.Fatalf("sent commands = %#v", sent.entries)
	}
	if sent.entries[0].command != "codex_input" || sent.entries[0].args["input"] != codexInputRaw {
		t.Fatalf("first command should be raw codex input: %#v", sent.entries[0])
	}
	if got, want := sent.entries[0].args["encoded"], "ihello\x1b[200~paste\x1b[201~"; got != want {
		t.Fatalf("raw forwarded bytes = %q, want %q", got, want)
	}
	if sent.entries[1].command != "codex_input" || sent.entries[1].args["input"] != "ctrl+c" || sent.entries[1].args["encoded"] != "\x03" {
		t.Fatalf("terminal ctrl+c should be sent as Codex interrupt input: %#v", sent.entries[1])
	}
	if sent.entries[2].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent.entries[2])
	}
}

func TestClientInputRouterForwardsCtrlCInsideBracketedPaste(t *testing.T) {
	router := newTestClientInputRouter(bytes.NewBufferString("\x1b[200~paste\x03text\x1b[201~\x02j"))
	router.SetCodexActive(true)
	sent := recordClientInputSends(router)

	if got, _ := readClientInput(t, router, 8); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent.entries) != 2 {
		t.Fatalf("sent commands = %#v", sent.entries)
	}
	if got, want := sent.entries[0].args["encoded"], "\x1b[200~paste\x03text\x1b[201~"; got != want {
		t.Fatalf("bracketed paste bytes = %q, want %q", got, want)
	}
	if sent.entries[0].args["input"] != codexInputRaw {
		t.Fatalf("bracketed paste should stay raw: %#v", sent.entries[0])
	}
	if sent.entries[1].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent.entries[1])
	}
}

func TestClientInputRouterHandlesEnhancedDrawerSequence(t *testing.T) {
	router := newTestClientInputRouter(bytes.NewBufferString("ihello\x1b[98;5uj"))
	router.SetCodexActive(true)
	sent := recordClientInputSends(router)

	if got, _ := readClientInput(t, router, 8); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent.entries) != 2 {
		t.Fatalf("sent commands = %#v", sent.entries)
	}
	if got := sent.entries[0].args["encoded"]; got != "ihello" {
		t.Fatalf("raw forwarded bytes = %q, want ihello", got)
	}
	if sent.entries[1].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent.entries[1])
	}
}

func TestClientInputRouterTriggersCommandMenuShortcut(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		readSize int
		focus    func(*clientInputRouter)
		prefix   string
		suffix   string
	}{
		{
			name:     "codex focus",
			input:    "ihello\x1dj",
			readSize: 8,
			focus:    func(router *clientInputRouter) { router.SetCodexActive(true) },
			prefix:   "ihello",
			suffix:   "j",
		},
		{
			name:     "terminal focus",
			input:    "typed\x1b[93;5uafter",
			readSize: 16,
			focus:    func(router *clientInputRouter) { router.SetTaskInputMode(taskInputTerminal) },
			prefix:   "typed",
			suffix:   "after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestClientInputRouter(bytes.NewBufferString(tt.input))
			router.commandMenuSequences = bindingTerminalSequences("C-]")
			tt.focus(router)
			menus := 0
			router.commandMenu = func() { menus++ }
			sent := recordClientInputSends(router)

			got, err := readClientInput(t, router, tt.readSize)
			if got != "" || err != io.EOF {
				t.Fatalf("read = %q, %v; want EOF after consuming command menu shortcut", got, err)
			}
			if menus != 1 {
				t.Fatalf("command menu callback count = %d, want 1", menus)
			}
			if len(sent.entries) != 2 || sent.entries[0].args["encoded"] != tt.prefix || sent.entries[1].args["encoded"] != tt.suffix {
				t.Fatalf("prefix and suffix should stay ordered input: %#v", sent.entries)
			}
		})
	}
}

func TestClientInputRouterMapsEnhancedCtrlCToCodexInterrupt(t *testing.T) {
	router := newTestClientInputRouter(bytes.NewBufferString("work\x1b[27;5;99~after\x02j"))
	router.SetCodexActive(true)
	sent := recordClientInputSends(router)

	if got, _ := readClientInput(t, router, 8); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent.entries) != 4 {
		t.Fatalf("sent commands = %#v", sent.entries)
	}
	if got := sent.entries[0].args["encoded"]; got != "work" {
		t.Fatalf("raw prefix = %q, want work", got)
	}
	if sent.entries[1].args["input"] != "ctrl+c" || sent.entries[1].args["encoded"] != "\x1b[27;5;99~" {
		t.Fatalf("enhanced ctrl+c should be Codex interrupt input: %#v", sent.entries[1])
	}
	if got := sent.entries[2].args["encoded"]; got != "after" {
		t.Fatalf("raw suffix = %q, want after", got)
	}
	if sent.entries[3].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent.entries[3])
	}
}

func TestClientInputRouterReturnsMouseSequencesToBubbleTea(t *testing.T) {
	router := newTestClientInputRouter(bytes.NewBufferString("ready\x1b[<0;9;2Mafter\x02j"))
	router.SetCodexActive(true)
	sent := recordClientInputSends(router)

	if got, _ := readClientInput(t, router, 64); got != "\x1b[<0;9;2Mj" {
		t.Fatalf("mouse and post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent.entries) != 3 {
		t.Fatalf("sent commands = %#v", sent.entries)
	}
	if got := sent.entries[0].args["encoded"]; got != "ready" {
		t.Fatalf("raw prefix = %q, want ready", got)
	}
	if got := sent.entries[1].args["encoded"]; got != "after" {
		t.Fatalf("raw suffix = %q, want after", got)
	}
	if sent.entries[2].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent.entries[2])
	}
}

func TestClientInputRouterHoldsSplitMouseSequencesForBubbleTea(t *testing.T) {
	router := newTestClientInputRouter(io.MultiReader(bytes.NewBufferString("ready\x1b[<65;7;"), bytes.NewBufferString("7Mafter\x02j")))
	router.SetCodexActive(true)
	sent := recordClientInputSends(router)

	if got, _ := readClientInput(t, router, 64); got != "\x1b[<65;7;7Mj" {
		t.Fatalf("split mouse and post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent.entries) != 3 {
		t.Fatalf("sent commands = %#v", sent.entries)
	}
	if got := sent.entries[0].args["encoded"]; got != "ready" {
		t.Fatalf("raw prefix = %q, want ready", got)
	}
	if got := sent.entries[1].args["encoded"]; got != "after" {
		t.Fatalf("raw suffix = %q, want after", got)
	}
	if sent.entries[2].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent.entries[2])
	}
}

func TestClientInputRouterLeavesDashboardBytesForBubbleTea(t *testing.T) {
	router := newTestClientInputRouter(bytes.NewBufferString("n"))
	router.send = func(command string, args map[string]string) error {
		t.Fatalf("dashboard input should not call supervisor directly: %s %#v", command, args)
		return nil
	}

	if got, _ := readClientInput(t, router, 8); got != "n" {
		t.Fatalf("dashboard bytes = %q, want n", got)
	}
}

func TestClientInputRouterDecodesTerminalKeyboardInput(t *testing.T) {
	router := newTestClientInputRouter(bytes.NewBufferString("\x1b[101;2u\x1b[99u\x1b[104u\x1b[111u\x1b[117;5u\x02j"))
	router.SetTaskInputMode(taskInputTerminal)
	sent := recordClientInputSends(router)

	if got, _ := readClientInput(t, router, 8); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent.entries) != 6 {
		t.Fatalf("sent commands = %#v", sent.entries)
	}
	var forwarded string
	for _, entry := range sent.entries[:5] {
		if entry.command != "task_input" {
			t.Fatalf("terminal key should be task input: %#v", entry)
		}
		forwarded += entry.args["encoded"]
	}
	if forwarded != "Echo\x15" {
		t.Fatalf("decoded terminal input = %q, want shifted E plus C-u", forwarded)
	}
	if sent.entries[5].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent.entries[5])
	}
}

func TestClientInputRouterRoutesTerminalCtrlCAsInterrupt(t *testing.T) {
	router := newTestClientInputRouter(bytes.NewBufferString("run\x03after\x1b[99;5uend\x02j"))
	router.SetTaskInputMode(taskInputTerminal)
	sent := recordClientInputSends(router)

	if got, _ := readClientInput(t, router, 8); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent.entries) != 6 {
		t.Fatalf("sent commands = %#v", sent.entries)
	}
	if got := sent.entries[0].args["encoded"]; sent.entries[0].command != "task_input" || got != "run" {
		t.Fatalf("raw prefix should be terminal input: %#v", sent.entries[0])
	}
	for _, index := range []int{1, 3} {
		if sent.entries[index].command != "task_input" || sent.entries[index].args["input"] != "ctrl+c" || sent.entries[index].args["encoded"] != "\x03" {
			t.Fatalf("terminal ctrl+c should be recorded and forwarded as ETX: %#v", sent.entries[index])
		}
	}
	if got := sent.entries[2].args["encoded"]; sent.entries[2].command != "task_input" || got != "after" {
		t.Fatalf("raw middle should be terminal input: %#v", sent.entries[2])
	}
	if got := sent.entries[4].args["encoded"]; sent.entries[4].command != "task_input" || got != "end" {
		t.Fatalf("raw suffix should be terminal input: %#v", sent.entries[4])
	}
	if sent.entries[5].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent.entries[5])
	}
}

func TestClientInputRouterDropsTerminalModifierOnlyEvents(t *testing.T) {
	router := newTestClientInputRouter(bytes.NewBufferString("\x1b[57441;2u\x1b[57442;5u\x02j"))
	router.SetTaskInputMode(taskInputTerminal)
	sent := recordClientInputSends(router)

	if got, _ := readClientInput(t, router, 8); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent.entries) != 1 || sent.entries[0].command != "toggle_drawer" {
		t.Fatalf("modifier-only events should be dropped and C-b should exit: %#v", sent.entries)
	}
}

func TestClientInputRouterHandlesTerminalCommandKClear(t *testing.T) {
	router := newTestClientInputRouter(bytes.NewBufferString("typed\x1b[107;9uafter"))
	router.SetTaskInputMode(taskInputTerminal)
	sent := recordClientInputSends(router)

	_, _ = readClientInput(t, router, 8)

	if len(sent.entries) != 3 {
		t.Fatalf("sent commands = %#v", sent.entries)
	}
	if got := sent.entries[0].args["encoded"]; got != "typed" {
		t.Fatalf("raw prefix = %q, want typed", got)
	}
	if sent.entries[1].command != "task_clear" {
		t.Fatalf("command-k should clear the focused terminal: %#v", sent.entries[1])
	}
	if got := sent.entries[2].args["encoded"]; got != "after" {
		t.Fatalf("raw suffix = %q, want after", got)
	}
}

func TestDrawerPrefixSuffixHandlesSplitMultiByteDrawer(t *testing.T) {
	if got := drawerPrefixSuffix([]byte("\x1b["), []byte("\x1b[15~")); got != 2 {
		t.Fatalf("prefix suffix = %d, want 2", got)
	}
	if got := drawerPrefixSuffix([]byte("abc"), []byte("\x1b[15~")); got != 0 {
		t.Fatalf("unrelated suffix = %d, want 0", got)
	}
}
