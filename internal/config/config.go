package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/edwmurph/weft/internal/tasktypes"
)

const (
	RootEnv                 = "WEFT_ROOT"
	AppDirEnv               = "WEFT_HOME"
	WorkspaceEnv            = "WEFT_WORKSPACE"
	AllowMainRuntimeEnv     = "WEFT_ALLOW_MAIN_RUNTIME"
	defaultRuntimeDirectory = ".weft"
	sourceRuntimeDirectory  = ".weft-runtime"
	modulePath              = "github.com/edwmurph/weft"
	DefaultTaskTypeCodex    = tasktypes.DefaultCodexID
	DefaultTaskTypeShell    = tasktypes.DefaultShellID
	TaskKindCodex           = tasktypes.KindCodex
	TaskKindTerminal        = tasktypes.KindTerminal
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
	NewTask      string `toml:"new_task"`
	MoveTask     string `toml:"move_task"`
	Edit         string `toml:"edit"`
	Delete       string `toml:"delete"`
	Repaint      string `toml:"repaint"`
	Help         string `toml:"help"`
	Quit         string `toml:"quit"`
}

type TaskType struct {
	ID            string `toml:"-"`
	Label         string `toml:"label"`
	Kind          string `toml:"kind"`
	Command       string `toml:"command"`
	Badge         string `toml:"badge"`
	TitleTemplate string `toml:"title_template"`
}

type TerminalAttention struct {
	Enabled          bool   `toml:"enabled"`
	RequestAttention string `toml:"request_attention"`
}

type TaskContext struct {
	Enabled bool `toml:"enabled"`
}

type Config struct {
	DefaultTaskType         string              `toml:"default_task_type"`
	TaskTypes               map[string]TaskType `toml:"task_types"`
	TitleHookCommand        string              `toml:"title_hook_command"`
	TitleHookTimeoutSeconds int                 `toml:"title_hook_timeout_seconds"`
	TerminalAttention       TerminalAttention   `toml:"terminal_attention"`
	TaskContext             TaskContext         `toml:"task_context"`
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
		NewTask:      "n",
		MoveTask:     "m",
		Edit:         "e",
		Delete:       "Backspace",
		Repaint:      "C-]",
		Help:         "?",
		Quit:         "C-c",
	}
}

func DefaultConfig() Config {
	taskTypes := DefaultTaskTypes()
	return Config{
		DefaultTaskType:         DefaultTaskTypeCodex,
		TaskTypes:               taskTypes,
		TitleHookCommand:        "",
		TitleHookTimeoutSeconds: 10,
		TerminalAttention: TerminalAttention{
			Enabled:          false,
			RequestAttention: "once",
		},
		TaskContext: TaskContext{Enabled: true},
		KeyBindings: DefaultKeyBindings(),
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
			TitleTemplate: "{live}",
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
		return filepath.Join(autoRoot, sourceRuntimeDirectory), true, nil
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
		DefaultTaskType         string              `toml:"default_task_type"`
		TaskTypes               map[string]TaskType `toml:"task_types"`
		TitleHookCommand        string              `toml:"title_hook_command"`
		TitleHookTimeoutSeconds int                 `toml:"title_hook_timeout_seconds"`
		TerminalAttention       struct {
			Enabled          bool   `toml:"enabled"`
			RequestAttention string `toml:"request_attention"`
		} `toml:"terminal_attention"`
		TaskContext struct {
			Enabled bool `toml:"enabled"`
		} `toml:"task_context"`
		KeyBindings struct {
			Drawer       string `toml:"drawer"`
			FocusLeft    string `toml:"focus_left"`
			FocusRight   string `toml:"focus_right"`
			SelectPrev   string `toml:"select_prev"`
			SelectNext   string `toml:"select_next"`
			Open         string `toml:"open"`
			NewWorkspace string `toml:"new_workspace"`
			NewGroup     string `toml:"new_group"`
			NewTask      string `toml:"new_task"`
			MoveTask     string `toml:"move_task"`
			Edit         string `toml:"edit"`
			Delete       string `toml:"delete"`
			Repaint      string `toml:"repaint"`
			Help         string `toml:"help"`
			Quit         string `toml:"quit"`
		} `toml:"key_bindings"`
	}
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return Config{}, ConfigError{Message: fmt.Sprintf("could not parse %s: %v", path, err)}
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Config{}, unknownConfigKeyError(path, undecoded)
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
	if md.IsDefined("terminal_attention", "enabled") {
		cfg.TerminalAttention.Enabled = raw.TerminalAttention.Enabled
	}
	if strings.TrimSpace(raw.TerminalAttention.RequestAttention) != "" {
		cfg.TerminalAttention.RequestAttention = raw.TerminalAttention.RequestAttention
	}
	cfg.TerminalAttention.RequestAttention = strings.ToLower(strings.TrimSpace(cfg.TerminalAttention.RequestAttention))
	if md.IsDefined("task_context", "enabled") {
		cfg.TaskContext.Enabled = raw.TaskContext.Enabled
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
	applyBinding(&cfg.KeyBindings.NewTask, raw.KeyBindings.NewTask)
	applyBinding(&cfg.KeyBindings.MoveTask, raw.KeyBindings.MoveTask)
	applyBinding(&cfg.KeyBindings.Edit, raw.KeyBindings.Edit)
	if strings.EqualFold(strings.TrimSpace(raw.KeyBindings.Delete), "d") {
		return Config{}, ConfigError{Message: fmt.Sprintf("unsupported config value in %s: key_bindings.delete cannot be \"d\"", path)}
	}
	applyBinding(&cfg.KeyBindings.Delete, raw.KeyBindings.Delete)
	applyBinding(&cfg.KeyBindings.Repaint, raw.KeyBindings.Repaint)
	applyBinding(&cfg.KeyBindings.Help, raw.KeyBindings.Help)
	applyBinding(&cfg.KeyBindings.Quit, raw.KeyBindings.Quit)
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
		if taskType.Badge == "" {
			taskType.Badge = "[" + id + "]"
		}
		taskType.TitleTemplate = strings.TrimSpace(taskType.TitleTemplate)
		next[id] = taskType
	}
	c.TaskTypes = next
	c.DefaultTaskType = strings.TrimSpace(c.DefaultTaskType)
	if c.DefaultTaskType == "" {
		c.DefaultTaskType = DefaultTaskTypeCodex
	}
}

func (c Config) Validate() error {
	c.normalizeTaskTypes()
	if c.TitleHookTimeoutSeconds <= 0 {
		return ConfigError{Message: "title_hook_timeout_seconds must be greater than zero"}
	}
	requestAttention := strings.ToLower(strings.TrimSpace(c.TerminalAttention.RequestAttention))
	if requestAttention == "" {
		requestAttention = "once"
	}
	switch requestAttention {
	case "off", "once":
	default:
		return ConfigError{Message: "terminal_attention.request_attention must be \"off\" or \"once\""}
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
		definition, ok := tasktypes.ForKind(taskType.Kind)
		if !ok {
			return ConfigError{Message: fmt.Sprintf("task_types.%s.kind %q is not supported; use %q for generic commands or a checked-in integrated type", id, taskType.Kind, TaskKindTerminal)}
		}
		if requiredID := definition.ConfiguredTypeID(); requiredID != "" && id != requiredID {
			return ConfigError{Message: fmt.Sprintf("task_types.%s.kind %q is reserved for the checked-in %s task type", id, taskType.Kind, requiredID)}
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
		if strings.Contains(taskType.TitleTemplate, "{codex}") {
			return ConfigError{Message: fmt.Sprintf("task_types.%s.title_template uses retired {codex}; use {live}", id)}
		}
	}
	for name, value := range map[string]string{
		"drawer": c.KeyBindings.Drawer, "focus_left": c.KeyBindings.FocusLeft, "focus_right": c.KeyBindings.FocusRight,
		"select_prev": c.KeyBindings.SelectPrev, "select_next": c.KeyBindings.SelectNext, "open": c.KeyBindings.Open,
		"new_workspace": c.KeyBindings.NewWorkspace, "new_group": c.KeyBindings.NewGroup, "new_task": c.KeyBindings.NewTask,
		"move_task": c.KeyBindings.MoveTask, "edit": c.KeyBindings.Edit, "delete": c.KeyBindings.Delete,
		"repaint": c.KeyBindings.Repaint, "help": c.KeyBindings.Help, "quit": c.KeyBindings.Quit,
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

func unknownConfigKeyError(path string, undecoded []toml.Key) ConfigError {
	keys := make([]string, 0, len(undecoded))
	for _, key := range undecoded {
		keys = append(keys, key.String())
	}
	sort.Strings(keys)
	return ConfigError{Message: fmt.Sprintf("unknown config key(s) in %s: %s", path, strings.Join(keys, ", "))}
}

func (c Config) TaskType(id string) (TaskType, bool) {
	c.normalizeTaskTypes()
	taskType, ok := c.TaskTypes[strings.TrimSpace(id)]
	return taskType, ok
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

func Fingerprint(c Config) string {
	c.normalizeTaskTypes()
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
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

[terminal_attention]
enabled = false
request_attention = "once"

[task_context]
enabled = true

[task_types.codex]
label = "Codex"
kind = "codex"
command = "codex"
badge = "[codex]"
title_template = "{live}"

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
repaint = "C-]"
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
