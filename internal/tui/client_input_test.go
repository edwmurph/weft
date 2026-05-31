package tui

import (
	"bytes"
	"io"
	"testing"
)

func TestClientInputRouterForwardsRawCodexBytesUntilDrawer(t *testing.T) {
	router := &clientInputRouter{
		input:              bytes.NewBufferString("ihello\x1b[200~paste\x1b[201~\x03\x02j"),
		drawer:             []byte{0x02},
		drawerSequences:    bindingTerminalSequences("C-b"),
		interruptSequences: terminalInterruptSequences(),
	}
	router.codexActive.Store(true)
	var sent []struct {
		command string
		args    map[string]string
	}
	router.send = func(command string, args map[string]string) error {
		sent = append(sent, struct {
			command string
			args    map[string]string
		}{command: command, args: args})
		return nil
	}

	buf := make([]byte, 8)
	n, err := router.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	if got := string(buf[:n]); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent) != 3 {
		t.Fatalf("sent commands = %#v", sent)
	}
	if sent[0].command != "codex_input" || sent[0].args["input"] != codexInputRaw {
		t.Fatalf("first command should be raw codex input: %#v", sent[0])
	}
	if got, want := sent[0].args["encoded"], "ihello\x1b[200~paste\x1b[201~"; got != want {
		t.Fatalf("raw forwarded bytes = %q, want %q", got, want)
	}
	if sent[1].command != "codex_input" || sent[1].args["input"] != "ctrl+c" || sent[1].args["encoded"] != "\x03" {
		t.Fatalf("terminal ctrl+c should be sent as Codex interrupt input: %#v", sent[1])
	}
	if sent[2].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent[2])
	}
}

func TestClientInputRouterForwardsCtrlCInsideBracketedPaste(t *testing.T) {
	router := &clientInputRouter{
		input:              bytes.NewBufferString("\x1b[200~paste\x03text\x1b[201~\x02j"),
		drawer:             []byte{0x02},
		drawerSequences:    bindingTerminalSequences("C-b"),
		interruptSequences: terminalInterruptSequences(),
	}
	router.codexActive.Store(true)
	var sent []struct {
		command string
		args    map[string]string
	}
	router.send = func(command string, args map[string]string) error {
		sent = append(sent, struct {
			command string
			args    map[string]string
		}{command: command, args: args})
		return nil
	}

	buf := make([]byte, 8)
	n, err := router.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	if got := string(buf[:n]); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent) != 2 {
		t.Fatalf("sent commands = %#v", sent)
	}
	if got, want := sent[0].args["encoded"], "\x1b[200~paste\x03text\x1b[201~"; got != want {
		t.Fatalf("bracketed paste bytes = %q, want %q", got, want)
	}
	if sent[0].args["input"] != codexInputRaw {
		t.Fatalf("bracketed paste should stay raw: %#v", sent[0])
	}
	if sent[1].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent[1])
	}
}

func TestClientInputRouterHandlesEnhancedDrawerSequence(t *testing.T) {
	router := &clientInputRouter{
		input:              bytes.NewBufferString("ihello\x1b[98;5uj"),
		drawer:             []byte{0x02},
		drawerSequences:    bindingTerminalSequences("C-b"),
		interruptSequences: terminalInterruptSequences(),
	}
	router.codexActive.Store(true)
	var sent []struct {
		command string
		args    map[string]string
	}
	router.send = func(command string, args map[string]string) error {
		sent = append(sent, struct {
			command string
			args    map[string]string
		}{command: command, args: args})
		return nil
	}

	buf := make([]byte, 8)
	n, err := router.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	if got := string(buf[:n]); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent) != 2 {
		t.Fatalf("sent commands = %#v", sent)
	}
	if got := sent[0].args["encoded"]; got != "ihello" {
		t.Fatalf("raw forwarded bytes = %q, want ihello", got)
	}
	if sent[1].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent[1])
	}
}

func TestClientInputRouterMapsEnhancedCtrlCToCodexInterrupt(t *testing.T) {
	router := &clientInputRouter{
		input:              bytes.NewBufferString("work\x1b[27;5;99~after\x02j"),
		drawer:             []byte{0x02},
		drawerSequences:    bindingTerminalSequences("C-b"),
		interruptSequences: terminalInterruptSequences(),
	}
	router.codexActive.Store(true)
	var sent []struct {
		command string
		args    map[string]string
	}
	router.send = func(command string, args map[string]string) error {
		sent = append(sent, struct {
			command string
			args    map[string]string
		}{command: command, args: args})
		return nil
	}

	buf := make([]byte, 8)
	n, err := router.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	if got := string(buf[:n]); got != "j" {
		t.Fatalf("post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent) != 4 {
		t.Fatalf("sent commands = %#v", sent)
	}
	if got := sent[0].args["encoded"]; got != "work" {
		t.Fatalf("raw prefix = %q, want work", got)
	}
	if sent[1].args["input"] != "ctrl+c" || sent[1].args["encoded"] != "\x1b[27;5;99~" {
		t.Fatalf("enhanced ctrl+c should be Codex interrupt input: %#v", sent[1])
	}
	if got := sent[2].args["encoded"]; got != "after" {
		t.Fatalf("raw suffix = %q, want after", got)
	}
	if sent[3].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent[3])
	}
}

func TestClientInputRouterReturnsMouseSequencesToBubbleTea(t *testing.T) {
	router := &clientInputRouter{
		input:              bytes.NewBufferString("ready\x1b[<0;9;2Mafter\x02j"),
		drawer:             []byte{0x02},
		drawerSequences:    bindingTerminalSequences("C-b"),
		interruptSequences: terminalInterruptSequences(),
	}
	router.codexActive.Store(true)
	var sent []struct {
		command string
		args    map[string]string
	}
	router.send = func(command string, args map[string]string) error {
		sent = append(sent, struct {
			command string
			args    map[string]string
		}{command: command, args: args})
		return nil
	}

	buf := make([]byte, 64)
	n, err := router.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	if got := string(buf[:n]); got != "\x1b[<0;9;2Mj" {
		t.Fatalf("mouse and post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent) != 3 {
		t.Fatalf("sent commands = %#v", sent)
	}
	if got := sent[0].args["encoded"]; got != "ready" {
		t.Fatalf("raw prefix = %q, want ready", got)
	}
	if got := sent[1].args["encoded"]; got != "after" {
		t.Fatalf("raw suffix = %q, want after", got)
	}
	if sent[2].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent[2])
	}
}

func TestClientInputRouterHoldsSplitMouseSequencesForBubbleTea(t *testing.T) {
	router := &clientInputRouter{
		input:              io.MultiReader(bytes.NewBufferString("ready\x1b[<65;7;"), bytes.NewBufferString("7Mafter\x02j")),
		drawer:             []byte{0x02},
		drawerSequences:    bindingTerminalSequences("C-b"),
		interruptSequences: terminalInterruptSequences(),
	}
	router.codexActive.Store(true)
	var sent []struct {
		command string
		args    map[string]string
	}
	router.send = func(command string, args map[string]string) error {
		sent = append(sent, struct {
			command string
			args    map[string]string
		}{command: command, args: args})
		return nil
	}

	buf := make([]byte, 64)
	n, err := router.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	if got := string(buf[:n]); got != "\x1b[<65;7;7Mj" {
		t.Fatalf("split mouse and post-drawer bytes should return to Bubble Tea, got %q", got)
	}
	if len(sent) != 3 {
		t.Fatalf("sent commands = %#v", sent)
	}
	if got := sent[0].args["encoded"]; got != "ready" {
		t.Fatalf("raw prefix = %q, want ready", got)
	}
	if got := sent[1].args["encoded"]; got != "after" {
		t.Fatalf("raw suffix = %q, want after", got)
	}
	if sent[2].command != "toggle_drawer" {
		t.Fatalf("drawer command = %#v", sent[2])
	}
}

func TestClientInputRouterLeavesDashboardBytesForBubbleTea(t *testing.T) {
	router := &clientInputRouter{
		input:              bytes.NewBufferString("n"),
		drawer:             []byte{0x02},
		drawerSequences:    bindingTerminalSequences("C-b"),
		interruptSequences: terminalInterruptSequences(),
	}
	router.send = func(command string, args map[string]string) error {
		t.Fatalf("dashboard input should not call supervisor directly: %s %#v", command, args)
		return nil
	}

	buf := make([]byte, 8)
	n, err := router.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "n" {
		t.Fatalf("dashboard bytes = %q, want n", got)
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
