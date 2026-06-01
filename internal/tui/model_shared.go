package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/titles"
)

func promptContextFor(prompt promptKind, pendingID string, st state.State, selectedAgent *state.Agent) promptContext {
	return promptContext{
		prompt:        prompt,
		pendingID:     pendingID,
		state:         st,
		selectedAgent: selectedAgent,
	}
}

func confirmKeySubmits(confirm confirmKind, msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyEnter
}

func confirmKeyCancels(confirm confirmKind, msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyEsc || strings.EqualFold(msg.String(), "esc") {
		return true
	}
	if confirm == confirmDeleteAgent {
		return strings.EqualFold(msg.String(), "n")
	}
	return false
}

func renderPromptExtraForState(cfg config.Config, st state.State, prompt promptKind, selectedAgent *state.Agent, input textinput.Model, width int) []string {
	if prompt != promptEditAgent {
		return nil
	}
	lines := []string{"", modalLabelStyle.Render("Preview")}
	if selectedAgent != nil {
		draft := *selectedAgent
		if value := strings.TrimSpace(input.Value()); value != "" {
			draft.Title = value
		}
		lines = append(lines, modalValueStyle.Render(clip(renderAgentBaseTitleForState(st, draft), width)))
		if notice := autoTitleNotice(cfg, *selectedAgent, draft.Title); notice != "" {
			lines = append(lines, mutedStyle.Render(clip(notice, width)))
		}
	}
	lines = append(lines, "", modalLabelStyle.Render("Variables"))
	for _, variable := range titles.TemplateVariables() {
		lines = append(lines, mutedStyle.Render(clip(fmt.Sprintf("- %s: %s", variable.Name, variable.Description), width)))
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
	case groupRowAgent:
		if agent := state.AgentByID(st, row.agentID); agent != nil {
			return promptEditAgent, agent.ID, agent.Title, false, true
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
	case groupRowAgent:
		if agent := state.AgentByID(st, row.agentID); agent != nil {
			return confirmDeleteAgent, agent.ID, true
		}
	}
	return "", "", false
}

func selectedAgentForState(st state.State, groupCursor int) *state.Agent {
	row := currentGroupRowForState(st, groupCursor)
	if row.kind == groupRowAgent {
		return state.AgentByID(st, row.agentID)
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
	for _, agent := range state.UngroupedAgentsForWorkspace(st, st.SelectedWorkspaceID) {
		rows = append(rows, groupRow{kind: groupRowAgent, agentID: agent.ID})
	}
	for _, group := range state.GroupsForWorkspace(st, st.SelectedWorkspaceID) {
		rows = append(rows, groupRow{kind: groupRowGroup, groupID: group.ID})
		if state.IsGroupCollapsed(st, group.ID) {
			continue
		}
		for _, agent := range state.AgentsForGroup(st, group.ID) {
			rows = append(rows, groupRow{kind: groupRowAgent, groupID: group.ID, agentID: agent.ID})
		}
	}
	return rows
}

func renderAgentTitleForState(_ config.Config, st state.State, agent state.Agent) string {
	return renderAgentWithTemplate(st, agent, agent.Title)
}

func renderAgentBaseTitleForState(st state.State, agent state.Agent) string {
	return renderAgentWithTemplate(st, agent, titles.TitleTemplate)
}

func renderAgentWithTemplate(st state.State, agent state.Agent, template string) string {
	workspace := state.Workspace{}
	group := state.Group{}
	if w := state.WorkspaceForAgent(st, agent); w != nil {
		workspace = *w
	}
	if f := state.GroupForAgent(st, agent); f != nil {
		group = *f
	}
	return titles.RenderAgent(agent, workspace, group, template)
}

func agentStatusIndicatesActivity(agent state.Agent) bool {
	return titles.StatusIndicatesActivity(agent)
}

func agentStatusShowsLoadingIndicator(agent state.Agent) bool {
	switch titles.CanonicalStatus(agent) {
	case string(state.StatusReady), "idle", string(state.StatusStopped), string(state.StatusKilled), string(state.StatusError), string(state.StatusSitting):
		return false
	default:
		return true
	}
}

func codexScreenStatus(screen *TerminalScreen) string {
	if screen == nil {
		return ""
	}
	content := strings.ToLower(screen.String())
	hasSubmitAction := strings.Contains(content, "to submit answer") ||
		strings.Contains(content, "to submit all")
	hasQuestionPrompt := strings.Contains(content, "question ") ||
		strings.Contains(content, "unanswered") ||
		strings.Contains(content, "user_note:")
	if hasSubmitAction && hasQuestionPrompt {
		return "Ready"
	}
	return ""
}

func autoTitleNotice(cfg config.Config, agent state.Agent, draftTitle string) string {
	if !agentUsesCodexIntegration(cfg, agent) {
		return ""
	}
	if !strings.Contains(draftTitle, titles.AutoTemplate) {
		return ""
	}
	if strings.TrimSpace(agent.AutoTitle) != "" {
		return "Auto title is ready."
	}
	if strings.TrimSpace(agent.AutoTitleError) != "" {
		return "Auto title error: " + agent.AutoTitleError
	}
	if agent.AutoTitleAttempted {
		return "Auto title is generating."
	}
	if strings.TrimSpace(cfg.TitleHookCommand) == "" {
		return "Auto title unavailable: set title_hook_command."
	}
	return "Auto title will generate from the first submitted message."
}
