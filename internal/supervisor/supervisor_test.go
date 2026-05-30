package supervisor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	if status.State == nil || len(status.State.Workdirs) != 0 {
		t.Fatalf("status state = %#v", status.State)
	}
	if status.Message == "" {
		t.Fatal("status message is empty")
	}

	response, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "bogus"}, time.Second)
	if err == nil {
		t.Fatal("bogus command succeeded")
	}
	if response.Error == nil || response.Error.Code != "unknown_command" {
		t.Fatalf("structured error = %#v, err = %v", response.Error, err)
	}
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

func TestUpgradeStatusDecisions(t *testing.T) {
	st := state.Empty()
	id := "agent"
	now := state.NowISO()
	st.Workdirs = []state.Workdir{{ID: "w", Path: "/tmp/work", CreatedAt: now, UpdatedAt: now}}
	st.Agents = []state.Agent{{ID: id, WorkdirID: "w", Title: "Alpha", Status: state.StatusRunning, CreatedAt: now, UpdatedAt: now}}
	response := ipc.Response{OK: true, State: &st, ProtocolVersion: ipc.ProtocolVersion, SupervisorVersion: "3.9.0"}

	upgrade := UpgradeStatus(response, "4.0.0")
	if upgrade == nil {
		t.Fatal("expected upgrade status")
	}
	if !upgrade.Compatible || !upgrade.RestartRequired || upgrade.RunningAgents != 1 {
		t.Fatalf("upgrade status = %#v", upgrade)
	}
	response.Upgrade = upgrade
	if ShouldAutoRestart(response) {
		t.Fatal("running agents must block automatic restart")
	}

	st.Agents[0].Status = state.StatusReady
	response = ipc.Response{OK: true, State: &st, ProtocolVersion: ipc.ProtocolVersion, SupervisorVersion: "3.9.0"}
	response = AnnotateUpgrade(response, false)
	if ShouldAutoRestart(response) {
		t.Fatal("ready agents must block automatic restart")
	}

	st.Agents[0].Status = state.StatusStopped
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
	cfg.CodexCommand = fakeCodex
	stop := runTestSupervisor(t, rt, cfg, store)
	defer stop()

	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "add_workspace", Args: map[string]string{"path": rt.Workdir}}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "new", Args: map[string]string{"title": "Fake"}}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	var agentID string
	waitFor(t, "fake codex running", func() bool {
		response, err := Status(rt)
		if err != nil || response.State == nil || len(response.State.Agents) != 1 {
			return false
		}
		agentID = response.State.Agents[0].ID
		return response.State.Agents[0].Status == state.StatusRunning && fileExists(pidPath)
	})

	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "snapshot"}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "new", Args: map[string]string{"title": "Second"}}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second fake codex running", func() bool {
		response, err := Status(rt)
		return err == nil && response.State != nil && len(response.State.Agents) == 2
	})
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "select", Args: map[string]string{"id": agentID}}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	selected, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "snapshot"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Snapshot == nil || selected.Snapshot.State.ActiveAgentID != agentID {
		t.Fatalf("selected snapshot active agent = %#v, want %s", selected.Snapshot, agentID)
	}
	if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "focus", Args: map[string]string{"target": string(state.FocusCodex)}}, 2*time.Second); err != nil {
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
	workdir := filepath.Join(dir, "work")
	runtimeDir := filepath.Join(dir, "home")
	if err := os.MkdirAll(workdir, 0o700); err != nil {
		t.Fatal(err)
	}
	rt := config.Runtime{
		Workdir:    workdir,
		Dir:        runtimeDir,
		ConfigPath: filepath.Join(runtimeDir, "config.toml"),
		StatePath:  filepath.Join(runtimeDir, "state.json"),
		SocketPath: filepath.Join(runtimeDir, "weft.sock"),
	}
	cfg, err := config.EnsureConfig(rt)
	if err != nil {
		t.Fatal(err)
	}
	store := state.NewStore(rt.StatePath, rt.Workdir)
	return rt, cfg, store
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
