package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/edwmurph/weft/internal/state"
)

func TestSinglePaneTUITmuxRuntime(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live tmux integration tests")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required")
	}

	root := repoRoot(t)
	tmp := t.TempDir()
	runID := "weft-it-" + strings.ReplaceAll(fmt.Sprintf("%d", time.Now().UnixNano()), "-", "")
	bin := filepath.Join(tmp, "weft")
	build := exec.Command("go", "build", "-o", bin, "./cmd/weft")
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

	runtimeDir := filepath.Join(tmp, "weft-home")
	workdir := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workdir, 0o700); err != nil {
		t.Fatal(err)
	}
	fakeCodex := filepath.Join(tmp, "fake-codex.sh")
	fakeLog := filepath.Join(tmp, "fake-codex.log")
	if err := os.WriteFile(fakeCodex, []byte(
		"#!/bin/sh\n"+
			"printf '\\033]2;Fake Codex Ready\\007'\n"+
			"echo started >> "+shellQuote(fakeLog)+"\n"+
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
	configText := fmt.Sprintf("tmux_session = %q\ncodex_command = %q\n", runID, fakeCodex)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"WEFT_HOME="+runtimeDir,
		"WEFT_WORKDIR="+workdir,
		"PATH="+wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TERM=xterm-256color",
	)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill")
		cmd.Env = env
		_ = cmd.Run()
		_ = exec.Command("tmux", "-L", runID, "kill-server").Run()
	})

	runWeft(t, env, bin, "--no-attach")
	if !waitForBool(time.Second*8, func() bool {
		_, err := os.Stat(filepath.Join(runtimeDir, "weft.sock"))
		return err == nil
	}) {
		capture := exec.Command(tmuxFromEnv(env), "capture-pane", "-p", "-t", runID)
		capture.Env = env
		out, _ := capture.CombinedOutput()
		log, _ := os.ReadFile(filepath.Join(runtimeDir, "weft.log"))
		t.Fatalf("timed out waiting for IPC socket; log:\n%s\ntmux pane:\n%s", log, out)
	}
	if panes := tmuxLines(t, env, "list-panes", "-t", runID+":", "-F", "#{pane_id}"); len(panes) != 1 {
		t.Fatalf("pane count = %d (%v), want 1", len(panes), panes)
	}

	runWeft(t, env, bin, "new", "Alpha")
	first := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 && st.Agents[0].Status == state.StatusRunning
	})
	firstID := first.Agents[0].ID
	runWeft(t, env, bin, "group", "add", "release")
	runWeft(t, env, bin, "new", "Beta")
	runWeft(t, env, bin, "move-right")
	runWeft(t, env, bin, "rename", "Renamed")
	runWeft(t, env, bin, "select", firstID)
	afterOps := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 2 && st.ActiveAgentID == firstID
	})
	foundRenamed := false
	for index := range afterOps.Agents {
		agent := &afterOps.Agents[index]
		folder := folderForAgent(afterOps, agent)
		if agent.Title == "Renamed" && folder != nil && folder.Path == "release" {
			foundRenamed = true
		}
	}
	if !foundRenamed {
		t.Fatalf("renamed agent not found in release group: %#v", afterOps)
	}

	runWeft(t, env, bin, "close")
	if panes := tmuxLines(t, env, "list-panes", "-t", runID+":", "-F", "#{pane_id}"); len(panes) != 1 {
		t.Fatalf("pane count after detach = %d (%v), want 1", len(panes), panes)
	}
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 2
	})
	runWeft(t, env, bin, "close", firstID)
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1
	})
}

func runWeft(t *testing.T, env []string, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("weft %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func waitState(t *testing.T, env []string, bin string, accept func(state.State) bool) state.State {
	t.Helper()
	var last state.State
	waitFor(t, "state", time.Second*8, func() bool {
		out := runWeft(t, env, bin, "status", "--json")
		if err := json.Unmarshal([]byte(out), &last); err != nil {
			return false
		}
		return accept(last)
	})
	return last
}

func waitFor(t *testing.T, name string, timeout time.Duration, fn func() bool) {
	t.Helper()
	if waitForBool(timeout, fn) {
		return
	}
	t.Fatalf("timed out waiting for %s", name)
}

func waitForBool(timeout time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func tmuxLines(t *testing.T, env []string, args ...string) []string {
	t.Helper()
	cmd := exec.Command(tmuxFromEnv(env), args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		log := ""
		for _, item := range env {
			if strings.HasPrefix(item, "WEFT_HOME=") {
				data, _ := os.ReadFile(filepath.Join(strings.TrimPrefix(item, "WEFT_HOME="), "weft.log"))
				log = string(data)
			}
		}
		t.Fatalf("tmux %v: %v\nlog:\n%s\n%s", args, err, log, out)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func tmuxFromEnv(env []string) string {
	for _, item := range env {
		if strings.HasPrefix(item, "PATH=") {
			first := strings.Split(strings.TrimPrefix(item, "PATH="), string(os.PathListSeparator))[0]
			candidate := filepath.Join(first, "tmux")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return "tmux"
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	return root
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
