package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/weft/internal/codexsession"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/navigation"
	"github.com/edwmurph/weft/internal/ptyx"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/titlehook"
	"github.com/edwmurph/weft/internal/titles"
)

type ptyStartedMsg struct {
	agentID string
	session *ptyx.Session
	err     error
}

type titleHookMsg struct {
	agentID string
	title   string
	err     error
}

type mode string

const (
	modeNormal  mode = ""
	modeHelp    mode = "help"
	modeInput   mode = "input"
	modeConfirm mode = "confirm"
	modeNewTask mode = "new-task"
)

type promptKind string

const (
	promptWorkspace      promptKind = "workspace"
	promptGroup          promptKind = "group"
	promptWorkspaceTitle promptKind = "workspace-title"
	promptEditGroup      promptKind = "edit-group"
	promptEditAgent      promptKind = "edit-agent"
	promptMoveAgent      promptKind = "move-agent"
)

type confirmKind string

const (
	confirmAddLaunchWorkspace confirmKind = "add-launch-workspace"
	confirmDeleteWorkspace    confirmKind = "delete-workspace"
	confirmDeleteGroup        confirmKind = "delete-group"
	confirmDeleteAgent        confirmKind = "delete-agent"
	confirmUpgradeResume      confirmKind = "upgrade-resume"
)

const (
	navAnimationInterval        = 12 * time.Millisecond
	navAnimationStep            = 4
	loadingInterval             = 90 * time.Millisecond
	terminalCommandLoadingFloor = 250 * time.Millisecond
)

type navAnimationTick struct{}
type loadingTick struct{}

var loadingFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type groupRowKind string

const (
	groupRowGroup groupRowKind = "group"
	groupRowAgent groupRowKind = "agent"
)

type groupRow struct {
	kind    groupRowKind
	groupID string
	agentID string
}

type Model struct {
	cfg      config.Config
	runtime  config.Runtime
	store    *state.Store
	state    state.State
	width    int
	height   int
	mode     mode
	message  string
	navWidth int
	loading  int

	screens           map[string]*TerminalScreen
	ptys              map[string]*ptyx.Session
	visible           map[string]bool
	codexInputBuffers map[string][]rune
	terminalCommands  map[string]time.Time
	agentInterrupts   map[string]time.Time
	sessionCaptures   map[string]time.Time
	dataCh            chan ptyx.Data
	ctx               context.Context
	cancel            context.CancelFunc

	input                 textinput.Model
	prompt                promptKind
	confirm               confirmKind
	pendingID             string
	newTaskIndex          int
	groupCursor           int
	groupCursorPinned     bool
	lastNavFocus          state.Focus
	promptSuggestionOpen  bool
	promptSuggestionIndex int
	editGroupField        int
	editGroupSilent       bool
}

func NewModel(rt config.Runtime, cfg config.Config, st state.State) Model {
	ctx, cancel := context.WithCancel(context.Background())
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 240
	input.Width = 60
	st = state.Repair(st, rt.Workspace)
	if state.ActiveAgent(st) == nil {
		st.ActiveAgentID = ""
		if len(st.Workspaces) == 0 {
			st.Focus = state.FocusWorkspaces
		} else {
			st.Focus = state.FocusAgents
		}
		st.NavOpen = true
	}
	lastNav := st.Focus
	if lastNav == state.FocusCodex || lastNav == "" {
		lastNav = state.FocusAgents
	}
	model := Model{
		cfg: cfg, runtime: rt, store: state.NewStore(rt.StatePath, rt.Workspace), state: st,
		width: 100, height: 32, screens: map[string]*TerminalScreen{}, ptys: map[string]*ptyx.Session{},
		visible:           map[string]bool{},
		codexInputBuffers: map[string][]rune{},
		terminalCommands:  map[string]time.Time{},
		agentInterrupts:   map[string]time.Time{},
		sessionCaptures:   map[string]time.Time{},
		dataCh:            make(chan ptyx.Data, 64),
		ctx:               ctx, cancel: cancel, input: input, lastNavFocus: lastNav,
	}
	if next, ok := state.SelectWorkspaceByPath(model.state, rt.Workspace); ok {
		model.state = next
	}
	model.syncGroupCursor()
	model.navWidth = model.targetNavWidth()
	for _, agent := range model.state.Agents {
		model.startPTY(agent.ID)
	}
	_ = model.store.Write(model.state)
	return model
}

func (m *Model) HandleSupervisorRequest(request ipc.Request) (ipc.Response, tea.Cmd) {
	return m.handleIPC(request)
}

func (m *Model) Data() <-chan ptyx.Data {
	return m.dataCh
}

func (m *Model) ApplyPTYData(data ptyx.Data) {
	m.applyPTYData(data)
}

func (m *Model) Stop() {
	m.cancel()
	for id, pty := range m.ptys {
		pty.Kill()
		delete(m.ptys, id)
	}
}

func (m *Model) Snapshot() ipc.Snapshot {
	content := m.activeOutput()
	plainLines := m.activePlainLines()
	scrollbackContent := m.activeScrollbackOutput()
	scrollbackPlainLines := m.activeScrollbackPlainLines()
	loadingText := ""
	if content == "" && m.codexLoading() {
		loadingText = m.loadingLabel()
	} else if content == "" && m.activeErrorText() != "" {
		content = m.activeErrorText()
	} else if content == "" {
		content = "No task open."
	}
	title := "Task"
	if active := state.ActiveAgent(m.state); active != nil {
		title = m.renderAgentTitle(*active)
	}
	return ipc.Snapshot{
		State:                m.state,
		CodexTitle:           title,
		CodexContent:         content,
		CodexPlainLines:      plainLines,
		CodexScrollback:      scrollbackContent,
		CodexScrollbackLines: scrollbackPlainLines,
		LoadingText:          loadingText,
		LoadingAgentIDs:      m.loadingAgentIDs(),
		Message:              m.message,
		NavWidth:             m.targetNavWidth(),
		GroupCursor:          m.groupCursor,
	}
}

func (m *Model) LiveAgentCount() int {
	count := ipc.RunningAgentCount(&m.state)
	counted := map[string]bool{}
	for _, agent := range m.state.Agents {
		switch agent.Status {
		case state.StatusStarting, state.StatusRunning, state.StatusReady, state.StatusSitting, state.StatusShipping:
			counted[agent.ID] = true
		}
	}
	for id, pty := range m.ptys {
		if pty == nil || counted[id] || state.AgentByID(m.state, id) == nil {
			continue
		}
		count++
	}
	return count
}

func (m *Model) PrepareUpgradeResume() codexsession.Report {
	next, report := codexsession.PrepareResumeState(m.state, m.runtime.Workspace)
	if report.Assigned > 0 {
		m.state = next
		m.save()
	}
	return report
}

func (m Model) activeErrorText() string {
	active := state.ActiveAgent(m.state)
	if active == nil || active.Status != state.StatusError {
		return ""
	}
	detail := strings.TrimSpace(active.CodexTitle)
	if detail == "" {
		detail = "unknown error"
	}
	label := taskTypeForAgent(m.cfg, *active).Label
	if strings.TrimSpace(label) == "" {
		label = "Task"
	}
	return label + " failed to start:\n" + detail
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitPTY(m.dataCh), tickLoading())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		m.navWidth = m.targetNavWidth()
		m.resizePTYs()
		m.resizeScreens()
		return m, nil
	case navAnimationTick:
		return m, m.stepNavAnimation()
	case loadingTick:
		m.refreshTerminalTaskActivity()
		if !m.anyAgentLoading() {
			return m, nil
		}
		m.loading++
		return m, tickLoading()
	case ptyx.Data:
		m.applyPTYData(typed)
		return m, waitPTY(m.dataCh)
	case ptyStartedMsg:
		m.applyPTYStarted(typed)
		return m, nil
	case titleHookMsg:
		m.applyTitleHook(typed)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(typed)
	}
	if input, ok := enhancedKeyboardInputFromMsg(msg); ok {
		return m.handleEnhancedKeyboardInput(input)
	}
	return m, nil
}

func (m Model) View() string {
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
	content := m.activeOutput()
	loadingText := ""
	if content == "" && m.codexLoading() {
		loadingText = m.loadingLabel()
	} else if content == "" {
		content = "No task open."
	}
	title := "Task"
	if active := state.ActiveAgent(m.state); active != nil {
		title = m.renderAgentTitle(*active)
	}
	if loadingText != "" {
		return renderLoadingWorkspaceWithNavWidthAndAgents(m.cfg, m.state, title, loadingText, m.loadingFrame(), m.loadingAgentSet(), m.width, m.height, m.message, m.navWidth, m.groupCursor)
	}
	return renderWorkspaceWithNavWidthAndAgents(m.cfg, m.state, title, content, m.loadingFrame(), m.loadingAgentSet(), m.width, m.height, m.message, m.navWidth, m.groupCursor)
}

func (m Model) modalView(content string) string {
	w := max(40, min(m.width-4, 82))
	box := modalStyle.Width(w).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderInputModal() string {
	width := max(36, min(m.width-16, 72))
	if m.prompt == promptGroup || m.prompt == promptEditGroup {
		return renderEditGroupPromptModal(m.promptContext(), m.input, width, m.height, m.editGroupField, m.editGroupSilent)
	}
	return renderPromptModal(m.promptContext(), m.input, width, m.height, m.promptSuggestionOpen, m.promptSuggestionIndex, m.renderPromptExtra(m.input, width))
}

func (m Model) promptContext() promptContext {
	return promptContextFor(m.prompt, m.pendingID, m.state, m.selectedAgent())
}

func (m Model) renderPromptExtra(input textinput.Model, width int) []string {
	return renderPromptExtraForState(m.cfg, m.state, m.prompt, m.selectedAgent(), input, width)
}

func (m Model) renderConfirmModal() string {
	width := max(36, min(m.width-16, 72))
	return renderConfirmPrompt(m.confirm, confirmTarget(m.confirm, m.state, m.pendingID, m.renderAgentTitle), width)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

	if bindingMatches(m.cfg.KeyBindings.Drawer, msg) {
		return m, m.toggleDrawer()
	}
	if m.state.Focus == state.FocusCodex && state.ActiveAgent(m.state) != nil {
		if active := state.ActiveAgent(m.state); active != nil {
			return m, m.applyCodexInput(codexInputArgs(msg))
		}
		return m, nil
	}
	if m.state.Focus == state.FocusCodex {
		cmd := m.openNav()
		updated, nextCmd := m.handleNavKey(msg)
		return updated, tea.Batch(cmd, nextCmd)
	}
	if bindingMatches(m.cfg.KeyBindings.Quit, msg) {
		m.closeWeft()
		return m, nil
	}
	if bindingMatches(m.cfg.KeyBindings.Help, msg) {
		m.mode = modeHelp
		return m, nil
	}
	return m.handleNavKey(msg)
}

func (m Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		cmd := m.applyPrompt(result.value)
		m.mode = modeNormal
		return m, cmd
	default:
		return m, result.cmd
	}
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m Model) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case bindingMatches(m.cfg.KeyBindings.FocusLeft, msg):
		m.focusNavPane(state.FocusWorkspaces)
	case bindingMatches(m.cfg.KeyBindings.FocusRight, msg):
		m.focusNavPane(state.FocusAgents)
	case msg.Type == tea.KeyShiftUp:
		m.reorderSelectedAgent(-1)
	case msg.Type == tea.KeyShiftDown:
		m.reorderSelectedAgent(1)
	case bindingMatches(m.cfg.KeyBindings.SelectPrev, msg) || msg.Type == tea.KeyUp:
		m.moveSelection(-1)
	case bindingMatches(m.cfg.KeyBindings.SelectNext, msg) || msg.Type == tea.KeyDown:
		m.moveSelection(1)
	case bindingMatches(m.cfg.KeyBindings.NewWorkspace, msg):
		m.startPrompt(promptWorkspace, defaultWorkspacePromptValue(m.state, m.runtime.Workspace))
	case bindingMatches(m.cfg.KeyBindings.NewGroup, msg):
		m.focusNavPane(state.FocusAgents)
		m.startPrompt(promptGroup, "")
	case bindingMatches(m.cfg.KeyBindings.NewAgent, msg):
		m.startNewTaskMenu()
	case bindingMatches(m.cfg.KeyBindings.MoveAgent, msg):
		if agent := m.selectedAgent(); agent != nil {
			m.startPrompt(promptMoveAgent, "")
		}
	case bindingMatches(m.cfg.KeyBindings.Edit, msg):
		m.startEditPrompt()
	case bindingMatches(m.cfg.KeyBindings.Delete, msg):
		m.startDeleteConfirm()
	case bindingMatches(m.cfg.KeyBindings.Open, msg) || msg.Type == tea.KeyEnter:
		if m.state.Focus == state.FocusWorkspaces {
			m.focusNavPane(state.FocusAgents)
			return m, nil
		}
		row := m.currentGroupRow()
		if row.kind == groupRowGroup {
			m.toggleSelectedGroup(row.groupID)
			return m, nil
		}
		if agent := m.selectedAgent(); agent != nil {
			m.state.SelectedAgentID = agent.ID
			m.state.ActiveAgentID = agent.ID
			m.state.SelectedWorkspaceID = agent.WorkspaceID
			m.state.SelectedGroupID = agent.GroupID
			m.save()
			return m, m.setCodexFocus()
		}
	}
	return m, nil
}

func (m Model) handleNewTaskKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	return m, m.newAgent("", taskType.ID)
}

func (m *Model) focusNavPane(focus state.Focus) {
	if focus != state.FocusWorkspaces && focus != state.FocusAgents {
		return
	}
	m.state.Focus = focus
	m.state.NavOpen = true
	m.lastNavFocus = focus
	m.save()
}

func (m *Model) rememberCurrentNavFocus() {
	if m.state.Focus == state.FocusWorkspaces || m.state.Focus == state.FocusAgents {
		m.lastNavFocus = m.state.Focus
	}
}

func (m *Model) moveSelection(delta int) {
	if m.state.Focus == state.FocusWorkspaces {
		workspaceIDs := make([]string, 0, len(m.state.Workspaces))
		for _, workspace := range m.state.Workspaces {
			workspaceIDs = append(workspaceIDs, workspace.ID)
		}
		current := navigation.IndexByID(workspaceIDs, m.state.SelectedWorkspaceID)
		next := navigation.MoveIndex(current, len(workspaceIDs), delta)
		if len(workspaceIDs) > 0 && workspaceIDs[next] != m.state.SelectedWorkspaceID {
			m.state.SelectedWorkspaceID = workspaceIDs[next]
			if groups := state.GroupsForWorkspace(m.state, m.state.SelectedWorkspaceID); len(groups) > 0 {
				m.state.SelectedGroupID = groups[0].ID
			}
			m.state.SelectedAgentID = ""
			m.groupCursor = 0
			m.groupCursorPinned = false
			m.save()
		}
		return
	}
	rows := m.groupRows()
	if len(rows) == 0 {
		return
	}
	m.groupCursor = navigation.MoveIndex(m.groupCursor, len(rows), delta)
	m.applyGroupCursor(rows[m.groupCursor])
}

func (m *Model) applyGroupCursor(row groupRow) {
	switch row.kind {
	case groupRowGroup:
		m.state.SelectedAgentID = ""
		m.state.SelectedGroupID = row.groupID
	case groupRowAgent:
		if agent := state.AgentByID(m.state, row.agentID); agent != nil {
			m.state.SelectedAgentID = agent.ID
			m.state.SelectedGroupID = agent.GroupID
			m.state.ActiveAgentID = agent.ID
		}
	}
	m.groupCursorPinned = true
	m.save()
}

func (m *Model) reorderSelectedAgent(delta int) {
	if m.state.Focus != state.FocusAgents {
		return
	}
	agent := m.selectedAgent()
	if agent == nil {
		return
	}
	agentID := agent.ID
	next, moved, err := state.ReorderAgent(m.state, agent.ID, delta)
	if err != nil {
		m.message = err.Error()
		return
	}
	if !moved {
		return
	}
	m.state = next
	m.syncGroupCursorToAgent(agentID)
	m.save()
}

func (m *Model) startPrompt(prompt promptKind, value string) {
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

func (m *Model) startEditPrompt() {
	prompt, id, value, silent, ok := editPromptTargetForState(m.state, m.groupCursor)
	if ok {
		m.pendingID = id
		m.editGroupSilent = silent
		m.startPrompt(prompt, value)
	}
}

func (m *Model) startDeleteConfirm() {
	confirm, id, ok := deleteConfirmTargetForState(m.state, m.groupCursor)
	if ok {
		m.confirm = confirm
		m.pendingID = id
		m.mode = modeConfirm
	}
}

func (m *Model) startNewTaskMenu() {
	if state.ActiveWorkspace(m.state) == nil {
		m.message = "add a workspace first"
		return
	}
	m.focusNavPane(state.FocusAgents)
	m.newTaskIndex = defaultTaskTypeIndex(m.cfg)
	m.mode = modeNewTask
}

func (m *Model) applyPrompt(value string) tea.Cmd {
	switch m.prompt {
	case promptWorkspace:
		next, workspace, err := state.AddWorkspace(m.state, shortID(), value, state.NowISO())
		if err != nil {
			m.message = err.Error()
			return nil
		}
		message := workspaceAddMessage(m.state, workspace)
		m.state = next
		m.message = message
		m.rememberCurrentNavFocus()
		m.syncGroupCursor()
		m.save()
	case promptGroup:
		workspace := state.ActiveWorkspace(m.state)
		if workspace == nil {
			m.message = "select a workspace first"
			return nil
		}
		next, group, err := state.AddGroupWithSilent(m.state, shortID(), workspace.ID, value, state.NowISO(), m.editGroupSilent)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "created group " + group.Path
		m.syncGroupCursor()
		m.save()
	case promptEditGroup:
		next, err := state.EditGroup(m.state, m.pendingID, value, m.editGroupSilent)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "updated group"
		m.syncGroupCursorToSelectedGroup()
		m.save()
	case promptWorkspaceTitle:
		next, err := state.SetWorkspaceTitle(m.state, m.pendingID, value)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		if value == "" {
			m.message = "cleared workspace title"
		} else {
			m.message = "renamed workspace"
		}
		m.save()
	case promptEditAgent:
		next, err := state.RenameAgent(m.state, m.pendingID, value)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "renamed task"
		if agent := state.AgentByID(m.state, m.pendingID); agent != nil && m.agentUsesAutoTitle(*agent) {
			m.message = m.autoTitleRenameMessage(*agent)
			if strings.TrimSpace(m.cfg.TitleHookCommand) == "" && strings.TrimSpace(agent.AutoTitle) == "" {
				m.recordAutoTitleError(agent.ID, "title_hook_command is not configured", false)
			}
		}
		m.save()
	case promptMoveAgent:
		agent := m.selectedAgent()
		if agent == nil {
			m.message = "select a task first"
			return nil
		}
		groupID := ""
		if value != "" {
			group := m.findGroupByPath(agent.WorkspaceID, value)
			if group == nil {
				m.message = "group not found"
				return nil
			}
			groupID = group.ID
		}
		next, err := state.MoveAgent(m.state, agent.ID, groupID)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "moved task"
		m.syncGroupCursorToAgent(agent.ID)
		m.save()
	}
	return nil
}

func (m *Model) applyConfirm() tea.Cmd {
	switch m.confirm {
	case confirmAddLaunchWorkspace:
		next, workspace, err := state.AddWorkspace(m.state, shortID(), m.pendingID, state.NowISO())
		if err != nil {
			m.message = err.Error()
			return nil
		}
		message := workspaceAddMessage(m.state, workspace)
		m.state = next
		m.message = message
		m.rememberCurrentNavFocus()
		m.syncGroupCursor()
		m.save()
	case confirmDeleteWorkspace:
		next, agents, err := state.RemoveWorkspace(m.state, m.pendingID)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		for _, agent := range agents {
			m.killAgentPTY(agent.ID)
		}
		m.state = state.Repair(next, m.runtime.Workspace)
		m.message = "removed workspace"
		m.syncGroupCursor()
		m.save()
		return m.startNavAnimation()
	case confirmDeleteGroup:
		next, err := state.DeleteGroup(m.state, m.pendingID)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "deleted group"
		m.syncGroupCursor()
		m.save()
	case confirmDeleteAgent:
		return m.closeAgent(m.pendingID)
	}
	return nil
}

func (m *Model) newAgent(title string, typeIDs ...string) tea.Cmd {
	workspace := state.ActiveWorkspace(m.state)
	if workspace == nil {
		m.message = "add a workspace first"
		return nil
	}
	typeID := m.cfg.DefaultTaskType
	if len(typeIDs) > 0 && strings.TrimSpace(typeIDs[0]) != "" {
		typeID = strings.TrimSpace(typeIDs[0])
	}
	taskType, ok := m.cfg.TaskType(typeID)
	if !ok {
		m.message = "task type not found"
		return nil
	}
	if strings.TrimSpace(title) == "" {
		title = taskType.TitleTemplate
	}
	next, agent, err := state.AddAgentWithType(m.state, shortID(), workspace.ID, "", taskType.ID, title, state.NowISO())
	if err != nil {
		m.message = err.Error()
		return nil
	}
	m.state = next
	m.syncGroupCursorToAgent(agent.ID)
	m.snapNavWidthToTarget()
	m.save()
	return tea.Batch(m.startPTYCmd(agent.ID), m.startNavAnimation(), tickLoading())
}

func (m *Model) closeAgent(agentID string) tea.Cmd {
	if agentID == "" {
		return nil
	}
	m.killAgentPTY(agentID)
	m.state = state.CloseAgent(m.state, agentID)
	m.syncGroupCursor()
	m.save()
	return m.startNavAnimation()
}

func (m *Model) killAgentPTY(agentID string) {
	if pty := m.ptys[agentID]; pty != nil {
		pty.Kill()
		delete(m.ptys, agentID)
	}
	delete(m.screens, agentID)
	delete(m.visible, agentID)
	delete(m.agentInterrupts, agentID)
	delete(m.sessionCaptures, agentID)
}

func (m *Model) selectedAgent() *state.Agent {
	return selectedAgentForState(m.state, m.groupCursor)
}

func (m Model) currentGroupRow() groupRow {
	return currentGroupRowForState(m.state, m.groupCursor)
}

func (m Model) groupRows() []groupRow {
	return groupRowsForState(m.state)
}

func (m *Model) toggleSelectedGroup(groupID string) {
	m.state = state.ToggleGroupCollapsed(m.state, groupID)
	m.state.SelectedAgentID = ""
	m.state.SelectedGroupID = groupID
	for index, row := range m.groupRows() {
		if row.kind == groupRowGroup && row.groupID == groupID {
			m.groupCursor = index
			m.groupCursorPinned = true
			break
		}
	}
	m.save()
}

func (m *Model) syncGroupCursor() {
	rows := m.groupRows()
	if len(rows) == 0 {
		m.groupCursor = 0
		m.groupCursorPinned = false
		return
	}
	if m.groupCursorPinned && m.groupCursorMatchesState(rows) {
		return
	}
	if m.state.SelectedAgentID != "" {
		for index, row := range rows {
			if row.kind == groupRowAgent && row.agentID == m.state.SelectedAgentID {
				m.groupCursor = index
				m.groupCursorPinned = true
				return
			}
		}
	}
	for index, row := range rows {
		if row.kind == groupRowGroup && row.groupID == m.state.SelectedGroupID {
			m.groupCursor = index
			m.groupCursorPinned = true
			return
		}
	}
	if m.state.ActiveAgentID != "" {
		for index, row := range rows {
			if row.kind == groupRowAgent && row.agentID == m.state.ActiveAgentID {
				m.groupCursor = index
				m.groupCursorPinned = true
				return
			}
		}
	}
	m.groupCursor = 0
	m.groupCursorPinned = true
}

func (m *Model) syncGroupCursorToAgent(agentID string) {
	rows := m.groupRows()
	if len(rows) == 0 {
		m.groupCursor = 0
		m.groupCursorPinned = false
		return
	}
	if agentID != "" {
		for index, row := range rows {
			if row.kind == groupRowAgent && row.agentID == agentID {
				m.groupCursor = index
				m.state.SelectedAgentID = agentID
				if agent := state.AgentByID(m.state, agentID); agent != nil {
					m.state.SelectedWorkspaceID = agent.WorkspaceID
					m.state.SelectedGroupID = agent.GroupID
				}
				m.groupCursorPinned = true
				return
			}
		}
	}
	m.syncGroupCursor()
}

func (m *Model) syncGroupCursorToSelectedGroup() {
	rows := m.groupRows()
	if len(rows) == 0 {
		m.groupCursor = 0
		m.groupCursorPinned = false
		return
	}
	if m.state.SelectedGroupID != "" {
		for index, row := range rows {
			if row.kind == groupRowGroup && row.groupID == m.state.SelectedGroupID {
				m.groupCursor = index
				m.state.SelectedAgentID = ""
				m.groupCursorPinned = true
				return
			}
		}
	}
	m.syncGroupCursor()
}

func (m Model) groupCursorMatchesState(rows []groupRow) bool {
	if m.groupCursor < 0 || m.groupCursor >= len(rows) {
		return false
	}
	row := rows[m.groupCursor]
	switch row.kind {
	case groupRowGroup:
		return row.groupID != "" && row.groupID == m.state.SelectedGroupID
	case groupRowAgent:
		if m.state.SelectedAgentID != "" {
			return row.agentID == m.state.SelectedAgentID
		}
		return row.agentID != "" && row.agentID == m.state.ActiveAgentID
	default:
		return false
	}
}

func (m Model) findGroupByPath(workspaceID string, path string) *state.Group {
	path = strings.TrimSpace(path)
	for _, group := range state.GroupsForWorkspace(m.state, workspaceID) {
		if group.Path == path {
			f := group
			return &f
		}
	}
	return nil
}

func (m *Model) toggleDrawer() tea.Cmd {
	if m.state.Focus == state.FocusCodex {
		return m.openNav()
	}
	if state.ActiveAgent(m.state) == nil {
		m.state.NavOpen = true
		m.state.Focus = m.lastNavFocus
		m.save()
		return m.startNavAnimation()
	}
	return m.setCodexFocus()
}

func (m *Model) openNav() tea.Cmd {
	m.state.NavOpen = true
	if m.lastNavFocus != state.FocusWorkspaces && m.lastNavFocus != state.FocusAgents {
		m.lastNavFocus = state.FocusAgents
	}
	m.state.Focus = m.lastNavFocus
	m.save()
	return m.startNavAnimation()
}

func (m *Model) setCodexFocus() tea.Cmd {
	if state.ActiveAgent(m.state) == nil {
		m.message = "select a task first"
		return nil
	}
	if m.state.Focus == state.FocusWorkspaces || m.state.Focus == state.FocusAgents {
		m.lastNavFocus = m.state.Focus
	}
	m.state.Focus = state.FocusCodex
	m.state.NavOpen = false
	m.save()
	return m.startNavAnimation()
}

func (m *Model) closeWeft() {
	m.message = "closed Weft clients"
}

func (m *Model) captureCodexInput(agent state.Agent, msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyRunes:
		m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], msg.Runes...)
	case tea.KeySpace:
		m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], ' ')
	case tea.KeyBackspace:
		if msg.Alt {
			m.codexInputBuffers[agent.ID] = trimPreviousInputToken(m.codexInputBuffers[agent.ID])
		} else {
			m.codexInputBuffers[agent.ID] = trimLastRune(m.codexInputBuffers[agent.ID])
		}
	case tea.KeyCtrlH:
		if msg.Alt {
			m.codexInputBuffers[agent.ID] = trimPreviousInputToken(m.codexInputBuffers[agent.ID])
		}
	case tea.KeyEnter:
		firstMessage := strings.TrimSpace(string(m.codexInputBuffers[agent.ID]))
		delete(m.codexInputBuffers, agent.ID)
		if firstMessage == "" {
			return nil
		}
		if agent.AutoTitleAttempted {
			return nil
		}
		if strings.TrimSpace(m.cfg.TitleHookCommand) == "" {
			if m.agentUsesAutoTitle(agent) {
				m.recordAutoTitleError(agent.ID, "title_hook_command is not configured", false)
				m.message = "auto title unavailable: set title_hook_command"
			}
			return nil
		}
		m.state = state.WithUpdatedAgent(m.state, agent.ID, func(agent state.Agent) state.Agent {
			agent.AutoTitleAttempted = true
			agent.AutoTitleError = ""
			return agent
		})
		m.save()
		if updated := state.AgentByID(m.state, agent.ID); updated != nil {
			agent = *updated
		}
		if m.agentUsesAutoTitle(agent) {
			m.message = "generating auto title"
		}
		return m.titleHookCmd(agent, firstMessage)
	default:
		switch strings.ToLower(msg.String()) {
		case "ctrl+u":
			delete(m.codexInputBuffers, agent.ID)
		case "ctrl+w":
			m.codexInputBuffers[agent.ID] = trimLastWord(m.codexInputBuffers[agent.ID])
		}
	}
	return nil
}

func (m Model) agentUsesAutoTitle(agent state.Agent) bool {
	return strings.Contains(agent.Title, titles.AutoTemplate)
}

func (m Model) titleHookCmd(agent state.Agent, firstMessage string) tea.Cmd {
	workspace := state.Workspace{}
	if found := state.WorkspaceForAgent(m.state, agent); found != nil {
		workspace = *found
	}
	group := state.Group{}
	if found := state.GroupForAgent(m.state, agent); found != nil {
		group = *found
	}
	payload := titlehook.BuildPayload(agent, workspace, group, agent.Title, firstMessage)
	command := m.cfg.TitleHookCommand
	timeout := time.Duration(m.cfg.TitleHookTimeoutSeconds) * time.Second
	ctx := m.ctx
	return func() tea.Msg {
		title, err := titlehook.Run(ctx, command, workspace.Path, timeout, payload)
		return titleHookMsg{agentID: agent.ID, title: title, err: err}
	}
}

func (m *Model) applyTitleHook(msg titleHookMsg) {
	agent := state.AgentByID(m.state, msg.agentID)
	if agent == nil {
		return
	}
	if msg.err != nil {
		m.recordAutoTitleError(msg.agentID, hookErrorText(msg.err), true)
		m.message = "auto title hook failed: " + hookErrorText(msg.err)
		return
	}
	if strings.TrimSpace(msg.title) == "" {
		m.recordAutoTitleError(msg.agentID, "hook produced no title", true)
		m.message = "auto title hook failed: hook produced no title"
		return
	}
	m.state = state.WithUpdatedAgent(m.state, msg.agentID, func(agent state.Agent) state.Agent {
		agent.AutoTitle = msg.title
		agent.AutoTitleAttempted = true
		agent.AutoTitleError = ""
		return agent
	})
	if m.agentUsesAutoTitle(*agent) {
		m.message = "auto title generated"
	}
	m.save()
}

func (m *Model) recordAutoTitleError(agentID string, message string, attempted bool) {
	m.state = state.WithUpdatedAgent(m.state, agentID, func(agent state.Agent) state.Agent {
		agent.AutoTitle = ""
		agent.AutoTitleAttempted = attempted
		agent.AutoTitleError = message
		return agent
	})
	m.save()
}

func hookErrorText(err error) string {
	text := strings.Join(strings.Fields(err.Error()), " ")
	if len(text) > 140 {
		return text[:137] + "..."
	}
	return text
}

func (m Model) autoTitleRenameMessage(agent state.Agent) string {
	if strings.TrimSpace(agent.AutoTitle) != "" {
		return "renamed task; auto title ready"
	}
	if strings.TrimSpace(agent.AutoTitleError) != "" {
		return "renamed task; auto title failed"
	}
	if agent.AutoTitleAttempted {
		return "renamed task; auto title is generating"
	}
	if strings.TrimSpace(m.cfg.TitleHookCommand) == "" {
		return "renamed task; auto title unavailable: set title_hook_command"
	}
	return "renamed task; auto title will generate from the first message"
}

func trimLastRune(value []rune) []rune {
	if len(value) == 0 {
		return value
	}
	return value[:len(value)-1]
}

func trimLastWord(value []rune) []rune {
	for len(value) > 0 && unicode.IsSpace(value[len(value)-1]) {
		value = value[:len(value)-1]
	}
	for len(value) > 0 && !unicode.IsSpace(value[len(value)-1]) {
		value = value[:len(value)-1]
	}
	return value
}

func (m *Model) startPTY(agentID string) {
	if m.ptys[agentID] != nil {
		return
	}
	if m.screens[agentID] == nil {
		m.screens[agentID] = NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
	}
	workspace := m.agentWorkspace(agentID)
	ptySession, err := ptyx.Start(m.ctx, agentID, m.taskCommandForAgent(agentID), workspace, m.ptyWidth(), m.ptyHeight(), func(data ptyx.Data) {
		m.dataCh <- data
	})
	if err != nil {
		m.state = state.WithUpdatedAgent(m.state, agentID, func(agent state.Agent) state.Agent {
			agent.Status = state.StatusError
			agent.CodexTitle = err.Error()
			return agent
		})
		m.save()
		return
	}
	m.ptys[agentID] = ptySession
	m.state = state.WithUpdatedAgent(m.state, agentID, func(agent state.Agent) state.Agent {
		if agentUsesCodexIntegration(m.cfg, agent) {
			agent.Status = state.StatusRunning
		} else {
			agent.Status = state.StatusReady
			m.visible[agent.ID] = true
		}
		return agent
	})
	m.save()
}

func (m *Model) startPTYCmd(agentID string) tea.Cmd {
	if m.ptys[agentID] != nil {
		return nil
	}
	ctx := m.ctx
	command := m.taskCommandForAgent(agentID)
	workspace := m.agentWorkspace(agentID)
	cols := m.ptyWidth()
	rows := m.ptyHeight()
	dataCh := m.dataCh
	return func() tea.Msg {
		ptySession, err := ptyx.Start(ctx, agentID, command, workspace, cols, rows, func(data ptyx.Data) {
			dataCh <- data
		})
		return ptyStartedMsg{agentID: agentID, session: ptySession, err: err}
	}
}

func (m *Model) applyPTYStarted(msg ptyStartedMsg) {
	if msg.err != nil {
		m.state = state.WithUpdatedAgent(m.state, msg.agentID, func(agent state.Agent) state.Agent {
			agent.Status = state.StatusError
			agent.CodexTitle = msg.err.Error()
			return agent
		})
		m.save()
		return
	}
	if state.AgentByID(m.state, msg.agentID) == nil {
		msg.session.Kill()
		return
	}
	if m.ptys[msg.agentID] != nil {
		msg.session.Kill()
		return
	}
	if m.screens[msg.agentID] == nil {
		m.screens[msg.agentID] = NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
	}
	m.ptys[msg.agentID] = msg.session
	m.state = state.WithUpdatedAgent(m.state, msg.agentID, func(agent state.Agent) state.Agent {
		if agentUsesCodexIntegration(m.cfg, agent) {
			agent.Status = state.StatusRunning
		} else {
			agent.Status = state.StatusReady
			m.visible[agent.ID] = true
		}
		return agent
	})
	m.save()
}

func (m *Model) applyPTYData(data ptyx.Data) {
	if state.AgentByID(m.state, data.AgentID) == nil {
		return
	}
	agent := state.AgentByID(m.state, data.AgentID)
	if agent == nil {
		return
	}
	usesCodex := agentUsesCodexIntegration(m.cfg, *agent)
	if usesCodex && (data.Text != "" || data.Title != "") {
		m.captureCodexSession(data.AgentID)
	}
	if data.Err != nil {
		delete(m.ptys, data.AgentID)
		activeExited := m.state.ActiveAgentID == data.AgentID
		status := state.StatusStopped
		title := taskTypeForAgent(m.cfg, *agent).Label + " exited"
		if m.recentAgentInterrupt(data.AgentID) {
			status = state.StatusKilled
			title = taskTypeForAgent(m.cfg, *agent).Label + " killed"
		}
		delete(m.agentInterrupts, data.AgentID)
		m.state = state.WithUpdatedAgent(m.state, data.AgentID, func(agent state.Agent) state.Agent {
			if agent.Status != state.StatusError {
				agent.Status = status
				agent.CodexTitle = title
			}
			return agent
		})
		if activeExited && m.state.Focus == state.FocusCodex {
			m.state.NavOpen = true
			m.state.Focus = state.FocusAgents
			m.lastNavFocus = state.FocusAgents
			m.syncGroupCursor()
			m.snapNavWidthToTarget()
		}
		m.save()
		return
	}
	screenStatus := ""
	if data.Text != "" {
		screen := m.screens[data.AgentID]
		if screen == nil {
			screen = NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
			m.screens[data.AgentID] = screen
		}
		screen.Write(data.Text)
		if screen.HasVisibleContent() {
			m.visible[data.AgentID] = true
		}
		if usesCodex {
			screenStatus = codexScreenStatus(screen)
		}
	}
	if usesCodex && (data.Title != "" || data.Text != "") {
		title := titles.NormalizeCodexTitle(data.Title)
		if data.Title != "" {
			delete(m.agentInterrupts, data.AgentID)
		}
		m.state = state.WithUpdatedAgent(m.state, data.AgentID, func(agent state.Agent) state.Agent {
			if data.Title != "" {
				agent.CodexTitle = title
				agent.CodexStatus = ""
				agent.Status = state.StatusRunning
			}
			switch {
			case screenStatus != "":
				agent.CodexStatus = screenStatus
				if !titles.CodexTitleIndicatesActivity(agent.CodexTitle) {
					agent.Status = state.StatusReady
				}
			case data.Text != "" && agent.CodexStatus != "":
				agent.CodexStatus = ""
				if agent.Status == state.StatusReady && !titles.CodexTitleIndicatesActivity(agent.CodexTitle) {
					agent.Status = state.StatusRunning
				}
			}
			return agent
		})
		m.save()
	}
}

func (m *Model) captureCodexSession(agentID string) {
	agent := state.AgentByID(m.state, agentID)
	if agent == nil || strings.TrimSpace(agent.CodexSessionID) != "" {
		return
	}
	if !agentUsesCodexIntegration(m.cfg, *agent) {
		return
	}
	now := time.Now()
	if last, ok := m.sessionCaptures[agentID]; ok && now.Sub(last) < time.Second {
		return
	}
	m.sessionCaptures[agentID] = now
	next, assigned := codexsession.AssignMissingSessionIDs(m.state, m.runtime.Workspace)
	if assigned == 0 {
		return
	}
	m.state = next
	delete(m.sessionCaptures, agentID)
	m.save()
}

func (m Model) taskCommandForAgent(agentID string) string {
	agent := state.AgentByID(m.state, agentID)
	if agent == nil {
		return ""
	}
	taskType := taskTypeForAgent(m.cfg, *agent)
	command := taskType.Command
	if agentUsesCodexIntegration(m.cfg, *agent) && strings.TrimSpace(agent.CodexSessionID) != "" {
		command = codexsession.ResumeCommand(command, agent.CodexSessionID)
	}
	return command
}

func (m *Model) activeOutput() string {
	active := state.ActiveAgent(m.state)
	if active == nil {
		return ""
	}
	if screen := m.screens[active.ID]; screen != nil {
		if !screen.HasVisibleContent() && !m.visible[active.ID] {
			return ""
		}
		return screen.ANSIStringWithCursor(m.state.Focus == state.FocusCodex)
	}
	return ""
}

func (m Model) activePlainLines() []string {
	active := state.ActiveAgent(m.state)
	if active == nil {
		return nil
	}
	if screen := m.screens[active.ID]; screen != nil {
		if !screen.HasVisibleContent() && !m.visible[active.ID] {
			return nil
		}
		return screen.PlainLines()
	}
	return nil
}

func (m *Model) activeScrollbackOutput() string {
	active := state.ActiveAgent(m.state)
	if active == nil {
		return ""
	}
	if screen := m.screens[active.ID]; screen != nil {
		if !screen.HasVisibleContent() && !m.visible[active.ID] {
			return ""
		}
		return screen.ScrollbackANSIStringWithCursor(m.state.Focus == state.FocusCodex)
	}
	return ""
}

func (m Model) activeScrollbackPlainLines() []string {
	active := state.ActiveAgent(m.state)
	if active == nil {
		return nil
	}
	if screen := m.screens[active.ID]; screen != nil {
		if !screen.HasVisibleContent() && !m.visible[active.ID] {
			return nil
		}
		return screen.ScrollbackPlainLines()
	}
	return nil
}

func (m Model) codexLoading() bool {
	active := state.ActiveAgent(m.state)
	if active == nil {
		return false
	}
	return m.agentLoading(active.ID)
}

func (m Model) anyAgentLoading() bool {
	for _, agent := range m.state.Agents {
		if m.agentLoading(agent.ID) {
			return true
		}
	}
	return false
}

func (m Model) loadingAgentIDs() []string {
	ids := make([]string, 0)
	for _, agent := range m.state.Agents {
		if m.agentLoading(agent.ID) {
			ids = append(ids, agent.ID)
		}
	}
	return ids
}

func (m Model) loadingAgentSet() map[string]bool {
	ids := m.loadingAgentIDs()
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func (m Model) agentLoading(agentID string) bool {
	agent := state.AgentByID(m.state, agentID)
	if agent == nil {
		return false
	}
	if !agentUsesCodexIntegration(m.cfg, *agent) {
		return agent.Status == state.StatusStarting && m.ptys[agentID] == nil ||
			agent.Status == state.StatusRunning
	}
	canonical := titles.CanonicalStatus(*agent)
	switch canonical {
	case string(state.StatusError), string(state.StatusStopped), string(state.StatusKilled), string(state.StatusSitting):
		return false
	}
	// Many non-active agents don't have a captured screen buffer; rely on the Codex title-derived
	// status first so we don't incorrectly show them as still "running"/loading.
	// For the active agent, keep the stricter behavior that waits for visible content.
	if agentID != m.state.ActiveAgentID {
		switch canonical {
		case "ready":
			return false
		case "working":
			return true
		}
	}
	if agentStatusIndicatesActivity(*agent) {
		return true
	}
	screen := m.screens[agentID]
	return screen == nil || (!screen.HasVisibleContent() && !m.visible[agentID])
}

func (m *Model) markTerminalCommandStarted(agentID string) {
	agent := state.AgentByID(m.state, agentID)
	if agent == nil || agentUsesCodexIntegration(m.cfg, *agent) {
		return
	}
	if m.terminalCommands == nil {
		m.terminalCommands = map[string]time.Time{}
	}
	m.terminalCommands[agentID] = time.Now()
	m.state = state.WithUpdatedAgent(m.state, agentID, func(agent state.Agent) state.Agent {
		switch agent.Status {
		case state.StatusError, state.StatusStopped, state.StatusKilled:
			return agent
		default:
			agent.Status = state.StatusRunning
			return agent
		}
	})
	m.save()
}

func (m *Model) refreshTerminalTaskActivity() {
	if len(m.terminalCommands) == 0 {
		return
	}
	changed := false
	for agentID, started := range m.terminalCommands {
		agent := state.AgentByID(m.state, agentID)
		if agent == nil || agentUsesCodexIntegration(m.cfg, *agent) {
			delete(m.terminalCommands, agentID)
			continue
		}
		if agent.Status != state.StatusRunning {
			delete(m.terminalCommands, agentID)
			continue
		}
		if time.Since(started) < terminalCommandLoadingFloor {
			continue
		}
		pty := m.ptys[agentID]
		if pty != nil && pty.ForegroundProcessActive() {
			continue
		}
		delete(m.terminalCommands, agentID)
		m.state = state.WithUpdatedAgent(m.state, agentID, func(agent state.Agent) state.Agent {
			if agent.Status == state.StatusRunning {
				agent.Status = state.StatusReady
			}
			return agent
		})
		changed = true
	}
	if changed {
		m.save()
	}
}

func (m Model) recentAgentInterrupt(agentID string) bool {
	if m.agentInterrupts == nil {
		return false
	}
	sentAt, ok := m.agentInterrupts[agentID]
	return ok && time.Since(sentAt) <= 5*time.Second
}

func (m *Model) recordAgentInterrupt(agentID string) {
	if m.agentInterrupts == nil {
		m.agentInterrupts = map[string]time.Time{}
	}
	m.agentInterrupts[agentID] = time.Now()
}

func (m Model) loadingFrame() string {
	return loadingFrames[m.loading%len(loadingFrames)]
}

func (m Model) loadingLabel() string {
	label := "task"
	if active := state.ActiveAgent(m.state); active != nil {
		label = taskTypeForAgent(m.cfg, *active).Label
	}
	return m.loadingFrame() + " Starting " + label
}

func (m *Model) save() {
	_ = m.store.Write(m.state)
}

func (m *Model) resizePTYs() {
	for _, pty := range m.ptys {
		pty.Resize(m.ptyWidth(), m.ptyHeight())
	}
}

func (m *Model) resizeScreens() {
	for agentID, screen := range m.screens {
		agent := state.AgentByID(m.state, agentID)
		if agent != nil && !agentUsesCodexIntegration(m.cfg, *agent) {
			screen.ResizeTopAligned(m.ptyWidth(), m.ptyHeight())
			continue
		}
		screen.Resize(m.ptyWidth(), m.ptyHeight())
	}
}

func (m *Model) snapNavWidthToTarget() {
	target := m.targetNavWidth()
	if m.navWidth == target {
		return
	}
	m.navWidth = target
	m.resizePTYs()
	m.resizeScreens()
}

func (m Model) ptyWidth() int {
	return max(20, codexLineContentWidth(max(0, m.codexPaneWidth()-2), m.effectiveNavWidth() > 0))
}

func (m Model) ptyHeight() int {
	return max(5, m.height-2)
}

func (m Model) effectiveNavWidth() int {
	return min(max(0, m.navWidth), max(0, m.width-22))
}

func (m Model) codexPaneWidth() int {
	if m.width <= 0 {
		return 0
	}
	navWidth := min(max(0, m.effectiveNavWidth()), m.width)
	codexWidth := m.width - navWidth
	navOnly := navWidth >= m.width
	if !navOnly && codexWidth < minCodexPaneWidth && navWidth > 0 {
		codexWidth = min(m.width, minCodexPaneWidth)
	}
	if navWidth <= 0 {
		return m.width
	}
	if codexWidth <= 0 {
		return 0
	}
	return codexWidth
}

func (m Model) targetNavWidth() int {
	if m.state.Focus == state.FocusCodex && state.ActiveAgent(m.state) != nil {
		return 0
	}
	return workspaceNavFrameWidth(m.state, m.width)
}

func (m *Model) startNavAnimation() tea.Cmd {
	if m.navWidth == m.targetNavWidth() {
		return nil
	}
	return tickNavAnimation()
}

func (m *Model) stepNavAnimation() tea.Cmd {
	target := m.targetNavWidth()
	delta := target - m.navWidth
	if delta == 0 {
		return nil
	}
	if abs(delta) <= navAnimationStep {
		m.navWidth = target
	} else if delta > 0 {
		m.navWidth += navAnimationStep
	} else {
		m.navWidth -= navAnimationStep
	}
	m.resizePTYs()
	m.resizeScreens()
	if m.navWidth != target {
		return tickNavAnimation()
	}
	return nil
}

func (m Model) ipcResponse(message string) ipc.Response {
	st := m.state
	snapshot := m.Snapshot()
	if message != "" {
		snapshot.Message = message
	}
	return ipc.Response{OK: true, State: &st, Snapshot: &snapshot, Message: message}
}

func ipcError(code string, err error) ipc.Response {
	return ipc.ErrorResponse(code, err.Error())
}

func (m *Model) handleIPC(request ipc.Request) (ipc.Response, tea.Cmd) {
	m.applyLaunchWorkspace(launchWorkspaceArg(request.Args))
	if request.Command == "snapshot" || request.Command == "status" {
		m.refreshTerminalTaskActivity()
	}
	switch request.Command {
	case "snapshot":
		return m.ipcResponse(m.message), nil
	case "status":
		response := m.ipcResponse(m.statusText())
		response.Message = m.statusText()
		return response, nil
	case "refresh":
		m.message = "refreshed"
		return m.ipcResponse("refreshed Weft dashboard"), nil
	case "resize":
		width, _ := strconv.Atoi(request.Args["width"])
		height, _ := strconv.Atoi(request.Args["height"])
		if width > 0 {
			m.width = width
		}
		if height > 0 {
			m.height = height
		}
		m.navWidth = m.targetNavWidth()
		m.resizePTYs()
		m.resizeScreens()
		return m.ipcResponse(m.message), nil
	case "toggle_drawer":
		cmd := m.toggleDrawer()
		m.snapNavWidthToTarget()
		return m.ipcResponse("focus updated"), cmd
	case "nav_move":
		delta, err := strconv.Atoi(request.Args["delta"])
		if err != nil || delta == 0 {
			return ipc.ErrorResponse("invalid_delta", "delta must be a non-zero integer"), nil
		}
		m.moveSelection(delta)
		return m.ipcResponse("selection updated"), nil
	case "reorder_agent":
		delta, err := strconv.Atoi(request.Args["delta"])
		if err != nil || delta == 0 {
			return ipc.ErrorResponse("invalid_delta", "delta must be a non-zero integer"), nil
		}
		id := request.Args["id"]
		if id == "" {
			if agent := m.selectedAgent(); agent != nil {
				id = agent.ID
			}
		}
		if id == "" {
			return ipc.ErrorResponse("agent_not_found", "agent not found"), nil
		}
		next, moved, err := state.ReorderAgent(m.state, id, delta)
		if err != nil {
			return ipcError("reorder_agent_failed", err), nil
		}
		if moved {
			m.state = next
			m.syncGroupCursorToAgent(id)
			m.save()
		}
		return m.ipcResponse("reordered task"), nil
	case "open":
		cmd := m.openSelection()
		m.navWidth = m.targetNavWidth()
		return m.ipcResponse("selection opened"), cmd
	case "new":
		if state.ActiveWorkspace(m.state) == nil {
			return ipc.ErrorResponse("workspace_required", "add a workspace first"), nil
		}
		title := request.Args["title"]
		typeID := strings.TrimSpace(request.Args["type"])
		if typeID == "" {
			typeID = strings.TrimSpace(request.Args["type_id"])
		}
		cmd := m.newAgent(title, typeID)
		m.snapNavWidthToTarget()
		return m.ipcResponse("created task"), cmd
	case "rename":
		id := request.Args["id"]
		if id == "" {
			id = m.state.ActiveAgentID
		}
		title := strings.TrimSpace(request.Args["title"])
		if title == "" {
			return ipc.Response{OK: false, Message: "title is required"}, nil
		}
		next, err := state.RenameAgent(m.state, id, title)
		if err != nil {
			return ipcError("rename_agent_failed", err), nil
		}
		m.state = next
		m.save()
		return m.ipcResponse("renamed task"), nil
	case "rename_group":
		groupID := request.Args["id"]
		silent := false
		if group := state.GroupByID(m.state, groupID); group != nil {
			silent = group.Silent
		}
		if raw := strings.TrimSpace(request.Args["silent"]); raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				return ipc.ErrorResponse("invalid_silent", "silent must be a boolean"), nil
			}
			silent = parsed
		}
		next, err := state.EditGroup(m.state, groupID, request.Args["path"], silent)
		if err != nil {
			return ipcError("rename_group_failed", err), nil
		}
		m.state = next
		m.syncGroupCursorToSelectedGroup()
		m.save()
		return m.ipcResponse("updated group"), nil
	case "rename_workspace":
		next, err := state.SetWorkspaceTitle(m.state, request.Args["id"], request.Args["title"])
		if err != nil {
			return ipcError("rename_workspace_failed", err), nil
		}
		m.state = next
		m.save()
		if strings.TrimSpace(request.Args["title"]) == "" {
			return m.ipcResponse("cleared workspace title"), nil
		}
		return m.ipcResponse("renamed workspace"), nil
	case "close":
		id := request.Args["id"]
		if id == "" {
			id = m.state.ActiveAgentID
		}
		cmd := m.closeAgent(id)
		m.snapNavWidthToTarget()
		return m.ipcResponse("closed task"), cmd
	case "remove_workspace":
		next, agents, err := state.RemoveWorkspace(m.state, request.Args["id"])
		if err != nil {
			return ipcError("remove_workspace_failed", err), nil
		}
		for _, agent := range agents {
			m.killAgentPTY(agent.ID)
		}
		m.state = state.Repair(next, m.runtime.Workspace)
		m.syncGroupCursor()
		m.save()
		m.snapNavWidthToTarget()
		return m.ipcResponse("removed workspace"), m.startNavAnimation()
	case "remove_group":
		next, err := state.DeleteGroup(m.state, request.Args["id"])
		if err != nil {
			return ipcError("remove_group_failed", err), nil
		}
		m.state = next
		m.syncGroupCursor()
		m.save()
		return m.ipcResponse("deleted group"), nil
	case "select":
		id := request.Args["id"]
		if agent := state.AgentByID(m.state, id); agent != nil {
			m.state.ActiveAgentID = id
			m.state.SelectedWorkspaceID = agent.WorkspaceID
			m.state.SelectedGroupID = agent.GroupID
			m.syncGroupCursorToAgent(id)
			m.save()
			return m.ipcResponse("selected task"), nil
		}
		return ipc.Response{OK: false, Message: "task not found"}, nil
	case "move":
		id := request.Args["id"]
		if id == "" {
			id = m.state.ActiveAgentID
		}
		agent := state.AgentByID(m.state, id)
		if agent == nil {
			return ipc.ErrorResponse("agent_not_found", "task not found"), nil
		}
		groupID, ok := m.destinationGroupIDForMove(*agent, request.Args)
		if !ok {
			return ipc.ErrorResponse("group_not_found", "group not found"), nil
		}
		next, err := state.MoveAgent(m.state, agent.ID, groupID)
		if err != nil {
			return ipcError("move_agent_failed", err), nil
		}
		m.state = next
		m.syncGroupCursorToAgent(agent.ID)
		m.save()
		return m.ipcResponse("moved task"), nil
	case "add_workspace":
		path := request.Args["path"]
		next, workspace, err := state.AddWorkspace(m.state, shortID(), path, state.NowISO())
		if err != nil {
			return ipcError("add_workspace_failed", err), nil
		}
		message := workspaceAddMessage(m.state, workspace)
		m.state = next
		m.rememberCurrentNavFocus()
		m.syncGroupCursor()
		m.save()
		return m.ipcResponse(message), nil
	case "add_group":
		path := request.Args["path"]
		silent := false
		if raw := strings.TrimSpace(request.Args["silent"]); raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				return ipc.ErrorResponse("invalid_silent", "silent must be a boolean"), nil
			}
			silent = parsed
		}
		workspaceID := request.Args["workspace_id"]
		if workspaceID == "" {
			workspaceID = m.state.SelectedWorkspaceID
		}
		next, _, err := state.AddGroupWithSilent(m.state, shortID(), workspaceID, path, state.NowISO(), silent)
		if err != nil {
			return ipcError("add_group_failed", err), nil
		}
		m.state = next
		m.syncGroupCursor()
		m.save()
		return m.ipcResponse("created group"), nil
	case "focus":
		target := state.Focus(request.Args["target"])
		if target == "workspaces" {
			target = state.FocusWorkspaces
		}
		if target == "agents" || target == "tasks" {
			target = state.FocusAgents
		}
		switch target {
		case state.FocusWorkspaces, state.FocusAgents:
			m.focusNavPane(target)
			m.snapNavWidthToTarget()
			return m.ipcResponse("focus updated"), m.startNavAnimation()
		case state.FocusCodex:
			cmd := m.setCodexFocus()
			m.snapNavWidthToTarget()
			return m.ipcResponse("focus updated"), cmd
		default:
			return ipc.Response{OK: false, Message: "focus target must be workspaces, tasks, or codex"}, nil
		}
	case "codex_input":
		cmd := m.applyCodexInput(request.Args)
		return m.ipcResponse(m.message), cmd
	case "task_input":
		cmd := m.applyTaskInput(request.Args)
		return m.ipcResponse(m.message), cmd
	case "task_clear":
		m.clearActiveTerminal()
		return m.ipcResponse(m.message), nil
	case "close_client":
		m.closeWeft()
		return m.ipcResponse("closed Weft clients"), nil
	default:
		return ipc.ErrorResponse("unknown_command", "unknown command: "+request.Command), nil
	}
}

func launchWorkspaceArg(args map[string]string) string {
	if value := strings.TrimSpace(args["launch_workspace"]); value != "" {
		return value
	}
	return ""
}

func (m *Model) applyLaunchWorkspace(path string) {
	next, ok := state.SelectWorkspaceByPath(m.state, path)
	if !ok {
		return
	}
	if next.SelectedWorkspaceID == m.state.SelectedWorkspaceID && next.SelectedGroupID == m.state.SelectedGroupID {
		return
	}
	m.state = next
	m.syncGroupCursor()
	m.save()
}

func (m *Model) openSelection() tea.Cmd {
	if m.state.Focus == state.FocusWorkspaces {
		m.focusNavPane(state.FocusAgents)
		return nil
	}
	row := m.currentGroupRow()
	if row.kind == groupRowGroup {
		m.toggleSelectedGroup(row.groupID)
		return nil
	}
	if agent := m.selectedAgent(); agent != nil {
		m.state.SelectedAgentID = agent.ID
		m.state.ActiveAgentID = agent.ID
		m.state.SelectedWorkspaceID = agent.WorkspaceID
		m.state.SelectedGroupID = agent.GroupID
		m.save()
		return m.setCodexFocus()
	}
	return nil
}

func (m *Model) applyCodexInput(args map[string]string) tea.Cmd {
	if m.state.Focus != state.FocusCodex {
		return nil
	}
	active := state.ActiveAgent(m.state)
	if active == nil {
		return nil
	}
	if !agentUsesCodexIntegration(m.cfg, *active) {
		return m.applyTaskInput(args)
	}
	args = routeCodexInputArgs(*active, args)
	if pty := m.ptys[active.ID]; pty != nil {
		if args["input"] == "ctrl+c" {
			m.recordAgentInterrupt(active.ID)
		}
		_ = pty.Write([]byte(args["encoded"]))
	}
	return m.captureCodexInputArgs(*active, args)
}

func (m *Model) applyTaskInput(args map[string]string) tea.Cmd {
	if m.state.Focus != state.FocusCodex {
		return nil
	}
	active := state.ActiveAgent(m.state)
	if active == nil {
		return nil
	}
	if agentUsesCodexIntegration(m.cfg, *active) {
		return m.applyCodexInput(args)
	}
	encoded := []byte(args["encoded"])
	if pty := m.ptys[active.ID]; pty != nil {
		_ = pty.Write(encoded)
	}
	return m.captureRawTerminalInput(*active, encoded)
}

func (m *Model) clearActiveTerminal() {
	if m.state.Focus != state.FocusCodex {
		return
	}
	active := state.ActiveAgent(m.state)
	if active == nil || agentUsesCodexIntegration(m.cfg, *active) {
		return
	}
	if screen := m.screens[active.ID]; screen != nil {
		screen.Clear()
		m.visible[active.ID] = true
	}
	if pty := m.ptys[active.ID]; pty != nil {
		_ = pty.Write([]byte{0x0c})
	}
}

func (m *Model) captureCodexInputArgs(agent state.Agent, args map[string]string) tea.Cmd {
	switch args["input"] {
	case "text":
		m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], []rune(args["text"])...)
	case "space":
		m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], ' ')
	case "backspace":
		m.codexInputBuffers[agent.ID] = trimLastRune(m.codexInputBuffers[agent.ID])
	case codexInputShiftEnter:
		m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], '\n')
	case "alt+backspace":
		m.codexInputBuffers[agent.ID] = trimPreviousInputToken(m.codexInputBuffers[agent.ID])
	case "enter":
		return m.submitCodexInputBuffer(agent)
	case "ctrl+u":
		delete(m.codexInputBuffers, agent.ID)
	case "ctrl+w":
		m.codexInputBuffers[agent.ID] = trimLastWord(m.codexInputBuffers[agent.ID])
	case codexInputRaw:
		return m.captureRawCodexInput(agent, []byte(args["encoded"]))
	}
	return nil
}

func (m *Model) submitCodexInputBuffer(agent state.Agent) tea.Cmd {
	firstMessage := strings.TrimSpace(string(m.codexInputBuffers[agent.ID]))
	delete(m.codexInputBuffers, agent.ID)
	if firstMessage == "" || agent.AutoTitleAttempted {
		return nil
	}
	if strings.TrimSpace(m.cfg.TitleHookCommand) == "" {
		if m.agentUsesAutoTitle(agent) {
			m.recordAutoTitleError(agent.ID, "title_hook_command is not configured", false)
			m.message = "auto title unavailable: set title_hook_command"
		}
		return nil
	}
	m.state = state.WithUpdatedAgent(m.state, agent.ID, func(agent state.Agent) state.Agent {
		agent.AutoTitleAttempted = true
		agent.AutoTitleError = ""
		return agent
	})
	m.save()
	if updated := state.AgentByID(m.state, agent.ID); updated != nil {
		agent = *updated
	}
	if m.agentUsesAutoTitle(agent) {
		m.message = "generating auto title"
	}
	return m.titleHookCmd(agent, firstMessage)
}

func (m *Model) captureRawCodexInput(agent state.Agent, data []byte) tea.Cmd {
	var cmd tea.Cmd
	for index := 0; index < len(data); {
		switch data[index] {
		case '\r', '\n':
			if cmd == nil {
				cmd = m.submitCodexInputBuffer(agent)
				if cmd != nil {
					agent.AutoTitleAttempted = true
				}
			} else {
				delete(m.codexInputBuffers, agent.ID)
			}
			index++
		case 0x7f, '\b':
			m.codexInputBuffers[agent.ID] = trimLastRune(m.codexInputBuffers[agent.ID])
			index++
		case 0x15:
			delete(m.codexInputBuffers, agent.ID)
			index++
		case 0x17:
			m.codexInputBuffers[agent.ID] = trimLastWord(m.codexInputBuffers[agent.ID])
			index++
		case 0x1b:
			if sequence, width, ok := consumeCSISequence(data[index:]); ok {
				if event, ok := parseCSIKeyboardEvent(sequence); ok {
					if event.isShiftEnter() {
						m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], '\n')
					} else if key, ok := event.keyMsg(); ok {
						next := m.captureCodexInput(agent, key)
						if cmd == nil && next != nil {
							cmd = next
							agent.AutoTitleAttempted = true
						}
					}
				}
				index += width
				continue
			}
			if index+1 < len(data) && (data[index+1] == 0x7f || data[index+1] == '\b') {
				m.codexInputBuffers[agent.ID] = trimPreviousInputToken(m.codexInputBuffers[agent.ID])
				index += 2
				continue
			}
			if index+1 < len(data) && data[index+1] >= 0x20 && data[index+1] != 0x7f {
				index += 2
				continue
			}
			index++
		default:
			r, width := utf8.DecodeRune(data[index:])
			if r == utf8.RuneError && width == 1 {
				index++
				continue
			}
			if !unicode.IsControl(r) {
				m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], r)
			}
			index += width
		}
	}
	return cmd
}

func (m *Model) captureRawTerminalInput(agent state.Agent, data []byte) tea.Cmd {
	var cmd tea.Cmd
	started := false
	for index := 0; index < len(data); {
		switch data[index] {
		case '\r', '\n':
			if m.submitTerminalInputBuffer(agent) {
				started = true
				if m.agentUsesAutoTitle(agent) && cmd == nil {
					cmd = m.submitCodexInputBuffer(agent)
					if cmd != nil {
						agent.AutoTitleAttempted = true
					}
				} else {
					delete(m.codexInputBuffers, agent.ID)
				}
			} else {
				delete(m.codexInputBuffers, agent.ID)
			}
			index++
		case 0x7f, '\b':
			m.codexInputBuffers[agent.ID] = trimLastRune(m.codexInputBuffers[agent.ID])
			index++
		case 0x15:
			delete(m.codexInputBuffers, agent.ID)
			index++
		case 0x17:
			m.codexInputBuffers[agent.ID] = trimLastWord(m.codexInputBuffers[agent.ID])
			index++
		case 0x1b:
			if sequence, width, ok := consumeCSISequence(data[index:]); ok {
				if event, ok := parseCSIKeyboardEvent(sequence); ok {
					if event.isShiftEnter() {
						m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], '\n')
					} else if key, ok := event.keyMsg(); ok {
						m.captureTerminalKey(agent, key)
					}
				}
				index += width
				continue
			}
			if index+1 < len(data) && (data[index+1] == 0x7f || data[index+1] == '\b') {
				m.codexInputBuffers[agent.ID] = trimPreviousInputToken(m.codexInputBuffers[agent.ID])
				index += 2
				continue
			}
			if index+1 < len(data) && data[index+1] >= 0x20 && data[index+1] != 0x7f {
				index += 2
				continue
			}
			index++
		default:
			r, width := utf8.DecodeRune(data[index:])
			if r == utf8.RuneError && width == 1 {
				index++
				continue
			}
			if !unicode.IsControl(r) {
				m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], r)
			}
			index += width
		}
	}
	if cmd != nil {
		return tea.Batch(cmd, tickLoading())
	}
	if started {
		return tickLoading()
	}
	return nil
}

func (m *Model) submitTerminalInputBuffer(agent state.Agent) bool {
	command := strings.TrimSpace(string(m.codexInputBuffers[agent.ID]))
	if command == "" {
		return false
	}
	m.markTerminalCommandStarted(agent.ID)
	return true
}

func (m *Model) captureTerminalKey(agent state.Agent, msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyRunes:
		m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], msg.Runes...)
	case tea.KeySpace:
		m.codexInputBuffers[agent.ID] = append(m.codexInputBuffers[agent.ID], ' ')
	case tea.KeyBackspace:
		m.codexInputBuffers[agent.ID] = trimLastRune(m.codexInputBuffers[agent.ID])
	case tea.KeyCtrlU:
		delete(m.codexInputBuffers, agent.ID)
	case tea.KeyCtrlW:
		m.codexInputBuffers[agent.ID] = trimLastWord(m.codexInputBuffers[agent.ID])
	}
}

func consumeCSISequence(data []byte) ([]byte, int, bool) {
	if len(data) < 3 || data[0] != 0x1b || data[1] != '[' {
		return nil, 0, false
	}
	for index := 2; index < len(data); index++ {
		if data[index] >= 0x40 && data[index] <= 0x7e {
			return data[:index+1], index + 1, true
		}
	}
	return nil, 0, false
}

func trimPreviousInputToken(value []rune) []rune {
	start := previousPromptTokenBoundary(string(value), len(value))
	return append([]rune{}, value[:start]...)
}

func (m Model) destinationGroupIDForMove(agent state.Agent, args map[string]string) (string, bool) {
	if value, ok := args["ungrouped"]; ok && strings.EqualFold(value, "true") {
		return "", true
	}
	if groupID, ok := args["group_id"]; ok {
		if groupID == "" {
			return "", true
		}
		group := state.GroupByID(m.state, groupID)
		if group != nil && group.WorkspaceID == agent.WorkspaceID {
			return group.ID, true
		}
		return "", false
	}
	if groupPath, ok := args["group"]; ok {
		if strings.TrimSpace(groupPath) == "" {
			return "", true
		}
		group := m.findGroupByPath(agent.WorkspaceID, groupPath)
		if group == nil {
			return "", false
		}
		return group.ID, true
	}
	groups := state.GroupsForWorkspace(m.state, agent.WorkspaceID)
	current := 0
	groupIDs := []string{""}
	for index, group := range groups {
		groupIDs = append(groupIDs, group.ID)
		if group.ID == agent.GroupID {
			current = index + 1
			break
		}
	}
	switch args["direction"] {
	case "left":
		current = navigation.MoveIndex(current, len(groupIDs), -1)
	case "right":
		current = navigation.MoveIndex(current, len(groupIDs), 1)
	default:
		return "", false
	}
	return groupIDs[current], true
}

func (m Model) agentWorkspace(agentID string) string {
	agent := state.AgentByID(m.state, agentID)
	if agent == nil {
		return m.runtime.Workspace
	}
	if workspace := state.WorkspaceForAgent(m.state, *agent); workspace != nil {
		return workspace.Path
	}
	return m.runtime.Workspace
}

func (m Model) renderAgentTitle(agent state.Agent) string {
	return renderAgentTitleForState(m.cfg, m.state, agent)
}

func (m Model) statusText() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "supervisor: running\n")
	fmt.Fprintf(&builder, "runtime dir: %s\n", m.runtime.Dir)
	fmt.Fprintf(&builder, "socket: %s\n", m.runtime.SocketPath)
	fmt.Fprintf(&builder, "launch workspace: %s\n", m.runtime.Workspace)
	fmt.Fprintf(&builder, "focus: %s\n", displayFocus(m.state.Focus))
	fmt.Fprintf(&builder, "nav open: %t\n", m.state.NavOpen)
	fmt.Fprintf(&builder, "workspaces: %d\n", len(m.state.Workspaces))
	fmt.Fprintf(&builder, "groups: %d\n", len(m.state.Groups))
	fmt.Fprintf(&builder, "tasks: %d\n", len(m.state.Agents))
	for _, agent := range m.state.Agents {
		marker := " "
		if agent.ID == m.state.ActiveAgentID {
			marker = "*"
		}
		group := ""
		if f := state.GroupForAgent(m.state, agent); f != nil {
			group = f.Path
		}
		workspace := ""
		if w := state.WorkspaceForAgent(m.state, agent); w != nil {
			workspace = w.Path
		}
		fmt.Fprintf(&builder, "%s %s %s %s %s %s\n", marker, agent.ID, group, agent.Status, m.renderAgentTitle(agent), workspace)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func displayFocus(focus state.Focus) string {
	if focus == state.FocusWorkspaces {
		return "workspaces"
	}
	if focus == state.FocusAgents {
		return "tasks"
	}
	return string(focus)
}

func waitPTY(ch <-chan ptyx.Data) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func tickNavAnimation() tea.Cmd {
	return tea.Tick(navAnimationInterval, func(time.Time) tea.Msg { return navAnimationTick{} })
}

func tickLoading() tea.Cmd {
	return tea.Tick(loadingInterval, func(time.Time) tea.Msg { return loadingTick{} })
}

func renderHelp(cfg config.Config) string {
	lines := []string{
		"Weft shortcuts",
		"",
		fmt.Sprintf("%s dashboard", cfg.KeyBindings.Drawer),
		fmt.Sprintf("%s/%s panes", cfg.KeyBindings.FocusLeft, cfg.KeyBindings.FocusRight),
		fmt.Sprintf("%s/%s select", cfg.KeyBindings.SelectPrev, cfg.KeyBindings.SelectNext),
		fmt.Sprintf("%s open task", cfg.KeyBindings.Open),
		fmt.Sprintf("%s new workspace", cfg.KeyBindings.NewWorkspace),
		fmt.Sprintf("%s new group", cfg.KeyBindings.NewGroup),
		fmt.Sprintf("%s new task", cfg.KeyBindings.NewAgent),
		fmt.Sprintf("%s move task", cfg.KeyBindings.MoveAgent),
		"Shift+Up/Down reorder task",
		fmt.Sprintf("%s edit", cfg.KeyBindings.Edit),
		fmt.Sprintf("%s delete", cfg.KeyBindings.Delete),
		"U upgrade supervisor and resume idle Codex tasks",
		fmt.Sprintf("%s help", cfg.KeyBindings.Help),
		fmt.Sprintf("%s quit", cfg.KeyBindings.Quit),
		"",
		"Esc close",
	}
	return strings.Join(lines, "\n")
}

func shortID() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%08x", uint32(time.Now().UnixNano()))
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
