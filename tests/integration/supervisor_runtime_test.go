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

func TestSupervisorRuntimeWithoutTmux(t *testing.T) {
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
		"PATH=/usr/bin:/bin",
		"TERM=xterm-256color",
	)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	runWeft(t, env, bin, "--no-attach")
	waitFor(t, "supervisor socket", 8*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(runtimeDir, "weft.sock"))
		return err == nil
	})
	status := runWeft(t, env, bin, "status")
	for _, expected := range []string{"supervisor: running", "supervisor version:", "upgrade: current"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("status missing %q:\n%s", expected, status)
		}
	}
	if out := runWeft(t, env, bin, "doctor"); !strings.Contains(out, "supervisor owns Codex PTYs") {
		t.Fatalf("doctor output missing supervisor ownership:\n%s", out)
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
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 2 && st.ActiveAgentID == firstID
	})
	runWeft(t, env, bin, "close", firstID)
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1
	})
}

func TestUpgradeSimulationNoRunningAgentsRestartsSupervisor(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir, workdir := createRuntime(t, tmp, writeFakeCodex(t, tmp, "fake-codex.sh"))
	oldEnv := upgradeEnv(runtimeDir, workdir, bin, "3.9.0")
	newEnv := baseIntegrationEnv(runtimeDir, workdir, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = newEnv
		_ = cmd.Run()
	})

	runWeft(t, oldEnv, bin, "--no-attach")
	oldPID := readPID(t, runtimeDir)
	out := runWeft(t, newEnv, bin, "--no-attach")
	newPID := readPID(t, runtimeDir)
	if oldPID == newPID {
		t.Fatalf("idle upgrade should restart supervisor, pid stayed %q\n%s", oldPID, out)
	}
	if !strings.Contains(out, "Supervisor restarted from Weft 3.9.0") {
		t.Fatalf("idle upgrade output missing restart notice:\n%s", out)
	}
}

func TestUpgradeSimulationWithRunningAgentPreservesSupervisor(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir, workdir := createRuntime(t, tmp, writeFakeCodex(t, tmp, "fake-codex.sh"))
	oldEnv := upgradeEnv(runtimeDir, workdir, bin, "3.9.0")
	newEnv := baseIntegrationEnv(runtimeDir, workdir, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = newEnv
		_ = cmd.Run()
	})

	runWeft(t, oldEnv, bin, "--no-attach")
	oldPID := readPID(t, runtimeDir)
	runWeft(t, oldEnv, bin, "new", "Alpha")
	waitState(t, oldEnv, bin, func(st state.State) bool {
		return len(st.Agents) == 1 && st.Agents[0].Status == state.StatusRunning
	})

	out := runWeft(t, newEnv, bin, "--no-attach")
	newPID := readPID(t, runtimeDir)
	if oldPID != newPID {
		t.Fatalf("running-agent upgrade should preserve supervisor, old pid %q new pid %q\n%s", oldPID, newPID, out)
	}
	if !strings.Contains(out, "1 live Codex terminal") {
		t.Fatalf("running upgrade output missing live-terminal warning:\n%s", out)
	}
	status := runWeft(t, newEnv, bin, "status")
	if !strings.Contains(status, "supervisor version: 3.9.0") || !strings.Contains(status, "upgrade: restart pending, 1 live Codex terminal") {
		t.Fatalf("status missing upgrade details:\n%s", status)
	}

	cancel := exec.Command(bin, "close", "--kill")
	cancel.Env = newEnv
	cancel.Stdin = strings.NewReader("n\n")
	cancelOut, err := cancel.CombinedOutput()
	if err != nil {
		t.Fatalf("weft close --kill cancel: %v\n%s", err, cancelOut)
	}
	if !strings.Contains(string(cancelOut), "Close canceled.") {
		t.Fatalf("close cancellation output missing:\n%s", cancelOut)
	}
	if after := readPID(t, runtimeDir); after != oldPID {
		t.Fatalf("canceled close should preserve supervisor, pid %q -> %q", oldPID, after)
	}
}

func TestStartClearNoAttachClearsStateAndRestartsSupervisor(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir, workdir := createRuntime(t, tmp, writeFakeCodex(t, tmp, "fake-codex.sh"))
	env := baseIntegrationEnv(runtimeDir, workdir, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	runWeft(t, env, bin, "--no-attach")
	runWeft(t, env, bin, "new", "Alpha")
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 1 && st.Agents[0].Status == state.StatusRunning
	})

	out := runWeft(t, env, bin, "--clear", "--no-attach")
	for _, expected := range []string{"Deleted", "Started Weft supervisor."} {
		if !strings.Contains(out, expected) {
			t.Fatalf("--clear --no-attach output missing %q:\n%s", expected, out)
		}
	}
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Agents) == 0 && len(st.Workdirs) == 1
	})
}

func buildWeft(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "weft")
	build := exec.Command("go", "build", "-o", bin, "./cmd/weft")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func createRuntime(t *testing.T, tmp string, fakeCodex string) (string, string) {
	t.Helper()
	runtimeDir := filepath.Join(tmp, "weft-home")
	workdir := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workdir, 0o700); err != nil {
		t.Fatal(err)
	}
	configText := fmt.Sprintf("codex_command = %q\n", fakeCodex)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	return runtimeDir, workdir
}

func writeFakeCodex(t *testing.T, dir string, name string) string {
	t.Helper()
	fakeCodex := filepath.Join(dir, name)
	if err := os.WriteFile(fakeCodex, []byte(
		"#!/bin/sh\n"+
			"printf '\\033]2;Fake Codex Ready\\007'\n"+
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

func baseIntegrationEnv(runtimeDir string, workdir string, bin string) []string {
	return append(os.Environ(),
		"WEFT_HOME="+runtimeDir,
		"WEFT_WORKDIR="+workdir,
		"WEFT_EXECUTABLE="+bin,
		"PATH=/usr/bin:/bin",
		"TERM=xterm-256color",
	)
}

func upgradeEnv(runtimeDir string, workdir string, bin string, version string) []string {
	return append(baseIntegrationEnv(runtimeDir, workdir, bin),
		"WEFT_CLIENT_VERSION_OVERRIDE="+version,
		"WEFT_SUPERVISOR_VERSION_OVERRIDE="+version,
	)
}

func readPID(t *testing.T, runtimeDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(runtimeDir, "weftd.pid"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
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
