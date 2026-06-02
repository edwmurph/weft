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

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/runtimebackup"
	"github.com/edwmurph/weft/internal/state"
	weftversion "github.com/edwmurph/weft/internal/version"
)

func TestSupervisorRuntimeWithoutTmux(t *testing.T) {
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
	configText := codexTaskConfig(fakeCodex)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"WEFT_HOME="+runtimeDir,
		"WEFT_WORKSPACE="+workspace,
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
	versionOut := runWeft(t, env, bin, "version")
	for _, expected := range []string{"cli version: " + weftversion.Version, "supervisor version: " + weftversion.Version, "main dashboard version: not attached", fmt.Sprintf("protocol: cli %d, supervisor %d", ipc.ProtocolVersion, ipc.ProtocolVersion), "upgrade: current"} {
		if !strings.Contains(versionOut, expected) {
			t.Fatalf("version missing %q:\n%s", expected, versionOut)
		}
	}
	clientOutput, _ := startDirectDashboardClient(t, env, bin, workspace, "version-dashboard", 120, 30)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Add this workspace to Weft?") ||
			strings.Contains(capture, "Workspaces")
	})
	if !waitForBool(15*time.Second, func() bool {
		versionOut = runWeft(t, env, bin, "version")
		return strings.Contains(versionOut, "main dashboard version: "+weftversion.Version)
	}) {
		t.Fatalf("version missing attached dashboard version:\n%s", versionOut)
	}
	directRun(t, env, "send-keys", "-t", "version-dashboard", "Escape")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "CLI        "+weftversion.Version) &&
			strings.Contains(capture, "Supervisor "+weftversion.Version)
	})
	if out := runWeft(t, env, bin, "doctor"); !strings.Contains(out, "supervisor owns task PTYs") {
		t.Fatalf("doctor output missing supervisor ownership:\n%s", out)
	}

	runWeft(t, env, bin, "workspace", "add", workspace)
	runWeft(t, env, bin, "new", "Alpha")
	first := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Tasks) == 1 && st.Tasks[0].Status == state.StatusRunning
	})
	firstID := first.Tasks[0].ID
	runWeft(t, env, bin, "group", "add", "release")
	runWeft(t, env, bin, "new", "Beta")
	runWeft(t, env, bin, "move-right")
	runWeft(t, env, bin, "rename", "Renamed")
	runWeft(t, env, bin, "select", firstID)
	afterOps := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Tasks) == 2 && st.ActiveTaskID == firstID
	})
	statusJSON := runWeft(t, env, bin, "status", "--json")
	for _, expected := range []string{`"version":5`, `"active_task_id":"` + firstID + `"`, `"selected_task_id":`, `"tasks":[`, `"type_id":"codex"`} {
		if !strings.Contains(statusJSON, expected) {
			t.Fatalf("status json missing %q:\n%s", expected, statusJSON)
		}
	}
	foundRenamed := false
	for index := range afterOps.Tasks {
		task := &afterOps.Tasks[index]
		group := groupForTask(afterOps, task)
		if task.Title == "Renamed" && group != nil && group.Path == "release" {
			foundRenamed = true
		}
	}
	if !foundRenamed {
		t.Fatalf("renamed task not found in release group: %#v", afterOps)
	}

	runWeft(t, env, bin, "close")
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Tasks) == 2 && st.ActiveTaskID == firstID
	})
	runWeft(t, env, bin, "close", firstID)
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Tasks) == 1
	})
}

func TestSourceBuildDefaultRuntimeGuardFailsBeforeCreatingRuntime(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	workspace := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	env := append(filteredEnv("WEFT_ROOT", "WEFT_HOME", "WEFT_ALLOW_MAIN_RUNTIME"),
		"HOME="+home,
		"WEFT_WORKSPACE="+workspace,
		"PATH=/usr/bin:/bin",
		"TERM=xterm-256color",
	)
	cmd := exec.Command(bin, "--no-attach")
	cmd.Env = env
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("source build default runtime should fail:\n%s", out)
	}
	if !strings.Contains(string(out), "source builds refuse to use the default Weft runtime") {
		t.Fatalf("guard output missing refusal:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".weft")); !os.IsNotExist(err) {
		t.Fatalf("guard should not create default runtime, stat err = %v", err)
	}
}

func TestRootEnvLaunchesIsolatedRuntime(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	root := filepath.Join(tmp, "worktree")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	env := append(filteredEnv("WEFT_ROOT", "WEFT_HOME", "WEFT_WORKSPACE", "WEFT_ALLOW_MAIN_RUNTIME"),
		"HOME="+home,
		"WEFT_ROOT="+root,
		"PATH=/usr/bin:/bin",
		"TERM=xterm-256color",
	)

	runWeft(t, env, bin, "--no-attach")
	if _, err := os.Stat(filepath.Join(root, ".weft", "config.toml")); err != nil {
		t.Fatalf("root env should create root-local config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".weft")); !os.IsNotExist(err) {
		t.Fatalf("root env should not touch default home runtime, stat err = %v", err)
	}

	runWeft(t, env, bin, "workspace", "add", root)
	st := waitState(t, env, bin, func(st state.State) bool {
		return len(st.Workspaces) == 1
	})
	if got := st.Workspaces[0].Path; got != root {
		t.Fatalf("workspace path = %q, want root env path %q", got, root)
	}
}

func TestSourceCheckoutCWDLaunchesIsolatedRuntime(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	root := writeIntegrationSourceCheckout(t, filepath.Join(tmp, "worktree"))
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	fakeCodex := writeFakeCodex(t, tmp, "fake-codex.sh")
	runtimeDir := filepath.Join(root, ".weft-runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configText := codexTaskConfig(fakeCodex)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(filteredEnv("WEFT_ROOT", "WEFT_HOME", "WEFT_WORKSPACE", "WEFT_ALLOW_MAIN_RUNTIME"),
		"HOME="+home,
		"WEFT_EXECUTABLE="+bin,
		"PATH=/usr/bin:/bin",
		"TERM=xterm-256color",
	)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		cmd.Dir = root
		_ = cmd.Run()
	})

	runWeftInDir(t, env, root, bin, "--no-attach")
	if _, err := os.Stat(filepath.Join(runtimeDir, "weft.sock")); err != nil {
		t.Fatalf("source checkout cwd should create checkout-local supervisor socket: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".weft")); !os.IsNotExist(err) {
		t.Fatalf("source checkout cwd should not touch default home runtime, stat err = %v", err)
	}

	out := runWeftInDir(t, env, root, bin, "doctor")
	for _, expected := range []string{
		"info launch workspace: " + resolvedRoot,
		"info runtime dir: " + filepath.Join(resolvedRoot, ".weft-runtime"),
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("doctor output missing %q:\n%s", expected, out)
		}
	}
}

func TestUpgradeSimulationNoRunningTasksRestartsSupervisor(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir, workspace := createRuntime(t, tmp, writeFakeCodex(t, tmp, "fake-codex.sh"))
	oldEnv := upgradeEnv(runtimeDir, workspace, bin, "3.9.0")
	newEnv := baseIntegrationEnv(runtimeDir, workspace, bin)
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
	assertBackupWithReason(t, runtimeDir, workspace, "pre-upgrade auto restart")
}

func TestUpgradeSimulationWithRunningTaskPreservesSupervisor(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir, workspace := createRuntime(t, tmp, writeFakeCodex(t, tmp, "fake-codex.sh"))
	oldEnv := upgradeEnv(runtimeDir, workspace, bin, "3.9.0")
	newEnv := baseIntegrationEnv(runtimeDir, workspace, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = newEnv
		_ = cmd.Run()
	})

	runWeft(t, oldEnv, bin, "--no-attach")
	oldPID := readPID(t, runtimeDir)
	runWeft(t, oldEnv, bin, "workspace", "add", workspace)
	runWeft(t, oldEnv, bin, "new", "Alpha")
	waitState(t, oldEnv, bin, func(st state.State) bool {
		return len(st.Tasks) == 1 && st.Tasks[0].Status == state.StatusRunning
	})

	out := runWeft(t, newEnv, bin, "--no-attach")
	newPID := readPID(t, runtimeDir)
	if oldPID != newPID {
		t.Fatalf("running-task upgrade should preserve supervisor, old pid %q new pid %q\n%s", oldPID, newPID, out)
	}
	if !strings.Contains(out, "1 live task terminal") {
		t.Fatalf("running upgrade output missing live-terminal warning:\n%s", out)
	}
	status := runWeft(t, newEnv, bin, "status")
	if !strings.Contains(status, "supervisor version: 3.9.0") || !strings.Contains(status, "upgrade: upgrade pending, wait for idle/resumable tasks (1 live)") {
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

func TestDashboardUpgradeResumeRestartsAndResumesIdleTask(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	fakeCodex, codexHome, resumeLog := writeResumeFakeCodex(t, tmp, "fake-codex-resume.sh")
	runtimeDir, workspace := createRuntime(t, tmp, fakeCodex)
	codexEnv := []string{"CODEX_HOME=" + codexHome, "FAKE_CODEX_LOG=" + resumeLog}
	oldEnv := appendUniqueEnv(upgradeEnv(runtimeDir, workspace, bin, "3.9.0"), codexEnv...)
	newEnv := appendUniqueEnv(baseIntegrationEnv(runtimeDir, workspace, bin), codexEnv...)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = newEnv
		_ = cmd.Run()
	})

	runWeft(t, oldEnv, bin, "--no-attach")
	oldPID := readPID(t, runtimeDir)
	runWeft(t, oldEnv, bin, "workspace", "add", workspace)
	runWeft(t, oldEnv, bin, "new", "Alpha")
	st := waitState(t, oldEnv, bin, func(st state.State) bool {
		return len(st.Tasks) == 1 &&
			st.Tasks[0].Status == state.StatusRunning &&
			strings.Contains(st.Tasks[0].CodexTitle, "Ready")
	})
	if st.Tasks[0].CodexSessionID == "" {
		logData, _ := os.ReadFile(resumeLog)
		t.Fatalf("task did not capture Codex session id: %#v\nfake log:\n%s", st.Tasks[0], logData)
	}
	taskID := st.Tasks[0].ID
	sessionID := st.Tasks[0].CodexSessionID

	pane := "upgrade-resume"
	clientOutput, _ := startDirectDashboardClient(t, newEnv, bin, workspace, pane, 150, 36)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Task Console")
	})
	if capture := clientOutput(); strings.Contains(capture, "Upgrade:") {
		t.Fatalf("task console should not duplicate upgrade banner:\n%s", capture)
	}
	if pid := readPID(t, runtimeDir); pid != oldPID {
		t.Fatalf("dashboard attach should preserve old supervisor, pid %q -> %q", oldPID, pid)
	}
	directRun(t, newEnv, "send-keys", "-t", pane, "C-b")
	waitState(t, newEnv, bin, func(st state.State) bool {
		return st.Focus == state.FocusTasks && st.NavOpen
	})
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Workspaces") &&
			strings.Contains(capture, "CLI        "+weftversion.Version) &&
			strings.Contains(capture, "Supervisor 3.9.0") &&
			strings.Contains(capture, "supervisor 3.9.0 → "+weftversion.Version) &&
			strings.Contains(capture, "Press U to upgrade and resume 1 idle Codex task")
	})
	directRun(t, newEnv, "send-keys", "-t", pane, "u")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Upgrade supervisor?") &&
			strings.Contains(capture, "Enter upgrade") &&
			!strings.Contains(capture, "Y upgrade") &&
			!strings.Contains(capture, "N cancel") &&
			!strings.Contains(capture, "Esc cancel") &&
			strings.Contains(capture, "saved session IDs") &&
			strings.Contains(capture, "fresh Codex tasks without one") &&
			screenContainsWrappedText(capture, "Shell jobs, env mutations, shell variables, and unsubmitted input are not preserved.")
	})
	directRun(t, newEnv, "send-keys", "-t", pane, "Enter")
	if !waitForBool(8*time.Second, func() bool {
		data, err := os.ReadFile(filepath.Join(runtimeDir, "weftd.pid"))
		return err == nil && strings.TrimSpace(string(data)) != oldPID
	}) {
		t.Fatalf("supervisor did not restart after upgrade confirmation; pid still %q\nscreen:\n%s", oldPID, clientOutput())
	}
	st = waitState(t, newEnv, bin, func(st state.State) bool {
		task := state.TaskByID(st, taskID)
		return task != nil &&
			task.Status == state.StatusRunning &&
			task.CodexSessionID == sessionID &&
			strings.Contains(task.CodexTitle, "Ready")
	})
	if len(st.Tasks) != 1 {
		t.Fatalf("upgrade resume should preserve task rows: %#v", st.Tasks)
	}
	if !waitForBool(4*time.Second, func() bool {
		data, err := os.ReadFile(resumeLog)
		return err == nil && strings.Contains(string(data), "resume:"+sessionID)
	}) {
		data, _ := os.ReadFile(resumeLog)
		t.Fatalf("fake Codex was not resumed with session %q:\n%s", sessionID, data)
	}
	status := runWeft(t, newEnv, bin, "status")
	if !strings.Contains(status, "upgrade: current") {
		t.Fatalf("status should be current after upgrade resume:\n%s", status)
	}
	assertBackupWithReason(t, runtimeDir, workspace, "pre-upgrade resume restart")
}

func TestDashboardUpgradeRestartStartsFreshCodexWithoutSession(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	fakeCodex, codexLog := writeFreshFakeCodex(t, tmp, "fake-codex-fresh.sh")
	runtimeDir, workspace := createRuntime(t, tmp, fakeCodex)
	codexEnv := []string{"FAKE_CODEX_LOG=" + codexLog}
	oldEnv := appendUniqueEnv(upgradeEnv(runtimeDir, workspace, bin, "3.9.0"), codexEnv...)
	newEnv := appendUniqueEnv(baseIntegrationEnv(runtimeDir, workspace, bin), codexEnv...)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = newEnv
		_ = cmd.Run()
	})

	runWeft(t, oldEnv, bin, "--no-attach")
	oldPID := readPID(t, runtimeDir)
	runWeft(t, oldEnv, bin, "workspace", "add", workspace)
	runWeft(t, oldEnv, bin, "new", "Fresh")
	st := waitState(t, oldEnv, bin, func(st state.State) bool {
		return len(st.Tasks) == 1 &&
			st.Tasks[0].Status == state.StatusRunning &&
			strings.Contains(st.Tasks[0].CodexTitle, "Ready")
	})
	taskID := st.Tasks[0].ID
	if st.Tasks[0].CodexSessionID != "" {
		t.Fatalf("fresh task should not have a Codex session id: %#v", st.Tasks[0])
	}

	pane := "upgrade-fresh"
	clientOutput, _ := startDirectDashboardClient(t, newEnv, bin, workspace, pane, 150, 36)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Task Console")
	})
	if capture := clientOutput(); strings.Contains(capture, "Upgrade:") {
		t.Fatalf("task console should not duplicate upgrade banner:\n%s", capture)
	}
	directRun(t, newEnv, "send-keys", "-t", pane, "C-b")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Workspaces") &&
			strings.Contains(capture, "Press U to upgrade and start 1 fresh Codex task")
	})
	directRun(t, newEnv, "send-keys", "-t", pane, "u")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Upgrade supervisor?") &&
			strings.Contains(capture, "fresh Codex tasks without one")
	})
	directRun(t, newEnv, "send-keys", "-t", pane, "Enter")
	if !waitForBool(8*time.Second, func() bool {
		data, err := os.ReadFile(filepath.Join(runtimeDir, "weftd.pid"))
		return err == nil && strings.TrimSpace(string(data)) != oldPID
	}) {
		t.Fatalf("supervisor did not restart after fresh upgrade confirmation; pid still %q\nscreen:\n%s", oldPID, clientOutput())
	}
	st = waitState(t, newEnv, bin, func(st state.State) bool {
		task := state.TaskByID(st, taskID)
		return task != nil &&
			task.Status == state.StatusRunning &&
			task.CodexSessionID == "" &&
			strings.Contains(task.CodexTitle, "Ready")
	})
	if len(st.Tasks) != 1 {
		t.Fatalf("fresh upgrade should preserve task rows: %#v", st.Tasks)
	}
	data, err := os.ReadFile(codexLog)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(data)
	if strings.Contains(logText, "resume:") {
		t.Fatalf("fresh Codex task should not resume a missing session:\n%s", logText)
	}
	if strings.Count(logText, "start:") < 2 {
		t.Fatalf("fresh Codex task should start before and after restart:\n%s", logText)
	}
	assertBackupWithReason(t, runtimeDir, workspace, "pre-upgrade resume restart")
}

func TestDashboardConfigDriftRestartAppliesChangedConfig(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	fakeCodex := writeFakeCodex(t, tmp, "fake-codex.sh")
	runtimeDir, workspace := createRuntime(t, tmp, fakeCodex)
	env := baseIntegrationEnv(runtimeDir, workspace, bin)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = env
		_ = cmd.Run()
	})

	runWeft(t, env, bin, "--no-attach")
	oldPID := readPID(t, runtimeDir)
	runWeft(t, env, bin, "workspace", "add", workspace)
	runWeft(t, env, bin, "new", "Fresh")
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Tasks) == 1 &&
			st.Tasks[0].Status == state.StatusRunning &&
			strings.Contains(st.Tasks[0].CodexTitle, "Ready")
	})

	pane := "config-drift-restart"
	clientOutput, _ := startDirectDashboardClient(t, env, bin, workspace, pane, 150, 36)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Task Console")
	})
	directRun(t, env, "send-keys", "-t", pane, "C-b")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Workspaces")
	})

	changedConfig := codexTaskConfig(fakeCodex) + "\n[key_bindings]\nnew_task = \"t\"\n"
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(changedConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Config ready: config.toml changed") &&
			strings.Contains(capture, "Press U to apply config and start 1 fresh Codex task")
	})
	directRun(t, env, "send-keys", "-t", pane, "u")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Upgrade supervisor?") &&
			strings.Contains(capture, "config.toml changed")
	})
	directRun(t, env, "send-keys", "-t", pane, "Enter")
	if !waitForBool(8*time.Second, func() bool {
		data, err := os.ReadFile(filepath.Join(runtimeDir, "weftd.pid"))
		return err == nil && strings.TrimSpace(string(data)) != oldPID
	}) {
		t.Fatalf("supervisor did not restart after config reload confirmation; pid still %q\nscreen:\n%s", oldPID, clientOutput())
	}
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "+ New task") &&
			strings.Contains(capture, "Press t to create")
	})
	status := runWeft(t, env, bin, "status")
	if !strings.Contains(status, "upgrade: current") {
		t.Fatalf("status should be current after config reload:\n%s", status)
	}
	assertBackupWithReason(t, runtimeDir, workspace, "pre-config reload restart")
}

func TestDashboardUpgradeRestartsIdleDefaultShellWithSnapshot(t *testing.T) {
	if os.Getenv("WEFT_RUN_INTEGRATION") != "1" {
		t.Skip("set WEFT_RUN_INTEGRATION=1 to run live supervisor integration tests")
	}

	bin := buildWeft(t)
	tmp := t.TempDir()
	runtimeDir := filepath.Join(tmp, "weft-home")
	workspace := filepath.Join(tmp, "workspace")
	nested := filepath.Join(workspace, "nested")
	for _, dir := range []string{runtimeDir, workspace, nested} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	canonicalNested, err := filepath.EvalSymlinks(nested)
	if err != nil {
		t.Fatal(err)
	}
	fakeCodex := writeFakeCodex(t, tmp, "fake-codex.sh")
	fakeShell, shellLog := writeUpgradeFakeShell(t, tmp, "fake-shell.sh")
	configText := fmt.Sprintf(`
default_task_type = "shell"

[task_types.codex]
command = %q
`, fakeCodex)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	shellEnv := []string{"SHELL=" + fakeShell, "FAKE_SHELL_LOG=" + shellLog}
	oldEnv := appendUniqueEnv(upgradeEnv(runtimeDir, workspace, bin, "3.9.0"), shellEnv...)
	newEnv := appendUniqueEnv(baseIntegrationEnv(runtimeDir, workspace, bin), shellEnv...)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "close", "--kill", "--yes")
		cmd.Env = newEnv
		_ = cmd.Run()
	})

	runWeft(t, oldEnv, bin, "--no-attach")
	oldPID := readPID(t, runtimeDir)
	runWeft(t, oldEnv, bin, "workspace", "add", workspace)
	runWeft(t, oldEnv, bin, "new", "Shell")
	st := waitState(t, oldEnv, bin, func(st state.State) bool {
		return len(st.Tasks) == 1 && st.Tasks[0].TypeID == config.DefaultTaskTypeShell && st.Tasks[0].Status == state.StatusReady
	})
	taskID := st.Tasks[0].ID

	pane := "upgrade-shell"
	clientOutput, _ := startDirectDashboardClient(t, newEnv, bin, workspace, pane, 150, 36)
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Task Console") && strings.Contains(capture, "fake-shell:")
	})
	directRun(t, newEnv, "send-keys", "-l", "-t", pane, "history-before-upgrade")
	directRun(t, newEnv, "send-keys", "-t", pane, "Enter")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "ran:history-before-upgrade")
	})
	directRun(t, newEnv, "send-keys", "-l", "-t", pane, "cd nested")
	directRun(t, newEnv, "send-keys", "-t", pane, "Enter")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return screenContainsWrappedText(capture, "cwd:"+canonicalNested)
	})
	waitState(t, newEnv, bin, func(st state.State) bool {
		task := state.TaskByID(st, taskID)
		return task != nil && task.TerminalCWD == canonicalNested
	})

	directRun(t, newEnv, "send-keys", "-t", pane, "C-b")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Workspaces") &&
			screenContainsWrappedText(capture, "Press U to upgrade and restart 1 idle shell task(s) with saved history/cwd")
	})
	directRun(t, newEnv, "send-keys", "-t", pane, "u")
	waitForOutput(t, clientOutput, func(capture string) bool {
		return strings.Contains(capture, "Upgrade supervisor?") &&
			strings.Contains(capture, "restarts idle shell task(s) with saved history/cwd") &&
			screenContainsWrappedText(capture, "Shell jobs, env mutations, shell variables, and unsubmitted input are not preserved.")
	})
	directRun(t, newEnv, "send-keys", "-t", pane, "Enter")
	if !waitForBool(8*time.Second, func() bool {
		data, err := os.ReadFile(filepath.Join(runtimeDir, "weftd.pid"))
		return err == nil && strings.TrimSpace(string(data)) != oldPID
	}) {
		t.Fatalf("supervisor did not restart after shell upgrade confirmation; pid still %q\nscreen:\n%s", oldPID, clientOutput())
	}
	st = waitState(t, newEnv, bin, func(st state.State) bool {
		task := state.TaskByID(st, taskID)
		return task != nil && task.TypeID == config.DefaultTaskTypeShell && task.Status == state.StatusReady && task.TerminalCWD == canonicalNested
	})
	if len(st.Tasks) != 1 {
		t.Fatalf("shell upgrade should preserve task rows: %#v", st.Tasks)
	}
	logData, err := os.ReadFile(shellLog)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	for _, expected := range []string{"start:" + canonicalWorkspace, "start:" + canonicalNested} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("fake shell log missing %q:\n%s", expected, logText)
		}
	}

	afterOutput, _ := startDirectDashboardClient(t, newEnv, bin, workspace, pane+"-after", 150, 36)
	directRun(t, newEnv, "send-keys", "-t", pane+"-after", "Enter")
	waitForOutput(t, afterOutput, func(capture string) bool {
		return screenContainsWrappedText(capture, "history-before-upgrade") &&
			screenContainsWrappedText(capture, "restarted this idle shell task with saved history/cwd") &&
			strings.Contains(capture, "fake-shell:")
	})
	assertBackupWithReason(t, runtimeDir, workspace, "pre-upgrade resume restart")
}

func TestStartClearNoAttachClearsStateAndRestartsSupervisor(t *testing.T) {
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
	runWeft(t, env, bin, "new", "Alpha")
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Tasks) == 1 && st.Tasks[0].Status == state.StatusRunning
	})

	out := runWeft(t, env, bin, "--clear", "--no-attach")
	for _, expected := range []string{"Deleted", "Started Weft supervisor."} {
		if !strings.Contains(out, expected) {
			t.Fatalf("--clear --no-attach output missing %q:\n%s", expected, out)
		}
	}
	assertBackupWithReason(t, runtimeDir, workspace, "pre-clear")
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Tasks) == 0 && len(st.Workspaces) == 0
	})
}

func TestCloseKillCreatesBackup(t *testing.T) {
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
	runWeft(t, env, bin, "new", "Alpha")
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Tasks) == 1 && st.Tasks[0].Status == state.StatusRunning
	})

	out := runWeft(t, env, bin, "close", "--kill", "--yes")
	if !strings.Contains(out, "Created backup:") || !strings.Contains(out, "Weft supervisor stopped.") {
		t.Fatalf("close --kill output missing backup/stop notice:\n%s", out)
	}
	assertBackupWithReason(t, runtimeDir, workspace, "pre-close kill")
	status := runWeft(t, env, bin, "status")
	if !strings.Contains(status, "supervisor: down") {
		t.Fatalf("status should show stopped supervisor:\n%s", status)
	}
}

func TestBackupRestoreRequiresConfirmationWhenSupervisorRunning(t *testing.T) {
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
	createOut := runWeft(t, env, bin, "backup", "create", "--reason", "restore point")
	backupID := parseBackupID(t, createOut)
	runWeft(t, env, bin, "workspace", "add", workspace)
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Workspaces) == 1
	})

	cancel := exec.Command(bin, "backup", "restore", backupID)
	cancel.Env = env
	cancel.Stdin = strings.NewReader("n\n")
	cancelOut, err := cancel.CombinedOutput()
	if err != nil {
		t.Fatalf("backup restore cancel: %v\n%s", err, cancelOut)
	}
	if !strings.Contains(string(cancelOut), "Restore canceled.") {
		t.Fatalf("restore cancellation output missing:\n%s", cancelOut)
	}
	waitState(t, env, bin, func(st state.State) bool {
		return len(st.Workspaces) == 1
	})

	restoreOut := runWeft(t, env, bin, "backup", "restore", backupID, "--yes")
	for _, expected := range []string{"Created pre-restore backup:", "Restored Weft backup: " + backupID} {
		if !strings.Contains(restoreOut, expected) {
			t.Fatalf("restore output missing %q:\n%s", expected, restoreOut)
		}
	}
	assertBackupWithReason(t, runtimeDir, workspace, "pre-restore "+backupID)
	statusJSON := runWeft(t, env, bin, "status", "--json")
	var restored state.State
	if err := json.Unmarshal([]byte(statusJSON), &restored); err != nil {
		t.Fatalf("status json: %v\n%s", err, statusJSON)
	}
	if len(restored.Workspaces) != 0 {
		t.Fatalf("restore should return to backup state: %#v", restored.Workspaces)
	}
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
	workspace := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	configText := codexTaskConfig(fakeCodex)
	if err := os.WriteFile(filepath.Join(runtimeDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	return runtimeDir, workspace
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

func writeResumeFakeCodex(t *testing.T, dir string, name string) (string, string, string) {
	t.Helper()
	codexHome := filepath.Join(dir, "codex-home")
	resumeLog := filepath.Join(dir, "fake-codex.log")
	fakeCodex := filepath.Join(dir, name)
	if err := os.WriteFile(fakeCodex, []byte(
		"#!/bin/sh\n"+
			"mkdir -p \"$CODEX_HOME/sessions/2026/05/31\"\n"+
			"if [ \"$1\" = \"resume\" ]; then\n"+
			"  printf 'resume:%s\\n' \"$2\" >> \"$FAKE_CODEX_LOG\"\n"+
			"else\n"+
			"  sid=\"fake-$$\"\n"+
			"  ts=$(date -u '+%Y-%m-%dT%H:%M:%SZ')\n"+
			"  session=\"$CODEX_HOME/sessions/2026/05/31/rollout-$sid.jsonl\"\n"+
			"  printf '{\"type\":\"session_meta\",\"payload\":{\"id\":\"%s\",\"cwd\":\"%s\",\"timestamp\":\"%s\"}}\\n' \"$sid\" \"$PWD\" \"$ts\" > \"$session\"\n"+
			"  printf 'session:%s\\n' \"$sid\" >> \"$FAKE_CODEX_LOG\"\n"+
			"fi\n"+
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
	return fakeCodex, codexHome, resumeLog
}

func writeFreshFakeCodex(t *testing.T, dir string, name string) (string, string) {
	t.Helper()
	codexLog := filepath.Join(dir, "fake-codex-fresh.log")
	fakeCodex := filepath.Join(dir, name)
	if err := os.WriteFile(fakeCodex, []byte(
		"#!/bin/sh\n"+
			"if [ \"$1\" = \"resume\" ]; then\n"+
			"  printf 'resume:%s\\n' \"$2\" >> \"$FAKE_CODEX_LOG\"\n"+
			"else\n"+
			"  printf 'start:%s\\n' \"$$\" >> \"$FAKE_CODEX_LOG\"\n"+
			"fi\n"+
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
	return fakeCodex, codexLog
}

func writeUpgradeFakeShell(t *testing.T, dir string, name string) (string, string) {
	t.Helper()
	shellLog := filepath.Join(dir, "fake-shell.log")
	fakeShell := filepath.Join(dir, name)
	if err := os.WriteFile(fakeShell, []byte(
		"#!/bin/sh\n"+
			"if [ \"$1\" = \"-lc\" ]; then\n"+
			"  shift\n"+
			"  exec /bin/sh -lc \"$1\"\n"+
			"fi\n"+
			"emit_cwd() { printf '\\033]7;file://localhost%s\\007' \"$PWD\"; }\n"+
			"printf 'start:%s\\n' \"$PWD\" >> \"$FAKE_SHELL_LOG\"\n"+
			"emit_cwd\n"+
			"printf 'fake-shell:%s\\r\\n$ ' \"$PWD\"\n"+
			"trap 'exit 0' HUP INT TERM\n"+
			"while IFS= read -r line; do\n"+
			"  printf 'cmd:%s:%s\\n' \"$PWD\" \"$line\" >> \"$FAKE_SHELL_LOG\"\n"+
			"  case \"$line\" in\n"+
			"    cd\\ *)\n"+
			"      target=${line#cd }\n"+
			"      if cd \"$target\"; then\n"+
			"        emit_cwd\n"+
			"        printf 'cwd:%s\\r\\n$ ' \"$PWD\"\n"+
			"      else\n"+
			"        printf 'cd failed\\r\\n$ '\n"+
			"      fi\n"+
			"      ;;\n"+
			"    *)\n"+
			"      printf 'ran:%s\\r\\n$ ' \"$line\"\n"+
			"      ;;\n"+
			"  esac\n"+
			"done\n"+
			"while :; do sleep 1; done\n",
	), 0o700); err != nil {
		t.Fatal(err)
	}
	return fakeShell, shellLog
}

func baseIntegrationEnv(runtimeDir string, workspace string, bin string) []string {
	return append(os.Environ(),
		"WEFT_HOME="+runtimeDir,
		"WEFT_WORKSPACE="+workspace,
		"WEFT_EXECUTABLE="+bin,
		"PATH=/usr/bin:/bin",
		"TERM=xterm-256color",
	)
}

func filteredEnv(dropKeys ...string) []string {
	drop := map[string]bool{}
	for _, key := range dropKeys {
		drop[key+"="] = true
	}
	var env []string
	for _, item := range os.Environ() {
		skip := false
		for prefix := range drop {
			if strings.HasPrefix(item, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			env = append(env, item)
		}
	}
	return env
}

func appendUniqueEnv(env []string, values ...string) []string {
	drop := map[string]bool{}
	for _, value := range values {
		if index := strings.Index(value, "="); index >= 0 {
			drop[value[:index]+"="] = true
		}
	}
	next := make([]string, 0, len(env)+len(values))
	for _, item := range env {
		skip := false
		for prefix := range drop {
			if strings.HasPrefix(item, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			next = append(next, item)
		}
	}
	return append(next, values...)
}

func upgradeEnv(runtimeDir string, workspace string, bin string, version string) []string {
	return append(baseIntegrationEnv(runtimeDir, workspace, bin),
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

func runWeftInDir(t *testing.T, env []string, dir string, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("weft %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func writeIntegrationSourceCheckout(t *testing.T, root string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "weft"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/edwmurph/weft\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "weft", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertBackupWithReason(t *testing.T, runtimeDir string, workspace string, reason string) runtimebackup.Metadata {
	t.Helper()
	rt := config.Runtime{
		Workspace:  workspace,
		Dir:        runtimeDir,
		ConfigPath: filepath.Join(runtimeDir, "config.toml"),
		StatePath:  filepath.Join(runtimeDir, "state.json"),
		SocketPath: filepath.Join(runtimeDir, "weft.sock"),
	}
	backups, err := runtimebackup.List(rt)
	if err != nil {
		t.Fatal(err)
	}
	for _, backup := range backups {
		if backup.Reason == reason {
			return backup
		}
	}
	t.Fatalf("backup reason %q not found: %#v", reason, backups)
	return runtimebackup.Metadata{}
}

func parseBackupID(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Created Weft backup: ") {
			return strings.TrimPrefix(line, "Created Weft backup: ")
		}
	}
	t.Fatalf("backup id not found in output:\n%s", output)
	return ""
}

func waitState(t *testing.T, env []string, bin string, accept func(state.State) bool) state.State {
	t.Helper()
	var last state.State
	waitFor(t, "state", time.Second*12, func() bool {
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
