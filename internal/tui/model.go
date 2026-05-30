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
	"github.com/charmbracelet/bubbles/viewport"
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
	tabID   string
	session *ptyx.Session
	err     error
}

type mode string

const (
	modeNormal mode = ""
	modeHelp   mode = "help"
	modeRename mode = "rename"
	modeClose  mode = "close"
)

const (
	navAnimationInterval  = 12 * time.Millisecond
	navAnimationStep      = 2
	loadingInterval       = 90 * time.Millisecond
	renameModalLabelWidth = 9
)

type navAnimationTick struct{}
type loadingTick struct{}

var loadingFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
	navHeight int
	loading   int

	screens map[string]*TerminalScreen
	ptys    map[string]*ptyx.Session
	visible map[string]bool
	dataCh  chan ptyx.Data
	ipcCh   chan ipcEnvelope
	stopIPC func() error
	ctx     context.Context
	cancel  context.CancelFunc

	renameInput  textinput.Model
	viewport     viewport.Model
	pendingClose string
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
	input.CharLimit = 160
	input.Width = 60
	vp := viewport.New(80, 20)
	st = state.RepairColumns(st, cfg.Columns)
	if state.ActiveTab(st) == nil {
		st.ActiveTabID = ""
		st.Focus = state.FocusNav
	}
	model := Model{
		cfg: cfg, runtime: rt, store: state.NewStore(rt.StatePath), state: st,
		width: 100, height: 32, screens: map[string]*TerminalScreen{}, ptys: map[string]*ptyx.Session{},
		visible: map[string]bool{},
		dataCh:  make(chan ptyx.Data, 64), ipcCh: make(chan ipcEnvelope, 16),
		ctx: ctx, cancel: cancel, renameInput: input, viewport: vp,
	}
	model.navHeight = model.targetNavHeight()
	for _, tab := range model.state.Tabs {
		model.startPTY(tab.ID)
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
		m.navHeight = m.targetNavHeight()
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
	if m.mode == modeRename {
		return m.modalView(m.renderRenameModal())
	}
	if m.mode == modeClose {
		title := "active tab"
		if tab := state.ActiveTab(m.state); tab != nil {
			title = titles.Render(*tab)
		}
		return m.modalView(fmt.Sprintf("Close %s?\n\nY close  N cancel", title))
	}
	content := m.activeOutput()
	loadingText := ""
	if content == "" && m.codexLoading() {
		loadingText = m.loadingLabel()
	} else if content == "" {
		content = "No Codex tabs open."
	}
	title := "Codex"
	if active := state.ActiveTab(m.state); active != nil {
		title = titles.Render(*active)
	}
	if loadingText != "" {
		return renderLoadingWorkspaceWithNavHeight(m.cfg, m.state, title, loadingText, m.width, m.height, m.message, m.runtime.Workdir, m.navHeight)
	}
	return renderWorkspaceWithNavHeight(m.cfg, m.state, title, content, m.width, m.height, m.message, m.runtime.Workdir, m.navHeight)
}

func (m Model) modalView(content string) string {
	w := max(40, min(m.width-4, 82))
	box := modalStyle.Width(w).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderRenameModal() string {
	width := max(36, min(m.width-16, 72))
	input := m.renameInput
	input.Width = max(16, width-renameModalLabelWidth-3)

	current := "active tab"
	preview := "title required"
	if active := state.ActiveTab(m.state); active != nil {
		current = titles.Render(*active)
		if value := strings.TrimSpace(input.Value()); value != "" {
			draft := *active
			draft.Title = value
			preview = titles.Render(draft)
		}
	}

	lines := []string{
		modalTitleStyle.Render("Rename tab"),
		"",
		renderRenameModalRow("Current", modalValueStyle.Render(clip(current, max(0, width-10))), width),
		renderRenameModalRow("Template", input.View(), width),
		renderRenameModalRow("Preview", modalValueStyle.Render(clip(preview, max(0, width-10))), width),
		"",
		modalLabelStyle.Render("Variables"),
	}
	lines = append(lines, renderTitleVariableRows(width)...)
	lines = append(lines, "", renderModalActions())
	return strings.Join(lines, "\n")
}

func renderRenameModalRow(label string, value string, width int) string {
	valueWidth := max(0, width-renameModalLabelWidth-1)
	return modalLabelStyle.Render(padVisual(label, renameModalLabelWidth)) + " " + clip(value, valueWidth)
}

func renderTitleVariableRows(width int) []string {
	variables := titles.TemplateVariables()
	tokenWidth := 0
	for _, variable := range variables {
		tokenWidth = max(tokenWidth, lipgloss.Width(variable.Name))
	}
	descriptionWidth := max(0, width-tokenWidth-2)
	lines := make([]string, 0, len(variables))
	for _, variable := range variables {
		token := modalTokenStyle.Render(padVisual(variable.Name, tokenWidth))
		description := mutedStyle.Render(clip(variable.Description, descriptionWidth))
		lines = append(lines, token+"  "+description)
	}
	return lines
}

func renderModalActions() string {
	return modalKeyStyle.Render("Enter") + " save  " + modalKeyStyle.Render("Esc") + " cancel"
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeRename {
		return m.handleRenameKey(msg)
	}
	if m.mode == modeHelp {
		if msg.Type == tea.KeyEsc || msg.String() == "q" || msg.String() == "?" {
			m.mode = modeNormal
		}
		return m, nil
	}
	if m.mode == modeClose {
		switch strings.ToLower(msg.String()) {
		case "y":
			cmd := m.closeTab(m.pendingClose)
			m.mode = modeNormal
			return m, cmd
		case "n", "esc":
			m.mode = modeNormal
		}
		return m, nil
	}

	closeCoduxPressed := bindingMatches(m.cfg.KeyBindings.CloseCodux, msg)
	if closeCoduxPressed && !m.activeCodexReceivesCloseBinding() {
		m.closeCodux()
		return m, nil
	}
	if bindingMatches(m.cfg.KeyBindings.FocusToggle, msg) {
		return m, m.toggleFocus()
	}
	if m.state.Focus == state.FocusCodex && state.ActiveTab(m.state) != nil {
		if active := state.ActiveTab(m.state); active != nil {
			if pty := m.ptys[active.ID]; pty != nil {
				_ = pty.Write(encodeKey(msg))
			}
		}
		return m, nil
	}
	if m.state.Focus == state.FocusCodex {
		cmd := m.setFocus(state.FocusNav)
		updated, nextCmd := m.handleNavKey(msg)
		return updated, tea.Batch(cmd, nextCmd)
	}
	if bindingMatches(m.cfg.KeyBindings.Help, msg) {
		m.mode = modeHelp
		return m, nil
	}
	return m.handleNavKey(msg)
}

func (m Model) activeCodexReceivesCloseBinding() bool {
	if m.state.Focus != state.FocusCodex {
		return false
	}
	active := state.ActiveTab(m.state)
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
	case bindingMatches(m.cfg.KeyBindings.New, msg):
		return m, m.newTab(titles.CodexTitleTemplate)
	case bindingMatches(m.cfg.KeyBindings.Close, msg):
		if active := state.ActiveTab(m.state); active != nil {
			m.pendingClose = active.ID
			m.mode = modeClose
		}
	case bindingMatches(m.cfg.KeyBindings.Rename, msg):
		if active := state.ActiveTab(m.state); active != nil {
			m.renameInput.SetValue(active.Title)
			m.renameInput.CursorEnd()
			m.renameInput.Focus()
			m.mode = modeRename
		}
	case bindingMatches(m.cfg.KeyBindings.Prev, msg):
		m.selectTab(navigation.SelectGridTab(m.state.Tabs, m.state.ActiveTabID, m.cfg.Columns, -1, 0))
	case bindingMatches(m.cfg.KeyBindings.Next, msg):
		m.selectTab(navigation.SelectGridTab(m.state.Tabs, m.state.ActiveTabID, m.cfg.Columns, 1, 0))
	case bindingMatches(m.cfg.KeyBindings.MoveLeft, msg):
		m.state = state.MoveActiveColumn(m.state, m.cfg.Columns, -1)
		m.save()
	case bindingMatches(m.cfg.KeyBindings.MoveRight, msg):
		m.state = state.MoveActiveColumn(m.state, m.cfg.Columns, 1)
		m.save()
	case msg.Type == tea.KeyUp:
		m.selectTab(navigation.SelectGridTab(m.state.Tabs, m.state.ActiveTabID, m.cfg.Columns, 0, -1))
	case msg.Type == tea.KeyDown:
		m.selectTab(navigation.SelectGridTab(m.state.Tabs, m.state.ActiveTabID, m.cfg.Columns, 0, 1))
	case msg.Type == tea.KeyEnter:
		return m, m.setFocus(state.FocusCodex)
	}
	return m, nil
}

func (m Model) handleRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeNormal
		return m, nil
	case tea.KeyEnter:
		value := strings.TrimSpace(m.renameInput.Value())
		if value == "" {
			m.message = "title cannot be empty"
			return m, nil
		}
		if active := state.ActiveTab(m.state); active != nil {
			m.renameTab(active.ID, value)
		}
		m.mode = modeNormal
		return m, nil
	}
	var cmd tea.Cmd
	m.renameInput, cmd = m.renameInput.Update(msg)
	return m, cmd
}

func (m *Model) newTab(title string) tea.Cmd {
	now := state.NowISO()
	tab := state.Tab{
		ID: shortID(), Title: title, Column: m.cfg.Columns[0],
		CreatedAt: now, UpdatedAt: now, Status: state.StatusStarting,
	}
	m.state.Tabs = append(m.state.Tabs, tab)
	m.state.ActiveTabID = tab.ID
	m.state.Focus = state.FocusCodex
	m.save()
	return tea.Batch(m.startPTYCmd(tab.ID), m.startNavAnimation(), tickLoading())
}

func (m *Model) closeTab(tabID string) tea.Cmd {
	if tabID == "" {
		return nil
	}
	if pty := m.ptys[tabID]; pty != nil {
		pty.Kill()
		delete(m.ptys, tabID)
	}
	delete(m.screens, tabID)
	delete(m.visible, tabID)
	m.state = state.CloseTab(m.state, tabID)
	if state.ActiveTab(m.state) == nil {
		m.state.Focus = state.FocusNav
	}
	m.save()
	return m.startNavAnimation()
}

func (m *Model) renameTab(tabID string, title string) {
	m.state = state.WithUpdatedTab(m.state, tabID, func(tab state.Tab) state.Tab {
		tab.Title = title
		return tab
	})
	m.save()
}

func (m *Model) selectTab(tabID string) {
	if tabID == "" {
		return
	}
	for _, tab := range m.state.Tabs {
		if tab.ID == tabID {
			m.state.ActiveTabID = tabID
			m.save()
			return
		}
	}
}

func (m *Model) toggleFocus() tea.Cmd {
	if m.state.Focus == state.FocusNav {
		return m.setFocus(state.FocusCodex)
	}
	return m.setFocus(state.FocusNav)
}

func (m *Model) setFocus(focus state.Focus) tea.Cmd {
	m.state.Focus = focus
	m.save()
	return m.startNavAnimation()
}

func (m *Model) closeCodux() {
	_ = exec.Command("tmux", "detach-client", "-s", m.cfg.TmuxSession).Run()
	m.message = "closed Codux clients"
}

func (m *Model) startPTY(tabID string) {
	if m.ptys[tabID] != nil {
		return
	}
	if m.screens[tabID] == nil {
		m.screens[tabID] = NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
	}
	ptySession, err := ptyx.Start(m.ctx, tabID, m.cfg.CodexCommand, m.runtime.Workdir, m.ptyWidth(), m.ptyHeight(), func(data ptyx.Data) {
		m.dataCh <- data
	})
	if err != nil {
		m.state = state.WithUpdatedTab(m.state, tabID, func(tab state.Tab) state.Tab {
			tab.Status = state.StatusError
			tab.CodexTitle = err.Error()
			return tab
		})
		m.save()
		return
	}
	m.ptys[tabID] = ptySession
	m.state = state.WithUpdatedTab(m.state, tabID, func(tab state.Tab) state.Tab {
		tab.Status = state.StatusRunning
		return tab
	})
	m.save()
}

func (m *Model) startPTYCmd(tabID string) tea.Cmd {
	if m.ptys[tabID] != nil {
		return nil
	}
	ctx := m.ctx
	command := m.cfg.CodexCommand
	workdir := m.runtime.Workdir
	cols := m.ptyWidth()
	rows := m.ptyHeight()
	dataCh := m.dataCh
	return func() tea.Msg {
		ptySession, err := ptyx.Start(ctx, tabID, command, workdir, cols, rows, func(data ptyx.Data) {
			dataCh <- data
		})
		return ptyStartedMsg{tabID: tabID, session: ptySession, err: err}
	}
}

func (m *Model) applyPTYStarted(msg ptyStartedMsg) {
	if msg.err != nil {
		m.state = state.WithUpdatedTab(m.state, msg.tabID, func(tab state.Tab) state.Tab {
			tab.Status = state.StatusError
			tab.CodexTitle = msg.err.Error()
			return tab
		})
		m.save()
		return
	}
	if !tabExists(m.state, msg.tabID) {
		msg.session.Kill()
		return
	}
	if m.ptys[msg.tabID] != nil {
		msg.session.Kill()
		return
	}
	if m.screens[msg.tabID] == nil {
		m.screens[msg.tabID] = NewTerminalScreen(m.ptyWidth(), m.ptyHeight())
	}
	m.ptys[msg.tabID] = msg.session
	m.state = state.WithUpdatedTab(m.state, msg.tabID, func(tab state.Tab) state.Tab {
		tab.Status = state.StatusRunning
		return tab
	})
	m.save()
}

func tabExists(st state.State, tabID string) bool {
	for _, tab := range st.Tabs {
		if tab.ID == tabID {
			return true
		}
	}
	return false
}

func (m *Model) applyPTYData(data ptyx.Data) {
	if !tabExists(m.state, data.TabID) {
		return
	}
	if data.Err != nil {
		m.state = state.WithUpdatedTab(m.state, data.TabID, func(tab state.Tab) state.Tab {
			if tab.Status != state.StatusError {
				tab.Status = state.StatusStopped
			}
			return tab
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
		m.state = state.WithUpdatedTab(m.state, data.TabID, func(tab state.Tab) state.Tab {
			tab.CodexTitle = titles.NormalizeCodexTitle(data.Title)
			tab.Status = state.StatusRunning
			return tab
		})
		m.save()
	}
}

func (m *Model) activeOutput() string {
	active := state.ActiveTab(m.state)
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
	active := state.ActiveTab(m.state)
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
	return max(20, m.width-4-codexLeftPadding)
}

func (m Model) ptyHeight() int {
	return max(5, m.height-m.effectiveNavHeight()-1)
}

func (m Model) effectiveNavHeight() int {
	return min(max(0, m.navHeight), max(0, m.height-3))
}

func (m Model) targetNavHeight() int {
	if m.state.Focus == state.FocusCodex && state.ActiveTab(m.state) != nil {
		return 0
	}
	return workspaceNavFrameHeight(m.cfg, m.state, m.height)
}

func (m *Model) startNavAnimation() tea.Cmd {
	if m.navHeight == m.targetNavHeight() {
		return nil
	}
	return tickNavAnimation()
}

func (m *Model) stepNavAnimation() tea.Cmd {
	target := m.targetNavHeight()
	delta := target - m.navHeight
	if delta == 0 {
		return nil
	}
	if abs(delta) <= navAnimationStep {
		m.navHeight = target
	} else if delta > 0 {
		m.navHeight += navAnimationStep
	} else {
		m.navHeight -= navAnimationStep
	}
	m.resizePTYs()
	m.resizeScreens()
	if m.navHeight != target {
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
		return ipc.Response{OK: true, State: &st, Message: "refreshed Codux dashboard"}, nil
	case "new":
		title := request.Args["title"]
		if title == "" {
			title = titles.CodexTitleTemplate
		}
		cmd := m.newTab(title)
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "created Codex session"}, cmd
	case "rename":
		id := request.Args["id"]
		if id == "" {
			id = m.state.ActiveTabID
		}
		title := strings.TrimSpace(request.Args["title"])
		if title == "" {
			return ipc.Response{OK: false, Message: "title is required"}, nil
		}
		m.renameTab(id, title)
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "renamed Codex session"}, nil
	case "close":
		id := request.Args["id"]
		if id == "" {
			id = m.state.ActiveTabID
		}
		cmd := m.closeTab(id)
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "closed Codex session"}, cmd
	case "select":
		m.selectTab(request.Args["id"])
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "selected Codex session"}, nil
	case "move":
		switch request.Args["direction"] {
		case "left":
			m.state = state.MoveActiveColumn(m.state, m.cfg.Columns, -1)
		case "right":
			m.state = state.MoveActiveColumn(m.state, m.cfg.Columns, 1)
		default:
			return ipc.Response{OK: false, Message: "direction must be left or right"}, nil
		}
		m.save()
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "moved Codex session"}, nil
	case "focus":
		target := state.Focus(request.Args["target"])
		if target != state.FocusNav && target != state.FocusCodex {
			return ipc.Response{OK: false, Message: "focus target must be nav or codex"}, nil
		}
		cmd := m.setFocus(target)
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "focus updated"}, cmd
	case "close_codux", "quit":
		m.closeCodux()
		st := m.state
		return ipc.Response{OK: true, State: &st, Message: "closed Codux clients"}, nil
	default:
		return ipc.Response{OK: false, Message: "unknown command: " + request.Command}, nil
	}
}

func (m Model) statusText() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "tmux session: %s\n", m.cfg.TmuxSession)
	fmt.Fprintf(&builder, "workdir: %s\n", m.runtime.Workdir)
	fmt.Fprintf(&builder, "runtime dir: %s\n", m.runtime.Dir)
	fmt.Fprintf(&builder, "focus: %s\n", m.state.Focus)
	fmt.Fprintf(&builder, "tabs: %d\n", len(m.state.Tabs))
	for _, tab := range m.state.Tabs {
		marker := " "
		if tab.ID == m.state.ActiveTabID {
			marker = "*"
		}
		fmt.Fprintf(&builder, "%s %s %s %s %s\n", marker, tab.ID, tab.Column, tab.Status, titles.Render(tab))
	}
	return strings.TrimRight(builder.String(), "\n")
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
		fmt.Sprintf("%s new session", cfg.KeyBindings.New),
		fmt.Sprintf("%s/%s select", cfg.KeyBindings.Prev, cfg.KeyBindings.Next),
		fmt.Sprintf("%s/%s move", cfg.KeyBindings.MoveLeft, cfg.KeyBindings.MoveRight),
		fmt.Sprintf("%s rename", cfg.KeyBindings.Rename),
		fmt.Sprintf("%s close", cfg.KeyBindings.Close),
		fmt.Sprintf("%s help", cfg.KeyBindings.Help),
		fmt.Sprintf("%s focus", cfg.KeyBindings.FocusToggle),
		fmt.Sprintf("%s close codux", cfg.KeyBindings.CloseCodux),
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
