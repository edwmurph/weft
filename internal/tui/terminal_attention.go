package tui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/titles"
)

const (
	terminalAttentionOff  = "off"
	terminalAttentionOnce = "once"
)

type terminalAttentionProvider interface {
	initialSequence() string
	notifySequence(message string) string
	requestAttentionSequence(value string) string
	clearSequence(attentionRequested bool) string
}

type itermAttentionProvider struct{}

type terminalAttentionState struct {
	initialized         bool
	enabled             bool
	provider            terminalAttentionProvider
	requestAttention    string
	attentionRequested  bool
	count               int
	attentionTasks      map[string]bool
	notificationTasks   map[string]bool
	notificationTaskIDs []string
	knownTasks          map[string]bool
	pendingNewTasks     map[string]bool
	newTasksThisTick    map[string]bool
}

func terminalAttentionCommand(cfg config.Config, snapshot ipc.Snapshot, previous terminalAttentionState) (terminalAttentionState, tea.Cmd) {
	next, sequence := terminalAttentionSequence(cfg.TerminalAttention, snapshot, previous, terminalAttentionProviderForEnv(os.Environ()))
	return next, terminalSequenceCommand(sequence)
}

func terminalAttentionExitCommand(previous terminalAttentionState) tea.Cmd {
	if !previous.initialized || !previous.enabled {
		return nil
	}
	return terminalSequenceCommand(terminalAttentionClearSequence(previous))
}

func terminalAttentionSequence(cfg config.TerminalAttention, snapshot ipc.Snapshot, previous terminalAttentionState, provider terminalAttentionProvider) (terminalAttentionState, string) {
	attentionTasks := globalAttentionTasks(snapshot)
	notificationTasks, notificationTaskIDs := terminalAttentionNotificationTasks(snapshot, attentionTasks)
	currentTasks := terminalAttentionCurrentTasks(snapshot)
	newTasksThisTick := terminalAttentionNewTasksThisTick(previous, currentTasks)
	requestAttention := normalizedTerminalAttentionRequest(cfg.RequestAttention)
	next := terminalAttentionState{
		initialized:         true,
		enabled:             cfg.Enabled && provider != nil,
		provider:            provider,
		requestAttention:    requestAttention,
		attentionRequested:  previous.attentionRequested && requestAttention != terminalAttentionOff,
		count:               len(notificationTasks),
		attentionTasks:      attentionTasks,
		notificationTasks:   notificationTasks,
		notificationTaskIDs: notificationTaskIDs,
		knownTasks:          currentTasks,
		pendingNewTasks:     terminalAttentionPendingNewTasks(newTasksThisTick, attentionTasks, currentTasks),
		newTasksThisTick:    newTasksThisTick,
	}
	if !next.enabled {
		if previous.initialized && previous.enabled {
			return next, terminalAttentionClearSequence(previous)
		}
		next.attentionRequested = false
		return next, ""
	}
	sequence := ""
	triggeredTaskIDs := terminalAttentionTriggeredTaskIDs(previous, next)
	notify := len(triggeredTaskIDs) > 0
	if !previous.initialized {
		sequence += provider.initialSequence()
	}
	if notify {
		sequence += provider.notifySequence(terminalAttentionNotificationText(snapshot, triggeredTaskIDs))
	}
	if requestAttention == terminalAttentionOnce && notify {
		sequence += provider.requestAttentionSequence(terminalAttentionOnce)
		next.attentionRequested = true
	}
	if previous.initialized && previous.enabled && previous.attentionRequested && next.count == 0 {
		sequence += provider.requestAttentionSequence("no")
		next.attentionRequested = false
	}
	return next, sequence
}

func terminalAttentionTriggeredTaskIDs(previous terminalAttentionState, next terminalAttentionState) []string {
	if !previous.initialized || !previous.enabled || next.count == 0 {
		return nil
	}
	triggered := []string{}
	for _, id := range next.notificationTaskIDs {
		if next.newTasksThisTick[id] {
			continue
		}
		if !previous.attentionTasks[id] {
			triggered = append(triggered, id)
		}
	}
	return triggered
}

func terminalAttentionClearSequence(previous terminalAttentionState) string {
	if previous.provider == nil {
		return ""
	}
	return previous.provider.clearSequence(previous.attentionRequested)
}

func globalNeedsAttentionCount(snapshot ipc.Snapshot) int {
	return len(globalAttentionTasks(snapshot))
}

func globalAttentionTasks(snapshot ipc.Snapshot) map[string]bool {
	attentionTasks := map[string]bool{}
	activeTasks := terminalAttentionActiveTasks(snapshot)
	for _, task := range snapshot.State.Tasks {
		if activeTasks[task.ID] {
			continue
		}
		if workspaceCardTaskActive(task) {
			continue
		}
		if taskSilenceEnabled(snapshot.State, task) && workspaceCardTaskSilenced(task) {
			continue
		}
		attentionTasks[task.ID] = true
	}
	return attentionTasks
}

func terminalAttentionNotificationTasks(snapshot ipc.Snapshot, attentionTasks map[string]bool) (map[string]bool, []string) {
	notificationTasks := make(map[string]bool, len(attentionTasks))
	notificationTaskIDs := []string{}
	focusedTaskID := ""
	if snapshot.State.Focus == state.FocusConsole {
		focusedTaskID = snapshot.State.ActiveTaskID
	}
	for _, task := range snapshot.State.Tasks {
		if !attentionTasks[task.ID] || task.ID == focusedTaskID {
			continue
		}
		notificationTasks[task.ID] = true
		notificationTaskIDs = append(notificationTaskIDs, task.ID)
	}
	return notificationTasks, notificationTaskIDs
}

func terminalAttentionCurrentTasks(snapshot ipc.Snapshot) map[string]bool {
	current := make(map[string]bool, len(snapshot.State.Tasks))
	for _, task := range snapshot.State.Tasks {
		if strings.TrimSpace(task.ID) != "" {
			current[task.ID] = true
		}
	}
	return current
}

func terminalAttentionNewTasksThisTick(previous terminalAttentionState, currentTasks map[string]bool) map[string]bool {
	newTasks := map[string]bool{}
	if !previous.initialized {
		return newTasks
	}
	for id := range currentTasks {
		if previous.pendingNewTasks[id] || !previous.knownTasks[id] {
			newTasks[id] = true
		}
	}
	return newTasks
}

func terminalAttentionPendingNewTasks(newTasks map[string]bool, attentionTasks map[string]bool, currentTasks map[string]bool) map[string]bool {
	pending := map[string]bool{}
	for id := range newTasks {
		if currentTasks[id] && !attentionTasks[id] {
			pending[id] = true
		}
	}
	return pending
}

func terminalAttentionActiveTasks(snapshot ipc.Snapshot) map[string]bool {
	active := make(map[string]bool, len(snapshot.LoadingTaskIDs)+len(snapshot.TerminalForegroundTaskIDs))
	for _, id := range snapshot.LoadingTaskIDs {
		if strings.TrimSpace(id) != "" {
			active[id] = true
		}
	}
	for _, id := range snapshot.TerminalForegroundTaskIDs {
		if strings.TrimSpace(id) != "" {
			active[id] = true
		}
	}
	return active
}

func terminalAttentionNotificationText(snapshot ipc.Snapshot, triggeredTaskIDs []string) string {
	title := terminalAttentionTaskTitle(snapshot.State, firstString(triggeredTaskIDs))
	if len(triggeredTaskIDs) <= 1 {
		return title + " needs attention"
	}
	return title + " and " + fmtInt(len(triggeredTaskIDs)-1) + " more need attention"
}

func terminalAttentionTaskTitle(st state.State, taskID string) string {
	task := state.TaskByID(st, taskID)
	if task == nil {
		return "Task"
	}
	title := terminalAttentionCompactTitle(terminalAttentionTaskTitleText(st, *task))
	if title == "" || title == "..." {
		return "Task"
	}
	return title
}

func terminalAttentionTaskTitleText(st state.State, task state.Task) string {
	title := strings.TrimSpace(task.Title)
	if title == "" {
		return renderTaskWithTemplate(st, task, titles.TitleTemplate)
	}
	if terminalAttentionVariableOnlyTitle(title) {
		return renderTaskWithTemplate(st, task, title)
	}
	return title
}

func terminalAttentionVariableOnlyTitle(title string) bool {
	remaining := title
	for _, variable := range titles.TemplateVariables() {
		remaining = strings.ReplaceAll(remaining, variable.Name, "")
	}
	return strings.TrimSpace(remaining) == ""
}

func terminalAttentionCompactTitle(title string) string {
	title = strings.Join(strings.Fields(sanitizeOSCText(title)), " ")
	const maxTitleRunes = 72
	runes := []rune(title)
	if len(runes) <= maxTitleRunes {
		return title
	}
	return strings.TrimSpace(string(runes[:maxTitleRunes-3])) + "..."
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func itermClearSessionBadge() string {
	return "\x1b]1337;SetBadgeFormat=\a"
}

func (itermAttentionProvider) initialSequence() string {
	return itermClearSessionBadge()
}

func (itermAttentionProvider) notifySequence(message string) string {
	return itermPostNotification(message)
}

func (itermAttentionProvider) requestAttentionSequence(value string) string {
	return itermRequestAttention(value)
}

func (itermAttentionProvider) clearSequence(attentionRequested bool) string {
	sequence := itermClearSessionBadge()
	if attentionRequested {
		sequence += itermRequestAttention("no")
	}
	return sequence
}

func itermPostNotification(message string) string {
	message = sanitizeOSCText(message)
	if message == "" {
		return ""
	}
	return "\x1b]9;" + message + "\a"
}

func itermRequestAttention(value string) string {
	return "\x1b]1337;RequestAttention=" + value + "\a"
}

func sanitizeOSCText(value string) string {
	return strings.NewReplacer("\x1b", "", "\a", "", "\r", " ", "\n", " ").Replace(strings.TrimSpace(value))
}

func normalizedTerminalAttentionRequest(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return terminalAttentionOnce
	}
	return value
}

func terminalAttentionProviderForEnv(env []string) terminalAttentionProvider {
	if terminalEnvIsIterm(env) {
		return itermAttentionProvider{}
	}
	return nil
}

func terminalEnvIsIterm(env []string) bool {
	return strings.EqualFold(terminalEnvValue(env, "TERM_PROGRAM"), "iTerm.app") ||
		strings.EqualFold(terminalEnvValue(env, "LC_TERMINAL"), "iTerm2") ||
		terminalEnvValue(env, "ITERM_SESSION_ID") != ""
}

func terminalEnvValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}
