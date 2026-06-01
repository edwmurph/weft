package tui

import (
	"bytes"
	"io"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
)

type taskInputMode int32

const (
	taskInputNone taskInputMode = iota
	taskInputCodex
	taskInputTerminal
)

type clientInputRouter struct {
	input              io.Reader
	rt                 config.Runtime
	clientID           string
	drawer             []byte
	drawerSequences    [][]byte
	repaintSequences   [][]byte
	interruptSequences [][]byte
	send               func(command string, args map[string]string) error
	repaint            func()

	inputMode            atomic.Int32
	pending              []byte
	hold                 []byte
	deferred             []byte
	bracketedPasteActive bool
}

func newClientInputRouter(input io.Reader, rt config.Runtime, clientID string, drawerBinding string, repaintBinding string) *clientInputRouter {
	router := &clientInputRouter{
		input:              input,
		rt:                 rt,
		clientID:           clientID,
		drawer:             bindingRawSequence(drawerBinding),
		drawerSequences:    bindingTerminalSequences(drawerBinding),
		repaintSequences:   bindingTerminalSequences(repaintBinding),
		interruptSequences: terminalInterruptSequences(),
	}
	router.send = router.sendIPC
	return router
}

func (r *clientInputRouter) SetCodexActive(active bool) {
	if active {
		r.SetTaskInputMode(taskInputCodex)
		return
	}
	r.SetTaskInputMode(taskInputNone)
}

func (r *clientInputRouter) CodexActive() bool {
	return r.TaskInputMode() == taskInputCodex
}

func (r *clientInputRouter) SetTaskInputMode(mode taskInputMode) {
	r.inputMode.Store(int32(mode))
}

func (r *clientInputRouter) TaskInputMode() taskInputMode {
	return taskInputMode(r.inputMode.Load())
}

func (r *clientInputRouter) TaskInputActive() bool {
	return r.TaskInputMode() != taskInputNone
}

func (r *clientInputRouter) Read(p []byte) (int, error) {
	var buf [256]byte
	for {
		if len(r.pending) > 0 {
			return r.readPending(p), nil
		}
		if len(r.deferred) > 0 {
			data := append([]byte(nil), r.deferred...)
			r.deferred = nil
			switch r.TaskInputMode() {
			case taskInputCodex:
				r.routeCodexInput(data)
			case taskInputTerminal:
				r.routeTerminalInput(data)
			default:
				r.pending = append(r.pending, data...)
				return r.readPending(p), nil
			}
			if len(r.pending) > 0 {
				return r.readPending(p), nil
			}
		}
		n, err := r.input.Read(buf[:])
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			switch r.TaskInputMode() {
			case taskInputCodex:
				r.routeCodexInput(data)
			case taskInputTerminal:
				r.routeTerminalInput(data)
			default:
				r.pending = append(r.pending, data...)
				return r.readPending(p), nil
			}
			if len(r.pending) > 0 {
				return r.readPending(p), nil
			}
		}
		if err != nil {
			return 0, err
		}
	}
}

func (r *clientInputRouter) Write(p []byte) (int, error) {
	if writer, ok := r.input.(io.Writer); ok {
		return writer.Write(p)
	}
	return 0, io.ErrClosedPipe
}

func (r *clientInputRouter) Close() error {
	return nil
}

func (r *clientInputRouter) Fd() uintptr {
	if file, ok := r.input.(interface{ Fd() uintptr }); ok {
		return file.Fd()
	}
	return ^uintptr(0)
}

func (r *clientInputRouter) readPending(p []byte) int {
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n
}

func (r *clientInputRouter) routeCodexInput(data []byte) {
	if len(r.hold) > 0 {
		next := make([]byte, 0, len(r.hold)+len(data))
		next = append(next, r.hold...)
		next = append(next, data...)
		data = next
		r.hold = nil
	}
	if len(r.drawerSequences) == 0 && len(r.repaintSequences) == 0 && len(r.interruptSequences) == 0 {
		r.sendCodexBytes(data)
		return
	}
	for len(data) > 0 {
		if r.TaskInputMode() != taskInputCodex {
			r.pending = append(r.pending, data...)
			return
		}
		kind, index, width := r.findCodexControl(data)
		if index >= 0 {
			r.sendCodexBytes(data[:index])
			switch kind {
			case codexControlDrawer:
				_ = r.send("toggle_drawer", nil)
				r.SetTaskInputMode(taskInputNone)
				r.pending = append(r.pending, data[index+width:]...)
				return
			case codexControlRepaint:
				r.triggerRepaint()
				r.deferred = append(r.deferred, data[index+width:]...)
				return
			case codexControlInterrupt:
				r.sendCodexInterrupt(data[index : index+width])
				data = data[index+width:]
				continue
			case codexControlMouse:
				r.pending = append(r.pending, data[index:index+width]...)
				data = data[index+width:]
				continue
			}
		}
		keep := max(drawerPrefixSuffix(data, r.drawer), sgrMousePrefixSuffix(data, r.bracketedPasteActive), csiKeyboardPrefixSuffix(data))
		if sendLen := len(data) - keep; sendLen > 0 {
			r.sendCodexBytes(data[:sendLen])
		}
		if keep > 0 {
			r.hold = append(r.hold, data[len(data)-keep:]...)
		}
		return
	}
}

func (r *clientInputRouter) routeTerminalInput(data []byte) {
	if len(r.hold) > 0 {
		next := make([]byte, 0, len(r.hold)+len(data))
		next = append(next, r.hold...)
		next = append(next, data...)
		data = next
		r.hold = nil
	}
	for len(data) > 0 {
		if r.TaskInputMode() != taskInputTerminal {
			r.pending = append(r.pending, data...)
			return
		}
		kind, index, width := r.findTerminalControl(data)
		if index >= 0 {
			r.sendTerminalBytes(data[:index])
			switch kind {
			case codexControlDrawer:
				_ = r.send("toggle_drawer", nil)
				r.SetTaskInputMode(taskInputNone)
				r.pending = append(r.pending, data[index+width:]...)
				return
			case codexControlRepaint:
				r.triggerRepaint()
				r.deferred = append(r.deferred, data[index+width:]...)
				return
			case codexControlMouse:
				r.pending = append(r.pending, data[index:index+width]...)
			case terminalControlInterrupt:
				r.sendTerminalInterrupt()
			case terminalControlKeyboard:
				r.handleTerminalKeyboard(data[index : index+width])
			}
			data = data[index+width:]
			continue
		}
		keep := max(drawerPrefixSuffix(data, r.drawer), sgrMousePrefixSuffix(data, r.bracketedPasteActive), csiKeyboardPrefixSuffix(data))
		if sendLen := len(data) - keep; sendLen > 0 {
			r.sendTerminalBytes(data[:sendLen])
		}
		if keep > 0 {
			r.hold = append(r.hold, data[len(data)-keep:]...)
		}
		return
	}
}

type codexControlKind string

const (
	codexControlDrawer       codexControlKind = "drawer"
	codexControlRepaint      codexControlKind = "repaint"
	codexControlInterrupt    codexControlKind = "interrupt"
	codexControlMouse        codexControlKind = "mouse"
	terminalControlInterrupt codexControlKind = "terminal_interrupt"
	terminalControlKeyboard  codexControlKind = "keyboard"
)

var (
	bracketedPasteStart = []byte("\x1b[200~")
	bracketedPasteEnd   = []byte("\x1b[201~")
)

func (r *clientInputRouter) findCodexControl(data []byte) (codexControlKind, int, int) {
	for index := 0; index < len(data); index++ {
		if r.bracketedPasteActive {
			if bytes.HasPrefix(data[index:], bracketedPasteEnd) {
				r.bracketedPasteActive = false
				index += len(bracketedPasteEnd) - 1
			}
			continue
		}
		if bytes.HasPrefix(data[index:], bracketedPasteStart) {
			r.bracketedPasteActive = true
			index += len(bracketedPasteStart) - 1
			continue
		}
		if width, ok := consumeSGRMouseSequence(data[index:]); ok {
			return codexControlMouse, index, width
		}
		if width := matchingTerminalSequenceWidth(data[index:], r.drawerSequences); width > 0 {
			return codexControlDrawer, index, width
		}
		if width := matchingTerminalSequenceWidth(data[index:], r.repaintSequences); width > 0 {
			return codexControlRepaint, index, width
		}
		if width := matchingTerminalSequenceWidth(data[index:], r.interruptSequences); width > 0 {
			return codexControlInterrupt, index, width
		}
	}
	return "", -1, 0
}

func (r *clientInputRouter) findTerminalControl(data []byte) (codexControlKind, int, int) {
	for index := 0; index < len(data); index++ {
		if r.bracketedPasteActive {
			if bytes.HasPrefix(data[index:], bracketedPasteEnd) {
				r.bracketedPasteActive = false
				index += len(bracketedPasteEnd) - 1
			}
			continue
		}
		if bytes.HasPrefix(data[index:], bracketedPasteStart) {
			r.bracketedPasteActive = true
			index += len(bracketedPasteStart) - 1
			continue
		}
		if width, ok := consumeSGRMouseSequence(data[index:]); ok {
			return codexControlMouse, index, width
		}
		if width := matchingTerminalSequenceWidth(data[index:], r.drawerSequences); width > 0 {
			return codexControlDrawer, index, width
		}
		if width := matchingTerminalSequenceWidth(data[index:], r.repaintSequences); width > 0 {
			return codexControlRepaint, index, width
		}
		if width := matchingTerminalSequenceWidth(data[index:], r.interruptSequences); width > 0 {
			return terminalControlInterrupt, index, width
		}
		if sequence, width, ok := consumeCSISequence(data[index:]); ok {
			if _, ok := parseCSIKeyboardEvent(sequence); ok {
				return terminalControlKeyboard, index, width
			}
		}
	}
	return "", -1, 0
}

func sgrMousePrefixSuffix(data []byte, bracketedPasteActive bool) int {
	if bracketedPasteActive {
		return 0
	}
	for index := max(0, len(data)-32); index < len(data); index++ {
		width, ok := incompleteSGRMousePrefix(data[index:])
		if ok && index+width == len(data) {
			return width
		}
	}
	return 0
}

func incompleteSGRMousePrefix(data []byte) (int, bool) {
	if len(data) < 3 || !bytes.HasPrefix(data, []byte("\x1b[<")) {
		return 0, false
	}
	semicolons := 0
	for index := 3; index < len(data); index++ {
		switch data[index] {
		case 'M', 'm':
			return 0, false
		case ';':
			semicolons++
			if semicolons > 2 {
				return 0, false
			}
		default:
			if data[index] < '0' || data[index] > '9' {
				return 0, false
			}
		}
	}
	return len(data), true
}

func consumeSGRMouseSequence(data []byte) (int, bool) {
	if len(data) < 4 || !bytes.HasPrefix(data, []byte("\x1b[<")) {
		return 0, false
	}
	for index := 3; index < len(data); index++ {
		switch data[index] {
		case 'M', 'm':
			return index + 1, true
		case ';':
			continue
		default:
			if data[index] < '0' || data[index] > '9' {
				return 0, false
			}
		}
	}
	return 0, false
}

func matchingTerminalSequenceWidth(data []byte, sequences [][]byte) int {
	width := 0
	for _, sequence := range sequences {
		if len(sequence) > width && bytes.HasPrefix(data, sequence) {
			width = len(sequence)
		}
	}
	return width
}

func (r *clientInputRouter) sendCodexBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	_ = r.send("codex_input", map[string]string{
		"encoded": string(data),
		"input":   codexInputRaw,
	})
}

func (r *clientInputRouter) sendCodexInterrupt(data []byte) {
	encoded := string(data)
	if encoded == "" {
		encoded = "\x03"
	}
	_ = r.send("codex_input", map[string]string{
		"encoded": encoded,
		"input":   "ctrl+c",
	})
}

func (r *clientInputRouter) sendTerminalBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	_ = r.send("task_input", map[string]string{
		"encoded": string(data),
		"input":   codexInputRaw,
	})
}

func (r *clientInputRouter) sendTerminalInterrupt() {
	_ = r.send("task_input", map[string]string{
		"encoded": "\x03",
		"input":   "ctrl+c",
	})
}

func (r *clientInputRouter) handleTerminalKeyboard(raw []byte) {
	event, ok := parseCSIKeyboardEvent(raw)
	if !ok || event.isRelease() {
		return
	}
	if key, ok := event.keyMsg(); ok && isCtrlCKey(key) {
		r.sendTerminalInterrupt()
		return
	}
	if event.isCommandK() {
		_ = r.send("task_clear", nil)
		return
	}
	if event.hasSuperModifier() {
		return
	}
	if encoded, ok := event.terminalBytes(); ok {
		r.sendTerminalBytes(encoded)
	}
}

func (r *clientInputRouter) sendIPC(command string, args map[string]string) error {
	args = clientRequestArgs(r.rt, r.clientID, command, args)
	_, err := ipc.Call(r.rt.SocketPath, ipc.Request{Command: command, Args: args}, 2*time.Second)
	return err
}

func (r *clientInputRouter) triggerRepaint() {
	if r.repaint != nil {
		r.repaint()
	}
}

func drawerPrefixSuffix(data []byte, drawer []byte) int {
	maxKeep := min(len(data), len(drawer)-1)
	for keep := maxKeep; keep > 0; keep-- {
		if bytes.Equal(data[len(data)-keep:], drawer[:keep]) {
			return keep
		}
	}
	return 0
}

func csiKeyboardPrefixSuffix(data []byte) int {
	for index := max(0, len(data)-32); index < len(data); index++ {
		if incompleteCSIKeyboardPrefix(data[index:]) {
			return len(data) - index
		}
	}
	return 0
}

func incompleteCSIKeyboardPrefix(data []byte) bool {
	if len(data) < 2 || data[0] != 0x1b || data[1] != '[' {
		return false
	}
	for index := 2; index < len(data); index++ {
		if data[index] >= 0x40 && data[index] <= 0x7e {
			return false
		}
	}
	return true
}

func (event csiKeyboardEvent) hasSuperModifier() bool {
	return event.modifiers&8 != 0
}

func (event csiKeyboardEvent) isCommandK() bool {
	return event.hasSuperModifier() && unicode.ToLower(rune(event.keyCode)) == 'k'
}

func (event csiKeyboardEvent) terminalBytes() ([]byte, bool) {
	if event.modifiers&4 != 0 {
		r := unicode.ToLower(rune(event.keyCode))
		if r >= 'a' && r <= 'z' {
			return []byte{byte(r - 'a' + 1)}, true
		}
	}
	if encoded, ok := event.terminalPrintableBytes(); ok {
		return encoded, true
	}
	if key, ok := event.keyMsg(); ok {
		encoded := encodeKey(key)
		if len(encoded) == 0 {
			return nil, false
		}
		if event.modifiers&2 != 0 {
			return append([]byte{0x1b}, encoded...), true
		}
		return encoded, true
	}
	return nil, false
}

func (event csiKeyboardEvent) terminalPrintableBytes() ([]byte, bool) {
	if event.modifiers&4 != 0 {
		return nil, false
	}
	alt := event.modifiers&2 != 0
	if event.modifiers&6 == 0 && len(event.text) > 0 {
		encoded := []byte(string(event.text))
		if alt {
			return append([]byte{0x1b}, encoded...), true
		}
		return encoded, true
	}
	if event.final == 'u' && event.modifiers&4 == 0 {
		r := rune(event.keyCode)
		if unicode.IsPrint(r) {
			if event.modifiers&1 != 0 && r >= 'a' && r <= 'z' {
				r = unicode.ToUpper(r)
			}
			encoded := []byte(string(r))
			if alt {
				return append([]byte{0x1b}, encoded...), true
			}
			return encoded, true
		}
	}
	return nil, false
}
