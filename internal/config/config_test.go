package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureConfigCreatesDefaults(t *testing.T) {
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
	if cfg.KeyBindings.Quit != "C-c" {
		t.Fatalf("Quit = %q", cfg.KeyBindings.Quit)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"tmux_session", "columns", "new_workdir", "new_folder", "focus_toggle", "close_weft"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("default config should not include %q:\n%s", forbidden, data)
		}
	}
}

func TestLoadConfigAppliesCurrentKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
codex_command = "codex --model gpt-5"
title_template = "{title}"
title_hook_command = "hooks/title.sh"
title_hook_timeout_seconds = 3

[key_bindings]
drawer = "C-a"
focus_left = "h"
focus_right = "l"
select_prev = "Up"
select_next = "Down"
open = "Space"
new_workspace = "W"
new_group = "G"
new_agent = "A"
move_agent = "M"
rename = "R"
delete = "D"
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

	if cfg.CodexCommand != "codex --model gpt-5" || cfg.TitleTemplate != "{title}" {
		t.Fatalf("config values = %#v", cfg)
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
		cfg.KeyBindings.Rename != "R" ||
		cfg.KeyBindings.Delete != "D" ||
		cfg.KeyBindings.Help != "H" ||
		cfg.KeyBindings.Quit != "Q" {
		t.Fatalf("key bindings = %#v", cfg.KeyBindings)
	}
}

func TestLoadConfigIgnoresLegacyKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
tmux_session = "custom"
columns = ["Backlog", "Active"]
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
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	defaults := DefaultKeyBindings()
	if cfg.KeyBindings.Drawer != defaults.Drawer ||
		cfg.KeyBindings.SelectPrev != defaults.SelectPrev ||
		cfg.KeyBindings.SelectNext != defaults.SelectNext ||
		cfg.KeyBindings.NewWorkspace != defaults.NewWorkspace ||
		cfg.KeyBindings.NewGroup != defaults.NewGroup ||
		cfg.KeyBindings.NewAgent != defaults.NewAgent ||
		cfg.KeyBindings.Delete != defaults.Delete ||
		cfg.KeyBindings.Quit != defaults.Quit {
		t.Fatalf("legacy keys should be ignored, got %#v", cfg.KeyBindings)
	}
	if cfg.TitleHookTimeoutSeconds != 3 {
		t.Fatalf("current title timeout should still load, got %d", cfg.TitleHookTimeoutSeconds)
	}
}

func TestRuntimeIDIncludesSanitizedNameAndDigest(t *testing.T) {
	id := RuntimeID("/tmp/My Repo!")
	if !strings.HasPrefix(id, "my-repo-") {
		t.Fatalf("RuntimeID = %q", id)
	}
	if len(id) <= len("my-repo-") {
		t.Fatalf("RuntimeID missing digest: %q", id)
	}
}

func TestCurrentWorkspacePrefersWorkspaceEnv(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv(WorkspaceEnv, workspace)

	got, err := CurrentWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	if got != workspace {
		t.Fatalf("CurrentWorkspace = %q, want workspace env %q", got, workspace)
	}
}

func TestDefaultRuntimeIsGlobal(t *testing.T) {
	t.Setenv(AppDirEnv, "")
	dir, err := AppDir("/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(dir, "workspaces") || strings.Contains(dir, "project-") {
		t.Fatalf("AppDir should be global, got %q", dir)
	}
}
