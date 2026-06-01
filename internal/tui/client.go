package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/weft/internal/codexsession"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
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

type clientAttachRetryTick struct{}

type clientToastTick struct {
	id int
}

type ClientModel struct {
	cfg               config.Config
	runtime           config.Runtime
	clientID          string
	snapshot          ipc.Snapshot
	width             int
	height            int
	mode              mode
	message           string
	upgrade           *ipc.Upgrade
	loading           int
	supervisorVersion string

	input                    textinput.Model
	prompt                   promptKind
	confirm                  confirmKind
	pendingID                string
	newTaskIndex             int
	promptSuggestionOpen     bool
	promptSuggestionIndex    int
	editGroupField           int
	editGroupSilent          bool
	loadingTickerActive      bool
	launchWorkspacePrompted  bool
	lastResumeScan           time.Time
	toastText                string
	toastID                  int
	mouseSelection           consoleSelection
	newWorkspaceCardSelected bool
	codexScrollOffset        int
	codexScrollAgentID       string
	inputRouter              *clientInputRouter
}

func RunClient(rt config.Runtime, cfg config.Config) error {
	model := NewClientModel(rt, cfg)
	inputRouter := newClientInputRouter(os.Stdin, rt, model.clientID, cfg.KeyBindings.Drawer)
	model.inputRouter = inputRouter
	enableTerminalKeyboardReporting()
	defer disableTerminalKeyboardReporting()
	options := []tea.ProgramOption{
		tea.WithInput(inputRouter),
		tea.WithOutput(os.Stdout),
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(),
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
		snapshot: ipc.Snapshot{State: st, CodexTitle: "Task", CodexContent: "No task open.", NavWidth: workspaceNavFrameWidth(st, 100)},
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
		if typed.err != nil {
			if typed.command == "attach_client" && typed.response.Error == nil {
				m.message = typed.err.Error()
				return m, tickClientAttachRetry()
			}
			m.message = typed.err.Error()
			return m, nil
		}
		m.applyResponse(typed.response)
		nextLoadingTick := m.ensureLoadingTick()
		if typed.response.Snapshot != nil && typed.response.Snapshot.DetachClient {
			return m, tea.Batch(nextLoadingTick, m.request("client_detached", nil), tea.Quit)
		}
		return m, nextLoadingTick
	case clientSnapshotTick:
		return m, tea.Batch(m.request("snapshot", nil), tickClientSnapshot())
	case clientAttachRetryTick:
		return m, m.request("attach_client", nil)
	case clientToastTick:
		if typed.id == m.toastID {
			m.toastText = ""
		}
		return m, nil
	case loadingTick:
		if !m.hasLoadingAnimation() {
			m.loadingTickerActive = false
			return m, nil
		}
		m.loading++
		return m, tickLoading()
	case tea.MouseMsg:
		return m.handleMouse(typed)
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
	if m.mode == modeNewTask {
		return m.modalView(renderNewTaskModal(m.cfg, m.newTaskIndex, max(36, min(m.width-16, 72))))
	}
	loadingText := m.snapshot.LoadingText
	loadingFrame := loadingFrames[m.loading%len(loadingFrames)]
	options := m.workspaceRenderOptions()
	options.loadingFrame = loadingFrame
	options.previewHeaderAnimation = livePreviewAnimationFrame(m.loading)
	dashboardState := m.dashboardState()
	if loadingText != "" {
		loadingText = loadingFrame + strings.TrimPrefix(loadingText, loadingFrames[0])
		options.loadingText = loadingText
		return renderWorkspaceView(m.cfg, dashboardState, m.snapshot.CodexTitle, "", m.width, m.height, m.messageText(), m.snapshot.NavWidth, m.snapshot.GroupCursor, options)
	}
	codexContent := m.codexVisibleContent()
	if m.mouseSelection.active {
		if area, ok := m.codexSelectionAreaForOffset(m.mouseSelection.colOffset); ok {
			codexContent = selectedStyledCodexContent(codexContent, m.mouseSelection, area.width)
			if strings.TrimSpace(codexContent) == "" {
				codexContent = selectedCodexContent(m.codexPlainLines(), m.mouseSelection, area.width)
			}
		}
	}
	return renderWorkspaceView(m.cfg, dashboardState, m.snapshot.CodexTitle, codexContent, m.width, m.height, m.messageText(), m.snapshot.NavWidth, m.snapshot.GroupCursor, options)
}

func (m ClientModel) dashboardState() state.State {
	st := m.snapshot.State
	if m.newWorkspaceCardSelected {
		st.Focus = state.FocusWorkspaces
		st.NavOpen = true
		st.SelectedWorkspaceID = ""
		st.SelectedGroupID = ""
		st.SelectedAgentID = ""
	}
	return st
}

func (m ClientModel) workspaceRenderOptions() workspaceRenderOptions {
	return workspaceRenderOptions{
		loadingAgents:            loadingAgentSet(m.snapshot.LoadingAgentIDs),
		workspaceFooterText:      workspaceUpgradeFooterText(m.upgrade, m.snapshot.State),
		workspaceInfoText:        m.workspaceInfoHeaderText(),
		newWorkspaceCardSelected: m.newWorkspaceCardSelected,
		codexToastText:           m.toastText,
	}
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
	if m.mode == modeNewTask {
		return m.handleNewTaskKey(msg)
	}

	if m.snapshot.State.Focus == state.FocusCodex &&
		state.ActiveAgent(m.snapshot.State) != nil &&
		m.inputRouter != nil &&
		!m.inputRouter.TaskInputActive() {
		m.snapshot.State.Focus = state.FocusAgents
		m.snapshot.State.NavOpen = true
	}
	if bindingMatches(m.cfg.KeyBindings.Drawer, msg) {
		return m, m.request("toggle_drawer", nil)
	}
	if m.snapshot.State.Focus == state.FocusCodex && state.ActiveAgent(m.snapshot.State) != nil {
		return m, nil
	}
	if m.snapshot.State.Focus == state.FocusCodex {
		return m, m.request("toggle_drawer", nil)
	}
	if bindingMatches(m.cfg.KeyBindings.Quit, msg) {
		return m, tea.Quit
	}
	if bindingMatches(m.cfg.KeyBindings.Help, msg) {
		m.mode = modeHelp
		return m, nil
	}
	return m.handleNavKey(msg)
}

func (m ClientModel) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.newWorkspaceCardSelected && !m.newWorkspaceCardVisible() {
		m.newWorkspaceCardSelected = false
	}
	switch {
	case bindingMatches(m.cfg.KeyBindings.FocusLeft, msg):
		m.newWorkspaceCardSelected = false
		return m, m.request("focus", map[string]string{"target": "workspaces"})
	case bindingMatches(m.cfg.KeyBindings.FocusRight, msg):
		m.newWorkspaceCardSelected = false
		return m, m.request("focus", map[string]string{"target": string(state.FocusAgents)})
	case msg.Type == tea.KeyShiftUp:
		return m.reorderSelectedRow(-1)
	case msg.Type == tea.KeyShiftDown:
		return m.reorderSelectedRow(1)
	case bindingMatches(m.cfg.KeyBindings.SelectPrev, msg) || msg.Type == tea.KeyUp:
		if m.newWorkspaceCardSelected {
			m.newWorkspaceCardSelected = false
			return m, nil
		}
		return m, m.request("nav_move", map[string]string{"delta": "-1"})
	case bindingMatches(m.cfg.KeyBindings.SelectNext, msg) || msg.Type == tea.KeyDown:
		if m.shouldMoveToNewWorkspaceCard() {
			m.newWorkspaceCardSelected = true
			return m, nil
		}
		if m.newWorkspaceCardSelected {
			return m, nil
		}
		return m, m.request("nav_move", map[string]string{"delta": "1"})
	case bindingMatches(m.cfg.KeyBindings.NewWorkspace, msg):
		m.newWorkspaceCardSelected = false
		m.startPrompt(promptWorkspace, defaultWorkspacePromptValue(m.snapshot.State, m.runtime.Workspace))
	case bindingMatches(m.cfg.KeyBindings.NewGroup, msg):
		m.newWorkspaceCardSelected = false
		m.startPrompt(promptGroup, "")
	case bindingMatches(m.cfg.KeyBindings.NewTask, msg):
		m.newWorkspaceCardSelected = false
		m.startNewTaskMenu()
	case m.canActOnUpgrade() && strings.EqualFold(msg.String(), "u"):
		m.newWorkspaceCardSelected = false
		m.startUpgradeConfirm()
	case bindingMatches(m.cfg.KeyBindings.MoveTask, msg):
		m.newWorkspaceCardSelected = false
		if agent := m.selectedAgent(); agent != nil {
			m.startPrompt(promptMoveAgent, "")
		}
	case bindingMatches(m.cfg.KeyBindings.Edit, msg):
		m.newWorkspaceCardSelected = false
		m.startEditPrompt()
	case bindingMatches(m.cfg.KeyBindings.Delete, msg):
		m.newWorkspaceCardSelected = false
		m.startDeleteConfirm()
	case bindingMatches(m.cfg.KeyBindings.Open, msg) || msg.Type == tea.KeyEnter:
		if m.newWorkspaceCardSelected {
			m.startPrompt(promptWorkspace, defaultWorkspacePromptValue(m.snapshot.State, m.runtime.Workspace))
			return m, nil
		}
		return m, m.request("open", nil)
	}
	return m, nil
}

func (m ClientModel) shouldMoveToNewWorkspaceCard() bool {
	if m.newWorkspaceCardSelected || m.snapshot.State.Focus != state.FocusWorkspaces || len(m.snapshot.State.Workspaces) == 0 || !m.newWorkspaceCardVisible() {
		return false
	}
	return m.snapshot.State.SelectedWorkspaceID == m.snapshot.State.Workspaces[len(m.snapshot.State.Workspaces)-1].ID
}

func (m ClientModel) newWorkspaceCardVisible() bool {
	_, ok := newWorkspaceTemplateCardAreaFor(m.cfg, m.dashboardState(), m.width, m.height, m.snapshot.NavWidth, m.workspaceRenderOptions())
	return ok
}

func (m ClientModel) handleNewTaskKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	nextIndex, submit, cancel := handleNewTaskKey(m.cfg, m.newTaskIndex, msg)
	m.newTaskIndex = nextIndex
	if cancel {
		m.mode = modeNormal
		return m, nil
	}
	if !submit {
		return m, nil
	}
	taskType, ok := selectedTaskType(m.cfg, m.newTaskIndex)
	if !ok {
		m.mode = modeNormal
		m.message = "no task types configured"
		return m, nil
	}
	m.mode = modeNormal
	return m, m.request("new", map[string]string{"type": taskType.ID})
}

func (m ClientModel) reorderSelectedRow(delta int) (tea.Model, tea.Cmd) {
	if m.snapshot.State.Focus != state.FocusAgents {
		return m, nil
	}
	row := currentGroupRowForState(m.snapshot.State, m.snapshot.GroupCursor)
	switch row.kind {
	case groupRowGroup:
		return m, m.request("reorder_group", map[string]string{
			"id":    row.groupID,
			"delta": strconv.Itoa(delta),
		})
	case groupRowAgent:
		return m.reorderSelectedAgent(delta)
	default:
		return m, nil
	}
}

func (m ClientModel) reorderSelectedAgent(delta int) (tea.Model, tea.Cmd) {
	if m.snapshot.State.Focus != state.FocusAgents {
		return m, nil
	}
	agent := m.selectedAgent()
	if agent == nil {
		return m, nil
	}
	return m, m.request("reorder_agent", map[string]string{
		"id":    agent.ID,
		"delta": strconv.Itoa(delta),
	})
}

func (m ClientModel) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.prompt == promptWorkspace && m.newWorkspaceCardSelected && msg.Type == tea.KeyEsc {
		m.mode = modeNormal
		m.promptSuggestionOpen = false
		m.promptSuggestionIndex = 0
		return m, nil
	}
	if m.prompt == promptGroup || m.prompt == promptEditGroup {
		result := handleEditGroupPromptInputKey(m.input, m.promptContext(), m.editGroupField, m.editGroupSilent, msg)
		m.input = result.input
		m.editGroupField = result.field
		m.editGroupSilent = result.silent
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
	result := handlePromptInputKey(m.input, m.promptContext(), m.promptSuggestionOpen, m.promptSuggestionIndex, msg)
	m.input = result.input
	m.promptSuggestionOpen = result.suggestionOpen
	m.promptSuggestionIndex = result.suggestionIndex
	if result.message != "" {
		m.message = result.message
	}
	switch result.action {
	case promptInputCancel:
		m.mode = modeNormal
		return m, nil
	case promptInputSubmit:
		if m.prompt == promptWorkspace {
			m.newWorkspaceCardSelected = false
		}
		cmd := m.applyPrompt(result.value)
		m.mode = modeNormal
		return m, cmd
	default:
		return m, result.cmd
	}
}

func (m ClientModel) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if confirmKeySubmits(m.confirm, msg) {
		cmd := m.applyConfirm()
		m.mode = modeNormal
		return m, cmd
	}
	if confirmKeyCancels(m.confirm, msg) {
		m.mode = modeNormal
	}
	return m, nil
}

func (m *ClientModel) startPrompt(prompt promptKind, value string) {
	m.prompt = prompt
	configurePromptInput(&m.input, m.promptContext(), value)
	m.promptSuggestionOpen = promptShouldOpenSuggestions(m.promptContext(), m.input.Value())
	m.promptSuggestionIndex = 0
	m.editGroupField = 0
	if prompt != promptGroup && prompt != promptEditGroup {
		m.editGroupSilent = false
	}
	m.mode = modeInput
}

func (m *ClientModel) startEditPrompt() {
	prompt, id, value, silent, ok := editPromptTargetForState(m.snapshot.State, m.snapshot.GroupCursor)
	if ok {
		m.pendingID = id
		m.editGroupSilent = silent
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

func (m *ClientModel) startNewTaskMenu() {
	if state.ActiveWorkspace(m.snapshot.State) == nil {
		m.message = "add a workspace first"
		return
	}
	m.newTaskIndex = defaultTaskTypeIndex(m.cfg)
	m.mode = modeNewTask
}

func (m *ClientModel) startUpgradeConfirm() {
	if m.upgrade == nil {
		return
	}
	if !m.canUpgradeResumeNow() {
		if blocked := codexsession.LiveNonCodexTaskCount(m.snapshot.State); blocked > 0 {
			m.message = fmt.Sprintf("Upgrade waits until %d non-resumable task(s) stop.", blocked)
			return
		}
		m.message = upgradeResumeWaitingMessage(codexsession.BuildReport(m.snapshot.State))
		return
	}
	m.confirm = confirmUpgradeResume
	m.pendingID = upgradeTarget(*m.upgrade)
	m.mode = modeConfirm
}

func (m ClientModel) applyPrompt(value string) tea.Cmd {
	switch m.prompt {
	case promptWorkspace:
		return m.request("add_workspace", map[string]string{"path": value})
	case promptGroup:
		return m.request("add_group", map[string]string{"path": value, "silent": fmt.Sprintf("%t", m.editGroupSilent)})
	case promptEditGroup:
		return m.request("rename_group", map[string]string{"id": m.pendingID, "path": value, "silent": fmt.Sprintf("%t", m.editGroupSilent)})
	case promptWorkspaceTitle:
		return m.request("rename_workspace", map[string]string{"id": m.pendingID, "title": value})
	case promptEditAgent:
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
	case confirmUpgradeResume:
		return m.request("upgrade_resume", nil)
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
		response, err := ipc.Call(rt.SocketPath, ipc.Request{Command: command, Args: args}, clientRequestTimeout(command))
		return clientResponseMsg{command: command, response: response, err: err}
	}
}

func clientRequestTimeout(command string) time.Duration {
	if command == "attach_client" {
		return 5 * time.Second
	}
	return 2 * time.Second
}

func clientRequestArgs(rt config.Runtime, clientID string, command string, args map[string]string) map[string]string {
	next := cloneArgs(args)
	next["client_id"] = clientID
	if command == "attach_client" {
		next["launch_workspace"] = rt.Workspace
	}
	if command == "upgrade_resume" {
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
	if strings.TrimSpace(response.SupervisorVersion) != "" {
		m.supervisorVersion = strings.TrimSpace(response.SupervisorVersion)
	}
	if strings.TrimSpace(response.Message) != "" {
		m.message = response.Message
	}
	if response.Snapshot != nil && strings.TrimSpace(response.Snapshot.Message) != "" {
		m.message = response.Snapshot.Message
	}
	if response.Upgrade != nil {
		upgrade := *response.Upgrade
		m.upgrade = &upgrade
		m.prepareSnapshotUpgradeResume(upgrade)
		m.message = dashboardUpgradeMessage(upgrade, m.snapshot.State)
	} else {
		m.upgrade = nil
	}
	m.maybePromptForLaunchWorkspace()
	m.syncCodexScroll()
	m.syncInputRouter()
}

func (m ClientModel) workspaceInfoHeaderText() string {
	clientVersion := strings.TrimSpace(m.snapshot.ActiveClientVersion)
	if clientVersion == "" && m.upgrade != nil {
		clientVersion = strings.TrimSpace(m.upgrade.ClientVersion)
	}
	if clientVersion == "" {
		clientVersion = version.Version
	}
	supervisorVersion := strings.TrimSpace(m.supervisorVersion)
	if supervisorVersion == "" {
		supervisorVersion = "starting"
	}
	return fmt.Sprintf("Weft\n%-10s %s\n%-10s %s", "CLI", clientVersion, "Supervisor", supervisorVersion)
}

func (m *ClientModel) syncCodexScroll() {
	activeID := ""
	if active := state.ActiveAgent(m.snapshot.State); active != nil {
		activeID = active.ID
	}
	if activeID == "" || activeID != m.codexScrollAgentID {
		m.codexScrollAgentID = activeID
		m.codexScrollOffset = 0
		return
	}
	m.codexScrollOffset = min(max(0, m.codexScrollOffset), m.maxCodexScrollOffset())
}

func (m *ClientModel) syncInputRouter() {
	if m.inputRouter == nil {
		return
	}
	active := m.mode == modeNormal &&
		m.snapshot.State.Focus == state.FocusCodex &&
		state.ActiveAgent(m.snapshot.State) != nil
	if !active {
		m.inputRouter.SetTaskInputMode(taskInputNone)
		return
	}
	if agent := state.ActiveAgent(m.snapshot.State); agent != nil && agentUsesCodexIntegration(m.cfg, *agent) {
		m.inputRouter.SetTaskInputMode(taskInputCodex)
		return
	}
	m.inputRouter.SetTaskInputMode(taskInputTerminal)
}

func (m *ClientModel) ensureLoadingTick() tea.Cmd {
	if !m.hasLoadingAnimation() || m.loadingTickerActive {
		return nil
	}
	m.loadingTickerActive = true
	return tickLoading()
}

func (m ClientModel) hasLoadingAnimation() bool {
	return strings.TrimSpace(m.snapshot.LoadingText) != "" ||
		len(m.snapshot.LoadingAgentIDs) > 0 ||
		m.mode == modeNormal && m.snapshot.State.NavOpen && state.ActiveAgent(m.snapshot.State) != nil
}

func dashboardUpgradeMessage(upgrade ipc.Upgrade, st state.State) string {
	if upgrade.AutoRestarted {
		return upgrade.Message
	}
	if !upgrade.Compatible {
		return upgrade.Message
	}
	if !upgrade.RestartRequired {
		return upgrade.Message
	}
	delta := fmt.Sprintf("supervisor %s → %s", upgrade.SupervisorVersion, upgrade.ClientVersion)
	report := codexsession.BuildReport(st)
	if blocked := codexsession.LiveNonCodexTaskCount(st); blocked > 0 {
		return fmt.Sprintf("Upgrade pending: %s must restart. Stop %d non-resumable task(s) first; reopening alone is not enough.", delta, blocked)
	}
	if len(report.Busy) > 0 {
		return fmt.Sprintf("Upgrade pending: %s must restart. Wait for %d Codex task(s) to become idle; reopening alone is not enough.", delta, len(report.Busy))
	}
	if len(report.Missing) > 0 {
		return fmt.Sprintf("Upgrade pending: %s must restart. Waiting for %d Codex session id(s); reopening alone is not enough.", delta, len(report.Missing))
	}
	if report.Total > 0 {
		return fmt.Sprintf("Upgrade ready: %s can restart and resume %d idle Codex task(s). Press U to continue.", delta, report.Total)
	}
	return fmt.Sprintf("Upgrade ready: %s is idle. Press U to restart it now.", delta)
}

func workspaceUpgradeFooterText(upgrade *ipc.Upgrade, st state.State) string {
	if upgrade == nil || !upgrade.RestartRequired {
		return ""
	}
	delta := fmt.Sprintf("supervisor %s → %s", upgrade.SupervisorVersion, upgrade.ClientVersion)
	if !upgrade.Compatible {
		return fmt.Sprintf("Upgrade blocked: client %s, supervisor %s.\nStop tasks before forced restart.", upgrade.ClientVersion, upgrade.SupervisorVersion)
	}
	report := codexsession.BuildReport(st)
	if blocked := codexsession.LiveNonCodexTaskCount(st); blocked > 0 {
		return fmt.Sprintf("Upgrade pending: %s.\nStop %d non-resumable task(s) first.", delta, blocked)
	}
	if len(report.Busy) > 0 {
		return fmt.Sprintf("Upgrade pending: %s.\nWait for %d Codex task(s) to become idle.", delta, len(report.Busy))
	}
	if len(report.Missing) > 0 {
		return fmt.Sprintf("Upgrade pending: %s.\nWaiting for %d Codex session id(s).", delta, len(report.Missing))
	}
	if report.Total > 0 {
		return fmt.Sprintf("Upgrade ready: %s.\nPress U to upgrade and resume %d idle Codex task(s).", delta, report.Total)
	}
	return fmt.Sprintf("Upgrade ready: %s.\nPress U to restart now.", delta)
}

func upgradeTarget(upgrade ipc.Upgrade) string {
	return fmt.Sprintf("supervisor %s → %s", upgrade.SupervisorVersion, upgrade.ClientVersion)
}

func (m ClientModel) canUpgradePending() bool {
	return m.upgrade != nil && m.upgrade.Compatible && m.upgrade.RestartRequired
}

func (m ClientModel) canUpgradeResumeNow() bool {
	return m.canUpgradePending() &&
		codexsession.LiveNonCodexTaskCount(m.snapshot.State) == 0 &&
		codexsession.BuildReport(m.snapshot.State).CanUpgrade()
}

func (m ClientModel) canActOnUpgrade() bool {
	return m.canUpgradePending() && m.canUpgradeResumeNow()
}

func (m *ClientModel) prepareSnapshotUpgradeResume(upgrade ipc.Upgrade) {
	if !upgrade.Compatible || !upgrade.RestartRequired {
		return
	}
	report := codexsession.BuildReport(m.snapshot.State)
	if len(report.Missing) == 0 || len(report.Busy) > 0 || time.Since(m.lastResumeScan) < time.Second {
		return
	}
	next, prepared := codexsession.PrepareResumeState(m.snapshot.State, m.runtime.Workspace)
	m.snapshot.State = next
	m.lastResumeScan = time.Now()
	if prepared.Assigned > 0 {
		m.message = fmt.Sprintf("Found %d Codex session id(s) for upgrade resume.", prepared.Assigned)
	}
}

func upgradeResumeWaitingMessage(report codexsession.Report) string {
	if len(report.Busy) > 0 {
		return fmt.Sprintf("Upgrade waits until %d Codex task(s) are idle.", len(report.Busy))
	}
	if len(report.Missing) > 0 {
		return fmt.Sprintf("Upgrade waits until %d Codex session id(s) are available.", len(report.Missing))
	}
	return "Upgrade is not ready yet."
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
	if m.prompt == promptGroup || m.prompt == promptEditGroup {
		return renderEditGroupPromptModal(m.promptContext(), m.input, width, m.height, m.editGroupField, m.editGroupSilent)
	}
	return renderPromptModal(m.promptContext(), m.input, width, m.height, m.promptSuggestionOpen, m.promptSuggestionIndex, m.renderPromptExtra(m.input, width))
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

func (m ClientModel) selectedAgent() *state.Agent {
	return selectedAgentForState(m.snapshot.State, m.snapshot.GroupCursor)
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

func tickClientAttachRetry() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return clientAttachRetryTick{}
	})
}

func (m *ClientModel) setToast(text string) tea.Cmd {
	m.toastID++
	m.toastText = text
	id := m.toastID
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg {
		return clientToastTick{id: id}
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
	if isCtrlCKey(msg) {
		args["input"] = "ctrl+c"
		args["encoded"] = terminalKeyboardCtrlC
	}
	return args
}
