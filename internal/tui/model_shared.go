package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/tasktypes"
	"github.com/edwmurph/weft/internal/titles"
)

func promptContextFor(prompt promptKind, pendingID string, st state.State, selectedTask *state.Task) promptContext {
	return promptContext{
		prompt:       prompt,
		pendingID:    pendingID,
		state:        st,
		selectedTask: selectedTask,
	}
}

func confirmKeySubmits(confirm confirmKind, msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyEnter
}

func confirmKeyCancels(confirm confirmKind, msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyEsc || strings.EqualFold(msg.String(), "esc") {
		return true
	}
	if confirm == confirmDeleteTask {
		return strings.EqualFold(msg.String(), "n")
	}
	return false
}

func renderPromptExtraForState(cfg config.Config, st state.State, prompt promptKind, selectedTask *state.Task, input textinput.Model, width int) []string {
	if prompt != promptEditTask {
		return nil
	}
	lines := []string{"", modalLabelStyle.Render("Preview")}
	if selectedTask != nil {
		draft := *selectedTask
		if value := strings.TrimSpace(input.Value()); value != "" {
			draft.Title = value
		}
		lines = append(lines, modalValueStyle.Render(clip(renderTaskBaseTitleForState(st, draft), width)))
		if notice := autoTitleNotice(cfg, *selectedTask, draft.Title); notice != "" {
			lines = append(lines, renderWrappedPromptNotice(notice, width)...)
		}
	}
	lines = append(lines, renderTitleTemplateVariables(width)...)
	return lines
}

func renderTitleTemplateVariables(width int) []string {
	lines := []string{"", modalLabelStyle.Render("Variables")}
	for _, variable := range titles.TemplateVariables() {
		lines = append(lines, mutedStyle.Render(clip(fmt.Sprintf("- %s: %s", variable.Name, variable.Description), width)))
	}
	return lines
}

func renderWrappedPromptNotice(notice string, width int) []string {
	wrapped := wrapPlain(notice, width, max(1, len([]rune(notice))))
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, mutedStyle.Render(line))
	}
	return lines
}

func editPromptTargetForState(st state.State, groupCursor int) (promptKind, string, string, bool, bool) {
	if st.Focus == state.FocusWorkspaces {
		if workspace := state.WorkspaceByID(st, st.SelectedWorkspaceID); workspace != nil {
			return promptWorkspaceTitle, workspace.ID, workspace.Title, false, true
		}
		return "", "", "", false, false
	}
	row := currentGroupRowForState(st, groupCursor)
	switch row.kind {
	case groupRowGroup:
		if group := state.GroupByID(st, row.groupID); group != nil {
			return promptEditGroup, group.ID, group.Path, group.Silent, true
		}
	case groupRowTask:
		if task := state.TaskByID(st, row.taskID); task != nil {
			return promptEditTask, task.ID, task.Title, task.Silent, true
		}
	}
	return "", "", "", false, false
}

func deleteConfirmTargetForState(st state.State, groupCursor int) (confirmKind, string, bool) {
	if st.Focus == state.FocusWorkspaces {
		if workspace := state.WorkspaceByID(st, st.SelectedWorkspaceID); workspace != nil {
			return confirmDeleteWorkspace, workspace.ID, true
		}
		return "", "", false
	}
	row := currentGroupRowForState(st, groupCursor)
	switch row.kind {
	case groupRowGroup:
		if group := state.GroupByID(st, row.groupID); group != nil {
			return confirmDeleteGroup, group.ID, true
		}
	case groupRowTask:
		if task := state.TaskByID(st, row.taskID); task != nil {
			return confirmDeleteTask, task.ID, true
		}
	}
	return "", "", false
}

func selectedTaskForState(st state.State, groupCursor int) *state.Task {
	row := currentGroupRowForState(st, groupCursor)
	if row.kind == groupRowTask {
		return state.TaskByID(st, row.taskID)
	}
	return nil
}

func currentGroupRowForState(st state.State, groupCursor int) groupRow {
	rows := groupRowsForState(st)
	if len(rows) == 0 {
		return groupRow{}
	}
	if groupCursor < 0 || groupCursor >= len(rows) {
		return rows[0]
	}
	return rows[groupCursor]
}

func groupRowsForState(st state.State) []groupRow {
	var rows []groupRow
	if state.ActiveWorkspace(st) != nil {
		rows = append(rows, groupRow{kind: groupRowNewTask})
	}
	for _, task := range state.UngroupedTasksForWorkspace(st, st.SelectedWorkspaceID) {
		rows = append(rows, groupRow{kind: groupRowTask, taskID: task.ID})
	}
	for _, group := range state.GroupsForWorkspace(st, st.SelectedWorkspaceID) {
		rows = append(rows, groupRow{kind: groupRowGroup, groupID: group.ID})
		if state.IsGroupCollapsed(st, group.ID) {
			continue
		}
		for _, task := range state.TasksForGroup(st, group.ID) {
			rows = append(rows, groupRow{kind: groupRowTask, groupID: group.ID, taskID: task.ID})
		}
	}
	return rows
}

func renderTaskTitleForState(_ config.Config, st state.State, task state.Task) string {
	return renderTaskWithTemplate(st, task, task.Title)
}

func renderTaskBaseTitleForState(st state.State, task state.Task) string {
	return renderTaskWithTemplate(st, task, titles.TitleTemplate)
}

func renderTaskWithTemplate(st state.State, task state.Task, template string) string {
	workspace := state.Workspace{}
	group := state.Group{}
	if w := state.WorkspaceForTask(st, task); w != nil {
		workspace = *w
	}
	if f := state.GroupForTask(st, task); f != nil {
		group = *f
	}
	return titles.RenderTask(task, workspace, group, template)
}

func taskStatusIndicatesActivity(task state.Task) bool {
	return titles.StatusIndicatesActivity(task)
}

func taskStatusShowsLoadingIndicator(task state.Task) bool {
	return tasktypes.StatusShowsLoadingIndicator(task)
}

func autoTitleNotice(cfg config.Config, task state.Task, draftTitle string) string {
	if !strings.Contains(draftTitle, titles.AutoTemplate) {
		return ""
	}
	if strings.TrimSpace(task.AutoTitle) != "" {
		return "Auto title is ready."
	}
	if strings.TrimSpace(task.AutoTitleError) != "" {
		return "Auto title error: " + task.AutoTitleError
	}
	if task.AutoTitleAttempted {
		return "Auto title is generating."
	}
	if strings.TrimSpace(cfg.TitleHookCommand) == "" {
		return "Auto title unavailable: set title_hook_command."
	}
	switch taskInputModeForTask(cfg, task) {
	case tasktypes.InputModeTerminal:
		return "Auto title will generate from the first submitted command."
	default:
		return "Auto title will generate from the first submitted message."
	}
}
