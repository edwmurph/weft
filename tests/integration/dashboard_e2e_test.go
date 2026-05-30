package integration_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/tui"
)

const (
	collapsedCodexToolbar = "WEFT  C-b command center  C-c interrupt/close"
)

func TestFreshDashboardNewAgentFallsBackWhenShellMissing(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir := filepath.Join(tmp, "weft-home")
	workdir := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workdir, 0o700); err != nil {
		t.Fatal(err)
	}
	fakeCodex := writeFakeCodex(t, tmp, "fake-codex.sh")
	configText := fmt.Sprintf("codex_command = %q\n", fakeCodex)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"WEFT_HOME="+runtimeDir,
		"WEFT_WORKDIR="+workdir,
		"WEFT_EXECUTABLE="+bin,
		"SHELL=/missing/zsh",
		"PATH=/usr/bin:/bin",
		"TERM=xterm-256color",
	)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	clientCmd := exec.Command(bin)
	clientCmd.Env = env
	clientCmd.Dir = workdir
	clientPTY, err := pty.StartWithSize(clientCmd, &pty.Winsize{Cols: 100, Rows: 32})
	if err != nil {
		t.Fatalf("start Weft client: %v", err)
	}
	clientDone := make(chan struct{})
	go func() {
		_ = clientCmd.Wait()
		close(clientDone)
	}()
	t.Cleanup(func() {
		_ = clientPTY.Close()
		if !clientExited(clientDone) && clientCmd.Process != nil {
			_ = clientCmd.Process.Kill()
		}
		<-clientDone
	})

	clientScreen := tui.NewTerminalScreen(100, 32)
	var clientMu sync.Mutex
	var clientRaw bytes.Buffer
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := clientPTY.Read(buf)
			if n > 0 {
				clientMu.Lock()
				clientScreen.Write(string(buf[:n]))
				clientRaw.Write(buf[:n])
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
	pane := "direct-client-missing-shell"
	registerDirectClient(clientCmd, clientPTY, clientScreen, &clientRaw, &clientMu, clientDone)
	waitFor(t, "supervisor socket", 8*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(runtimeDir, "weft.sock"))
		return err == nil
	})
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Workdirs") &&
			strings.Contains(capture, "Agents")
	})

	directRun(t, env, "send-keys", "-t", pane, "n")
	st := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 && st.Agents[0].Status != state.StatusStarting
	})
	if st.Agents[0].Status != state.StatusRunning {
		t.Fatalf("agent should start even when SHELL is invalid, status=%s title=%q\nscreen:\n%s", st.Agents[0].Status, st.Agents[0].CodexTitle, clientOutput())
	}
	capture := waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Fake Codex Ready") || strings.Contains(capture, "Starting Codex")
	})
	if strings.Contains(capture, "No Codex agent open") || strings.Contains(capture, "fork/exec") {
		t.Fatalf("new agent rendered stale empty/error state:\n%s", capture)
	}
}

func TestAttachedDashboardKeyboardAndRenderingE2E(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()

	runtimeDir := filepath.Join(tmp, "weft-home")
	workdir := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workdir, 0o700); err != nil {
		t.Fatal(err)
	}
	startupMarker := filepath.Join(tmp, "fake-codex-color-only")
	titleHookPayload := filepath.Join(tmp, "title-hook-payload.json")
	titleHook := filepath.Join(tmp, "title-hook.sh")
	if err := os.WriteFile(titleHook, []byte(
		"#!/bin/sh\n"+
			"cat > \"$TITLE_HOOK_PAYLOAD\"\n"+
			"printf 'Auto hook title\\n'\n",
	), 0o700); err != nil {
		t.Fatal(err)
	}
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
			"printf '│ directory: ~/code/personal/weft/.worktrees/single-pane… │\\n'\n"+
			"printf '╰──────────────────────────────────────────────────────────╯\\n'\n"+
			"printf '\\n\\033[48;2;40;40;49m› Summarize recent commits                         \\033[0m\\n'\n"+
			"printf '\\n  gpt-5.5 xhigh · ~/code/personal/weft/.worktrees/single-pane-tui-dashboard · Context 100%% left\\n'\n"+
			"i=0; while [ \"$i\" -lt 220 ]; do printf 'x'; i=$((i + 1)); done; printf '\\n'\n"+
			"printf '\\033[20;8Hready'\n"+
			"trap 'exit 0' HUP TERM\n"+
			"trap 'printf \"\\033]2;Fake Codex Ready\\007\"' INT\n"+
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
			"  sleep 1\n"+
			"  printf '\\033]2;Fake Codex Ready\\007'\n"+
			"done\n"+
			"while :; do sleep 1; done\n",
	), 0o700); err != nil {
		t.Fatal(err)
	}
	titleHookCommand := "TITLE_HOOK_PAYLOAD=" + shellQuote(titleHookPayload) + " " + shellQuote(titleHook)
	configText := fmt.Sprintf("codex_command = %q\ntitle_template = %q\ntitle_hook_command = %q\n", fakeCodex, "{title}", titleHookCommand)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"WEFT_HOME="+runtimeDir,
		"WEFT_WORKDIR="+workdir,
		"WEFT_EXECUTABLE="+bin,
		"STARTUP_DELAY=1.2",
		"STARTUP_MARKER="+startupMarker,
		"PATH="+os.Getenv("PATH"),
		"TERM=xterm-256color",
	)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	clientCmd := exec.Command(bin)
	clientCmd.Env = env
	clientCmd.Dir = workdir
	clientPTY, err := pty.StartWithSize(clientCmd, &pty.Winsize{Cols: 160, Rows: 38})
	if err != nil {
		t.Fatalf("start Weft client: %v", err)
	}
	clientDone := make(chan struct{})
	go func() {
		_ = clientCmd.Wait()
		close(clientDone)
	}()
	pane := "direct-client"
	if !waitForBool(8*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(runtimeDir, "weft.sock"))
		return err == nil
	}) {
		t.Fatalf("timed out waiting for supervisor socket; client:\n%s\nlog:\n%s", paneInfo(t, env, pane), readLog(runtimeDir))
	}
	t.Cleanup(func() {
		_ = clientPTY.Close()
		if !clientExited(clientDone) && clientCmd.Process != nil {
			_ = clientCmd.Process.Kill()
		}
		<-clientDone
	})
	clientScreen := tui.NewTerminalScreen(160, 38)
	var clientMu sync.Mutex
	var clientRaw bytes.Buffer
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := clientPTY.Read(buf)
			if n > 0 {
				clientMu.Lock()
				clientScreen.Write(string(buf[:n]))
				clientRaw.Write(buf[:n])
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
	registerDirectClient(clientCmd, clientPTY, clientScreen, &clientRaw, &clientMu, clientDone)
	if panes := directLines(t, env, "list-panes", "-t", pane, "-F", "#{pane_id}"); len(panes) != 1 {
		t.Fatalf("pane count = %d (%v), want 1", len(panes), panes)
	}

	timedStep(t, "initial render", func() {
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Agents") &&
				strings.Contains(capture, "No Codex agent open")
		})
		assertDashboardNotCorrupt(t, clientOutput(), true)
	})

	var firstID string
	timedStep(t, "keyboard n creates agent", func() {
		started := time.Now()
		directRun(t, env, "send-keys", "-t", pane, "n")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Starting Codex") &&
				strings.Contains(capture, collapsedCodexToolbar) &&
				!strings.Contains(capture, "No Codex agent open")
		})
		placeholderDuration := time.Since(started)
		t.Logf("dashboard_e2e metric=%q duration=%s", "new agent startup placeholder visible", placeholderDuration.Round(time.Millisecond))
		if placeholderDuration > 500*time.Millisecond {
			t.Fatalf("startup placeholder took too long: %s", placeholderDuration.Round(time.Millisecond))
		}
		st := waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 1 && st.Agents[0].Status == state.StatusRunning && st.Focus == state.FocusCodex
		})
		firstID = st.Agents[0].ID
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
		t.Logf("dashboard_e2e metric=%q duration=%s", "new agent color-only startup covered", time.Since(started).Round(time.Millisecond))
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, ">_ OpenAI Codex") &&
				!strings.Contains(capture, "No Codex agent open") &&
				!strings.Contains(capture, "Workdirs") &&
				!strings.Contains(capture, "Agents") &&
				strings.Contains(capture, collapsedCodexToolbar)
		})
		t.Logf("dashboard_e2e metric=%q duration=%s", "new agent first Codex content visible", time.Since(started).Round(time.Millisecond))
		waitForEscapedCapture(t, env, pane, func(capture string) bool {
			return strings.Contains(capture, "38;2;237;239;241") &&
				strings.Contains(capture, "48;2;40;49;56") &&
				strings.Contains(capture, "38;2;0;0;0") &&
				strings.Contains(capture, "48;2;255;255;255") &&
				strings.Contains(capture, "48;2;40;40;49") &&
				strings.Contains(capture, "Summarize recent commits")
		})
		assertDashboardNotCorrupt(t, clientOutput(), false)
		assertCodexBoxNotDrifted(t, clientOutput())
	})

	timedStep(t, "nav shortcut keys stay inside codex focus", func() {
		directRun(t, env, "send-keys", "-l", "-t", pane, "lj")
		time.Sleep(250 * time.Millisecond)
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && agent.FolderID == "" && st.Focus == state.FocusCodex
		})
		assertDashboardNotCorrupt(t, clientOutput(), false)
	})

	timedStep(t, "codex focus forwards dashboard shortcut keys", func() {
		directRun(t, env, "send-keys", "-l", "-t", pane, "s?n")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "received:ljs?n") &&
				!strings.Contains(capture, "Weft shortcuts") &&
				!strings.Contains(capture, "Other Weft sessions")
		})
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && agent.AutoTitle == "Auto hook title" && agent.AutoTitleAttempted
		})
		assertDashboardNotCorrupt(t, clientOutput(), false)
	})

	timedStep(t, "C-b opens command center", func() {
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusFolders && st.NavOpen })
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Agents") &&
				strings.Contains(capture, ">_ OpenAI Codex")
		})
		assertDashboardNotCorrupt(t, capture, false)
	})

	timedStep(t, "auto title variable reveals title generated from first message", func() {
		directRun(t, env, "send-keys", "-t", pane, "r")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Rename agent") &&
				strings.Contains(capture, "Variables") &&
				strings.Contains(capture, "{auto}")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-u")
		directRun(t, env, "send-keys", "-l", "-t", pane, "{auto}")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && agent.Title == "{auto}" && agent.AutoTitle == "Auto hook title"
		})
		payload, err := os.ReadFile(titleHookPayload)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(payload), `"first_message":"ljs?n"`) {
			t.Fatalf("title hook payload missing first message:\n%s", payload)
		}
	})

	timedStep(t, "group prompt and move agent update structure", func() {
		directRun(t, env, "send-keys", "-t", pane, "g")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Create group")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, "release")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return folderByPath(st, "release") != nil
		})
		directRun(t, env, "send-keys", "-t", pane, "m")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Move agent")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, "release")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			folder := folderForAgent(st, agent)
			return agent != nil && folder != nil && folder.Path == "release"
		})
		assertDashboardNotCorrupt(t, clientOutput(), false)
	})

	timedStep(t, "rename modal saves status template", func() {
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && strings.Contains(agent.CodexTitle, "Ready")
		})
		directRun(t, env, "send-keys", "-t", pane, "r")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Rename agent") &&
				strings.Contains(capture, "Preview")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-u")
		directRun(t, env, "send-keys", "-l", "-t", pane, "Codex {status}")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && agent.Title == "Codex {status}"
		})
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Codex ready") && !strings.Contains(capture, "Rename agent")
		})
		assertDashboardNotCorrupt(t, capture, false)
	})

	timedStep(t, "status template follows Codex activity", func() {
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusCodex })
		directRun(t, env, "send-keys", "-l", "-t", pane, "status check")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && strings.Contains(agent.CodexTitle, "Working")
		})
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Codex working")
		})
		assertDashboardNotCorrupt(t, capture, false)
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && strings.Contains(agent.CodexTitle, "Ready")
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Codex ready")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusFolders && st.NavOpen })
	})

	timedStep(t, "help modal closes", func() {
		directRun(t, env, "send-keys", "-t", pane, "?")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Weft shortcuts")
		})
		directRun(t, env, "send-keys", "-t", pane, "Escape")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return !strings.Contains(capture, "Weft shortcuts")
		})
	})

	timedStep(t, "close confirmation cancels then closes", func() {
		directRun(t, env, "send-keys", "-t", pane, "d")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete agent Codex ready?")
		})
		directRun(t, env, "send-keys", "-t", pane, "n")
		waitState(t, env, bin, func(st state.State) bool { return len(st.Agents) == 1 })
		directRun(t, env, "send-keys", "-t", pane, "d")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete agent Codex ready?")
		})
		directRun(t, env, "send-keys", "-t", pane, "y")
		waitState(t, env, bin, func(st state.State) bool { return len(st.Agents) == 0 })
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No Codex agent open")
		})
		assertDashboardNotCorrupt(t, clientOutput(), true)
		directRun(t, env, "send-keys", "-t", pane, "d")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete group release?")
		})
		directRun(t, env, "send-keys", "-t", pane, "y")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 0 && len(st.Folders) == 0 && st.SelectedWorkdirID != ""
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No agents")
		})
	})

	timedStep(t, "C-c interrupts working codex before ready codex closes weft", func() {
		directRun(t, env, "send-keys", "-t", pane, "n")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 1 &&
				st.Focus == state.FocusCodex &&
				strings.Contains(st.Agents[0].CodexTitle, "Ready")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, "interrupt me")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 1 &&
				st.Focus == state.FocusCodex &&
				st.Agents[0].Status == state.StatusRunning &&
				strings.Contains(st.Agents[0].CodexTitle, "Working")
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Fake Codex Working") || strings.Contains(capture, "Codex working")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-c")
		time.Sleep(250 * time.Millisecond)
		attached := directLines(t, env, "display-message", "-p", "-t", pane, "#{session_attached}")
		if len(attached) != 1 || attached[0] != "1" {
			t.Fatalf("C-c should stay with Codex while CODEX is running: %v", attached)
		}
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 1 &&
				st.Focus == state.FocusCodex &&
				st.Agents[0].Status == state.StatusRunning &&
				strings.Contains(st.Agents[0].CodexTitle, "Ready")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-c")
		if !waitForBool(2*time.Second, func() bool {
			attached := directLines(t, env, "display-message", "-p", "-t", pane, "#{session_attached}")
			return len(attached) == 1 && attached[0] == "0"
		}) {
			t.Fatalf("C-c did not detach Weft clients")
		}
		if panes := directLines(t, env, "list-panes", "-t", pane, "-F", "#{pane_id}"); len(panes) != 1 {
			t.Fatalf("pane count after C-c close = %d (%v), want 1", len(panes), panes)
		}
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 1 &&
				st.Focus == state.FocusCodex &&
				st.Agents[0].Status == state.StatusRunning &&
				strings.Contains(st.Agents[0].CodexTitle, "Ready")
		})
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

type directClientHarness struct {
	cmd    *exec.Cmd
	pty    *os.File
	screen *tui.TerminalScreen
	raw    *bytes.Buffer
	mu     *sync.Mutex
	done   <-chan struct{}
}

var directClient directClientHarness

func registerDirectClient(cmd *exec.Cmd, ptyFile *os.File, screen *tui.TerminalScreen, raw *bytes.Buffer, mu *sync.Mutex, done <-chan struct{}) {
	directClient = directClientHarness{
		cmd:    cmd,
		pty:    ptyFile,
		screen: screen,
		raw:    raw,
		mu:     mu,
		done:   done,
	}
}

func capturePane(t *testing.T, env []string, pane string) string {
	t.Helper()
	directClient.mu.Lock()
	defer directClient.mu.Unlock()
	return directClient.screen.String()
}

func capturePaneEscaped(t *testing.T, env []string, pane string) string {
	t.Helper()
	directClient.mu.Lock()
	defer directClient.mu.Unlock()
	return directClient.raw.String()
}

func paneInfo(t *testing.T, env []string, pane string) string {
	t.Helper()
	if directClient.cmd == nil {
		return "direct client not registered"
	}
	status := "running"
	if clientExited(directClient.done) {
		status = "exited"
	}
	return fmt.Sprintf("pid=%d status=%s args=%v", directClient.cmd.Process.Pid, status, directClient.cmd.Args)
}

func readLog(runtimeDir string) string {
	data, _ := os.ReadFile(filepath.Join(runtimeDir, "weft.log"))
	return string(data)
}

func directRun(t *testing.T, env []string, args ...string) {
	t.Helper()
	if len(args) == 0 || args[0] != "send-keys" {
		t.Fatalf("unsupported direct client command: %v", args)
	}
	literal := false
	keys := args[1:]
	if len(keys) > 0 && keys[0] == "-l" {
		literal = true
		keys = keys[1:]
	}
	if len(keys) >= 2 && keys[0] == "-t" {
		keys = keys[2:]
	}
	if len(keys) != 1 {
		t.Fatalf("unsupported send-keys args: %v", args)
	}
	if literal {
		writeClientInput(t, keys[0])
		return
	}
	switch keys[0] {
	case "Enter":
		writeClientInput(t, "\r")
	case "C-b":
		writeClientInput(t, "\x02")
	case "C-c":
		writeClientInput(t, "\x03")
	case "C-u":
		writeClientInput(t, "\x15")
	case "Escape":
		writeClientInput(t, "\x1b")
	default:
		writeClientInput(t, keys[0])
	}
}

func directLines(t *testing.T, env []string, args ...string) []string {
	t.Helper()
	if len(args) > 0 && args[0] == "list-panes" {
		return []string{"direct-client"}
	}
	if len(args) > 0 && args[0] == "display-message" {
		if clientExited(directClient.done) {
			return []string{"0"}
		}
		return []string{"1"}
	}
	t.Fatalf("unsupported direct client query: %v", args)
	return nil
}

func writeClientInput(t *testing.T, value string) {
	t.Helper()
	if directClient.pty == nil {
		t.Fatal("direct client PTY is not registered")
	}
	if _, err := directClient.pty.Write([]byte(value)); err != nil {
		t.Fatalf("write client input %q: %v", value, err)
	}
}

func clientExited(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
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
	if count := strings.Count(capture, "WEFT"); count > 1 {
		t.Fatalf("dashboard should render at most one WEFT frame label, got %d:\n%s", count, capture)
	}
	if count := strings.Count(capture, "C-c interrupt/close"); count > 1 {
		t.Fatalf("dashboard rendered duplicate footer/header labels, got %d:\n%s", count, capture)
	}
	if !empty && strings.Contains(capture, "No Codex agent open") {
		t.Fatalf("dashboard kept empty state after agent was created:\n%s", capture)
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

func findAgent(st state.State, id string) *state.Agent {
	for index := range st.Agents {
		if st.Agents[index].ID == id {
			return &st.Agents[index]
		}
	}
	return nil
}

func folderForAgent(st state.State, agent *state.Agent) *state.Folder {
	if agent == nil {
		return nil
	}
	for index := range st.Folders {
		if st.Folders[index].ID == agent.FolderID {
			return &st.Folders[index]
		}
	}
	return nil
}

func folderByPath(st state.State, path string) *state.Folder {
	for index := range st.Folders {
		if st.Folders[index].Path == path {
			return &st.Folders[index]
		}
	}
	return nil
}
