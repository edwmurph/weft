package runtimebackup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edwmurph/weft/internal/config"
)

func TestCreateListRestoreHandlesMissingState(t *testing.T) {
	rt := testRuntime(t)
	if err := os.MkdirAll(rt.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	currentConfig := "[task_types.codex]\ncommand = \"codex\"\n"
	if err := os.WriteFile(rt.ConfigPath, []byte(currentConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := Create(rt, Options{
		Reason: "manual test",
		Now: func() time.Time {
			return time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if backup.ID != "20260531T120000Z-manual-test" {
		t.Fatalf("backup id = %q", backup.ID)
	}
	if !contains(backup.Missing, "state.json") {
		t.Fatalf("missing files = %#v, want state.json", backup.Missing)
	}
	if len(backup.Files) != 1 || backup.Files[0].Name != "config.toml" {
		t.Fatalf("files = %#v", backup.Files)
	}

	backups, err := List(rt)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].ID != backup.ID {
		t.Fatalf("backups = %#v", backups)
	}

	if err := os.WriteFile(rt.StatePath, []byte("current state\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pre, err := Create(rt, Options{Reason: "pre-restore " + backup.ID, IncludeLogs: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RestoreWithPreRestore(rt, backup, &pre)
	if err != nil {
		t.Fatal(err)
	}
	if result.PreRestore == nil || result.PreRestore.ID == "" {
		t.Fatalf("pre-restore backup missing: %#v", result)
	}
	if _, err := os.Stat(rt.StatePath); !os.IsNotExist(err) {
		t.Fatalf("state should be removed when missing from backup, err = %v", err)
	}
	data, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != currentConfig {
		t.Fatalf("config not restored:\n%s", data)
	}
}

func TestResolveRejectsMalformedBackupPaths(t *testing.T) {
	rt := testRuntime(t)
	filePath := filepath.Join(t.TempDir(), "not-a-backup")
	if err := os.WriteFile(filePath, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(rt, filePath); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Resolve file error = %v", err)
	}

	dir := filepath.Join(t.TempDir(), "missing-metadata")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(rt, dir); err == nil {
		t.Fatal("Resolve should reject backup without metadata")
	}

	if _, err := Resolve(rt, "missing-id"); err == nil || !strings.Contains(err.Error(), "backup not found") {
		t.Fatalf("Resolve missing id error = %v", err)
	}
}

func testRuntime(t *testing.T) config.Runtime {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	return config.Runtime{
		Workspace:  workspace,
		Dir:        runtimeDir,
		ConfigPath: filepath.Join(runtimeDir, "config.toml"),
		StatePath:  filepath.Join(runtimeDir, "state.json"),
		SocketPath: filepath.Join(runtimeDir, "weft.sock"),
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
