package config

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	AppDirEnv  = "WEFT_HOME"
	WorkdirEnv = "WEFT_WORKDIR"
)

var (
	DefaultColumns    = []string{"inbox", "implement", "ship"}
	oldDefaultColumns = []string{"Backlog", "Active", "Review", "Done"}
)

type KeyBindings struct {
	Drawer     string `toml:"drawer"`
	FocusLeft  string `toml:"focus_left"`
	FocusRight string `toml:"focus_right"`
	SelectPrev string `toml:"select_prev"`
	SelectNext string `toml:"select_next"`
	Open       string `toml:"open"`
	NewWorkdir string `toml:"new_workdir"`
	NewGroup   string `toml:"new_group"`
	NewAgent   string `toml:"new_agent"`
	MoveAgent  string `toml:"move_agent"`
	Rename     string `toml:"rename"`
	Delete     string `toml:"delete"`
	Help       string `toml:"help"`
	Quit       string `toml:"quit"`
}

type Config struct {
	TmuxSession             string      `toml:"tmux_session"`
	CodexCommand            string      `toml:"codex_command"`
	TitleTemplate           string      `toml:"title_template"`
	TitleHookCommand        string      `toml:"title_hook_command"`
	TitleHookTimeoutSeconds int         `toml:"title_hook_timeout_seconds"`
	Columns                 []string    `toml:"columns"`
	KeyBindings             KeyBindings `toml:"key_bindings"`
}

type Runtime struct {
	Workdir          string
	Dir              string
	ConfigPath       string
	StatePath        string
	SocketPath       string
	LegacySocketPath string
}

type ConfigError struct {
	Message string
}

func (e ConfigError) Error() string {
	return e.Message
}

func DefaultKeyBindings() KeyBindings {
	return KeyBindings{
		Drawer:     "C-b",
		FocusLeft:  "Left",
		FocusRight: "Right",
		SelectPrev: "k",
		SelectNext: "j",
		Open:       "Enter",
		NewWorkdir: "w",
		NewGroup:   "g",
		NewAgent:   "n",
		MoveAgent:  "m",
		Rename:     "r",
		Delete:     "d",
		Help:       "?",
		Quit:       "C-c",
	}
}

func DefaultConfig(defaultSession string) Config {
	return Config{
		CodexCommand:            "codex",
		TitleTemplate:           "{status} {auto}",
		TitleHookCommand:        "",
		TitleHookTimeoutSeconds: 10,
		Columns:                 append([]string(nil), DefaultColumns...),
		KeyBindings:             DefaultKeyBindings(),
	}
}

func ResolveRuntime() (Runtime, error) {
	workdir, err := CurrentWorkdir()
	if err != nil {
		return Runtime{}, err
	}
	dir, err := AppDir(workdir)
	if err != nil {
		return Runtime{}, err
	}
	return Runtime{
		Workdir:          workdir,
		Dir:              dir,
		ConfigPath:       filepath.Join(dir, "config.toml"),
		StatePath:        filepath.Join(dir, "state.json"),
		SocketPath:       filepath.Join(dir, "weft.sock"),
		LegacySocketPath: filepath.Join(dir, "weft-tui.sock"),
	}, nil
}

func (r Runtime) TUISocket() string {
	if r.LegacySocketPath != "" {
		return r.LegacySocketPath
	}
	return r.SocketPath
}

func CurrentWorkdir() (string, error) {
	if configured := os.Getenv(WorkdirEnv); configured != "" {
		return filepath.Abs(expandHome(configured))
	}
	return os.Getwd()
}

func AppDir(workdir string) (string, error) {
	if configured := os.Getenv(AppDirEnv); configured != "" {
		return filepath.Abs(expandHome(configured))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".weft"), nil
}

func RuntimeID(workdir string) string {
	name := strings.ToLower(filepath.Base(workdir))
	name = regexp.MustCompile(`[^A-Za-z0-9_-]+`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "workdir"
	}
	sum := sha1.Sum([]byte(workdir))
	return fmt.Sprintf("%s-%s", name, hex.EncodeToString(sum[:])[:8])
}

func DefaultTmuxSession(workdir string) string {
	return "weft"
}

func EnsureConfig(rt Runtime) (Config, error) {
	if err := os.MkdirAll(rt.Dir, 0o700); err != nil {
		return Config{}, err
	}
	if _, err := os.Stat(rt.ConfigPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(rt.ConfigPath, []byte(DefaultConfigText()), 0o600); err != nil {
			return Config{}, err
		}
	} else if err != nil {
		return Config{}, err
	} else if err := MigrateDefaultConfig(rt.ConfigPath); err != nil {
		return Config{}, err
	}
	return LoadConfig(rt.ConfigPath, DefaultTmuxSession(rt.Workdir))
}

func LoadConfig(path string, defaultSession string) (Config, error) {
	cfg := DefaultConfig(defaultSession)
	var raw struct {
		TmuxSession             string   `toml:"tmux_session"`
		CodexCommand            string   `toml:"codex_command"`
		TitleTemplate           string   `toml:"title_template"`
		TitleHookCommand        string   `toml:"title_hook_command"`
		TitleHookTimeoutSeconds int      `toml:"title_hook_timeout_seconds"`
		Columns                 []string `toml:"columns"`
		KeyBindings             struct {
			Drawer       string `toml:"drawer"`
			FocusLeft    string `toml:"focus_left"`
			FocusRight   string `toml:"focus_right"`
			SelectPrev   string `toml:"select_prev"`
			SelectNext   string `toml:"select_next"`
			Open         string `toml:"open"`
			NewWorkdir   string `toml:"new_workdir"`
			NewGroup     string `toml:"new_group"`
			NewFolder    string `toml:"new_folder"`
			NewAgent     string `toml:"new_agent"`
			MoveAgent    string `toml:"move_agent"`
			Delete       string `toml:"delete"`
			DrawerLegacy string `toml:"focus_toggle"`
			New          string `toml:"new"`
			Prev         string `toml:"prev"`
			Previous     string `toml:"previous"`
			Next         string `toml:"next"`
			MoveLeft     string `toml:"move_left"`
			MoveRight    string `toml:"move_right"`
			Rename       string `toml:"rename"`
			Close        string `toml:"close"`
			Help         string `toml:"help"`
			CloseWeft    string `toml:"close_weft"`
			Quit         string `toml:"quit"`
		} `toml:"key_bindings"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return Config{}, ConfigError{Message: fmt.Sprintf("could not parse %s: %v", path, err)}
	}
	if raw.TmuxSession != "" {
		cfg.TmuxSession = raw.TmuxSession
	}
	if raw.CodexCommand != "" {
		cfg.CodexCommand = raw.CodexCommand
	}
	if raw.TitleTemplate != "" {
		cfg.TitleTemplate = raw.TitleTemplate
	}
	if raw.TitleHookCommand != "" {
		cfg.TitleHookCommand = raw.TitleHookCommand
	}
	if raw.TitleHookTimeoutSeconds != 0 {
		cfg.TitleHookTimeoutSeconds = raw.TitleHookTimeoutSeconds
	}
	if raw.Columns != nil {
		cfg.Columns = normalizeColumns(raw.Columns)
	}
	applyBinding := func(target *string, value string) {
		if strings.TrimSpace(value) != "" {
			*target = value
		}
	}
	if raw.KeyBindings.Drawer != "" {
		applyBinding(&cfg.KeyBindings.Drawer, raw.KeyBindings.Drawer)
	} else if raw.KeyBindings.DrawerLegacy != "" {
		applyBinding(&cfg.KeyBindings.Drawer, raw.KeyBindings.DrawerLegacy)
	}
	applyBinding(&cfg.KeyBindings.FocusLeft, raw.KeyBindings.FocusLeft)
	applyBinding(&cfg.KeyBindings.FocusRight, raw.KeyBindings.FocusRight)
	if raw.KeyBindings.SelectPrev != "" {
		applyBinding(&cfg.KeyBindings.SelectPrev, raw.KeyBindings.SelectPrev)
	} else if raw.KeyBindings.Prev != "" {
		applyBinding(&cfg.KeyBindings.SelectPrev, raw.KeyBindings.Prev)
	} else {
		applyBinding(&cfg.KeyBindings.SelectPrev, raw.KeyBindings.Previous)
	}
	if raw.KeyBindings.SelectNext != "" {
		applyBinding(&cfg.KeyBindings.SelectNext, raw.KeyBindings.SelectNext)
	} else {
		applyBinding(&cfg.KeyBindings.SelectNext, raw.KeyBindings.Next)
	}
	applyBinding(&cfg.KeyBindings.Open, raw.KeyBindings.Open)
	if raw.KeyBindings.NewAgent != "" {
		applyBinding(&cfg.KeyBindings.NewAgent, raw.KeyBindings.NewAgent)
	} else {
		applyBinding(&cfg.KeyBindings.NewAgent, raw.KeyBindings.New)
	}
	applyBinding(&cfg.KeyBindings.NewWorkdir, raw.KeyBindings.NewWorkdir)
	if raw.KeyBindings.NewGroup != "" {
		applyBinding(&cfg.KeyBindings.NewGroup, raw.KeyBindings.NewGroup)
	} else {
		applyBinding(&cfg.KeyBindings.NewGroup, raw.KeyBindings.NewFolder)
	}
	applyBinding(&cfg.KeyBindings.MoveAgent, raw.KeyBindings.MoveAgent)
	applyBinding(&cfg.KeyBindings.Rename, raw.KeyBindings.Rename)
	if raw.KeyBindings.Delete != "" {
		applyBinding(&cfg.KeyBindings.Delete, raw.KeyBindings.Delete)
	} else {
		applyBinding(&cfg.KeyBindings.Delete, raw.KeyBindings.Close)
	}
	applyBinding(&cfg.KeyBindings.Help, raw.KeyBindings.Help)
	if raw.KeyBindings.Quit != "" {
		applyBinding(&cfg.KeyBindings.Quit, legacyCloseWeftBinding(raw.KeyBindings.Quit))
	} else if raw.KeyBindings.CloseWeft != "" {
		applyBinding(&cfg.KeyBindings.Quit, legacyCloseWeftBinding(raw.KeyBindings.CloseWeft))
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.CodexCommand) == "" {
		return ConfigError{Message: "codex_command must be a non-empty string"}
	}
	if strings.TrimSpace(c.TitleTemplate) == "" {
		return ConfigError{Message: "title_template must be a non-empty string"}
	}
	if c.TitleHookTimeoutSeconds <= 0 {
		return ConfigError{Message: "title_hook_timeout_seconds must be greater than zero"}
	}
	seen := map[string]bool{}
	for _, column := range c.Columns {
		if strings.TrimSpace(column) == "" {
			return ConfigError{Message: "columns must be non-empty strings"}
		}
		if seen[column] {
			return ConfigError{Message: "columns must be unique"}
		}
		seen[column] = true
	}
	for name, value := range map[string]string{
		"drawer": c.KeyBindings.Drawer, "focus_left": c.KeyBindings.FocusLeft, "focus_right": c.KeyBindings.FocusRight,
		"select_prev": c.KeyBindings.SelectPrev, "select_next": c.KeyBindings.SelectNext, "open": c.KeyBindings.Open,
		"new_workdir": c.KeyBindings.NewWorkdir, "new_group": c.KeyBindings.NewGroup, "new_agent": c.KeyBindings.NewAgent,
		"move_agent": c.KeyBindings.MoveAgent, "rename": c.KeyBindings.Rename, "delete": c.KeyBindings.Delete,
		"help": c.KeyBindings.Help, "quit": c.KeyBindings.Quit,
	} {
		if strings.TrimSpace(value) == "" {
			return ConfigError{Message: fmt.Sprintf("key binding %q must be a non-empty string", name)}
		}
	}
	return nil
}

func MigrateDefaultConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	updated := strings.ReplaceAll(text, `columns = ["Backlog", "Active", "Review", "Done"]`, `columns = ["inbox", "implement", "ship"]`)
	updated = strings.ReplaceAll(updated, `prev = "h"`, `prev = "Left"`)
	updated = strings.ReplaceAll(updated, `next = "l"`, `next = "Right"`)
	updated = strings.ReplaceAll(updated, `focus_left = "h"`, `focus_left = "Left"`)
	updated = strings.ReplaceAll(updated, `focus_right = "l"`, `focus_right = "Right"`)
	updated = strings.ReplaceAll(updated, `move_left = "H"`, `move_left = "S-Left"`)
	updated = strings.ReplaceAll(updated, `move_right = "L"`, `move_right = "S-Right"`)
	updated = strings.ReplaceAll(updated, `close = "x"`, `close = "c"`)
	updated = strings.ReplaceAll(updated, `new_folder = "f"`, `new_group = "g"`)
	updated = regexp.MustCompile(`(?m)^new_folder\s*=\s*"([^"\n]*)"\s*$`).ReplaceAllString(updated, `new_group = "$1"`)
	updated = strings.ReplaceAll(updated, `focus_toggle = "C-a"`, `focus_toggle = "C-g"`)
	updated = strings.ReplaceAll(updated, `focus_toggle = "C-d"`, `focus_toggle = "C-g"`)
	updated = regexp.MustCompile(`(?m)^sessions\s*=\s*"[^"\n]*"\n?`).ReplaceAllString(updated, "")
	if !strings.Contains(updated, "\ntitle_template =") {
		codexCommandRE := regexp.MustCompile(`(?m)^codex_command\s*=\s*"[^"\n]*"\n`)
		if codexCommandRE.MatchString(updated) {
			updated = codexCommandRE.ReplaceAllStringFunc(updated, func(match string) string {
				return match + `title_template = "{status} {auto}"` + "\n"
			})
		} else {
			updated = `title_template = "{status} {auto}"` + "\n" + updated
		}
	}
	if !strings.Contains(updated, "\ntitle_hook_command =") {
		titleTemplateRE := regexp.MustCompile(`(?m)^title_template\s*=\s*"[^"\n]*"\n`)
		if titleTemplateRE.MatchString(updated) {
			updated = titleTemplateRE.ReplaceAllStringFunc(updated, func(match string) string {
				return match + `title_hook_command = ""` + "\n" + `title_hook_timeout_seconds = 10` + "\n"
			})
		} else {
			updated = `title_hook_command = ""` + "\n" + `title_hook_timeout_seconds = 10` + "\n" + updated
		}
	} else if !strings.Contains(updated, "\ntitle_hook_timeout_seconds =") {
		titleHookRE := regexp.MustCompile(`(?m)^title_hook_command\s*=\s*"[^"\n]*"\n`)
		updated = titleHookRE.ReplaceAllStringFunc(updated, func(match string) string {
			return match + `title_hook_timeout_seconds = 10` + "\n"
		})
	}
	quitRE := regexp.MustCompile(`(?m)^quit\s*=\s*"([^"\n]*)"\s*$`)
	updated = quitRE.ReplaceAllStringFunc(updated, func(line string) string {
		matches := quitRE.FindStringSubmatch(line)
		if len(matches) != 2 {
			return line
		}
		return fmt.Sprintf(`quit = "%s"`, legacyCloseWeftBinding(matches[1]))
	})
	closeWeftRE := regexp.MustCompile(`(?m)^close_weft\s*=\s*"([^"\n]*)"\s*$`)
	closeWeftMatches := closeWeftRE.FindStringSubmatch(updated)
	hasQuit := quitRE.MatchString(updated)
	updated = closeWeftRE.ReplaceAllStringFunc(updated, func(line string) string {
		if hasQuit {
			return ""
		}
		matches := closeWeftRE.FindStringSubmatch(line)
		if len(matches) != 2 {
			return ""
		}
		hasQuit = true
		return fmt.Sprintf(`quit = "%s"`, legacyCloseWeftBinding(matches[1]))
	})
	if !hasQuit && len(closeWeftMatches) == 2 && strings.Contains(updated, "[key_bindings]") {
		updated = insertKeyBinding(updated, fmt.Sprintf(`quit = "%s"`, legacyCloseWeftBinding(closeWeftMatches[1])))
	}
	for _, line := range []string{
		`drawer = "C-b"`,
		`focus_left = "Left"`,
		`focus_right = "Right"`,
		`select_prev = "k"`,
		`select_next = "j"`,
		`open = "Enter"`,
		`new_workdir = "w"`,
		`new_group = "g"`,
		`new_agent = "n"`,
		`move_agent = "m"`,
		`delete = "d"`,
		`quit = "C-c"`,
	} {
		name := strings.SplitN(line, " ", 2)[0]
		if strings.Contains(updated, "[key_bindings]") && !regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(name)+`\s*=`).MatchString(updated) {
			updated = insertKeyBinding(updated, line)
		}
	}
	if updated == text {
		return nil
	}
	return os.WriteFile(path, []byte(updated), 0o600)
}

func DefaultConfigText() string {
	return `# Weft global runtime configuration.
# Run ` + "`weft config info`" + ` to see the runtime directory, state file, and
# supervisor socket.

# Command launched inside each Codex PTY owned by the supervisor.
codex_command = "codex"

# Global title template for agent rows.
title_template = "{status} {auto}"

# Optional command hook for generated titles. Weft sends each agent's first
# submitted Codex message to this command as JSON on stdin and uses the first
# non-empty stdout line as the generated title for {auto}.
title_hook_command = ""
title_hook_timeout_seconds = 10

[key_bindings]
drawer = "C-b"
focus_left = "Left"
focus_right = "Right"
select_prev = "k"
select_next = "j"
open = "Enter"
new_workdir = "w"
new_group = "g"
new_agent = "n"
move_agent = "m"
rename = "r"
delete = "d"
help = "?"
quit = "C-c"
`
}

func legacyCloseWeftBinding(binding string) string {
	normalized := strings.ToLower(strings.TrimSpace(binding))
	normalized = strings.ReplaceAll(normalized, "ctrl+", "c-")
	if normalized == "c-q" {
		return "C-c"
	}
	return binding
}

func insertKeyBinding(text string, line string) string {
	focusRE := regexp.MustCompile(`(?m)^focus_toggle\s*=\s*"[^"\n]*"\n`)
	if focusRE.MatchString(text) {
		return focusRE.ReplaceAllStringFunc(text, func(match string) string {
			return match + line + "\n"
		})
	}
	return strings.Replace(text, "[key_bindings]\n", "[key_bindings]\n"+line+"\n", 1)
}

func normalizeColumns(columns []string) []string {
	normalized := make([]string, 0, len(columns))
	for _, column := range columns {
		normalized = append(normalized, strings.TrimSpace(column))
	}
	if sameColumns(normalized, oldDefaultColumns) {
		return append([]string(nil), DefaultColumns...)
	}
	return normalized
}

func sameColumns(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
