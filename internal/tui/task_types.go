package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
)

func taskTypeForTask(cfg config.Config, task state.Task) config.TaskType {
	taskType, ok := cfg.TaskType(state.TaskTypeID(task))
	if ok {
		return taskType
	}
	return config.TaskType{}
}

func taskUsesCodexIntegration(cfg config.Config, task state.Task) bool {
	return taskTypeForTask(cfg, task).Kind == config.TaskKindCodex
}

func taskTypeBadgeForTask(cfg config.Config, task state.Task) string {
	return taskTypeBadge(taskTypeForTask(cfg, task))
}

func taskTypeBadgeCellForTask(cfg config.Config, task state.Task) string {
	return taskTypeBadgeCell(cfg, taskTypeForTask(cfg, task))
}

func taskTypeBadge(taskType config.TaskType) string {
	return strings.TrimSpace(taskType.Badge)
}

func taskTypeBadgeColumnWidth(cfg config.Config) int {
	width := 0
	for _, taskType := range cfg.OrderedTaskTypes() {
		width = max(width, lipgloss.Width(taskTypeBadge(taskType)))
	}
	return width
}

func taskTypeBadgeCell(cfg config.Config, taskType config.TaskType) string {
	return padVisual(taskTypeBadge(taskType), taskTypeBadgeColumnWidth(cfg))
}

func taskRowPrefixWidth(cfg config.Config, nested bool) int {
	width := lipgloss.Width("·") + lipgloss.Width(" ") + taskTypeBadgeColumnWidth(cfg) + lipgloss.Width(" ")
	if nested {
		width += lipgloss.Width("  ")
	}
	return width
}

func defaultTaskTypeIndex(cfg config.Config) int {
	taskTypes := cfg.OrderedTaskTypes()
	for index, taskType := range taskTypes {
		if taskType.ID == cfg.DefaultTaskType {
			return index
		}
	}
	return 0
}

func moveTaskTypeIndex(index int, delta int, cfg config.Config) int {
	count := len(cfg.OrderedTaskTypes())
	if count == 0 {
		return 0
	}
	next := (index + delta) % count
	if next < 0 {
		next += count
	}
	return next
}

func selectedTaskType(cfg config.Config, index int) (config.TaskType, bool) {
	taskTypes := cfg.OrderedTaskTypes()
	if len(taskTypes) == 0 {
		return config.TaskType{}, false
	}
	index = max(0, min(index, len(taskTypes)-1))
	return taskTypes[index], true
}

func renderNewTaskModal(cfg config.Config, selectedIndex int, width int) string {
	taskTypes := cfg.OrderedTaskTypes()
	lines := []string{modalTitleStyle.Render("New task"), ""}
	valueWidth := max(16, width-2)
	for index, taskType := range taskTypes {
		marker := "  "
		style := modalValueStyle
		if index == selectedIndex {
			marker = "> "
			style = modalKeyStyle
		}
		label := strings.TrimSpace(taskType.Label)
		if label == "" {
			label = strings.TrimSpace(taskType.ID)
		}
		lines = append(lines, style.Render(padVisual(clip(marker+label, valueWidth), valueWidth)))
	}
	lines = append(lines, "", modalKeyStyle.Render("Enter")+" create  "+modalKeyStyle.Render("Up/Down")+" move  "+modalKeyStyle.Render("Esc")+" cancel")
	return strings.Join(lines, "\n")
}

func handleNewTaskKey(cfg config.Config, index int, msg tea.KeyMsg) (int, bool, bool) {
	switch {
	case msg.Type == tea.KeyEsc:
		return index, false, true
	case msg.Type == tea.KeyEnter:
		return index, true, false
	case msg.Type == tea.KeyUp || bindingMatches(cfg.KeyBindings.SelectPrev, msg):
		return moveTaskTypeIndex(index, -1, cfg), false, false
	case msg.Type == tea.KeyDown || bindingMatches(cfg.KeyBindings.SelectNext, msg):
		return moveTaskTypeIndex(index, 1, cfg), false, false
	default:
		return index, false, false
	}
}
