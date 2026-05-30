package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/codux/internal/config"
	"github.com/edwmurph/codux/internal/state"
)

func TestEmptyDashboardStartsInNavFocus(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("codux-test")

	model := NewModel(rt, cfg, state.Empty())

	if model.state.Focus != state.FocusNav {
		t.Fatalf("focus = %s", model.state.Focus)
	}
}

func TestNavKeyWorksWhenPersistedEmptyStateHadCodexFocus(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("codux-test")
	cfg.CodexCommand = "cat"
	model := NewModel(rt, cfg, state.State{Version: state.Version, Focus: state.FocusCodex})
	defer func() {
		for _, pty := range model.ptys {
			pty.Kill()
		}
	}()

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model = updated.(Model)
	defer func() {
		for _, pty := range model.ptys {
			pty.Kill()
		}
	}()

	if len(model.state.Tabs) != 1 {
		t.Fatalf("tabs = %#v", model.state.Tabs)
	}
}

func TestCodexFocusOnlyHandlesGlobalShortcuts(t *testing.T) {
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("s")},
		{Type: tea.KeyRunes, Runes: []rune("?")},
		{Type: tea.KeyRunes, Runes: []rune("n")},
		{Type: tea.KeyShiftRight},
		{Type: tea.KeyCtrlD},
	} {
		rt := testRuntime(t)
		cfg := config.DefaultConfig("codux-test")
		model := NewModel(rt, cfg, state.State{
			Version: state.Version,
			Focus:   state.FocusCodex,
			Tabs: []state.Tab{
				{ID: "a", Title: "alpha", Column: "inbox", Status: state.StatusRunning},
			},
			ActiveTabID: "a",
		})

		updated, cmd := model.handleKey(msg)
		model = updated.(Model)

		if cmd != nil {
			t.Fatalf("%s should not start dashboard command in codex focus", msg.String())
		}
		if model.mode != modeNormal {
			t.Fatalf("%s changed mode to %s", msg.String(), model.mode)
		}
		if model.state.Focus != state.FocusCodex {
			t.Fatalf("%s changed focus to %s", msg.String(), model.state.Focus)
		}
		if len(model.state.Tabs) != 1 {
			t.Fatalf("%s changed tabs: %#v", msg.String(), model.state.Tabs)
		}
		active := state.ActiveTab(model.state)
		if active == nil || active.Column != "inbox" {
			t.Fatalf("%s changed active tab: %#v", msg.String(), active)
		}
	}

	rt := testRuntime(t)
	cfg := config.DefaultConfig("codux-test")
	model := NewModel(rt, cfg, state.State{
		Version: state.Version,
		Focus:   state.FocusCodex,
		Tabs: []state.Tab{
			{ID: "a", Title: "alpha", Column: "inbox", Status: state.StatusRunning},
		},
		ActiveTabID: "a",
	})

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlG})
	model = updated.(Model)
	if model.state.Focus != state.FocusNav {
		t.Fatalf("C-g should focus nav, got %s", model.state.Focus)
	}
}

func TestActiveOutputPreservesTerminalStyles(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("codux-test")
	model := NewModel(rt, cfg, state.State{
		Version: state.Version,
		Focus:   state.FocusCodex,
		Tabs: []state.Tab{
			{ID: "a", Title: "alpha", Column: "inbox", Status: state.StatusRunning},
		},
		ActiveTabID: "a",
	})
	screen := NewTerminalScreen(20, 3)
	screen.Write("\x1b[48;2;1;2;3m input \x1b[0m")
	model.screens["a"] = screen

	output := model.activeOutput()

	if !strings.Contains(output, "\x1b[48;2;1;2;3m input \x1b[m") {
		t.Fatalf("active output should preserve terminal styling:\n%q", output)
	}
	if stripped := ansi.Strip(output); !strings.Contains(stripped, " input ") {
		t.Fatalf("styled active output should strip to visible screen content:\nplain  %q\nstyled %q", screen.String(), stripped)
	}
}

func TestActiveOutputSuppressesColorOnlyStartupScreen(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("codux-test")
	model := NewModel(rt, cfg, state.State{
		Version: state.Version,
		Focus:   state.FocusCodex,
		Tabs: []state.Tab{
			{ID: "a", Title: "alpha", Column: "inbox", Status: state.StatusRunning},
		},
		ActiveTabID: "a",
	})
	screen := NewTerminalScreen(20, 3)
	screen.Write("\x1b]10;rgb:eded/efef/f1f1\x1b\\")
	screen.Write("\x1b]11;rgb:2828/3131/3838\x1b\\")
	model.screens["a"] = screen

	if output := model.activeOutput(); output != "" {
		t.Fatalf("color-only startup screen should not replace placeholder:\n%q", output)
	}
	view := model.View()
	if !strings.Contains(view, "Starting Codex") {
		t.Fatalf("view should keep startup loading state for color-only screen:\n%s", view)
	}
	if strings.Contains(view, "Codex PTY is starting...") {
		t.Fatalf("view should not render old startup text:\n%s", view)
	}
}

func TestLoadingTickAnimatesStartupView(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("codux-test")
	model := NewModel(rt, cfg, state.State{
		Version: state.Version,
		Focus:   state.FocusCodex,
		Tabs: []state.Tab{
			{ID: "a", Title: "alpha", Column: "inbox", Status: state.StatusStarting},
		},
		ActiveTabID: "a",
	})

	before := model.loadingLabel()
	updated, cmd := model.Update(loadingTick{})
	model = updated.(Model)
	after := model.loadingLabel()

	if before == after {
		t.Fatalf("loading label should animate, before=%q after=%q", before, after)
	}
	if cmd == nil {
		t.Fatal("loading tick should continue while Codex is starting")
	}
}

func TestNavHeightAnimatesOnFocusChanges(t *testing.T) {
	rt := testRuntime(t)
	cfg := config.DefaultConfig("codux-test")
	cfg.CodexCommand = "cat"
	model := NewModel(rt, cfg, state.State{
		Version: state.Version,
		Focus:   state.FocusNav,
		Tabs: []state.Tab{
			{ID: "a", Title: "alpha", Column: "inbox", Status: state.StatusRunning},
		},
		ActiveTabID: "a",
	})
	defer func() {
		for _, pty := range model.ptys {
			pty.Kill()
		}
	}()

	expanded := model.navHeight
	if expanded <= 0 {
		t.Fatalf("expanded nav height = %d", expanded)
	}
	cmd := model.setFocus(state.FocusCodex)
	if cmd == nil {
		t.Fatal("expected collapse animation command")
	}
	for model.navHeight != 0 {
		model.stepNavAnimation()
	}
	if got := model.View(); strings.Contains(got, "INBOX") || !strings.Contains(got, "CODUX  C-g focus nav  C-q quit") {
		t.Fatalf("codex focus should collapse nav pane:\n%s", got)
	}

	cmd = model.setFocus(state.FocusNav)
	if cmd == nil {
		t.Fatal("expected expand animation command")
	}
	model.stepNavAnimation()
	if model.navHeight <= 0 || model.navHeight >= expanded {
		t.Fatalf("expected partial expansion, height=%d expanded=%d", model.navHeight, expanded)
	}
	for model.navHeight != model.targetNavHeight() {
		model.stepNavAnimation()
	}
	got := model.View()
	if !strings.Contains(got, "CODUX  ←↑↓→ select") || !strings.Contains(got, "S-←/→ move") {
		t.Fatalf("nav focus should expand nav while keeping codex visible:\n%s", got)
	}
}

func testRuntime(t *testing.T) config.Runtime {
	t.Helper()
	dir := t.TempDir()
	return config.Runtime{
		Workdir:    dir,
		Dir:        dir,
		ConfigPath: filepath.Join(dir, "config.toml"),
		StatePath:  filepath.Join(dir, "state.json"),
		SocketPath: filepath.Join(dir, "codux.sock"),
	}
}
