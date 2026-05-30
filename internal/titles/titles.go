package titles

import (
	"strings"
	"unicode"

	"github.com/edwmurph/codux/internal/state"
)

const (
	CodexTitleTemplate = "{codex}"
	StatusTemplate     = "{status}"
	PendingTitle       = "..."
)

type TemplateVariable struct {
	Name        string
	Description string
}

func TemplateVariables() []TemplateVariable {
	return []TemplateVariable{
		{Name: CodexTitleTemplate, Description: "live Codex title"},
		{Name: StatusTemplate, Description: "live Codex status"},
	}
}

func NormalizeCodexTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" || title == "Terminal" || title == "tmux" {
		return ""
	}
	return title
}

func UsesCodexPlaceholder(title string) bool {
	return strings.Contains(title, "{codex}")
}

func RenderStatus(tab state.Tab) string {
	switch tab.Status {
	case state.StatusStarting, state.StatusStopped, state.StatusError:
		return string(tab.Status)
	}
	if status := codexActivityStatus(tab.CodexTitle); status != "" {
		return status
	}
	if tab.Status != "" {
		return string(tab.Status)
	}
	return string(state.StatusStarting)
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

func Render(tab state.Tab) string {
	title := tab.Title
	if title == "" {
		title = CodexTitleTemplate
	}
	codexTitle := NormalizeCodexTitle(tab.CodexTitle)
	if codexTitle == "" {
		codexTitle = PendingTitle
	}
	replacements := []struct {
		variable string
		value    string
	}{
		{CodexTitleTemplate, codexTitle},
		{StatusTemplate, RenderStatus(tab)},
	}
	for _, replacement := range replacements {
		title = strings.ReplaceAll(title, replacement.variable, replacement.value)
	}
	return strings.TrimSpace(title)
}
