package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
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

func renderPromptExtraForState(cfg config.Config, st state.State, prompt promptKind, selectedAgent *state.Agent, input textinput.Model, width int) []string {
	if prompt != promptRenameAgent {
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

func renamePromptTargetForState(st state.State, folderCursor int) (promptKind, string, string, bool) {
	if st.Focus == state.FocusWorkdirs {
		if workdir := state.WorkdirByID(st, st.SelectedWorkdirID); workdir != nil {
			return promptWorkdirTitle, workdir.ID, workdir.Title, true
		}
		return "", "", "", false
	}
	row := currentFolderRowForState(st, folderCursor)
	switch row.kind {
	case folderRowFolder:
		if folder := state.FolderByID(st, row.folderID); folder != nil {
			return promptRenameGroup, folder.ID, folder.Path, true
		}
	case folderRowAgent:
		if agent := state.AgentByID(st, row.agentID); agent != nil {
			return promptRenameAgent, agent.ID, agent.Title, true
		}
	}
	return "", "", "", false
}

func deleteConfirmTargetForState(st state.State, folderCursor int) (confirmKind, string, bool) {
	if st.Focus == state.FocusWorkdirs {
		if workdir := state.WorkdirByID(st, st.SelectedWorkdirID); workdir != nil {
			return confirmDeleteWorkdir, workdir.ID, true
		}
		return "", "", false
	}
	row := currentFolderRowForState(st, folderCursor)
	switch row.kind {
	case folderRowFolder:
		if folder := state.FolderByID(st, row.folderID); folder != nil {
			return confirmDeleteGroup, folder.ID, true
		}
	case folderRowAgent:
		if agent := state.AgentByID(st, row.agentID); agent != nil {
			return confirmDeleteAgent, agent.ID, true
		}
	}
	return "", "", false
}

func selectedAgentForState(st state.State, folderCursor int) *state.Agent {
	row := currentFolderRowForState(st, folderCursor)
	if row.kind == folderRowAgent {
		return state.AgentByID(st, row.agentID)
	}
	return nil
}

func currentFolderRowForState(st state.State, folderCursor int) folderRow {
	rows := folderRowsForState(st)
	if len(rows) == 0 {
		return folderRow{}
	}
	if folderCursor < 0 || folderCursor >= len(rows) {
		return rows[0]
	}
	return rows[folderCursor]
}

func folderRowsForState(st state.State) []folderRow {
	var rows []folderRow
	for _, agent := range state.UngroupedAgentsForWorkdir(st, st.SelectedWorkdirID) {
		rows = append(rows, folderRow{kind: folderRowAgent, agentID: agent.ID})
	}
	for _, folder := range state.FoldersForWorkdir(st, st.SelectedWorkdirID) {
		rows = append(rows, folderRow{kind: folderRowFolder, folderID: folder.ID})
		if state.IsGroupCollapsed(st, folder.ID) {
			continue
		}
		for _, agent := range state.AgentsForFolder(st, folder.ID) {
			rows = append(rows, folderRow{kind: folderRowAgent, folderID: folder.ID, agentID: agent.ID})
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
	workdir := state.Workdir{}
	folder := state.Folder{}
	if w := state.WorkdirForAgent(st, agent); w != nil {
		workdir = *w
	}
	if f := state.FolderForAgent(st, agent); f != nil {
		folder = *f
	}
	return titles.RenderAgent(agent, workdir, folder, template)
}

func autoTitleNotice(cfg config.Config, agent state.Agent, draftTitle string) string {
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
