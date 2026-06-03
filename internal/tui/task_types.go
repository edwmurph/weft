package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/tasktypes"
)

func taskTypeForTask(cfg config.Config, task state.Task) config.TaskType {
	taskType, ok := cfg.TaskType(state.TaskTypeID(task))
	if ok {
		return taskType
	}
	return config.TaskType{}
}

func taskDefinitionForTask(cfg config.Config, task state.Task) tasktypes.Definition {
	taskType := taskTypeForTask(cfg, task)
	if definition, ok := tasktypes.ForKind(taskType.Kind); ok {
		return definition
	}
	definition, _ := tasktypes.ForKind(tasktypes.KindTerminal)
	return definition
}

func taskInputModeForTask(cfg config.Config, task state.Task) tasktypes.InputMode {
	return taskDefinitionForTask(cfg, task).InputMode()
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

func taskRowPrefixWidth(cfg config.Config, task state.Task, nested bool) int {
	width := lipgloss.Width("·") + lipgloss.Width(" ") + taskSilentMarkerWidth(task) + taskTypeBadgeColumnWidth(cfg) + lipgloss.Width(" ")
	if nested {
		width += lipgloss.Width("  ")
	}
	return width
}

func taskSilentMarkerWidth(task state.Task) int {
	if task.Silent {
		return lipgloss.Width("⊘ ")
	}
	return 0
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

func renderNewTaskModal(cfg config.Config, selectedIndex int, input textinput.Model, width int, field int, silent bool, typeOpen bool) string {
	lines := []string{modalTitleStyle.Render("New task"), ""}
	lines = append(lines, renderNewTaskTypeField(cfg, selectedIndex, width, field == 0, typeOpen)...)
	lines = append(lines, "", renderSilentCheckbox(silent, field == 1), "")
	titleLines := renderPromptInput("Title", input, width)
	if field != 2 {
		titleLines[0] = mutedStyle.Render(titleLines[0])
	}
	lines = append(lines, titleLines...)
	if strings.TrimSpace(input.Value()) == "" {
		lines = append(lines, modalErrorStyle.Render("Title required"))
	}
	lines = append(lines, renderTitleTemplateVariables(width)...)
	lines = append(lines, "", renderNewTaskActions(typeOpen))
	return strings.Join(lines, "\n")
}

func renderNewTaskTypeField(cfg config.Config, selectedIndex int, width int, focused bool, open bool) []string {
	label := modalLabelStyle.Render("Type")
	if !focused {
		label = mutedStyle.Render(label)
	}
	valueWidth := max(16, width-4)
	taskType, ok := selectedTaskType(cfg, selectedIndex)
	value := "No task types configured"
	if ok {
		value = taskTypeLabel(taskType)
	}
	boxValue := padVisual(clip(value, valueWidth), valueWidth)
	if focused {
		boxValue = modalValueStyle.Render(boxValue)
	}
	style := modalInputStyle
	if focused {
		style = modalInputFocusedStyle
	}
	lines := []string{label, style.Width(valueWidth).Render(boxValue)}
	if !ok {
		lines = append(lines, modalErrorStyle.Render("No task types configured"))
		return lines
	}
	if !open {
		return lines
	}
	for index, taskType := range cfg.OrderedTaskTypes() {
		marker := "  "
		style := mutedStyle
		if index == selectedIndex {
			marker = "> "
			style = modalSuggestionSelectedStyle
		}
		option := padVisual(clip(marker+taskTypeLabel(taskType), valueWidth), valueWidth)
		lines = append(lines, " "+style.Render(option))
	}
	return lines
}

func renderSilentCheckbox(silent bool, focused bool) string {
	glyph := "[ ]"
	if silent {
		glyph = "[x]"
	}
	if focused {
		return modalKeyStyle.Render(glyph) + " " + modalValueStyle.Render("Silent")
	}
	return mutedStyle.Render(glyph + " Silent")
}

func taskTypeLabel(taskType config.TaskType) string {
	label := strings.TrimSpace(taskType.Label)
	if label == "" {
		label = strings.TrimSpace(taskType.ID)
	}
	return label
}

func renderNewTaskActions(typeOpen bool) string {
	if typeOpen {
		return modalKeyStyle.Render("Enter") + " choose  " + modalKeyStyle.Render("Tab") + " choose  " + modalKeyStyle.Render("Up/Down") + " type  " + modalKeyStyle.Render("Esc") + " close"
	}
	return modalKeyStyle.Render("Enter") + " create  " + modalKeyStyle.Render("Tab") + " move  " + modalKeyStyle.Render("Up/Down") + " move  " + modalKeyStyle.Render("Left/Right") + " type  " + modalKeyStyle.Render("Space") + " type/toggle  " + modalKeyStyle.Render("Esc") + " cancel"
}

type newTaskInputResult struct {
	index    int
	input    textinput.Model
	field    int
	silent   bool
	typeOpen bool
	submit   bool
	cancel   bool
	message  string
	cmd      tea.Cmd
}

func handleNewTaskKey(cfg config.Config, index int, input textinput.Model, field int, silent bool, typeOpen bool, msg tea.KeyMsg) newTaskInputResult {
	result := newTaskInputResult{index: index, input: input, field: field, silent: silent, typeOpen: typeOpen}
	keyText := strings.ToLower(msg.String())
	if result.typeOpen {
		switch {
		case msg.Type == tea.KeyEsc:
			result.typeOpen = false
			return result
		case msg.Type == tea.KeyEnter || msg.Type == tea.KeyTab || keyText == "tab":
			result.typeOpen = false
			if msg.Type == tea.KeyTab || keyText == "tab" {
				result.field = 1
				focusNewTaskInput(&result.input, result.field)
			}
			return result
		case msg.Type == tea.KeyUp || bindingMatches(cfg.KeyBindings.SelectPrev, msg):
			result.index = newTaskTypeIndexAfterMove(cfg, index, &result.input, -1)
			return result
		case msg.Type == tea.KeyDown || bindingMatches(cfg.KeyBindings.SelectNext, msg):
			result.index = newTaskTypeIndexAfterMove(cfg, index, &result.input, 1)
			return result
		default:
			return result
		}
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
	case msg.Type == tea.KeyTab || keyText == "tab":
		result.field = (result.field + 1) % 3
		focusNewTaskInput(&result.input, result.field)
		return result
	case msg.Type == tea.KeyUp || bindingMatches(cfg.KeyBindings.SelectPrev, msg):
		result.field = max(0, result.field-1)
		focusNewTaskInput(&result.input, result.field)
		return result
	case msg.Type == tea.KeyDown || bindingMatches(cfg.KeyBindings.SelectNext, msg):
		result.field = min(2, result.field+1)
		focusNewTaskInput(&result.input, result.field)
		return result
	case result.field == 0 && (msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight):
		delta := -1
		if msg.Type == tea.KeyRight {
			delta = 1
		}
		result.index = newTaskTypeIndexAfterMove(cfg, index, &result.input, delta)
		return result
	case result.field == 0:
		if msg.Type == tea.KeySpace || msg.String() == " " || keyText == "space" {
			result.typeOpen = true
		}
		return result
	case result.field == 1:
		if msg.Type == tea.KeySpace || msg.String() == " " || keyText == "space" {
			result.silent = !result.silent
		}
		return result
	default:
		if handlePromptWordKey(&result.input, msg) {
			return result
		}
		result.input, result.cmd = result.input.Update(msg)
		return result
	}
}

func focusNewTaskInput(input *textinput.Model, field int) {
	if field == 2 {
		input.Focus()
	} else {
		input.Blur()
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
