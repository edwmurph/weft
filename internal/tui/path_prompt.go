package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/edwmurph/weft/internal/sessions"
	"github.com/edwmurph/weft/internal/state"
)

const (
	workdirSuggestionLimit        = 200
	workdirSuggestionVisibleLimit = 8
)

type promptStatus struct {
	message string
	style   lipgloss.Style
}

type promptContext struct {
	prompt        promptKind
	pendingID     string
	state         state.State
	selectedAgent *state.Agent
}

type promptInputAction int

const (
	promptInputNoop promptInputAction = iota
	promptInputSubmit
	promptInputCancel
)

type promptInputResult struct {
	input          textinput.Model
	suggestionOpen bool
	action         promptInputAction
	value          string
	message        string
	cmd            tea.Cmd
}

func configurePromptInput(input *textinput.Model, ctx promptContext, value string) {
	input.KeyMap = promptInputKeyMap()
	input.SetValue(value)
	input.CursorEnd()
	input.Focus()
	input.Placeholder = promptPlaceholder(ctx.prompt)
	if promptHasAutocomplete(ctx.prompt) {
		input.ShowSuggestions = true
		input.SetSuggestions(promptSuggestions(ctx, value))
		return
	}
	input.ShowSuggestions = true
	input.SetSuggestions(nil)
	input.ShowSuggestions = false
}

func promptInputKeyMap() textinput.KeyMap {
	keyMap := textinput.DefaultKeyMap
	keyMap.WordForward = key.NewBinding()
	keyMap.WordBackward = key.NewBinding()
	keyMap.DeleteWordBackward = key.NewBinding()
	keyMap.DeleteWordForward = key.NewBinding()
	return keyMap
}

func handlePromptWordKey(input *textinput.Model, prompt promptKind, msg tea.KeyMsg) bool {
	switch msg.String() {
	case "alt+left", "alt+b", "ctrl+left":
		input.SetCursor(previousPromptTokenBoundary(input.Value(), input.Position()))
		return true
	case "alt+right", "alt+f", "ctrl+right":
		input.SetCursor(nextPromptTokenBoundary(input.Value(), input.Position()))
		return true
	case "ctrl+h", "ctrl+w":
		deletePreviousPromptToken(input)
		return true
	case "alt+backspace", "alt+delete", "alt+ctrl+h", "alt+\b", "alt+\x7f":
		deletePreviousPromptToken(input)
		return true
	}
	if msg.Type == tea.KeyRunes && promptRuneDeletesPreviousToken(prompt, msg) {
		deletePreviousPromptToken(input)
		return true
	}
	if !msg.Alt {
		return false
	}
	switch msg.Type {
	case tea.KeyLeft:
		input.SetCursor(previousPromptTokenBoundary(input.Value(), input.Position()))
		return true
	case tea.KeyRight:
		input.SetCursor(nextPromptTokenBoundary(input.Value(), input.Position()))
		return true
	case tea.KeyBackspace, tea.KeyDelete, tea.KeyCtrlH:
		deletePreviousPromptToken(input)
		return true
	}
	return false
}

func promptRuneDeletesPreviousToken(prompt promptKind, msg tea.KeyMsg) bool {
	if len(msg.Runes) != 1 {
		return false
	}
	r := msg.Runes[0]
	if msg.Alt && (r == '\b' || r == 0x7f) {
		return true
	}
	// Some macOS terminal configurations send Option-Backspace as a printable
	// erase/left-arrow glyph instead of preserving the Alt modifier.
	if r == '⌫' || r == '⌦' || r == '␈' || r == '␡' || r == '←' || r == '⇤' {
		return true
	}
	return prompt == promptWorkdir && isOptionBackspaceGlyph(r)
}

func previousPromptTokenBoundary(value string, position int) int {
	runes := []rune(value)
	index := max(0, min(position, len(runes)))
	for index > 0 && !isPromptTokenRune(runes[index-1]) {
		index--
	}
	for index > 0 && isPromptTokenRune(runes[index-1]) {
		index--
	}
	return index
}

func nextPromptTokenBoundary(value string, position int) int {
	runes := []rune(value)
	index := max(0, min(position, len(runes)))
	for index < len(runes) && isPromptTokenRune(runes[index]) {
		index++
	}
	for index < len(runes) && !isPromptTokenRune(runes[index]) {
		index++
	}
	return index
}

func deletePreviousPromptToken(input *textinput.Model) {
	value := input.Value()
	position := input.Position()
	start := previousPromptTokenBoundary(value, position)
	if start == position {
		return
	}
	runes := []rune(value)
	next := string(append(append([]rune{}, runes[:start]...), runes[position:]...))
	input.SetValue(next)
	input.SetCursor(start)
}

func isPromptTokenRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func isOptionBackspaceGlyph(r rune) bool {
	switch r {
	case '⌫', '⌦', '␈', '␡', '←', '↤', '⇤', '⬅', '◀', '➜', '➔':
		return true
	default:
		return false
	}
}

func handlePromptInputKey(input textinput.Model, ctx promptContext, suggestionOpen bool, msg tea.KeyMsg) promptInputResult {
	result := promptInputResult{input: input, suggestionOpen: suggestionOpen}
	if handlePromptWordKey(&result.input, ctx.prompt, msg) {
		refreshPromptInput(&result.input, ctx)
		if promptHasAutocomplete(ctx.prompt) {
			result.suggestionOpen = len(result.input.MatchedSuggestions()) > 0
		}
		return result
	}
	switch msg.Type {
	case tea.KeyEsc:
		if promptHasAutocomplete(ctx.prompt) && result.suggestionOpen {
			result.suggestionOpen = false
			return result
		}
		result.action = promptInputCancel
		return result
	case tea.KeyEnter:
		if promptHasAutocomplete(ctx.prompt) && result.suggestionOpen && completePromptSuggestion(&result.input, ctx) {
			result.suggestionOpen = false
			return result
		}
		value := strings.TrimSpace(result.input.Value())
		if message := promptSubmitBlocker(ctx, value); message != "" {
			result.message = message
			return result
		}
		result.action = promptInputSubmit
		result.value = value
		return result
	case tea.KeyTab:
		if promptHasAutocomplete(ctx.prompt) && len(result.input.MatchedSuggestions()) > 0 {
			if result.suggestionOpen {
				completePromptSuggestion(&result.input, ctx)
				result.suggestionOpen = false
				return result
			}
			result.suggestionOpen = true
			return result
		}
	case tea.KeyUp, tea.KeyDown:
		if promptHasAutocomplete(ctx.prompt) && len(result.input.MatchedSuggestions()) > 0 && !result.suggestionOpen {
			result.suggestionOpen = true
			return result
		}
	}
	oldValue := result.input.Value()
	result.input, result.cmd = result.input.Update(msg)
	refreshPromptInput(&result.input, ctx)
	if promptHasAutocomplete(ctx.prompt) {
		if result.input.Value() != oldValue {
			result.suggestionOpen = len(result.input.MatchedSuggestions()) > 0
		} else if len(result.input.MatchedSuggestions()) == 0 {
			result.suggestionOpen = false
		}
	}
	return result
}

func promptPlaceholder(prompt promptKind) string {
	switch prompt {
	case promptWorkdir:
		return "~/code/project"
	case promptGroup, promptRenameGroup, promptMoveAgent:
		return "release"
	case promptWorkdirTitle, promptRenameAgent:
		return "Codex {status}"
	default:
		return ""
	}
}

func promptHasAutocomplete(prompt promptKind) bool {
	return prompt == promptWorkdir || prompt == promptMoveAgent
}

func refreshPromptInput(input *textinput.Model, ctx promptContext) {
	if !promptHasAutocomplete(ctx.prompt) {
		return
	}
	input.SetSuggestions(promptSuggestions(ctx, input.Value()))
}

func promptSuggestions(ctx promptContext, value string) []string {
	switch ctx.prompt {
	case promptWorkdir:
		return workdirPathSuggestions(value)
	case promptMoveAgent:
		return groupNameSuggestions(ctx)
	default:
		return nil
	}
}

func completePromptSuggestion(input *textinput.Model, ctx promptContext) bool {
	suggestion := input.CurrentSuggestion()
	if strings.TrimSpace(suggestion) == "" {
		return false
	}
	value := suggestion
	if ctx.prompt == promptWorkdir {
		value = pathWithoutTrailingSeparator(suggestion)
	}
	if value == input.Value() {
		return false
	}
	input.SetValue(value)
	input.CursorEnd()
	refreshPromptInput(input, ctx)
	return true
}

func pathWithoutTrailingSeparator(path string) string {
	if path == "" || path == string(os.PathSeparator) {
		return path
	}
	return strings.TrimRight(path, string(os.PathSeparator))
}

func workdirInputIsExistingDirectory(value string) bool {
	info, err := os.Stat(state.NormalizeWorkdirPath(value))
	return err == nil && info.IsDir()
}

func defaultWorkdirPromptValue(st state.State, fallback string) string {
	path := fallback
	if workdir := state.ActiveWorkdir(st); workdir != nil && strings.TrimSpace(workdir.Path) != "" {
		path = workdir.Path
	}
	path = state.NormalizeWorkdirPath(path)
	parent := filepath.Dir(path)
	if parent == "." || parent == "" {
		parent = path
	}
	return withTrailingSeparator(displayPathForPrompt(parent))
}

func workdirAddMessage(previous state.State, workdir state.Workdir) string {
	if workdirByPath(previous, workdir.Path) != nil {
		return "selected existing workdir " + sessions.DisplayPath(workdir.Path)
	}
	return "added workdir " + sessions.DisplayPath(workdir.Path)
}

func renderPromptInput(label string, input textinput.Model, width int) []string {
	input.Width = max(16, width-4)
	input.SetSuggestions(nil)
	box := modalInputStyle.Width(max(16, width-4)).Render(input.View())
	lines := []string{modalLabelStyle.Render(label)}
	lines = append(lines, strings.Split(box, "\n")...)
	return lines
}

func renderPromptActions(ctx promptContext, input textinput.Model, menuOpen bool) string {
	if menuOpen {
		return modalKeyStyle.Render("Enter") + " choose  " + modalKeyStyle.Render("Up/Down") + " move  " + modalKeyStyle.Render("Esc") + " close suggestions"
	}
	actions := []string{}
	if label := promptSubmitActionLabel(ctx, input.Value()); label != "" {
		actions = append(actions, modalKeyStyle.Render("Enter")+" "+label)
	}
	if promptHasAutocomplete(ctx.prompt) && len(input.MatchedSuggestions()) > 0 {
		actions = append(actions, modalKeyStyle.Render("Down")+" suggestions")
	}
	actions = append(actions, modalKeyStyle.Render("Esc")+" cancel")
	return strings.Join(actions, "  ")
}

func renderPromptStatus(ctx promptContext, value string, width int) string {
	status := inspectPromptStatus(ctx, value)
	return status.style.Render(clip(status.message, width))
}

func renderPromptModal(ctx promptContext, input textinput.Model, width int, height int, menuOpen bool, extra []string) string {
	lines := []string{modalTitleStyle.Render(promptTitle(ctx.prompt)), ""}
	lines = append(lines, renderPromptInput(promptLabel(ctx.prompt), input, width)...)
	if suggestions := renderPromptSuggestionMenu(ctx, input, width, menuOpen, promptSuggestionRows(height)); len(suggestions) > 0 {
		lines = append(lines, suggestions...)
	}
	lines = append(lines, renderPromptStatus(ctx, input.Value(), width))
	if hint := promptHint(ctx.prompt); hint != "" {
		lines = append(lines, "", mutedStyle.Render(clip(hint, width)))
	}
	if len(extra) > 0 {
		lines = append(lines, extra...)
	}
	lines = append(lines, "", renderPromptActions(ctx, input, menuOpen))
	return strings.Join(lines, "\n")
}

func promptTitle(prompt promptKind) string {
	switch prompt {
	case promptWorkdir:
		return "Add workdir"
	case promptGroup:
		return "Create group"
	case promptWorkdirTitle:
		return "Rename workdir"
	case promptRenameGroup:
		return "Rename group"
	case promptRenameAgent:
		return "Rename agent"
	case promptMoveAgent:
		return "Move agent"
	default:
		return "Input"
	}
}

func promptLabel(prompt promptKind) string {
	switch prompt {
	case promptWorkdir:
		return "Path"
	case promptGroup, promptRenameGroup, promptMoveAgent:
		return "Group"
	default:
		return "Title"
	}
}

func promptHint(prompt promptKind) string {
	switch prompt {
	case promptGroup:
		return "Flat and unique in this workdir."
	case promptWorkdirTitle:
		return "Blank uses the path title."
	case promptMoveAgent:
		return "Blank makes the agent top-level."
	default:
		return ""
	}
}

func renderConfirmPrompt(confirm confirmKind, target string, width int) string {
	lines := []string{
		modalTitleStyle.Render(confirmTitle(confirm)),
		"",
		modalLabelStyle.Render("Target"),
		modalValueStyle.Render(clip(target, width)),
		"",
		modalKeyStyle.Render("Y") + " delete  " + modalKeyStyle.Render("N") + " cancel  " + modalKeyStyle.Render("Esc") + " cancel",
	}
	return strings.Join(lines, "\n")
}

func confirmTitle(confirm confirmKind) string {
	switch confirm {
	case confirmDeleteWorkdir:
		return "Delete workdir"
	case confirmDeleteGroup:
		return "Delete group"
	case confirmDeleteAgent:
		return "Delete agent"
	default:
		return "Delete item"
	}
}

func confirmTarget(confirm confirmKind, st state.State, pendingID string, renderAgentTitle func(state.Agent) string) string {
	switch confirm {
	case confirmDeleteWorkdir:
		if workdir := state.WorkdirByID(st, pendingID); workdir != nil {
			return workdir.Path
		}
	case confirmDeleteGroup:
		if folder := state.FolderByID(st, pendingID); folder != nil {
			return folder.Path
		}
	case confirmDeleteAgent:
		if agent := state.AgentByID(st, pendingID); agent != nil {
			return renderAgentTitle(*agent)
		}
	}
	return "item"
}

func renderPromptSuggestionMenu(ctx promptContext, input textinput.Model, width int, open bool, maxRows int) []string {
	if !open {
		return nil
	}
	suggestions := input.MatchedSuggestions()
	if len(suggestions) == 0 {
		return nil
	}
	valueWidth := max(0, width-2)
	selected := input.CurrentSuggestionIndex()
	start, end := suggestionWindow(selected, len(suggestions), maxRows)
	lines := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		suggestion := suggestions[index]
		marker := "  "
		style := mutedStyle
		if index == selected {
			marker = "> "
			style = modalSuggestionSelectedStyle
		}
		value := padVisual(clip(marker+promptSuggestionMenuLabel(ctx, input.Value(), suggestion), valueWidth), valueWidth)
		lines = append(lines, " "+style.Render(value))
	}
	return lines
}

func promptSuggestionRows(height int) int {
	if height <= 0 {
		return workdirSuggestionVisibleLimit
	}
	return max(3, min(workdirSuggestionVisibleLimit, height-16))
}

func suggestionWindow(selected int, count int, maxRows int) (int, int) {
	if count <= 0 || maxRows <= 0 {
		return 0, 0
	}
	if maxRows >= count {
		return 0, count
	}
	selected = max(0, min(selected, count-1))
	start := selected - maxRows + 1
	if start < 0 {
		start = 0
	}
	end := start + maxRows
	if end > count {
		end = count
		start = max(0, end-maxRows)
	}
	return start, end
}

func promptSuggestionMenuLabel(ctx promptContext, value string, suggestion string) string {
	if ctx.prompt != promptWorkdir {
		return suggestion
	}
	dirText, _ := splitPromptPath(strings.TrimSpace(value))
	if dirText != "" && strings.HasPrefix(suggestion, dirText) {
		if label := strings.TrimPrefix(suggestion, dirText); label != "" {
			return pathWithoutTrailingSeparator(label)
		}
	}
	clean := strings.TrimSuffix(suggestion, string(os.PathSeparator))
	if clean == "" {
		return suggestion
	}
	return filepath.Base(clean)
}

func inspectPromptStatus(ctx promptContext, raw string) promptStatus {
	raw = strings.TrimSpace(raw)
	switch ctx.prompt {
	case promptWorkdir:
		return inspectWorkdirPromptPath(ctx.state, raw)
	case promptGroup, promptRenameGroup:
		if message := validateGroupPrompt(ctx, raw); message != "" {
			return promptStatus{message: message, style: modalWarningStyle}
		}
		return promptStatus{message: "Ready", style: modalSuccessStyle}
	case promptWorkdirTitle:
		if raw == "" {
			return promptStatus{message: "Blank uses path title", style: mutedStyle}
		}
		return promptStatus{message: "Ready", style: modalSuccessStyle}
	case promptRenameAgent:
		if raw == "" {
			return promptStatus{message: "Title required", style: modalWarningStyle}
		}
		return promptStatus{message: "Ready", style: modalSuccessStyle}
	case promptMoveAgent:
		if raw == "" {
			return promptStatus{message: "Top-level agent", style: mutedStyle}
		}
		if promptMoveGroup(ctx, raw) == nil {
			return promptStatus{message: "Group not found", style: modalWarningStyle}
		}
		return promptStatus{message: "Group found", style: modalSuccessStyle}
	default:
		return promptStatus{message: "", style: mutedStyle}
	}
}

func promptSubmitBlocker(ctx promptContext, value string) string {
	switch ctx.prompt {
	case promptWorkdir:
		if !workdirInputIsExistingDirectory(value) {
			return inspectWorkdirPromptPath(ctx.state, value).message
		}
	case promptGroup, promptRenameGroup:
		return validateGroupPrompt(ctx, value)
	case promptRenameAgent:
		if value == "" {
			return "Title required"
		}
	case promptMoveAgent:
		if ctx.selectedAgent == nil {
			return "Select an agent first"
		}
		if value != "" && promptMoveGroup(ctx, value) == nil {
			return "Group not found"
		}
	}
	return ""
}

func promptSubmitActionLabel(ctx promptContext, value string) string {
	value = strings.TrimSpace(value)
	if promptSubmitBlocker(ctx, value) != "" {
		return ""
	}
	switch ctx.prompt {
	case promptWorkdir:
		return "add"
	case promptGroup:
		return "create"
	case promptWorkdirTitle:
		if value == "" {
			return "clear"
		}
		return "save"
	case promptRenameGroup, promptRenameAgent:
		return "save"
	case promptMoveAgent:
		if value == "" {
			return "top-level"
		}
		return "move"
	default:
		return "submit"
	}
}

func validateGroupPrompt(ctx promptContext, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Group required"
	}
	if strings.Contains(value, "/") {
		return "No / in group names"
	}
	workdirID := ctx.state.SelectedWorkdirID
	if ctx.prompt == promptRenameGroup {
		folder := state.FolderByID(ctx.state, ctx.pendingID)
		if folder == nil {
			return "Group not found"
		}
		workdirID = folder.WorkdirID
	}
	if workdirID == "" || state.WorkdirByID(ctx.state, workdirID) == nil {
		return "Select a workdir first"
	}
	for _, folder := range state.FoldersForWorkdir(ctx.state, workdirID) {
		if folder.Path == value && folder.ID != ctx.pendingID {
			return "Group already exists"
		}
	}
	return ""
}

func promptMoveGroup(ctx promptContext, value string) *state.Folder {
	if ctx.selectedAgent == nil {
		return nil
	}
	for _, folder := range state.FoldersForWorkdir(ctx.state, ctx.selectedAgent.WorkdirID) {
		if folder.Path == value {
			return &folder
		}
	}
	return nil
}

func groupNameSuggestions(ctx promptContext) []string {
	if ctx.selectedAgent == nil {
		return nil
	}
	folders := state.FoldersForWorkdir(ctx.state, ctx.selectedAgent.WorkdirID)
	suggestions := make([]string, 0, len(folders))
	for _, folder := range folders {
		suggestions = append(suggestions, folder.Path)
	}
	sort.Slice(suggestions, func(i int, j int) bool {
		return strings.ToLower(suggestions[i]) < strings.ToLower(suggestions[j])
	})
	return suggestions
}

func inspectWorkdirPromptPath(st state.State, raw string) promptStatus {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return promptStatus{message: "Type a path", style: mutedStyle}
	}
	path := state.NormalizeWorkdirPath(raw)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if parent := nearestExistingParent(path); parent != "" && parent != path {
				return promptStatus{message: "! Parent exists: " + sessions.DisplayPath(parent), style: modalWarningStyle}
			}
			return promptStatus{message: "! Not found: " + sessions.DisplayPath(path), style: modalWarningStyle}
		}
		return promptStatus{message: "! Cannot read path: " + err.Error(), style: modalErrorStyle}
	}
	if !info.IsDir() {
		return promptStatus{message: "! Not a directory: " + sessions.DisplayPath(path), style: modalErrorStyle}
	}
	if workdirByPath(st, path) != nil {
		return promptStatus{message: "Already added: " + sessions.DisplayPath(path), style: mutedStyle}
	}
	return promptStatus{message: "✓ " + sessions.DisplayPath(path), style: modalSuccessStyle}
}

func workdirByPath(st state.State, path string) *state.Workdir {
	path = state.NormalizeWorkdirPath(path)
	for index := range st.Workdirs {
		if st.Workdirs[index].Path == path {
			return &st.Workdirs[index]
		}
	}
	return nil
}

func nearestExistingParent(path string) string {
	path = state.NormalizeWorkdirPath(path)
	for {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return path
		}
		next := filepath.Dir(path)
		if next == path {
			return ""
		}
		path = next
	}
}

func workdirPathSuggestions(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if raw == "~" {
		return []string{"~/"}
	}
	dirText, base := splitPromptPath(raw)
	fsDir := expandPromptPath(dirText)
	if fsDir == "" {
		fsDir = "."
	}
	entries, err := os.ReadDir(fsDir)
	if err != nil {
		return nil
	}
	baseLower := strings.ToLower(base)
	showHidden := strings.HasPrefix(base, ".")
	var suggestions []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(name, ".") && !showHidden {
			continue
		}
		if baseLower != "" && !strings.HasPrefix(strings.ToLower(name), baseLower) {
			continue
		}
		suggestions = append(suggestions, withTrailingSeparator(joinPromptPath(dirText, name)))
	}
	sort.Slice(suggestions, func(i int, j int) bool {
		return strings.ToLower(suggestions[i]) < strings.ToLower(suggestions[j])
	})
	if len(suggestions) > workdirSuggestionLimit {
		suggestions = suggestions[:workdirSuggestionLimit]
	}
	return suggestions
}

func splitPromptPath(path string) (string, string) {
	if strings.HasSuffix(path, string(os.PathSeparator)) {
		return path, ""
	}
	return filepath.Split(path)
}

func expandPromptPath(path string) string {
	if path == "" {
		return "."
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func joinPromptPath(dir string, name string) string {
	if dir == "" {
		return name
	}
	if strings.HasSuffix(dir, string(os.PathSeparator)) {
		return dir + name
	}
	return filepath.Join(dir, name)
}

func displayPathForPrompt(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+string(os.PathSeparator)) {
			return "~" + strings.TrimPrefix(path, home)
		}
	}
	return path
}

func withTrailingSeparator(path string) string {
	if path == "" || path == string(os.PathSeparator) || strings.HasSuffix(path, string(os.PathSeparator)) {
		return path
	}
	return path + string(os.PathSeparator)
}
