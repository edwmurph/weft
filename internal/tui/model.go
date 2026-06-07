package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/edwmurph/weft/internal/codexsession"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/navigation"
	"github.com/edwmurph/weft/internal/ptyx"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/taskcontext"
	"github.com/edwmurph/weft/internal/tasktypes"
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
	modeCommand mode = "command"
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
	confirmScheduleUpgrade    confirmKind = "schedule-upgrade"
)

const (
	loadingInterval             = 90 * time.Millisecond
	terminalCommandLoadingFloor = 250 * time.Millisecond
)

type loadingTick struct{}

var loadingFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type taskOperationStart struct {
	startedAt         time.Time
	allowReady        bool
	completedDuration time.Duration
}

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
	message  string
	navWidth int

	screens           map[string]*TerminalScreen
	ptys              map[string]*ptyx.Session
	visible           map[string]bool
	codexInputBuffers map[string][]rune
	terminalCommands  map[string]time.Time
	operationStarts   *sync.Map
	taskInterrupts    map[string]time.Time
	sessionCaptures   map[string]time.Time
	taskContextStore  *taskcontext.Store
	taskContexts      map[string]taskcontext.Record
	taskContextErr    error
	dataCh            chan ptyx.Data
	ctx               context.Context
	cancel            context.CancelFunc

	groupCursor       int
	groupCursorPinned bool
	lastNavFocus      state.Focus
}

func NewModel(rt config.Runtime, cfg config.Config, st state.State) Model {
	ctx, cancel := context.WithCancel(context.Background())
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
		cfg: cfg, runtime: rt, store: state.NewStore(rt.StatePath), state: st,
		width: 100, height: 32, screens: map[string]*TerminalScreen{}, ptys: map[string]*ptyx.Session{},
		visible:           map[string]bool{},
		codexInputBuffers: map[string][]rune{},
		terminalCommands:  map[string]time.Time{},
		operationStarts:   &sync.Map{},
		taskInterrupts:    map[string]time.Time{},
		sessionCaptures:   map[string]time.Time{},
		taskContextStore:  taskcontext.NewStore(rt.Dir),
		taskContexts:      map[string]taskcontext.Record{},
		dataCh:            make(chan ptyx.Data, 64),
		ctx:               ctx, cancel: cancel, lastNavFocus: lastNav,
	}
	model.syncGroupCursor()
	model.navWidth = model.targetNavWidth()
	model.restoreTerminalUpgradeSnapshots()
	model.loadTaskContexts()
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
	m.syncTaskOperationStartsWithStatuses()
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
		State:                       m.state,
		LiveTitle:                   title,
		ActiveTaskContext:           m.activeTaskContextForSnapshot(),
		CodexContent:                content,
		CodexPlainLines:             plainLines,
		CodexScrollback:             scrollbackContent,
		CodexScrollbackLines:        scrollbackPlainLines,
		ActiveTaskInAlternateScreen: m.activeTaskInAlternateScreen(),
		LoadingText:                 loadingText,
		LoadingTaskIDs:              m.loadingTaskIDs(),
		TerminalForegroundTaskIDs:   m.terminalForegroundTaskIDs(),
		TaskOperationStartedAt:      m.taskOperationStartedAtForSnapshot(),
		TaskOperationDurations:      m.taskOperationDurationsForSnapshot(),
		Message:                     m.message,
		NavWidth:                    m.targetNavWidth(),
		GroupCursor:                 m.groupCursor,
	}
}

func (m Model) activeTaskInAlternateScreen() bool {
	active := state.ActiveTask(m.state)
	if active == nil {
		return false
	}
	screen := m.screens[active.ID]
	return screen != nil && screen.InAlternateScreen()
}

func (m *Model) loadTaskContexts() {
	if m.taskContextStore == nil {
		m.taskContextStore = taskcontext.NewStore(m.runtime.Dir)
	}
	records, err := m.taskContextStore.Load()
	if err != nil {
		m.taskContextErr = err
		m.taskContexts = map[string]taskcontext.Record{}
		return
	}
	m.taskContexts = records
	if _, err := m.taskContextStore.Cleanup(taskIDSet(m.state)); err != nil {
		m.taskContextErr = err
		return
	}
	records, err = m.taskContextStore.Load()
	if err != nil {
		m.taskContextErr = err
		return
	}
	m.taskContexts = records
	m.taskContextErr = nil
}

func taskIDSet(st state.State) map[string]bool {
	ids := make(map[string]bool, len(st.Tasks))
	for _, task := range st.Tasks {
		ids[task.ID] = true
	}
	return ids
}

func (m Model) activeTaskContextForSnapshot() *ipc.TaskContext {
	if !m.cfg.TaskContext.Enabled {
		return nil
	}
	active := state.ActiveTask(m.state)
	if active == nil || !taskIsCodex(m.cfg, *active) {
		return nil
	}
	record, ok := m.taskContexts[active.ID]
	if !ok {
		return nil
	}
	return taskContextRecordToIPC(record)
}

func taskContextRecordToIPC(record taskcontext.Record) *ipc.TaskContext {
	return &ipc.TaskContext{
		TaskID:    record.TaskID,
		Heading:   record.Heading,
		Detail:    record.Detail,
		Summary:   record.Summary(),
		UpdatedAt: record.UpdatedAt,
	}
}

func (m *Model) PrepareUpgradeResume() codexsession.Report {
	next, report := codexsession.PrepareResumeState(m.state, m.runtime.Workspace)
	assigned := report.Assigned
	if report.Assigned > 0 {
		m.state = next
		m.save()
	}
	m.refreshTerminalTaskActivity()
	report = codexsession.BuildUpgradeReport(m.state, m.cfg, m.terminalForegroundProcessActive)
	report.Assigned = assigned
	return report
}

func (m Model) activeErrorText() string {
	active := state.ActiveTask(m.state)
	if active == nil || active.Status != state.StatusError {
		return ""
	}
	detail := strings.TrimSpace(active.LiveTitle)
	if detail == "" {
		detail = "unknown error"
	}
	label := taskTypeForTask(m.cfg, *active).Label
	if strings.TrimSpace(label) == "" {
		label = "Task"
	}
	return label + " failed to start:\n" + detail
}

func (m *Model) focusNavPane(focus state.Focus) {
	if focus != state.FocusWorkspaces && focus != state.FocusTasks {
		return
	}
	m.state.Focus = focus
	m.state.NavOpen = true
	m.lastNavFocus = focus
	if focus == state.FocusTasks {
		if !m.groupCursorPinned {
			m.restoreActiveTaskSelectionForSelectedWorkspace()
		}
		m.syncGroupCursor()
	}
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
			m.restoreActiveTaskSelectionForSelectedWorkspace()
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

func (m *Model) newTaskWithSilent(title string, silent bool, typeIDs ...string) tea.Cmd {
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
	next, task, err := state.AddTaskWithTypeAndSilent(m.state, shortID(), workspace.ID, "", taskType.ID, title, state.NowISO(), silent)
	if err != nil {
		m.message = err.Error()
		return nil
	}
	m.state = next
	m.syncGroupCursorToTask(task.ID)
	m.snapNavWidthToTarget()
	m.save()
	return tea.Batch(m.startPTYCmd(task.ID), tickLoading())
}

func (m *Model) closeTask(taskID string) tea.Cmd {
	if taskID == "" {
		return nil
	}
	m.killTaskPTY(taskID)
	m.clearTaskContextRecord(taskID)
	m.state = state.CloseTask(m.state, taskID)
	m.syncGroupCursor()
	m.save()
	return nil
}

func (m *Model) killTaskPTY(taskID string) {
	if pty := m.ptys[taskID]; pty != nil {
		pty.Kill()
		delete(m.ptys, taskID)
	}
	delete(m.screens, taskID)
	delete(m.visible, taskID)
	delete(m.terminalCommands, taskID)
	m.clearTaskOperationStarted(taskID)
	delete(m.taskInterrupts, taskID)
	delete(m.sessionCaptures, taskID)
}

func (m *Model) killFocusedTerminalTask(taskID string) tea.Cmd {
	task := state.TaskByID(m.state, taskID)
	if task == nil {
		return nil
	}
	if pty := m.ptys[taskID]; pty != nil {
		pty.Kill()
		delete(m.ptys, taskID)
	}
	delete(m.terminalCommands, taskID)
	m.clearTaskOperationStarted(taskID)
	delete(m.codexInputBuffers, taskID)
	delete(m.taskInterrupts, taskID)
	delete(m.sessionCaptures, taskID)
	title := taskTypeForTask(m.cfg, *task).Label + " killed"
	m.state = state.WithUpdatedTask(m.state, taskID, func(task state.Task) state.Task {
		if task.Status != state.StatusError {
			task.Status = state.StatusKilled
			task.LiveTitle = title
		}
		return task
	})
	if m.state.ActiveTaskID == taskID && m.state.Focus == state.FocusConsole {
		m.state.NavOpen = true
		m.state.Focus = state.FocusTasks
		m.lastNavFocus = state.FocusTasks
		m.syncGroupCursor()
		m.snapNavWidthToTarget()
	}
	m.save()
	return nil
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

func (m *Model) restoreActiveTaskSelectionForSelectedWorkspace() {
	active := state.ActiveTask(m.state)
	if active == nil || active.WorkspaceID != m.state.SelectedWorkspaceID {
		return
	}
	m.state.SelectedTaskID = active.ID
	m.state.SelectedGroupID = active.GroupID
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
		return nil
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
	return nil
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
	return nil
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
		m.markInputSubmitted(task.ID)
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

func (m *Model) markInputSubmitted(taskID string) {
	if task := state.TaskByID(m.state, taskID); task != nil && taskInputModeForTask(m.cfg, *task) == tasktypes.InputModeCodex {
		m.markTaskOperationStarted(taskID, true)
	}
	m.state = state.WithUpdatedTask(m.state, taskID, func(task state.Task) state.Task {
		task.InputSubmitted = true
		if taskInputModeForTask(m.cfg, task) == tasktypes.InputModeCodex {
			task.LiveStatus = string(state.StatusRunning)
			task.Status = state.StatusRunning
			task.UpdatedAt = state.NowISO()
		}
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
	payload := titlehook.BuildPayload(
		task,
		workspace,
		group,
		task.Title,
		firstMessage,
		taskTitleColumnWidth(m.cfg, m.state, task, m.width),
		autoTitleMaxColumns(m.cfg, m.state, task, m.width),
	)
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
	return strings.Join(strings.Fields(err.Error()), " ")
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
	if task := state.TaskByID(m.state, taskID); task != nil {
		if taskDefinitionForTask(m.cfg, *task).StartPolicy().TrackOperation {
			m.ensureTaskOperationStarted(taskID)
		}
	}
	if m.screens[taskID] == nil {
		m.screens[taskID] = NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
	}
	workspace := m.taskWorkspace(taskID)
	ptySession, err := ptyx.StartWithOptions(
		m.ctx,
		taskID,
		m.taskCommandForTask(taskID),
		workspace,
		m.ptyWidth(),
		m.ptyHeight(),
		ptyx.StartOptions{Env: m.taskEnvForTask(taskID)},
		func(data ptyx.Data) {
			m.dataCh <- data
		},
	)
	if err != nil {
		m.state = state.WithUpdatedTask(m.state, taskID, func(task state.Task) state.Task {
			task.Status = state.StatusError
			task.LiveTitle = err.Error()
			return task
		})
		m.clearTaskOperationStarted(taskID)
		m.save()
		return
	}
	m.ptys[taskID] = ptySession
	m.state = state.WithUpdatedTask(m.state, taskID, func(task state.Task) state.Task {
		policy := taskDefinitionForTask(m.cfg, task).StartPolicy()
		task.Status = policy.Status
		if policy.Visible {
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
	if task := state.TaskByID(m.state, taskID); task != nil {
		if taskDefinitionForTask(m.cfg, *task).StartPolicy().TrackOperation {
			m.ensureTaskOperationStarted(taskID)
		}
	}
	ctx := m.ctx
	command := m.taskCommandForTask(taskID)
	env := m.taskEnvForTask(taskID)
	workspace := m.taskWorkspace(taskID)
	cols := m.ptyWidth()
	rows := m.ptyHeight()
	dataCh := m.dataCh
	return func() tea.Msg {
		ptySession, err := ptyx.StartWithOptions(
			ctx,
			taskID,
			command,
			workspace,
			cols,
			rows,
			ptyx.StartOptions{Env: env},
			func(data ptyx.Data) {
				dataCh <- data
			},
		)
		return ptyStartedMsg{taskID: taskID, session: ptySession, err: err}
	}
}

func (m *Model) applyPTYStarted(msg ptyStartedMsg) {
	if msg.err != nil {
		m.state = state.WithUpdatedTask(m.state, msg.taskID, func(task state.Task) state.Task {
			task.Status = state.StatusError
			task.LiveTitle = msg.err.Error()
			return task
		})
		m.clearTaskOperationStarted(msg.taskID)
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
		policy := taskDefinitionForTask(m.cfg, task).StartPolicy()
		if policy.TrackOperation {
			m.ensureTaskOperationStarted(task.ID)
		}
		task.Status = policy.Status
		if policy.Visible {
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
	definition := taskDefinitionForTask(m.cfg, *task)
	if definition.TracksSessions() && (data.Text != "" || data.Title != "") {
		m.captureCodexSession(data.TaskID)
	}
	if data.Err != nil {
		delete(m.ptys, data.TaskID)
		m.clearTaskOperationStarted(data.TaskID)
		activeExited := m.state.ActiveTaskID == data.TaskID
		status := state.StatusStopped
		title := taskTypeForTask(m.cfg, *task).Label + " exited"
		if task.Status == state.StatusKilled {
			status = state.StatusKilled
			title = task.LiveTitle
			if strings.TrimSpace(title) == "" {
				title = taskTypeForTask(m.cfg, *task).Label + " killed"
			}
		} else if m.recentTaskInterrupt(data.TaskID) {
			status = state.StatusKilled
			title = taskTypeForTask(m.cfg, *task).Label + " killed"
		}
		delete(m.taskInterrupts, data.TaskID)
		m.state = state.WithUpdatedTask(m.state, data.TaskID, func(task state.Task) state.Task {
			if task.Status != state.StatusError {
				task.Status = status
				task.LiveTitle = title
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
		screenStatus = definition.ScreenStatus(screen.String())
		if definition.TracksTerminalCWD() {
			if cwd := screen.LastCWD(); cwd != "" && cwd != task.TerminalCWD {
				m.state = state.WithUpdatedTask(m.state, data.TaskID, func(task state.Task) state.Task {
					task.TerminalCWD = cwd
					task.UpdatedAt = state.NowISO()
					return task
				})
				m.save()
			}
		}
	}
	if data.Title != "" || data.Text != "" {
		if data.Title != "" {
			delete(m.taskInterrupts, data.TaskID)
		}
		m.state = state.WithUpdatedTask(m.state, data.TaskID, func(task state.Task) state.Task {
			return definition.ApplyPTYTitle(task, data.Title, screenStatus)
		})
		if definition.InputMode() == tasktypes.InputModeCodex && m.codexOperationComplete(data.TaskID, screenStatus, data.Title) {
			m.completeTaskOperationStarted(data.TaskID)
		}
		m.save()
	}
}

func (m *Model) captureCodexSession(taskID string) {
	task := state.TaskByID(m.state, taskID)
	if task == nil || strings.TrimSpace(task.ResumeID) != "" {
		return
	}
	if !taskDefinitionForTask(m.cfg, *task).TracksSessions() {
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
	return taskDefinitionForTask(m.cfg, *task).Command(taskType.Command, *task)
}

func (m Model) taskEnvForTask(taskID string) map[string]string {
	task := state.TaskByID(m.state, taskID)
	if task == nil || !taskIsCodex(m.cfg, *task) {
		return nil
	}
	return map[string]string{
		config.AppDirEnv:    m.runtime.Dir,
		"WEFT_TASK_ID":      task.ID,
		"WEFT_TASK_TYPE_ID": state.TaskTypeID(*task),
		"WEFT_TASK_KIND":    tasktypes.KindCodex,
	}
}

func (m *Model) activeOutput() string {
	return m.activeTerminalANSI((*TerminalScreen).ANSIStringWithCursor)
}

func (m Model) activePlainLines() []string {
	return m.activeTerminalPlainLines((*TerminalScreen).PlainLines)
}

func (m *Model) activeScrollbackOutput() string {
	return m.activeTerminalANSI((*TerminalScreen).ScrollbackANSIStringWithCursor)
}

func (m Model) activeScrollbackPlainLines() []string {
	return m.activeTerminalPlainLines((*TerminalScreen).ScrollbackPlainLines)
}

func (m Model) activeScreenFooter() (*TerminalScreen, string, bool) {
	active := state.ActiveTask(m.state)
	if active == nil {
		return nil, "", false
	}
	return m.screens[active.ID], m.terminalExitFooter(*active), m.visible[active.ID]
}

func (m Model) terminalExitFooter(task state.Task) string {
	if !taskDefinitionForTask(m.cfg, task).ShowsExitFooter() {
		return ""
	}
	switch task.Status {
	case state.StatusKilled, state.StatusStopped:
	default:
		return ""
	}
	title := strings.TrimSpace(task.LiveTitle)
	if title == "" {
		title = taskTypeForTask(m.cfg, task).Label + " exited"
	}
	return title + "\n\nProcess exited."
}

func (m Model) activeTerminalANSI(render func(*TerminalScreen, bool) string) string {
	screen, footer, visible := m.activeScreenFooter()
	if screen == nil {
		return footer
	}
	if !screen.HasVisibleContent() && !visible {
		return footer
	}
	return appendTerminalExitFooter(render(screen, m.state.Focus == state.FocusConsole && footer == ""), footer)
}

func (m Model) activeTerminalPlainLines(render func(*TerminalScreen) []string) []string {
	screen, footer, visible := m.activeScreenFooter()
	if screen == nil {
		return terminalExitFooterLines(footer)
	}
	if !screen.HasVisibleContent() && !visible {
		return terminalExitFooterLines(footer)
	}
	return appendTerminalExitFooterPlainLines(render(screen), footer)
}

func appendTerminalExitFooter(output string, footer string) string {
	if strings.TrimSpace(footer) == "" {
		return output
	}
	lines := strings.Split(strings.ReplaceAll(output, "\r", ""), "\n")
	lines = trimTrailingANSIBlankLines(lines)
	if len(lines) == 0 {
		return footer
	}
	lines = append(lines, "")
	lines = append(lines, strings.Split(footer, "\n")...)
	return strings.Join(lines, "\n")
}

func appendTerminalExitFooterPlainLines(lines []string, footer string) []string {
	if strings.TrimSpace(footer) == "" {
		return lines
	}
	out := append([]string(nil), lines...)
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	footerLines := strings.Split(footer, "\n")
	if len(out) == 0 {
		return footerLines
	}
	out = append(out, "")
	out = append(out, footerLines...)
	return out
}

func terminalExitFooterLines(footer string) []string {
	if strings.TrimSpace(footer) == "" {
		return nil
	}
	return strings.Split(footer, "\n")
}

func trimTrailingANSIBlankLines(lines []string) []string {
	out := append([]string(nil), lines...)
	for len(out) > 0 && strings.TrimSpace(ansi.Strip(out[len(out)-1])) == "" {
		out = out[:len(out)-1]
	}
	return out
}

func (m Model) codexLoading() bool {
	active := state.ActiveTask(m.state)
	if active == nil {
		return false
	}
	return m.taskLoading(active.ID) || m.taskOperationActive(active.ID)
}

func (m Model) loadingTaskIDs() []string {
	ids := make([]string, 0)
	for _, task := range m.state.Tasks {
		if m.taskLoading(task.ID) || m.taskOperationActive(task.ID) {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func (m *Model) taskOperationStartedAtForSnapshot() map[string]time.Time {
	if m.operationStarts == nil {
		return nil
	}
	startedAt := map[string]time.Time{}
	for _, task := range m.state.Tasks {
		if started, ok := m.taskOperationStartedAt(task.ID); ok && (m.taskLoading(task.ID) || m.taskOperationActive(task.ID)) {
			startedAt[task.ID] = started
		}
	}
	if len(startedAt) == 0 {
		return nil
	}
	return startedAt
}

func (m *Model) taskOperationDurationsForSnapshot() map[string]time.Duration {
	if m.operationStarts == nil {
		return nil
	}
	durations := map[string]time.Duration{}
	for _, task := range m.state.Tasks {
		if duration, ok := m.taskOperationCompletedDuration(task.ID); ok && titles.ConsolidatedStatus(task) == string(state.StatusReady) {
			durations[task.ID] = duration
		}
	}
	if len(durations) == 0 {
		return nil
	}
	return durations
}

func (m *Model) syncTaskOperationStartsWithStatuses() {
	for _, task := range m.state.Tasks {
		if m.taskLoading(task.ID) || taskStatusIndicatesActivity(task) {
			m.ensureTaskOperationStarted(task.ID)
			continue
		}
		if titles.ConsolidatedStatus(task) == string(state.StatusReady) {
			m.completeTaskOperationStarted(task.ID)
			continue
		}
		m.clearTaskOperationStarted(task.ID)
	}
}

func (m *Model) taskOperationTiming(taskID string) (taskOperationStart, bool) {
	if m.operationStarts == nil {
		return taskOperationStart{}, false
	}
	value, ok := m.operationStarts.Load(taskID)
	if !ok {
		return taskOperationStart{}, false
	}
	switch typed := value.(type) {
	case taskOperationStart:
		if typed.startedAt.IsZero() && typed.completedDuration <= 0 {
			return taskOperationStart{}, false
		}
		return typed, true
	case time.Time:
		if typed.IsZero() {
			return taskOperationStart{}, false
		}
		return taskOperationStart{startedAt: typed}, true
	default:
		return taskOperationStart{}, false
	}
}

func (m *Model) taskOperationStart(taskID string) (taskOperationStart, bool) {
	operation, ok := m.taskOperationTiming(taskID)
	if !ok || operation.startedAt.IsZero() {
		return taskOperationStart{}, false
	}
	return operation, true
}

func (m *Model) taskOperationStartedAt(taskID string) (time.Time, bool) {
	start, ok := m.taskOperationStart(taskID)
	return start.startedAt, ok
}

func (m *Model) taskOperationCompletedDuration(taskID string) (time.Duration, bool) {
	operation, ok := m.taskOperationTiming(taskID)
	if !ok || operation.completedDuration <= 0 {
		return 0, false
	}
	return operation.completedDuration, true
}

func (m *Model) taskOperationActive(taskID string) bool {
	task := state.TaskByID(m.state, taskID)
	if task == nil {
		return false
	}
	operation, ok := m.taskOperationStart(taskID)
	if !ok {
		return false
	}
	switch titles.ConsolidatedStatus(*task) {
	case string(state.StatusError), string(state.StatusStopped), string(state.StatusKilled), string(state.StatusSitting):
		return false
	case string(state.StatusReady):
		return operation.allowReady
	default:
		return true
	}
}

func (m *Model) markTaskOperationStarted(taskID string, allowReady bool) {
	if strings.TrimSpace(taskID) == "" {
		return
	}
	if m.operationStarts == nil {
		m.operationStarts = &sync.Map{}
	}
	m.operationStarts.Store(taskID, taskOperationStart{startedAt: time.Now(), allowReady: allowReady})
}

func (m *Model) ensureTaskOperationStarted(taskID string) {
	if strings.TrimSpace(taskID) == "" {
		return
	}
	if m.operationStarts == nil {
		m.operationStarts = &sync.Map{}
	}
	if _, ok := m.taskOperationStart(taskID); ok {
		return
	}
	m.operationStarts.Store(taskID, taskOperationStart{startedAt: time.Now()})
}

func (m *Model) clearTaskOperationStarted(taskID string) {
	if m.operationStarts == nil {
		return
	}
	m.operationStarts.Delete(taskID)
}

func (m *Model) completeTaskOperationStarted(taskID string) {
	if m.operationStarts == nil {
		return
	}
	operation, ok := m.taskOperationStart(taskID)
	if !ok {
		return
	}
	duration := time.Since(operation.startedAt)
	if duration < time.Second {
		duration = time.Second
	}
	m.operationStarts.Store(taskID, taskOperationStart{completedDuration: duration})
}

func (m *Model) codexOperationComplete(taskID string, screenStatus string, terminalTitle string) bool {
	operation, ok := m.taskOperationStart(taskID)
	if !ok {
		return false
	}
	if m.taskLoading(taskID) {
		return false
	}
	if strings.TrimSpace(terminalTitle) != "" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(screenStatus), string(state.StatusReady)) {
		return true
	}
	return !operation.allowReady
}

func (m Model) taskLoading(taskID string) bool {
	task := state.TaskByID(m.state, taskID)
	if task == nil {
		return false
	}
	screen := m.screens[taskID]
	return taskDefinitionForTask(m.cfg, *task).Loading(*task, tasktypes.LoadingContext{
		Active:        taskID == m.state.ActiveTaskID,
		ScreenVisible: screen != nil && screen.HasVisibleContent() || m.visible[taskID],
	})
}

func (m *Model) markTerminalCommandStarted(taskID string) {
	task := state.TaskByID(m.state, taskID)
	if task == nil || !taskDefinitionForTask(m.cfg, *task).TracksForegroundCommands() {
		return
	}
	if m.terminalCommands == nil {
		m.terminalCommands = map[string]time.Time{}
	}
	m.terminalCommands[taskID] = time.Now()
	m.markTaskOperationStarted(taskID, false)
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
		if task == nil || !taskDefinitionForTask(m.cfg, *task).TracksForegroundCommands() {
			delete(m.terminalCommands, taskID)
			m.clearTaskOperationStarted(taskID)
			continue
		}
		if task.Status != state.StatusRunning {
			delete(m.terminalCommands, taskID)
			m.clearTaskOperationStarted(taskID)
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
		m.completeTaskOperationStarted(taskID)
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

func (m Model) loadingLabel() string {
	label := "task"
	if active := state.ActiveTask(m.state); active != nil {
		label = taskTypeForTask(m.cfg, *active).Label
	}
	return loadingFrames[0] + " Starting " + label
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
		if task != nil && taskDefinitionForTask(m.cfg, *task).TopAlignedResize() {
			screen.ResizeTopAligned(max(screen.cols, m.ptyWidth()), m.ptyHeight())
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

func (m *Model) handleTaskContextSet(args map[string]string) ipc.Response {
	if response := m.taskContextCommandAvailable(); !response.OK {
		return response
	}
	if arg := unsupportedIPCArg(args, "task_id", "kind", "content"); arg != "" {
		return ipc.ErrorResponse("unsupported_arg", "unsupported argument: "+arg)
	}
	kind, response := taskContextKind(args["kind"])
	if !response.OK {
		return response
	}
	task, response := m.taskContextTarget(args)
	if !response.OK {
		return response
	}
	var record taskcontext.Record
	var err error
	switch kind {
	case "heading":
		record, err = m.taskContextStore.SetHeading(task.ID, args["content"])
	case "detail":
		record, err = m.taskContextStore.SetDetail(task.ID, args["content"])
	}
	if err != nil {
		return ipcError("task_context_set_failed", err)
	}
	m.taskContexts[task.ID] = record
	response = m.ipcResponse("Set task notes " + kind + " for task " + task.ID + ".")
	response.TaskContext = taskContextRecordToIPC(record)
	return response
}

func (m *Model) handleTaskContextShow(args map[string]string) ipc.Response {
	if response := m.taskContextCommandAvailable(); !response.OK {
		return response
	}
	if arg := unsupportedIPCArg(args, "task_id", "kind"); arg != "" {
		return ipc.ErrorResponse("unsupported_arg", "unsupported argument: "+arg)
	}
	if _, response := taskContextKind(args["kind"]); !response.OK {
		return response
	}
	task, response := m.taskContextTarget(args)
	if !response.OK {
		return response
	}
	record, ok, err := m.taskContextStore.Show(task.ID)
	if err != nil {
		return ipcError("task_context_show_failed", err)
	}
	response = m.ipcResponse("")
	if !ok {
		delete(m.taskContexts, task.ID)
		response.Message = "No task notes for task " + task.ID + "."
		response.TaskContext = &ipc.TaskContext{TaskID: task.ID}
		return response
	}
	m.taskContexts[task.ID] = record
	response.TaskContext = taskContextRecordToIPC(record)
	return response
}

func (m *Model) handleTaskContextClear(args map[string]string) ipc.Response {
	if response := m.taskContextCommandAvailable(); !response.OK {
		return response
	}
	if arg := unsupportedIPCArg(args, "task_id", "kind"); arg != "" {
		return ipc.ErrorResponse("unsupported_arg", "unsupported argument: "+arg)
	}
	kind, response := taskContextKind(args["kind"])
	if !response.OK {
		return response
	}
	task, response := m.taskContextTarget(args)
	if !response.OK {
		return response
	}
	removed, err := m.taskContextStore.Clear(task.ID, kind)
	if err != nil {
		return ipcError("task_context_clear_failed", err)
	}
	if record, ok, err := m.taskContextStore.Show(task.ID); err == nil && ok {
		m.taskContexts[task.ID] = record
	} else {
		delete(m.taskContexts, task.ID)
	}
	if removed {
		response = m.ipcResponse("Cleared task notes " + kind + " for task " + task.ID + ".")
	} else {
		response = m.ipcResponse("No task notes " + kind + " for task " + task.ID + ".")
	}
	response.TaskContext = &ipc.TaskContext{TaskID: task.ID}
	return response
}

func taskContextKind(kind string) (string, ipc.Response) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "heading"
	}
	switch kind {
	case "heading", "detail":
		return kind, ipc.Response{OK: true}
	default:
		return "", ipc.ErrorResponse("invalid_task_context_kind", "task notes kind must be heading or detail")
	}
}

func (m *Model) taskContextCommandAvailable() ipc.Response {
	if !m.cfg.TaskContext.Enabled {
		return ipc.ErrorResponse("task_context_disabled", "task notes are disabled in config")
	}
	if m.taskContextErr != nil {
		return ipc.ErrorResponse("task_context_unavailable", m.taskContextErr.Error())
	}
	if m.taskContextStore == nil {
		m.taskContextStore = taskcontext.NewStore(m.runtime.Dir)
	}
	if m.taskContexts == nil {
		m.taskContexts = map[string]taskcontext.Record{}
	}
	return ipc.Response{OK: true}
}

func (m *Model) taskContextTarget(args map[string]string) (*state.Task, ipc.Response) {
	taskID := strings.TrimSpace(args["task_id"])
	if taskID == "" {
		taskID = m.state.ActiveTaskID
	}
	if taskID == "" {
		return nil, ipc.ErrorResponse("task_not_found", "no active task")
	}
	task := state.TaskByID(m.state, taskID)
	if task == nil {
		return nil, ipc.ErrorResponse("task_not_found", "task not found: "+taskID)
	}
	if !taskIsCodex(m.cfg, *task) {
		return nil, ipc.ErrorResponse("task_context_not_supported", "task notes are only supported for Codex tasks")
	}
	return task, ipc.Response{OK: true}
}

func (m *Model) clearTaskContextRecord(taskID string) {
	if strings.TrimSpace(taskID) == "" || m.taskContextStore == nil {
		return
	}
	_, err := m.taskContextStore.Clear(taskID, "all")
	if err != nil {
		m.taskContextErr = err
		return
	}
	delete(m.taskContexts, taskID)
}

func unsupportedIPCArg(args map[string]string, allowed ...string) string {
	allowedSet := map[string]bool{}
	for _, key := range allowed {
		allowedSet[key] = true
	}
	for key := range args {
		if !allowedSet[key] {
			return key
		}
	}
	return ""
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
		width := request.Width
		height := request.Height
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
		return m.ipcResponse("selection updated"), nil
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
		if arg := unsupportedIPCArg(request.Args, "title", "type", "silent"); arg != "" {
			return ipc.ErrorResponse("unsupported_arg", "unsupported argument: "+arg), nil
		}
		title := request.Args["title"]
		typeID := strings.TrimSpace(request.Args["type"])
		if typeID == "" {
			typeID = m.cfg.DefaultTaskType
		}
		if _, ok := m.cfg.TaskType(typeID); !ok {
			return ipc.ErrorResponse("task_type_not_found", "task type not found: "+typeID), nil
		}
		silent := false
		if raw := strings.TrimSpace(request.Args["silent"]); raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				return ipc.ErrorResponse("invalid_silent", "silent must be a boolean"), nil
			}
			silent = parsed
		}
		cmd := m.newTaskWithSilent(title, silent, typeID)
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
		var next state.State
		var err error
		if raw := strings.TrimSpace(request.Args["silent"]); raw != "" {
			silent, parseErr := strconv.ParseBool(raw)
			if parseErr != nil {
				return ipc.ErrorResponse("invalid_silent", "silent must be a boolean"), nil
			}
			next, err = state.EditTask(m.state, id, title, silent)
		} else {
			next, err = state.RenameTask(m.state, id, title)
		}
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
	case "task_context_set":
		return m.handleTaskContextSet(request.Args), nil
	case "task_context_show":
		return m.handleTaskContextShow(request.Args), nil
	case "task_context_clear":
		return m.handleTaskContextClear(request.Args), nil
	case "remove_workspace":
		next, tasks, err := state.RemoveWorkspace(m.state, request.Args["id"])
		if err != nil {
			return ipcError("remove_workspace_failed", err), nil
		}
		for _, task := range tasks {
			m.killTaskPTY(task.ID)
			m.clearTaskContextRecord(task.ID)
		}
		m.state = next
		m.syncGroupCursor()
		m.save()
		m.snapNavWidthToTarget()
		return m.ipcResponse("removed workspace"), nil
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
		if arg := unsupportedIPCArg(request.Args, "id", "direction", "group"); arg != "" {
			return ipc.ErrorResponse("unsupported_arg", "unsupported argument: "+arg), nil
		}
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
			return m.ipcResponse("focus updated"), nil
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
	if taskInputModeForTask(m.cfg, *active) != tasktypes.InputModeCodex {
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
	if taskInputModeForTask(m.cfg, *active) == tasktypes.InputModeCodex {
		return m.applyCodexInput(args)
	}
	encoded := []byte(args["encoded"])
	if args["input"] == "ctrl+c" {
		encoded = []byte{0x03}
		if pty := m.ptys[active.ID]; pty == nil || !pty.ForegroundProcessActive() {
			return m.killFocusedTerminalTask(active.ID)
		}
	}
	if pty := m.ptys[active.ID]; pty != nil {
		if args["input"] == "ctrl+c" {
			m.recordTaskInterrupt(active.ID)
		}
		_ = pty.Write(encoded)
	}
	return m.captureRawTerminalInput(*active, encoded)
}

func (m *Model) clearActiveTerminal() {
	if m.state.Focus != state.FocusConsole {
		return
	}
	active := state.ActiveTask(m.state)
	if active == nil || taskInputModeForTask(m.cfg, *active) != tasktypes.InputModeTerminal {
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
	m.markInputSubmitted(task.ID)
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
		case 0x03:
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
	if cwd := terminalTaskCWD(m.cfg, *task); cwd != "" {
		return cwd
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
		fmt.Sprintf("%s task tools", cfg.KeyBindings.Repaint),
		"U upgrade now, or schedule when tasks are idle",
		fmt.Sprintf("%s help", cfg.KeyBindings.Help),
		"C-r repaint whole screen",
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
