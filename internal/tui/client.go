package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/runtimebackup"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/version"
)

const clientSnapshotInterval = 120 * time.Millisecond

type clientResponseMsg struct {
	command  string
	response ipc.Response
	err      error
}

type clientSnapshotTick struct{}

type localRestartMsg struct {
	backupID string
	err      error
}

type ClientModel struct {
	cfg      config.Config
	runtime  config.Runtime
	clientID string
	snapshot ipc.Snapshot
	width    int
	height   int
	mode     mode
	message  string
	upgrade  *ipc.Upgrade
	loading  int

	input                   textinput.Model
	prompt                  promptKind
	confirm                 confirmKind
	pendingID               string
	promptSuggestionOpen    bool
	loadingTickerActive     bool
	launchWorkspacePrompted bool
	localRestartWhenIdle    bool
	localRestartInFlight    bool
	codexInputQueue         []map[string]string
	codexInputInFlight      bool
}

func RunClient(rt config.Runtime, cfg config.Config) error {
	model := NewClientModel(rt, cfg)
	enableTerminalKeyboardReporting()
	defer disableTerminalKeyboardReporting()
	options := []tea.ProgramOption{
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stdout),
	}
	if os.Getenv("WEFT_HEADLESS") == "1" {
		options = append(options, tea.WithoutRenderer())
	} else {
		options = append(options, tea.WithAltScreen())
	}
	_, err := tea.NewProgram(model, options...).Run()
	return err
}

func NewClientModel(rt config.Runtime, cfg config.Config) ClientModel {
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 240
	input.Width = 60
	st := state.Repair(state.Empty(), rt.Workspace)
	return ClientModel{
		cfg: cfg, runtime: rt, clientID: shortID(), width: 100, height: 32,
		snapshot: ipc.Snapshot{State: st, CodexTitle: "Codex", CodexContent: "No Codex agent open.", NavWidth: workspaceNavFrameWidth(st, 100)},
		input:    input,
	}
}

func (m ClientModel) Init() tea.Cmd {
	return tea.Batch(m.request("attach_client", nil), tickClientSnapshot())
}

func (m ClientModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, m.request("resize", map[string]string{"width": strconv.Itoa(typed.Width), "height": strconv.Itoa(typed.Height)})
	case clientResponseMsg:
		if typed.command == "codex_input" {
			m.codexInputInFlight = false
		}
		if typed.err != nil {
			if typed.command == "restart_when_idle" && restartWhenIdleUnsupported(typed.response) {
				m.message = "Restart when idle queued locally; Weft will wait for Codex agents to close."
				return m, tea.Batch(m.localRestartWhenIdleCmd(), m.nextCodexInputRequest())
			}
			m.message = typed.err.Error()
			return m, m.nextCodexInputRequest()
		}
		m.applyResponse(typed.response)
		restartCmd := m.localRestartWhenIdleCmd()
		nextCodexInput := m.nextCodexInputRequest()
		nextLoadingTick := m.ensureLoadingTick()
		if typed.response.Snapshot != nil && typed.response.Snapshot.DetachClient {
			return m, tea.Batch(nextCodexInput, nextLoadingTick, m.request("client_detached", nil), tea.Quit)
		}
		return m, tea.Batch(nextCodexInput, nextLoadingTick, restartCmd)
	case clientSnapshotTick:
		return m, tea.Batch(m.request("snapshot", nil), tickClientSnapshot())
	case localRestartMsg:
		m.localRestartInFlight = false
		if typed.err != nil {
			m.message = "Restart when idle failed: " + typed.err.Error()
			return m, nil
		}
		m.localRestartWhenIdle = false
		m.message = "Restarted idle supervisor to finish the upgrade. Backup: " + typed.backupID + "."
		return m, m.request("attach_client", nil)
	case loadingTick:
		if !m.hasLoadingAnimation() {
			m.loadingTickerActive = false
			return m, nil
		}
		m.loading++
		return m, tickLoading()
	case tea.KeyMsg:
		return m.handleKey(typed)
	}
	if input, ok := enhancedKeyboardInputFromMsg(msg); ok {
		return m.handleEnhancedKeyboardInput(input)
	}
	return m, nil
}

func (m ClientModel) View() string {
	if m.mode == modeHelp {
		return m.modalView(renderHelp(m.cfg))
	}
	if m.mode == modeInput {
		return m.modalView(m.renderInputModal())
	}
	if m.mode == modeConfirm {
		return m.modalView(m.renderConfirmModal())
	}
	loadingText := m.snapshot.LoadingText
	loadingFrame := loadingFrames[m.loading%len(loadingFrames)]
	if loadingText != "" {
		loadingText = loadingFrame + strings.TrimPrefix(loadingText, loadingFrames[0])
		return renderLoadingWorkspaceWithNavWidthAndAgents(m.cfg, m.snapshot.State, m.snapshot.CodexTitle, loadingText, loadingFrame, loadingAgentSet(m.snapshot.LoadingAgentIDs), m.width, m.height, m.messageText(), m.snapshot.NavWidth, m.snapshot.GroupCursor)
	}
	return renderWorkspaceWithNavWidthAndAgents(m.cfg, m.snapshot.State, m.snapshot.CodexTitle, m.snapshot.CodexContent, loadingFrame, loadingAgentSet(m.snapshot.LoadingAgentIDs), m.width, m.height, m.messageText(), m.snapshot.NavWidth, m.snapshot.GroupCursor)
}

func (m ClientModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeInput {
		return m.handleInputKey(msg)
	}
	if m.mode == modeHelp {
		if msg.Type == tea.KeyEsc || msg.String() == "q" || msg.String() == "?" {
			m.mode = modeNormal
		}
		return m, nil
	}
	if m.mode == modeConfirm {
		return m.handleConfirmKey(msg)
	}

	quitPressed := bindingMatches(m.cfg.KeyBindings.Quit, msg)
	if quitPressed && !m.activeCodexReceivesQuitBinding() {
		return m, tea.Quit
	}
	if bindingMatches(m.cfg.KeyBindings.Drawer, msg) {
		return m, m.request("toggle_drawer", nil)
	}
	if m.snapshot.State.Focus == state.FocusCodex && state.ActiveAgent(m.snapshot.State) != nil {
		return m.enqueueCodexInput(codexInputArgs(msg))
	}
	if m.snapshot.State.Focus == state.FocusCodex {
		return m, m.request("toggle_drawer", nil)
	}
	if bindingMatches(m.cfg.KeyBindings.Help, msg) {
		m.mode = modeHelp
		return m, nil
	}
	return m.handleNavKey(msg)
}

func (m ClientModel) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case bindingMatches(m.cfg.KeyBindings.FocusLeft, msg):
		return m, m.request("focus", map[string]string{"target": "workspaces"})
	case bindingMatches(m.cfg.KeyBindings.FocusRight, msg):
		return m, m.request("focus", map[string]string{"target": string(state.FocusAgents)})
	case bindingMatches(m.cfg.KeyBindings.SelectPrev, msg) || msg.Type == tea.KeyUp:
		return m, m.request("nav_move", map[string]string{"delta": "-1"})
	case bindingMatches(m.cfg.KeyBindings.SelectNext, msg) || msg.Type == tea.KeyDown:
		return m, m.request("nav_move", map[string]string{"delta": "1"})
	case bindingMatches(m.cfg.KeyBindings.NewWorkspace, msg):
		m.startPrompt(promptWorkspace, defaultWorkspacePromptValue(m.snapshot.State, m.runtime.Workspace))
	case bindingMatches(m.cfg.KeyBindings.NewGroup, msg):
		m.startPrompt(promptGroup, "")
	case bindingMatches(m.cfg.KeyBindings.NewAgent, msg):
		return m, m.request("new", nil)
	case m.canRestartWhenIdle() && strings.EqualFold(msg.String(), "u"):
		m.startRestartWhenIdleConfirm()
	case bindingMatches(m.cfg.KeyBindings.MoveAgent, msg):
		if agent := m.selectedAgent(); agent != nil {
			m.startPrompt(promptMoveAgent, "")
		}
	case bindingMatches(m.cfg.KeyBindings.Rename, msg):
		m.startRenamePrompt()
	case bindingMatches(m.cfg.KeyBindings.Delete, msg):
		m.startDeleteConfirm()
	case bindingMatches(m.cfg.KeyBindings.Open, msg) || msg.Type == tea.KeyEnter:
		return m, m.request("open", nil)
	}
	return m, nil
}

func (m ClientModel) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	result := handlePromptInputKey(m.input, m.promptContext(), m.promptSuggestionOpen, msg)
	m.input = result.input
	m.promptSuggestionOpen = result.suggestionOpen
	if result.message != "" {
		m.message = result.message
	}
	switch result.action {
	case promptInputCancel:
		m.mode = modeNormal
		return m, nil
	case promptInputSubmit:
		cmd := m.applyPrompt(result.value)
		m.mode = modeNormal
		return m, cmd
	default:
		return m, result.cmd
	}
}

func (m ClientModel) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "y":
		cmd := m.applyConfirm()
		m.mode = modeNormal
		return m, cmd
	case "n", "esc":
		m.mode = modeNormal
	}
	return m, nil
}

func (m *ClientModel) startPrompt(prompt promptKind, value string) {
	m.prompt = prompt
	configurePromptInput(&m.input, m.promptContext(), value)
	m.promptSuggestionOpen = false
	m.mode = modeInput
}

func (m *ClientModel) startRenamePrompt() {
	prompt, id, value, ok := renamePromptTargetForState(m.snapshot.State, m.snapshot.GroupCursor)
	if ok {
		m.pendingID = id
		m.startPrompt(prompt, value)
	}
}

func (m *ClientModel) startDeleteConfirm() {
	confirm, id, ok := deleteConfirmTargetForState(m.snapshot.State, m.snapshot.GroupCursor)
	if ok {
		m.confirm = confirm
		m.pendingID = id
		m.mode = modeConfirm
	}
}

func (m *ClientModel) startRestartWhenIdleConfirm() {
	if !m.canRestartWhenIdle() {
		return
	}
	m.confirm = confirmRestartWhenIdle
	m.pendingID = upgradeTarget(*m.upgrade)
	m.mode = modeConfirm
}

func (m ClientModel) applyPrompt(value string) tea.Cmd {
	switch m.prompt {
	case promptWorkspace:
		return m.request("add_workspace", map[string]string{"path": value})
	case promptGroup:
		return m.request("add_group", map[string]string{"path": value})
	case promptRenameGroup:
		return m.request("rename_group", map[string]string{"id": m.pendingID, "path": value})
	case promptWorkspaceTitle:
		return m.request("rename_workspace", map[string]string{"id": m.pendingID, "title": value})
	case promptRenameAgent:
		return m.request("rename", map[string]string{"id": m.pendingID, "title": value})
	case promptMoveAgent:
		if agent := m.selectedAgent(); agent != nil {
			return m.request("move", map[string]string{"id": agent.ID, "group": value})
		}
	}
	return nil
}

func (m *ClientModel) applyConfirm() tea.Cmd {
	switch m.confirm {
	case confirmAddLaunchWorkspace:
		return m.request("add_workspace", map[string]string{"path": m.pendingID})
	case confirmDeleteWorkspace:
		return m.request("remove_workspace", map[string]string{"id": m.pendingID})
	case confirmDeleteGroup:
		return m.request("remove_group", map[string]string{"id": m.pendingID})
	case confirmDeleteAgent:
		return m.request("close", map[string]string{"id": m.pendingID})
	case confirmRestartWhenIdle:
		m.localRestartWhenIdle = true
		return m.request("restart_when_idle", nil)
	}
	return nil
}

func (m ClientModel) request(command string, args map[string]string) tea.Cmd {
	rt := m.runtime
	clientID := m.clientID
	width := m.width
	height := m.height
	return func() tea.Msg {
		args = clientRequestArgs(rt, clientID, command, args)
		if width > 0 {
			args["width"] = strconv.Itoa(width)
		}
		if height > 0 {
			args["height"] = strconv.Itoa(height)
		}
		response, err := ipc.Call(rt.SocketPath, ipc.Request{Command: command, Args: args}, 2*time.Second)
		return clientResponseMsg{command: command, response: response, err: err}
	}
}

func clientRequestArgs(rt config.Runtime, clientID string, command string, args map[string]string) map[string]string {
	next := cloneArgs(args)
	next["client_id"] = clientID
	if command == "attach_client" {
		next["launch_workspace"] = rt.Workspace
	}
	if command == "restart_when_idle" {
		if exe := clientExecutablePath(); exe != "" {
			next["client_executable"] = exe
		}
	}
	return next
}

func clientExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

func (m ClientModel) enqueueCodexInput(args map[string]string) (ClientModel, tea.Cmd) {
	m.codexInputQueue = append(m.codexInputQueue, cloneArgs(args))
	return m, m.nextCodexInputRequest()
}

func (m *ClientModel) nextCodexInputRequest() tea.Cmd {
	if m.codexInputInFlight || len(m.codexInputQueue) == 0 {
		return nil
	}
	args := m.codexInputQueue[0]
	m.codexInputQueue = m.codexInputQueue[1:]
	m.codexInputInFlight = true
	return m.request("codex_input", args)
}

func cloneArgs(args map[string]string) map[string]string {
	next := make(map[string]string, len(args)+1)
	for key, value := range args {
		next[key] = value
	}
	return next
}

func (m *ClientModel) applyResponse(response ipc.Response) {
	if response.Snapshot != nil {
		m.snapshot = *response.Snapshot
	} else if response.State != nil {
		m.snapshot.State = *response.State
	}
	if strings.TrimSpace(response.Message) != "" {
		m.message = response.Message
	}
	if response.Snapshot != nil && strings.TrimSpace(response.Snapshot.Message) != "" {
		m.message = response.Snapshot.Message
	}
	upgrade := clientUpgradeFromResponse(response)
	if upgrade != nil {
		m.upgrade = upgrade
		if upgrade.RestartWhenIdleQueued {
			m.localRestartWhenIdle = false
		}
		m.message = dashboardUpgradeMessage(*upgrade)
	} else {
		m.upgrade = nil
	}
	m.maybePromptForLaunchWorkspace()
}

func (m *ClientModel) ensureLoadingTick() tea.Cmd {
	if !m.hasLoadingAnimation() || m.loadingTickerActive {
		return nil
	}
	m.loadingTickerActive = true
	return tickLoading()
}

func (m ClientModel) hasLoadingAnimation() bool {
	return strings.TrimSpace(m.snapshot.LoadingText) != "" || len(m.snapshot.LoadingAgentIDs) > 0
}

func clientUpgradeFromResponse(response ipc.Response) *ipc.Upgrade {
	if response.Upgrade != nil {
		upgrade := *response.Upgrade
		return &upgrade
	}
	return ipc.UpgradeStatus(response, version.Version)
}

func dashboardUpgradeMessage(upgrade ipc.Upgrade) string {
	if upgrade.AutoRestarted {
		return upgrade.Message
	}
	if upgrade.RestartWhenIdleQueued {
		return fmt.Sprintf("Upgrade queued: supervisor %s will restart when idle; live Codex terminals stay running.", upgrade.SupervisorVersion)
	}
	if !upgrade.Compatible {
		return upgrade.Message
	}
	if !upgrade.RestartRequired {
		return upgrade.Message
	}
	if upgrade.RunningAgents > 0 {
		return fmt.Sprintf("Upgrade pending: supervisor %s must restart. Press U to restart when idle; reopening alone is not enough (%d live).", upgrade.SupervisorVersion, upgrade.RunningAgents)
	}
	return fmt.Sprintf("Upgrade ready: supervisor %s is idle. Press U to restart it now.", upgrade.SupervisorVersion)
}

func upgradeTarget(upgrade ipc.Upgrade) string {
	return fmt.Sprintf("client %s, supervisor %s", upgrade.ClientVersion, upgrade.SupervisorVersion)
}

func (m ClientModel) canRestartWhenIdle() bool {
	return m.upgrade != nil && m.upgrade.Compatible && m.upgrade.RestartRequired
}

func restartWhenIdleUnsupported(response ipc.Response) bool {
	return response.Error != nil && response.Error.Code == "unknown_command"
}

func (m *ClientModel) localRestartWhenIdleCmd() tea.Cmd {
	if !m.localRestartWhenIdle || m.localRestartInFlight || !m.canRestartWhenIdle() || len(m.snapshot.State.Agents) > 0 {
		return nil
	}
	m.localRestartInFlight = true
	rt := m.runtime
	exe := clientExecutablePath()
	return func() tea.Msg {
		backup, err := runtimebackup.Create(rt, runtimebackup.Options{Reason: "pre-upgrade restart when idle", IncludeLogs: true})
		if err != nil {
			return localRestartMsg{err: fmt.Errorf("could not create pre-upgrade backup: %w", err)}
		}
		_, _ = ipc.Call(rt.SocketPath, ipc.Request{Command: "shutdown"}, time.Second)
		waitForClientSupervisorStop(rt, 2*time.Second)
		if err := startClientSupervisor(rt, exe); err != nil {
			return localRestartMsg{err: err}
		}
		return localRestartMsg{backupID: backup.ID}
	}
}

func waitForClientSupervisorStop(rt config.Runtime, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "handshake"}, 100*time.Millisecond); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func startClientSupervisor(rt config.Runtime, exe string) error {
	if strings.TrimSpace(exe) == "" {
		return fmt.Errorf("client executable path is unknown")
	}
	log, err := os.OpenFile(filepath.Join(rt.Dir, "weftd.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "_supervisor")
	cmd.Env = clientSupervisorEnv(rt, exe)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return err
	}
	_ = cmd.Process.Release()
	_ = log.Close()
	deadline := time.Now().Add(4 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "status"}, time.Second); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("started replacement Weft supervisor but it did not become ready: %w", lastErr)
}

func clientSupervisorEnv(rt config.Runtime, exe string) []string {
	env := withoutClientVersionOverrides(os.Environ())
	env = upsertClientEnv(env, config.AppDirEnv, rt.Dir)
	env = upsertClientEnv(env, config.WorkspaceEnv, rt.Workspace)
	env = upsertClientEnv(env, "WEFT_EXECUTABLE", exe)
	return env
}

func withoutClientVersionOverrides(env []string) []string {
	next := make([]string, 0, len(env))
	for _, item := range env {
		if strings.HasPrefix(item, "WEFT_CLIENT_VERSION_OVERRIDE=") || strings.HasPrefix(item, "WEFT_SUPERVISOR_VERSION_OVERRIDE=") {
			continue
		}
		next = append(next, item)
	}
	return next
}

func upsertClientEnv(env []string, key string, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func (m *ClientModel) maybePromptForLaunchWorkspace() {
	if m.launchWorkspacePrompted || m.mode != modeNormal {
		return
	}
	path := strings.TrimSpace(m.runtime.Workspace)
	if path == "" || !workspaceInputIsExistingDirectory(path) || state.WorkspaceByPath(m.snapshot.State, path) != nil {
		m.launchWorkspacePrompted = true
		return
	}
	m.confirm = confirmAddLaunchWorkspace
	m.pendingID = path
	m.mode = modeConfirm
	m.launchWorkspacePrompted = true
}

func (m ClientModel) modalView(content string) string {
	w := max(40, min(m.width-4, 82))
	box := modalStyle.Width(w).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m ClientModel) renderInputModal() string {
	width := max(36, min(m.width-16, 72))
	return renderPromptModal(m.promptContext(), m.input, width, m.height, m.promptSuggestionOpen, m.renderPromptExtra(m.input, width))
}

func (m ClientModel) promptContext() promptContext {
	return promptContextFor(m.prompt, m.pendingID, m.snapshot.State, m.selectedAgent())
}

func (m ClientModel) renderPromptExtra(input textinput.Model, width int) []string {
	return renderPromptExtraForState(m.cfg, m.snapshot.State, m.prompt, m.selectedAgent(), input, width)
}

func (m ClientModel) renderConfirmModal() string {
	width := max(36, min(m.width-16, 72))
	return renderConfirmPrompt(m.confirm, confirmTarget(m.confirm, m.snapshot.State, m.pendingID, m.renderAgentTitle), width)
}

func (m ClientModel) activeCodexReceivesQuitBinding() bool {
	if m.snapshot.State.Focus != state.FocusCodex {
		return false
	}
	active := state.ActiveAgent(m.snapshot.State)
	if active == nil {
		return false
	}
	return activeAgentReceivesQuitBinding(*active, strings.TrimSpace(m.snapshot.LoadingText) != "")
}

func (m ClientModel) selectedAgent() *state.Agent {
	return selectedAgentForState(m.snapshot.State, m.snapshot.GroupCursor)
}

func (m ClientModel) currentGroupRow() groupRow {
	return currentGroupRowForState(m.snapshot.State, m.snapshot.GroupCursor)
}

func (m ClientModel) renderAgentTitle(agent state.Agent) string {
	return renderAgentTitleForState(m.cfg, m.snapshot.State, agent)
}

func (m ClientModel) messageText() string {
	if m.message != "" {
		return m.message
	}
	return m.snapshot.Message
}

func tickClientSnapshot() tea.Cmd {
	return tea.Tick(clientSnapshotInterval, func(time.Time) tea.Msg {
		return clientSnapshotTick{}
	})
}

func codexInputArgs(msg tea.KeyMsg) map[string]string {
	args := map[string]string{"encoded": string(encodeKey(msg))}
	switch msg.Type {
	case tea.KeyRunes:
		args["input"] = "text"
		args["text"] = string(msg.Runes)
	case tea.KeySpace:
		args["input"] = "space"
	case tea.KeyShiftTab:
		args["input"] = codexInputShiftTab
	case tea.KeyBackspace:
		if msg.Alt {
			args["input"] = "alt+backspace"
		} else {
			args["input"] = "backspace"
		}
	case tea.KeyCtrlH:
		if msg.Alt {
			args["input"] = "alt+backspace"
		} else {
			args["input"] = "ctrl+h"
		}
	case tea.KeyEnter:
		args["input"] = "enter"
	default:
		key := strings.ToLower(msg.String())
		if strings.HasPrefix(key, "ctrl+") {
			args["input"] = key
		}
		if len([]rune(key)) == 1 && !unicode.IsControl([]rune(key)[0]) {
			args["input"] = "text"
			args["text"] = key
		}
	}
	return args
}
