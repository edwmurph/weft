package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureConfigCreatesDefaults(t *testing.T) {
	t.Setenv(RootEnv, "")
	t.Setenv(AppDirEnv, "")
	t.Setenv(WorkspaceEnv, "")
	dir := t.TempDir()
	rt := Runtime{
		Workspace:  dir,
		Dir:        filepath.Join(dir, "runtime"),
		ConfigPath: filepath.Join(dir, "runtime", "config.toml"),
		StatePath:  filepath.Join(dir, "runtime", "state.json"),
		SocketPath: filepath.Join(dir, "runtime", "weft.sock"),
	}

	cfg, err := EnsureConfig(rt)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.CodexCommand != "codex" {
		t.Fatalf("CodexCommand = %q", cfg.CodexCommand)
	}
	if cfg.TitleTemplate != "{status} {auto}" {
		t.Fatalf("TitleTemplate = %q", cfg.TitleTemplate)
	}
	if cfg.DefaultTaskType != DefaultTaskTypeCodex {
		t.Fatalf("DefaultTaskType = %q", cfg.DefaultTaskType)
	}
	codexType, ok := cfg.TaskTypes[DefaultTaskTypeCodex]
	if !ok {
		t.Fatalf("missing codex task type: %#v", cfg.TaskTypes)
	}
	if codexType.Kind != TaskKindCodex || codexType.Command != "codex" || codexType.Badge != "[codex]" || codexType.TitleTemplate != "{status} {auto}" {
		t.Fatalf("codex task type = %#v", codexType)
	}
	shellType, ok := cfg.TaskTypes[DefaultTaskTypeShell]
	if !ok {
		t.Fatalf("missing shell task type: %#v", cfg.TaskTypes)
	}
	if shellType.Kind != TaskKindTerminal || shellType.Command == "" || shellType.Badge != "[shell]" || shellType.TitleTemplate != "Shell" {
		t.Fatalf("shell task type = %#v", shellType)
	}
	if cfg.TitleHookCommand != "" {
		t.Fatalf("TitleHookCommand = %q", cfg.TitleHookCommand)
	}
	if cfg.TitleHookTimeoutSeconds != 10 {
		t.Fatalf("TitleHookTimeoutSeconds = %d", cfg.TitleHookTimeoutSeconds)
	}
	if cfg.KeyBindings.Drawer != "C-b" {
		t.Fatalf("Drawer = %q", cfg.KeyBindings.Drawer)
	}
	if cfg.KeyBindings.FocusLeft != "Left" || cfg.KeyBindings.FocusRight != "Right" {
		t.Fatalf("focus bindings = %q/%q", cfg.KeyBindings.FocusLeft, cfg.KeyBindings.FocusRight)
	}
	if cfg.KeyBindings.NewWorkspace != "w" || cfg.KeyBindings.NewGroup != "g" || cfg.KeyBindings.NewAgent != "n" {
		t.Fatalf("new bindings = %#v", cfg.KeyBindings)
	}
	if cfg.KeyBindings.MoveAgent != "m" {
		t.Fatalf("MoveAgent = %q", cfg.KeyBindings.MoveAgent)
	}
	if cfg.KeyBindings.Edit != "e" {
		t.Fatalf("Edit = %q", cfg.KeyBindings.Edit)
	}
	if cfg.KeyBindings.Delete != "Backspace" {
		t.Fatalf("Delete = %q", cfg.KeyBindings.Delete)
	}
	if cfg.KeyBindings.Quit != "C-c" {
		t.Fatalf("Quit = %q", cfg.KeyBindings.Quit)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	defaultText := "\n" + string(data)
	for _, expected := range []string{
		"\ndefault_task_type = \"codex\"",
		"\n[task_types.codex]",
		"\n[task_types.shell]",
		"\nnew_task = \"n\"",
		"\nmove_task = \"m\"",
	} {
		if !strings.Contains(defaultText, expected) {
			t.Fatalf("default config missing %q:\n%s", expected, data)
		}
	}
	for _, forbidden := range []string{"tmux_session", "columns", "new_workdir", "new_folder", "focus_toggle", "close_weft", "prev", "previous", "new", "close", "new_agent", "move_agent"} {
		if strings.Contains(defaultText, "\n"+forbidden+" ") {
			t.Fatalf("default config should not include %q:\n%s", forbidden, data)
		}
	}
}

func TestLoadConfigAppliesCurrentTaskKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
default_task_type = "shell"
title_hook_command = "hooks/title.sh"
title_hook_timeout_seconds = 3

[task_types.codex]
command = "codex --model gpt-5"
badge = "[codex-custom]"
title_template = "{title}"

[task_types.shell]
command = "exec bash -l"
badge = "[shell-custom]"
title_template = "Shell {title}"

[task_types.logs]
label = "Logs"
kind = "terminal"
command = "tail -f app.log"
badge = "[logs]"
title_template = "Logs"

[key_bindings]
drawer = "C-a"
focus_left = "h"
focus_right = "l"
select_prev = "Up"
select_next = "Down"
open = "Space"
new_workspace = "W"
new_group = "G"
new_task = "A"
move_task = "M"
edit = "E"
delete = "X"
help = "H"
quit = "Q"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DefaultTaskType != "shell" {
		t.Fatalf("DefaultTaskType = %q", cfg.DefaultTaskType)
	}
	if cfg.CodexCommand != "codex --model gpt-5" || cfg.TitleTemplate != "{title}" {
		t.Fatalf("config values = %#v", cfg)
	}
	if got := cfg.TaskTypes["codex"]; got.Command != "codex --model gpt-5" || got.Badge != "[codex-custom]" || got.TitleTemplate != "{title}" {
		t.Fatalf("codex task type = %#v", got)
	}
	if got := cfg.TaskTypes["shell"]; got.Command != "exec bash -l" || got.Badge != "[shell-custom]" || got.TitleTemplate != "Shell {title}" {
		t.Fatalf("shell task type = %#v", got)
	}
	if got := cfg.TaskTypes["logs"]; got.Label != "Logs" || got.Kind != TaskKindTerminal || got.Command != "tail -f app.log" || got.Badge != "[logs]" || got.TitleTemplate != "Logs" {
		t.Fatalf("logs task type = %#v", got)
	}
	if cfg.TitleHookCommand != "hooks/title.sh" || cfg.TitleHookTimeoutSeconds != 3 {
		t.Fatalf("title hook = %q/%d", cfg.TitleHookCommand, cfg.TitleHookTimeoutSeconds)
	}
	if cfg.KeyBindings.Drawer != "C-a" ||
		cfg.KeyBindings.FocusLeft != "h" ||
		cfg.KeyBindings.FocusRight != "l" ||
		cfg.KeyBindings.SelectPrev != "Up" ||
		cfg.KeyBindings.SelectNext != "Down" ||
		cfg.KeyBindings.Open != "Space" ||
		cfg.KeyBindings.NewWorkspace != "W" ||
		cfg.KeyBindings.NewGroup != "G" ||
		cfg.KeyBindings.NewAgent != "A" ||
		cfg.KeyBindings.MoveAgent != "M" ||
		cfg.KeyBindings.Edit != "E" ||
		cfg.KeyBindings.Delete != "X" ||
		cfg.KeyBindings.Help != "H" ||
		cfg.KeyBindings.Quit != "Q" {
		t.Fatalf("key bindings = %#v", cfg.KeyBindings)
	}
}

func TestLoadConfigTreatsLegacyDefaultDeleteAsBackspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
codex_command = "codex"
title_template = "{status} {auto}"
title_hook_timeout_seconds = 10

[key_bindings]
delete = "d"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.KeyBindings.Delete != "Backspace" {
		t.Fatalf("Delete = %q", cfg.KeyBindings.Delete)
	}
}

func TestLoadConfigAcceptsLegacyTaskIconAsBadge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
[task_types.codex]
icon = "[legacy-codex]"

[task_types.shell]
icon = "[legacy-shell]"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := cfg.TaskTypes["codex"]; got.Badge != "[legacy-codex]" {
		t.Fatalf("codex badge = %#v", got)
	}
	if got := cfg.TaskTypes["shell"]; got.Badge != "[legacy-shell]" {
		t.Fatalf("shell badge = %#v", got)
	}
}

func TestLoadConfigAppliesLegacyCodexKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
codex_command = "codex --model gpt-5"
title_template = "{title}"

[key_bindings]
new_agent = "A"
move_agent = "M"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.CodexCommand != "codex --model gpt-5" || cfg.TitleTemplate != "{title}" {
		t.Fatalf("legacy codex values = %#v", cfg)
	}
	if got := cfg.TaskTypes[DefaultTaskTypeCodex]; got.Command != "codex --model gpt-5" || got.TitleTemplate != "{title}" {
		t.Fatalf("legacy codex task type = %#v", got)
	}
	if cfg.KeyBindings.NewAgent != "A" || cfg.KeyBindings.MoveAgent != "M" {
		t.Fatalf("legacy task bindings = %#v", cfg.KeyBindings)
	}
}

func TestLoadConfigRejectsUnsupportedIntegratedTaskKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
[task_types.claude]
label = "Claude"
kind = "claude"
command = "claude"
badge = "[claude]"
title_template = "Claude"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	_, err = LoadConfig(path)
	if err == nil {
		t.Fatal("expected unsupported task type error")
	}
	if !strings.Contains(err.Error(), `kind "claude" is not supported`) ||
		!strings.Contains(err.Error(), `checked-in integrated type`) {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadConfigRejectsUnknownKeys(t *testing.T) {
	for name, body := range map[string]string{
		"top level": `
tmux_session = "custom"
columns = ["Backlog", "Active"]
title_hook_timeout_seconds = 3
`,
		"key bindings": `
title_hook_timeout_seconds = 3

[key_bindings]
focus_toggle = "C-g"
previous = "Left"
prev = "Left"
next = "Right"
new = "a"
new_workdir = "z"
new_folder = "x"
close = "x"
close_weft = "C-q"
`,
		"legacy rename binding": `
[key_bindings]
rename = "r"
`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := LoadConfig(path)
			if err == nil {
				t.Fatal("expected unknown key error")
			}
			if !strings.Contains(err.Error(), "unknown config key") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCurrentWorkspacePrefersWorkspaceEnv(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	t.Setenv(RootEnv, root)
	t.Setenv(WorkspaceEnv, workspace)

	got, err := CurrentWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	if got != workspace {
		t.Fatalf("CurrentWorkspace = %q, want workspace env %q", got, workspace)
	}
}

func TestRootEnvDerivesWorkspaceAndRuntime(t *testing.T) {
	t.Setenv(RootEnv, "")
	t.Setenv(AppDirEnv, "")
	t.Setenv(WorkspaceEnv, "")
	root := t.TempDir()
	t.Setenv(RootEnv, root)

	rt, err := ResolveRuntimeWithOptions(ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Workspace != root {
		t.Fatalf("Workspace = %q, want root env %q", rt.Workspace, root)
	}
	if rt.Dir != filepath.Join(root, ".weft") {
		t.Fatalf("Dir = %q, want root-local runtime", rt.Dir)
	}
	if !rt.HomeExplicit {
		t.Fatalf("RootEnv should count as an explicit source-build runtime: %#v", rt)
	}
}

func TestSourceCheckoutCWDCanDeriveWorkspaceAndRuntime(t *testing.T) {
	t.Setenv(RootEnv, "")
	t.Setenv(AppDirEnv, "")
	t.Setenv(WorkspaceEnv, "")
	root := writeSourceCheckout(t)
	t.Chdir(root)

	rt, err := ResolveRuntimeWithOptions(ResolveOptions{AutoRootFromCWD: true})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Workspace != root {
		t.Fatalf("Workspace = %q, want checkout cwd %q", rt.Workspace, root)
	}
	if rt.Dir != filepath.Join(root, ".weft") {
		t.Fatalf("Dir = %q, want cwd-local runtime", rt.Dir)
	}
	if !rt.HomeExplicit {
		t.Fatalf("auto-rooted checkout should count as explicit: %#v", rt)
	}
}

func TestSourceCheckoutCWDRespectsWorkspaceEnv(t *testing.T) {
	t.Setenv(RootEnv, "")
	t.Setenv(AppDirEnv, "")
	root := writeSourceCheckout(t)
	workspace := t.TempDir()
	t.Setenv(WorkspaceEnv, workspace)
	t.Chdir(root)

	rt, err := ResolveRuntimeWithOptions(ResolveOptions{AutoRootFromCWD: true})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Workspace != workspace {
		t.Fatalf("Workspace = %q, want workspace env %q", rt.Workspace, workspace)
	}
	if rt.Dir != filepath.Join(root, ".weft") {
		t.Fatalf("Dir = %q, want checkout-local runtime", rt.Dir)
	}
}

func TestAutoRootFromCWDRequiresSourceCheckout(t *testing.T) {
	t.Setenv(RootEnv, "")
	t.Setenv(AppDirEnv, "")
	t.Setenv(WorkspaceEnv, "")
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(workspace)

	rt, err := ResolveRuntimeWithOptions(ResolveOptions{AutoRootFromCWD: true})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Dir != filepath.Join(home, ".weft") {
		t.Fatalf("Dir = %q, want default home runtime", rt.Dir)
	}
	if rt.HomeExplicit {
		t.Fatalf("non-checkout cwd should not count as explicit: %#v", rt)
	}
}

func TestSpecificEnvOverridesRootEnv(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv(RootEnv, root)
	t.Setenv(WorkspaceEnv, workspace)
	t.Setenv(AppDirEnv, runtimeDir)

	rt, err := ResolveRuntimeWithOptions(ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Workspace != workspace {
		t.Fatalf("Workspace = %q, want specific workspace env %q", rt.Workspace, workspace)
	}
	if rt.Dir != runtimeDir {
		t.Fatalf("Dir = %q, want specific home env %q", rt.Dir, runtimeDir)
	}
}

func TestDefaultRuntimeIsGlobal(t *testing.T) {
	t.Setenv(RootEnv, "")
	t.Setenv(AppDirEnv, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	rt, err := ResolveRuntimeWithOptions(ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Dir != filepath.Join(home, ".weft") {
		t.Fatalf("runtime dir = %q, want global home runtime", rt.Dir)
	}
	if strings.HasSuffix(rt.Dir, "workspaces") || strings.Contains(rt.Dir, "project-") {
		t.Fatalf("runtime dir should be global, got %q", rt.Dir)
	}
}

func writeSourceCheckout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/edwmurph/weft\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmdDir := filepath.Join(root, "cmd", "weft")
	if err := os.MkdirAll(cmdDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}
