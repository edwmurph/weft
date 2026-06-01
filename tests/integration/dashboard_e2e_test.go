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
	collapsedCodexToolbar = "C-b dashboard"
	keyboardProtocolSetup = "\x1b[>4;2m\x1b[>29u"
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
			strings.Contains(capture, "Enter yes") &&
			strings.Contains(capture, "Esc no")
	})
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Workspaces") &&
			strings.Contains(capture, "Tasks")
	})

	directRun(t, env, "send-keys", "-t", pane, "n")
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	st := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 && st.Agents[0].Status != state.StatusStarting
	})
	if st.Agents[0].Status != state.StatusRunning {
		t.Fatalf("agent should start even when SHELL is invalid, status=%s title=%q\nscreen:\n%s", st.Agents[0].Status, st.Agents[0].CodexTitle, clientOutput())
	}
	waitForEscapedCapture(t, env, pane, func(capture string) bool {
		return strings.Contains(capture, keyboardProtocolSetup)
	})
	assertClientEnablesMouseTracking(t, capturePaneEscaped(t, env, pane))
	directRun(t, env, "send-keys", "-l", "-t", pane, "probe")
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	capture := waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "echo:probe")
	})
	if strings.Contains(capture, "No task open") || strings.Contains(capture, "fork/exec") {
		t.Fatalf("new task rendered stale empty/error state:\n%s", capture)
	}
}

func TestDashboardCanCreateConfiguredShellTaskE2E(t *testing.T) {
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
	shellCommand := "/bin/sh -lc " + shellQuote("printf 'shell-ready\n'; while IFS= read -r line; do printf 'line:%s\n' \"$line\"; done")
	configText := fmt.Sprintf(`
default_task_type = "shell"

[task_types.codex]
command = %q

[task_types.shell]
label = "Shell"
kind = "terminal"
command = %q
badge = "[shell]"
title_template = "Shell"
`, fakeCodex, shellCommand)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	env := baseIntegrationEnv(runtimeDir, workspace, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	pane := "direct-client-shell-task"
	clientOutput, _ := startDirectDashboardClient(t, env, bin, workspace, pane, 120, 32)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Add this workspace to Weft?")
	})
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Tasks") && strings.Contains(capture, "No task open")
	})

	directRun(t, env, "send-keys", "-t", pane, "n")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "New task") &&
			strings.Contains(capture, "Codex") &&
			strings.Contains(capture, "Shell") &&
			!strings.Contains(capture, "[codex] Codex") &&
			!strings.Contains(capture, "[shell] Shell")
	})
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	st := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 &&
			st.Agents[0].TypeID == "shell" &&
			st.Agents[0].Status == state.StatusReady
	})
	if st.Agents[0].CodexSessionID != "" {
		t.Fatalf("shell task should not capture Codex session id: %#v", st.Agents[0])
	}
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "shell-ready") &&
			strings.Contains(capture, collapsedCodexToolbar)
	})

	directRun(t, env, "send-keys", "-l", "-t", pane, "\x1b[101u\x1b[99u\x1b[104u\x1b[111u\x1b[32u\x1b[115u\x1b[104u\x1b[101u\x1b[108u\x1b[108u\x1b[45u\x1b[105u\x1b[110u\x1b[112u\x1b[117u\x1b[116u\x1b[13u")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "line:echo shell-input")
	})

	directRun(t, env, "send-keys", "-l", "-t", pane, "\x1b[101u\x1b[99u\x1b[104u\x1b[111u\x1b[32u\x1b[115u\x1b[116u\x1b[97u\x1b[108u\x1b[101u\x1b[117;5u\x1b[101u\x1b[99u\x1b[104u\x1b[111u\x1b[32u\x1b[99u\x1b[108u\x1b[101u\x1b[97u\x1b[114u\x1b[101u\x1b[100u\x1b[13u")
	capture := waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "line:echo cleared")
	})
	if strings.Contains(capture, "line:echo stale") {
		t.Fatalf("C-u should clear the shell input line:\n%s", capture)
	}

	directRun(t, env, "send-keys", "-l", "-t", pane, "\x1b[57441;2u\x1b[57442;5u\x1b[101;2u\x1b[99u\x1b[104u\x1b[111u\x1b[32u\x1b[109u\x1b[111u\x1b[100u\x1b[105u\x1b[102u\x1b[105u\x1b[101u\x1b[114u\x1b[115u\x1b[13u")
	capture = waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "line:Echo modifiers")
	})
	if strings.Contains(capture, "57441") || strings.Contains(capture, "57442") {
		t.Fatalf("modifier-only enhanced key events should not leak into shell input:\n%s", capture)
	}

	directRun(t, env, "send-keys", "-t", pane, "C-b")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Tasks") &&
			strings.Contains(capture, "[shell] Shell")
	})
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
		return strings.Contains(capture, "path missing; press Backspace to remove")
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

	directRun(t, env, "send-keys", "-t", pane, "Backspace")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Delete workspace")
	})
	directRun(t, env, "send-keys", "-t", pane, "Enter")
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
		return strings.Contains(capture, "Workspaces") && strings.Contains(capture, "Tasks")
	})
	for _, name := range []string{"alpha", "beta", "gamma", "delta", "shipit"} {
		directRun(t, env, "send-keys", "-t", pane, "g")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Group required") &&
				strings.Contains(capture, "[ ] Silent") &&
				!strings.Contains(capture, "Enter create")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, name)
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "> "+name)
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return groupByPath(st, name) != nil
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return !strings.Contains(capture, "Create group")
		})
	}
	directRun(t, env, "send-keys", "-t", pane, "n")
	directRun(t, env, "send-keys", "-t", pane, "Enter")
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
		return strings.Contains(capture, "Top-level task") &&
			strings.Contains(capture, "Blank makes the task top-level.")
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
	time.Sleep(250 * time.Millisecond)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "shipit") && !agentRowVisible(capture, "Ship Agent")
	})
	directRun(t, env, "send-keys", "-t", pane, "e")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Group") &&
			strings.Contains(capture, "shipit") &&
			strings.Contains(capture, "Silent")
	})
	directRun(t, env, "send-keys", "-t", pane, "C-u")
	directRun(t, env, "send-keys", "-l", "-t", pane, "shipit-later")
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitState(t, env, bin, func(st state.State) bool {
		return groupByPath(st, "shipit-later") != nil
	})
	directRun(t, env, "send-keys", "-t", pane, "Backspace")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Delete group") &&
			strings.Contains(capture, "shipit-later")
	})
	directRun(t, env, "send-keys", "-t", pane, "Escape")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return !strings.Contains(capture, "Delete group")
	})
	directRun(t, env, "send-keys", "-t", pane, "j")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "shipit-later") && agentRowVisible(capture, "Ship Agent")
	})
	directRun(t, env, "send-keys", "-t", pane, "C-c")
	if !waitForBool(8*time.Second, func() bool { return clientExited(clientDone) }) {
		t.Fatalf("bottom shipit client did not exit after dashboard quit")
	}
}

func TestAgentsPaneGroupCursorSurvivesSupervisorRestartE2E(t *testing.T) {
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
	for _, group := range []string{"in progress", "shipit", "planning"} {
		runWeft(t, env, bin, "group", "add", group)
	}
	for _, title := range []string{"Progress A", "Progress B"} {
		runWeft(t, env, bin, "new", title)
		runWeft(t, env, bin, "move-right")
	}
	runWeft(t, env, bin, "new", "Planning Agent")
	pane := "planning-cursor-client"
	clientOutput, clientDone := startDirectDashboardClient(t, env, bin, workspace, pane, 130, 18)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, collapsedCodexToolbar) ||
			strings.Contains(capture, "Workspaces")
	})
	directRun(t, env, "send-keys", "-t", pane, "C-b")
	waitState(t, env, bin, func(st state.State) bool {
		return st.Focus == state.FocusAgents && st.NavOpen
	})
	directRun(t, env, "send-keys", "-t", pane, "m")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Move task")
	})
	directRun(t, env, "send-keys", "-l", "-t", pane, "planning")
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitState(t, env, bin, func(st state.State) bool {
		group := groupByPath(st, "planning")
		active := state.ActiveAgent(st)
		return group != nil && active != nil && active.Title == "Planning Agent" && active.GroupID == group.ID
	})
	waitForOutput(t, clientOutput, func(capture string) bool {
		return !strings.Contains(capture, "Move task") &&
			strings.Contains(capture, "planning (1)") &&
			agentRowVisible(capture, "Planning Agent")
	})

	directRun(t, env, "send-keys", "-t", pane, "k")
	time.Sleep(250 * time.Millisecond)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "planning") && agentRowVisible(capture, "Planning Agent")
	})
	directRun(t, env, "send-keys", "-t", pane, "e")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Group") && strings.Contains(capture, "planning")
	})
	directRun(t, env, "send-keys", "-t", pane, "Escape")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return !strings.Contains(capture, "Edit group")
	})
	directRun(t, env, "send-keys", "-t", pane, "C-c")
	if !waitForBool(8*time.Second, func() bool { return clientExited(clientDone) }) {
		t.Fatalf("planning cursor client did not exit")
	}

	runWeft(t, env, bin, "close", "--kill", "--yes")
	runWeft(t, env, bin, "--no-attach")
	clientOutput, clientDone = startDirectDashboardClient(t, env, bin, workspace, pane+"-reattach", 130, 18)
	waitState(t, env, bin, func(st state.State) bool {
		return st.Focus == state.FocusAgents && st.NavOpen
	})
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Workspaces") &&
			strings.Contains(capture, "planning (1)")
	})
	directRun(t, env, "send-keys", "-t", pane+"-reattach", "e")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Group") && strings.Contains(capture, "planning")
	})
	directRun(t, env, "send-keys", "-t", pane+"-reattach", "Escape")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return !strings.Contains(capture, "Edit group")
	})
	directRun(t, env, "send-keys", "-t", pane+"-reattach", "j")
	time.Sleep(250 * time.Millisecond)
	directRun(t, env, "send-keys", "-t", pane+"-reattach", "e")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Variables") && strings.Contains(capture, "Planning Agent")
	})
	directRun(t, env, "send-keys", "-t", pane+"-reattach", "Escape")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return !strings.Contains(capture, "Variables")
	})
	directRun(t, env, "send-keys", "-t", pane+"-reattach", "C-c")
	if !waitForBool(8*time.Second, func() bool { return clientExited(clientDone) }) {
		t.Fatalf("reattached planning cursor client did not exit")
	}
}

func TestAgentConsoleMouseWheelScrollsHistoryE2E(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir, workspace := createRuntime(t, tmp, writeScrollbackFakeCodex(t, tmp, "fake-codex-scrollback.sh"))
	env := baseIntegrationEnv(runtimeDir, workspace, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	runWeft(t, env, bin, "--no-attach")
	runWeft(t, env, bin, "workspace", "add", workspace)

	pane := "mouse-wheel-client"
	clientOutput, _ := startDirectDashboardClient(t, env, bin, workspace, pane, 100, 32)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Workspaces") && strings.Contains(capture, "Tasks")
	})
	assertClientEnablesMouseTracking(t, capturePaneEscaped(t, env, pane))

	directRun(t, env, "send-keys", "-t", pane, "n")
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitState(t, env, bin, func(st state.State) bool {
		active := state.ActiveAgent(st)
		return active != nil && active.Status == state.StatusRunning && st.Focus == state.FocusCodex
	})
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "history line 80")
	})

	writeClientInput(t, "\x1b[<65;7;")
	writeClientInput(t, "7M")
	writeClientInput(t, "\x1b[<64;7;")
	writeClientInput(t, "7M")
	for range 14 {
		writeClientInput(t, "\x1b[<64;7;7M")
	}
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "history line 20") &&
			!strings.Contains(capture, "history line 80")
	})
	for range 16 {
		writeClientInput(t, "\x1b[<65;7;7M")
	}
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "history line 80")
	})
	assertDashboardNotCorrupt(t, clientOutput(), false)
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
	interruptLog := filepath.Join(tmp, "fake-codex-interrupt.log")
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
		"#!/bin/bash\n"+
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
			"side_mode=0\n"+
			"trap 'if [ -n \"${INTERRUPT_LOG:-}\" ]; then printf int >> \"$INTERRUPT_LOG\"; fi; if [ \"${side_mode:-0}\" -eq 1 ]; then side_mode=0; printf \"\\033]2;Fake Codex Ready\\007\"; printf \"\\033[2J\\033[Hreturned from side\\n\"; else printf \"\\033]2;Fake Codex Ready\\007\"; fi' INT\n"+
			"read_work_interrupt() {\n"+
			"  local active_side=\"$1\"\n"+
			"  local max_reads=\"${2:-10}\"\n"+
			"  local i=0 ch next seq\n"+
			"  while [ \"$i\" -lt \"$max_reads\" ]; do\n"+
			"    if IFS= read -r -s -n 1 -t 1 ch; then\n"+
			"      if [ \"$ch\" = $'\\003' ]; then\n"+
			"        if [ -n \"${INTERRUPT_LOG:-}\" ]; then printf int >> \"$INTERRUPT_LOG\"; fi\n"+
			"        printf '\\033]2;Fake Codex Ready\\007'\n"+
			"        if [ \"$active_side\" -eq 1 ]; then\n"+
			"          side_mode=0\n"+
			"          printf '\\033[2J\\033[Hraw ctrl-c returned main thread\\n'\n"+
			"        fi\n"+
			"        return 0\n"+
			"      fi\n"+
			"      if [ \"$ch\" = $'\\033' ]; then\n"+
			"        seq=$ch\n"+
			"        while IFS= read -r -s -n 1 -t 1 next; do\n"+
			"          seq=$seq$next\n"+
			"          if [ \"$next\" = \"u\" ] || [ \"$next\" = \"~\" ]; then break; fi\n"+
			"        done\n"+
			"        if [ \"$seq\" = $'\\033' ] || [ \"$seq\" = $'\\033[99;5u' ] || [ \"$seq\" = $'\\033[99;5:1u' ] || [ \"$seq\" = $'\\033[27;5;99~' ]; then\n"+
			"          if [ -n \"${INTERRUPT_LOG:-}\" ]; then printf int >> \"$INTERRUPT_LOG\"; fi\n"+
			"          printf '\\033]2;Fake Codex Ready\\007'\n"+
			"          if [ \"$active_side\" -eq 1 ]; then\n"+
			"            printf '\\033[2J\\033[Hinterrupted side work\\nside prompt\\n'\n"+
			"          fi\n"+
			"          return 0\n"+
			"        fi\n"+
			"      fi\n"+
			"    fi\n"+
			"    i=$((i + 1))\n"+
			"  done\n"+
			"  return 1\n"+
			"}\n"+
			"while IFS= read -r line; do\n"+
			"  if [ -n \"${INPUT_LOG:-}\" ]; then printf '%s\\n' \"$line\" >> \"$INPUT_LOG\"; fi\n"+
			"  if [ \"$line\" = \"/side\" ]; then\n"+
			"    side_mode=1\n"+
			"    printf '\\033]2;Fake Codex Ready\\007'\n"+
			"    printf '\\033[2J\\033[H'\n"+
			"    printf 'Side conversation boundary.\\n'\n"+
			"    printf 'You are in a side conversation, not the main thread.\\n'\n"+
			"    printf 'side prompt\\n'\n"+
			"    continue\n"+
			"  fi\n"+
			"  if [ \"$line\" = \"waiting status\" ]; then\n"+
			"    printf '\\033]2;Fake Codex Waiting\\007'\n"+
			"    printf '\\033[2J\\033[Hwaiting for approval\\n'\n"+
			"    continue\n"+
			"  fi\n"+
			"  if [ \"$line\" = \"crafting status\" ]; then\n"+
			"    printf '\\033]2;Fake Codex Crafting\\007'\n"+
			"    printf '\\033[2J\\033[Hcrafting a response\\n'\n"+
			"    continue\n"+
			"  fi\n"+
			"  if [ \"$line\" = \"ready status\" ]; then\n"+
			"    printf '\\033]2;Fake Codex Ready\\007'\n"+
			"    printf '\\033[2J\\033[Hready again\\n'\n"+
			"    continue\n"+
			"  fi\n"+
			"  if [ \"$line\" = \"plan answer\" ]; then\n"+
			"    printf '\\033]2;Fake Codex Running\\007'\n"+
			"    printf '\\033[2J\\033[HQuestion 1\\n'\n"+
			"    printf 'Choose the next step\\n'\n"+
			"    printf '1 unanswered question\\n'\n"+
			"    printf '\\nEnter to submit answer\\n'\n"+
			"    continue\n"+
			"  fi\n"+
			"  printf '\\033]2;Fake Codex Working\\007'\n"+
			"  printf '\\033[2J\\033[H'\n"+
			"  printf '╭──────────────────────────────────────────────────────────╮\\n'\n"+
			"  printf '│ >_ OpenAI Codex (v0.fake.0)                               │\\n'\n"+
			"  printf '│ model:     gpt-5.5 xhigh   /model to change              │\\n'\n"+
			"  printf '╰──────────────────────────────────────────────────────────╯\\n'\n"+
			"  i=0; while [ \"$i\" -lt 220 ]; do printf 'y'; i=$((i + 1)); done; printf '\\n'\n"+
			"  printf 'received:%s\\n' \"$line\"\n"+
			"  printf '\\033[10;5Hprompt'\n"+
			"  if [ \"$line\" = \"interrupt signal\" ] || [ \"$line\" = \"side-work\" ]; then\n"+
			"    saved_stty=$(stty -g)\n"+
			"    stty raw -echo -isig\n"+
			"    printf '\\nawaiting interrupt\\n'\n"+
			"    read_work_interrupt \"$side_mode\" 8\n"+
			"    interrupted=$?\n"+
			"    stty \"$saved_stty\"\n"+
			"    if [ \"$interrupted\" -eq 0 ]; then continue; fi\n"+
			"  else\n"+
			"    sleep 1\n"+
			"  fi\n"+
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
		"INTERRUPT_LOG="+interruptLog,
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
				strings.Contains(capture, "Enter yes") &&
				strings.Contains(capture, "Esc no")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Tasks") &&
				strings.Contains(capture, "No task open")
		})
		assertDashboardNotCorrupt(t, clientOutput(), true)
	})

	var firstID string
	timedStep(t, "keyboard n creates agent", func() {
		started := time.Now()
		directRun(t, env, "send-keys", "-t", pane, "n")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Starting Codex") &&
				strings.Contains(capture, collapsedCodexToolbar) &&
				!strings.Contains(capture, "C-c") &&
				!strings.Contains(capture, "No task open")
		})
		placeholderDuration := time.Since(started)
		t.Logf("dashboard_e2e metric=%q duration=%s", "new task startup placeholder visible", placeholderDuration.Round(time.Millisecond))
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
			return strings.Contains(capture, "Tasks") &&
				strings.Contains(capture, "Fake Codex Ready") &&
				agentLineHasLoadingFrame(capture, "Fake Codex Ready")
		})
		if strings.Contains(loadingNavCapture, "• Fake Codex Ready") {
			t.Fatalf("loading task row should not keep the static bullet marker:\n%s", loadingNavCapture)
		}
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, collapsedCodexToolbar) &&
				!strings.Contains(capture, "C-c") &&
				!strings.Contains(capture, "Tasks")
		})
		t.Logf("dashboard_e2e metric=%q duration=%s", "new task color-only startup covered", time.Since(started).Round(time.Millisecond))
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, ">_ OpenAI Codex") &&
				!strings.Contains(capture, "No task open") &&
				!strings.Contains(capture, "Workspaces") &&
				!strings.Contains(capture, "Tasks") &&
				strings.Contains(capture, collapsedCodexToolbar) &&
				!strings.Contains(capture, "C-c") &&
				longestRuneRun(capture, 'x') >= 120
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
		t.Logf("dashboard_e2e metric=%q duration=%s", "new task first Codex content visible", time.Since(started).Round(time.Millisecond))
		waitForEscapedCapture(t, env, pane, func(capture string) bool {
			return strings.Contains(capture, keyboardProtocolSetup) &&
				strings.Contains(capture, "48;2;40;40;49") &&
				strings.Contains(capture, "Summarize recent commits")
		})
		assertClientEnablesMouseTracking(t, capturePaneEscaped(t, env, pane))
		assertDashboardNotCorrupt(t, clientOutput(), false)
		assertCodexBoxNotDrifted(t, clientOutput())
	})

	timedStep(t, "codex focus highlights mouse drag selection", func() {
		writeClientInput(t, "\x1b[<0;9;2M")
		writeClientInput(t, "\x1b[<32;12;2M")
		waitForEscapedCapture(t, env, pane, func(capture string) bool {
			return containsReverseVideo(capture)
		})
		writeClientInput(t, "\x1b[<0;12;2m")
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

	timedStep(t, "codex focus forwards vim and paste bytes unchanged", func() {
		before, _ := os.ReadFile(inputLog)
		raw := "\x1bihello\x1b[200~paste\x1b[201~\x1b[A"
		writeClientInput(t, raw)
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		if !waitForBool(2*time.Second, func() bool {
			data, _ := os.ReadFile(inputLog)
			if len(data) < len(before) {
				return false
			}
			return bytes.Contains(data[len(before):], []byte(raw+"\n"))
		}) {
			data, _ := os.ReadFile(inputLog)
			t.Fatalf("raw Codex bytes were not forwarded unchanged:\nbefore=%q\nafter=%q", before, data)
		}
	})

	timedStep(t, "codex focus sends enhanced ctrl-c as Codex interrupt", func() {
		before, _ := os.ReadFile(inputLog)
		beforeInterrupt, _ := os.ReadFile(interruptLog)
		directRun(t, env, "send-keys", "-l", "-t", pane, "interrupt signal")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && strings.Contains(agent.CodexTitle, "Working")
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "received:interrupt signal") &&
				strings.Contains(capture, "awaiting interrupt")
		})
		writeClientInput(t, "\x1b[27;5;99~")
		if !waitForBool(2*time.Second, func() bool {
			data, _ := os.ReadFile(interruptLog)
			return len(data) > len(beforeInterrupt)
		}) {
			data, _ := os.ReadFile(interruptLog)
			t.Fatalf("enhanced ctrl+c did not reach Codex as an interrupt:\nbefore=%q\nafter=%q", beforeInterrupt, data)
		}
		data, _ := os.ReadFile(inputLog)
		if bytes.Contains(data[len(before):], []byte("\x1b[27;5;99~")) {
			t.Fatalf("enhanced ctrl+c should not be submitted as prompt text:\nbefore=%q\nafter=%q", before, data)
		}
		if clientExited(clientDone) {
			t.Fatal("enhanced ctrl+c should not quit Weft in Task Console")
		}
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && strings.Contains(agent.CodexTitle, "Ready")
		})
	})

	timedStep(t, "C-b opens dashboard", func() {
		writeClientInput(t, "\x1b[98;5u")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusAgents && st.NavOpen })
		time.Sleep(250 * time.Millisecond)
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusAgents && st.NavOpen })
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Tasks") &&
				strings.Contains(capture, "Fake Codex Ready")
		})
		assertDashboardNotCorrupt(t, capture, false)
	})

	timedStep(t, "auto title variable reveals title generated from first message", func() {
		directRun(t, env, "send-keys", "-t", pane, "e")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Variables") &&
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
		directRun(t, env, "send-keys", "-l", "-t", pane, "release-flow")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return groupByPath(st, "release-flow") != nil
		})
		directRun(t, env, "send-keys", "-t", pane, "m")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Move task")
		})
		directRun(t, env, "send-keys", "-l", "-t", pane, "flow")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "> release-flow") &&
				strings.Contains(capture, "Enter choose")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "release-flow") &&
				strings.Contains(capture, "Enter move")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			group := groupForAgent(st, agent)
			return agent != nil && group != nil && group.Path == "release-flow"
		})
		assertDashboardNotCorrupt(t, clientOutput(), false)
	})

	timedStep(t, "edit modal saves status template", func() {
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && strings.Contains(agent.CodexTitle, "Ready")
		})
		directRun(t, env, "send-keys", "-t", pane, "e")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Preview")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-u")
		directRun(t, env, "send-keys", "-l", "-t", pane, "Codex {status}")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && agent.Title == "Codex {status}"
		})
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Codex Ready") && !strings.Contains(capture, "Edit task")
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
			return strings.Contains(capture, "Codex Ready")
		})
	})

	timedStep(t, "status template passes through Codex live statuses", func() {
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusCodex })
		directRun(t, env, "send-keys", "-l", "-t", pane, "crafting status")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && strings.Contains(agent.CodexTitle, "Crafting")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusAgents && st.NavOpen })
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Codex Crafting") && !strings.Contains(capture, "Codex running")
		})

		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusCodex })
		directRun(t, env, "send-keys", "-l", "-t", pane, "waiting status")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && strings.Contains(agent.CodexTitle, "Waiting")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusAgents && st.NavOpen })
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Codex Waiting") && !strings.Contains(capture, "Codex running")
		})

		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusCodex })
		directRun(t, env, "send-keys", "-l", "-t", pane, "ready status")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && strings.Contains(agent.CodexTitle, "Ready")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusAgents && st.NavOpen })
	})

	timedStep(t, "plan-mode request input renders ready status", func() {
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusCodex })
		directRun(t, env, "send-keys", "-l", "-t", pane, "plan answer")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstID)
			return agent != nil && agent.Status == state.StatusReady && agent.CodexStatus == "Ready" && strings.Contains(agent.CodexTitle, "Running")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool { return st.Focus == state.FocusAgents && st.NavOpen })
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Codex Ready") &&
				strings.Contains(capture, "1 needs attention") &&
				!strings.Contains(capture, "Codex running")
		})
	})

	timedStep(t, "help modal closes", func() {
		directRun(t, env, "send-keys", "-t", pane, "?")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Weft shortcuts") &&
				strings.Contains(capture, "Backspace delete")
		})
		directRun(t, env, "send-keys", "-t", pane, "Escape")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return !strings.Contains(capture, "Weft shortcuts")
		})
	})

	timedStep(t, "close confirmation cancels then closes", func() {
		directRun(t, env, "send-keys", "-t", pane, "Backspace")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete task") &&
				strings.Contains(capture, "Codex Ready") &&
				strings.Contains(capture, "Stops the terminal, then removes this task from Weft.") &&
				strings.Contains(capture, "Enter stop and delete") &&
				strings.Contains(capture, "N Esc") &&
				!strings.Contains(capture, "Y stop and delete") &&
				!strings.Contains(capture, "Esc cancel")
		})
		directRun(t, env, "send-keys", "-t", pane, "n")
		waitState(t, env, bin, func(st state.State) bool { return len(st.Agents) == 1 })
		directRun(t, env, "send-keys", "-t", pane, "Backspace")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete task") &&
				strings.Contains(capture, "Codex Ready") &&
				strings.Contains(capture, "N Esc") &&
				!strings.Contains(capture, "N cancel")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool { return len(st.Agents) == 0 })
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No task open")
		})
		assertDashboardNotCorrupt(t, clientOutput(), true)
		directRun(t, env, "send-keys", "-t", pane, "Backspace")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete group") &&
				strings.Contains(capture, "release") &&
				strings.Contains(capture, "Enter delete") &&
				strings.Contains(capture, "Esc") &&
				!strings.Contains(capture, "N cancel") &&
				!strings.Contains(capture, "Esc cancel")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 0 && len(st.Groups) == 0 && st.SelectedWorkspaceID != ""
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No tasks")
		})
	})

	timedStep(t, "C-c stays owned by Codex in Task Console", func() {
		directRun(t, env, "send-keys", "-t", pane, "n")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
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
				strings.Contains(capture, collapsedCodexToolbar) &&
				!strings.Contains(capture, "C-c")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-c")
		time.Sleep(250 * time.Millisecond)
		if clientExited(clientDone) {
			t.Fatal("C-c in Task Console should not quit Weft")
		}
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 1 &&
				st.Focus == state.FocusCodex &&
				st.Agents[0].Status == state.StatusRunning &&
				strings.Contains(st.Agents[0].CodexTitle, "Ready")
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, collapsedCodexToolbar) &&
				!strings.Contains(capture, "C-c")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-c")
		time.Sleep(250 * time.Millisecond)
		if clientExited(clientDone) {
			t.Fatal("C-c in ready Task Console should still not quit Weft")
		}
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 1 &&
				st.Focus == state.FocusCodex &&
				st.Agents[0].Status == state.StatusRunning &&
				strings.Contains(st.Agents[0].CodexTitle, "Ready")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-b")
		waitState(t, env, bin, func(st state.State) bool {
			return st.Focus == state.FocusAgents && st.NavOpen
		})
	})
}

func TestAgentConsoleCtrlCExitRecoveryE2E(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	fakeCodex := writeExitOnInterruptFakeCodex(t, tmp, "fake-codex-exits-on-int.sh")
	runtimeDir, workspace := createRuntime(t, tmp, fakeCodex)
	configText := fmt.Sprintf("codex_command = %q\ntitle_template = %q\n", fakeCodex, "{codex}")
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	env := baseIntegrationEnv(runtimeDir, workspace, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	pane := "ctrl-c-exit-client"
	clientOutput, clientDone := startDirectDashboardClient(t, env, bin, workspace, pane, 120, 32)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Add this workspace to Weft?") &&
			strings.Contains(capture, "Enter yes") &&
			strings.Contains(capture, "Esc no")
	})
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Tasks") &&
			strings.Contains(capture, "No task open")
	})

	directRun(t, env, "send-keys", "-t", pane, "n")
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	st := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 &&
			st.Focus == state.FocusCodex &&
			strings.Contains(st.Agents[0].CodexTitle, "Ready")
	})
	firstAgentID := st.Agents[0].ID
	capture := waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Exit-on-interrupt Codex Ready") &&
			strings.Contains(capture, collapsedCodexToolbar)
	})
	if strings.Contains(capture, "C-c") {
		t.Fatalf("Task Console toolbar should not mention C-c:\n%s", capture)
	}

	directRun(t, env, "send-keys", "-t", pane, "C-c")
	st = waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 &&
			st.ActiveAgentID == firstAgentID &&
			st.Focus == state.FocusAgents &&
			st.NavOpen &&
			st.Agents[0].Status == state.StatusKilled &&
			st.Agents[0].CodexTitle == "Codex killed"
	})
	if clientExited(clientDone) {
		t.Fatal("C-c that exits Codex should not quit Weft")
	}
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Tasks") &&
			strings.Contains(capture, "Codex killed")
	})

	directRun(t, env, "send-keys", "-t", pane, "n")
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	st = waitState(t, env, bin, func(st state.State) bool {
		active := state.ActiveAgent(st)
		return len(st.Agents) == 2 &&
			active != nil &&
			active.ID != firstAgentID &&
			st.Focus == state.FocusCodex &&
			strings.Contains(active.CodexTitle, "Ready")
	})
	secondAgentID := st.ActiveAgentID
	directRun(t, env, "send-keys", "-t", pane, "C-b")
	waitState(t, env, bin, func(st state.State) bool {
		return st.ActiveAgentID == secondAgentID &&
			st.Focus == state.FocusAgents &&
			st.NavOpen
	})
	if clientExited(clientDone) {
		t.Fatal("C-b dashboard return should keep Weft attached")
	}
}

func TestAgentConsoleCtrlCSideWorkE2E(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	fakeCodex := writeSideModeFakeCodex(t, tmp, "fake-codex-side-mode.sh")
	runtimeDir, workspace := createRuntime(t, tmp, fakeCodex)
	configText := fmt.Sprintf("codex_command = %q\ntitle_template = %q\n", fakeCodex, "{codex}")
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	env := baseIntegrationEnv(runtimeDir, workspace, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	pane := "ctrl-c-side-client"
	clientOutput, clientDone := startDirectDashboardClient(t, env, bin, workspace, pane, 120, 32)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Add this workspace to Weft?") &&
			strings.Contains(capture, "Enter yes") &&
			strings.Contains(capture, "Esc no")
	})
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Tasks") &&
			strings.Contains(capture, "No task open")
	})

	directRun(t, env, "send-keys", "-t", pane, "n")
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 &&
			st.Focus == state.FocusCodex &&
			strings.Contains(st.Agents[0].CodexTitle, "Ready")
	})
	directRun(t, env, "send-keys", "-l", "-t", pane, "/side")
	time.Sleep(100 * time.Millisecond)
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "You are in a side conversation") &&
			strings.Contains(capture, collapsedCodexToolbar) &&
			!strings.Contains(capture, "C-c")
	})

	directRun(t, env, "send-keys", "-l", "-t", pane, "side-work")
	time.Sleep(100 * time.Millisecond)
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 &&
			st.Focus == state.FocusCodex &&
			st.Agents[0].Status == state.StatusRunning &&
			strings.Contains(st.Agents[0].CodexTitle, "Working")
	})
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "received:side-work") &&
			strings.Contains(capture, "awaiting side interrupt")
	})
	directRun(t, env, "send-keys", "-t", pane, "C-c")
	var wrongCapture string
	if waitForBool(1*time.Second, func() bool {
		wrongCapture = clientOutput()
		return strings.Contains(wrongCapture, "raw ctrl-c returned main thread") ||
			strings.Contains(wrongCapture, "enhanced ctrl-c returned main thread") ||
			strings.Contains(wrongCapture, "Codex killed")
	}) {
		t.Fatalf("C-c during side work returned/closed the side agent instead of interrupting it:\n%s", wrongCapture)
	}
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "interrupted side work") &&
			strings.Contains(capture, "side prompt") &&
			!strings.Contains(capture, "raw ctrl-c returned main thread") &&
			!strings.Contains(capture, "enhanced ctrl-c returned main thread") &&
			!strings.Contains(capture, "Codex killed") &&
			strings.Contains(capture, collapsedCodexToolbar) &&
			!strings.Contains(capture, "C-c")
	})
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 &&
			st.Focus == state.FocusCodex &&
			st.Agents[0].Status == state.StatusRunning &&
			strings.Contains(st.Agents[0].CodexTitle, "Ready")
	})
	if clientExited(clientDone) {
		t.Fatal("C-c during side work should not quit Weft")
	}
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
				strings.Contains(capture, "Enter yes") &&
				strings.Contains(capture, "Esc no")
		})
		directRun(t, env, "send-keys", "-t", pane, "Escape")
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
			return strings.Contains(capture, "No tasks") &&
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
				strings.Contains(capture, "Enter add") &&
				strings.Contains(capture, "Tab choose")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Enter add") ||
				(strings.Contains(capture, "Workspaces") &&
					strings.Contains(capture, "Tasks") &&
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
		directRun(t, env, "send-keys", "-t", pane, "e")
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

		directRun(t, env, "send-keys", "-t", pane, "e")
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
	var secondAgentID string
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

		directRun(t, env, "send-keys", "-t", pane, "e")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Group")
		})
		directRun(t, env, "send-keys", "-t", pane, "C-u")
		directRun(t, env, "send-keys", "-l", "-t", pane, "renamed")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			group := groupByPath(st, "renamed")
			return group != nil
		})
		directRun(t, env, "send-keys", "-t", pane, "e")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Group") && strings.Contains(capture, "renamed")
		})
		directRun(t, env, "send-keys", "-t", pane, "Tab")
		directRun(t, env, "send-keys", "-t", pane, "Space")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			group := groupByPath(st, "renamed")
			return group != nil && group.Silent
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "⊘ renamed")
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
		directRun(t, env, "send-keys", "-t", pane, "Enter")
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
			return strings.Contains(capture, "Move task")
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
		directRun(t, env, "send-keys", "-t", pane, "Backspace")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete group") &&
				strings.Contains(capture, "renamed")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			group := groupByPath(st, "renamed")
			agent := findAgent(st, firstAgentID)
			return group != nil && agent != nil && agent.GroupID == group.ID
		})

		directRun(t, env, "send-keys", "-t", pane, "j")
		time.Sleep(250 * time.Millisecond)
		directRun(t, env, "send-keys", "-t", pane, "m")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Move task") &&
				strings.Contains(capture, "Top-level task") &&
				strings.Contains(capture, "Esc close suggestions")
		})
		directRun(t, env, "send-keys", "-t", pane, "Escape")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Move task") &&
				strings.Contains(capture, "Enter top-level")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			agent := findAgent(st, firstAgentID)
			return agent != nil && agent.GroupID == ""
		})

		directRun(t, env, "send-keys", "-t", pane, "j")
		time.Sleep(250 * time.Millisecond)
		directRun(t, env, "send-keys", "-t", pane, "Backspace")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete group") &&
				strings.Contains(capture, "renamed")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Groups) == 0
		})

		directRun(t, env, "send-keys", "-t", pane, "n")
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			active := state.ActiveAgent(st)
			if active != nil {
				secondAgentID = active.ID
			}
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
		directRun(t, env, "send-keys", "-t", pane, "S-Up")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 2 &&
				secondAgentID != "" &&
				st.Agents[0].ID == secondAgentID &&
				st.Agents[1].ID == firstAgentID &&
				st.ActiveAgentID == secondAgentID
		})
		directRun(t, env, "send-keys", "-t", pane, "S-Down")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Agents) == 2 &&
				st.Agents[0].ID == firstAgentID &&
				st.Agents[1].ID == secondAgentID &&
				st.ActiveAgentID == secondAgentID
		})
		directRun(t, env, "send-keys", "-t", pane, "k")
		time.Sleep(250 * time.Millisecond)
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			first := findAgent(st, firstAgentID)
			second := findAgent(st, secondAgentID)
			return st.ActiveAgentID == firstAgentID &&
				st.Focus == state.FocusCodex &&
				first != nil &&
				second != nil &&
				strings.Contains(first.CodexTitle, "Ready") &&
				strings.Contains(second.CodexTitle, "Ready")
		})
		capture := waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, collapsedCodexToolbar) &&
				strings.Contains(capture, "1 other task ready")
		})
		if strings.Contains(capture, "Task Console  WEFT") {
			t.Fatalf("Task Console title should not include WEFT branding:\n%s", capture)
		}
	})

	timedStep(t, "refresh and second attach preserve selected running agent", func() {
		out := runWeft(t, env, bin, "refresh")
		if !strings.Contains(out, "refreshed Weft dashboard") {
			t.Fatalf("refresh output missing message:\n%s", out)
		}
		clientOutput, _ = startDirectDashboardClient(t, env, bin, alpha, pane+"-reattach", 150, 36)
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, collapsedCodexToolbar) ||
				(strings.Contains(capture, "Workspaces") && strings.Contains(capture, "Tasks"))
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

		directRun(t, env, "send-keys", "-t", pane, "Backspace")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete workspace")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
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

		directRun(t, env, "send-keys", "-t", pane, "Backspace")
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "Delete workspace")
		})
		directRun(t, env, "send-keys", "-t", pane, "Enter")
		waitState(t, env, bin, func(st state.State) bool {
			return len(st.Workspaces) == 0 && len(st.Groups) == 0 && len(st.Agents) == 0
		})
		waitForOutput(t, clientOutput, func(capture string) bool {
			return strings.Contains(capture, "No workspaces") &&
				strings.Contains(capture, "No task open")
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
			strings.Contains(capture, "Enter yes") &&
			strings.Contains(capture, "Esc no")
	})
	assertPerformanceBudget(t, "fresh dashboard prompt visible", time.Since(started), 8*time.Second)

	started = time.Now()
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Tasks") &&
			strings.Contains(capture, "No task open")
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
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Starting Codex") &&
			strings.Contains(capture, collapsedCodexToolbar) &&
			!strings.Contains(capture, "C-c")
	})
	assertPerformanceBudget(t, "task startup placeholder visible", time.Since(started), time.Second)

	waitForOutput(t, clientOutput, func(capture string) bool {
		return (strings.Contains(capture, "Fake Codex dashboard ready") ||
			strings.Contains(capture, "Ready waiting for first message")) &&
			strings.Contains(capture, collapsedCodexToolbar) &&
			!strings.Contains(capture, "C-c")
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
		return strings.Contains(capture, collapsedCodexToolbar) ||
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
	case "Backspace":
		writeClientInput(t, "\x7f")
	case "Up":
		writeClientInput(t, "\x1b[A")
	case "Down":
		writeClientInput(t, "\x1b[B")
	case "S-Up":
		writeClientInput(t, "\x1b[1;2A")
	case "S-Down":
		writeClientInput(t, "\x1b[1;2B")
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
	if strings.Contains(capture, "C-c interrupt") || strings.Contains(capture, "C-c quit") {
		t.Fatalf("Task Console should not advertise C-c ownership:\n%s", capture)
	}
	if !empty && strings.Contains(capture, "No task open") {
		t.Fatalf("dashboard kept empty state after agent was created:\n%s", capture)
	}
}

func assertClientEnablesMouseTracking(t *testing.T, capture string) {
	t.Helper()
	for _, expected := range []string{"\x1b[?1003h", "\x1b[?1006h"} {
		if !strings.Contains(capture, expected) {
			t.Fatalf("client should enable mouse tracking for Workspaces hover, Task Console scrollback, and drag-copy support; missing %q in raw capture", expected)
		}
	}
}

func containsReverseVideo(capture string) bool {
	for _, marker := range []string{"\x1b[7m", "\x1b[7;", ";7m", ";7;"} {
		if strings.Contains(capture, marker) {
			return true
		}
	}
	return false
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
	markers := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏", "·", "◦", "!"}
	for _, line := range strings.Split(capture, "\n") {
		if !strings.Contains(line, title) {
			continue
		}
		for _, marker := range markers {
			if strings.Contains(line, marker+" ") {
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

func writeScrollbackFakeCodex(t *testing.T, dir string, name string) string {
	t.Helper()
	fakeCodex := filepath.Join(dir, name)
	if err := os.WriteFile(fakeCodex, []byte(
		"#!/bin/bash\n"+
			"printf '\\033]2;Fake Codex Ready\\007'\n"+
			"i=1\n"+
			"while [ \"$i\" -le 80 ]; do printf 'history line %02d\\r\\n' \"$i\"; i=$((i + 1)); done\n"+
			"trap 'stty sane 2>/dev/null; exit 0' HUP INT TERM\n"+
			"stty raw -echo -isig\n"+
			"while :; do sleep 1; done\n",
	), 0o700); err != nil {
		t.Fatal(err)
	}
	return fakeCodex
}

func writeExitOnInterruptFakeCodex(t *testing.T, dir string, name string) string {
	t.Helper()
	fakeCodex := filepath.Join(dir, name)
	if err := os.WriteFile(fakeCodex, []byte(
		"#!/bin/bash\n"+
			"printf '\\033]2;Exit-on-interrupt Codex Ready\\007'\n"+
			"printf 'Exit-on-interrupt Codex Ready\\n'\n"+
			"trap 'exit 0' HUP TERM\n"+
			"trap 'exit 130' INT\n"+
			"saved_stty=$(stty -g)\n"+
			"stty raw -echo -isig\n"+
			"while IFS= read -r -s -n 1 ch; do\n"+
			"  if [ \"$ch\" = $'\\003' ]; then stty \"$saved_stty\"; exit 130; fi\n"+
			"  if [ \"$ch\" = $'\\033' ]; then\n"+
			"    seq=$ch\n"+
			"    while IFS= read -r -s -n 1 -t 1 next; do\n"+
			"      seq=$seq$next\n"+
			"      if [ \"$next\" = \"u\" ] || [ \"$next\" = \"~\" ]; then break; fi\n"+
			"    done\n"+
			"    if [ \"$seq\" = $'\\033[99;5u' ] || [ \"$seq\" = $'\\033[99;5:1u' ] || [ \"$seq\" = $'\\033[27;5;99~' ]; then stty \"$saved_stty\"; exit 130; fi\n"+
			"  fi\n"+
			"done\n"+
			"stty \"$saved_stty\"\n"+
			"exit 130\n",
	), 0o700); err != nil {
		t.Fatal(err)
	}
	return fakeCodex
}

func writeSideModeFakeCodex(t *testing.T, dir string, name string) string {
	t.Helper()
	fakeCodex := filepath.Join(dir, name)
	if err := os.WriteFile(fakeCodex, []byte(
		"#!/bin/bash\n"+
			"printf '\\033]2;Side-mode Codex Ready\\007'\n"+
			"printf 'Side-mode Codex Ready\\n'\n"+
			"trap 'exit 0' HUP TERM\n"+
			"side_mode=0\n"+
			"read_side_interrupt() {\n"+
			"  local ch next seq\n"+
			"  if IFS= read -r -s -n 1 -t 8 ch; then\n"+
			"    if [ \"$ch\" = $'\\003' ]; then\n"+
			"      side_mode=0\n"+
			"      printf '\\033]2;Side-mode Codex Ready\\007'\n"+
			"      printf '\\033[2J\\033[Hraw ctrl-c returned main thread\\n'\n"+
			"      return 0\n"+
			"    fi\n"+
			"    if [ \"$ch\" = $'\\033' ]; then\n"+
			"      seq=$ch\n"+
			"      while IFS= read -r -s -n 1 -t 1 next; do\n"+
			"        seq=$seq$next\n"+
			"        if [ \"$next\" = \"u\" ] || [ \"$next\" = \"~\" ]; then break; fi\n"+
			"      done\n"+
			"      if [ \"$seq\" = $'\\033[99;5u' ] || [ \"$seq\" = $'\\033[99;5:1u' ] || [ \"$seq\" = $'\\033[27;5;99~' ]; then\n"+
			"        side_mode=0\n"+
			"        printf '\\033]2;Side-mode Codex Ready\\007'\n"+
			"        printf '\\033[2J\\033[Henhanced ctrl-c returned main thread\\n'\n"+
			"        return 0\n"+
			"      fi\n"+
			"      if [ \"$seq\" = $'\\033' ]; then\n"+
			"        printf '\\033]2;Side-mode Codex Ready\\007'\n"+
			"        printf '\\033[2J\\033[Hinterrupted side work\\nside prompt\\n'\n"+
			"        return 0\n"+
			"      fi\n"+
			"    fi\n"+
			"  fi\n"+
			"  printf '\\033]2;Side-mode Codex Ready\\007'\n"+
			"  printf '\\033[2J\\033[Hside work timed out\\n'\n"+
			"  return 1\n"+
			"}\n"+
			"while IFS= read -r line; do\n"+
			"  if [ \"$line\" = \"/side\" ]; then\n"+
			"    side_mode=1\n"+
			"    printf '\\033]2;Side-mode Codex Ready\\007'\n"+
			"    printf '\\033[2J\\033[H'\n"+
			"    printf 'Side conversation boundary.\\n'\n"+
			"    printf 'You are in a side conversation, not the main thread.\\n'\n"+
			"    printf 'side prompt\\n'\n"+
			"    continue\n"+
			"  fi\n"+
			"  printf '\\033]2;Side-mode Codex Working\\007'\n"+
			"  printf '\\033[2J\\033[Hreceived:%s\\n' \"$line\"\n"+
			"  if [ \"$line\" = \"side-work\" ] && [ \"$side_mode\" -eq 1 ]; then\n"+
			"    saved_stty=$(stty -g)\n"+
			"    stty raw -echo -isig\n"+
			"    printf 'awaiting side interrupt\\n'\n"+
			"    read_side_interrupt\n"+
			"    stty \"$saved_stty\"\n"+
			"    continue\n"+
			"  fi\n"+
			"  printf '\\033]2;Side-mode Codex Ready\\007'\n"+
			"done\n"+
			"while :; do sleep 1; done\n",
	), 0o700); err != nil {
		t.Fatal(err)
	}
	return fakeCodex
}
