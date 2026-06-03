package titles

import (
	"strings"
	"unicode"

	"github.com/edwmurph/weft/internal/state"
)

const (
	TitleTemplate     = "{title}"
	AutoTemplate      = "{auto}"
	LiveTemplate      = "{live}"
	StatusTemplate    = "{status}"
	WorkspaceTemplate = "{workspace}"
	GroupTemplate     = "{group}"
	PendingTitle      = "..."
	AutoPending       = "waiting for first message"
	AutoFailed        = "auto title failed"
)

type TemplateVariable struct {
	Name        string
	Description string
}

func TemplateVariables() []TemplateVariable {
	return []TemplateVariable{
		{Name: TitleTemplate, Description: "configured task title"},
		{Name: AutoTemplate, Description: "generated title from first message"},
		{Name: LiveTemplate, Description: "live task title"},
		{Name: StatusTemplate, Description: "live task status"},
		{Name: WorkspaceTemplate, Description: "workspace path"},
		{Name: GroupTemplate, Description: "group name"},
	}
}

func NormalizeLiveTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" || title == "Terminal" || title == "tmux" {
		return ""
	}
	return title
}

func RenderStatus(task state.Task) string {
	switch task.Status {
	case state.StatusStarting, state.StatusStopped, state.StatusKilled, state.StatusError, state.StatusSitting, state.StatusShipping:
		return string(task.Status)
	}
	liveStatus := strings.TrimSpace(task.LiveStatus)
	if liveStatus != "" {
		return liveStatus
	}
	if task.Status != "" {
		return string(task.Status)
	}
	return "unknown"
}

func CanonicalStatus(task state.Task) string {
	return strings.ToLower(RenderStatus(task))
}

func ConsolidatedStatus(task state.Task) string {
	return consolidateStatus(CanonicalStatus(task))
}

func StatusIndicatesActivity(task state.Task) bool {
	switch ConsolidatedStatus(task) {
	case string(state.StatusStarting), string(state.StatusRunning), "waiting", "working", string(state.StatusShipping):
		return true
	case string(state.StatusReady), string(state.StatusStopped), string(state.StatusKilled), string(state.StatusError), string(state.StatusSitting):
		return false
	default:
		return true
	}
}

func consolidateStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case string(state.StatusStarting), string(state.StatusRunning), "waiting", "working", string(state.StatusShipping), string(state.StatusReady), string(state.StatusStopped), string(state.StatusKilled), string(state.StatusError), string(state.StatusSitting):
		return status
	case "idle", "complete", "completed", "done":
		return string(state.StatusReady)
	case "failed", "failure":
		return string(state.StatusError)
	case "":
		return string(state.StatusError)
	default:
		return "working"
	}
}

func LiveStatusIndicatesActivity(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", string(state.StatusRunning), string(state.StatusReady), "waiting", "idle", string(state.StatusStopped), string(state.StatusKilled), string(state.StatusError), string(state.StatusSitting):
		return false
	default:
		return true
	}
}

func CodexActivityStatus(title string) string {
	title = NormalizeLiveTitle(title)
	tokens := codexTitleTokens(title)
	if len(tokens) == 1 {
		if strings.EqualFold(tokens[0], "codex") {
			return ""
		}
		if codexStatusCandidate(tokens[0]) {
			return tokens[0]
		}
	}
	for index, token := range tokens {
		if !strings.EqualFold(token, "codex") {
			continue
		}
		if index < len(tokens)-1 {
			candidate := tokens[len(tokens)-1]
			if codexStatusCandidate(candidate) {
				return candidate
			}
		}
	}
	for _, token := range tokens {
		switch strings.ToLower(token) {
		case "ready", "waiting", "idle", "working", "shipping", "starting":
			return token
		}
	}
	return ""
}

func codexTitleTokens(title string) []string {
	return strings.FieldsFunc(title, func(r rune) bool {
		return !unicode.IsLetter(r)
	})
}

func codexStatusCandidate(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || strings.EqualFold(token, "codex") {
		return false
	}
	for _, r := range token {
		return unicode.IsUpper(r)
	}
	return false
}

func RenderTask(task state.Task, workspace state.Workspace, group state.Group, template string) string {
	if strings.TrimSpace(template) == "" {
		template = TitleTemplate
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = PendingTitle
	}
	liveTitle := NormalizeLiveTitle(task.LiveTitle)
	if liveTitle == "" {
		liveTitle = PendingTitle
	}
	values := map[string]string{
		TitleTemplate:     title,
		AutoTemplate:      renderAutoTitle(task),
		LiveTemplate:      liveTitle,
		StatusTemplate:    RenderStatus(task),
		WorkspaceTemplate: fallback(workspace.Path, PendingTitle),
		GroupTemplate:     fallback(group.Path, PendingTitle),
	}
	renderedTitle := replaceVariables(title, values)
	values[TitleTemplate] = renderedTitle
	rendered := replaceVariables(template, values)
	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return PendingTitle
	}
	return rendered
}

func renderAutoTitle(task state.Task) string {
	if title := strings.TrimSpace(task.AutoTitle); title != "" {
		return title
	}
	if strings.TrimSpace(task.AutoTitleError) != "" {
		return AutoFailed
	}
	if task.AutoTitleAttempted {
		return "generating auto title"
	}
	return AutoPending
}

func replaceVariables(value string, values map[string]string) string {
	for _, variable := range TemplateVariables() {
		replacement := values[variable.Name]
		if replacement == "" {
			switch variable.Name {
			case StatusTemplate:
				replacement = "unknown"
			default:
				replacement = PendingTitle
			}
		}
		value = strings.ReplaceAll(value, variable.Name, replacement)
	}
	return value
}

func fallback(value string, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}
