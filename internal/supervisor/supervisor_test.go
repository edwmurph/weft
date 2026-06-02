package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/version"
)

func TestSupervisorServesHandshakeStatusAndStructuredErrors(t *testing.T) {
	rt, cfg, store := testRuntime(t)
	stop := runTestSupervisor(t, rt, cfg, store)
	defer stop()

	handshake, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "handshake"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if handshake.ProtocolVersion != ipc.ProtocolVersion {
		t.Fatalf("protocol version = %d", handshake.ProtocolVersion)
	}
	if handshake.SupervisorVersion != version.Version {
		t.Fatalf("supervisor version = %q", handshake.SupervisorVersion)
	}

	status, err := Status(rt)
	if err != nil {
		t.Fatal(err)
	}
	if status.State == nil || len(status.State.Workspaces) != 0 {
		t.Fatalf("status state = %#v", status.State)
	}
	if status.Message == "" {
		t.Fatal("status message is empty")
	}

	_, err = ipc.Call(rt.SocketPath, ipc.Request{
		Command:       "attach_client",
		ClientVersion: "7.8.0",
		ClientID:      "dashboard-1",
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	status, err = Status(rt)
	if err != nil {
		t.Fatal(err)
	}
	if status.Snapshot == nil || status.Snapshot.ActiveClientVersion != "7.8.0" {
		t.Fatalf("active dashboard version = %#v", status.Snapshot)
	}

	response, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "bogus"}, time.Second)
	if err == nil {
		t.Fatal("bogus command succeeded")
	}
	if response.Error == nil || response.Error.Code != "unknown_command" {
		t.Fatalf("structured error = %#v, err = %v", response.Error, err)
	}
}

func TestSupervisorRejectsRawProtocolMismatch(t *testing.T) {
	rt, cfg, store := testRuntime(t)
	stop := runTestSupervisor(t, rt, cfg, store)
	defer stop()

	for _, tc := range []struct {
		name    string
		request map[string]any
	}{
		{
			name:    "missing protocol",
			request: map[string]any{"command": "handshake"},
		},
		{
			name:    "unsupported protocol",
			request: map[string]any{"protocol_version": ipc.ProtocolVersion + 1, "command": "handshake"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := rawSupervisorCall(t, rt.SocketPath, tc.request)
			if response.OK {
				t.Fatalf("raw request succeeded: %#v", response)
			}
			if response.Error == nil || response.Error.Code != "protocol_mismatch" {
				t.Fatalf("structured error = %#v", response.Error)
			}
			if response.ProtocolVersion != ipc.ProtocolVersion {
				t.Fatalf("response protocol version = %d", response.ProtocolVersion)
			}
			if response.SupervisorVersion != version.Version {
				t.Fatalf("supervisor version = %q", response.SupervisorVersion)
			}
		})
	}
}

func TestShutdownStopsIncompatibleSupervisorByPID(t *testing.T) {
	rt, _, _ := testRuntime(t)
	if err := os.MkdirAll(rt.Dir, 0o700); err != nil {
		t.Fatal(err)
	}

	readyPath := filepath.Join(rt.Dir, "helper.ready")
	cmd := exec.Command(os.Args[0], "-test.run=TestIncompatibleSupervisorProcessHelper")
	cmd.Env = append(os.Environ(),
		"WEFT_TEST_INCOMPATIBLE_SUPERVISOR_PROCESS=1",
		"WEFT_TEST_LOCK_PATH="+LockPath(rt),
		"WEFT_TEST_PID_PATH="+PIDPath(rt),
		"WEFT_TEST_READY_PATH="+readyPath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if waited {
			return
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	waitFor(t, "helper supervisor process", func() bool {
		return fileExists(readyPath)
	})

	stop, err := ipc.Serve(rt.SocketPath, func(request ipc.Request) ipc.Response {
		return ipc.Response{
			OK:                false,
			Message:           "unsupported protocol version 2",
			Error:             &ipc.Error{Code: "protocol_mismatch", Message: "unsupported protocol version 2"},
			ProtocolVersion:   1,
			SupervisorVersion: "0.3.3",
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	if err := Shutdown(rt); err != nil {
		t.Fatalf("shutdown should signal incompatible supervisor process: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case <-done:
		waited = true
	case <-time.After(2 * time.Second):
		t.Fatal("incompatible supervisor helper did not stop")
	}
	if fileExists(PIDPath(rt)) {
		t.Fatalf("pid file should be removed after shutdown")
	}
	if fileExists(rt.SocketPath) {
		t.Fatalf("socket should be removed after shutdown")
	}
}

func TestIncompatibleSupervisorProcessHelper(t *testing.T) {
	if os.Getenv("WEFT_TEST_INCOMPATIBLE_SUPERVISOR_PROCESS") != "1" {
		return
	}
	lock, err := acquireLock(os.Getenv("WEFT_TEST_LOCK_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.WriteFile(os.Getenv("WEFT_TEST_PID_PATH"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("WEFT_TEST_READY_PATH"), []byte("ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}

func TestSupervisorRejectsSecondRuntimeForSameHome(t *testing.T) {
	rt, cfg, store := testRuntime(t)
	stop := runTestSupervisor(t, rt, cfg, store)
	defer stop()

	err := Run(context.Background(), rt, cfg, store)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Run error = %v, want ErrAlreadyRunning", err)
	}
}

func TestSupervisorRejectsStateTaskTypesMissingFromConfig(t *testing.T) {
	rt, cfg, store := testRuntime(t)
	now := state.NowISO()
	raw := `{
  "version": 5,
  "selected_workspace_id": "w",
  "focus": "tasks",
  "nav_open": true,
  "workspaces": [{"id": "w", "path": "` + rt.Workspace + `", "created_at": "` + now + `", "updated_at": "` + now + `"}],
  "groups": [],
  "tasks": [{"id": "a", "workspace_id": "w", "group_id": "", "type_id": "ghost", "title": "Ghost", "status": "stopped", "created_at": "` + now + `", "updated_at": "` + now + `"}],
  "collapsed_group_ids": []
}`
	if err := os.WriteFile(rt.StatePath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Run(context.Background(), rt, cfg, store)
	if err == nil {
		t.Fatal("expected undefined task type error")
	}
	for _, expected := range []string{`tasks[0].type_id "ghost" is not defined in active config`, "run `weft clear` to reset"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error missing %q: %v", expected, err)
		}
	}
}

func TestRepeatedAttachDoesNotReselectLaunchWorkspace(t *testing.T) {
	rt, cfg, store := testRuntime(t)
	stop := runTestSupervisor(t, rt, cfg, store)
	defer stop()

	other := filepath.Join(filepath.Dir(rt.Workspace), "other")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "add_workspace", Args: map[string]string{"path": rt.Workspace}}, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "add_workspace", Args: map[string]string{"path": other}}, time.Second); err != nil {
		t.Fatal(err)
	}

	attach := ipc.Request{
		Command:         "attach_client",
		ClientID:        "dashboard-1",
		LaunchWorkspace: rt.Workspace,
	}
	if _, err := ipc.Call(rt.SocketPath, attach, time.Second); err != nil {
		t.Fatal(err)
	}
	status, err := Status(rt)
	if err != nil {
		t.Fatal(err)
	}
	if selected := state.WorkspaceByID(*status.State, status.State.SelectedWorkspaceID); selected == nil || selected.Path != rt.Workspace {
		t.Fatalf("initial attach should select launch workspace: %#v", status.State)
	}

	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "focus", Args: map[string]string{"target": string(state.FocusWorkspaces)}}, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "nav_move", Args: map[string]string{"delta": "1"}}, time.Second); err != nil {
		t.Fatal(err)
	}
	status, err = Status(rt)
	if err != nil {
		t.Fatal(err)
	}
	otherWorkspace := state.WorkspaceByPath(*status.State, other)
	if otherWorkspace == nil || status.State.SelectedWorkspaceID != otherWorkspace.ID {
		t.Fatalf("nav move should select other workspace: %#v", status.State)
	}

	if _, err := ipc.Call(rt.SocketPath, attach, time.Second); err != nil {
		t.Fatal(err)
	}
	status, err = Status(rt)
	if err != nil {
		t.Fatal(err)
	}
	if status.State.SelectedWorkspaceID != otherWorkspace.ID {
		t.Fatalf("repeated attach reselected launch workspace: %#v", status.State)
	}
}

func TestUpgradeStatusDecisions(t *testing.T) {
	st := state.Empty()
	id := "task"
	now := state.NowISO()
	st.Workspaces = []state.Workspace{{ID: "w", Path: "/tmp/work", CreatedAt: now, UpdatedAt: now}}
	st.Tasks = []state.Task{{ID: id, WorkspaceID: "w", Title: "Alpha", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now}}
	response := ipc.Response{OK: true, State: &st, ProtocolVersion: ipc.ProtocolVersion, SupervisorVersion: "3.9.0"}

	upgrade := ipc.UpgradeStatus(response, "4.0.0")
	if upgrade == nil {
		t.Fatal("expected upgrade status")
	}
	if !upgrade.Compatible || !upgrade.RestartRequired || upgrade.RunningTasks != 1 {
		t.Fatalf("upgrade status = %#v", upgrade)
	}
	response.Upgrade = upgrade
	if ShouldAutoRestart(response) {
		t.Fatal("running tasks must block automatic restart")
	}

	st.Tasks[0].Status = state.StatusReady
	response = ipc.Response{OK: true, State: &st, ProtocolVersion: ipc.ProtocolVersion, SupervisorVersion: "3.9.0"}
	response = AnnotateUpgrade(response, false)
	if ShouldAutoRestart(response) {
		t.Fatal("ready tasks must block automatic restart")
	}

	st.Tasks[0].Status = state.StatusStopped
	response = ipc.Response{OK: true, State: &st, ProtocolVersion: ipc.ProtocolVersion, SupervisorVersion: "3.9.0"}
	response = AnnotateUpgrade(response, false)
	if !ShouldAutoRestart(response) {
		t.Fatalf("idle compatible supervisor should auto restart: %#v", response.Upgrade)
	}

	response.State = nil
	if ShouldAutoRestart(response) {
		t.Fatal("unknown state must not auto restart")
	}
}

func TestUpgradeStatusRejectsIncompatibleProtocol(t *testing.T) {
	st := state.Empty()
	response := ipc.Response{OK: true, State: &st, ProtocolVersion: ipc.ProtocolVersion + 1, SupervisorVersion: "3.9.0"}
	response = AnnotateUpgrade(response, false)
	if response.Upgrade == nil {
		t.Fatal("expected upgrade status")
	}
	if response.Upgrade.Compatible {
		t.Fatalf("incompatible protocol marked compatible: %#v", response.Upgrade)
	}
	if ShouldAutoRestart(response) {
		t.Fatal("incompatible protocol must not auto restart")
	}
}

func TestUpgradeResumeRestartMessageIncludesShellRestart(t *testing.T) {
	got := upgradeResumeRestartMessage(1, 1, 2, "backup-1")

	for _, expected := range []string{
		"resuming 1 idle Codex task(s)",
		"starting 1 fresh Codex task(s)",
		"restarting 2 idle shell task(s) with saved history/cwd",
		"Backup: backup-1",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("message missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "resume shell") {
		t.Fatalf("message should not imply shell resume:\n%s", got)
	}
}

func TestSupervisorOwnsPTYAndAcceptsCodexInput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required")
	}
	rt, cfg, store := testRuntime(t)
	pidPath := filepath.Join(rt.Dir, "fake-codex.pid")
	inputPath := filepath.Join(rt.Dir, "fake-codex-input.log")
	fakeCodex := filepath.Join(rt.Dir, "fake-codex.sh")
	if err := os.WriteFile(fakeCodex, []byte(
		"#!/bin/sh\n"+
			"echo $$ > "+shellQuote(pidPath)+"\n"+
			"printf '\\033]2;Fake Codex Ready\\007'\n"+
			"trap 'exit 0' TERM HUP INT\n"+
			"while IFS= read -r line; do\n"+
			"  echo \"$line\" >> "+shellQuote(inputPath)+"\n"+
			"  printf 'received:%s\\n' \"$line\"\n"+
			"done\n"+
			"while :; do sleep 1; done\n",
	), 0o700); err != nil {
		t.Fatal(err)
	}
	codexType := cfg.TaskTypes[config.DefaultTaskTypeCodex]
	codexType.Command = fakeCodex
	cfg.TaskTypes[config.DefaultTaskTypeCodex] = codexType
	stop := runTestSupervisor(t, rt, cfg, store)
	defer stop()

	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "add_workspace", Args: map[string]string{"path": rt.Workspace}}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "new", Args: map[string]string{"title": "Fake"}}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	var taskID string
	waitFor(t, "fake codex running", func() bool {
		response, err := Status(rt)
		if err != nil || response.State == nil || len(response.State.Tasks) != 1 {
			return false
		}
		taskID = response.State.Tasks[0].ID
		return response.State.Tasks[0].Status == state.StatusRunning && fileExists(pidPath)
	})

	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "snapshot"}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "new", Args: map[string]string{"title": "Second"}}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second fake codex running", func() bool {
		response, err := Status(rt)
		return err == nil && response.State != nil && len(response.State.Tasks) == 2
	})
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "select", Args: map[string]string{"id": taskID}}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	selected, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "snapshot"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Snapshot == nil || selected.Snapshot.State.ActiveTaskID != taskID {
		t.Fatalf("selected snapshot active task = %#v, want %s", selected.Snapshot, taskID)
	}
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "focus", Args: map[string]string{"target": string(state.FocusConsole)}}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	for _, request := range []ipc.Request{
		{Command: "codex_input", Args: map[string]string{"encoded": "hello", "input": "text", "text": "hello"}},
		{Command: "codex_input", Args: map[string]string{"encoded": "\r", "input": "enter"}},
	} {
		if _, err := ipc.Call(rt.SocketPath, request, 2*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "fake codex input", func() bool {
		data, err := os.ReadFile(inputPath)
		return err == nil && strings.Contains(string(data), "hello")
	})
}

func runTestSupervisor(t *testing.T, rt config.Runtime, cfg config.Config, store *state.Store) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, rt, cfg, store)
	}()
	waitFor(t, "supervisor status", func() bool {
		_, err := Status(rt)
		return err == nil
	})
	return func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("supervisor exited with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("supervisor did not stop")
		}
	}
}

func testRuntime(t *testing.T) (config.Runtime, config.Config, *state.Store) {
	t.Helper()
	dir := t.TempDir()
	workspace := filepath.Join(dir, "work")
	runtimeDir := filepath.Join(dir, "home")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	rt := config.Runtime{
		Workspace:  workspace,
		Dir:        runtimeDir,
		ConfigPath: filepath.Join(runtimeDir, "config.toml"),
		StatePath:  filepath.Join(runtimeDir, "state.json"),
		SocketPath: filepath.Join(runtimeDir, "weft.sock"),
	}
	cfg, err := config.EnsureConfig(rt)
	if err != nil {
		t.Fatal(err)
	}
	store := state.NewStore(rt.StatePath)
	return rt, cfg, store
}

func rawSupervisorCall(t *testing.T, socketPath string, request map[string]any) ipc.Response {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Dir(socketPath)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatal(err)
		}
	}()
	conn, err := net.DialTimeout("unix", filepath.Base(socketPath), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		t.Fatal(err)
	}
	var response ipc.Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}

func waitFor(t *testing.T, name string, accept func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if accept() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", name)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
