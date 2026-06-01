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
	taskID  string
	session *ptyx.Session
	err     error
}

type titleHookMsg struct {
	taskID string
	title  string
	err    error
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
	promptEditTask       promptKind = "edit-task"
	promptMoveTask       promptKind = "move-task"
)

type confirmKind string

const (
	confirmAddLaunchWorkspace confirmKind = "add-launch-workspace"
	confirmDeleteWorkspace    confirmKind = "delete-workspace"
	confirmDeleteGroup        confirmKind = "delete-group"
	confirmDeleteTask         confirmKind = "delete-task"
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
	groupRowNewTask groupRowKind = "new-task"
	groupRowGroup   groupRowKind = "group"
	groupRowTask    groupRowKind = "task"
)

type groupRow struct {
	kind    groupRowKind
	groupID string
	taskID  string
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
	taskInterrupts    map[string]time.Time
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
	if state.ActiveTask(st) == nil {
		st.ActiveTaskID = ""
		if len(st.Workspaces) == 0 {
			st.Focus = state.FocusWorkspaces
		} else if st.Focus != state.FocusWorkspaces && st.Focus != state.FocusTasks {
			st.Focus = state.FocusTasks
		}
		st.NavOpen = true
	}
	lastNav := st.Focus
	if lastNav == state.FocusConsole || lastNav == "" {
		lastNav = state.FocusTasks
	}
	model := Model{
		cfg: cfg, runtime: rt, store: state.NewStore(rt.StatePath, rt.Workspace), state: st,
		width: 100, height: 32, screens: map[string]*TerminalScreen{}, ptys: map[string]*ptyx.Session{},
		visible:           map[string]bool{},
		codexInputBuffers: map[string][]rune{},
		terminalCommands:  map[string]time.Time{},
		taskInterrupts:    map[string]time.Time{},
		sessionCaptures:   map[string]time.Time{},
		dataCh:            make(chan ptyx.Data, 64),
		ctx:               ctx, cancel: cancel, input: input, lastNavFocus: lastNav,
	}
	model.syncGroupCursor()
	model.navWidth = model.targetNavWidth()
	for _, task := range model.state.Tasks {
		model.startPTY(task.ID)
	}
	_ = model.store.Write(model.state)
	return model
}

func (m *Model) HandleSupervisorRequest(request ipc.Request) (ipc.Response, tea.Cmd) {
	return m.handleIPC(request)
}

func (m *Model) ApplyLaunchWorkspace(path string) {
	m.applyLaunchWorkspace(path)
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
	if m.state.NavOpen && m.state.Focus == state.FocusTasks {
		m.syncGroupCursor()
	}
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
	if active := state.ActiveTask(m.state); active != nil {
		title = m.renderTaskTitle(*active)
	}
	return ipc.Snapshot{
		State:                m.state,
		CodexTitle:           title,
		CodexContent:         content,
		CodexPlainLines:      plainLines,
		CodexScrollback:      scrollbackContent,
		CodexScrollbackLines: scrollbackPlainLines,
		LoadingText:          loadingText,
		LoadingTaskIDs:       m.loadingTaskIDs(),
		Message:              m.message,
		NavWidth:             m.targetNavWidth(),
		GroupCursor:          m.groupCursor,
	}
}

func (m *Model) LiveTaskCount() int {
	count := ipc.RunningTaskCount(&m.state)
	counted := map[string]bool{}
	for _, task := range m.state.Tasks {
		switch task.Status {
		case state.StatusStarting, state.StatusRunning, state.StatusReady, state.StatusSitting, state.StatusShipping:
			counted[task.ID] = true
		}
	}
	for id, pty := range m.ptys {
		if pty == nil || counted[id] || state.TaskByID(m.state, id) == nil {
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
	active := state.ActiveTask(m.state)
	if active == nil || active.Status != state.StatusError {
		return ""
	}
	detail := strings.TrimSpace(active.CodexTitle)
	if detail == "" {
		detail = "unknown error"
	}
	label := taskTypeForTask(m.cfg, *active).Label
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
		if !m.hasLoadingAnimation() {
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
	if active := state.ActiveTask(m.state); active != nil {
		title = m.renderTaskTitle(*active)
	}
	if loadingText != "" {
		return renderWorkspaceView(m.cfg, m.state, title, "", m.width, m.height, m.message, m.navWidth, m.groupCursor, workspaceRenderOptions{
			loadingText:            loadingText,
			loadingFrame:           m.loadingFrame(),
			previewHeaderAnimation: livePreviewAnimationFrame(m.loading),
			loadingTasks:           m.loadingTaskSet(),
		})
	}
	return renderWorkspaceView(m.cfg, m.state, title, content, m.width, m.height, m.message, m.navWidth, m.groupCursor, workspaceRenderOptions{
		loadingFrame:           m.loadingFrame(),
		previewHeaderAnimation: livePreviewAnimationFrame(m.loading),
		loadingTasks:           m.loadingTaskSet(),
	})
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
	return promptContextFor(m.prompt, m.pendingID, m.state, m.selectedTask())
}

func (m Model) renderPromptExtra(input textinput.Model, width int) []string {
	return renderPromptExtraForState(m.cfg, m.state, m.prompt, m.selectedTask(), input, width)
}

func (m Model) renderConfirmModal() string {
	width := max(36, min(m.width-16, 72))
	return renderConfirmPrompt(m.confirm, confirmTarget(m.confirm, m.state, m.pendingID, m.renderTaskTitle), width)
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
	if m.state.Focus == state.FocusConsole && state.ActiveTask(m.state) != nil {
		if active := state.ActiveTask(m.state); active != nil {
			return m, m.applyCodexInput(codexInputArgs(msg))
		}
		return m, nil
	}
	if m.state.Focus == state.FocusConsole {
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
		m.focusNavPane(state.FocusTasks)
	case msg.Type == tea.KeyShiftUp:
		m.reorderSelectedRow(-1)
	case msg.Type == tea.KeyShiftDown:
		m.reorderSelectedRow(1)
	case bindingMatches(m.cfg.KeyBindings.SelectPrev, msg) || msg.Type == tea.KeyUp:
		m.moveSelection(-1)
	case bindingMatches(m.cfg.KeyBindings.SelectNext, msg) || msg.Type == tea.KeyDown:
		m.moveSelection(1)
	case bindingMatches(m.cfg.KeyBindings.NewWorkspace, msg):
		m.startPrompt(promptWorkspace, defaultWorkspacePromptValue(m.state, m.runtime.Workspace))
	case bindingMatches(m.cfg.KeyBindings.NewGroup, msg):
		m.focusNavPane(state.FocusTasks)
		m.startPrompt(promptGroup, "")
	case bindingMatches(m.cfg.KeyBindings.NewTask, msg):
		m.startNewTaskMenu()
	case bindingMatches(m.cfg.KeyBindings.MoveTask, msg):
		if task := m.selectedTask(); task != nil {
			m.startPrompt(promptMoveTask, "")
		}
	case bindingMatches(m.cfg.KeyBindings.Edit, msg):
		m.startEditPrompt()
	case bindingMatches(m.cfg.KeyBindings.Delete, msg):
		m.startDeleteConfirm()
	case bindingMatches(m.cfg.KeyBindings.Open, msg) || msg.Type == tea.KeyEnter:
		if m.state.Focus == state.FocusWorkspaces {
			m.focusNavPane(state.FocusTasks)
			return m, nil
		}
		row := m.currentGroupRow()
		if row.kind == groupRowNewTask {
			m.startNewTaskMenu()
			return m, nil
		}
		if row.kind == groupRowGroup {
			m.toggleSelectedGroup(row.groupID)
			return m, nil
		}
		if task := m.selectedTask(); task != nil {
			m.state.SelectedTaskID = task.ID
			m.state.ActiveTaskID = task.ID
			m.state.SelectedWorkspaceID = task.WorkspaceID
			m.state.SelectedGroupID = task.GroupID
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
	return m, m.newTask("", taskType.ID)
}

func (m *Model) focusNavPane(focus state.Focus) {
	if focus != state.FocusWorkspaces && focus != state.FocusTasks {
		return
	}
	m.state.Focus = focus
	m.state.NavOpen = true
	m.lastNavFocus = focus
	m.save()
}

func (m *Model) rememberCurrentNavFocus() {
	if m.state.Focus == state.FocusWorkspaces || m.state.Focus == state.FocusTasks {
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
			m.state = state.SelectWorkspace(m.state, workspaceIDs[next])
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
	case groupRowNewTask:
		m.state.SelectedTaskID = ""
		m.state.SelectedGroupID = ""
	case groupRowGroup:
		m.state.SelectedTaskID = ""
		m.state.SelectedGroupID = row.groupID
	case groupRowTask:
		if task := state.TaskByID(m.state, row.taskID); task != nil {
			m.state.SelectedTaskID = task.ID
			m.state.SelectedGroupID = task.GroupID
			m.state.ActiveTaskID = task.ID
		}
	}
	m.groupCursorPinned = true
	m.save()
}

func (m *Model) reorderSelectedRow(delta int) {
	if m.state.Focus == state.FocusWorkspaces {
		m.reorderSelectedWorkspace(delta)
		return
	}
	if m.state.Focus != state.FocusTasks {
		return
	}
	row := m.currentGroupRow()
	switch row.kind {
	case groupRowGroup:
		m.reorderSelectedGroup(row.groupID, delta)
	case groupRowTask:
		m.reorderSelectedTask(delta)
	}
}

func (m *Model) reorderSelectedWorkspace(delta int) {
	workspaceID := m.state.SelectedWorkspaceID
	if workspaceID == "" {
		return
	}
	next, moved, err := state.ReorderWorkspace(m.state, workspaceID, delta)
	if err != nil {
		m.message = err.Error()
		return
	}
	if !moved {
		return
	}
	m.state = next
	m.groupCursor = 0
	m.groupCursorPinned = false
	m.save()
}

func (m *Model) reorderSelectedTask(delta int) {
	if m.state.Focus != state.FocusTasks {
		return
	}
	task := m.selectedTask()
	if task == nil {
		return
	}
	taskID := task.ID
	next, moved, err := state.ReorderTask(m.state, task.ID, delta)
	if err != nil {
		m.message = err.Error()
		return
	}
	if !moved {
		return
	}
	m.state = next
	m.syncGroupCursorToTask(taskID)
	m.save()
}

func (m *Model) reorderSelectedGroup(groupID string, delta int) {
	if groupID == "" {
		return
	}
	next, moved, err := state.ReorderGroup(m.state, groupID, delta)
	if err != nil {
		m.message = err.Error()
		return
	}
	if !moved {
		return
	}
	m.state = next
	m.syncGroupCursorToSelectedGroup()
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
	m.focusNavPane(state.FocusTasks)
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
	case promptEditTask:
		next, err := state.RenameTask(m.state, m.pendingID, value)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "renamed task"
		if task := state.TaskByID(m.state, m.pendingID); task != nil && m.taskUsesAutoTitle(*task) {
			m.message = m.autoTitleRenameMessage(*task)
			if strings.TrimSpace(m.cfg.TitleHookCommand) == "" && strings.TrimSpace(task.AutoTitle) == "" {
				m.recordAutoTitleError(task.ID, "title_hook_command is not configured", false)
			}
		}
		m.save()
	case promptMoveTask:
		task := m.selectedTask()
		if task == nil {
			m.message = "select a task first"
			return nil
		}
		groupID := ""
		if value != "" {
			group := m.findGroupByPath(task.WorkspaceID, value)
			if group == nil {
				m.message = "group not found"
				return nil
			}
			groupID = group.ID
		}
		next, err := state.MoveTask(m.state, task.ID, groupID)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		m.state = next
		m.message = "moved task"
		m.syncGroupCursorToTask(task.ID)
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
		next, tasks, err := state.RemoveWorkspace(m.state, m.pendingID)
		if err != nil {
			m.message = err.Error()
			return nil
		}
		for _, task := range tasks {
			m.killTaskPTY(task.ID)
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
	case confirmDeleteTask:
		return m.closeTask(m.pendingID)
	}
	return nil
}

func (m *Model) newTask(title string, typeIDs ...string) tea.Cmd {
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
	next, task, err := state.AddTaskWithType(m.state, shortID(), workspace.ID, "", taskType.ID, title, state.NowISO())
	if err != nil {
		m.message = err.Error()
		return nil
	}
	m.state = next
	m.syncGroupCursorToTask(task.ID)
	m.snapNavWidthToTarget()
	m.save()
	return tea.Batch(m.startPTYCmd(task.ID), m.startNavAnimation(), tickLoading())
}

func (m *Model) closeTask(taskID string) tea.Cmd {
	if taskID == "" {
		return nil
	}
	m.killTaskPTY(taskID)
	m.state = state.CloseTask(m.state, taskID)
	m.syncGroupCursor()
	m.save()
	return m.startNavAnimation()
}

func (m *Model) killTaskPTY(taskID string) {
	if pty := m.ptys[taskID]; pty != nil {
		pty.Kill()
		delete(m.ptys, taskID)
	}
	delete(m.screens, taskID)
	delete(m.visible, taskID)
	delete(m.taskInterrupts, taskID)
	delete(m.sessionCaptures, taskID)
}

func (m *Model) selectedTask() *state.Task {
	return selectedTaskForState(m.state, m.groupCursor)
}

func (m Model) currentGroupRow() groupRow {
	return currentGroupRowForState(m.state, m.groupCursor)
}

func (m Model) groupRows() []groupRow {
	return groupRowsForState(m.state)
}

func (m *Model) toggleSelectedGroup(groupID string) {
	m.state = state.ToggleGroupCollapsed(m.state, groupID)
	m.state.SelectedTaskID = ""
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
	if m.state.SelectedTaskID != "" {
		for index, row := range rows {
			if row.kind == groupRowTask && row.taskID == m.state.SelectedTaskID {
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
	if m.state.ActiveTaskID != "" {
		for index, row := range rows {
			if row.kind == groupRowTask && row.taskID == m.state.ActiveTaskID {
				m.groupCursor = index
				m.groupCursorPinned = true
				return
			}
		}
	}
	m.groupCursor = 0
	m.groupCursorPinned = true
}

func (m *Model) syncGroupCursorToTask(taskID string) {
	rows := m.groupRows()
	if len(rows) == 0 {
		m.groupCursor = 0
		m.groupCursorPinned = false
		return
	}
	if taskID != "" {
		for index, row := range rows {
			if row.kind == groupRowTask && row.taskID == taskID {
				m.groupCursor = index
				m.state.SelectedTaskID = taskID
				if task := state.TaskByID(m.state, taskID); task != nil {
					m.state.SelectedWorkspaceID = task.WorkspaceID
					m.state.SelectedGroupID = task.GroupID
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
				m.state.SelectedTaskID = ""
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
	case groupRowNewTask:
		return state.ActiveWorkspace(m.state) != nil && m.state.SelectedTaskID == "" && m.state.SelectedGroupID == ""
	case groupRowGroup:
		return m.state.SelectedTaskID == "" && row.groupID != "" && row.groupID == m.state.SelectedGroupID
	case groupRowTask:
		if m.state.SelectedTaskID != "" {
			return row.taskID == m.state.SelectedTaskID
		}
		return row.taskID != "" && row.taskID == m.state.ActiveTaskID
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
	if m.state.Focus == state.FocusConsole {
		return m.openNav()
	}
	if state.ActiveTask(m.state) == nil {
		m.state.NavOpen = true
		m.state.Focus = m.lastNavFocus
		m.save()
		return m.startNavAnimation()
	}
	return m.setCodexFocus()
}

func (m *Model) openNav() tea.Cmd {
	m.state.NavOpen = true
	if m.lastNavFocus != state.FocusWorkspaces && m.lastNavFocus != state.FocusTasks {
		m.lastNavFocus = state.FocusTasks
	}
	m.state.Focus = m.lastNavFocus
	m.save()
	return m.startNavAnimation()
}

func (m *Model) setCodexFocus() tea.Cmd {
	if state.ActiveTask(m.state) == nil {
		m.message = "select a task first"
		return nil
	}
	if m.state.Focus == state.FocusWorkspaces || m.state.Focus == state.FocusTasks {
		m.lastNavFocus = m.state.Focus
	}
	m.state.Focus = state.FocusConsole
	m.state.NavOpen = false
	m.save()
	return m.startNavAnimation()
}

func (m *Model) closeWeft() {
	m.message = "closed Weft clients"
}

func (m *Model) captureCodexInput(task state.Task, msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyRunes:
		m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], msg.Runes...)
	case tea.KeySpace:
		m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], ' ')
	case tea.KeyBackspace:
		if msg.Alt {
			m.codexInputBuffers[task.ID] = trimPreviousInputToken(m.codexInputBuffers[task.ID])
		} else {
			m.codexInputBuffers[task.ID] = trimLastRune(m.codexInputBuffers[task.ID])
		}
	case tea.KeyCtrlH:
		if msg.Alt {
			m.codexInputBuffers[task.ID] = trimPreviousInputToken(m.codexInputBuffers[task.ID])
		}
	case tea.KeyEnter:
		firstMessage := strings.TrimSpace(string(m.codexInputBuffers[task.ID]))
		delete(m.codexInputBuffers, task.ID)
		if firstMessage == "" {
			return nil
		}
		m.markCodexInputSubmitted(task.ID)
		if updated := state.TaskByID(m.state, task.ID); updated != nil {
			task = *updated
		}
		if task.AutoTitleAttempted {
			return nil
		}
		if strings.TrimSpace(m.cfg.TitleHookCommand) == "" {
			if m.taskUsesAutoTitle(task) {
				m.recordAutoTitleError(task.ID, "title_hook_command is not configured", false)
				m.message = "auto title unavailable: set title_hook_command"
			}
			return nil
		}
		m.state = state.WithUpdatedTask(m.state, task.ID, func(task state.Task) state.Task {
			task.AutoTitleAttempted = true
			task.AutoTitleError = ""
			return task
		})
		m.save()
		if updated := state.TaskByID(m.state, task.ID); updated != nil {
			task = *updated
		}
		if m.taskUsesAutoTitle(task) {
			m.message = "generating auto title"
		}
		return m.titleHookCmd(task, firstMessage)
	default:
		switch strings.ToLower(msg.String()) {
		case "ctrl+u":
			delete(m.codexInputBuffers, task.ID)
		case "ctrl+w":
			m.codexInputBuffers[task.ID] = trimLastWord(m.codexInputBuffers[task.ID])
		}
	}
	return nil
}

func (m Model) taskUsesAutoTitle(task state.Task) bool {
	return strings.Contains(task.Title, titles.AutoTemplate)
}

func (m *Model) markCodexInputSubmitted(taskID string) {
	m.state = state.WithUpdatedTask(m.state, taskID, func(task state.Task) state.Task {
		task.CodexInputSubmitted = true
		return task
	})
	m.save()
}

func (m Model) titleHookCmd(task state.Task, firstMessage string) tea.Cmd {
	workspace := state.Workspace{}
	if found := state.WorkspaceForTask(m.state, task); found != nil {
		workspace = *found
	}
	group := state.Group{}
	if found := state.GroupForTask(m.state, task); found != nil {
		group = *found
	}
	payload := titlehook.BuildPayload(task, workspace, group, task.Title, firstMessage)
	command := m.cfg.TitleHookCommand
	timeout := time.Duration(m.cfg.TitleHookTimeoutSeconds) * time.Second
	ctx := m.ctx
	return func() tea.Msg {
		title, err := titlehook.Run(ctx, command, workspace.Path, timeout, payload)
		return titleHookMsg{taskID: task.ID, title: title, err: err}
	}
}

func (m *Model) applyTitleHook(msg titleHookMsg) {
	task := state.TaskByID(m.state, msg.taskID)
	if task == nil {
		return
	}
	if msg.err != nil {
		m.recordAutoTitleError(msg.taskID, hookErrorText(msg.err), true)
		m.message = "auto title hook failed: " + hookErrorText(msg.err)
		return
	}
	if strings.TrimSpace(msg.title) == "" {
		m.recordAutoTitleError(msg.taskID, "hook produced no title", true)
		m.message = "auto title hook failed: hook produced no title"
		return
	}
	m.state = state.WithUpdatedTask(m.state, msg.taskID, func(task state.Task) state.Task {
		task.AutoTitle = msg.title
		task.AutoTitleAttempted = true
		task.AutoTitleError = ""
		return task
	})
	if m.taskUsesAutoTitle(*task) {
		m.message = "auto title generated"
	}
	m.save()
}

func (m *Model) recordAutoTitleError(taskID string, message string, attempted bool) {
	m.state = state.WithUpdatedTask(m.state, taskID, func(task state.Task) state.Task {
		task.AutoTitle = ""
		task.AutoTitleAttempted = attempted
		task.AutoTitleError = message
		return task
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

func (m Model) autoTitleRenameMessage(task state.Task) string {
	if strings.TrimSpace(task.AutoTitle) != "" {
		return "renamed task; auto title ready"
	}
	if strings.TrimSpace(task.AutoTitleError) != "" {
		return "renamed task; auto title failed"
	}
	if task.AutoTitleAttempted {
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

func (m *Model) startPTY(taskID string) {
	if m.ptys[taskID] != nil {
		return
	}
	if m.screens[taskID] == nil {
		m.screens[taskID] = NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
	}
	workspace := m.taskWorkspace(taskID)
	ptySession, err := ptyx.Start(m.ctx, taskID, m.taskCommandForTask(taskID), workspace, m.ptyWidth(), m.ptyHeight(), func(data ptyx.Data) {
		m.dataCh <- data
	})
	if err != nil {
		m.state = state.WithUpdatedTask(m.state, taskID, func(task state.Task) state.Task {
			task.Status = state.StatusError
			task.CodexTitle = err.Error()
			return task
		})
		m.save()
		return
	}
	m.ptys[taskID] = ptySession
	m.state = state.WithUpdatedTask(m.state, taskID, func(task state.Task) state.Task {
		if taskUsesCodexIntegration(m.cfg, task) {
			task.Status = state.StatusRunning
		} else {
			task.Status = state.StatusReady
			m.visible[task.ID] = true
		}
		return task
	})
	m.save()
}

func (m *Model) startPTYCmd(taskID string) tea.Cmd {
	if m.ptys[taskID] != nil {
		return nil
	}
	ctx := m.ctx
	command := m.taskCommandForTask(taskID)
	workspace := m.taskWorkspace(taskID)
	cols := m.ptyWidth()
	rows := m.ptyHeight()
	dataCh := m.dataCh
	return func() tea.Msg {
		ptySession, err := ptyx.Start(ctx, taskID, command, workspace, cols, rows, func(data ptyx.Data) {
			dataCh <- data
		})
		return ptyStartedMsg{taskID: taskID, session: ptySession, err: err}
	}
}

func (m *Model) applyPTYStarted(msg ptyStartedMsg) {
	if msg.err != nil {
		m.state = state.WithUpdatedTask(m.state, msg.taskID, func(task state.Task) state.Task {
			task.Status = state.StatusError
			task.CodexTitle = msg.err.Error()
			return task
		})
		m.save()
		return
	}
	if state.TaskByID(m.state, msg.taskID) == nil {
		msg.session.Kill()
		return
	}
	if m.ptys[msg.taskID] != nil {
		msg.session.Kill()
		return
	}
	if m.screens[msg.taskID] == nil {
		m.screens[msg.taskID] = NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
	}
	m.ptys[msg.taskID] = msg.session
	m.state = state.WithUpdatedTask(m.state, msg.taskID, func(task state.Task) state.Task {
		if taskUsesCodexIntegration(m.cfg, task) {
			task.Status = state.StatusRunning
		} else {
			task.Status = state.StatusReady
			m.visible[task.ID] = true
		}
		return task
	})
	m.save()
}

func (m *Model) applyPTYData(data ptyx.Data) {
	if state.TaskByID(m.state, data.TaskID) == nil {
		return
	}
	task := state.TaskByID(m.state, data.TaskID)
	if task == nil {
		return
	}
	usesCodex := taskUsesCodexIntegration(m.cfg, *task)
	if usesCodex && (data.Text != "" || data.Title != "") {
		m.captureCodexSession(data.TaskID)
	}
	if data.Err != nil {
		delete(m.ptys, data.TaskID)
		activeExited := m.state.ActiveTaskID == data.TaskID
		status := state.StatusStopped
		title := taskTypeForTask(m.cfg, *task).Label + " exited"
		if m.recentTaskInterrupt(data.TaskID) {
			status = state.StatusKilled
			title = taskTypeForTask(m.cfg, *task).Label + " killed"
		}
		delete(m.taskInterrupts, data.TaskID)
		m.state = state.WithUpdatedTask(m.state, data.TaskID, func(task state.Task) state.Task {
			if task.Status != state.StatusError {
				task.Status = status
				task.CodexTitle = title
			}
			return task
		})
		if activeExited && m.state.Focus == state.FocusConsole {
			m.state.NavOpen = true
			m.state.Focus = state.FocusTasks
			m.lastNavFocus = state.FocusTasks
			m.syncGroupCursor()
			m.snapNavWidthToTarget()
		}
		m.save()
		return
	}
	screenStatus := ""
	if data.Text != "" {
		screen := m.screens[data.TaskID]
		if screen == nil {
			screen = NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
			m.screens[data.TaskID] = screen
		}
		screen.Write(data.Text)
		if screen.HasVisibleContent() {
			m.visible[data.TaskID] = true
		}
		if usesCodex {
			screenStatus = codexScreenStatus(screen)
		}
	}
	if usesCodex && (data.Title != "" || data.Text != "") {
		title := titles.NormalizeCodexTitle(data.Title)
		if data.Title != "" {
			delete(m.taskInterrupts, data.TaskID)
		}
		m.state = state.WithUpdatedTask(m.state, data.TaskID, func(task state.Task) state.Task {
			if data.Title != "" {
				task.CodexTitle = title
				task.CodexStatus = ""
				task.Status = state.StatusRunning
			}
			switch {
			case screenStatus != "":
				task.CodexStatus = screenStatus
				if !titles.CodexTitleIndicatesActivity(task.CodexTitle) {
					task.Status = state.StatusReady
				}
			case data.Text != "" && task.CodexStatus != "":
				task.CodexStatus = ""
				if task.Status == state.StatusReady && !titles.CodexTitleIndicatesActivity(task.CodexTitle) {
					task.Status = state.StatusRunning
				}
			}
			return task
		})
		m.save()
	}
}

func (m *Model) captureCodexSession(taskID string) {
	task := state.TaskByID(m.state, taskID)
	if task == nil || strings.TrimSpace(task.CodexSessionID) != "" {
		return
	}
	if !taskUsesCodexIntegration(m.cfg, *task) {
		return
	}
	now := time.Now()
	if last, ok := m.sessionCaptures[taskID]; ok && now.Sub(last) < time.Second {
		return
	}
	m.sessionCaptures[taskID] = now
	next, assigned := codexsession.AssignMissingSessionIDs(m.state, m.runtime.Workspace)
	if assigned == 0 {
		return
	}
	m.state = next
	delete(m.sessionCaptures, taskID)
	m.save()
}

func (m Model) taskCommandForTask(taskID string) string {
	task := state.TaskByID(m.state, taskID)
	if task == nil {
		return ""
	}
	taskType := taskTypeForTask(m.cfg, *task)
	command := taskType.Command
	if taskUsesCodexIntegration(m.cfg, *task) && strings.TrimSpace(task.CodexSessionID) != "" {
		command = codexsession.ResumeCommand(command, task.CodexSessionID)
	}
	return command
}

func (m *Model) activeOutput() string {
	active := state.ActiveTask(m.state)
	if active == nil {
		return ""
	}
	if screen := m.screens[active.ID]; screen != nil {
		if !screen.HasVisibleContent() && !m.visible[active.ID] {
			return ""
		}
		return screen.ANSIStringWithCursor(m.state.Focus == state.FocusConsole)
	}
	return ""
}

func (m Model) activePlainLines() []string {
	active := state.ActiveTask(m.state)
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
	active := state.ActiveTask(m.state)
	if active == nil {
		return ""
	}
	if screen := m.screens[active.ID]; screen != nil {
		if !screen.HasVisibleContent() && !m.visible[active.ID] {
			return ""
		}
		return screen.ScrollbackANSIStringWithCursor(m.state.Focus == state.FocusConsole)
	}
	return ""
}

func (m Model) activeScrollbackPlainLines() []string {
	active := state.ActiveTask(m.state)
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
	active := state.ActiveTask(m.state)
	if active == nil {
		return false
	}
	return m.taskLoading(active.ID)
}

func (m Model) anyTaskLoading() bool {
	for _, task := range m.state.Tasks {
		if m.taskLoading(task.ID) {
			return true
		}
	}
	return false
}

func (m Model) loadingTaskIDs() []string {
	ids := make([]string, 0)
	for _, task := range m.state.Tasks {
		if m.taskLoading(task.ID) {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func (m Model) loadingTaskSet() map[string]bool {
	ids := m.loadingTaskIDs()
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func (m Model) taskLoading(taskID string) bool {
	task := state.TaskByID(m.state, taskID)
	if task == nil {
		return false
	}
	if !taskUsesCodexIntegration(m.cfg, *task) {
		return taskStatusShowsLoadingIndicator(*task)
	}
	canonical := titles.CanonicalStatus(*task)
	switch canonical {
	case string(state.StatusError), string(state.StatusStopped), string(state.StatusKilled), string(state.StatusSitting):
		return false
	}
	// Many non-active tasks don't have a captured screen buffer; rely on the Codex title-derived
	// status first so we don't incorrectly show them as still "running"/loading.
	// For the active task, keep the stricter behavior that waits for visible content.
	if taskID != m.state.ActiveTaskID {
		return taskStatusShowsLoadingIndicator(*task)
	}
	if taskStatusShowsLoadingIndicator(*task) {
		return true
	}
	if canonical == "idle" {
		return false
	}
	screen := m.screens[taskID]
	return screen == nil || (!screen.HasVisibleContent() && !m.visible[taskID])
}

func (m *Model) markTerminalCommandStarted(taskID string) {
	task := state.TaskByID(m.state, taskID)
	if task == nil || taskUsesCodexIntegration(m.cfg, *task) {
		return
	}
	if m.terminalCommands == nil {
		m.terminalCommands = map[string]time.Time{}
	}
	m.terminalCommands[taskID] = time.Now()
	m.state = state.WithUpdatedTask(m.state, taskID, func(task state.Task) state.Task {
		switch task.Status {
		case state.StatusError, state.StatusStopped, state.StatusKilled:
			return task
		default:
			task.Status = state.StatusRunning
			return task
		}
	})
	m.save()
}

func (m *Model) refreshTerminalTaskActivity() {
	if len(m.terminalCommands) == 0 {
		return
	}
	changed := false
	for taskID, started := range m.terminalCommands {
		task := state.TaskByID(m.state, taskID)
		if task == nil || taskUsesCodexIntegration(m.cfg, *task) {
			delete(m.terminalCommands, taskID)
			continue
		}
		if task.Status != state.StatusRunning {
			delete(m.terminalCommands, taskID)
			continue
		}
		if time.Since(started) < terminalCommandLoadingFloor {
			continue
		}
		pty := m.ptys[taskID]
		if pty != nil && pty.ForegroundProcessActive() {
			continue
		}
		delete(m.terminalCommands, taskID)
		m.state = state.WithUpdatedTask(m.state, taskID, func(task state.Task) state.Task {
			if task.Status == state.StatusRunning {
				task.Status = state.StatusReady
			}
			return task
		})
		changed = true
	}
	if changed {
		m.save()
	}
}

func (m Model) recentTaskInterrupt(taskID string) bool {
	if m.taskInterrupts == nil {
		return false
	}
	sentAt, ok := m.taskInterrupts[taskID]
	return ok && time.Since(sentAt) <= 5*time.Second
}

func (m *Model) recordTaskInterrupt(taskID string) {
	if m.taskInterrupts == nil {
		m.taskInterrupts = map[string]time.Time{}
	}
	m.taskInterrupts[taskID] = time.Now()
}

func (m Model) loadingFrame() string {
	return loadingFrames[m.loading%len(loadingFrames)]
}

func (m Model) loadingLabel() string {
	label := "task"
	if active := state.ActiveTask(m.state); active != nil {
		label = taskTypeForTask(m.cfg, *active).Label
	}
	return m.loadingFrame() + " Starting " + label
}

func (m Model) hasLoadingAnimation() bool {
	return m.anyTaskLoading() || m.state.NavOpen && state.ActiveTask(m.state) != nil
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
	for taskID, screen := range m.screens {
		task := state.TaskByID(m.state, taskID)
		if task != nil && !taskUsesCodexIntegration(m.cfg, *task) {
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
	if m.state.Focus == state.FocusConsole && state.ActiveTask(m.state) != nil {
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
	case "select_new_task":
		if state.ActiveWorkspace(m.state) == nil {
			return ipc.ErrorResponse("workspace_required", "add a workspace first"), nil
		}
		m.focusNavPane(state.FocusTasks)
		m.groupCursor = 0
		m.applyGroupCursor(m.currentGroupRow())
		m.snapNavWidthToTarget()
		return m.ipcResponse("selection updated"), m.startNavAnimation()
	case "reorder_task":
		delta, err := strconv.Atoi(request.Args["delta"])
		if err != nil || delta == 0 {
			return ipc.ErrorResponse("invalid_delta", "delta must be a non-zero integer"), nil
		}
		id := request.Args["id"]
		if id == "" {
			if task := m.selectedTask(); task != nil {
				id = task.ID
			}
		}
		if id == "" {
			return ipc.ErrorResponse("task_not_found", "task not found"), nil
		}
		next, moved, err := state.ReorderTask(m.state, id, delta)
		if err != nil {
			return ipcError("reorder_task_failed", err), nil
		}
		if moved {
			m.state = next
			m.syncGroupCursorToTask(id)
			m.save()
		}
		return m.ipcResponse("reordered task"), nil
	case "reorder_group":
		delta, err := strconv.Atoi(request.Args["delta"])
		if err != nil || delta == 0 {
			return ipc.ErrorResponse("invalid_delta", "delta must be a non-zero integer"), nil
		}
		id := request.Args["id"]
		if id == "" {
			row := m.currentGroupRow()
			if row.kind == groupRowGroup {
				id = row.groupID
			}
		}
		if id == "" {
			return ipc.ErrorResponse("group_not_found", "group not found"), nil
		}
		next, moved, err := state.ReorderGroup(m.state, id, delta)
		if err != nil {
			return ipcError("reorder_group_failed", err), nil
		}
		if moved {
			m.state = next
			m.syncGroupCursorToSelectedGroup()
			m.save()
		}
		return m.ipcResponse("reordered group"), nil
	case "reorder_workspace":
		delta, err := strconv.Atoi(request.Args["delta"])
		if err != nil || delta == 0 {
			return ipc.ErrorResponse("invalid_delta", "delta must be a non-zero integer"), nil
		}
		id := request.Args["id"]
		if id == "" {
			id = m.state.SelectedWorkspaceID
		}
		if id == "" {
			return ipc.ErrorResponse("workspace_not_found", "workspace not found"), nil
		}
		next, moved, err := state.ReorderWorkspace(m.state, id, delta)
		if err != nil {
			return ipcError("reorder_workspace_failed", err), nil
		}
		if moved {
			m.state = next
			m.groupCursor = 0
			m.groupCursorPinned = false
			m.save()
		}
		return m.ipcResponse("reordered workspace"), nil
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
		cmd := m.newTask(title, typeID)
		m.snapNavWidthToTarget()
		return m.ipcResponse("created task"), cmd
	case "rename":
		id := request.Args["id"]
		if id == "" {
			id = m.state.ActiveTaskID
		}
		title := strings.TrimSpace(request.Args["title"])
		if title == "" {
			return ipc.Response{OK: false, Message: "title is required"}, nil
		}
		next, err := state.RenameTask(m.state, id, title)
		if err != nil {
			return ipcError("rename_task_failed", err), nil
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
			id = m.state.ActiveTaskID
		}
		cmd := m.closeTask(id)
		m.snapNavWidthToTarget()
		return m.ipcResponse("closed task"), cmd
	case "remove_workspace":
		next, tasks, err := state.RemoveWorkspace(m.state, request.Args["id"])
		if err != nil {
			return ipcError("remove_workspace_failed", err), nil
		}
		for _, task := range tasks {
			m.killTaskPTY(task.ID)
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
		if task := state.TaskByID(m.state, id); task != nil {
			m.state.ActiveTaskID = id
			m.state.SelectedWorkspaceID = task.WorkspaceID
			m.state.SelectedGroupID = task.GroupID
			m.syncGroupCursorToTask(id)
			m.save()
			return m.ipcResponse("selected task"), nil
		}
		return ipc.Response{OK: false, Message: "task not found"}, nil
	case "move":
		id := request.Args["id"]
		if id == "" {
			id = m.state.ActiveTaskID
		}
		task := state.TaskByID(m.state, id)
		if task == nil {
			return ipc.ErrorResponse("task_not_found", "task not found"), nil
		}
		groupID, ok := m.destinationGroupIDForMove(*task, request.Args)
		if !ok {
			return ipc.ErrorResponse("group_not_found", "group not found"), nil
		}
		next, err := state.MoveTask(m.state, task.ID, groupID)
		if err != nil {
			return ipcError("move_task_failed", err), nil
		}
		m.state = next
		m.syncGroupCursorToTask(task.ID)
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
		if target == "tasks" {
			target = state.FocusTasks
		}
		switch target {
		case state.FocusWorkspaces, state.FocusTasks:
			m.focusNavPane(target)
			m.snapNavWidthToTarget()
			return m.ipcResponse("focus updated"), m.startNavAnimation()
		case state.FocusConsole:
			cmd := m.setCodexFocus()
			m.snapNavWidthToTarget()
			return m.ipcResponse("focus updated"), cmd
		default:
			return ipc.Response{OK: false, Message: "focus target must be workspaces, tasks, or console"}, nil
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
		m.focusNavPane(state.FocusTasks)
		return nil
	}
	row := m.currentGroupRow()
	if row.kind == groupRowNewTask {
		return nil
	}
	if row.kind == groupRowGroup {
		m.toggleSelectedGroup(row.groupID)
		return nil
	}
	if task := m.selectedTask(); task != nil {
		m.state.SelectedTaskID = task.ID
		m.state.ActiveTaskID = task.ID
		m.state.SelectedWorkspaceID = task.WorkspaceID
		m.state.SelectedGroupID = task.GroupID
		m.save()
		return m.setCodexFocus()
	}
	return nil
}

func (m *Model) applyCodexInput(args map[string]string) tea.Cmd {
	if m.state.Focus != state.FocusConsole {
		return nil
	}
	active := state.ActiveTask(m.state)
	if active == nil {
		return nil
	}
	if !taskUsesCodexIntegration(m.cfg, *active) {
		return m.applyTaskInput(args)
	}
	args = routeCodexInputArgs(*active, args)
	if pty := m.ptys[active.ID]; pty != nil {
		if args["input"] == "ctrl+c" {
			m.recordTaskInterrupt(active.ID)
		}
		_ = pty.Write([]byte(args["encoded"]))
	}
	return m.captureCodexInputArgs(*active, args)
}

func (m *Model) applyTaskInput(args map[string]string) tea.Cmd {
	if m.state.Focus != state.FocusConsole {
		return nil
	}
	active := state.ActiveTask(m.state)
	if active == nil {
		return nil
	}
	if taskUsesCodexIntegration(m.cfg, *active) {
		return m.applyCodexInput(args)
	}
	encoded := []byte(args["encoded"])
	if pty := m.ptys[active.ID]; pty != nil {
		_ = pty.Write(encoded)
	}
	return m.captureRawTerminalInput(*active, encoded)
}

func (m *Model) clearActiveTerminal() {
	if m.state.Focus != state.FocusConsole {
		return
	}
	active := state.ActiveTask(m.state)
	if active == nil || taskUsesCodexIntegration(m.cfg, *active) {
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

func (m *Model) captureCodexInputArgs(task state.Task, args map[string]string) tea.Cmd {
	switch args["input"] {
	case "text":
		m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], []rune(args["text"])...)
	case "space":
		m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], ' ')
	case "backspace":
		m.codexInputBuffers[task.ID] = trimLastRune(m.codexInputBuffers[task.ID])
	case codexInputShiftEnter:
		m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], '\n')
	case "alt+backspace":
		m.codexInputBuffers[task.ID] = trimPreviousInputToken(m.codexInputBuffers[task.ID])
	case "enter":
		return m.submitCodexInputBuffer(task)
	case "ctrl+u":
		delete(m.codexInputBuffers, task.ID)
	case "ctrl+w":
		m.codexInputBuffers[task.ID] = trimLastWord(m.codexInputBuffers[task.ID])
	case codexInputRaw:
		return m.captureRawCodexInput(task, []byte(args["encoded"]))
	}
	return nil
}

func (m *Model) submitCodexInputBuffer(task state.Task) tea.Cmd {
	firstMessage := strings.TrimSpace(string(m.codexInputBuffers[task.ID]))
	delete(m.codexInputBuffers, task.ID)
	if firstMessage == "" {
		return nil
	}
	m.markCodexInputSubmitted(task.ID)
	if updated := state.TaskByID(m.state, task.ID); updated != nil {
		task = *updated
	}
	if task.AutoTitleAttempted {
		return nil
	}
	if strings.TrimSpace(m.cfg.TitleHookCommand) == "" {
		if m.taskUsesAutoTitle(task) {
			m.recordAutoTitleError(task.ID, "title_hook_command is not configured", false)
			m.message = "auto title unavailable: set title_hook_command"
		}
		return nil
	}
	m.state = state.WithUpdatedTask(m.state, task.ID, func(task state.Task) state.Task {
		task.AutoTitleAttempted = true
		task.AutoTitleError = ""
		return task
	})
	m.save()
	if updated := state.TaskByID(m.state, task.ID); updated != nil {
		task = *updated
	}
	if m.taskUsesAutoTitle(task) {
		m.message = "generating auto title"
	}
	return m.titleHookCmd(task, firstMessage)
}

func (m *Model) captureRawCodexInput(task state.Task, data []byte) tea.Cmd {
	var cmd tea.Cmd
	for index := 0; index < len(data); {
		switch data[index] {
		case '\r', '\n':
			if cmd == nil {
				cmd = m.submitCodexInputBuffer(task)
				if cmd != nil {
					task.AutoTitleAttempted = true
				}
			} else {
				delete(m.codexInputBuffers, task.ID)
			}
			index++
		case 0x7f, '\b':
			m.codexInputBuffers[task.ID] = trimLastRune(m.codexInputBuffers[task.ID])
			index++
		case 0x15:
			delete(m.codexInputBuffers, task.ID)
			index++
		case 0x17:
			m.codexInputBuffers[task.ID] = trimLastWord(m.codexInputBuffers[task.ID])
			index++
		case 0x1b:
			if sequence, width, ok := consumeCSISequence(data[index:]); ok {
				if event, ok := parseCSIKeyboardEvent(sequence); ok {
					if event.isShiftEnter() {
						m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], '\n')
					} else if key, ok := event.keyMsg(); ok {
						next := m.captureCodexInput(task, key)
						if cmd == nil && next != nil {
							cmd = next
							task.AutoTitleAttempted = true
						}
					}
				}
				index += width
				continue
			}
			if index+1 < len(data) && (data[index+1] == 0x7f || data[index+1] == '\b') {
				m.codexInputBuffers[task.ID] = trimPreviousInputToken(m.codexInputBuffers[task.ID])
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
				m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], r)
			}
			index += width
		}
	}
	return cmd
}

func (m *Model) captureRawTerminalInput(task state.Task, data []byte) tea.Cmd {
	var cmd tea.Cmd
	started := false
	for index := 0; index < len(data); {
		switch data[index] {
		case '\r', '\n':
			if m.submitTerminalInputBuffer(task) {
				started = true
				if m.taskUsesAutoTitle(task) && cmd == nil {
					cmd = m.submitCodexInputBuffer(task)
					if cmd != nil {
						task.AutoTitleAttempted = true
					}
				} else {
					delete(m.codexInputBuffers, task.ID)
				}
			} else {
				delete(m.codexInputBuffers, task.ID)
			}
			index++
		case 0x7f, '\b':
			m.codexInputBuffers[task.ID] = trimLastRune(m.codexInputBuffers[task.ID])
			index++
		case 0x15:
			delete(m.codexInputBuffers, task.ID)
			index++
		case 0x17:
			m.codexInputBuffers[task.ID] = trimLastWord(m.codexInputBuffers[task.ID])
			index++
		case 0x1b:
			if sequence, width, ok := consumeCSISequence(data[index:]); ok {
				if event, ok := parseCSIKeyboardEvent(sequence); ok {
					if event.isShiftEnter() {
						m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], '\n')
					} else if key, ok := event.keyMsg(); ok {
						m.captureTerminalKey(task, key)
					}
				}
				index += width
				continue
			}
			if index+1 < len(data) && (data[index+1] == 0x7f || data[index+1] == '\b') {
				m.codexInputBuffers[task.ID] = trimPreviousInputToken(m.codexInputBuffers[task.ID])
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
				m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], r)
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

func (m *Model) submitTerminalInputBuffer(task state.Task) bool {
	command := strings.TrimSpace(string(m.codexInputBuffers[task.ID]))
	if command == "" {
		return false
	}
	m.markTerminalCommandStarted(task.ID)
	return true
}

func (m *Model) captureTerminalKey(task state.Task, msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyRunes:
		m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], msg.Runes...)
	case tea.KeySpace:
		m.codexInputBuffers[task.ID] = append(m.codexInputBuffers[task.ID], ' ')
	case tea.KeyBackspace:
		m.codexInputBuffers[task.ID] = trimLastRune(m.codexInputBuffers[task.ID])
	case tea.KeyCtrlU:
		delete(m.codexInputBuffers, task.ID)
	case tea.KeyCtrlW:
		m.codexInputBuffers[task.ID] = trimLastWord(m.codexInputBuffers[task.ID])
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

func (m Model) destinationGroupIDForMove(task state.Task, args map[string]string) (string, bool) {
	if value, ok := args["ungrouped"]; ok && strings.EqualFold(value, "true") {
		return "", true
	}
	if groupID, ok := args["group_id"]; ok {
		if groupID == "" {
			return "", true
		}
		group := state.GroupByID(m.state, groupID)
		if group != nil && group.WorkspaceID == task.WorkspaceID {
			return group.ID, true
		}
		return "", false
	}
	if groupPath, ok := args["group"]; ok {
		if strings.TrimSpace(groupPath) == "" {
			return "", true
		}
		group := m.findGroupByPath(task.WorkspaceID, groupPath)
		if group == nil {
			return "", false
		}
		return group.ID, true
	}
	groups := state.GroupsForWorkspace(m.state, task.WorkspaceID)
	current := 0
	groupIDs := []string{""}
	for index, group := range groups {
		groupIDs = append(groupIDs, group.ID)
		if group.ID == task.GroupID {
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

func (m Model) taskWorkspace(taskID string) string {
	task := state.TaskByID(m.state, taskID)
	if task == nil {
		return m.runtime.Workspace
	}
	if workspace := state.WorkspaceForTask(m.state, *task); workspace != nil {
		return workspace.Path
	}
	return m.runtime.Workspace
}

func (m Model) renderTaskTitle(task state.Task) string {
	return renderTaskTitleForState(m.cfg, m.state, task)
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
	fmt.Fprintf(&builder, "tasks: %d\n", len(m.state.Tasks))
	for _, task := range m.state.Tasks {
		marker := " "
		if task.ID == m.state.ActiveTaskID {
			marker = "*"
		}
		group := ""
		if f := state.GroupForTask(m.state, task); f != nil {
			group = f.Path
		}
		workspace := ""
		if w := state.WorkspaceForTask(m.state, task); w != nil {
			workspace = w.Path
		}
		fmt.Fprintf(&builder, "%s %s %s %s %s %s\n", marker, task.ID, group, task.Status, m.renderTaskTitle(task), workspace)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func displayFocus(focus state.Focus) string {
	if focus == state.FocusWorkspaces {
		return "workspaces"
	}
	if focus == state.FocusTasks {
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
		fmt.Sprintf("%s new task", cfg.KeyBindings.NewTask),
		fmt.Sprintf("%s move task", cfg.KeyBindings.MoveTask),
		"Shift+Up/Down reorder selected workspace, task, or group",
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
