package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
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
	"github.com/edwmurph/weft/internal/titles"
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
	executable      string
	clientVersion   string
	reason          string
	backupID        string
	runningTasks    int
	freshTasks      int
	terminalTasks   int
	terminalTaskIDs []string
	message         string
}

type supervisorControl struct {
	restartNow        *restartRequest
	configFingerprint string
	configDrift       configDriftStatus
	lastConfigCheck   time.Time
}

type configDriftStatus struct {
	changed bool
	err     error
	cfg     config.Config
}

type blockingTask struct {
	workspace string
	task      string
}

func Ensure(rt config.Runtime) (EnsureResult, error) {
	if response, err := Status(rt); err == nil {
		response = AnnotateUpgrade(response, false)
		if ShouldAutoRestart(response) {
			backup, err := runtimebackup.Create(rt, runtimebackup.Options{Reason: preAutoRestartBackupReason(response.Upgrade), IncludeLogs: true})
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
		if protocolMismatch(err) && response.Upgrade != nil && response.Upgrade.Compatible {
			return EnsureResult{Status: response}, nil
		}
		return EnsureResult{Status: response}, upgradeError(response, err)
	}
	return start(rt)
}

func preAutoRestartBackupReason(upgrade *ipc.Upgrade) string {
	if upgrade != nil && upgrade.Reason == ipc.UpgradeReasonConfig {
		return "pre-config reload auto restart"
	}
	return "pre-upgrade auto restart"
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
	if err := cfg.ValidateStateTaskTypesWithResetHint(st); err != nil {
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
	control := supervisorControl{configFingerprint: config.Fingerprint(cfg)}
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
	response, err := statusWithProtocol(rt, 0)
	response = AnnotateUpgrade(response, false)
	if protocolMismatch(err) && ipc.ProtocolSupportsUpgradeBridge(response.ProtocolVersion) {
		bridged, bridgeErr := statusWithProtocol(rt, response.ProtocolVersion)
		bridged = AnnotateUpgrade(bridged, false)
		if bridgeErr == nil {
			return bridged, nil
		}
		if hasSupervisorResponse(bridged) {
			return bridged, bridgeErr
		}
	}
	return response, err
}

func statusWithProtocol(rt config.Runtime, protocolVersion int) (ipc.Response, error) {
	request := ipc.Request{Command: "status"}
	if protocolVersion > 0 {
		request.ProtocolVersion = protocolVersion
	}
	return ipc.Call(rt.SocketPath, request, time.Second)
}

func Shutdown(rt config.Runtime) error {
	response, err := shutdownWithProtocol(rt, 0)
	if protocolMismatch(err) && ipc.ProtocolSupportsUpgradeBridge(response.ProtocolVersion) {
		_, err = shutdownWithProtocol(rt, response.ProtocolVersion)
	}
	return err
}

func shutdownWithProtocol(rt config.Runtime, protocolVersion int) (ipc.Response, error) {
	request := ipc.Request{Command: "shutdown"}
	if protocolVersion > 0 {
		request.ProtocolVersion = protocolVersion
	}
	return ipc.Call(rt.SocketPath, request, time.Second)
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
	if request.ProtocolVersion != ipc.ProtocolVersion && !upgradeBridgeRequest(request) {
		return withSupervisorFields(ipc.ErrorResponse("protocol_mismatch", fmt.Sprintf("unsupported protocol version %d", request.ProtocolVersion)), request, control, rt), nil
	}
	applyRequestSize(engine, request)
	switch request.Command {
	case "attach_client":
		clientID := request.ClientID
		if clientID == "" {
			return withSupervisorFields(ipc.ErrorResponse("missing_client_id", "client_id is required"), request, control, rt), nil
		}
		if launchWorkspace := strings.TrimSpace(request.LaunchWorkspace); launchWorkspace != "" && clients.activeID != clientID {
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
		return withSupervisorFields(response, request, control, rt), nil
	case "client_detached":
		clientID := request.ClientID
		if clients.activeID == clientID {
			clients.activeID = ""
			clients.activeVersion = ""
		}
		if clients.detachID == clientID {
			clients.detachID = ""
			clients.message = ""
		}
		return withSupervisorFields(ipc.Response{OK: true, Message: "detached Weft client"}, request, control, rt), nil
	case "close_client":
		clientID := clients.activeID
		if clientID == "" {
			return withSupervisorFields(ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "No Weft client is attached."}, request, control, rt), nil
		}
		clients.detachID = clientID
		clients.message = "closed Weft client"
		response := ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "closed Weft client"}
		applyClientState(&response, clients, clientID)
		return withSupervisorFields(response, request, control, rt), nil
	case "handshake":
		response := ipc.Response{OK: true, Snapshot: snapshotPtr(engine.Snapshot()), Message: "Weft supervisor is running"}
		applyClientState(&response, clients, request.ClientID)
		return withSupervisorFields(response, request, control, rt), nil
	case "upgrade_resume":
		response := upgradeResume(rt, engine, control, request, cancel)
		applyClientState(&response, clients, request.ClientID)
		return withSupervisorFields(response, request, control, rt), nil
	case "shutdown":
		go cancel()
		return withSupervisorFields(ipc.Response{OK: true, Message: "Weft supervisor stopped"}, request, control, rt), nil
	default:
		response, cmd := engine.HandleSupervisorRequest(request)
		applyClientState(&response, clients, request.ClientID)
		return withSupervisorFields(response, request, control, rt), cmd
	}
}

func upgradeBridgeRequest(request ipc.Request) bool {
	if !ipc.ProtocolCanRequestUpgradeBridge(request.ProtocolVersion) {
		return false
	}
	switch request.Command {
	case "attach_client", "client_detached", "handshake", "snapshot", "status", "resize", "upgrade_resume":
		return true
	default:
		return false
	}
}

func applyRequestSize(engine *tui.Model, request ipc.Request) {
	if request.Command == "resize" {
		return
	}
	if request.Width <= 0 && request.Height <= 0 {
		return
	}
	engine.HandleSupervisorRequest(ipc.Request{Command: "resize", Width: request.Width, Height: request.Height})
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

func withSupervisorFields(response ipc.Response, request ipc.Request, control *supervisorControl, rt config.Runtime) ipc.Response {
	response.ProtocolVersion = ipc.ProtocolVersion
	response.SupervisorVersion = ReportedVersion()
	if control != nil {
		response.ConfigFingerprint = control.configFingerprint
	}
	response = ipc.AnnotateUpgrade(response, request.ClientVersion, false)
	if response.Upgrade == nil && control != nil {
		if drift := control.detectConfigDrift(rt); drift.changed {
			response.Upgrade = configDriftUpgrade(response, request.ClientVersion, drift)
		}
	}
	if response.Upgrade != nil && control != nil {
		if control.restartNow != nil {
			response.Upgrade.Message = restartNowMessage(*control.restartNow)
			response.Upgrade.BackupID = control.restartNow.backupID
		}
	}
	return response
}

func (control *supervisorControl) detectConfigDrift(rt config.Runtime) configDriftStatus {
	if control == nil || strings.TrimSpace(control.configFingerprint) == "" {
		return configDriftStatus{}
	}
	if time.Since(control.lastConfigCheck) < 250*time.Millisecond {
		return control.configDrift
	}
	control.lastConfigCheck = time.Now()
	cfg, err := config.LoadConfig(rt.ConfigPath)
	if err != nil {
		control.configDrift = configDriftStatus{changed: true, err: err}
		return control.configDrift
	}
	control.configDrift = configDriftStatus{changed: config.Fingerprint(cfg) != control.configFingerprint, cfg: cfg}
	return control.configDrift
}

func configDriftUpgrade(response ipc.Response, clientVersion string, drift configDriftStatus) *ipc.Upgrade {
	clientVersion = strings.TrimSpace(clientVersion)
	if clientVersion == "" {
		clientVersion = ReportedClientVersion()
	}
	running := ipc.RunningTaskCount(responseState(response))
	upgrade := &ipc.Upgrade{
		ClientVersion:     clientVersion,
		SupervisorVersion: response.SupervisorVersion,
		Reason:            ipc.UpgradeReasonConfig,
		Compatible:        drift.err == nil,
		RestartRequired:   drift.err == nil,
		RunningTasks:      running,
	}
	if drift.err != nil {
		upgrade.Message = fmt.Sprintf("Config changed, but the supervisor cannot reload it yet: %v", drift.err)
		return upgrade
	}
	if st := responseState(response); st != nil {
		if err := drift.cfg.ValidateStateTaskTypes(*st); err != nil {
			upgrade.Compatible = false
			upgrade.RestartRequired = false
			upgrade.Message = fmt.Sprintf("Config changed, but the supervisor cannot apply it yet: %v", err)
			return upgrade
		}
	}
	if running == 0 {
		upgrade.Message = "Config changed; the idle supervisor can restart safely to apply it."
		return upgrade
	}
	upgrade.Message = fmt.Sprintf("Config changed; restart the supervisor when %d live task terminal(s) are idle or resumable to apply it.", running)
	return upgrade
}

func responseState(response ipc.Response) *state.State {
	if response.State != nil {
		return response.State
	}
	if response.Snapshot != nil {
		return &response.Snapshot.State
	}
	return nil
}

func upgradeResume(rt config.Runtime, engine *tui.Model, control *supervisorControl, request ipc.Request, cancel context.CancelFunc) ipc.Response {
	response := withSupervisorFields(supervisorSnapshotResponse(engine, ""), request, control, rt)
	upgrade := response.Upgrade
	if upgrade == nil || !upgrade.RestartRequired {
		return supervisorSnapshotResponse(engine, "No supervisor restart needed; client, supervisor, and config are current.")
	}
	if !upgrade.Compatible {
		if upgrade.Reason == ipc.UpgradeReasonConfig {
			return ipc.ErrorResponse("config_reload_blocked", upgrade.Message)
		}
		return ipc.ErrorResponse("upgrade_incompatible", "Cannot upgrade from the dashboard because the supervisor protocol is incompatible. Use `weft close --kill` when ready.")
	}
	report := engine.PrepareUpgradeResume()
	if !report.CanUpgrade() {
		return ipc.ErrorResponse("upgrade_resume_blocked", upgradeResumeBlockedMessage(report, *response.State))
	}
	restart := restartRequestFromRequest(request)
	restart.reason = upgrade.Reason
	restart.runningTasks = report.Ready
	restart.freshTasks = report.Fresh
	restart.terminalTasks = len(report.TerminalReady)
	restart.terminalTaskIDs = tui.TerminalTaskIDs(report.TerminalReady)
	return triggerResumeUpgrade(rt, engine, control, restart, cancel)
}

func triggerResumeUpgrade(rt config.Runtime, engine *tui.Model, control *supervisorControl, restart restartRequest, cancel context.CancelFunc) ipc.Response {
	backup, err := runtimebackup.Create(rt, runtimebackup.Options{Reason: preRestartBackupReason(restart.reason), IncludeLogs: true})
	if err != nil {
		return supervisorSnapshotResponse(engine, fmt.Sprintf("Upgrade canceled: could not create pre-upgrade backup: %v", err))
	}
	restart.backupID = backup.ID
	if err := engine.PrepareTerminalUpgradeSnapshots(restart.terminalTaskIDs); err != nil {
		return supervisorSnapshotResponse(engine, fmt.Sprintf("Upgrade canceled: could not save shell history/cwd snapshot: %v", err))
	}
	restart.message = upgradeResumeRestartMessage(restart.reason, restart.runningTasks, restart.freshTasks, restart.terminalTasks, backup.ID)
	control.restartNow = &restart
	go cancel()
	return supervisorSnapshotResponse(engine, restart.message)
}

func preRestartBackupReason(reason string) string {
	if reason == ipc.UpgradeReasonConfig {
		return "pre-config reload restart"
	}
	return "pre-upgrade resume restart"
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
	exe := strings.TrimSpace(request.ClientExecutable)
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
	if restart.reason == ipc.UpgradeReasonConfig {
		if restart.backupID == "" {
			return "Restarting idle supervisor to apply config changes."
		}
		return "Restarting idle supervisor to apply config changes. Backup: " + restart.backupID + "."
	}
	if restart.backupID == "" {
		return "Restarting idle supervisor to finish the upgrade."
	}
	return "Restarting idle supervisor to finish the upgrade. Backup: " + restart.backupID + "."
}

func upgradeResumeRestartMessage(reason string, resumeTasks int, freshTasks int, terminalTasks int, backupID string) string {
	suffix := "finish the supervisor upgrade"
	if reason == ipc.UpgradeReasonConfig {
		suffix = "apply config changes"
	}
	if resumeTasks <= 0 && freshTasks <= 0 && terminalTasks <= 0 {
		if backupID == "" {
			if reason == ipc.UpgradeReasonConfig {
				return "Restarting supervisor to apply config changes."
			}
			return "Restarting supervisor to finish the upgrade."
		}
		if reason == ipc.UpgradeReasonConfig {
			return "Restarting supervisor to apply config changes. Backup: " + backupID + "."
		}
		return "Restarting supervisor to finish the upgrade. Backup: " + backupID + "."
	}
	action := upgradeResumeRestartAction(resumeTasks, freshTasks, terminalTasks)
	if backupID == "" {
		return action + " to " + suffix + "."
	}
	return fmt.Sprintf("%s to %s. Backup: %s.", action, suffix, backupID)
}

func upgradeResumeRestartAction(resumeTasks int, freshTasks int, terminalTasks int) string {
	actions := []string{}
	if resumeTasks > 0 {
		actions = append(actions, fmt.Sprintf("resuming %d idle Codex task(s)", resumeTasks))
	}
	if freshTasks > 0 {
		actions = append(actions, fmt.Sprintf("starting %d fresh Codex task(s)", freshTasks))
	}
	if terminalTasks > 0 {
		actions = append(actions, fmt.Sprintf("restarting %d idle shell task(s) with saved history/cwd", terminalTasks))
	}
	return "Closing and " + strings.Join(actions, " and ")
}

func upgradeResumeBlockedMessage(report codexsession.Report, st state.State) string {
	var blockers []string
	if len(report.Busy) > 0 {
		blockers = append(blockers, fmt.Sprintf("%d still active", len(report.Busy)))
	}
	if len(report.Missing) > 0 {
		blockers = append(blockers, fmt.Sprintf("%d missing a Codex session id", len(report.Missing)))
	}
	if len(report.TerminalBusy) > 0 {
		blockers = append(blockers, fmt.Sprintf("%d shell task(s) not idle", len(report.TerminalBusy)))
	}
	if len(blockers) == 0 {
		return "Upgrade waits for idle, resumable Codex tasks and restartable idle shell task(s) before closing terminals and restarting the supervisor."
	}
	message := "Upgrade waits for idle, resumable Codex tasks and restartable idle shell task(s) before closing terminals and restarting the supervisor: " + strings.Join(blockers, ", ") + "."
	if lines := blockingTaskLines(st, report); len(lines) > 0 {
		message += "\n" + strings.Join(lines, "\n")
	}
	return message
}

func blockingTaskLines(st state.State, report codexsession.Report) []string {
	tasks := blockingTasks(st, report)
	if len(tasks) == 0 {
		return nil
	}
	lines := []string{"Blocking:"}
	for _, task := range tasks {
		lines = append(lines, "- workspace: "+task.workspace, "  task: "+task.task)
	}
	return lines
}

func blockingTasks(st state.State, report codexsession.Report) []blockingTask {
	tasks := append([]state.Task{}, report.TerminalBusy...)
	tasks = append(tasks, report.Busy...)
	tasks = append(tasks, report.Missing...)
	if len(tasks) == 0 {
		return nil
	}
	items := make([]blockingTask, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, blockingTaskDetails(st, task))
	}
	return items
}

func blockingTaskDetails(st state.State, task state.Task) blockingTask {
	workspaceState := state.Workspace{}
	workspace := task.WorkspaceID
	if found := state.WorkspaceForTask(st, task); found != nil {
		workspaceState = *found
		workspace = strings.TrimSpace(found.Title)
		if workspace == "" {
			workspace = filepath.Base(strings.TrimRight(found.Path, string(os.PathSeparator)))
		}
		if strings.TrimSpace(workspace) == "" {
			workspace = found.Path
		}
	}
	groupState := state.Group{}
	if found := state.GroupForTask(st, task); found != nil {
		groupState = *found
	}
	title := strings.TrimSpace(titles.RenderTask(task, workspaceState, groupState, task.Title))
	if title == "" {
		title = task.ID
	}
	return blockingTask{
		workspace: singleLineValue(workspace),
		task:      singleLineValue(title),
	}
}

func singleLineValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
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
	reason := ipc.UpgradeReasonVersion
	if previous.Upgrade != nil && previous.Upgrade.Reason == ipc.UpgradeReasonConfig {
		reason = ipc.UpgradeReasonConfig
		message = "Supervisor restarted to apply config changes."
	}
	if previous.SupervisorVersion != "" && current.SupervisorVersion != "" {
		message = fmt.Sprintf("Supervisor restarted from Weft %s to %s.", previous.SupervisorVersion, current.SupervisorVersion)
		if reason == ipc.UpgradeReasonConfig && previous.SupervisorVersion == current.SupervisorVersion {
			message = "Supervisor restarted to apply config changes."
		}
	}
	return &ipc.Upgrade{
		ClientVersion:     clientVersion,
		SupervisorVersion: current.SupervisorVersion,
		Reason:            reason,
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

func protocolMismatch(err error) bool {
	var ipcErr ipc.Error
	return errors.As(err, &ipcErr) && ipcErr.Code == "protocol_mismatch"
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

func ForceShutdown(rt config.Runtime, timeout time.Duration) error {
	pid, err := readPID(PIDPath(rt))
	if err != nil {
		return err
	}
	if !processExists(pid) {
		return nil
	}
	if err := signalProcessGroup(pid, syscall.SIGTERM); err != nil {
		return err
	}
	if waitForPIDExit(pid, timeout) {
		_ = os.Remove(PIDPath(rt))
		return nil
	}
	if err := signalProcessGroup(pid, syscall.SIGKILL); err != nil {
		return err
	}
	if waitForPIDExit(pid, time.Second) {
		_ = os.Remove(PIDPath(rt))
		return nil
	}
	return fmt.Errorf("supervisor process %d did not stop", pid)
}

func readPID(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid supervisor pid file %s", path)
	}
	return pid, nil
}

func signalProcessGroup(pid int, signal syscall.Signal) error {
	if err := syscall.Kill(-pid, signal); err == nil {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(signal); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func waitForPIDExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !processExists(pid)
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
