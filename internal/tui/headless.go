package tui

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/edwmurph/codux/internal/config"
	"github.com/edwmurph/codux/internal/ipc"
	"github.com/edwmurph/codux/internal/state"
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
			return ipc.Response{OK: true, Message: "Codux TUI stopped"}
		}
		mu.Lock()
		defer mu.Unlock()
		response, _ := model.handleIPC(request)
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
