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
	collapsedCodexToolbarPrefix    = "WEFT  C-b dashboard  C-c "
	collapsedCodexInterruptToolbar = "WEFT  C-b dashboard  C-c interrupt"
	collapsedCodexQuitToolbar      = "WEFT  C-b dashboard  C-c quit"
	keyboardProtocolSetup          = "\x1b[>4;2m\x1b[>29u"
)

func TestFreshDashboardNewAgentFallsBackWhenShellMissing(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir := filepath.Join(tmp, "weft-home")
	workspace := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	fakeCodex := writeFakeCodex(t, tmp, "fake-codex.sh")
	configText := fmt.Sprintf("codex_command = %q\n", fakeCodex)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"WEFT_HOME="+runtimeDir,
		"WEFT_WORKSPACE="+workspace,
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
	clientCmd.Dir = workspace
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
		return strings.Contains(capture, "Add this workspace to Weft?") &&
			strings.Contains(capture, "Y yes")
	})
	directRun(t, env, "send-keys", "-t", pane, "y")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Workspaces") &&
			strings.Contains(capture, "Agents")
	})

	directRun(t, env, "send-keys", "-t", pane, "n")
	st := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 && st.Agents[0].Status != state.StatusStarting
	})
	if st.Agents[0].Status != state.StatusRunning {
		t.Fatalf("agent should start even when SHELL is invalid, status=%s title=%q\nscreen:\n%s", st.Agents[0].Status, st.Agents[0].CodexTitle, clientOutput())
	}
	waitForEscapedCapture(t, env, pane, func(capture string) bool {
		return strings.Contains(capture, keyboardProtocolSetup)
	})
	assertClientDoesNotEnableMouseTracking(t, capturePaneEscaped(t, env, pane))
	directRun(t, env, "send-keys", "-l", "-t", pane, "probe")
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	capture := waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "echo:probe")
	})
	if strings.Contains(capture, "No Codex agent open") || strings.Contains(capture, "fork/exec") {
		t.Fatalf("new agent rendered stale empty/error state:\n%s", capture)
	}
}

func TestStaleWorkspaceCanBeSelectedAndRemovedE2E(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	fakeCodex := writeFakeCodex(t, tmp, "fake-codex.sh")
	runtimeDir, workspace := createRuntime(t, tmp, fakeCodex)
	staleWorkspace := filepath.Join(tmp, "old-worktree")
	if err := os.Mkdir(staleWorkspace, 0o700); err != nil {
		t.Fatal(err)
	}
	env := baseIntegrationEnv(runtimeDir, workspace, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	runWeft(t, env, bin, "workspace", "add", workspace)
	runWeft(t, env, bin, "workspace", "add", staleWorkspace)
	if err := os.Remove(staleWorkspace); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Workspaces) == 2 && state.WorkspaceByPath(st, staleWorkspace) != nil
	})
	stale := state.WorkspaceByPath(st, staleWorkspace)
	if stale == nil {
		t.Fatalf("stale workspace missing before client attach: %#v", st.Workspaces)
	}

	pane := "stale-workspace-client"
	clientOutput, _ := startDirectDashboardClient(t, env, bin, workspace, pane, 120, 32)
	waitState(t, env, bin, func(st state.State) bool {
		selected := state.WorkspaceByID(st, st.SelectedWorkspaceID)
		return selected != nil && selected.Path == workspace
	})
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "path missing; press d to remove")
	})

	directRun(t, env, "send-keys", "-t", pane, "Left")
	waitState(t, env, bin, func(st state.State) bool {
		return st.Focus == state.FocusWorkspaces
	})
	directRun(t, env, "send-keys", "-t", pane, "j")
	waitState(t, env, bin, func(st state.State) bool {
		return st.Focus == state.FocusWorkspaces && st.SelectedWorkspaceID == stale.ID
	})
	time.Sleep(300 * time.Millisecond)
	st = waitState(t, env, bin, func(st state.State) bool {
		return st.Focus == state.FocusWorkspaces && st.SelectedWorkspaceID == stale.ID
	})
	if st.SelectedWorkspaceID != stale.ID {
		t.Fatalf("stale workspace selection bounced back: %#v", st)
	}

	directRun(t, env, "send-keys", "-t", pane, "d")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Delete workspace")
	})
	directRun(t, env, "send-keys", "-t", pane, "y")
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Workspaces) == 1 &&
			state.WorkspaceByPath(st, staleWorkspace) == nil &&
			state.WorkspaceByPath(st, workspace) != nil
	})
}

func TestBottomShipitGroupAgentCanBeReachedE2E(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir, workspace := createRuntime(t, tmp, writeFakeCodex(t, tmp, "fake-codex.sh"))
	env := baseIntegrationEnv(runtimeDir, workspace, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	runWeft(t, env, bin, "--no-attach")
	runWeft(t, env, bin, "workspace", "add", workspace)

	pane := "bottom-shipit-client"
	clientOutput, clientDone := startDirectDashboardClient(t, env, bin, workspace, pane, 120, 11)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Workspaces") && strings.Contains(capture, "Agents")
	})
	for _, name := range []string{"alpha", "beta", "gamma", "delta", "shipit"} {
		directRun(t, env, "send-keys", "-t", pane, "g")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Flat and unique in this workspace.")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, name)
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "> "+name)
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return groupByPath(st, name) != nil
		})
	}
	directRun(t, env, "send-keys", "-t", pane, "n")
	waitState(t, env, bin, func(st state.State) bool {
		active := state.ActiveAgent(st)
		return active != nil &&
			active.GroupID == "" &&
			active.Status == state.StatusRunning
	})
	runWeft(t, env, bin, "rename", "Ship Agent")
	waitState(t, env, bin, func(st state.State) bool {
		active := state.ActiveAgent(st)
		return active != nil && active.Title == "Ship Agent"
	})
	directRun(t, env, "send-keys", "-t", pane, "C-b")
	waitState(t, env, bin, func(st state.State) bool {
		return st.Focus == state.FocusAgents && st.NavOpen
	})
	directRun(t, env, "send-keys", "-t", pane, "m")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Top-level agent") &&
			strings.Contains(capture, "Blank makes the agent top-level.")
	})
	directRun(t, env, "send-keys", "-t", pane, "C-u")
	directRun(t, env, "send-keys", "-l", "-t", pane, "shipit")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "> shipit")
	})
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitState(t, env, bin, func(st state.State) bool {
		group := groupByPath(st, "shipit")
		active := state.ActiveAgent(st)
		return group != nil &&
			active != nil &&
			active.GroupID == group.ID
	})

	directRun(t, env, "send-keys", "-t", pane, "k")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "shipit") && !agentRowVisible(capture, "Ship Agent")
	})
	directRun(t, env, "send-keys", "-t", pane, "j")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "shipit") && agentRowVisible(capture, "Ship Agent")
	})
	directRun(t, env, "send-keys", "-t", pane, "C-c")
	if !waitForBool(8*time.Second, func() bool { return clientExited(clientDone) }) {
		t.Fatalf("bottom shipit client did not exit after dashboard quit")
	}
}

func TestAttachedDashboardKeyboardAndRenderingE2E(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()

	runtimeDir := filepath.Join(tmp, "weft-home")
	workspace := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	startupMarker := filepath.Join(tmp, "fake-codex-color-only")
	titleHookPayload := filepath.Join(tmp, "title-hook-payload.json")
	inputLog := filepath.Join(tmp, "fake-codex-input.log")
	ptySizeLog := filepath.Join(tmp, "fake-codex-pty-size")
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
			"cols=$(stty size 2>/dev/null); cols=${cols#* }; if [ -n \"${PTY_SIZE_LOG:-}\" ]; then printf '%s\\n' \"$cols\" > \"$PTY_SIZE_LOG\"; fi\n"+
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
			"  if [ -n \"${INPUT_LOG:-}\" ]; then printf '%s\\n' \"$line\" >> \"$INPUT_LOG\"; fi\n"+
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
	configText := fmt.Sprintf("codex_command = %q\ntitle_template = %q\ntitle_hook_command = %q\n", fakeCodex, "{codex}", titleHookCommand)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"WEFT_HOME="+runtimeDir,
		"WEFT_WORKSPACE="+workspace,
		"WEFT_EXECUTABLE="+bin,
		"STARTUP_DELAY=1.2",
		"STARTUP_MARKER="+startupMarker,
		"INPUT_LOG="+inputLog,
		"PTY_SIZE_LOG="+ptySizeLog,
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
	clientCmd.Dir = workspace
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
			return strings.Contains(capture, "Add this workspace to Weft?") &&
				strings.Contains(capture, "Y yes")
		})
		directRun(t, env, "send-keys", "-t", pane, "y")
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
				strings.Contains(capture, collapsedCodexInterruptToolbar) &&
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
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		loadingNavCapture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Agents") &&
				strings.Contains(capture, "Fake Codex Ready") &&
				agentLineHasLoadingFrame(capture, "Fake Codex Ready")
		})
		if strings.Contains(loadingNavCapture, "• Fake Codex Ready") {
			t.Fatalf("loading agent row should not keep the static bullet marker:\n%s", loadingNavCapture)
		}
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, collapsedCodexInterruptToolbar) && !strings.Contains(capture, "Agents")
		})
		t.Logf("dashboard_e2e metric=%q duration=%s", "new agent color-only startup covered", time.Since(started).Round(time.Millisecond))
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, ">_ OpenAI Codex") &&
				!strings.Contains(capture, "No Codex agent open") &&
				!strings.Contains(capture, "Workspaces") &&
				!strings.Contains(capture, "Agents") &&
				strings.Contains(capture, collapsedCodexQuitToolbar)
		})
		if !waitForBool(2*time.Second, func() bool {
			data, _ := os.ReadFile(ptySizeLog)
			return strings.TrimSpace(string(data)) == "157"
		}) {
			data, _ := os.ReadFile(ptySizeLog)
			t.Fatalf("new Codex PTY should start at the full visible console width, got %q:\n%s", strings.TrimSpace(string(data)), capture)
		}
		if run := longestRuneRun(capture, 'x'); run < 120 {
			t.Fatalf("long Codex output wrapped before using the full console width, longest run=%d:\n%s", run, capture)
		}
		t.Logf("dashboard_e2e metric=%q duration=%s", "new agent first Codex content visible", time.Since(started).Round(time.Millisecond))
		waitForEscapedCapture(t, env, pane, func(capture string) bool {
			return strings.Contains(capture, keyboardProtocolSetup) &&
				strings.Contains(capture, "48;2;40;40;49") &&
				strings.Contains(capture, "Summarize recent commits")
		})
		assertClientDoesNotEnableMouseTracking(t, capturePaneEscaped(t, env, pane))
		assertDashboardNotCorrupt(t, clientOutput(), false)
		assertCodexBoxNotDrifted(t, clientOutput())
	})

	timedStep(t, "nav shortcut keys stay inside codex focus", func() {
		directRun(t, env, "send-keys", "-l", "-t", pane, "lj")
		time.Sleep(250 * time.Millisecond)
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && agent.GroupID == "" && st.Focus == state.FocusCodex
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

	timedStep(t, "codex focus forwards shift enter without submitting", func() {
		before, _ := os.ReadFile(inputLog)
		directRun(t, env, "send-keys", "-l", "-t", pane, "multi")
		writeClientInput(t, "\x1b[13;2u")
		directRun(t, env, "send-keys", "-l", "-t", pane, "line")
		time.Sleep(150 * time.Millisecond)
		mid, _ := os.ReadFile(inputLog)
		if len(mid) != len(before) {
			t.Fatalf("shift enter submitted before normal Enter:\nbefore=%q\nafter=%q", before, mid)
		}
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		if !waitForBool(2*time.Second, func() bool {
			data, _ := os.ReadFile(inputLog)
			if len(data) < len(before) {
				return false
			}
			return bytes.Contains(data[len(before):], []byte("multi\x1b[13;2uline\n"))
		}) {
			data, _ := os.ReadFile(inputLog)
			t.Fatalf("shift enter sequence was not forwarded to Codex:\nbefore=%q\nafter=%q", before, data)
		}
	})

	timedStep(t, "codex focus forwards shift tab plan mode shortcuts", func() {
		before, _ := os.ReadFile(inputLog)
		writeClientInput(t, "\x1b[9;2u")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		if !waitForBool(2*time.Second, func() bool {
			data, _ := os.ReadFile(inputLog)
			if len(data) < len(before) {
				return false
			}
			return bytes.Contains(data[len(before):], []byte("\x1b[9;2u\n"))
		}) {
			data, _ := os.ReadFile(inputLog)
			t.Fatalf("enhanced shift tab sequence was not forwarded to Codex:\nbefore=%q\nafter=%q", before, data)
		}

		before, _ = os.ReadFile(inputLog)
		writeClientInput(t, "\x1b[Z")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		if !waitForBool(2*time.Second, func() bool {
			data, _ := os.ReadFile(inputLog)
			if len(data) < len(before) {
				return false
			}
			return bytes.Contains(data[len(before):], []byte("\x1b[Z\n"))
		}) {
			data, _ := os.ReadFile(inputLog)
			t.Fatalf("backtab sequence was not forwarded to Codex:\nbefore=%q\nafter=%q", before, data)
		}
	})

	timedStep(t, "C-b opens dashboard", func() {
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusAgents && st.NavOpen })
		time.Sleep(250 * time.Millisecond)
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusAgents && st.NavOpen })
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Agents") &&
				strings.Contains(capture, "Fake Codex Ready")
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
			return groupByPath(st, "release") != nil
		})
		directRun(t, env, "send-keys", "-t", pane, "m")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Move agent")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, "release")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			group := groupForAgent(st, agent)
			return agent != nil && group != nil && group.Path == "release"
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
			return strings.Contains(capture, "received:status check")
		})
		assertDashboardNotCorrupt(t, capture, false)
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && strings.Contains(agent.CodexTitle, "Ready")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusAgents && st.NavOpen })
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Codex ready")
		})
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
			return strings.Contains(capture, "Delete agent") &&
				strings.Contains(capture, "Codex ready") &&
				strings.Contains(capture, "Stops the Codex terminal, then removes this agent from Weft.") &&
				strings.Contains(capture, "Y stop and delete")
		})
		directRun(t, env, "send-keys", "-t", pane, "n")
		waitState(t, env, bin, func(st state.State) bool { return len(st.Agents) == 1 })
		directRun(t, env, "send-keys", "-t", pane, "d")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete agent") &&
				strings.Contains(capture, "Codex ready") &&
				strings.Contains(capture, "N cancel")
		})
		directRun(t, env, "send-keys", "-t", pane, "y")
		waitState(t, env, bin, func(st state.State) bool { return len(st.Agents) == 0 })
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No Codex agent open")
		})
		assertDashboardNotCorrupt(t, clientOutput(), true)
		directRun(t, env, "send-keys", "-t", pane, "d")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete group") &&
				strings.Contains(capture, "release") &&
				strings.Contains(capture, "Esc cancel")
		})
		directRun(t, env, "send-keys", "-t", pane, "y")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 0 && len(st.Groups) == 0 && st.SelectedWorkspaceID != ""
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No agents")
		})
	})

	timedStep(t, "C-c interrupts active Codex and quits Weft when ready", func() {
		directRun(t, env, "send-keys", "-t", pane, "n")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 1 &&
				st.Focus == state.FocusCodex &&
				strings.Contains(st.Agents[0].CodexTitle, "Ready")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, "interrupt")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 1 &&
				st.Focus == state.FocusCodex &&
				st.Agents[0].Status == state.StatusRunning &&
				strings.Contains(st.Agents[0].CodexTitle, "Working")
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "received:interrupt") &&
				strings.Contains(capture, collapsedCodexInterruptToolbar)
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
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, collapsedCodexQuitToolbar)
		})
		directRun(t, env, "send-keys", "-t", pane, "C-c")
		if !waitForBool(2*time.Second, func() bool {
			attached := directLines(t, env, "display-message", "-p", "-t", pane, "#{session_attached}")
			return len(attached) == 1 && attached[0] == "0"
		}) {
			t.Fatalf("dashboard C-c did not detach Weft clients")
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

func TestDashboardOrganizationJourneysE2E(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir := filepath.Join(tmp, "weft-home")
	projectRoot := filepath.Join(tmp, "projects")
	alpha := filepath.Join(projectRoot, "alpha")
	beta := filepath.Join(projectRoot, "beta")
	for _, dir := range []string{runtimeDir, alpha, beta} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fakeCodex := writeVisibleFakeCodex(t, tmp, "fake-codex-dashboard.sh")
	configText := fmt.Sprintf("codex_command = %q\n", fakeCodex)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"WEFT_HOME="+runtimeDir,
		"WEFT_WORKSPACE="+alpha,
		"WEFT_EXECUTABLE="+bin,
		"PATH="+os.Getenv("PATH"),
		"TERM=xterm-256color",
	)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	pane := "direct-client-organization"
	clientOutput, clientDone := startDirectDashboardClient(t, env, bin, alpha, pane, 150, 36)
	waitFor(t, "supervisor socket", 8*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(runtimeDir, "weft.sock"))
		return err == nil
	})

	timedStep(t, "launch prompt can be declined then workspace is added with autocomplete", func() {
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Add this workspace to Weft?") &&
				strings.Contains(capture, "N no")
		})
		directRun(t, env, "send-keys", "-t", pane, "n")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Workspaces) == 0 && len(st.Agents) == 0
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No workspaces") &&
				strings.Contains(capture, "Add a workspace first.")
		})

		directRun(t, env, "send-keys", "-t", pane, "w")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Add workspace")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-u")
		directRun(t, env, "send-keys", "-l", "-t", pane, filepath.Join(projectRoot, "alp"))
		directRun(t, env, "send-keys", "-t", pane, "Down")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "> alpha") &&
				strings.Contains(capture, "Enter choose")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Enter add") &&
				!strings.Contains(capture, "> alpha")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			workspace := state.WorkspaceByPath(st, alpha)
			return len(st.Workspaces) == 1 && workspace != nil && st.SelectedWorkspaceID == workspace.ID
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No agents") &&
				strings.Contains(capture, "Press n to create one.")
		})
	})

	timedStep(t, "second workspace is added and workspace title can be set and cleared", func() {
		directRun(t, env, "send-keys", "-t", pane, "w")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Add workspace")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-u")
		directRun(t, env, "send-keys", "-l", "-t", pane, beta)
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "> beta") &&
				strings.Contains(capture, "Enter choose")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Enter add") ||
				(strings.Contains(capture, "Workspaces") &&
					strings.Contains(capture, "Agents") &&
					!strings.Contains(capture, "Add workspace"))
		})
		if strings.Contains(capture, "Enter add") {
			directRun(t, env, "send-keys", "-t", pane, "Enter")
		}
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Workspaces) == 2 && state.WorkspaceByPath(st, beta) != nil
		})

		directRun(t, env, "send-keys", "-t", pane, "Left")
		st := waitState(t, env, bin, func(st state.State) bool {
			return st.Focus == state.FocusWorkspaces
		})
		alphaWorkspace := state.WorkspaceByPath(st, alpha)
		if alphaWorkspace == nil {
			t.Fatalf("alpha workspace missing after beta add: %#v", st.Workspaces)
		}
		if st.SelectedWorkspaceID != alphaWorkspace.ID {
			directRun(t, env, "send-keys", "-t", pane, "k")
			waitState(t, env, bin, func(st state.State) bool {
				workspace := state.WorkspaceByPath(st, alpha)
				return workspace != nil && st.SelectedWorkspaceID == workspace.ID
			})
		}
		directRun(t, env, "send-keys", "-t", pane, "r")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Rename workspace")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, "Alpha Workspace")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			workspace := state.WorkspaceByPath(st, alpha)
			return workspace != nil && workspace.Title == "Alpha Workspace"
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Alpha Workspace")
		})

		directRun(t, env, "send-keys", "-t", pane, "r")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Rename workspace")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-u")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			workspace := state.WorkspaceByPath(st, alpha)
			return workspace != nil && workspace.Title == ""
		})
	})

	var firstAgentID string
	timedStep(t, "group lifecycle and grouped agent journeys run through the dashboard", func() {
		directRun(t, env, "send-keys", "-t", pane, "Right")
		waitState(t, env, bin, func(st state.State) bool {
			return st.Focus == state.FocusAgents
		})
		directRun(t, env, "send-keys", "-t", pane, "g")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Create group")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, "release")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return groupByPath(st, "release") != nil
		})

		directRun(t, env, "send-keys", "-t", pane, "r")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Rename group")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-u")
		directRun(t, env, "send-keys", "-l", "-t", pane, "renamed")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return groupByPath(st, "renamed") != nil
		})

		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			group := groupByPath(st, "renamed")
			return group != nil && state.IsGroupCollapsed(st, group.ID)
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "renamed") && strings.Contains(capture, "▸")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			group := groupByPath(st, "renamed")
			return group != nil && !state.IsGroupCollapsed(st, group.ID)
		})

		directRun(t, env, "send-keys", "-t", pane, "n")
		st := waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 1 &&
				st.Agents[0].GroupID == "" &&
				st.Agents[0].Status == state.StatusRunning &&
				st.Focus == state.FocusCodex
		})
		firstAgentID = st.Agents[0].ID

		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool {
			return st.Focus == state.FocusAgents && st.NavOpen
		})
		directRun(t, env, "send-keys", "-t", pane, "m")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Move agent")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, "renamed")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			group := groupByPath(st, "renamed")
			agent := findAgent(st, firstAgentID)
			return group != nil && agent != nil && agent.GroupID == group.ID
		})
		directRun(t, env, "send-keys", "-t", pane, "k")
		time.Sleep(250 * time.Millisecond)
		directRun(t, env, "send-keys", "-t", pane, "d")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete group") &&
				strings.Contains(capture, "renamed")
		})
		directRun(t, env, "send-keys", "-t", pane, "y")
		waitState(t, env, bin, func(st state.State) bool {
			group := groupByPath(st, "renamed")
			agent := findAgent(st, firstAgentID)
			return group != nil && agent != nil && agent.GroupID == group.ID
		})

		directRun(t, env, "send-keys", "-t", pane, "j")
		time.Sleep(250 * time.Millisecond)
		directRun(t, env, "send-keys", "-t", pane, "m")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Move agent") &&
				strings.Contains(capture, "Top-level agent")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstAgentID)
			return agent != nil && agent.GroupID == ""
		})

		directRun(t, env, "send-keys", "-t", pane, "j")
		time.Sleep(250 * time.Millisecond)
		directRun(t, env, "send-keys", "-t", pane, "d")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete group") &&
				strings.Contains(capture, "renamed")
		})
		directRun(t, env, "send-keys", "-t", pane, "y")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Groups) == 0
		})

		directRun(t, env, "send-keys", "-t", pane, "n")
		waitState(t, env, bin, func(st state.State) bool {
			active := state.ActiveAgent(st)
			return len(st.Agents) == 2 &&
				active != nil &&
				active.ID != firstAgentID &&
				active.GroupID == "" &&
				st.Focus == state.FocusCodex
		})
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool {
			return st.Focus == state.FocusAgents && st.NavOpen
		})
		directRun(t, env, "send-keys", "-t", pane, "k")
		time.Sleep(250 * time.Millisecond)
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return st.ActiveAgentID == firstAgentID && st.Focus == state.FocusCodex
		})
	})

	timedStep(t, "refresh and second attach preserve selected running agent", func() {
		out := runWeft(t, env, bin, "refresh")
		if !strings.Contains(out, "refreshed Weft dashboard") {
			t.Fatalf("refresh output missing message:\n%s", out)
		}
		clientOutput, _ = startDirectDashboardClient(t, env, bin, alpha, pane+"-reattach", 150, 36)
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, collapsedCodexToolbarPrefix) ||
				(strings.Contains(capture, "Workspaces") && strings.Contains(capture, "Agents"))
		})
		if !waitForBool(8*time.Second, func() bool { return clientExited(clientDone) }) {
			t.Fatalf("first client did not detach after second attach")
		}
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstAgentID)
			return agent != nil &&
				st.ActiveAgentID == firstAgentID &&
				agent.Status == state.StatusRunning &&
				len(st.Agents) == 2
		})
	})

	timedStep(t, "workspace deletion removes empty and active workspaces", func() {
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool {
			return st.Focus == state.FocusAgents && st.NavOpen
		})
		time.Sleep(250 * time.Millisecond)
		directRun(t, env, "send-keys", "-t", pane, "Left")
		st := waitState(t, env, bin, func(st state.State) bool {
			return st.Focus == state.FocusWorkspaces
		})
		time.Sleep(250 * time.Millisecond)
		selected := state.WorkspaceByID(st, st.SelectedWorkspaceID)
		if selected == nil {
			t.Fatalf("no selected workspace before deletion: %#v", st.Workspaces)
		}

		directRun(t, env, "send-keys", "-t", pane, "d")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete workspace")
		})
		directRun(t, env, "send-keys", "-t", pane, "y")
		if selected.Path == beta {
			waitState(t, env, bin, func(st state.State) bool {
				return len(st.Workspaces) == 1 && state.WorkspaceByPath(st, beta) == nil && len(st.Agents) == 2
			})
		} else {
			waitState(t, env, bin, func(st state.State) bool {
				return len(st.Workspaces) == 1 && state.WorkspaceByPath(st, alpha) == nil && len(st.Agents) == 0
			})
		}
		time.Sleep(250 * time.Millisecond)

		directRun(t, env, "send-keys", "-t", pane, "d")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete workspace")
		})
		directRun(t, env, "send-keys", "-t", pane, "y")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Workspaces) == 0 && len(st.Groups) == 0 && len(st.Agents) == 0
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No workspaces") &&
				strings.Contains(capture, "No Codex agent open")
		})
	})
}

func TestDashboardPerformanceSmokeE2E(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	fakeCodex := writeVisibleFakeCodex(t, tmp, "fake-codex-performance.sh")
	runtimeDir, workspace := createRuntime(t, tmp, fakeCodex)
	secondWorkspace := filepath.Join(tmp, "second-workspace")
	if err := os.Mkdir(secondWorkspace, 0o700); err != nil {
		t.Fatal(err)
	}
	env := baseIntegrationEnv(runtimeDir, workspace, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	pane := "direct-client-performance"
	started := time.Now()
	clientOutput, firstClientDone := startDirectDashboardClient(t, env, bin, workspace, pane, 150, 36)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Add this workspace to Weft?") &&
			strings.Contains(capture, "Y yes")
	})
	assertPerformanceBudget(t, "fresh dashboard prompt visible", time.Since(started), 8*time.Second)

	started = time.Now()
	directRun(t, env, "send-keys", "-t", pane, "y")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Agents") &&
			strings.Contains(capture, "No Codex agent open")
	})
	assertPerformanceBudget(t, "initial workspace accepted", time.Since(started), 2*time.Second)

	started = time.Now()
	directRun(t, env, "send-keys", "-t", pane, "w")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Add workspace")
	})
	directRun(t, env, "send-keys", "-t", pane, "C-u")
	directRun(t, env, "send-keys", "-l", "-t", pane, secondWorkspace)
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Workspaces) == 2 && state.WorkspaceByPath(st, secondWorkspace) != nil
	})
	assertPerformanceBudget(t, "add workspace prompt completes", time.Since(started), 3*time.Second)

	started = time.Now()
	directRun(t, env, "send-keys", "-t", pane, "n")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Starting Codex") &&
			strings.Contains(capture, collapsedCodexInterruptToolbar)
	})
	assertPerformanceBudget(t, "agent startup placeholder visible", time.Since(started), time.Second)

	waitForOutput(t, clientOutput, func(capture string) bool {
		return (strings.Contains(capture, "Fake Codex dashboard ready") ||
			strings.Contains(capture, "ready waiting for first message")) &&
			strings.Contains(capture, collapsedCodexQuitToolbar)
	})
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 &&
			st.Agents[0].Status == state.StatusRunning &&
			st.Focus == state.FocusCodex
	})
	assertPerformanceBudget(t, "first agent content visible", time.Since(started), 4*time.Second)

	started = time.Now()
	out := runWeft(t, env, bin, "refresh")
	if !strings.Contains(out, "refreshed Weft dashboard") {
		t.Fatalf("refresh output missing message:\n%s", out)
	}
	assertPerformanceBudget(t, "refresh command returns", time.Since(started), 3*time.Second)

	started = time.Now()
	clientOutput, _ = startDirectDashboardClient(t, env, bin, workspace, pane+"-reattach", 150, 36)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, collapsedCodexToolbarPrefix) ||
			strings.Contains(capture, "Fake Codex dashboard ready")
	})
	assertPerformanceBudget(t, "reattach renders running agent", time.Since(started), 8*time.Second)
	if !waitForBool(8*time.Second, func() bool { return clientExited(firstClientDone) }) {
		t.Fatalf("first client did not detach after second attach")
	}
}

func timedStep(t *testing.T, name string, fn func()) {
	t.Helper()
	start := time.Now()
	fn()
	t.Logf("dashboard_e2e step=%q duration=%s", name, time.Since(start).Round(time.Millisecond))
}

func assertPerformanceBudget(t *testing.T, metric string, elapsed time.Duration, budget time.Duration) {
	t.Helper()
	rounded := elapsed.Round(time.Millisecond)
	t.Logf("dashboard_e2e perf metric=%q duration=%s budget=%s", metric, rounded, budget)
	if elapsed > budget {
		t.Fatalf("%s exceeded budget: duration=%s budget=%s", metric, rounded, budget)
	}
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
	case "Tab":
		writeClientInput(t, "\t")
	case "Up":
		writeClientInput(t, "\x1b[A")
	case "Down":
		writeClientInput(t, "\x1b[B")
	case "Right":
		writeClientInput(t, "\x1b[C")
	case "Left":
		writeClientInput(t, "\x1b[D")
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
	if count := strings.Count(capture, "C-c interrupt") + strings.Count(capture, "C-c quit"); count > 1 {
		t.Fatalf("dashboard rendered duplicate footer/header labels, got %d:\n%s", count, capture)
	}
	if !empty && strings.Contains(capture, "No Codex agent open") {
		t.Fatalf("dashboard kept empty state after agent was created:\n%s", capture)
	}
}

func assertClientDoesNotEnableMouseTracking(t *testing.T, capture string) {
	t.Helper()
	for _, forbidden := range []string{"\x1b[?1002h", "\x1b[?1003h", "\x1b[?1006h"} {
		if strings.Contains(capture, forbidden) {
			t.Fatalf("client should not enable mouse tracking because it blocks terminal drag selection; found %q in raw capture", forbidden)
		}
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

func agentLineHasLoadingFrame(capture string, title string) bool {
	for _, line := range strings.Split(capture, "\n") {
		if !strings.Contains(line, title) {
			continue
		}
		for _, frame := range []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"} {
			if strings.Contains(line, frame) {
				return true
			}
		}
	}
	return false
}

func agentRowVisible(capture string, title string) bool {
	markers := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏", "●", "•", "◦", "!"}
	for _, line := range strings.Split(capture, "\n") {
		if !strings.Contains(line, title) {
			continue
		}
		for _, marker := range markers {
			if strings.Contains(line, marker+" "+title) {
				return true
			}
		}
	}
	return false
}

func longestRuneRun(value string, target rune) int {
	best := 0
	current := 0
	for _, r := range value {
		if r == target {
			current++
			if current > best {
				best = current
			}
			continue
		}
		current = 0
	}
	return best
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

func groupForAgent(st state.State, agent *state.Agent) *state.Group {
	if agent == nil {
		return nil
	}
	for index := range st.Groups {
		if st.Groups[index].ID == agent.GroupID {
			return &st.Groups[index]
		}
	}
	return nil
}

func groupByPath(st state.State, path string) *state.Group {
	for index := range st.Groups {
		if st.Groups[index].Path == path {
			return &st.Groups[index]
		}
	}
	return nil
}

func startDirectDashboardClient(t *testing.T, env []string, bin string, workspace string, pane string, cols int, rows int) (func() string, <-chan struct{}) {
	t.Helper()
	clientCmd := exec.Command(bin)
	clientCmd.Env = env
	clientCmd.Dir = workspace
	clientPTY, err := pty.StartWithSize(clientCmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
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

	clientScreen := tui.NewTerminalScreen(cols, rows)
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
	registerDirectClient(clientCmd, clientPTY, clientScreen, &clientRaw, &clientMu, clientDone)
	output := func() string {
		clientMu.Lock()
		defer clientMu.Unlock()
		return clientScreen.String()
	}
	_ = pane
	return output, clientDone
}

func writeVisibleFakeCodex(t *testing.T, dir string, name string) string {
	t.Helper()
	fakeCodex := filepath.Join(dir, name)
	if err := os.WriteFile(fakeCodex, []byte(
		"#!/bin/sh\n"+
			"printf '\\033]2;Fake Codex Ready\\007'\n"+
			"printf 'Fake Codex dashboard ready\\n'\n"+
			"trap 'exit 0' HUP INT TERM\n"+
			"while IFS= read -r line; do\n"+
			"  printf '\\033]2;Fake Codex Working\\007'\n"+
			"  printf 'echo:%s\\n' \"$line\"\n"+
			"  printf '\\033]2;Fake Codex Ready\\007'\n"+
			"done\n"+
			"while :; do sleep 1; done\n",
	), 0o700); err != nil {
		t.Fatal(err)
	}
	return fakeCodex
}
