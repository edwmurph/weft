package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/codux/internal/config"
	"github.com/edwmurph/codux/internal/ipc"
	"github.com/edwmurph/codux/internal/navigation"
	"github.com/edwmurph/codux/internal/ptyx"
	"github.com/edwmurph/codux/internal/state"
	"github.com/edwmurph/codux/internal/titles"
	"github.com/edwmurph/codux/internal/tmuxhost"
)

type ipcEnvelope struct {
	request ipc.Request
	reply   chan ipc.Response
}

type ptyStartedMsg struct {
	agentID string
	session *ptyx.Session
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
	confirmDeleteWorkdir confirmKind = "delete-workdir"
	confirmDeleteGroup   confirmKind = "delete-group"
	confirmDeleteAgent   confirmKind = "delete-agent"
)

const (
	navAnimationInterval = 12 * time.Millisecond
	navAnimationStep     = 4
	loadingInterval      = 90 * time.Millisecond
	inputModalLabelWidth = 9
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
	cfg       config.Config
	runtime   config.Runtime
	store     *state.Store
	state     state.State
	migration string
	width     int
	height    int
	mode      mode
	message   string
	navWidth  int
	loading   int

	screens map[string]*TerminalScreen
	ptys    map[string]*ptyx.Session
	visible map[string]bool
	dataCh  chan ptyx.Data
	ipcCh   chan ipcEnvelope
	stopIPC func() error
	ctx     context.Context
	cancel  context.CancelFunc

	input        textinput.Model
	prompt       promptKind
	confirm      confirmKind
	pendingID    string
	folderCursor int
	lastNavFocus state.Focus
}

func Run(rt config.Runtime, cfg config.Config, st state.State, migration *state.Migration) error {
	model := NewModel(rt, cfg, st)
	if migration != nil {
		model.migration = migration.Message
		model.message = "state migrated"
	}
	options := []tea.ProgramOption{
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stdout),
		tea.WithMouseCellMotion(),
	}
	if os.Getenv("CODUX_HEADLESS") == "1" {
		options = append(options, tea.WithoutRenderer())
	} else {
		options = append(options, tea.WithAltScreen())
	}
	program := tea.NewProgram(model, options...)
	_, err := program.Run()
	return err
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
		st.Focus = state.FocusFolders
		st.NavOpen = true
	}
	lastNav := st.Focus
	if lastNav == state.FocusCodex || lastNav == "" {
		lastNav = state.FocusFolders
	}
	model := Model{
		cfg: cfg, runtime: rt, store: state.NewStore(rt.StatePath, rt.Workdir), state: st,
		width: 100, height: 32, screens: map[string]*TerminalScreen{}, ptys: map[string]*ptyx.Session{},
		visible: map[string]bool{},
		dataCh:  make(chan ptyx.Data, 64), ipcCh: make(chan ipcEnvelope, 16),
		ctx: ctx, cancel: cancel, input: input, lastNavFocus: lastNav,
	}
	model.syncFolderCursor()
	model.navWidth = model.targetNavWidth()
	for _, agent := range model.state.Agents {
		model.startPTY(agent.ID)
	}
	_ = model.store.Write(model.state)
	return model
}

func (m Model) Init() tea.Cmd {
	stop, err := ipc.Serve(m.runtime.SocketPath, func(request ipc.Request) ipc.Response {
		reply := make(chan ipc.Response, 1)
		m.ipcCh <- ipcEnvelope{request: request, reply: reply}
		return <-reply
	})
	if err == nil {
		m.stopIPC = stop
	} else {
		m.message = fmt.Sprintf("IPC unavailable: %v", err)
	}
	return tea.Batch(waitPTY(m.dataCh), waitIPC(m.ipcCh), tickLoading())
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
	case ipcEnvelope:
		response, cmd := m.handleIPC(typed.request)
		typed.reply <- response
		return m, tea.Batch(waitIPC(m.ipcCh), cmd)
	case tea.KeyMsg:
		return m.handleKey(typed)
	}
	return m, nil
}

func (m Model) View() string {
	if m.mode == modeHelp {
		return m.modalView(renderHelp(m.cfg, m.migration))
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
	input := m.input
	input.Width = max(16, width-inputModalLabelWidth-3)
	lines := []string{modalTitleStyle.Render(m.promptTitle()), ""}
	label := m.promptLabel()
	lines = append(lines, renderInputModalRow(label, input.View(), width))
	if hint := m.promptHint(); hint != "" {
		lines = append(lines, "", mutedStyle.Render(clip(hint, width)))
	}
	if m.prompt == promptRenameAgent {
		lines = append(lines, "", modalLabelStyle.Render("Preview"))
		if active := m.selectedAgent(); active != nil {
			draft := *active
			if value := strings.TrimSpace(input.Value()); value != "" {
				draft.Title = value
			}
			lines = append(lines, modalValueStyle.Render(clip(m.renderAgentTitle(draft), width)))
		}
	}
	lines = append(lines, "", renderModalActions())
	return strings.Join(lines, "\n")
}

func renderInputModalRow(label string, value string, width int) string {
	valueWidth := max(0, width-inputModalLabelWidth-1)
	return modalLabelStyle.Render(padVisual(label, inputModalLabelWidth)) + " " + clip(value, valueWidth)
}

func renderModalActions() string {
	return modalKeyStyle.Render("Enter") + " save  " + modalKeyStyle.Render("Esc") + " cancel"
}

func (m Model) promptTitle() string {
	switch m.prompt {
	case promptWorkdir:
		return "Create workdir"
	case promptGroup:
		return "Create group"
	case promptWorkdirTitle:
		return "Rename workdir"
	case promptRenameGroup:
		return "Rename group"
	case promptRenameAgent:
		return "Rename agent"
	case promptMoveAgent:
		return "Move agent"
	default:
		return "Input"
	}
}

func (m Model) promptLabel() string {
	switch m.prompt {
	case promptWorkdir:
		return "Path"
	case promptGroup, promptRenameGroup, promptMoveAgent:
		return "Group"
	default:
		return "Title"
	}
}

func (m Model) promptHint() string {
	switch m.prompt {
	case promptWorkdir:
		return "Path must exist. Codux stores the absolute path and never deletes project files."
	case promptGroup:
		return "Group names are flat and unique within the selected workdir."
	case promptWorkdirTitle:
		return "Leave blank to use the default path title."
	case promptMoveAgent:
		return "Enter a group name in this workdir, or leave blank to make the agent top-level."
	default:
		return ""
	}
}

func (m Model) renderConfirmModal() string {
	name := "item"
	switch m.confirm {
	case confirmDeleteWorkdir:
		if workdir := state.WorkdirByID(m.state, m.pendingID); workdir != nil {
			name = "workdir " + workdir.Path
		}
	case confirmDeleteGroup:
		if folder := state.FolderByID(m.state, m.pendingID); folder != nil {
			name = "group " + folder.Path
		}
	case confirmDeleteAgent:
		if agent := state.AgentByID(m.state, m.pendingID); agent != nil {
			name = "agent " + m.renderAgentTitle(*agent)
		}
	}
	return fmt.Sprintf("Delete %s?\n\nY delete  N cancel", name)
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
		m.closeCodux()
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
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeNormal
		return m, nil
	case tea.KeyEnter:
		value := strings.TrimSpace(m.input.Value())
		if value == "" && m.prompt != promptMoveAgent && m.prompt != promptWorkdirTitle {
			m.message = "value is required"
			return m, nil
		}
		cmd := m.applyPrompt(value)
		m.mode = modeNormal
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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
	switch active.Status {
	case state.StatusStarting:
		return true
	case state.StatusRunning:
		return codexActivityStatus(active.CodexTitle) != "ready"
	default:
		return false
	}
}

func codexActivityStatus(title string) string {
	title = strings.ToLower(titles.NormalizeCodexTitle(title))
	for _, token := range strings.FieldsFunc(title, func(r rune) bool {
		return !unicode.IsLetter(r)
	}) {
		switch token {
		case "ready", "working":
			return token
		}
	}
	return ""
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
	case bindingMatches(m.cfg.KeyBindings.NewWorkdir, msg):
		m.startPrompt(promptWorkdir, "")
	case bindingMatches(m.cfg.KeyBindings.NewGroup, msg):
		m.focusNavPane(state.FocusFolders)
		m.startPrompt(promptGroup, "")
	case bindingMatches(m.cfg.KeyBindings.NewAgent, msg):
		return m, m.newAgent("Codex")
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
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.Focus()
	m.mode = modeInput
}

func (m *Model) startRenamePrompt() {
	if m.state.Focus == state.FocusWorkdirs {
		if workdir := state.WorkdirByID(m.state, m.state.SelectedWorkdirID); workdir != nil {
			m.pendingID = workdir.ID
			m.startPrompt(promptWorkdirTitle, workdir.Title)
		}
		return
	}
	row := m.currentFolderRow()
	switch row.kind {
	case folderRowFolder:
		if folder := state.FolderByID(m.state, row.folderID); folder != nil {
			m.pendingID = folder.ID
			m.startPrompt(promptRenameGroup, folder.Path)
		}
	case folderRowAgent:
		if agent := state.AgentByID(m.state, row.agentID); agent != nil {
			m.pendingID = agent.ID
			m.startPrompt(promptRenameAgent, agent.Title)
		}
	}
}

func (m *Model) startDeleteConfirm() {
	if m.state.Focus == state.FocusWorkdirs {
		if workdir := state.WorkdirByID(m.state, m.state.SelectedWorkdirID); workdir != nil {
			m.confirm = confirmDeleteWorkdir
			m.pendingID = workdir.ID
			m.mode = modeConfirm
		}
		return
	}
	row := m.currentFolderRow()
	switch row.kind {
	case folderRowFolder:
		if folder := state.FolderByID(m.state, row.folderID); folder != nil {
			m.confirm = confirmDeleteGroup
			m.pendingID = folder.ID
			m.mode = modeConfirm
		}
	case folderRowAgent:
		if agent := state.AgentByID(m.state, row.agentID); agent != nil {
			m.confirm = confirmDeleteAgent
			m.pendingID = agent.ID
			m.mode = modeConfirm
		}
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
		m.state = next
		m.message = "added workdir " + workdir.Path
		m.syncFolderCursor()
		m.save()
	case promptGroup:
		workdir := state.ActiveWorkdir(m.state)
		if workdir == nil {
			m.message = "select a workdir first"
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
			m.message = "cleared workdir title"
		} else {
			m.message = "renamed workdir"
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
		m.message = "removed workdir"
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
		m.message = "select a workdir first"
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
	row := m.currentFolderRow()
	if row.kind == folderRowAgent {
		return state.AgentByID(m.state, row.agentID)
	}
	return nil
}

func (m Model) currentFolderRow() folderRow {
	rows := m.folderRows()
	if len(rows) == 0 {
		return folderRow{}
	}
	if m.folderCursor < 0 || m.folderCursor >= len(rows) {
		return rows[0]
	}
	return rows[m.folderCursor]
}

func (m Model) folderRows() []folderRow {
	var rows []folderRow
	for _, agent := range state.UngroupedAgentsForWorkdir(m.state, m.state.SelectedWorkdirID) {
		rows = append(rows, folderRow{kind: folderRowAgent, agentID: agent.ID})
	}
	for _, folder := range state.FoldersForWorkdir(m.state, m.state.SelectedWorkdirID) {
		rows = append(rows, folderRow{kind: folderRowFolder, folderID: folder.ID})
		if state.IsGroupCollapsed(m.state, folder.ID) {
			continue
		}
		for _, agent := range state.AgentsForFolder(m.state, folder.ID) {
			rows = append(rows, folderRow{kind: folderRowAgent, folderID: folder.ID, agentID: agent.ID})
		}
	}
	return rows
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

func (m *Model) closeCodux() {
	_ = exec.Command("tmux", "detach-client", "-s", m.cfg.TmuxSession).Run()
	m.message = "closed Codux clients"
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
	return max(20, m.width-m.effectiveNavWidth()-4-codexLeftPadding)
}

func (m Model) ptyHeight() int {
	return max(5, m.height-1)
}

func (m Model) effectiveNavWidth() int {
	return min(max(0, m.navWidth), max(0, m.width-22))
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

func (m *Model) handleIPC(request ipc.Request) (ipc.Response, tea.Cmd) {
	switch request.Command {
	case "status":
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: m.statusText()}, nil
	case "refresh":
		m.message = "refreshed"
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "refreshed Codux command center"}, nil
	case "new":
		title := request.Args["title"]
		if title == "" {
			title = "Codex"
		}
		cmd := m.newAgent(title)
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "created Codex agent"}, cmd
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
			return ipc.Response{OK: false, Message: err.Error()}, nil
		}
		m.state = next
		m.save()
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "renamed Codex agent"}, nil
	case "close":
		id := request.Args["id"]
		if id == "" {
			id = m.state.ActiveAgentID
		}
		cmd := m.closeAgent(id)
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "closed Codex agent"}, cmd
	case "select":
		id := request.Args["id"]
		if agent := state.AgentByID(m.state, id); agent != nil {
			m.state.ActiveAgentID = id
			m.state.SelectedWorkdirID = agent.WorkdirID
			m.state.SelectedFolderID = agent.FolderID
			m.syncFolderCursor()
			m.save()
			st := m.state
			return ipc.Response{OK: true, State: &st, Message: "selected Codex agent"}, nil
		}
		return ipc.Response{OK: false, Message: "agent not found"}, nil
	case "move":
		id := request.Args["id"]
		if id == "" {
			id = m.state.ActiveAgentID
		}
		agent := state.AgentByID(m.state, id)
		if agent == nil {
			return ipc.Response{OK: false, Message: "agent not found"}, nil
		}
		folderID, ok := m.destinationGroupIDForMove(*agent, request.Args)
		if !ok {
			return ipc.Response{OK: false, Message: "group not found"}, nil
		}
		next, err := state.MoveAgent(m.state, agent.ID, folderID)
		if err != nil {
			return ipc.Response{OK: false, Message: err.Error()}, nil
		}
		m.state = next
		m.syncFolderCursor()
		m.save()
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "moved Codex agent"}, nil
	case "add_workdir":
		path := request.Args["path"]
		next, _, err := state.AddWorkdir(m.state, shortID(), path, state.NowISO())
		if err != nil {
			return ipc.Response{OK: false, Message: err.Error()}, nil
		}
		m.state = next
		m.syncFolderCursor()
		m.save()
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "added workdir"}, nil
	case "add_group", "add_folder":
		path := request.Args["path"]
		workdirID := request.Args["workdir_id"]
		if workdirID == "" {
			workdirID = m.state.SelectedWorkdirID
		}
		next, _, err := state.AddFolder(m.state, shortID(), workdirID, path, state.NowISO())
		if err != nil {
			return ipc.Response{OK: false, Message: err.Error()}, nil
		}
		m.state = next
		m.syncFolderCursor()
		m.save()
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "created group"}, nil
	case "focus":
		target := state.Focus(request.Args["target"])
		if target == "agents" || target == "groups" || target == "folders" {
			target = state.FocusFolders
		}
		switch target {
		case state.FocusWorkdirs, state.FocusFolders:
			m.focusNavPane(target)
			st := m.state
			return ipc.Response{OK: true, State: &st, Message: "focus updated"}, m.startNavAnimation()
		case state.FocusCodex:
			cmd := m.setCodexFocus()
			st := m.state
			return ipc.Response{OK: true, State: &st, Message: "focus updated"}, cmd
		default:
			return ipc.Response{OK: false, Message: "focus target must be workdirs, agents, or codex"}, nil
		}
	case "close_codux", "quit":
		m.closeCodux()
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "closed Codux clients"}, nil
	default:
		return ipc.Response{OK: false, Message: "unknown command: " + request.Command}, nil
	}
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
	workdir := state.Workdir{}
	folder := state.Folder{}
	if w := state.WorkdirForAgent(m.state, agent); w != nil {
		workdir = *w
	}
	if f := state.FolderForAgent(m.state, agent); f != nil {
		folder = *f
	}
	return titles.RenderAgent(agent, workdir, folder, m.cfg.TitleTemplate)
}

func (m Model) statusText() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "tmux session: %s\n", m.cfg.TmuxSession)
	fmt.Fprintf(&builder, "runtime dir: %s\n", m.runtime.Dir)
	fmt.Fprintf(&builder, "focus: %s\n", displayFocus(m.state.Focus))
	fmt.Fprintf(&builder, "nav open: %t\n", m.state.NavOpen)
	fmt.Fprintf(&builder, "workdirs: %d\n", len(m.state.Workdirs))
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

func waitIPC(ch <-chan ipcEnvelope) tea.Cmd {
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

func renderHelp(cfg config.Config, migration string) string {
	lines := []string{
		"Codux shortcuts",
		"",
		fmt.Sprintf("%s command center", cfg.KeyBindings.Drawer),
		fmt.Sprintf("%s/%s panes", cfg.KeyBindings.FocusLeft, cfg.KeyBindings.FocusRight),
		fmt.Sprintf("%s/%s select", cfg.KeyBindings.SelectPrev, cfg.KeyBindings.SelectNext),
		fmt.Sprintf("%s open agent", cfg.KeyBindings.Open),
		fmt.Sprintf("%s new workdir", cfg.KeyBindings.NewWorkdir),
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
	if migration != "" {
		lines = append(lines, "", migration)
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

func _hostVersionReference() string {
	return tmuxhost.HostVersion
}
