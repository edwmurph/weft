package tui

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/state"
)

func RunHeadless(rt config.Runtime, cfg config.Config, st state.State, migration *state.Migration) error {
	model := NewModel(rt, cfg, st)
	if migration != nil {
		model.migration = migration.Message
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	stopIPC, err := ipc.Serve(rt.SocketPath, func(request ipc.Request) ipc.Response {
		if request.Command == "shutdown" {
			cancel()
			return ipc.Response{OK: true, Message: "Weft TUI stopped"}
		}
		mu.Lock()
		response, cmd := model.handleIPC(request)
		mu.Unlock()
		runHeadlessCmd(cmd, &model, &mu)
		return response
	})
	if err != nil {
		return err
	}
	defer stopIPC()

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(signals)

	for {
		select {
		case data := <-model.dataCh:
			mu.Lock()
			model.applyPTYData(data)
			mu.Unlock()
		case signal := <-signals:
			if signal == syscall.SIGHUP {
				continue
			}
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func runHeadlessCmd(cmd tea.Cmd, model *Model, mu *sync.Mutex) {
	if cmd == nil {
		return
	}
	go func() {
		applyHeadlessMsg(cmd(), model, mu)
	}()
}

func applyHeadlessMsg(msg tea.Msg, model *Model, mu *sync.Mutex) {
	switch typed := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, cmd := range typed {
			runHeadlessCmd(cmd, model, mu)
		}
	case ptyStartedMsg:
		mu.Lock()
		model.applyPTYStarted(typed)
		mu.Unlock()
	case titleHookMsg:
		mu.Lock()
		model.applyTitleHook(typed)
		mu.Unlock()
	}
}
