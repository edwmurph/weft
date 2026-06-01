package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	RootEnv                 = "WEFT_ROOT"
	AppDirEnv               = "WEFT_HOME"
	WorkspaceEnv            = "WEFT_WORKSPACE"
	AllowMainRuntimeEnv     = "WEFT_ALLOW_MAIN_RUNTIME"
	defaultRuntimeDirectory = ".weft"
	modulePath              = "github.com/edwmurph/weft"
	DefaultTaskTypeCodex    = "codex"
	DefaultTaskTypeShell    = "shell"
	TaskKindCodex           = "codex"
	TaskKindTerminal        = "terminal"
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
	Edit         string `toml:"edit"`
	Delete       string `toml:"delete"`
	Help         string `toml:"help"`
	Quit         string `toml:"quit"`
}

type TaskType struct {
	ID            string `toml:"-"`
	Label         string `toml:"label"`
	Kind          string `toml:"kind"`
	Command       string `toml:"command"`
	Badge         string `toml:"badge"`
	Icon          string `toml:"icon"`
	TitleTemplate string `toml:"title_template"`
}

type Config struct {
	CodexCommand            string              `toml:"codex_command"`
	TitleTemplate           string              `toml:"title_template"`
	DefaultTaskType         string              `toml:"default_task_type"`
	TaskTypes               map[string]TaskType `toml:"task_types"`
	TitleHookCommand        string              `toml:"title_hook_command"`
	TitleHookTimeoutSeconds int                 `toml:"title_hook_timeout_seconds"`
	KeyBindings             KeyBindings         `toml:"key_bindings"`
}

type Runtime struct {
	Workspace    string
	Dir          string
	ConfigPath   string
	StatePath    string
	SocketPath   string
	HomeExplicit bool
}

type ResolveOptions struct {
	AutoRootFromCWD bool
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
		Edit:         "e",
		Delete:       "Backspace",
		Help:         "?",
		Quit:         "C-c",
	}
}

func DefaultConfig() Config {
	taskTypes := DefaultTaskTypes()
	return Config{
		CodexCommand:            "codex",
		TitleTemplate:           "{status} {auto}",
		DefaultTaskType:         DefaultTaskTypeCodex,
		TaskTypes:               taskTypes,
		TitleHookCommand:        "",
		TitleHookTimeoutSeconds: 10,
		KeyBindings:             DefaultKeyBindings(),
	}
}

func DefaultTaskTypes() map[string]TaskType {
	return map[string]TaskType{
		DefaultTaskTypeCodex: {
			ID:            DefaultTaskTypeCodex,
			Label:         "Codex",
			Kind:          TaskKindCodex,
			Command:       "codex",
			Badge:         "[codex]",
			TitleTemplate: "{status} {auto}",
		},
		DefaultTaskTypeShell: {
			ID:            DefaultTaskTypeShell,
			Label:         "Shell",
			Kind:          TaskKindTerminal,
			Command:       `exec "$SHELL" -l`,
			Badge:         "[shell]",
			TitleTemplate: "Shell",
		},
	}
}

func ResolveRuntimeWithOptions(options ResolveOptions) (Runtime, error) {
	workspace, err := CurrentWorkspace()
	if err != nil {
		return Runtime{}, err
	}
	autoRoot, err := autoRootFromCWD(options)
	if err != nil {
		return Runtime{}, err
	}
	dir, explicit, err := appDirInfo(workspace, autoRoot)
	if err != nil {
		return Runtime{}, err
	}
	return Runtime{
		Workspace:    workspace,
		Dir:          dir,
		ConfigPath:   filepath.Join(dir, "config.toml"),
		StatePath:    filepath.Join(dir, "state.json"),
		SocketPath:   filepath.Join(dir, "weft.sock"),
		HomeExplicit: explicit,
	}, nil
}

func CurrentWorkspace() (string, error) {
	if configured := os.Getenv(WorkspaceEnv); configured != "" {
		return filepath.Abs(expandHome(configured))
	}
	if configured := os.Getenv(RootEnv); configured != "" {
		return filepath.Abs(expandHome(configured))
	}
	return os.Getwd()
}

func appDirInfo(workspace string, autoRoot string) (string, bool, error) {
	if configured := os.Getenv(AppDirEnv); configured != "" {
		dir, err := filepath.Abs(expandHome(configured))
		return dir, true, err
	}
	if configured := os.Getenv(RootEnv); configured != "" {
		root, err := filepath.Abs(expandHome(configured))
		if err != nil {
			return "", true, err
		}
		return filepath.Join(root, defaultRuntimeDirectory), true, nil
	}
	if autoRoot != "" {
		return filepath.Join(autoRoot, defaultRuntimeDirectory), true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(home, defaultRuntimeDirectory), false, nil
}

func autoRootFromCWD(options ResolveOptions) (string, error) {
	if !options.AutoRootFromCWD || os.Getenv(AppDirEnv) != "" || os.Getenv(RootEnv) != "" {
		return "", nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, ok := sourceCheckoutRoot(cwd)
	if !ok {
		return "", nil
	}
	return root, nil
}

func sourceCheckoutRoot(dir string) (string, bool) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", false
	}
	if modulePathFromGoMod(data) != modulePath {
		return "", false
	}
	if info, err := os.Stat(filepath.Join(root, "cmd", "weft", "main.go")); err != nil || info.IsDir() {
		return "", false
	}
	return root, true
}

func modulePathFromGoMod(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	return ""
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
		CodexCommand            string              `toml:"codex_command"`
		TitleTemplate           string              `toml:"title_template"`
		DefaultTaskType         string              `toml:"default_task_type"`
		TaskTypes               map[string]TaskType `toml:"task_types"`
		TitleHookCommand        string              `toml:"title_hook_command"`
		TitleHookTimeoutSeconds int                 `toml:"title_hook_timeout_seconds"`
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
			NewTask      string `toml:"new_task"`
			MoveAgent    string `toml:"move_agent"`
			MoveTask     string `toml:"move_task"`
			Edit         string `toml:"edit"`
			Delete       string `toml:"delete"`
			Help         string `toml:"help"`
			Quit         string `toml:"quit"`
		} `toml:"key_bindings"`
	}
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return Config{}, ConfigError{Message: fmt.Sprintf("could not parse %s: %v", path, err)}
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		hasLegacyRename := false
		for _, key := range undecoded {
			keys = append(keys, key.String())
			if key.String() == "key_bindings.rename" {
				hasLegacyRename = true
			}
		}
		if hasLegacyRename {
			return Config{}, ConfigError{Message: fmt.Sprintf("unknown config key(s) in %s: key_bindings.rename (dashboard shortcut was renamed; use key_bindings.edit = \"e\" and delete key_bindings.rename)", path)}
		}
		return Config{}, ConfigError{Message: fmt.Sprintf("unknown config key(s) in %s: %s", path, strings.Join(keys, ", "))}
	}
	if raw.CodexCommand != "" {
		cfg.CodexCommand = raw.CodexCommand
		codex := cfg.TaskTypes[DefaultTaskTypeCodex]
		codex.Command = raw.CodexCommand
		cfg.TaskTypes[DefaultTaskTypeCodex] = codex
	}
	if raw.TitleTemplate != "" {
		cfg.TitleTemplate = raw.TitleTemplate
		codex := cfg.TaskTypes[DefaultTaskTypeCodex]
		codex.TitleTemplate = raw.TitleTemplate
		cfg.TaskTypes[DefaultTaskTypeCodex] = codex
	}
	if raw.DefaultTaskType != "" {
		cfg.DefaultTaskType = raw.DefaultTaskType
	}
	for id, rawTaskType := range raw.TaskTypes {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		taskType := cfg.TaskTypes[id]
		if strings.TrimSpace(taskType.ID) == "" {
			taskType.ID = id
		}
		if strings.TrimSpace(rawTaskType.Label) != "" {
			taskType.Label = rawTaskType.Label
		}
		if strings.TrimSpace(rawTaskType.Kind) != "" {
			taskType.Kind = rawTaskType.Kind
		}
		if strings.TrimSpace(rawTaskType.Command) != "" {
			taskType.Command = rawTaskType.Command
		}
		if strings.TrimSpace(rawTaskType.Badge) != "" {
			taskType.Badge = rawTaskType.Badge
		}
		if strings.TrimSpace(rawTaskType.Icon) != "" {
			if strings.TrimSpace(rawTaskType.Badge) == "" {
				taskType.Badge = rawTaskType.Icon
			}
			taskType.Icon = rawTaskType.Icon
		}
		if strings.TrimSpace(rawTaskType.TitleTemplate) != "" {
			taskType.TitleTemplate = rawTaskType.TitleTemplate
		}
		cfg.TaskTypes[id] = taskType
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
	applyBinding(&cfg.KeyBindings.NewAgent, raw.KeyBindings.NewTask)
	applyBinding(&cfg.KeyBindings.MoveAgent, raw.KeyBindings.MoveAgent)
	applyBinding(&cfg.KeyBindings.MoveAgent, raw.KeyBindings.MoveTask)
	applyBinding(&cfg.KeyBindings.Edit, raw.KeyBindings.Edit)
	if !strings.EqualFold(strings.TrimSpace(raw.KeyBindings.Delete), "d") {
		applyBinding(&cfg.KeyBindings.Delete, raw.KeyBindings.Delete)
	}
	applyBinding(&cfg.KeyBindings.Help, raw.KeyBindings.Help)
	applyBinding(&cfg.KeyBindings.Quit, raw.KeyBindings.Quit)
	if codex, ok := cfg.TaskTypes[DefaultTaskTypeCodex]; ok {
		cfg.CodexCommand = codex.Command
		cfg.TitleTemplate = codex.TitleTemplate
	}
	cfg.normalizeTaskTypes()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) normalizeTaskTypes() {
	if c.TaskTypes == nil {
		c.TaskTypes = DefaultTaskTypes()
	}
	next := make(map[string]TaskType, len(c.TaskTypes))
	for id, taskType := range c.TaskTypes {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		taskType.ID = id
		taskType.Label = strings.TrimSpace(taskType.Label)
		if taskType.Label == "" {
			taskType.Label = id
		}
		taskType.Kind = strings.TrimSpace(taskType.Kind)
		taskType.Command = strings.TrimSpace(taskType.Command)
		taskType.Badge = strings.TrimSpace(taskType.Badge)
		taskType.Icon = strings.TrimSpace(taskType.Icon)
		if taskType.Badge == "" {
			if taskType.Icon != "" {
				taskType.Badge = taskType.Icon
			} else {
				taskType.Badge = "[" + id + "]"
			}
		}
		taskType.TitleTemplate = strings.TrimSpace(taskType.TitleTemplate)
		next[id] = taskType
	}
	c.TaskTypes = next
	c.DefaultTaskType = strings.TrimSpace(c.DefaultTaskType)
	if c.DefaultTaskType == "" {
		c.DefaultTaskType = DefaultTaskTypeCodex
	}
	if codex, ok := c.TaskTypes[DefaultTaskTypeCodex]; ok {
		c.CodexCommand = codex.Command
		c.TitleTemplate = codex.TitleTemplate
	}
}

func (c Config) Validate() error {
	c.normalizeTaskTypes()
	if c.TitleHookTimeoutSeconds <= 0 {
		return ConfigError{Message: "title_hook_timeout_seconds must be greater than zero"}
	}
	if _, ok := c.TaskTypes[c.DefaultTaskType]; !ok {
		return ConfigError{Message: fmt.Sprintf("default_task_type %q is not defined in task_types", c.DefaultTaskType)}
	}
	if _, ok := c.TaskTypes[DefaultTaskTypeCodex]; !ok {
		return ConfigError{Message: "task_types.codex must be defined"}
	}
	for id, taskType := range c.TaskTypes {
		if !validTaskTypeID(id) {
			return ConfigError{Message: fmt.Sprintf("task type id %q must contain only letters, numbers, dash, or underscore", id)}
		}
		if strings.TrimSpace(taskType.Kind) == "" {
			return ConfigError{Message: fmt.Sprintf("task_types.%s.kind must be a non-empty string", id)}
		}
		if taskType.Kind != TaskKindCodex && taskType.Kind != TaskKindTerminal {
			return ConfigError{Message: fmt.Sprintf("task_types.%s.kind %q is not supported; use %q for generic commands or a checked-in integrated type", id, taskType.Kind, TaskKindTerminal)}
		}
		if taskType.Kind == TaskKindCodex && id != DefaultTaskTypeCodex {
			return ConfigError{Message: fmt.Sprintf("task_types.%s.kind %q is reserved for the checked-in codex task type", id, TaskKindCodex)}
		}
		if strings.TrimSpace(taskType.Command) == "" {
			return ConfigError{Message: fmt.Sprintf("task_types.%s.command must be a non-empty string", id)}
		}
		if strings.TrimSpace(taskType.Badge) == "" {
			return ConfigError{Message: fmt.Sprintf("task_types.%s.badge must be a non-empty string", id)}
		}
		if strings.TrimSpace(taskType.TitleTemplate) == "" {
			return ConfigError{Message: fmt.Sprintf("task_types.%s.title_template must be a non-empty string", id)}
		}
	}
	for name, value := range map[string]string{
		"drawer": c.KeyBindings.Drawer, "focus_left": c.KeyBindings.FocusLeft, "focus_right": c.KeyBindings.FocusRight,
		"select_prev": c.KeyBindings.SelectPrev, "select_next": c.KeyBindings.SelectNext, "open": c.KeyBindings.Open,
		"new_workspace": c.KeyBindings.NewWorkspace, "new_group": c.KeyBindings.NewGroup, "new_task": c.KeyBindings.NewAgent,
		"move_task": c.KeyBindings.MoveAgent, "edit": c.KeyBindings.Edit, "delete": c.KeyBindings.Delete,
		"help": c.KeyBindings.Help, "quit": c.KeyBindings.Quit,
	} {
		if strings.TrimSpace(value) == "" {
			return ConfigError{Message: fmt.Sprintf("key binding %q must be a non-empty string", name)}
		}
	}
	return nil
}

func validTaskTypeID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func (c Config) TaskType(id string) (TaskType, bool) {
	c.normalizeTaskTypes()
	taskType, ok := c.TaskTypes[strings.TrimSpace(id)]
	return taskType, ok
}

func (c Config) TaskTypeOrDefault(id string) TaskType {
	c.normalizeTaskTypes()
	if taskType, ok := c.TaskTypes[strings.TrimSpace(id)]; ok {
		return taskType
	}
	if taskType, ok := c.TaskTypes[c.DefaultTaskType]; ok {
		return taskType
	}
	return c.TaskTypes[DefaultTaskTypeCodex]
}

func (c Config) OrderedTaskTypes() []TaskType {
	c.normalizeTaskTypes()
	keys := make([]string, 0, len(c.TaskTypes))
	for id := range c.TaskTypes {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	ordered := make([]TaskType, 0, len(keys))
	for _, id := range keys {
		ordered = append(ordered, c.TaskTypes[id])
	}
	return ordered
}

func DefaultConfigText() string {
	return `# Weft global runtime configuration.
# Run ` + "`weft config info`" + ` to see the runtime directory, state file, and
# supervisor socket.

default_task_type = "codex"

# Optional command hook for generated titles. Weft sends each Codex task's first
# submitted message, or each opted-in terminal task's first command, to this
# command as JSON on stdin and uses the first non-empty stdout line as the
# generated title for {auto}.
title_hook_command = ""
title_hook_timeout_seconds = 10

[task_types.codex]
label = "Codex"
kind = "codex"
command = "codex"
badge = "[codex]"
title_template = "{status} {auto}"

[task_types.shell]
label = "Shell"
kind = "terminal"
command = "exec \"$SHELL\" -l"
badge = "[shell]"
title_template = "Shell"

[key_bindings]
drawer = "C-b"
focus_left = "Left"
focus_right = "Right"
select_prev = "k"
select_next = "j"
open = "Enter"
new_workspace = "w"
new_group = "g"
new_task = "n"
move_task = "m"
edit = "e"
delete = "Backspace"
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
