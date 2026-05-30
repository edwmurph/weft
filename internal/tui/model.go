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

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
)

type promptKind string

const (
	promptWorkdir      promptKind = "workdir"
	promptGroup        promptKind = "group"
	promptWorkdirTitle promptKind = "workdir-title"
	promptRenameGroup  promptKind = "rename-group"
	promptRenameAgent  promptKind = "rename-agent"
	promptMoveAgent    promptKind = "move-agent"
)

type confirmKind string

const (
	confirmAddLaunchWorkdir confirmKind = "add-launch-workdir"
	confirmDeleteWorkdir    confirmKind = "delete-workdir"
	confirmDeleteGroup      confirmKind = "delete-group"
	confirmDeleteAgent      confirmKind = "delete-agent"
)

const (
	navAnimationInterval = 12 * time.Millisecond
	navAnimationStep     = 4
	loadingInterval      = 90 * time.Millisecond
)

type navAnimationTick struct{}
type loadingTick struct{}

var loadingFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type folderRowKind string

const (
	folderRowFolder folderRowKind = "folder"
	folderRowAgent  folderRowKind = "agent"
)

type folderRow struct {
	kind     folderRowKind
	folderID string
	agentID  string
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
	dataCh            chan ptyx.Data
	ctx               context.Context
	cancel            context.CancelFunc

	input                textinput.Model
	prompt               promptKind
	confirm              confirmKind
	pendingID            string
	folderCursor         int
	lastNavFocus         state.Focus
	promptSuggestionOpen bool
}

func NewModel(rt config.Runtime, cfg config.Config, st state.State) Model {
	ctx, cancel := context.WithCancel(context.Background())
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 240
	input.Width = 60
	st = state.Repair(st, rt.Workdir)
	if state.ActiveAgent(st) == nil {
		st.ActiveAgentID = ""
		if len(st.Workdirs) == 0 {
			st.Focus = state.FocusWorkdirs
		} else {
			st.Focus = state.FocusFolders
		}
		st.NavOpen = true
	}
	lastNav := st.Focus
	if lastNav == state.FocusCodex || lastNav == "" {
		lastNav = state.FocusFolders
	}
	model := Model{
		cfg: cfg, runtime: rt, store: state.NewStore(rt.StatePath, rt.Workdir), state: st,
		width: 100, height: 32, screens: map[string]*TerminalScreen{}, ptys: map[string]*ptyx.Session{},
		visible:           map[string]bool{},
		codexInputBuffers: map[string][]rune{},
		dataCh:            make(chan ptyx.Data, 64),
		ctx:               ctx, cancel: cancel, input: input, lastNavFocus: lastNav,
	}
	if next, ok := state.SelectWorkdirByPath(model.state, rt.Workdir); ok {
		model.state = next
	}
	model.syncFolderCursor()
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
	loadingText := ""
	if content == "" && m.codexLoading() {
		loadingText = m.loadingLabel()
	} else if content == "" && m.activeErrorText() != "" {
		content = m.activeErrorText()
	} else if content == "" {
		content = "No Codex agent open."
	}
	title := "Codex"
	if active := state.ActiveAgent(m.state); active != nil {
		title = m.renderAgentTitle(*active)
	}
	return ipc.Snapshot{
		State:        m.state,
		CodexTitle:   title,
		CodexContent: content,
		LoadingText:  loadingText,
		Message:      m.message,
		NavWidth:     m.targetNavWidth(),
		FolderCursor: m.folderCursor,
	}
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
	return "Codex failed to start:\n" + detail
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
		if !m.codexLoading() {
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
	content := m.activeOutput()
	loadingText := ""
	if content == "" && m.codexLoading() {
		loadingText = m.loadingLabel()
	} else if content == "" {
		content = "No Codex agent open."
	}
	title := "Codex"
	if active := state.ActiveAgent(m.state); active != nil {
		title = m.renderAgentTitle(*active)
	}
	if loadingText != "" {
		return renderLoadingWorkspaceWithNavWidth(m.cfg, m.state, title, loadingText, m.width, m.height, m.message, m.navWidth, m.folderCursor)
	}
	return renderWorkspaceWithNavWidth(m.cfg, m.state, title, content, m.width, m.height, m.message, m.navWidth, m.folderCursor)
}

func (m Model) modalView(content string) string {
	w := max(40, min(m.width-4, 82))
	box := modalStyle.Width(w).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderInputModal() string {
	width := max(36, min(m.width-16, 72))
	return renderPromptModal(m.promptContext(), m.input, width, m.height, m.promptSuggestionOpen, m.renderPromptExtra(m.input, width))
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

	quitPressed := bindingMatches(m.cfg.KeyBindings.Quit, msg)
	if quitPressed && !m.activeCodexReceivesQuitBinding() {
		m.closeWeft()
		return m, nil
	}
	if bindingMatches(m.cfg.KeyBindings.Drawer, msg) {
		return m, m.toggleDrawer()
	}
	if m.state.Focus == state.FocusCodex && state.ActiveAgent(m.state) != nil {
		if active := state.ActiveAgent(m.state); active != nil {
			if pty := m.ptys[active.ID]; pty != nil {
				_ = pty.Write(encodeKey(msg))
			}
			return m, m.captureCodexInput(*active, msg)
		}
		return m, nil
	}
	if m.state.Focus == state.FocusCodex {
		cmd := m.openNav()
		updated, nextCmd := m.handleNavKey(msg)
		return updated, tea.Batch(cmd, nextCmd)
	}
	if bindingMatches(m.cfg.KeyBindings.Help, msg) {
		m.mode = modeHelp
		return m, nil
	}
	return m.handleNavKey(msg)
}

func (m Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m Model) activeCodexReceivesQuitBinding() bool {
	if m.state.Focus != state.FocusCodex {
		return false
	}
	active := state.ActiveAgent(m.state)
	if active == nil || m.ptys[active.ID] == nil {
		return false
	}
	return true
}

func (m Model) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case bindingMatches(m.cfg.KeyBindings.FocusLeft, msg):
		m.focusNavPane(state.FocusWorkdirs)
	case bindingMatches(m.cfg.KeyBindings.FocusRight, msg):
		m.focusNavPane(state.FocusFolders)
	case bindingMatches(m.cfg.KeyBindings.SelectPrev, msg) || msg.Type == tea.KeyUp:
		m.moveSelection(-1)
	case bindingMatches(m.cfg.KeyBindings.SelectNext, msg) || msg.Type == tea.KeyDown:
		m.moveSelection(1)
	case bindingMatches(m.cfg.KeyBindings.NewWorkspace, msg):
		m.startPrompt(promptWorkdir, defaultWorkdirPromptValue(m.state, m.runtime.Workdir))
	case bindingMatches(m.cfg.KeyBindings.NewGroup, msg):
		m.focusNavPane(state.FocusFolders)
		m.startPrompt(promptGroup, "")
	case bindingMatches(m.cfg.KeyBindings.NewAgent, msg):
		return m, m.newAgent(titles.CodexTemplate)
	case bindingMatches(m.cfg.KeyBindings.MoveAgent, msg):
		if agent := m.selectedAgent(); agent != nil {
			m.startPrompt(promptMoveAgent, "")
		}
	case bindingMatches(m.cfg.KeyBindings.Rename, msg):
		m.startRenamePrompt()
	case bindingMatches(m.cfg.KeyBindings.Delete, msg):
		m.startDeleteConfirm()
	case bindingMatches(m.cfg.KeyBindings.Open, msg) || msg.Type == tea.KeyEnter:
		if m.state.Focus == state.FocusWorkdirs {
			m.focusNavPane(state.FocusFolders)
			return m, nil
		}
		row := m.currentFolderRow()
		if row.kind == folderRowFolder {
			m.toggleSelectedGroup(row.folderID)
			return m, nil
		}
		if agent := m.selectedAgent(); agent != nil {
			m.state.ActiveAgentID = agent.ID
			m.state.SelectedWorkdirID = agent.WorkdirID
			m.state.SelectedFolderID = agent.FolderID
			m.save()
			return m, m.setCodexFocus()
		}
	}
	return m, nil
}

func (m *Model) focusNavPane(focus state.Focus) {
	if focus != state.FocusWorkdirs && focus != state.FocusFolders {
		return
	}
	m.state.Focus = focus
	m.state.NavOpen = true
	m.lastNavFocus = focus
	m.save()
}

func (m *Model) rememberCurrentNavFocus() {
	if m.state.Focus == state.FocusWorkdirs || m.state.Focus == state.FocusFolders {
		m.lastNavFocus = m.state.Focus
	}
}

func (m *Model) moveSelection(delta int) {
	if m.state.Focus == state.FocusWorkdirs {
		workdirIDs := make([]string, 0, len(m.state.Workdirs))
		for _, workdir := range m.state.Workdirs {
			workdirIDs = append(workdirIDs, workdir.ID)
		}
		current := navigation.IndexByID(workdirIDs, m.state.SelectedWorkdirID)
		next := navigation.MoveIndex(current, len(workdirIDs), delta)
		if len(workdirIDs) > 0 && workdirIDs[next] != m.state.SelectedWorkdirID {
			m.state.SelectedWorkdirID = workdirIDs[next]
			if folders := state.FoldersForWorkdir(m.state, m.state.SelectedWorkdirID); len(folders) > 0 {
				m.state.SelectedFolderID = folders[0].ID
			}
			m.folderCursor = 0
			m.save()
		}
		return
	}
	rows := m.folderRows()
	if len(rows) == 0 {
		return
	}
	m.folderCursor = navigation.MoveIndex(m.folderCursor, len(rows), delta)
	m.applyFolderCursor(rows[m.folderCursor])
}

func (m *Model) applyFolderCursor(row folderRow) {
	switch row.kind {
	case folderRowFolder:
		m.state.SelectedFolderID = row.folderID
	case folderRowAgent:
		if agent := state.AgentByID(m.state, row.agentID); agent != nil {
			m.state.SelectedFolderID = agent.FolderID
			m.state.ActiveAgentID = agent.ID
		}
	}
	m.save()
}

func (m *Model) startPrompt(prompt promptKind, value string) {
	m.prompt = prompt
	configurePromptInput(&m.input, m.promptContext(), value)
	m.promptSuggestionOpen = false
	m.mode = modeInput
}

func (m *Model) startRenamePrompt() {
	prompt, id, value, ok := renamePromptTargetForState(m.state, m.folderCursor)
	if ok {
		m.pendingID = id
		m.startPrompt(prompt, value)
	}
}

func (m *Model) startDeleteConfirm() {
	confirm, id, ok := deleteConfirmTargetForState(m.state, m.folderCursor)
	if ok {
		m.confirm = confirm
		m.pendingID = id
		m.mode = modeConfirm
	}
}

func (m *Model) applyPrompt(value string) tea.Cmd {
	switch m.prompt {
	case promptWorkdir:
		next, workdir, err := state.AddWorkdir(m.state, shortID(), value, state.NowISO())
		if err != nil {
			m.message = err.Error()
			return nil
		}
		message := workdirAddMessage(m.state, workdir)
		m.state = next
		m.message = message
		m.rememberCurrentNavFocus()
		m.syncFolderCursor()
		m.save()
	case promptGroup:
		workdir := state.ActiveWorkdir(m.state)
		if workdir == nil {
			m.message = "select a workspace first"
			return nil
		}
		next, folder, err := state.AddFolder(m.state, shortID(), workdir.ID, value, state.NowISO())
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "created group " + folder.Path
		m.syncFolderCursor()
		m.save()
	case promptRenameGroup:
		next, err := state.RenameFolder(m.state, m.pendingID, value)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "renamed group"
		m.syncFolderCursor()
		m.save()
	case promptWorkdirTitle:
		next, err := state.SetWorkdirTitle(m.state, m.pendingID, value)
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
	case promptRenameAgent:
		next, err := state.RenameAgent(m.state, m.pendingID, value)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "renamed agent"
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
			m.message = "select an agent first"
			return nil
		}
		folderID := ""
		if value != "" {
			folder := m.findFolderByPath(agent.WorkdirID, value)
			if folder == nil {
				m.message = "group not found"
				return nil
			}
			folderID = folder.ID
		}
		next, err := state.MoveAgent(m.state, agent.ID, folderID)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "moved agent"
		m.syncFolderCursor()
		m.save()
	}
	return nil
}

func (m *Model) applyConfirm() tea.Cmd {
	switch m.confirm {
	case confirmAddLaunchWorkdir:
		next, workdir, err := state.AddWorkdir(m.state, shortID(), m.pendingID, state.NowISO())
		if err != nil {
			m.message = err.Error()
			return nil
		}
		message := workdirAddMessage(m.state, workdir)
		m.state = next
		m.message = message
		m.rememberCurrentNavFocus()
		m.syncFolderCursor()
		m.save()
	case confirmDeleteWorkdir:
		next, agents, err := state.RemoveWorkdir(m.state, m.pendingID)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		for _, agent := range agents {
			m.killAgentPTY(agent.ID)
		}
		m.state = state.Repair(next, m.runtime.Workdir)
		m.message = "removed workspace"
		m.syncFolderCursor()
		m.save()
		return m.startNavAnimation()
	case confirmDeleteGroup:
		next, err := state.DeleteFolder(m.state, m.pendingID)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "deleted group"
		m.syncFolderCursor()
		m.save()
	case confirmDeleteAgent:
		return m.closeAgent(m.pendingID)
	}
	return nil
}

func (m *Model) newAgent(title string) tea.Cmd {
	workdir := state.ActiveWorkdir(m.state)
	if workdir == nil {
		m.message = "add a workspace first"
		return nil
	}
	folderID := m.groupIDForNewAgent()
	next, agent, err := state.AddAgent(m.state, shortID(), workdir.ID, folderID, title, state.NowISO())
	if err != nil {
		m.message = err.Error()
		return nil
	}
	m.state = next
	m.syncFolderCursor()
	m.save()
	return tea.Batch(m.startPTYCmd(agent.ID), m.startNavAnimation(), tickLoading())
}

func (m *Model) closeAgent(agentID string) tea.Cmd {
	if agentID == "" {
		return nil
	}
	m.killAgentPTY(agentID)
	m.state = state.CloseAgent(m.state, agentID)
	m.syncFolderCursor()
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
}

func (m *Model) selectedAgent() *state.Agent {
	return selectedAgentForState(m.state, m.folderCursor)
}

func (m Model) currentFolderRow() folderRow {
	return currentFolderRowForState(m.state, m.folderCursor)
}

func (m Model) folderRows() []folderRow {
	return folderRowsForState(m.state)
}

func (m Model) groupIDForNewAgent() string {
	row := m.currentFolderRow()
	switch row.kind {
	case folderRowFolder:
		return row.folderID
	case folderRowAgent:
		return row.folderID
	default:
		return ""
	}
}

func (m *Model) toggleSelectedGroup(folderID string) {
	m.state = state.ToggleGroupCollapsed(m.state, folderID)
	m.state.SelectedFolderID = folderID
	for index, row := range m.folderRows() {
		if row.kind == folderRowFolder && row.folderID == folderID {
			m.folderCursor = index
			break
		}
	}
	m.save()
}

func (m *Model) syncFolderCursor() {
	rows := m.folderRows()
	if len(rows) == 0 {
		m.folderCursor = 0
		return
	}
	if m.state.ActiveAgentID != "" {
		for index, row := range rows {
			if row.kind == folderRowAgent && row.agentID == m.state.ActiveAgentID {
				m.folderCursor = index
				return
			}
		}
	}
	for index, row := range rows {
		if row.folderID == m.state.SelectedFolderID {
			m.folderCursor = index
			return
		}
	}
	m.folderCursor = 0
}

func (m Model) findFolderByPath(workdirID string, path string) *state.Folder {
	path = strings.TrimSpace(path)
	for _, folder := range state.FoldersForWorkdir(m.state, workdirID) {
		if folder.Path == path {
			f := folder
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
	if m.lastNavFocus != state.FocusWorkdirs && m.lastNavFocus != state.FocusFolders {
		m.lastNavFocus = state.FocusFolders
	}
	m.state.Focus = m.lastNavFocus
	m.save()
	return m.startNavAnimation()
}

func (m *Model) setCodexFocus() tea.Cmd {
	if state.ActiveAgent(m.state) == nil {
		m.message = "select an agent first"
		return nil
	}
	if m.state.Focus == state.FocusWorkdirs || m.state.Focus == state.FocusFolders {
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
	return strings.Contains(m.cfg.TitleTemplate, titles.AutoTemplate) || strings.Contains(agent.Title, titles.AutoTemplate)
}

func (m Model) titleHookCmd(agent state.Agent, firstMessage string) tea.Cmd {
	workdir := state.Workdir{}
	if found := state.WorkdirForAgent(m.state, agent); found != nil {
		workdir = *found
	}
	folder := state.Folder{}
	if found := state.FolderForAgent(m.state, agent); found != nil {
		folder = *found
	}
	payload := titlehook.BuildPayload(agent, workdir, folder, m.cfg.TitleTemplate, firstMessage)
	command := m.cfg.TitleHookCommand
	timeout := time.Duration(m.cfg.TitleHookTimeoutSeconds) * time.Second
	ctx := m.ctx
	return func() tea.Msg {
		title, err := titlehook.Run(ctx, command, workdir.Path, timeout, payload)
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
		return "renamed agent; auto title ready"
	}
	if strings.TrimSpace(agent.AutoTitleError) != "" {
		return "renamed agent; auto title failed"
	}
	if agent.AutoTitleAttempted {
		return "renamed agent; auto title is generating"
	}
	if strings.TrimSpace(m.cfg.TitleHookCommand) == "" {
		return "renamed agent; auto title unavailable: set title_hook_command"
	}
	return "renamed agent; auto title will generate from the first message"
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
	workdir := m.agentWorkdir(agentID)
	ptySession, err := ptyx.Start(m.ctx, agentID, m.cfg.CodexCommand, workdir, m.ptyWidth(), m.ptyHeight(), func(data ptyx.Data) {
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
		agent.Status = state.StatusRunning
		return agent
	})
	m.save()
}

func (m *Model) startPTYCmd(agentID string) tea.Cmd {
	if m.ptys[agentID] != nil {
		return nil
	}
	ctx := m.ctx
	command := m.cfg.CodexCommand
	workdir := m.agentWorkdir(agentID)
	cols := m.ptyWidth()
	rows := m.ptyHeight()
	dataCh := m.dataCh
	return func() tea.Msg {
		ptySession, err := ptyx.Start(ctx, agentID, command, workdir, cols, rows, func(data ptyx.Data) {
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
		agent.Status = state.StatusRunning
		return agent
	})
	m.save()
}

func (m *Model) applyPTYData(data ptyx.Data) {
	if state.AgentByID(m.state, data.TabID) == nil {
		return
	}
	if data.Err != nil {
		m.state = state.WithUpdatedAgent(m.state, data.TabID, func(agent state.Agent) state.Agent {
			if agent.Status != state.StatusError {
				agent.Status = state.StatusStopped
			}
			return agent
		})
		m.save()
		return
	}
	if data.Text != "" {
		screen := m.screens[data.TabID]
		if screen == nil {
			screen = NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
			m.screens[data.TabID] = screen
		}
		screen.Write(data.Text)
		if screen.HasVisibleContent() {
			m.visible[data.TabID] = true
		}
	}
	if data.Title != "" {
		m.state = state.WithUpdatedAgent(m.state, data.TabID, func(agent state.Agent) state.Agent {
			agent.CodexTitle = titles.NormalizeCodexTitle(data.Title)
			agent.Status = state.StatusRunning
			return agent
		})
		m.save()
	}
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

func (m Model) codexLoading() bool {
	active := state.ActiveAgent(m.state)
	if active == nil || active.Status == state.StatusError || active.Status == state.StatusStopped {
		return false
	}
	screen := m.screens[active.ID]
	return screen == nil || (!screen.HasVisibleContent() && !m.visible[active.ID])
}

func (m Model) loadingLabel() string {
	frame := loadingFrames[m.loading%len(loadingFrames)]
	return frame + " Starting Codex"
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
	for _, screen := range m.screens {
		screen.Resize(m.ptyWidth(), m.ptyHeight())
	}
}

func (m Model) ptyWidth() int {
	return max(20, codexLineContentWidth(max(0, m.codexPaneWidth()-2)))
}

func (m Model) ptyHeight() int {
	return max(5, m.height-1)
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
	m.applyLaunchWorkdir(launchWorkspaceArg(request.Args))
	switch request.Command {
	case "snapshot":
		return m.ipcResponse(m.message), nil
	case "status":
		response := m.ipcResponse(m.statusText())
		response.Message = m.statusText()
		return response, nil
	case "refresh":
		m.message = "refreshed"
		return m.ipcResponse("refreshed Weft command center"), nil
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
		m.navWidth = m.targetNavWidth()
		return m.ipcResponse("focus updated"), cmd
	case "nav_move":
		delta, err := strconv.Atoi(request.Args["delta"])
		if err != nil || delta == 0 {
			return ipc.ErrorResponse("invalid_delta", "delta must be a non-zero integer"), nil
		}
		m.moveSelection(delta)
		return m.ipcResponse("selection updated"), nil
	case "open":
		cmd := m.openSelection()
		m.navWidth = m.targetNavWidth()
		return m.ipcResponse("selection opened"), cmd
	case "new":
		if state.ActiveWorkdir(m.state) == nil {
			return ipc.ErrorResponse("workdir_required", "add a workspace first"), nil
		}
		title := request.Args["title"]
		if title == "" {
			title = titles.CodexTemplate
		}
		cmd := m.newAgent(title)
		m.navWidth = m.targetNavWidth()
		return m.ipcResponse("created Codex agent"), cmd
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
		return m.ipcResponse("renamed Codex agent"), nil
	case "rename_group":
		next, err := state.RenameFolder(m.state, request.Args["id"], request.Args["path"])
		if err != nil {
			return ipcError("rename_group_failed", err), nil
		}
		m.state = next
		m.syncFolderCursor()
		m.save()
		return m.ipcResponse("renamed group"), nil
	case "rename_workspace", "rename_workdir":
		next, err := state.SetWorkdirTitle(m.state, request.Args["id"], request.Args["title"])
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
		m.navWidth = m.targetNavWidth()
		return m.ipcResponse("closed Codex agent"), cmd
	case "remove_workspace", "remove_workdir":
		next, agents, err := state.RemoveWorkdir(m.state, request.Args["id"])
		if err != nil {
			return ipcError("remove_workspace_failed", err), nil
		}
		for _, agent := range agents {
			m.killAgentPTY(agent.ID)
		}
		m.state = state.Repair(next, m.runtime.Workdir)
		m.syncFolderCursor()
		m.save()
		m.navWidth = m.targetNavWidth()
		return m.ipcResponse("removed workspace"), m.startNavAnimation()
	case "remove_group":
		next, err := state.DeleteFolder(m.state, request.Args["id"])
		if err != nil {
			return ipcError("remove_group_failed", err), nil
		}
		m.state = next
		m.syncFolderCursor()
		m.save()
		return m.ipcResponse("deleted group"), nil
	case "select":
		id := request.Args["id"]
		if agent := state.AgentByID(m.state, id); agent != nil {
			m.state.ActiveAgentID = id
			m.state.SelectedWorkdirID = agent.WorkdirID
			m.state.SelectedFolderID = agent.FolderID
			m.syncFolderCursor()
			m.save()
			return m.ipcResponse("selected Codex agent"), nil
		}
		return ipc.Response{OK: false, Message: "agent not found"}, nil
	case "move":
		id := request.Args["id"]
		if id == "" {
			id = m.state.ActiveAgentID
		}
		agent := state.AgentByID(m.state, id)
		if agent == nil {
			return ipc.ErrorResponse("agent_not_found", "agent not found"), nil
		}
		folderID, ok := m.destinationGroupIDForMove(*agent, request.Args)
		if !ok {
			return ipc.ErrorResponse("group_not_found", "group not found"), nil
		}
		next, err := state.MoveAgent(m.state, agent.ID, folderID)
		if err != nil {
			return ipcError("move_agent_failed", err), nil
		}
		m.state = next
		m.syncFolderCursor()
		m.save()
		return m.ipcResponse("moved Codex agent"), nil
	case "add_workspace", "add_workdir":
		path := request.Args["path"]
		next, workdir, err := state.AddWorkdir(m.state, shortID(), path, state.NowISO())
		if err != nil {
			return ipcError("add_workspace_failed", err), nil
		}
		message := workdirAddMessage(m.state, workdir)
		m.state = next
		m.rememberCurrentNavFocus()
		m.syncFolderCursor()
		m.save()
		return m.ipcResponse(message), nil
	case "add_group", "add_folder":
		path := request.Args["path"]
		workdirID := request.Args["workdir_id"]
		if workdirID == "" {
			workdirID = m.state.SelectedWorkdirID
		}
		next, _, err := state.AddFolder(m.state, shortID(), workdirID, path, state.NowISO())
		if err != nil {
			return ipcError("add_group_failed", err), nil
		}
		m.state = next
		m.syncFolderCursor()
		m.save()
		return m.ipcResponse("created group"), nil
	case "focus":
		target := state.Focus(request.Args["target"])
		if target == "workspaces" {
			target = state.FocusWorkdirs
		}
		if target == "agents" || target == "groups" || target == "folders" {
			target = state.FocusFolders
		}
		switch target {
		case state.FocusWorkdirs, state.FocusFolders:
			m.focusNavPane(target)
			m.navWidth = m.targetNavWidth()
			return m.ipcResponse("focus updated"), m.startNavAnimation()
		case state.FocusCodex:
			cmd := m.setCodexFocus()
			m.navWidth = m.targetNavWidth()
			return m.ipcResponse("focus updated"), cmd
		default:
			return ipc.Response{OK: false, Message: "focus target must be workspaces, agents, or codex"}, nil
		}
	case "codex_input":
		cmd := m.applyCodexInput(request.Args)
		return m.ipcResponse(m.message), cmd
	case "close_weft", "quit":
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
	return args["launch_workdir"]
}

func (m *Model) applyLaunchWorkdir(path string) {
	next, ok := state.SelectWorkdirByPath(m.state, path)
	if !ok {
		return
	}
	if next.SelectedWorkdirID == m.state.SelectedWorkdirID && next.SelectedFolderID == m.state.SelectedFolderID {
		return
	}
	m.state = next
	m.syncFolderCursor()
	m.save()
}

func (m *Model) openSelection() tea.Cmd {
	if m.state.Focus == state.FocusWorkdirs {
		m.focusNavPane(state.FocusFolders)
		return nil
	}
	row := m.currentFolderRow()
	if row.kind == folderRowFolder {
		m.toggleSelectedGroup(row.folderID)
		return nil
	}
	if agent := m.selectedAgent(); agent != nil {
		m.state.ActiveAgentID = agent.ID
		m.state.SelectedWorkdirID = agent.WorkdirID
		m.state.SelectedFolderID = agent.FolderID
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
	if pty := m.ptys[active.ID]; pty != nil {
		_ = pty.Write([]byte(args["encoded"]))
	}
	return m.captureCodexInputArgs(*active, args)
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
	case "ctrl+u":
		delete(m.codexInputBuffers, agent.ID)
	case "ctrl+w":
		m.codexInputBuffers[agent.ID] = trimLastWord(m.codexInputBuffers[agent.ID])
	}
	return nil
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
		folder := state.FolderByID(m.state, groupID)
		if folder != nil && folder.WorkdirID == agent.WorkdirID {
			return folder.ID, true
		}
		return "", false
	}
	if folderID, ok := args["folder_id"]; ok {
		if folderID == "" {
			return "", true
		}
		folder := state.FolderByID(m.state, folderID)
		if folder != nil && folder.WorkdirID == agent.WorkdirID {
			return folder.ID, true
		}
		return "", false
	}
	if folderPath, ok := args["folder"]; ok {
		if strings.TrimSpace(folderPath) == "" {
			return "", true
		}
		folder := m.findFolderByPath(agent.WorkdirID, folderPath)
		if folder == nil {
			return "", false
		}
		return folder.ID, true
	}
	if groupPath, ok := args["group"]; ok {
		if strings.TrimSpace(groupPath) == "" {
			return "", true
		}
		folder := m.findFolderByPath(agent.WorkdirID, groupPath)
		if folder == nil {
			return "", false
		}
		return folder.ID, true
	}
	folders := state.FoldersForWorkdir(m.state, agent.WorkdirID)
	current := 0
	groupIDs := []string{""}
	for index, folder := range folders {
		groupIDs = append(groupIDs, folder.ID)
		if folder.ID == agent.FolderID {
			current = index + 1
			break
		}
	}
	switch args["direction"] {
	case "left", "prev":
		current = navigation.MoveIndex(current, len(groupIDs), -1)
	case "right", "next":
		current = navigation.MoveIndex(current, len(groupIDs), 1)
	default:
		return "", false
	}
	return groupIDs[current], true
}

func (m Model) agentWorkdir(agentID string) string {
	agent := state.AgentByID(m.state, agentID)
	if agent == nil {
		return m.runtime.Workdir
	}
	if workdir := state.WorkdirForAgent(m.state, *agent); workdir != nil {
		return workdir.Path
	}
	return m.runtime.Workdir
}

func (m Model) renderAgentTitle(agent state.Agent) string {
	return renderAgentTitleForState(m.cfg, m.state, agent)
}

func (m Model) statusText() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "supervisor: running\n")
	fmt.Fprintf(&builder, "runtime dir: %s\n", m.runtime.Dir)
	fmt.Fprintf(&builder, "socket: %s\n", m.runtime.SocketPath)
	fmt.Fprintf(&builder, "launch workspace: %s\n", m.runtime.Workdir)
	fmt.Fprintf(&builder, "focus: %s\n", displayFocus(m.state.Focus))
	fmt.Fprintf(&builder, "nav open: %t\n", m.state.NavOpen)
	fmt.Fprintf(&builder, "workspaces: %d\n", len(m.state.Workdirs))
	fmt.Fprintf(&builder, "groups: %d\n", len(m.state.Folders))
	fmt.Fprintf(&builder, "agents: %d\n", len(m.state.Agents))
	for _, agent := range m.state.Agents {
		marker := " "
		if agent.ID == m.state.ActiveAgentID {
			marker = "*"
		}
		folder := ""
		if f := state.FolderForAgent(m.state, agent); f != nil {
			folder = f.Path
		}
		workdir := ""
		if w := state.WorkdirForAgent(m.state, agent); w != nil {
			workdir = w.Path
		}
		fmt.Fprintf(&builder, "%s %s %s %s %s %s\n", marker, agent.ID, folder, agent.Status, m.renderAgentTitle(agent), workdir)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func displayFocus(focus state.Focus) string {
	if focus == state.FocusWorkdirs {
		return "workspaces"
	}
	if focus == state.FocusFolders {
		return "agents"
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
		fmt.Sprintf("%s command center", cfg.KeyBindings.Drawer),
		fmt.Sprintf("%s/%s panes", cfg.KeyBindings.FocusLeft, cfg.KeyBindings.FocusRight),
		fmt.Sprintf("%s/%s select", cfg.KeyBindings.SelectPrev, cfg.KeyBindings.SelectNext),
		fmt.Sprintf("%s open agent", cfg.KeyBindings.Open),
		fmt.Sprintf("%s new workspace", cfg.KeyBindings.NewWorkspace),
		fmt.Sprintf("%s new group", cfg.KeyBindings.NewGroup),
		fmt.Sprintf("%s new agent", cfg.KeyBindings.NewAgent),
		fmt.Sprintf("%s move agent", cfg.KeyBindings.MoveAgent),
		fmt.Sprintf("%s rename", cfg.KeyBindings.Rename),
		fmt.Sprintf("%s delete", cfg.KeyBindings.Delete),
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
