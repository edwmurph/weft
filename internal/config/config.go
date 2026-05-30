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
	AppDirEnv    = "WEFT_HOME"
	WorkspaceEnv = "WEFT_WORKSPACE"
)

type KeyBindings struct {
	Drawer       string `toml:"drawer"`
	FocusLeft    string `toml:"focus_left"`
	FocusRight   string `toml:"focus_right"`
	SelectPrev   string `toml:"select_prev"`
	SelectNext   string `toml:"select_next"`
	Open         string `toml:"open"`
	NewWorkspace string `toml:"new_workspace"`
	NewGroup     string `toml:"new_group"`
	NewAgent     string `toml:"new_agent"`
	MoveAgent    string `toml:"move_agent"`
	Rename       string `toml:"rename"`
	Delete       string `toml:"delete"`
	Help         string `toml:"help"`
	Quit         string `toml:"quit"`
}

type Config struct {
	CodexCommand            string      `toml:"codex_command"`
	TitleTemplate           string      `toml:"title_template"`
	TitleHookCommand        string      `toml:"title_hook_command"`
	TitleHookTimeoutSeconds int         `toml:"title_hook_timeout_seconds"`
	KeyBindings             KeyBindings `toml:"key_bindings"`
}

type Runtime struct {
	Workspace  string
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
		Drawer:       "C-b",
		FocusLeft:    "Left",
		FocusRight:   "Right",
		SelectPrev:   "k",
		SelectNext:   "j",
		Open:         "Enter",
		NewWorkspace: "w",
		NewGroup:     "g",
		NewAgent:     "n",
		MoveAgent:    "m",
		Rename:       "r",
		Delete:       "d",
		Help:         "?",
		Quit:         "C-c",
	}
}

func DefaultConfig() Config {
	return Config{
		CodexCommand:            "codex",
		TitleTemplate:           "{status} {auto}",
		TitleHookCommand:        "",
		TitleHookTimeoutSeconds: 10,
		KeyBindings:             DefaultKeyBindings(),
	}
}

func ResolveRuntime() (Runtime, error) {
	workspace, err := CurrentWorkspace()
	if err != nil {
		return Runtime{}, err
	}
	dir, err := AppDir(workspace)
	if err != nil {
		return Runtime{}, err
	}
	return Runtime{
		Workspace:  workspace,
		Dir:        dir,
		ConfigPath: filepath.Join(dir, "config.toml"),
		StatePath:  filepath.Join(dir, "state.json"),
		SocketPath: filepath.Join(dir, "weft.sock"),
	}, nil
}

func CurrentWorkspace() (string, error) {
	if configured := os.Getenv(WorkspaceEnv); configured != "" {
		return filepath.Abs(expandHome(configured))
	}
	return os.Getwd()
}

func AppDir(workspace string) (string, error) {
	if configured := os.Getenv(AppDirEnv); configured != "" {
		return filepath.Abs(expandHome(configured))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".weft"), nil
}

func RuntimeID(workspace string) string {
	name := strings.ToLower(filepath.Base(workspace))
	name = regexp.MustCompile(`[^A-Za-z0-9_-]+`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "workspace"
	}
	sum := sha1.Sum([]byte(workspace))
	return fmt.Sprintf("%s-%s", name, hex.EncodeToString(sum[:])[:8])
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
	}
	return LoadConfig(rt.ConfigPath)
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	var raw struct {
		CodexCommand            string `toml:"codex_command"`
		TitleTemplate           string `toml:"title_template"`
		TitleHookCommand        string `toml:"title_hook_command"`
		TitleHookTimeoutSeconds int    `toml:"title_hook_timeout_seconds"`
		KeyBindings             struct {
			Drawer       string `toml:"drawer"`
			FocusLeft    string `toml:"focus_left"`
			FocusRight   string `toml:"focus_right"`
			SelectPrev   string `toml:"select_prev"`
			SelectNext   string `toml:"select_next"`
			Open         string `toml:"open"`
			NewWorkspace string `toml:"new_workspace"`
			NewGroup     string `toml:"new_group"`
			NewAgent     string `toml:"new_agent"`
			MoveAgent    string `toml:"move_agent"`
			Rename       string `toml:"rename"`
			Delete       string `toml:"delete"`
			Help         string `toml:"help"`
			Quit         string `toml:"quit"`
		} `toml:"key_bindings"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return Config{}, ConfigError{Message: fmt.Sprintf("could not parse %s: %v", path, err)}
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
	applyBinding := func(target *string, value string) {
		if strings.TrimSpace(value) != "" {
			*target = value
		}
	}
	applyBinding(&cfg.KeyBindings.Drawer, raw.KeyBindings.Drawer)
	applyBinding(&cfg.KeyBindings.FocusLeft, raw.KeyBindings.FocusLeft)
	applyBinding(&cfg.KeyBindings.FocusRight, raw.KeyBindings.FocusRight)
	applyBinding(&cfg.KeyBindings.SelectPrev, raw.KeyBindings.SelectPrev)
	applyBinding(&cfg.KeyBindings.SelectNext, raw.KeyBindings.SelectNext)
	applyBinding(&cfg.KeyBindings.Open, raw.KeyBindings.Open)
	applyBinding(&cfg.KeyBindings.NewWorkspace, raw.KeyBindings.NewWorkspace)
	applyBinding(&cfg.KeyBindings.NewGroup, raw.KeyBindings.NewGroup)
	applyBinding(&cfg.KeyBindings.NewAgent, raw.KeyBindings.NewAgent)
	applyBinding(&cfg.KeyBindings.MoveAgent, raw.KeyBindings.MoveAgent)
	applyBinding(&cfg.KeyBindings.Rename, raw.KeyBindings.Rename)
	applyBinding(&cfg.KeyBindings.Delete, raw.KeyBindings.Delete)
	applyBinding(&cfg.KeyBindings.Help, raw.KeyBindings.Help)
	applyBinding(&cfg.KeyBindings.Quit, raw.KeyBindings.Quit)
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
	for name, value := range map[string]string{
		"drawer": c.KeyBindings.Drawer, "focus_left": c.KeyBindings.FocusLeft, "focus_right": c.KeyBindings.FocusRight,
		"select_prev": c.KeyBindings.SelectPrev, "select_next": c.KeyBindings.SelectNext, "open": c.KeyBindings.Open,
		"new_workspace": c.KeyBindings.NewWorkspace, "new_group": c.KeyBindings.NewGroup, "new_agent": c.KeyBindings.NewAgent,
		"move_agent": c.KeyBindings.MoveAgent, "rename": c.KeyBindings.Rename, "delete": c.KeyBindings.Delete,
		"help": c.KeyBindings.Help, "quit": c.KeyBindings.Quit,
	} {
		if strings.TrimSpace(value) == "" {
			return ConfigError{Message: fmt.Sprintf("key binding %q must be a non-empty string", name)}
		}
	}
	return nil
}

func DefaultConfigText() string {
	return `# Weft global runtime configuration.
# Run ` + "`weft config info`" + ` to see the runtime directory, state file, and
# supervisor socket.

# Command launched inside each Codex PTY owned by the supervisor.
codex_command = "codex"

# Default title template copied into new agents.
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
new_workspace = "w"
new_group = "g"
new_agent = "n"
move_agent = "m"
rename = "r"
delete = "d"
help = "?"
quit = "C-c"
`
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
