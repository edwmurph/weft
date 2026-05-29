package titles

import (
	"strings"

	"github.com/edwmurph/codux/internal/state"
)

const (
	CodexTitleTemplate = "{codex}"
	PendingTitle       = "..."
)

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

func Render(tab state.Tab) string {
	title := tab.Title
	if title == "" {
		title = CodexTitleTemplate
	}
	codexTitle := NormalizeCodexTitle(tab.CodexTitle)
	if codexTitle == "" {
		codexTitle = PendingTitle
	}
	replacements := map[string]string{
		"{codex}":  codexTitle,
		"{title}":  tab.Title,
		"{id}":     tab.ID,
		"{column}": tab.Column,
		"{status}": string(tab.Status),
	}
	for variable, value := range replacements {
		title = strings.ReplaceAll(title, variable, value)
	}
	return strings.TrimSpace(title)
}
