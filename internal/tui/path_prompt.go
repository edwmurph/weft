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

type workdirPromptStatus struct {
	message string
	style   lipgloss.Style
}

func configurePromptInput(input *textinput.Model, prompt promptKind, value string) {
	input.KeyMap = promptInputKeyMap()
	input.SetValue(value)
	input.CursorEnd()
	input.Focus()
	input.Placeholder = ""
	if prompt == promptWorkdir {
		input.Placeholder = "~/code/project"
		input.ShowSuggestions = true
		input.SetSuggestions(workdirPathSuggestions(value))
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

func refreshPromptInput(input *textinput.Model, prompt promptKind) {
	if prompt != promptWorkdir {
		return
	}
	input.SetSuggestions(workdirPathSuggestions(input.Value()))
}

func completeWorkdirSuggestion(input *textinput.Model) bool {
	suggestion := input.CurrentSuggestion()
	if strings.TrimSpace(suggestion) == "" || suggestion == input.Value() {
		return false
	}
	input.SetValue(pathWithoutTrailingSeparator(suggestion))
	input.CursorEnd()
	refreshPromptInput(input, promptWorkdir)
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

func renderWorkdirPromptInput(input textinput.Model, width int) []string {
	input.Width = max(16, width-4)
	input.SetSuggestions(nil)
	box := modalInputStyle.Width(max(16, width-4)).Render(input.View())
	lines := []string{modalLabelStyle.Render("Path")}
	lines = append(lines, strings.Split(box, "\n")...)
	return lines
}

func renderWorkdirModalActions(input textinput.Model, menuOpen bool) string {
	if menuOpen {
		return modalKeyStyle.Render("Enter") + " choose  " + modalKeyStyle.Render("Up/Down") + " move  " + modalKeyStyle.Render("Esc") + " close options"
	}
	actions := []string{}
	if workdirInputIsExistingDirectory(input.Value()) {
		actions = append(actions, modalKeyStyle.Render("Enter")+" add")
	}
	if len(input.MatchedSuggestions()) > 0 {
		actions = append(actions, modalKeyStyle.Render("Down")+" open options")
	}
	actions = append(actions, modalKeyStyle.Render("Esc")+" cancel")
	return strings.Join(actions, "  ")
}

func renderWorkdirPromptStatus(st state.State, value string, width int) string {
	status := inspectWorkdirPromptPath(st, value)
	return status.style.Render(clip(status.message, width))
}

func renderWorkdirSuggestionMenu(input textinput.Model, width int, open bool, maxRows int) []string {
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
		value := padVisual(clip(marker+workdirSuggestionMenuLabel(input.Value(), suggestion), valueWidth), valueWidth)
		lines = append(lines, " "+style.Render(value))
	}
	return lines
}

func workdirSuggestionRows(height int) int {
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

func workdirSuggestionMenuLabel(value string, suggestion string) string {
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

func inspectWorkdirPromptPath(st state.State, raw string) workdirPromptStatus {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return workdirPromptStatus{message: "Type a path", style: mutedStyle}
	}
	path := state.NormalizeWorkdirPath(raw)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if parent := nearestExistingParent(path); parent != "" && parent != path {
				return workdirPromptStatus{message: "! Parent exists: " + sessions.DisplayPath(parent), style: modalWarningStyle}
			}
			return workdirPromptStatus{message: "! Not found: " + sessions.DisplayPath(path), style: modalWarningStyle}
		}
		return workdirPromptStatus{message: "! Cannot read path: " + err.Error(), style: modalErrorStyle}
	}
	if !info.IsDir() {
		return workdirPromptStatus{message: "! Not a directory: " + sessions.DisplayPath(path), style: modalErrorStyle}
	}
	if workdirByPath(st, path) != nil {
		return workdirPromptStatus{message: "Already added: " + sessions.DisplayPath(path), style: mutedStyle}
	}
	return workdirPromptStatus{message: "✓ " + sessions.DisplayPath(path), style: modalSuccessStyle}
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
