package titles

import (
	"strings"
	"unicode"

	"github.com/edwmurph/weft/internal/state"
)

const (
	TitleTemplate     = "{title}"
	AutoTemplate      = "{auto}"
	CodexTemplate     = "{codex}"
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
		{Name: TitleTemplate, Description: "configured agent title"},
		{Name: AutoTemplate, Description: "generated title from first message"},
		{Name: CodexTemplate, Description: "live Codex title"},
		{Name: StatusTemplate, Description: "live Codex status"},
		{Name: WorkspaceTemplate, Description: "workspace path"},
		{Name: GroupTemplate, Description: "group name"},
	}
}

func NormalizeCodexTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" || title == "Terminal" || title == "tmux" {
		return ""
	}
	return title
}

func RenderStatus(agent state.Agent) string {
	switch agent.Status {
	case state.StatusStarting, state.StatusStopped, state.StatusError, state.StatusReady, state.StatusSitting, state.StatusShipping:
		return string(agent.Status)
	}
	if status := codexActivityStatus(agent.CodexTitle); status != "" {
		return status
	}
	if agent.Status != "" {
		return string(agent.Status)
	}
	return "unknown"
}

func codexActivityStatus(title string) string {
	title = strings.ToLower(NormalizeCodexTitle(title))
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

func RenderAgent(agent state.Agent, workspace state.Workspace, group state.Group, template string) string {
	if strings.TrimSpace(template) == "" {
		template = TitleTemplate
	}
	title := strings.TrimSpace(agent.Title)
	if title == "" {
		title = PendingTitle
	}
	codexTitle := NormalizeCodexTitle(agent.CodexTitle)
	if codexTitle == "" {
		codexTitle = PendingTitle
	}
	values := map[string]string{
		TitleTemplate:     title,
		AutoTemplate:      renderAutoTitle(agent),
		CodexTemplate:     codexTitle,
		StatusTemplate:    RenderStatus(agent),
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

func renderAutoTitle(agent state.Agent) string {
	if title := strings.TrimSpace(agent.AutoTitle); title != "" {
		return title
	}
	if strings.TrimSpace(agent.AutoTitleError) != "" {
		return AutoFailed
	}
	if agent.AutoTitleAttempted {
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
