package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureConfigCreatesDefaults(t *testing.T) {
	t.Setenv(AppDirEnv, "")
	t.Setenv(WorkdirEnv, "")
	dir := t.TempDir()
	rt := Runtime{
		Workdir: dir, Dir: filepath.Join(dir, "runtime"),
		ConfigPath: filepath.Join(dir, "runtime", "config.toml"),
		StatePath:  filepath.Join(dir, "runtime", "state.json"),
		SocketPath: filepath.Join(dir, "runtime", "codux.sock"),
	}

	cfg, err := EnsureConfig(rt)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.CodexCommand != "codex" {
		t.Fatalf("CodexCommand = %q", cfg.CodexCommand)
	}
	if cfg.TitleTemplate != "{title}" {
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
	if cfg.KeyBindings.NewGroup != "g" {
		t.Fatalf("NewGroup = %q", cfg.KeyBindings.NewGroup)
	}
	if cfg.KeyBindings.NewAgent != "n" {
		t.Fatalf("NewAgent = %q", cfg.KeyBindings.NewAgent)
	}
	if cfg.KeyBindings.Quit != "C-c" {
		t.Fatalf("Quit = %q", cfg.KeyBindings.Quit)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "quit = \"C-q\"") || strings.Contains(string(data), "focus_toggle =") {
		t.Fatalf("default config should not include legacy exit bindings:\n%s", data)
	}
}

func TestLoadConfigMapsLegacyBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
columns = ["Backlog", "Active", "Review", "Done"]
title_hook_command = "hooks/title.sh"
title_hook_timeout_seconds = 3

[key_bindings]
previous = "Left"
next = "Right"
new = "a"
new_folder = "x"
close = "x"
focus_toggle = "C-g"
quit = "C-q"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path, "codux-test")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(cfg.Columns, ",") != "inbox,implement,ship" {
		t.Fatalf("columns = %#v", cfg.Columns)
	}
	if cfg.KeyBindings.SelectPrev != "Left" {
		t.Fatalf("select_prev = %q", cfg.KeyBindings.SelectPrev)
	}
	if cfg.KeyBindings.NewAgent != "a" {
		t.Fatalf("new_agent = %q", cfg.KeyBindings.NewAgent)
	}
	if cfg.KeyBindings.NewGroup != "x" {
		t.Fatalf("new_group = %q", cfg.KeyBindings.NewGroup)
	}
	if cfg.KeyBindings.Delete != "x" {
		t.Fatalf("delete = %q", cfg.KeyBindings.Delete)
	}
	if cfg.KeyBindings.Drawer != "C-g" {
		t.Fatalf("drawer = %q", cfg.KeyBindings.Drawer)
	}
	if cfg.KeyBindings.Quit != "C-c" {
		t.Fatalf("quit = %q", cfg.KeyBindings.Quit)
	}
	if cfg.TitleHookCommand != "hooks/title.sh" || cfg.TitleHookTimeoutSeconds != 3 {
		t.Fatalf("title hook = %q/%d", cfg.TitleHookCommand, cfg.TitleHookTimeoutSeconds)
	}
}

func TestMigrateDefaultConfigAddsGlobalCommandCenterKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
codex_command = "codex"

[key_bindings]
close = "c"
sessions = "s"
focus_toggle = "C-g"
new_folder = "f"
quit = "C-q"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if err := MigrateDefaultConfig(path); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{
		`title_template = "{title}"`,
		`title_hook_command = ""`,
		`title_hook_timeout_seconds = 10`,
		`drawer = "C-b"`,
		`select_prev = "k"`,
		`new_workdir = "w"`,
		`new_group = "g"`,
		`new_agent = "n"`,
		`quit = "C-c"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("migrated config missing %s:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "sessions =") {
		t.Fatalf("migrated config should remove dashboard sessions binding:\n%s", data)
	}
	if strings.Contains(text, "new_folder =") {
		t.Fatalf("migrated config should rename new_folder:\n%s", data)
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

func TestDefaultRuntimeIsGlobal(t *testing.T) {
	t.Setenv(AppDirEnv, "")
	dir, err := AppDir("/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(dir, "workdirs") || strings.Contains(dir, "project-") {
		t.Fatalf("AppDir should be global, got %q", dir)
	}
	if got := DefaultTmuxSession("/tmp/project"); got != "codux" {
		t.Fatalf("DefaultTmuxSession = %q", got)
	}
}
