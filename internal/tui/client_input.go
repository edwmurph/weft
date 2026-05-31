package tui

import (
	"bytes"
	"io"
	"sync/atomic"
	"time"

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
)

type clientInputRouter struct {
	input              io.Reader
	rt                 config.Runtime
	clientID           string
	drawer             []byte
	drawerSequences    [][]byte
	interruptSequences [][]byte
	send               func(command string, args map[string]string) error

	codexActive          atomic.Bool
	pending              []byte
	hold                 []byte
	bracketedPasteActive bool
}

func newClientInputRouter(input io.Reader, rt config.Runtime, clientID string, drawerBinding string) *clientInputRouter {
	router := &clientInputRouter{
		input:              input,
		rt:                 rt,
		clientID:           clientID,
		drawer:             bindingRawSequence(drawerBinding),
		drawerSequences:    bindingTerminalSequences(drawerBinding),
		interruptSequences: terminalInterruptSequences(),
	}
	router.send = router.sendIPC
	return router
}

func (r *clientInputRouter) SetCodexActive(active bool) {
	r.codexActive.Store(active)
}

func (r *clientInputRouter) CodexActive() bool {
	return r.codexActive.Load()
}

func (r *clientInputRouter) Read(p []byte) (int, error) {
	var buf [256]byte
	for {
		if len(r.pending) > 0 {
			return r.readPending(p), nil
		}
		n, err := r.input.Read(buf[:])
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if !r.codexActive.Load() {
				r.pending = append(r.pending, data...)
				return r.readPending(p), nil
			}
			r.routeCodexInput(data)
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
	if len(r.drawerSequences) == 0 && len(r.interruptSequences) == 0 {
		r.sendCodexBytes(data)
		return
	}
	for len(data) > 0 {
		if !r.codexActive.Load() {
			r.pending = append(r.pending, data...)
			return
		}
		kind, index, width := r.findCodexControl(data)
		if index >= 0 {
			r.sendCodexBytes(data[:index])
			switch kind {
			case codexControlDrawer:
				_ = r.send("toggle_drawer", nil)
				r.codexActive.Store(false)
				r.pending = append(r.pending, data[index+width:]...)
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
		keep := drawerPrefixSuffix(data, r.drawer)
		if sendLen := len(data) - keep; sendLen > 0 {
			r.sendCodexBytes(data[:sendLen])
		}
		if keep > 0 {
			r.hold = append(r.hold, data[len(data)-keep:]...)
		}
		return
	}
}

type codexControlKind string

const (
	codexControlDrawer    codexControlKind = "drawer"
	codexControlInterrupt codexControlKind = "interrupt"
	codexControlMouse     codexControlKind = "mouse"
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
		if width := matchingTerminalSequenceWidth(data[index:], r.interruptSequences); width > 0 {
			return codexControlInterrupt, index, width
		}
	}
	return "", -1, 0
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

func (r *clientInputRouter) sendIPC(command string, args map[string]string) error {
	args = clientRequestArgs(r.rt, r.clientID, command, args)
	_, err := ipc.Call(r.rt.SocketPath, ipc.Request{Command: command, Args: args}, 2*time.Second)
	return err
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
