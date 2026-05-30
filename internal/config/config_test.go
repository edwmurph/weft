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
	if cfg.KeyBindings.FocusToggle != "C-g" {
		t.Fatalf("FocusToggle = %q", cfg.KeyBindings.FocusToggle)
	}
	if cfg.KeyBindings.CloseCodux != "C-c" {
		t.Fatalf("CloseCodux = %q", cfg.KeyBindings.CloseCodux)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "quit =") {
		t.Fatalf("default config should not include legacy quit binding:\n%s", data)
	}
	if strings.Contains(string(data), "sessions =") {
		t.Fatalf("default config should not include dashboard sessions binding:\n%s", data)
	}
}

func TestLoadConfigMigratesLegacyColumnsAndBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
columns = ["Backlog", "Active", "Review", "Done"]

[key_bindings]
previous = "h"
next = "l"
close = "x"
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
	if cfg.KeyBindings.Prev != "h" {
		t.Fatalf("prev = %q", cfg.KeyBindings.Prev)
	}
	if cfg.KeyBindings.Close != "x" {
		t.Fatalf("close = %q", cfg.KeyBindings.Close)
	}
	if cfg.KeyBindings.CloseCodux != "C-c" {
		t.Fatalf("close_codux = %q", cfg.KeyBindings.CloseCodux)
	}
}

func TestMigrateDefaultConfigRemovesLegacyExitBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
[key_bindings]
close = "c"
sessions = "s"
focus_toggle = "C-g"
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
	if strings.Contains(string(data), "sessions =") {
		t.Fatalf("migrated config should remove dashboard sessions binding:\n%s", data)
	}
	if strings.Contains(string(data), "quit =") {
		t.Fatalf("migrated config should remove legacy quit binding:\n%s", data)
	}
	if !strings.Contains(string(data), `close_codux = "C-c"`) {
		t.Fatalf("migrated config should add close_codux binding:\n%s", data)
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
