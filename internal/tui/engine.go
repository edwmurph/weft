package tui

import (
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

func RunEngineCmd(cmd tea.Cmd, model *Model, mu *sync.Mutex) {
	if cmd == nil {
		return
	}
	go func() {
		ApplyEngineMsg(cmd(), model, mu)
	}()
}

func ApplyEngineMsg(msg tea.Msg, model *Model, mu *sync.Mutex) {
	switch typed := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, cmd := range typed {
			RunEngineCmd(cmd, model, mu)
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
