package integration_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/edwmurph/codux/internal/state"
	"github.com/edwmurph/codux/internal/tui"
)

const (
	navToolbar            = "CODUX  ←↑↓→ select  S-←/→ move  Enter codex  n new  c close  C-q quit"
	collapsedCodexToolbar = "CODUX  C-g focus nav  C-q quit"
)

func TestAttachedDashboardKeyboardAndRenderingE2E(t *testing.T) {
	if os.Getenv("CODUX_RUN_INTEGRATION") != "1" {
		t.Skip("set CODUX_RUN_INTEGRATION=1 to run live tmux integration tests")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required")
	}

	root := repoRoot(t)
	tmp := t.TempDir()
	runID := "codux-e2e-" + fmt.Sprintf("%d", time.Now().UnixNano())
	bin := filepath.Join(tmp, "codux")
	build := exec.Command("go", "build", "-o", bin, "./cmd/codux")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	realTmux, _ := exec.LookPath("tmux")
	wrapperDir := filepath.Join(tmp, "bin")
	if err := os.Mkdir(wrapperDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tmuxWrapper := filepath.Join(wrapperDir, "tmux")
	if err := os.WriteFile(tmuxWrapper, []byte("#!/bin/sh\nexec "+shellQuote(realTmux)+" -f /dev/null -L "+shellQuote(runID)+" \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	runtimeDir := filepath.Join(tmp, "codux-home")
	workdir := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workdir, 0o700); err != nil {
		t.Fatal(err)
	}
	startupMarker := filepath.Join(tmp, "fake-codex-color-only")
	fakeCodex := filepath.Join(tmp, "fake-codex.sh")
	if err := os.WriteFile(fakeCodex, []byte(
		"#!/bin/sh\n"+
			"startup_delay=${STARTUP_DELAY:-1.2}\n"+
			"printf '\\033]2;Fake Codex Ready\\007'\n"+
			"stty raw -echo\n"+
			"printf '\\033]10;?\\033\\\\'\n"+
			"printf '\\033]11;?\\033\\\\'\n"+
			"dd bs=1 count=50 >/dev/null 2>/dev/null\n"+
			"stty sane\n"+
			"if [ -n \"${STARTUP_MARKER:-}\" ]; then : > \"$STARTUP_MARKER\"; fi\n"+
			"sleep \"$startup_delay\"\n"+
			"printf '\\033[?1049h\\033[2J\\033[H'\n"+
			"printf '╭──────────────────────────────────────────────────────────╮\\n'\n"+
			"printf '│ >_ OpenAI Codex (v0.fake.0)                               │\\n'\n"+
			"printf '│                                                          │\\n'\n"+
			"printf '│ model:     gpt-5.5 xhigh   /model to change              │\\n'\n"+
			"printf '│ directory: ~/code/personal/codux/.worktrees/single-pane… │\\n'\n"+
			"printf '╰──────────────────────────────────────────────────────────╯\\n'\n"+
			"printf '\\n\\033[48;2;40;40;49m› Summarize recent commits                         \\033[0m\\n'\n"+
			"printf '\\n  gpt-5.5 xhigh · ~/code/personal/codux/.worktrees/single-pane-tui-dashboard · Context 100%% left\\n'\n"+
			"i=0; while [ \"$i\" -lt 220 ]; do printf 'x'; i=$((i + 1)); done; printf '\\n'\n"+
			"printf '\\033[20;8Hready'\n"+
			"trap 'exit 0' HUP INT TERM\n"+
			"while IFS= read -r line; do\n"+
			"  printf '\\033]2;Fake Codex Working\\007'\n"+
			"  printf '\\033[2J\\033[H'\n"+
			"  printf '╭──────────────────────────────────────────────────────────╮\\n'\n"+
			"  printf '│ >_ OpenAI Codex (v0.fake.0)                               │\\n'\n"+
			"  printf '│ model:     gpt-5.5 xhigh   /model to change              │\\n'\n"+
			"  printf '╰──────────────────────────────────────────────────────────╯\\n'\n"+
			"  i=0; while [ \"$i\" -lt 220 ]; do printf 'y'; i=$((i + 1)); done; printf '\\n'\n"+
			"  printf 'received:%s\\n' \"$line\"\n"+
			"  printf '\\033[10;5Hprompt'\n"+
			"done\n"+
			"while :; do sleep 1; done\n",
	), 0o700); err != nil {
		t.Fatal(err)
	}
	configText := fmt.Sprintf("tmux_session = %q\ncodex_command = %q\n", runID, fakeCodex)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"CODUX_HOME="+runtimeDir,
		"CODUX_WORKDIR="+workdir,
		"CODUX_EXECUTABLE="+bin,
		"STARTUP_DELAY=1.2",
		"STARTUP_MARKER="+startupMarker,
		"PATH="+wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TERM=xterm-256color",
	)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "quit", "--kill")
		cmd.Env = env
		_ = cmd.Run()
		_ = exec.Command("tmux", "-L", runID, "kill-server").Run()
	})

	tuiCommand := strings.Join([]string{
		"env",
		"CODUX_HOME=" + shellQuote(runtimeDir),
		"CODUX_WORKDIR=" + shellQuote(workdir),
		"CODUX_EXECUTABLE=" + shellQuote(bin),
		"PATH=" + shellQuote(wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH")),
		"TERM=xterm-256color",
		shellQuote(bin),
		"tui",
	}, " ")
	tmuxRun(t, env, "new-session", "-d", "-x", "160", "-y", "38", "-s", runID, "-c", workdir, tuiCommand)
	pane := runID + ":0.0"
	if !waitForBool(8*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(runtimeDir, "codux.sock"))
		return err == nil
	}) {
		t.Fatalf("timed out waiting for TUI socket; pane:\n%s\nlog:\n%s", paneInfo(t, env, pane), readLog(runtimeDir))
	}
	clientCmd := exec.Command(tmuxFromEnv(env), "attach-session", "-t", runID)
	clientCmd.Env = env
	clientPTY, err := pty.StartWithSize(clientCmd, &pty.Winsize{Cols: 160, Rows: 38})
	if err != nil {
		t.Fatalf("attach tmux client: %v", err)
	}
	t.Cleanup(func() {
		_ = clientPTY.Close()
		_ = clientCmd.Process.Kill()
		_ = clientCmd.Wait()
	})
	clientScreen := tui.NewTerminalScreen(160, 38)
	var clientMu sync.Mutex
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := clientPTY.Read(buf)
			if n > 0 {
				clientMu.Lock()
				clientScreen.Write(string(buf[:n]))
				clientMu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	clientOutput := func() string {
		clientMu.Lock()
		defer clientMu.Unlock()
		return clientScreen.String()
	}
	if panes := tmuxLines(t, env, "list-panes", "-t", runID+":", "-F", "#{pane_id}"); len(panes) != 1 {
		t.Fatalf("pane count = %d (%v), want 1", len(panes), panes)
	}

	timedStep(t, "initial render", func() {
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "CODUX") && strings.Contains(capture, "No Codex tabs open")
		})
		assertDashboardNotCorrupt(t, clientOutput(), true)
	})

	var firstID string
	timedStep(t, "keyboard n creates tab", func() {
		started := time.Now()
		tmuxRun(t, env, "send-keys", "-t", pane, "n")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Starting Codex") &&
				strings.Contains(capture, collapsedCodexToolbar) &&
				!strings.Contains(capture, "No Codex tabs open")
		})
		placeholderDuration := time.Since(started)
		t.Logf("dashboard_e2e metric=%q duration=%s", "new tab startup placeholder visible", placeholderDuration.Round(time.Millisecond))
		if placeholderDuration > 500*time.Millisecond {
			t.Fatalf("startup placeholder took too long: %s", placeholderDuration.Round(time.Millisecond))
		}
		st := waitState(t, env, bin, func(st state.State) bool {
			return len(st.Tabs) == 1 && st.Tabs[0].Status == state.StatusRunning && st.Focus == state.FocusCodex
		})
		firstID = st.Tabs[0].ID
		if !waitForBool(2*time.Second, func() bool {
			_, err := os.Stat(startupMarker)
			return err == nil
		}) {
			t.Fatalf("fake Codex never reached color-only startup point")
		}
		time.Sleep(150 * time.Millisecond)
		colorOnlyCapture := clientOutput()
		if strings.Contains(colorOnlyCapture, ">_ OpenAI Codex") {
			t.Fatalf("fake Codex rendered visible content before delayed startup completed:\n%s", colorOnlyCapture)
		}
		if !strings.Contains(colorOnlyCapture, "Starting Codex") {
			t.Fatalf("dashboard should keep startup loading state during color-only Codex output:\n%s", colorOnlyCapture)
		}
		if strings.Contains(colorOnlyCapture, "Codex PTY is starting...") {
			t.Fatalf("dashboard should not render old startup text:\n%s", colorOnlyCapture)
		}
		if !loadingLineIsCentered(colorOnlyCapture) {
			t.Fatalf("dashboard should center startup loading state:\n%s", colorOnlyCapture)
		}
		t.Logf("dashboard_e2e metric=%q duration=%s", "new tab color-only startup covered", time.Since(started).Round(time.Millisecond))
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, ">_ OpenAI Codex") &&
				!strings.Contains(capture, "No Codex tabs open") &&
				!strings.Contains(capture, "INBOX") &&
				strings.Contains(capture, collapsedCodexToolbar)
		})
		t.Logf("dashboard_e2e metric=%q duration=%s", "new tab first Codex content visible", time.Since(started).Round(time.Millisecond))
		waitForEscapedCapture(t, env, pane, func(capture string) bool {
			return strings.Contains(capture, "38;2;237;239;241") &&
				strings.Contains(capture, "48;2;40;49;56") &&
				strings.Contains(capture, "48;2;40;40;49") &&
				strings.Contains(capture, "Summarize recent commits")
		})
		assertDashboardNotCorrupt(t, clientOutput(), false)
		assertCodexBoxNotDrifted(t, clientOutput())
	})

	timedStep(t, "shift-right moves fresh tab from codex focus", func() {
		tmuxRun(t, env, "send-keys", "-t", pane, "S-Right")
		waitState(t, env, bin, func(st state.State) bool {
			tab := findTab(st, firstID)
			return tab != nil && tab.Column == "implement"
		})
		assertDashboardNotCorrupt(t, clientOutput(), false)
	})

	timedStep(t, "codex focus blocks C-c and C-d", func() {
		tmuxRun(t, env, "send-keys", "-t", pane, "C-c")
		tmuxRun(t, env, "send-keys", "-t", pane, "C-d")
		time.Sleep(250 * time.Millisecond)
		st := waitState(t, env, bin, func(st state.State) bool {
			tab := findTab(st, firstID)
			return tab != nil && tab.Status == state.StatusRunning && st.Focus == state.FocusCodex
		})
		if tab := findTab(st, firstID); tab == nil || tab.Status != state.StatusRunning {
			t.Fatalf("tab should still be running after blocked controls: %#v", st)
		}
		assertDashboardNotCorrupt(t, clientOutput(), false)
	})

	timedStep(t, "codex focus forwards input", func() {
		tmuxRun(t, env, "send-keys", "-t", pane, "hello", "Enter")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "received:hello")
		})
		assertDashboardNotCorrupt(t, clientOutput(), false)
	})

	timedStep(t, "C-g focuses nav", func() {
		tmuxRun(t, env, "send-keys", "-t", pane, "C-g")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusNav })
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, navToolbar) &&
				strings.Contains(capture, ">_ OpenAI Codex")
		})
		assertDashboardNotCorrupt(t, capture, false)
	})

	timedStep(t, "shift-left moves active tab in nav focus", func() {
		tmuxRun(t, env, "send-keys", "-t", pane, "S-Left")
		waitState(t, env, bin, func(st state.State) bool {
			tab := findTab(st, firstID)
			return tab != nil && tab.Column == "inbox"
		})
		assertDashboardNotCorrupt(t, clientOutput(), false)
	})

	timedStep(t, "rename modal saves title", func() {
		tmuxRun(t, env, "send-keys", "-t", pane, "r")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Rename active tab")
		})
		tmuxRun(t, env, "send-keys", "-t", pane, "C-u")
		tmuxRun(t, env, "send-keys", "-l", "-t", pane, "Renamed")
		tmuxRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			tab := findTab(st, firstID)
			return tab != nil && tab.Title == "Renamed"
		})
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Renamed") && !strings.Contains(capture, "Rename active tab")
		})
		assertDashboardNotCorrupt(t, capture, false)
	})

	timedStep(t, "help and sessions modals close", func() {
		tmuxRun(t, env, "send-keys", "-t", pane, "?")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Codux shortcuts")
		})
		tmuxRun(t, env, "send-keys", "-t", pane, "Escape")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return !strings.Contains(capture, "Codux shortcuts")
		})
		tmuxRun(t, env, "send-keys", "-t", pane, "s")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Other Codux sessions")
		})
		tmuxRun(t, env, "send-keys", "-t", pane, "Escape")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return !strings.Contains(capture, "Other Codux sessions")
		})
	})

	timedStep(t, "close confirmation cancels then closes", func() {
		tmuxRun(t, env, "send-keys", "-t", pane, "c")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Close Renamed?")
		})
		tmuxRun(t, env, "send-keys", "-t", pane, "n")
		waitState(t, env, bin, func(st state.State) bool { return len(st.Tabs) == 1 })
		tmuxRun(t, env, "send-keys", "-t", pane, "c")
		tmuxRun(t, env, "send-keys", "-t", pane, "y")
		waitState(t, env, bin, func(st state.State) bool { return len(st.Tabs) == 0 })
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No Codex tabs open")
		})
		assertDashboardNotCorrupt(t, clientOutput(), true)
	})
}

func timedStep(t *testing.T, name string, fn func()) {
	t.Helper()
	start := time.Now()
	fn()
	t.Logf("dashboard_e2e step=%q duration=%s", name, time.Since(start).Round(time.Millisecond))
}

func waitForCapture(t *testing.T, env []string, pane string, accept func(string) bool) string {
	t.Helper()
	var capture string
	if waitForBool(8*time.Second, func() bool {
		capture = capturePane(t, env, pane)
		return accept(capture)
	}) {
		return capture
	}
	t.Fatalf("timed out waiting for dashboard capture; pane:\n%s\ncapture:\n%s", paneInfo(t, env, pane), capture)
	return capture
}

func waitForEscapedCapture(t *testing.T, env []string, pane string, accept func(string) bool) string {
	t.Helper()
	var capture string
	if waitForBool(8*time.Second, func() bool {
		capture = capturePaneEscaped(t, env, pane)
		return accept(capture)
	}) {
		return capture
	}
	t.Fatalf("timed out waiting for escaped dashboard capture; pane:\n%s\ncapture:\n%q", paneInfo(t, env, pane), capture)
	return capture
}

func waitForOutput(t *testing.T, output func() string, accept func(string) bool) string {
	t.Helper()
	var capture string
	if waitForBool(8*time.Second, func() bool {
		capture = output()
		return accept(capture)
	}) {
		return capture
	}
	t.Fatalf("timed out waiting for dashboard output:\n%s", capture)
	return capture
}

func capturePane(t *testing.T, env []string, pane string) string {
	t.Helper()
	cmd := exec.Command(tmuxFromEnv(env), "capture-pane", "-p", "-a", "-t", pane)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out)
	}
	if !strings.Contains(string(out), "no alternate screen") {
		t.Fatalf("tmux capture-pane: %v\n%s", err, out)
	}
	cmd = exec.Command(tmuxFromEnv(env), "capture-pane", "-p", "-t", pane)
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tmux capture-pane: %v\n%s", err, out)
	}
	return string(out)
}

func capturePaneEscaped(t *testing.T, env []string, pane string) string {
	t.Helper()
	cmd := exec.Command(tmuxFromEnv(env), "capture-pane", "-p", "-e", "-t", pane)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out)
	}
	if !strings.Contains(string(out), "no alternate screen") {
		t.Fatalf("tmux capture-pane -e: %v\n%s", err, out)
	}
	cmd = exec.Command(tmuxFromEnv(env), "capture-pane", "-p", "-e", "-a", "-t", pane)
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tmux capture-pane -e: %v\n%s", err, out)
	}
	return string(out)
}

func paneInfo(t *testing.T, env []string, pane string) string {
	t.Helper()
	cmd := exec.Command(tmuxFromEnv(env), "display-message", "-p", "-t", pane, "#{pane_current_command}\t#{pane_dead}\t#{pane_start_command}")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("tmux display-message: %v\n%s", err, out)
	}
	return string(out)
}

func readLog(runtimeDir string) string {
	data, _ := os.ReadFile(filepath.Join(runtimeDir, "codux.log"))
	return string(data)
}

func tmuxRun(t *testing.T, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command(tmuxFromEnv(env), args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %v: %v\n%s", args, err, out)
	}
}

func assertDashboardNotCorrupt(t *testing.T, capture string, empty bool) {
	t.Helper()
	for _, forbidden := range []string{"\x1b", "]10;", "]11;", "^[[", "rgb:"} {
		if strings.Contains(capture, forbidden) {
			t.Fatalf("dashboard leaked terminal control content %q:\n%s", forbidden, capture)
		}
	}
	for _, expected := range []string{"╭─", "─╮", "╰─", "─╯", "│"} {
		if !strings.Contains(capture, expected) {
			t.Fatalf("dashboard missing thick frame fragment %q:\n%s", expected, capture)
		}
	}
	for _, forbidden := range []string{"├─", "─┤"} {
		if strings.Contains(capture, forbidden) {
			t.Fatalf("dashboard join should not use sideways T fragment %q:\n%s", forbidden, capture)
		}
	}
	if count := strings.Count(capture, "CODUX"); count > 1 || (empty && count != 1) {
		t.Fatalf("dashboard should render at most one CODUX frame label, got %d:\n%s", count, capture)
	}
	if count := strings.Count(capture, "C-q quit"); count > 2 {
		t.Fatalf("dashboard rendered duplicate footer/header labels, got %d:\n%s", count, capture)
	}
	if !empty && strings.Contains(capture, "No Codex tabs open") {
		t.Fatalf("dashboard kept empty state after tab was created:\n%s", capture)
	}
}

func loadingLineIsCentered(capture string) bool {
	for _, line := range strings.Split(capture, "\n") {
		index := strings.Index(line, "Starting Codex")
		if index >= 0 {
			return index > 40
		}
	}
	return false
}

func assertCodexBoxNotDrifted(t *testing.T, capture string) {
	t.Helper()
	for _, expected := range []string{
		"│ │ >_ OpenAI Codex (v0.fake.0)",
		"│ >_ OpenAI Codex (v0.fake.0)",
		"│ model:     gpt-5.5 xhigh",
		"╰──────────────────────────────────────────────────────────╯",
		"› Summarize recent commits",
	} {
		if !strings.Contains(capture, expected) {
			t.Fatalf("codex box drifted; missing %q:\n%s", expected, capture)
		}
	}
	for _, broken := range []string{
		"││ >_ OpenAI Codex",
		"│                                                            │ >_ OpenAI Codex",
		"\n│ xhigh   /model to change",
	} {
		if strings.Contains(capture, broken) {
			t.Fatalf("codex box drifted; found broken fragment %q:\n%s", broken, capture)
		}
	}
}

func findTab(st state.State, id string) *state.Tab {
	for index := range st.Tabs {
		if st.Tabs[index].ID == id {
			return &st.Tabs[index]
		}
	}
	return nil
}
