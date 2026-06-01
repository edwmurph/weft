package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
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

func configureNewTaskTitleInput(input *textinput.Model, cfg config.Config, selectedIndex int) {
	input.KeyMap = promptInputKeyMap()
	input.SetValue(defaultNewTaskTitle(cfg, selectedIndex))
	input.CursorEnd()
	input.Focus()
	input.Placeholder = "Title"
	input.ShowSuggestions = true
	input.SetSuggestions(nil)
	input.ShowSuggestions = false
}

func defaultNewTaskTitle(cfg config.Config, selectedIndex int) string {
	taskType, ok := selectedTaskType(cfg, selectedIndex)
	if !ok {
		return ""
	}
	return taskType.TitleTemplate
}

func renderNewTaskModal(cfg config.Config, selectedIndex int, input textinput.Model, width int) string {
	taskTypes := cfg.OrderedTaskTypes()
	lines := []string{modalTitleStyle.Render("New task"), "", modalLabelStyle.Render("Type")}
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
	if len(taskTypes) == 0 {
		lines = append(lines, modalErrorStyle.Render("No task types configured"))
	}
	lines = append(lines, "")
	lines = append(lines, renderPromptInput("Title", input, width)...)
	if strings.TrimSpace(input.Value()) == "" {
		lines = append(lines, modalErrorStyle.Render("Title required"))
	}
	lines = append(lines, "", modalKeyStyle.Render("Enter")+" create  "+modalKeyStyle.Render("Up/Down")+" type  "+modalKeyStyle.Render("Esc")+" cancel")
	return strings.Join(lines, "\n")
}

type newTaskInputResult struct {
	index   int
	input   textinput.Model
	submit  bool
	cancel  bool
	message string
	cmd     tea.Cmd
}

func handleNewTaskKey(cfg config.Config, index int, input textinput.Model, msg tea.KeyMsg) newTaskInputResult {
	result := newTaskInputResult{index: index, input: input}
	if handlePromptWordKey(&result.input, msg) {
		return result
	}
	switch {
	case msg.Type == tea.KeyEsc:
		result.cancel = true
		return result
	case msg.Type == tea.KeyEnter:
		if strings.TrimSpace(result.input.Value()) == "" {
			result.message = "title is required"
			return result
		}
		result.submit = true
		return result
	case msg.Type == tea.KeyUp || bindingMatches(cfg.KeyBindings.SelectPrev, msg):
		result.index = newTaskTypeIndexAfterMove(cfg, index, &result.input, -1)
		return result
	case msg.Type == tea.KeyDown || bindingMatches(cfg.KeyBindings.SelectNext, msg):
		result.index = newTaskTypeIndexAfterMove(cfg, index, &result.input, 1)
		return result
	default:
		result.input, result.cmd = result.input.Update(msg)
		return result
	}
}

func newTaskTypeIndexAfterMove(cfg config.Config, index int, input *textinput.Model, delta int) int {
	oldDefault := strings.TrimSpace(defaultNewTaskTitle(cfg, index))
	title := strings.TrimSpace(input.Value())
	next := moveTaskTypeIndex(index, delta, cfg)
	if title == "" || title == oldDefault {
		input.SetValue(defaultNewTaskTitle(cfg, next))
		input.CursorEnd()
	}
	return next
}
