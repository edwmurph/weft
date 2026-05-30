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
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/titles"
	"github.com/edwmurph/weft/internal/version"
)

const clientSnapshotInterval = 120 * time.Millisecond

type clientResponseMsg struct {
	command  string
	response ipc.Response
	err      error
}

type clientSnapshotTick struct{}

type ClientModel struct {
	cfg      config.Config
	runtime  config.Runtime
	clientID string
	snapshot ipc.Snapshot
	width    int
	height   int
	mode     mode
	message  string
	loading  int

	input                 textinput.Model
	prompt                promptKind
	confirm               confirmKind
	pendingID             string
	workdirSuggestionOpen bool
	codexInputQueue       []map[string]string
	codexInputInFlight    bool
}

func RunClient(rt config.Runtime, cfg config.Config) error {
	model := NewClientModel(rt, cfg)
	enableTerminalKeyboardReporting()
	defer disableTerminalKeyboardReporting()
	options := []tea.ProgramOption{
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stdout),
		tea.WithMouseCellMotion(),
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
	st := state.Repair(state.Empty(), rt.Workdir)
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
			m.message = typed.err.Error()
			return m, m.nextCodexInputRequest()
		}
		m.applyResponse(typed.response)
		nextCodexInput := m.nextCodexInputRequest()
		if typed.response.Snapshot != nil && typed.response.Snapshot.DetachClient {
			return m, tea.Batch(nextCodexInput, m.request("client_detached", nil), tea.Quit)
		}
		return m, nextCodexInput
	case clientSnapshotTick:
		return m, tea.Batch(m.request("snapshot", nil), tickClientSnapshot())
	case loadingTick:
		if strings.TrimSpace(m.snapshot.LoadingText) == "" {
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
		return m.modalView(renderHelp(m.cfg, ""))
	}
	if m.mode == modeInput {
		return m.modalView(m.renderInputModal())
	}
	if m.mode == modeConfirm {
		return m.modalView(m.renderConfirmModal())
	}
	loadingText := m.snapshot.LoadingText
	if loadingText != "" {
		loadingText = loadingFrames[m.loading%len(loadingFrames)] + strings.TrimPrefix(loadingText, loadingFrames[0])
		return renderLoadingWorkspaceWithNavWidth(m.cfg, m.snapshot.State, m.snapshot.CodexTitle, loadingText, m.width, m.height, m.messageText(), m.snapshot.NavWidth, m.snapshot.FolderCursor)
	}
	return renderWorkspaceWithNavWidth(m.cfg, m.snapshot.State, m.snapshot.CodexTitle, m.snapshot.CodexContent, m.width, m.height, m.messageText(), m.snapshot.NavWidth, m.snapshot.FolderCursor)
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
		return m, m.request("focus", map[string]string{"target": string(state.FocusWorkdirs)})
	case bindingMatches(m.cfg.KeyBindings.FocusRight, msg):
		return m, m.request("focus", map[string]string{"target": string(state.FocusFolders)})
	case bindingMatches(m.cfg.KeyBindings.SelectPrev, msg) || msg.Type == tea.KeyUp:
		return m, m.request("nav_move", map[string]string{"delta": "-1"})
	case bindingMatches(m.cfg.KeyBindings.SelectNext, msg) || msg.Type == tea.KeyDown:
		return m, m.request("nav_move", map[string]string{"delta": "1"})
	case bindingMatches(m.cfg.KeyBindings.NewWorkdir, msg):
		m.startPrompt(promptWorkdir, defaultWorkdirPromptValue(m.snapshot.State, m.runtime.Workdir))
	case bindingMatches(m.cfg.KeyBindings.NewGroup, msg):
		m.startPrompt(promptGroup, "")
	case bindingMatches(m.cfg.KeyBindings.NewAgent, msg):
		return m, m.request("new", map[string]string{"title": titles.CodexTemplate})
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
	if handlePromptWordKey(&m.input, m.prompt, msg) {
		refreshPromptInput(&m.input, m.prompt)
		if m.prompt == promptWorkdir {
			m.workdirSuggestionOpen = len(m.input.MatchedSuggestions()) > 0
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		if m.prompt == promptWorkdir && m.workdirSuggestionOpen {
			m.workdirSuggestionOpen = false
			return m, nil
		}
		m.mode = modeNormal
		return m, nil
	case tea.KeyEnter:
		if m.prompt == promptWorkdir && m.workdirSuggestionOpen && completeWorkdirSuggestion(&m.input) {
			m.workdirSuggestionOpen = false
			return m, nil
		}
		if m.prompt == promptWorkdir && !workdirInputIsExistingDirectory(m.input.Value()) {
			m.message = inspectWorkdirPromptPath(m.snapshot.State, m.input.Value()).message
			return m, nil
		}
		value := strings.TrimSpace(m.input.Value())
		if value == "" && m.prompt != promptMoveAgent && m.prompt != promptWorkdirTitle {
			m.message = "value is required"
			return m, nil
		}
		cmd := m.applyPrompt(value)
		m.mode = modeNormal
		return m, cmd
	case tea.KeyTab:
		if m.prompt == promptWorkdir && len(m.input.MatchedSuggestions()) > 0 {
			if m.workdirSuggestionOpen {
				completeWorkdirSuggestion(&m.input)
				m.workdirSuggestionOpen = false
				return m, nil
			}
			m.workdirSuggestionOpen = true
			return m, nil
		}
	case tea.KeyUp, tea.KeyDown:
		if m.prompt == promptWorkdir && len(m.input.MatchedSuggestions()) > 0 && !m.workdirSuggestionOpen {
			m.workdirSuggestionOpen = true
			return m, nil
		}
	}
	oldValue := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	refreshPromptInput(&m.input, m.prompt)
	if m.prompt == promptWorkdir {
		if m.input.Value() != oldValue {
			m.workdirSuggestionOpen = len(m.input.MatchedSuggestions()) > 0
		} else if len(m.input.MatchedSuggestions()) == 0 {
			m.workdirSuggestionOpen = false
		}
	}
	return m, cmd
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
	configurePromptInput(&m.input, prompt, value)
	m.workdirSuggestionOpen = false
	m.mode = modeInput
}

func (m *ClientModel) startRenamePrompt() {
	st := m.snapshot.State
	if st.Focus == state.FocusWorkdirs {
		if workdir := state.WorkdirByID(st, st.SelectedWorkdirID); workdir != nil {
			m.pendingID = workdir.ID
			m.startPrompt(promptWorkdirTitle, workdir.Title)
		}
		return
	}
	row := m.currentFolderRow()
	switch row.kind {
	case folderRowFolder:
		if folder := state.FolderByID(st, row.folderID); folder != nil {
			m.pendingID = folder.ID
			m.startPrompt(promptRenameGroup, folder.Path)
		}
	case folderRowAgent:
		if agent := state.AgentByID(st, row.agentID); agent != nil {
			m.pendingID = agent.ID
			m.startPrompt(promptRenameAgent, agent.Title)
		}
	}
}

func (m *ClientModel) startDeleteConfirm() {
	st := m.snapshot.State
	if st.Focus == state.FocusWorkdirs {
		if workdir := state.WorkdirByID(st, st.SelectedWorkdirID); workdir != nil {
			m.confirm = confirmDeleteWorkdir
			m.pendingID = workdir.ID
			m.mode = modeConfirm
		}
		return
	}
	row := m.currentFolderRow()
	switch row.kind {
	case folderRowFolder:
		if folder := state.FolderByID(st, row.folderID); folder != nil {
			m.confirm = confirmDeleteGroup
			m.pendingID = folder.ID
			m.mode = modeConfirm
		}
	case folderRowAgent:
		if agent := state.AgentByID(st, row.agentID); agent != nil {
			m.confirm = confirmDeleteAgent
			m.pendingID = agent.ID
			m.mode = modeConfirm
		}
	}
}

func (m ClientModel) applyPrompt(value string) tea.Cmd {
	switch m.prompt {
	case promptWorkdir:
		return m.request("add_workdir", map[string]string{"path": value})
	case promptGroup:
		return m.request("add_group", map[string]string{"path": value})
	case promptRenameGroup:
		return m.request("rename_group", map[string]string{"id": m.pendingID, "path": value})
	case promptWorkdirTitle:
		return m.request("rename_workdir", map[string]string{"id": m.pendingID, "title": value})
	case promptRenameAgent:
		return m.request("rename", map[string]string{"id": m.pendingID, "title": value})
	case promptMoveAgent:
		if agent := m.selectedAgent(); agent != nil {
			return m.request("move", map[string]string{"id": agent.ID, "group": value})
		}
	}
	return nil
}

func (m ClientModel) applyConfirm() tea.Cmd {
	switch m.confirm {
	case confirmDeleteWorkdir:
		return m.request("remove_workdir", map[string]string{"id": m.pendingID})
	case confirmDeleteGroup:
		return m.request("remove_group", map[string]string{"id": m.pendingID})
	case confirmDeleteAgent:
		return m.request("close", map[string]string{"id": m.pendingID})
	}
	return nil
}

func (m ClientModel) request(command string, args map[string]string) tea.Cmd {
	rt := m.runtime
	clientID := m.clientID
	return func() tea.Msg {
		args = cloneArgs(args)
		args["client_id"] = clientID
		response, err := ipc.Call(rt.SocketPath, ipc.Request{Command: command, Args: args}, 2*time.Second)
		return clientResponseMsg{command: command, response: response, err: err}
	}
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
	if response.Upgrade != nil && strings.TrimSpace(response.Upgrade.Message) != "" {
		m.message = response.Upgrade.Message
	} else if response.SupervisorVersion != "" && response.SupervisorVersion != version.Version {
		m.message = fmt.Sprintf("Weft client %s found running supervisor %s. Restarting the supervisor can stop live Codex terminals. Saved layout and metadata remain.", version.Version, response.SupervisorVersion)
	}
}

func (m ClientModel) modalView(content string) string {
	w := max(40, min(m.width-4, 82))
	box := modalStyle.Width(w).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m ClientModel) renderInputModal() string {
	width := max(36, min(m.width-16, 72))
	input := m.input
	input.Width = max(16, width-inputModalLabelWidth-3)
	lines := []string{modalTitleStyle.Render(m.promptTitle()), ""}
	if m.prompt == promptWorkdir {
		lines = append(lines, renderWorkdirPromptInput(input, width)...)
		if suggestions := renderWorkdirSuggestionMenu(input, width, m.workdirSuggestionOpen, workdirSuggestionRows(m.height)); len(suggestions) > 0 {
			lines = append(lines, suggestions...)
		}
		lines = append(lines, renderWorkdirPromptStatus(m.snapshot.State, input.Value(), width))
	} else {
		label := m.promptLabel()
		lines = append(lines, renderInputModalRow(label, input.View(), width))
	}
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
			lines = append(lines, modalValueStyle.Render(clip(m.renderAgentBaseTitle(draft), width)))
			if notice := m.autoTitleNotice(*active, draft.Title); notice != "" {
				lines = append(lines, mutedStyle.Render(clip(notice, width)))
			}
		}
		lines = append(lines, "", modalLabelStyle.Render("Variables"))
		lines = append(lines, m.renderTitleVariables(width)...)
	}
	if m.prompt == promptWorkdir {
		lines = append(lines, "", renderWorkdirModalActions(input, m.workdirSuggestionOpen))
	} else {
		lines = append(lines, "", renderModalActions(m.prompt))
	}
	return strings.Join(lines, "\n")
}

func (m ClientModel) renderTitleVariables(width int) []string {
	var lines []string
	for _, variable := range titles.TemplateVariables() {
		lines = append(lines, mutedStyle.Render(clip(fmt.Sprintf("- %s: %s", variable.Name, variable.Description), width)))
	}
	return lines
}

func (m ClientModel) promptTitle() string {
	return Model{prompt: m.prompt}.promptTitle()
}

func (m ClientModel) promptLabel() string {
	return Model{prompt: m.prompt}.promptLabel()
}

func (m ClientModel) promptHint() string {
	return Model{prompt: m.prompt}.promptHint()
}

func (m ClientModel) renderConfirmModal() string {
	name := "item"
	st := m.snapshot.State
	switch m.confirm {
	case confirmDeleteWorkdir:
		if workdir := state.WorkdirByID(st, m.pendingID); workdir != nil {
			name = "workdir " + workdir.Path
		}
	case confirmDeleteGroup:
		if folder := state.FolderByID(st, m.pendingID); folder != nil {
			name = "group " + folder.Path
		}
	case confirmDeleteAgent:
		if agent := state.AgentByID(st, m.pendingID); agent != nil {
			name = "agent " + m.renderAgentTitle(*agent)
		}
	}
	return fmt.Sprintf("Delete %s?\n\nY delete  N cancel", name)
}

func (m ClientModel) activeCodexReceivesQuitBinding() bool {
	if m.snapshot.State.Focus != state.FocusCodex {
		return false
	}
	active := state.ActiveAgent(m.snapshot.State)
	if active == nil {
		return false
	}
	return true
}

func (m ClientModel) selectedAgent() *state.Agent {
	row := m.currentFolderRow()
	if row.kind == folderRowAgent {
		return state.AgentByID(m.snapshot.State, row.agentID)
	}
	return nil
}

func (m ClientModel) currentFolderRow() folderRow {
	rows := m.folderRows()
	if len(rows) == 0 {
		return folderRow{}
	}
	if m.snapshot.FolderCursor < 0 || m.snapshot.FolderCursor >= len(rows) {
		return rows[0]
	}
	return rows[m.snapshot.FolderCursor]
}

func (m ClientModel) folderRows() []folderRow {
	st := m.snapshot.State
	var rows []folderRow
	for _, agent := range state.UngroupedAgentsForWorkdir(st, st.SelectedWorkdirID) {
		rows = append(rows, folderRow{kind: folderRowAgent, agentID: agent.ID})
	}
	for _, folder := range state.FoldersForWorkdir(st, st.SelectedWorkdirID) {
		rows = append(rows, folderRow{kind: folderRowFolder, folderID: folder.ID})
		if state.IsGroupCollapsed(st, folder.ID) {
			continue
		}
		for _, agent := range state.AgentsForFolder(st, folder.ID) {
			rows = append(rows, folderRow{kind: folderRowAgent, folderID: folder.ID, agentID: agent.ID})
		}
	}
	return rows
}

func (m ClientModel) renderAgentTitle(agent state.Agent) string {
	workdir := state.Workdir{}
	folder := state.Folder{}
	if w := state.WorkdirForAgent(m.snapshot.State, agent); w != nil {
		workdir = *w
	}
	if f := state.FolderForAgent(m.snapshot.State, agent); f != nil {
		folder = *f
	}
	return titles.RenderAgent(agent, workdir, folder, m.cfg.TitleTemplate)
}

func (m ClientModel) renderAgentBaseTitle(agent state.Agent) string {
	workdir := state.Workdir{}
	folder := state.Folder{}
	if w := state.WorkdirForAgent(m.snapshot.State, agent); w != nil {
		workdir = *w
	}
	if f := state.FolderForAgent(m.snapshot.State, agent); f != nil {
		folder = *f
	}
	return titles.RenderAgent(agent, workdir, folder, titles.TitleTemplate)
}

func (m ClientModel) autoTitleNotice(agent state.Agent, draftTitle string) string {
	return Model{cfg: m.cfg}.autoTitleNotice(agent, draftTitle)
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
