package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
)

func taskTypeForAgent(cfg config.Config, agent state.Agent) config.TaskType {
	return cfg.TaskTypeOrDefault(state.AgentTypeID(agent))
}

func agentUsesCodexIntegration(cfg config.Config, agent state.Agent) bool {
	return taskTypeForAgent(cfg, agent).Kind == config.TaskKindCodex
}

func taskTypeBadgeForAgent(cfg config.Config, agent state.Agent) string {
	return taskTypeBadge(taskTypeForAgent(cfg, agent))
}

func taskTypeBadgeCellForAgent(cfg config.Config, agent state.Agent) string {
	return taskTypeBadgeCell(cfg, taskTypeForAgent(cfg, agent))
}

func taskTypeBadge(taskType config.TaskType) string {
	badge := strings.TrimSpace(taskType.Badge)
	if badge == "" {
		badge = strings.TrimSpace(taskType.Icon)
	}
	if badge == "" && strings.TrimSpace(taskType.ID) != "" {
		badge = "[" + strings.TrimSpace(taskType.ID) + "]"
	}
	if badge == "" {
		return "[?]"
	}
	return badge
}

func taskTypeBadgeColumnWidth(cfg config.Config) int {
	width := 0
	for _, taskType := range cfg.OrderedTaskTypes() {
		width = max(width, lipgloss.Width(taskTypeBadge(taskType)))
	}
	return max(width, lipgloss.Width("[?]"))
}

func taskTypeBadgeCell(cfg config.Config, taskType config.TaskType) string {
	return padVisual(taskTypeBadge(taskType), taskTypeBadgeColumnWidth(cfg))
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
