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
	"github.com/edwmurph/weft/internal/codexsession"
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

type restartRequest struct {
	executable    string
	clientVersion string
	backupID      string
	runningTasks  int
	freshTasks    int
	message       string
}

type supervisorControl struct {
	restartNow *restartRequest
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
	exe, err := os.Executable()
	if err != nil {
		return EnsureResult{}, err
	}
	if err := startSupervisorProcess(rt, exe, supervisorEnv(rt, exe)); err != nil {
		return EnsureResult{}, err
	}

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

func startSupervisorProcess(rt config.Runtime, exe string, env []string) error {
	log, err := os.OpenFile(LogPath(rt), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	resolvedExe, err := filepath.EvalSymlinks(exe)
	if err == nil && resolvedExe != "" {
		exe = resolvedExe
	}
	cmd := exec.Command(exe, CommandName)
	cmd.Env = env
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return err
	}
	_ = cmd.Process.Release()
	_ = log.Close()
	return nil
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

	st, err := store.Ensure()
	if err != nil {
		_ = lock.Close()
		return err
	}
	engine := tui.NewModel(rt, cfg, st)
	if err := os.WriteFile(PIDPath(rt), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		engine.Stop()
		_ = lock.Close()
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	stopSignals := notifySignals(ctx, cancel)
	var mu sync.Mutex
	clients := clientCoordinator{}
	control := supervisorControl{}
	stop, err := ipc.Serve(rt.SocketPath, func(request ipc.Request) ipc.Response {
		mu.Lock()
		response, cmd := handleRequest(rt, &engine, &clients, &control, request, cancel)
		mu.Unlock()
		tui.RunEngineCmd(cmd, &engine, &mu)
		return response
	})
	if err != nil {
		stopSignals()
		engine.Stop()
		_ = os.Remove(PIDPath(rt))
		_ = lock.Close()
		return err
	}

	for {
		select {
		case data := <-engine.Data():
			mu.Lock()
			engine.ApplyPTYData(data)
			mu.Unlock()
		case <-ctx.Done():
			_ = stop()
			stopSignals()
			engine.Stop()
			_ = os.Remove(PIDPath(rt))
			_ = lock.Close()
			if control.restartNow != nil {
				return startReplacementSupervisor(rt, *control.restartNow)
			}
			return nil
		}
	}
}

type clientCoordinator struct {
	activeID      string
	activeVersion string
	detachID      string
	message       string
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

func handleRequest(rt config.Runtime, engine *tui.Model, clients *clientCoordinator, control *supervisorControl, request ipc.Request, cancel context.CancelFunc) (ipc.Response, tea.Cmd) {
	if request.ProtocolVersion != 0 && request.ProtocolVersion != ipc.ProtocolVersion {
		return withSupervisorFields(ipc.ErrorResponse("protocol_mismatch", fmt.Sprintf("unsupported protocol version %d", request.ProtocolVersion)), request, control), nil
	}
	applyRequestSize(engine, request)
	switch request.Command {
	case "attach_client":
		clientID := request.Args["client_id"]
		if clientID == "" {
			return withSupervisorFields(ipc.ErrorResponse("missing_client_id", "client_id is required"), request, control), nil
		}
		if launchWorkspace := strings.TrimSpace(request.Args["launch_workspace"]); launchWorkspace != "" && clients.activeID != clientID {
			engine.ApplyLaunchWorkspace(launchWorkspace)
		}
		if clients.activeID != "" && clients.activeID != clientID {
			clients.detachID = clients.activeID
			clients.message = "another Weft client attached"
		}
		clients.activeID = clientID
		clients.activeVersion = strings.TrimSpace(request.ClientVersion)
		response := ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "attached Weft client"}
		applyClientState(&response, clients, clientID)
		return withSupervisorFields(response, request, control), nil
	case "client_detached":
		clientID := request.Args["client_id"]
		if clients.activeID == clientID {
			clients.activeID = ""
			clients.activeVersion = ""
		}
		if clients.detachID == clientID {
			clients.detachID = ""
			clients.message = ""
		}
		return withSupervisorFields(ipc.Response{OK: true, Message: "detached Weft client"}, request, control), nil
	case "close_client":
		clientID := clients.activeID
		if clientID == "" {
			return withSupervisorFields(ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "No Weft client is attached."}, request, control), nil
		}
		clients.detachID = clientID
		clients.message = "closed Weft client"
		response := ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "closed Weft client"}
		applyClientState(&response, clients, clientID)
		return withSupervisorFields(response, request, control), nil
	case "handshake":
		response := ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "Weft supervisor is running"}
		applyClientState(&response, clients, request.Args["client_id"])
		return withSupervisorFields(response, request, control), nil
	case "upgrade_resume":
		response := upgradeResume(rt, engine, control, request, cancel)
		applyClientState(&response, clients, request.Args["client_id"])
		return withSupervisorFields(response, request, control), nil
	case "shutdown":
		go cancel()
		return withSupervisorFields(ipc.Response{OK: true, Message: "Weft supervisor stopped"}, request, control), nil
	default:
		response, cmd := engine.HandleSupervisorRequest(request)
		applyClientState(&response, clients, request.Args["client_id"])
		return withSupervisorFields(response, request, control), cmd
	}
}

func applyRequestSize(engine *tui.Model, request ipc.Request) {
	if request.Command == "resize" {
		return
	}
	if request.Args["width"] == "" && request.Args["height"] == "" {
		return
	}
	engine.HandleSupervisorRequest(ipc.Request{Command: "resize", Args: request.Args})
}

func snapshotPtr(snapshot ipc.Snapshot) *ipc.Snapshot {
	return &snapshot
}

func applyClientState(response *ipc.Response, clients *clientCoordinator, clientID string) {
	if response.Snapshot == nil {
		return
	}
	response.Snapshot.ActiveClientID = clients.activeID
	response.Snapshot.ActiveClientVersion = clients.activeVersion
	if clientID != "" && clients.detachID == clientID {
		response.Snapshot.DetachClient = true
		if clients.message != "" {
			response.Snapshot.Message = clients.message
		}
	}
}

func withSupervisorFields(response ipc.Response, request ipc.Request, control *supervisorControl) ipc.Response {
	response.ProtocolVersion = ipc.ProtocolVersion
	response.SupervisorVersion = ReportedVersion()
	response = ipc.AnnotateUpgrade(response, request.ClientVersion, false)
	if response.Upgrade != nil && control != nil {
		if control.restartNow != nil {
			response.Upgrade.Message = restartNowMessage(*control.restartNow)
			response.Upgrade.BackupID = control.restartNow.backupID
		}
	}
	return response
}

func upgradeResume(rt config.Runtime, engine *tui.Model, control *supervisorControl, request ipc.Request, cancel context.CancelFunc) ipc.Response {
	response := supervisorSnapshotResponse(engine, "")
	response.ProtocolVersion = ipc.ProtocolVersion
	response.SupervisorVersion = ReportedVersion()
	upgrade := ipc.UpgradeStatus(response, request.ClientVersion)
	if upgrade == nil {
		return supervisorSnapshotResponse(engine, "No supervisor upgrade needed; client and supervisor are current.")
	}
	if !upgrade.Compatible {
		return ipc.ErrorResponse("upgrade_incompatible", "Cannot upgrade from the dashboard because the supervisor protocol is incompatible. Use `weft close --kill` when ready.")
	}
	report := engine.PrepareUpgradeResume()
	if blocked := codexsession.LiveNonCodexTaskCount(engine.Snapshot().State); blocked > 0 {
		return ipc.ErrorResponse("upgrade_resume_blocked", fmt.Sprintf("Upgrade waits until %d non-resumable task(s) stop.", blocked))
	}
	if !report.CanUpgrade() {
		return ipc.ErrorResponse("upgrade_resume_blocked", upgradeResumeBlockedMessage(report))
	}
	restart := restartRequestFromRequest(request)
	restart.runningTasks = report.Ready
	restart.freshTasks = report.Fresh
	return triggerResumeUpgrade(rt, engine, control, restart, cancel)
}

func triggerResumeUpgrade(rt config.Runtime, engine *tui.Model, control *supervisorControl, restart restartRequest, cancel context.CancelFunc) ipc.Response {
	backup, err := runtimebackup.Create(rt, runtimebackup.Options{Reason: "pre-upgrade resume restart", IncludeLogs: true})
	if err != nil {
		return supervisorSnapshotResponse(engine, fmt.Sprintf("Upgrade canceled: could not create pre-upgrade backup: %v", err))
	}
	restart.backupID = backup.ID
	restart.message = upgradeResumeRestartMessage(restart.runningTasks, restart.freshTasks, backup.ID)
	control.restartNow = &restart
	go cancel()
	return supervisorSnapshotResponse(engine, restart.message)
}

func supervisorSnapshotResponse(engine *tui.Model, message string) ipc.Response {
	snapshot := engine.Snapshot()
	if message != "" {
		snapshot.Message = message
	}
	st := snapshot.State
	return ipc.Response{OK: true, State: &st, Snapshot: &snapshot, Message: message}
}

func restartRequestFromRequest(request ipc.Request) restartRequest {
	exe := strings.TrimSpace(request.Args["client_executable"])
	if exe == "" {
		exe = strings.TrimSpace(os.Getenv("WEFT_EXECUTABLE"))
	}
	if exe == "" {
		if current, err := os.Executable(); err == nil {
			exe = current
		}
	}
	return restartRequest{executable: exe, clientVersion: request.ClientVersion}
}

func restartNowMessage(restart restartRequest) string {
	if restart.message != "" {
		return restart.message
	}
	if restart.backupID == "" {
		return "Restarting idle supervisor to finish the upgrade."
	}
	return "Restarting idle supervisor to finish the upgrade. Backup: " + restart.backupID + "."
}

func upgradeResumeRestartMessage(resumeTasks int, freshTasks int, backupID string) string {
	if resumeTasks <= 0 && freshTasks <= 0 {
		if backupID == "" {
			return "Restarting supervisor to finish the upgrade."
		}
		return "Restarting supervisor to finish the upgrade. Backup: " + backupID + "."
	}
	action := upgradeResumeRestartAction(resumeTasks, freshTasks)
	if backupID == "" {
		return action + " to finish the supervisor upgrade."
	}
	return fmt.Sprintf("%s to finish the supervisor upgrade. Backup: %s.", action, backupID)
}

func upgradeResumeRestartAction(resumeTasks int, freshTasks int) string {
	actions := []string{}
	if resumeTasks > 0 {
		actions = append(actions, fmt.Sprintf("resuming %d idle Codex task(s)", resumeTasks))
	}
	if freshTasks > 0 {
		actions = append(actions, fmt.Sprintf("starting %d fresh Codex task(s)", freshTasks))
	}
	return "Closing and " + strings.Join(actions, " and ")
}

func upgradeResumeBlockedMessage(report codexsession.Report) string {
	var blockers []string
	if len(report.Busy) > 0 {
		blockers = append(blockers, fmt.Sprintf("%d still active", len(report.Busy)))
	}
	if len(report.Missing) > 0 {
		blockers = append(blockers, fmt.Sprintf("%d missing a Codex session id", len(report.Missing)))
	}
	if len(blockers) == 0 {
		return "Upgrade waits for idle, resumable Codex tasks before closing terminals and restarting the supervisor."
	}
	return "Upgrade waits for idle, resumable Codex tasks before closing terminals and restarting the supervisor: " + strings.Join(blockers, ", ") + "."
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
	return ipc.AnnotateUpgrade(response, ReportedClientVersion(), autoRestarted)
}

func ShouldAutoRestart(response ipc.Response) bool {
	return ipc.ShouldAutoRestart(response)
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
		RunningTasks:      0,
		Message:           message,
	}
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

func startReplacementSupervisor(rt config.Runtime, restart restartRequest) error {
	exe := strings.TrimSpace(restart.executable)
	if exe == "" {
		return errors.New("cannot restart supervisor: client executable path is unknown")
	}
	if err := startSupervisorProcess(rt, exe, replacementSupervisorEnv(rt, exe)); err != nil {
		return err
	}
	deadline := time.Now().Add(4 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "status", ClientVersion: restart.clientVersion}, time.Second)
		if err == nil {
			if restart.clientVersion == "" || response.SupervisorVersion == "" || response.SupervisorVersion == restart.clientVersion {
				return nil
			}
			lastErr = fmt.Errorf("replacement supervisor reported version %s, want %s", response.SupervisorVersion, restart.clientVersion)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("started replacement Weft supervisor but it did not become ready: %w", lastErr)
}

func supervisorEnv(rt config.Runtime, exe string) []string {
	env := append([]string{}, os.Environ()...)
	env = upsertEnv(env, config.AppDirEnv, rt.Dir)
	env = upsertEnv(env, config.WorkspaceEnv, rt.Workspace)
	env = upsertEnv(env, "WEFT_EXECUTABLE", exe)
	return env
}

func replacementSupervisorEnv(rt config.Runtime, exe string) []string {
	env := withoutEnv(os.Environ(), ClientVersionEnv, VersionOverrideEnv)
	env = upsertEnv(env, config.AppDirEnv, rt.Dir)
	env = upsertEnv(env, config.WorkspaceEnv, rt.Workspace)
	env = upsertEnv(env, "WEFT_EXECUTABLE", exe)
	return env
}

func withoutEnv(env []string, keys ...string) []string {
	drop := map[string]bool{}
	for _, key := range keys {
		drop[key+"="] = true
	}
	next := make([]string, 0, len(env))
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
	return next
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
