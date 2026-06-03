package tasktypes

import (
	"sort"
	"strings"
	"unicode"

	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/titles"
)

const (
	DefaultCodexID = "codex"
	DefaultShellID = "shell"
	KindCodex      = "codex"
	KindTerminal   = "terminal"
)

type InputMode string

const (
	InputModeCodex    InputMode = "codex"
	InputModeTerminal InputMode = "terminal"
)

type StartPolicy struct {
	Status         state.TaskStatus
	Visible        bool
	TrackOperation bool
}

type LoadingContext struct {
	Active        bool
	ScreenVisible bool
}

type Definition interface {
	Kind() string
	ConfiguredTypeID() string
	InputMode() InputMode
	StartPolicy() StartPolicy
	Command(baseCommand string, task state.Task) string
	ScreenStatus(screenContent string) string
	ApplyPTYTitle(task state.Task, terminalTitle string, screenStatus string) state.Task
	Loading(task state.Task, ctx LoadingContext) bool
	TracksSessions() bool
	TracksTerminalCWD() bool
	TracksForegroundCommands() bool
	ShowsExitFooter() bool
	TopAlignedResize() bool
	RestartableTerminal() bool
}

var registry = map[string]Definition{
	KindCodex:    codexDefinition{},
	KindTerminal: terminalDefinition{},
}

func ForKind(kind string) (Definition, bool) {
	definition, ok := registry[strings.TrimSpace(kind)]
	return definition, ok
}

func SupportedKinds() []string {
	kinds := make([]string, 0, len(registry))
	for kind := range registry {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

func StatusShowsLoadingIndicator(task state.Task) bool {
	switch titles.ConsolidatedStatus(task) {
	case string(state.StatusReady), string(state.StatusStopped), string(state.StatusKilled), string(state.StatusError), string(state.StatusSitting):
		return false
	default:
		return true
	}
}

type codexDefinition struct{}

func (codexDefinition) Kind() string {
	return KindCodex
}

func (codexDefinition) ConfiguredTypeID() string {
	return DefaultCodexID
}

func (codexDefinition) InputMode() InputMode {
	return InputModeCodex
}

func (codexDefinition) StartPolicy() StartPolicy {
	return StartPolicy{Status: state.StatusRunning, TrackOperation: true}
}

func (codexDefinition) Command(baseCommand string, task state.Task) string {
	if strings.TrimSpace(task.ResumeID) == "" {
		return strings.TrimSpace(baseCommand)
	}
	return CodexResumeCommand(baseCommand, task.ResumeID)
}

func (codexDefinition) ScreenStatus(screenContent string) string {
	return codexScreenStatus(screenContent)
}

func (codexDefinition) ApplyPTYTitle(task state.Task, terminalTitle string, screenStatus string) state.Task {
	hadLiveStatus := task.LiveStatus != ""
	if terminalTitle != "" {
		task.LiveTitle = titles.NormalizeLiveTitle(terminalTitle)
		task.Status = state.StatusRunning
	}
	titleStatus := titles.CodexActivityStatus(task.LiveTitle)
	titleIndicatesActivity := titles.LiveStatusIndicatesActivity(titleStatus)
	switch {
	case screenStatus != "":
		if titleIndicatesActivity {
			task.LiveStatus = titleStatus
		} else {
			task.LiveStatus = screenStatus
			task.Status = state.StatusReady
		}
	case titleStatus != "":
		task.LiveStatus = titleStatus
		if task.Status == state.StatusReady {
			task.Status = state.StatusRunning
		}
	case hadLiveStatus:
		task.LiveStatus = ""
		if task.Status == state.StatusReady {
			task.Status = state.StatusRunning
		}
	}
	return task
}

func (codexDefinition) Loading(task state.Task, ctx LoadingContext) bool {
	switch titles.ConsolidatedStatus(task) {
	case string(state.StatusError), string(state.StatusStopped), string(state.StatusKilled), string(state.StatusSitting):
		return false
	}
	if !ctx.Active {
		return StatusShowsLoadingIndicator(task)
	}
	if StatusShowsLoadingIndicator(task) {
		return true
	}
	return !ctx.ScreenVisible
}

func (codexDefinition) TracksSessions() bool {
	return true
}

func (codexDefinition) TracksTerminalCWD() bool {
	return false
}

func (codexDefinition) TracksForegroundCommands() bool {
	return false
}

func (codexDefinition) ShowsExitFooter() bool {
	return false
}

func (codexDefinition) TopAlignedResize() bool {
	return false
}

func (codexDefinition) RestartableTerminal() bool {
	return false
}

type terminalDefinition struct{}

func (terminalDefinition) Kind() string {
	return KindTerminal
}

func (terminalDefinition) ConfiguredTypeID() string {
	return ""
}

func (terminalDefinition) InputMode() InputMode {
	return InputModeTerminal
}

func (terminalDefinition) StartPolicy() StartPolicy {
	return StartPolicy{Status: state.StatusReady, Visible: true}
}

func (terminalDefinition) Command(baseCommand string, _ state.Task) string {
	return strings.TrimSpace(baseCommand)
}

func (terminalDefinition) ScreenStatus(string) string {
	return ""
}

func (terminalDefinition) ApplyPTYTitle(task state.Task, _ string, _ string) state.Task {
	return task
}

func (terminalDefinition) Loading(task state.Task, _ LoadingContext) bool {
	return StatusShowsLoadingIndicator(task)
}

func (terminalDefinition) TracksSessions() bool {
	return false
}

func (terminalDefinition) TracksTerminalCWD() bool {
	return true
}

func (terminalDefinition) TracksForegroundCommands() bool {
	return true
}

func (terminalDefinition) ShowsExitFooter() bool {
	return true
}

func (terminalDefinition) TopAlignedResize() bool {
	return true
}

func (terminalDefinition) RestartableTerminal() bool {
	return true
}

func CodexResumeCommand(codexCommand string, sessionID string) string {
	codexCommand = strings.TrimSpace(codexCommand)
	if codexCommand == "" {
		codexCommand = DefaultCodexID
	}
	return codexCommand + " resume " + shellQuote(sessionID)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func codexScreenStatus(content string) string {
	content = strings.ToLower(content)
	contentKey := screenStatusKey(content)
	hasSubmitAction := strings.Contains(content, "to submit answer") ||
		strings.Contains(content, "to submit all")
	hasQuestionPrompt := strings.Contains(content, "question ") ||
		strings.Contains(content, "unanswered") ||
		strings.Contains(content, "user_note:")
	if hasSubmitAction && hasQuestionPrompt {
		return "Ready"
	}
	hasPermissionPrompt := strings.Contains(content, "allow codex to ") &&
		strings.Contains(content, "allow this request") &&
		strings.Contains(content, "deny") &&
		strings.Contains(content, "enter to submit")
	if hasPermissionPrompt {
		return "Ready"
	}
	hasCommandApprovalPrompt := strings.Contains(contentKey, "wouldyouliketorunthefollowingcommand?") &&
		strings.Contains(contentKey, "yes,proceed") &&
		strings.Contains(contentKey, "no,andtellcodex")
	if hasCommandApprovalPrompt {
		return "Ready"
	}
	return ""
}

func screenStatusKey(content string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("╭╮╰╯─│", r) {
			return -1
		}
		return r
	}, content)
}
