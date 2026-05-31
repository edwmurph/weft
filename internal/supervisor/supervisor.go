package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/runtimebackup"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/tui"
	"github.com/edwmurph/weft/internal/version"
)

const (
	CommandName        = "_supervisor"
	ClientVersionEnv   = "WEFT_CLIENT_VERSION_OVERRIDE"
	VersionOverrideEnv = "WEFT_SUPERVISOR_VERSION_OVERRIDE"
	lockFile           = "weftd.lock"
	logFile            = "weftd.log"
	pidFile            = "weftd.pid"
)

var ErrAlreadyRunning = errors.New("weft supervisor is already running")

type EnsureResult struct {
	Started bool
	Status  ipc.Response
}

func Ensure(rt config.Runtime) (EnsureResult, error) {
	if response, err := Status(rt); err == nil {
		response = AnnotateUpgrade(response, false)
		if ShouldAutoRestart(response) {
			backup, err := runtimebackup.Create(rt, runtimebackup.Options{Reason: "pre-upgrade auto restart", IncludeLogs: true})
			if err != nil {
				return EnsureResult{}, fmt.Errorf("could not create pre-restart backup: %w", err)
			}
			_ = Shutdown(rt)
			waitForStop(rt, 2*time.Second)
			started, err := start(rt)
			if err != nil {
				return EnsureResult{}, err
			}
			started.Status.Upgrade = restartedUpgrade(response, started.Status)
			started.Status.Upgrade.BackupID = backup.ID
			if started.Status.Upgrade.Message != "" {
				started.Status.Upgrade.Message += " Backup: " + backup.ID + "."
			}
			return started, nil
		}
		return EnsureResult{Status: response}, nil
	} else if hasSupervisorResponse(response) {
		return EnsureResult{Status: response}, upgradeError(response, err)
	}
	return start(rt)
}

func start(rt config.Runtime) (EnsureResult, error) {
	if err := os.MkdirAll(rt.Dir, 0o700); err != nil {
		return EnsureResult{}, err
	}
	exe := os.Getenv("WEFT_EXECUTABLE")
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return EnsureResult{}, err
		}
	}
	log, err := os.OpenFile(LogPath(rt), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return EnsureResult{}, err
	}
	cmd := exec.Command(exe, CommandName)
	cmd.Env = supervisorEnv(rt, exe)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return EnsureResult{}, err
	}
	_ = cmd.Process.Release()
	_ = log.Close()

	deadline := time.Now().Add(4 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := Status(rt)
		if err == nil {
			return EnsureResult{Started: true, Status: AnnotateUpgrade(response, false)}, nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return EnsureResult{}, fmt.Errorf("started Weft supervisor but it did not become ready: %w", lastErr)
}

func Run(ctx context.Context, rt config.Runtime, cfg config.Config, store *state.Store) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(rt.Dir, 0o700); err != nil {
		return err
	}
	lock, err := acquireLock(LockPath(rt))
	if err != nil {
		return err
	}
	defer lock.Close()

	st, err := store.Ensure()
	if err != nil {
		return err
	}
	engine := tui.NewModel(rt, cfg, st)
	defer engine.Stop()
	if err := os.WriteFile(PIDPath(rt), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		return err
	}
	defer os.Remove(PIDPath(rt))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopSignals := notifySignals(ctx, cancel)
	defer stopSignals()
	var mu sync.Mutex
	clients := clientCoordinator{}
	stop, err := ipc.Serve(rt.SocketPath, func(request ipc.Request) ipc.Response {
		mu.Lock()
		response, cmd := handleRequest(&engine, &clients, request, cancel)
		mu.Unlock()
		tui.RunEngineCmd(cmd, &engine, &mu)
		return response
	})
	if err != nil {
		return err
	}
	defer stop()

	for {
		select {
		case data := <-engine.Data():
			mu.Lock()
			engine.ApplyPTYData(data)
			mu.Unlock()
		case <-ctx.Done():
			return nil
		}
	}
}

type clientCoordinator struct {
	activeID string
	detachID string
	message  string
}

func notifySignals(ctx context.Context, cancel context.CancelFunc) func() {
	terminate := make(chan os.Signal, 2)
	hangup := make(chan os.Signal, 1)
	signal.Notify(terminate, syscall.SIGINT, syscall.SIGTERM)
	signal.Notify(hangup, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-terminate:
				cancel()
			case <-hangup:
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() {
		signal.Stop(terminate)
		signal.Stop(hangup)
		cancel()
		<-done
	}
}

func Status(rt config.Runtime) (ipc.Response, error) {
	response, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "status"}, time.Second)
	return AnnotateUpgrade(response, false), err
}

func Shutdown(rt config.Runtime) error {
	_, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "shutdown"}, time.Second)
	return err
}

func LockPath(rt config.Runtime) string {
	return filepath.Join(rt.Dir, lockFile)
}

func LogPath(rt config.Runtime) string {
	return filepath.Join(rt.Dir, logFile)
}

func PIDPath(rt config.Runtime) string {
	return filepath.Join(rt.Dir, pidFile)
}

func acquireLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return file, nil
}

func handleRequest(engine *tui.Model, clients *clientCoordinator, request ipc.Request, cancel context.CancelFunc) (ipc.Response, tea.Cmd) {
	if request.ProtocolVersion != 0 && request.ProtocolVersion != ipc.ProtocolVersion {
		return withSupervisorFields(ipc.ErrorResponse("protocol_mismatch", fmt.Sprintf("unsupported protocol version %d", request.ProtocolVersion))), nil
	}
	switch request.Command {
	case "attach_client":
		clientID := request.Args["client_id"]
		if clientID == "" {
			return withSupervisorFields(ipc.ErrorResponse("missing_client_id", "client_id is required")), nil
		}
		if strings.TrimSpace(request.Args["launch_workspace"]) != "" {
			engine.HandleSupervisorRequest(ipc.Request{Command: "snapshot", Args: request.Args})
		}
		if clients.activeID != "" && clients.activeID != clientID {
			clients.detachID = clients.activeID
			clients.message = "another Weft client attached"
		}
		clients.activeID = clientID
		response := ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "attached Weft client"}
		applyClientState(&response, clients, clientID)
		return withSupervisorFields(response), nil
	case "client_detached":
		clientID := request.Args["client_id"]
		if clients.activeID == clientID {
			clients.activeID = ""
		}
		if clients.detachID == clientID {
			clients.detachID = ""
			clients.message = ""
		}
		return withSupervisorFields(ipc.Response{OK: true, Message: "detached Weft client"}), nil
	case "close_client":
		clientID := clients.activeID
		if clientID == "" {
			return withSupervisorFields(ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "No Weft client is attached."}), nil
		}
		clients.detachID = clientID
		clients.message = "closed Weft client"
		response := ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "closed Weft client"}
		applyClientState(&response, clients, clientID)
		return withSupervisorFields(response), nil
	case "handshake":
		response := ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "Weft supervisor is running"}
		applyClientState(&response, clients, request.Args["client_id"])
		return withSupervisorFields(response), nil
	case "shutdown":
		go cancel()
		return withSupervisorFields(ipc.Response{OK: true, Message: "Weft supervisor stopped"}), nil
	default:
		response, cmd := engine.HandleSupervisorRequest(request)
		applyClientState(&response, clients, request.Args["client_id"])
		return withSupervisorFields(response), cmd
	}
}

func snapshotPtr(snapshot ipc.Snapshot) *ipc.Snapshot {
	return &snapshot
}

func applyClientState(response *ipc.Response, clients *clientCoordinator, clientID string) {
	if response.Snapshot == nil {
		return
	}
	response.Snapshot.ActiveClientID = clients.activeID
	if clientID != "" && clients.detachID == clientID {
		response.Snapshot.DetachClient = true
		if clients.message != "" {
			response.Snapshot.Message = clients.message
		}
	}
}

func withSupervisorFields(response ipc.Response) ipc.Response {
	response.ProtocolVersion = ipc.ProtocolVersion
	response.SupervisorVersion = ReportedVersion()
	return response
}

func ReportedVersion() string {
	if override := os.Getenv(VersionOverrideEnv); strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	return version.Version
}

func ReportedClientVersion() string {
	if override := os.Getenv(ClientVersionEnv); strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	return version.Version
}

func AnnotateUpgrade(response ipc.Response, autoRestarted bool) ipc.Response {
	if response.SupervisorVersion == "" {
		return response
	}
	upgrade := UpgradeStatus(response, ReportedClientVersion())
	if upgrade == nil {
		return response
	}
	upgrade.AutoRestarted = autoRestarted
	if autoRestarted {
		upgrade.RestartRequired = false
		upgrade.Message = "Supervisor restarted on the new Weft version."
	}
	response.Upgrade = upgrade
	return response
}

func UpgradeStatus(response ipc.Response, clientVersion string) *ipc.Upgrade {
	supervisorVersion := response.SupervisorVersion
	if supervisorVersion == "" || supervisorVersion == clientVersion {
		return nil
	}
	running := runningAgentCount(response.State)
	compatible := response.ProtocolVersion == ipc.ProtocolVersion
	message := upgradeMessage(supervisorVersion, clientVersion, running)
	if !compatible {
		message = incompatibleUpgradeMessage(supervisorVersion, clientVersion, running)
	}
	return &ipc.Upgrade{
		ClientVersion:     clientVersion,
		SupervisorVersion: supervisorVersion,
		Compatible:        compatible,
		RestartRequired:   true,
		RunningAgents:     running,
		Message:           message,
	}
}

func ShouldAutoRestart(response ipc.Response) bool {
	return response.Upgrade != nil &&
		response.State != nil &&
		response.Upgrade.Compatible &&
		response.Upgrade.RestartRequired &&
		response.Upgrade.RunningAgents == 0
}

func upgradeMessage(supervisorVersion string, clientVersion string, runningAgents int) string {
	if runningAgents == 0 {
		return fmt.Sprintf("Weft client %s found idle supervisor %s; restarting the supervisor is safe.", clientVersion, supervisorVersion)
	}
	return fmt.Sprintf("Weft client %s found running supervisor %s. Restarting the supervisor will stop %d live Codex terminal(s). Saved layout and metadata remain. Run `weft close --kill` when ready; it will ask before stopping them.", clientVersion, supervisorVersion, runningAgents)
}

func incompatibleUpgradeMessage(supervisorVersion string, clientVersion string, runningAgents int) string {
	if runningAgents == 0 {
		return fmt.Sprintf("Weft client %s found incompatible supervisor %s. Saved layout and metadata remain.", clientVersion, supervisorVersion)
	}
	return fmt.Sprintf("Weft client %s found incompatible supervisor %s. Restarting the supervisor will stop %d live Codex terminal(s). Saved layout and metadata remain.", clientVersion, supervisorVersion, runningAgents)
}

func restartedUpgrade(previous ipc.Response, current ipc.Response) *ipc.Upgrade {
	clientVersion := ReportedClientVersion()
	message := "Supervisor restarted on the new Weft version."
	if previous.SupervisorVersion != "" && current.SupervisorVersion != "" {
		message = fmt.Sprintf("Supervisor restarted from Weft %s to %s.", previous.SupervisorVersion, current.SupervisorVersion)
	}
	return &ipc.Upgrade{
		ClientVersion:     clientVersion,
		SupervisorVersion: current.SupervisorVersion,
		Compatible:        true,
		RestartRequired:   false,
		AutoRestarted:     true,
		RunningAgents:     0,
		Message:           message,
	}
}

func runningAgentCount(st *state.State) int {
	if st == nil {
		return 0
	}
	count := 0
	for _, agent := range st.Agents {
		switch agent.Status {
		case state.StatusStarting, state.StatusRunning, state.StatusReady, state.StatusSitting, state.StatusShipping:
			count++
		}
	}
	return count
}

func hasSupervisorResponse(response ipc.Response) bool {
	return response.SupervisorVersion != "" || response.ProtocolVersion != 0 || response.Error != nil
}

func upgradeError(response ipc.Response, err error) error {
	if response.Upgrade != nil && !response.Upgrade.Compatible {
		return fmt.Errorf("%s Run `weft close --kill` when ready to restart the supervisor; saved layout and metadata remain.", response.Upgrade.Message)
	}
	if response.Error != nil {
		return *response.Error
	}
	return err
}

func waitForStop(rt config.Runtime, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "handshake"}, 100*time.Millisecond); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func supervisorEnv(rt config.Runtime, exe string) []string {
	env := append([]string{}, os.Environ()...)
	env = upsertEnv(env, config.AppDirEnv, rt.Dir)
	env = upsertEnv(env, config.WorkspaceEnv, rt.Workspace)
	env = upsertEnv(env, "WEFT_EXECUTABLE", exe)
	return env
}

func upsertEnv(env []string, key string, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
