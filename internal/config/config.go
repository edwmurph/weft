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
	AppDirEnv  = "CODUX_HOME"
	WorkdirEnv = "CODUX_WORKDIR"
)

var (
	DefaultColumns    = []string{"inbox", "implement", "ship"}
	oldDefaultColumns = []string{"Backlog", "Active", "Review", "Done"}
)

type KeyBindings struct {
	New         string `toml:"new"`
	Prev        string `toml:"prev"`
	Next        string `toml:"next"`
	MoveLeft    string `toml:"move_left"`
	MoveRight   string `toml:"move_right"`
	Rename      string `toml:"rename"`
	Close       string `toml:"close"`
	Help        string `toml:"help"`
	FocusToggle string `toml:"focus_toggle"`
	CloseCodux  string `toml:"close_codux"`
}

type Config struct {
	TmuxSession  string      `toml:"tmux_session"`
	CodexCommand string      `toml:"codex_command"`
	Columns      []string    `toml:"columns"`
	KeyBindings  KeyBindings `toml:"key_bindings"`
}

type Runtime struct {
	Workdir    string
	Dir        string
	ConfigPath string
	StatePath  string
	SocketPath string
}

type ConfigError struct {
	Message string
}

func (e ConfigError) Error() string {
	return e.Message
}

func DefaultKeyBindings() KeyBindings {
	return KeyBindings{
		New:         "n",
		Prev:        "Left",
		Next:        "Right",
		MoveLeft:    "S-Left",
		MoveRight:   "S-Right",
		Rename:      "r",
		Close:       "c",
		Help:        "?",
		FocusToggle: "C-g",
		CloseCodux:  "C-c",
	}
}

func DefaultConfig(defaultSession string) Config {
	return Config{
		TmuxSession:  defaultSession,
		CodexCommand: "codex",
		Columns:      append([]string(nil), DefaultColumns...),
		KeyBindings:  DefaultKeyBindings(),
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
		Workdir:    workdir,
		Dir:        dir,
		ConfigPath: filepath.Join(dir, "config.toml"),
		StatePath:  filepath.Join(dir, "state.json"),
		SocketPath: filepath.Join(dir, "codux.sock"),
	}, nil
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
	return filepath.Join(home, ".codux", "workdirs", RuntimeID(workdir)), nil
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
	if os.Getenv(AppDirEnv) != "" && os.Getenv(WorkdirEnv) == "" {
		return "codux"
	}
	return "codux-" + RuntimeID(workdir)
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
		TmuxSession  string   `toml:"tmux_session"`
		CodexCommand string   `toml:"codex_command"`
		Columns      []string `toml:"columns"`
		KeyBindings  struct {
			New         string `toml:"new"`
			Prev        string `toml:"prev"`
			Previous    string `toml:"previous"`
			Next        string `toml:"next"`
			MoveLeft    string `toml:"move_left"`
			MoveRight   string `toml:"move_right"`
			Rename      string `toml:"rename"`
			Close       string `toml:"close"`
			Help        string `toml:"help"`
			FocusToggle string `toml:"focus_toggle"`
			CloseCodux  string `toml:"close_codux"`
			Quit        string `toml:"quit"`
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
	if raw.Columns != nil {
		cfg.Columns = normalizeColumns(raw.Columns)
	}
	applyBinding := func(target *string, value string) {
		if strings.TrimSpace(value) != "" {
			*target = value
		}
	}
	applyBinding(&cfg.KeyBindings.New, raw.KeyBindings.New)
	if raw.KeyBindings.Prev != "" {
		applyBinding(&cfg.KeyBindings.Prev, raw.KeyBindings.Prev)
	} else {
		applyBinding(&cfg.KeyBindings.Prev, raw.KeyBindings.Previous)
	}
	applyBinding(&cfg.KeyBindings.Next, raw.KeyBindings.Next)
	applyBinding(&cfg.KeyBindings.MoveLeft, raw.KeyBindings.MoveLeft)
	applyBinding(&cfg.KeyBindings.MoveRight, raw.KeyBindings.MoveRight)
	applyBinding(&cfg.KeyBindings.Rename, raw.KeyBindings.Rename)
	applyBinding(&cfg.KeyBindings.Close, raw.KeyBindings.Close)
	applyBinding(&cfg.KeyBindings.Help, raw.KeyBindings.Help)
	applyBinding(&cfg.KeyBindings.FocusToggle, raw.KeyBindings.FocusToggle)
	if raw.KeyBindings.CloseCodux != "" {
		applyBinding(&cfg.KeyBindings.CloseCodux, raw.KeyBindings.CloseCodux)
	} else if raw.KeyBindings.Quit != "" {
		applyBinding(&cfg.KeyBindings.CloseCodux, legacyCloseCoduxBinding(raw.KeyBindings.Quit))
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.TmuxSession) == "" {
		return ConfigError{Message: "tmux_session must be a non-empty string"}
	}
	if strings.TrimSpace(c.CodexCommand) == "" {
		return ConfigError{Message: "codex_command must be a non-empty string"}
	}
	if len(c.Columns) == 0 {
		return ConfigError{Message: "columns must include at least one column"}
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
		"new": c.KeyBindings.New, "prev": c.KeyBindings.Prev, "next": c.KeyBindings.Next,
		"move_left": c.KeyBindings.MoveLeft, "move_right": c.KeyBindings.MoveRight,
		"rename": c.KeyBindings.Rename, "close": c.KeyBindings.Close, "help": c.KeyBindings.Help,
		"focus_toggle": c.KeyBindings.FocusToggle, "close_codux": c.KeyBindings.CloseCodux,
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
	updated = strings.ReplaceAll(updated, `move_left = "H"`, `move_left = "S-Left"`)
	updated = strings.ReplaceAll(updated, `move_right = "L"`, `move_right = "S-Right"`)
	updated = strings.ReplaceAll(updated, `close = "x"`, `close = "c"`)
	updated = strings.ReplaceAll(updated, `focus_toggle = "C-a"`, `focus_toggle = "C-g"`)
	updated = strings.ReplaceAll(updated, `focus_toggle = "C-d"`, `focus_toggle = "C-g"`)
	updated = regexp.MustCompile(`(?m)^sessions\s*=\s*"[^"\n]*"\n?`).ReplaceAllString(updated, "")
	updated = strings.ReplaceAll(updated, `close_codux = "C-q"`, `close_codux = "C-c"`)
	quitRE := regexp.MustCompile(`(?m)^quit\s*=\s*"([^"\n]*)"\s*$`)
	if strings.Contains(updated, "\nclose_codux =") {
		updated = quitRE.ReplaceAllString(updated, "")
	} else {
		updated = quitRE.ReplaceAllStringFunc(updated, func(line string) string {
			matches := quitRE.FindStringSubmatch(line)
			if len(matches) != 2 {
				return line
			}
			return fmt.Sprintf(`close_codux = "%s"`, legacyCloseCoduxBinding(matches[1]))
		})
	}
	if strings.Contains(updated, "[key_bindings]") && !strings.Contains(updated, "\nclose_codux =") {
		updated = insertKeyBinding(updated, `close_codux = "C-c"`)
	}
	if updated == text {
		return nil
	}
	return os.WriteFile(path, []byte(updated), 0o600)
}

func DefaultConfigText() string {
	return `# Codux runtime configuration for one launch directory.
# Run ` + "`codux config info`" + ` to see the workdir, runtime directory, state file, and
# generated tmux session. Set tmux_session only when you need to override it.

# Command launched inside each Codex PTY owned by the dashboard TUI.
codex_command = "codex"

# Ordered columns shown in the dashboard.
columns = ["inbox", "implement", "ship"]

[key_bindings]
new = "n"
prev = "Left"
next = "Right"
move_left = "S-Left"
move_right = "S-Right"
rename = "r"
close = "c"
help = "?"
focus_toggle = "C-g"
close_codux = "C-c"
`
}

func legacyCloseCoduxBinding(binding string) string {
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
