package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/runtimebackup"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/tui"
	weftversion "github.com/edwmurph/weft/internal/version"
)

func requireMacPlutil(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("iTerm2 plist checks require macOS plutil")
	}
	if _, err := os.Stat("/usr/bin/plutil"); err != nil {
		t.Skipf("iTerm2 plist checks require /usr/bin/plutil: %v", err)
	}
}

func TestCLIHelpIncludesLogoAndClearLaunch(t *testing.T) {
	help := cliHelpText()

	if !strings.HasPrefix(help, "\n  ") {
		t.Fatalf("help should leave breathing room above and left of the logo:\n%s", help)
	}
	for _, line := range tui.WeftLogoLines() {
		if !strings.Contains(help, line) {
			t.Fatalf("help missing logo line %q:\n%s", line, help)
		}
	}
	for _, expected := range []string{
		"Terminal dashboard for Codex and shell tasks.",
		weftversion.Label(),
		"weft [--clear] [--attach|--no-attach]",
		"weft <command> [--clear]",
		"weft --clear                 Clear runtime state, then open a fresh dashboard.",
		"weft <command> --clear       Clear runtime state, then run the command.",
		"weft version                 Show CLI, supervisor, and dashboard versions.",
		"weft workspace add <path>    Add a workspace to the dashboard.",
		"weft new [--type id] [title] Create a task.",
		"weft task notes set <text>   Set the current Codex task note.",
		"weft task notes detail set    Set longer notes for Task Tools.",
		"weft close --kill [--yes]    Stop the supervisor and all task PTYs.",
		"weft backup create           Back up config, state, and logs.",
		"weft skill install           Install the bundled Codex skill.",
		"weft doctor attention        Check terminal notification settings.",
		"weft doctor keys             Diagnose terminal key encoding.",
	} {
		if !strings.Contains(help, expected) {
			t.Fatalf("help missing %q:\n%s", expected, help)
		}
	}
	for _, forbidden := range []string{
		"Title templates:",
		"Weft uses one global runtime",
		"unless you use close --kill or clear",
	} {
		if strings.Contains(help, forbidden) {
			t.Fatalf("help should not contain %q:\n%s", forbidden, help)
		}
	}
}

func TestVersionReportIncludesCliSupervisorAndDashboardVersions(t *testing.T) {
	response := ipc.Response{
		OK:                true,
		ProtocolVersion:   ipc.ProtocolVersion,
		SupervisorVersion: "7.8.1",
		Snapshot:          &ipc.Snapshot{ActiveClientID: "client-1", ActiveClientVersion: "7.8.0"},
	}

	report := versionReport(response, nil, nil)

	for _, expected := range []string{
		"cli version: " + weftversion.Version,
		"supervisor version: 7.8.1",
		"main dashboard version: 7.8.0",
		fmt.Sprintf("protocol: cli %d, supervisor %d", ipc.ProtocolVersion, ipc.ProtocolVersion),
		"upgrade: current",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("version report missing %q:\n%s", expected, report)
		}
	}
}

func TestVersionReportHandlesMissingSupervisor(t *testing.T) {
	report := versionReport(ipc.Response{}, errors.New("dial unix weft.sock: connect: no such file or directory"), nil)

	for _, expected := range []string{
		"cli version: " + weftversion.Version,
		"supervisor version: not running",
		"main dashboard version: not attached",
		fmt.Sprintf("protocol: cli %d", ipc.ProtocolVersion),
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("version report missing %q:\n%s", expected, report)
		}
	}
	if strings.Contains(report, "upgrade:") {
		t.Fatalf("offline version report should not include upgrade status:\n%s", report)
	}
}

func TestVersionFlagIsUnsupported(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--clear", "--version"}} {
		err := Run(args)
		if err == nil || !strings.Contains(err.Error(), "use `weft version`") {
			t.Fatalf("Run(%q) error = %v", strings.Join(args, " "), err)
		}
	}
}

func TestSourceBuildRefusesDefaultMainRuntime(t *testing.T) {
	withBuildChannel(t, "source")
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.RootEnv, "")
	t.Setenv(config.AppDirEnv, "")
	t.Setenv(config.WorkspaceEnv, workspace)
	t.Setenv(config.AllowMainRuntimeEnv, "")

	_, _, _, err := resolveRuntime()
	if err == nil {
		t.Fatal("source build should refuse default runtime")
	}
	if !strings.Contains(err.Error(), "source builds refuse to use the default Weft runtime") ||
		!strings.Contains(err.Error(), config.RootEnv+"="+workspace) ||
		!strings.Contains(err.Error(), config.AllowMainRuntimeEnv+"=1") {
		t.Fatalf("runtime guard error missing guidance:\n%s", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".weft")); !os.IsNotExist(statErr) {
		t.Fatalf("guard should not create default runtime, stat err = %v", statErr)
	}
}

func TestSourceBuildAllowsExplicitRuntime(t *testing.T) {
	withBuildChannel(t, "source")
	workspace := t.TempDir()
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv(config.RootEnv, "")
	t.Setenv(config.AppDirEnv, runtimeDir)
	t.Setenv(config.WorkspaceEnv, workspace)
	t.Setenv(config.AllowMainRuntimeEnv, "")

	rt, _, _, err := resolveRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if !rt.HomeExplicit {
		t.Fatalf("runtime should be explicit: %#v", rt)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "config.toml")); err != nil {
		t.Fatalf("explicit runtime should create config: %v", err)
	}
}

func TestSourceBuildAllowsRootRuntime(t *testing.T) {
	withBuildChannel(t, "source")
	root := t.TempDir()
	t.Setenv(config.RootEnv, root)
	t.Setenv(config.AppDirEnv, "")
	t.Setenv(config.WorkspaceEnv, "")
	t.Setenv(config.AllowMainRuntimeEnv, "")

	rt, _, _, err := resolveRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if rt.Workspace != root {
		t.Fatalf("workspace = %q, want root env %q", rt.Workspace, root)
	}
	if rt.Dir != filepath.Join(root, ".weft") {
		t.Fatalf("runtime dir = %q, want root-local .weft", rt.Dir)
	}
	if !rt.HomeExplicit {
		t.Fatalf("root env runtime should be explicit: %#v", rt)
	}
}

func TestSourceBuildAutoRootsFromCheckoutCWD(t *testing.T) {
	withBuildChannel(t, "source")
	home := t.TempDir()
	root := writeAppTestSourceCheckout(t)
	t.Setenv("HOME", home)
	t.Setenv(config.RootEnv, "")
	t.Setenv(config.AppDirEnv, "")
	t.Setenv(config.WorkspaceEnv, "")
	t.Setenv(config.AllowMainRuntimeEnv, "")
	t.Chdir(root)

	rt, _, _, err := resolveRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if rt.Workspace != root {
		t.Fatalf("workspace = %q, want checkout cwd %q", rt.Workspace, root)
	}
	if rt.Dir != filepath.Join(root, ".weft-runtime") {
		t.Fatalf("runtime dir = %q, want checkout-local .weft-runtime", rt.Dir)
	}
	if _, err := os.Stat(filepath.Join(root, ".weft-runtime", "config.toml")); err != nil {
		t.Fatalf("checkout-local runtime should create config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".weft")); !os.IsNotExist(err) {
		t.Fatalf("source auto-root should not touch default home runtime, stat err = %v", err)
	}
}

func TestReleaseBuildAllowsDefaultRuntime(t *testing.T) {
	withBuildChannel(t, "release")
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.RootEnv, "")
	t.Setenv(config.AppDirEnv, "")
	t.Setenv(config.WorkspaceEnv, workspace)
	t.Setenv(config.AllowMainRuntimeEnv, "")

	rt, _, _, err := resolveRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if rt.HomeExplicit {
		t.Fatalf("default runtime should not be marked explicit: %#v", rt)
	}
	if rt.Dir != filepath.Join(home, ".weft") {
		t.Fatalf("runtime dir = %q, want default under HOME", rt.Dir)
	}
}

func TestReleaseBuildIgnoresCheckoutCWDAutoRoot(t *testing.T) {
	withBuildChannel(t, "release")
	home := t.TempDir()
	root := writeAppTestSourceCheckout(t)
	t.Setenv("HOME", home)
	t.Setenv(config.RootEnv, "")
	t.Setenv(config.AppDirEnv, "")
	t.Setenv(config.WorkspaceEnv, "")
	t.Setenv(config.AllowMainRuntimeEnv, "")
	t.Chdir(root)

	rt, _, _, err := resolveRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if rt.Dir != filepath.Join(home, ".weft") {
		t.Fatalf("runtime dir = %q, want default home runtime", rt.Dir)
	}
	if _, err := os.Stat(filepath.Join(root, ".weft-runtime")); !os.IsNotExist(err) {
		t.Fatalf("release build should not touch checkout-local runtime, stat err = %v", err)
	}
}

func TestAllowMainRuntimeOverrideAllowsSourceDefaultRuntime(t *testing.T) {
	withBuildChannel(t, "source")
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.RootEnv, "")
	t.Setenv(config.AppDirEnv, "")
	t.Setenv(config.WorkspaceEnv, workspace)
	t.Setenv(config.AllowMainRuntimeEnv, "1")

	if _, _, _, err := resolveRuntime(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".weft", "config.toml")); err != nil {
		t.Fatalf("override should create default runtime config: %v", err)
	}
}

func TestBackupRestoreCreatesPreRestoreBackup(t *testing.T) {
	withBuildChannel(t, "source")
	workspace := t.TempDir()
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv(config.AppDirEnv, runtimeDir)
	t.Setenv(config.WorkspaceEnv, workspace)

	rt, _, _, err := resolveRuntime()
	if err != nil {
		t.Fatal(err)
	}
	originalConfig := []byte("[task_types.codex]\ncommand = \"codex\"\ntitle_template = \"{title}\"\n")
	originalState := []byte("{\"version\":5,\"focus\":\"workspaces\",\"nav_open\":true,\"workspaces\":[],\"groups\":[],\"tasks\":[],\"collapsed_group_ids\":[]}\n")
	if err := os.WriteFile(rt.ConfigPath, originalConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rt.StatePath, originalState, 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := runtimebackup.Create(rt, runtimebackup.Options{Reason: "known good"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rt.ConfigPath, []byte("[task_types.codex]\ncommand = \"broken\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rt.StatePath, []byte("broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := backupRestore([]string{backup.ID, "--yes"}); err != nil {
		t.Fatal(err)
	}

	gotConfig, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotConfig) != string(originalConfig) {
		t.Fatalf("config not restored:\n%s", gotConfig)
	}
	gotState, err := os.ReadFile(rt.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotState) != string(originalState) {
		t.Fatalf("state not restored:\n%s", gotState)
	}
	backups, err := runtimebackup.List(rt)
	if err != nil {
		t.Fatal(err)
	}
	foundPreRestore := false
	for _, item := range backups {
		if item.Reason == "pre-restore "+backup.ID {
			foundPreRestore = true
			break
		}
	}
	if !foundPreRestore {
		t.Fatalf("pre-restore backup not found: %#v", backups)
	}
}

func TestExtractClearFlag(t *testing.T) {
	clear, args := extractClearFlag([]string{"doctor", "keys", "--clear"})
	if !clear {
		t.Fatal("expected --clear to be detected")
	}
	if got := strings.Join(args, " "); got != "doctor keys" {
		t.Fatalf("args = %q, want doctor keys", got)
	}

	clear, args = extractClearFlag([]string{"--clear", "--no-attach"})
	if !clear {
		t.Fatal("expected leading --clear to be detected")
	}
	if got := strings.Join(args, " "); got != "--no-attach" {
		t.Fatalf("args = %q, want --no-attach", got)
	}
}

func TestTaskContextSetUsesArgsAndEnvTarget(t *testing.T) {
	t.Setenv("WEFT_TASK_ID", "env-task")
	previous := taskContextCallIPC
	t.Cleanup(func() { taskContextCallIPC = previous })
	var gotCommand string
	var gotArgs map[string]string
	taskContextCallIPC = func(command string, args map[string]string, quiet bool) (ipc.Response, error) {
		gotCommand = command
		gotArgs = args
		if !quiet {
			t.Fatal("task notes commands should use quiet IPC")
		}
		return ipc.Response{OK: true, Message: "Set task notes heading for task env-task."}, nil
	}

	var out bytes.Buffer
	if err := taskCommand([]string{"notes", "set", "review", "PR", "123"}, os.Stdin, &out); err != nil {
		t.Fatal(err)
	}

	if gotCommand != "task_context_set" || gotArgs["task_id"] != "env-task" || gotArgs["kind"] != "heading" || gotArgs["content"] != "review PR 123" {
		t.Fatalf("ipc call = %s %#v", gotCommand, gotArgs)
	}
	if !strings.Contains(out.String(), "Set task notes") {
		t.Fatalf("set output = %q", out.String())
	}
}

func TestTaskContextSetReadsPipedStdinAndRejectsTerminalStdin(t *testing.T) {
	previous := taskContextCallIPC
	t.Cleanup(func() { taskContextCallIPC = previous })
	var gotContent string
	var gotKind string
	taskContextCallIPC = func(command string, args map[string]string, quiet bool) (ipc.Response, error) {
		gotContent = args["content"]
		gotKind = args["kind"]
		return ipc.Response{OK: true, Message: "Set task notes detail for task a."}, nil
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString("from stdin\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	var out bytes.Buffer
	if err := taskCommand([]string{"notes", "detail", "set"}, reader, &out); err != nil {
		t.Fatal(err)
	}
	if gotContent != "from stdin\n" {
		t.Fatalf("piped content = %q", gotContent)
	}
	if gotKind != "detail" {
		t.Fatalf("piped kind = %q", gotKind)
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	if err := taskCommand([]string{"notes", "detail", "set"}, devNull, &out); err == nil || !strings.Contains(err.Error(), "requires text or piped stdin") {
		t.Fatalf("terminal stdin error = %v", err)
	}
}

func TestTaskContextShowJSONPrintsTaskContextField(t *testing.T) {
	previous := taskContextCallIPC
	t.Cleanup(func() { taskContextCallIPC = previous })
	taskContextCallIPC = func(command string, args map[string]string, quiet bool) (ipc.Response, error) {
		if command != "task_context_show" || args["task_id"] != "task-a" || args["kind"] != "detail" {
			t.Fatalf("ipc call = %s %#v", command, args)
		}
		return ipc.Response{OK: true, TaskContext: &ipc.TaskContext{TaskID: "task-a", Heading: "full", Detail: "full\ntext", Summary: "full", UpdatedAt: "2026-06-03T12:00:00Z"}}, nil
	}

	var out bytes.Buffer
	if err := taskCommand([]string{"notes", "detail", "show", "--task", "task-a", "--json"}, os.Stdin, &out); err != nil {
		t.Fatal(err)
	}
	var got ipc.TaskContext
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json output = %q err=%v", out.String(), err)
	}
	if got.TaskID != "task-a" || got.Heading != "full" || got.Detail != "full\ntext" || got.Summary != "full" {
		t.Fatalf("json task notes = %#v", got)
	}
}

func TestSkillInstallUsesCodexHomeAndForce(t *testing.T) {
	codexHome := filepath.Join(t.TempDir(), "codex")
	t.Setenv("CODEX_HOME", codexHome)

	var out bytes.Buffer
	if err := skillCommand([]string{"install"}, &out); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(codexHome, "skills", "weft", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "weft task notes set") ||
		!strings.Contains(string(data), "weft status --json") ||
		!strings.Contains(string(data), "resume_id") ||
		!strings.Contains(out.String(), filepath.Join(codexHome, "skills", "weft")) {
		t.Fatalf("installed skill/output missing expected content:\n%s\n%s", data, out.String())
	}

	if err := skillCommand([]string{"install"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second install error = %v", err)
	}
	if err := skillCommand([]string{"install", "--force"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("force install failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("skill install should not create Weft config, stat err=%v", err)
	}
}

func TestDoctorKeySequenceDescriptions(t *testing.T) {
	tests := []struct {
		name string
		seq  []byte
		want string
	}{
		{name: "backspace", seq: []byte{0x7f}, want: "backspace"},
		{name: "ctrl h", seq: []byte{0x08}, want: "ctrl+h"},
		{name: "alt backspace", seq: []byte("\x1b\x7f"), want: "alt+backspace"},
		{name: "alt ctrl h", seq: []byte("\x1b\b"), want: "alt+ctrl+h"},
		{name: "delete", seq: []byte("\x1b[3~"), want: "delete"},
		{name: "alt b", seq: []byte("\x1bb"), want: "alt+b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeDoctorKeySequence(tt.seq); got != tt.want {
				t.Fatalf("describeDoctorKeySequence(%q) = %q, want %q", tt.seq, got, tt.want)
			}
		})
	}
}

func TestDoctorKeyReportExplainsIndistinguishableOptionBackspace(t *testing.T) {
	report := keyDoctorReport([]keyDoctorSample{
		{Name: "Backspace", Bytes: []byte{0x7f}, Label: "backspace"},
		{Name: "Option+Backspace", Bytes: []byte{0x7f}, Label: "backspace"},
		{Name: "Ctrl+Backspace", Bytes: []byte{0x08}, Label: "ctrl+h"},
	})

	for _, expected := range []string{
		"Backspace:         backspace",
		"Option+Backspace:  backspace",
		"Ctrl+Backspace:    ctrl+h",
		"Issue: Option+Backspace is indistinguishable from Backspace.",
		"For custom mappings, send bytes: 1b 7f.",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("report missing %q:\n%s", expected, report)
		}
	}
}

func TestDoctorKeyReportAcceptsAltBackspace(t *testing.T) {
	report := keyDoctorReport([]keyDoctorSample{
		{Name: "Backspace", Bytes: []byte{0x7f}, Label: "backspace"},
		{Name: "Option+Backspace", Bytes: []byte("\x1b\x7f"), Label: "alt+backspace"},
		{Name: "Ctrl+Backspace", Bytes: []byte{0x08}, Label: "ctrl+h"},
	})

	if !strings.Contains(report, "OK: Option+Backspace is distinguishable.") {
		t.Fatalf("report should accept alt backspace:\n%s", report)
	}
}

func TestDetectDoctorTerminal(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		kind string
	}{
		{name: "iterm term program", env: []string{"TERM_PROGRAM=iTerm.app"}, kind: "iterm2"},
		{name: "iterm session", env: []string{"ITERM_SESSION_ID=w0t0p0"}, kind: "iterm2"},
		{name: "apple terminal", env: []string{"TERM_PROGRAM=Apple_Terminal"}, kind: "apple_terminal"},
		{name: "wezterm", env: []string{"TERM_PROGRAM=WezTerm"}, kind: "wezterm"},
		{name: "ghostty", env: []string{"GHOSTTY_RESOURCES_DIR=/Applications/Ghostty.app"}, kind: "ghostty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectDoctorTerminal(tt.env).Kind; got != tt.kind {
				t.Fatalf("terminal kind = %q, want %q", got, tt.kind)
			}
		})
	}
}

func TestIterm2FixErrorIncludesDebugContext(t *testing.T) {
	err := iterm2FixError(
		"write updated preferences",
		"/Users/me/Library/Preferences/com.googlecode.iterm2.plist",
		"Default",
		commandOutputError("write plist", []string{"/usr/bin/plutil", "-convert", "xml1", "-o", "/tmp/out.plist", "/tmp/in.json"}, errors.New("exit status 1"), []byte("not a plist\n")),
	)
	message := err.Error()
	for _, expected := range []string{
		"could not update iTerm2 Option+Backspace mapping",
		"step: write updated preferences",
		"preferences: /Users/me/Library/Preferences/com.googlecode.iterm2.plist",
		"profile: Default",
		"command: \"/usr/bin/plutil\" \"-convert\" \"xml1\" \"-o\" \"/tmp/out.plist\" \"/tmp/in.json\"",
		"output: not a plist",
		"Manual fix:",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("error missing %q:\n%s", expected, message)
		}
	}
}

func withBuildChannel(t *testing.T, channel string) {
	t.Helper()
	previous := weftversion.BuildChannel
	weftversion.BuildChannel = channel
	t.Cleanup(func() {
		weftversion.BuildChannel = previous
	})
}

func writeAppTestSourceCheckout(t *testing.T) string {
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

func TestIterm2AttentionPlutilHelpersEnableNotifications(t *testing.T) {
	requireMacPlutil(t)

	path := filepath.Join(t.TempDir(), "com.googlecode.iterm2.plist")
	if err := os.WriteFile(path, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Default Bookmark Guid</key>
  <string>default-guid</string>
  <key>New Bookmarks</key>
  <array>
    <dict>
      <key>Guid</key>
      <string>default-guid</string>
      <key>Name</key>
      <string>Default</string>
      <key>BM Growl</key>
      <false/>
    </dict>
  </array>
</dict>
</plist>
`), 0o600); err != nil {
		t.Fatal(err)
	}

	target, err := selectIterm2ProfileTarget(path, "")
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := iterm2NotificationAlertsEnabled(path, target.Index)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("notifications should start disabled")
	}

	if err := updateIterm2NotificationAlertsFile(path, target.Index); err != nil {
		t.Fatal(err)
	}
	enabled, err = iterm2NotificationAlertsEnabled(path, target.Index)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("notifications should be enabled")
	}
}

func TestDoctorAttentionReportsEnabledItermNotifications(t *testing.T) {
	requireMacPlutil(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	prefsDir := filepath.Join(home, "Library", "Preferences")
	if err := os.MkdirAll(prefsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(prefsDir, "com.googlecode.iterm2.plist")
	if err := os.WriteFile(path, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Default Bookmark Guid</key>
  <string>default-guid</string>
  <key>New Bookmarks</key>
  <array>
    <dict>
      <key>Guid</key>
      <string>default-guid</string>
      <key>Name</key>
      <string>Default</string>
      <key>BM Growl</key>
      <true/>
    </dict>
  </array>
</dict>
</plist>
`), 0o600); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := doctorAttention(strings.NewReader(""), &out, []string{"TERM_PROGRAM=iTerm.app", "ITERM_PROFILE=Default"}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{
		"Weft attention doctor",
		"Preferences: " + path,
		"Profile: Default",
		"Notification Center alerts: enabled",
		"OK: iTerm2 Notification Center alerts are enabled",
		"\x1b]9;Weft test notification\a",
		"Sent iTerm2 test notification.",
		"Filter Alerts",
		"macOS System Settings",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q:\n%s", expected, text)
		}
	}
}

func TestDoctorAttentionCanApplyItermNotificationFix(t *testing.T) {
	requireMacPlutil(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	prefsDir := filepath.Join(home, "Library", "Preferences")
	if err := os.MkdirAll(prefsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(prefsDir, "com.googlecode.iterm2.plist")
	if err := os.WriteFile(path, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Default Bookmark Guid</key>
  <string>default-guid</string>
  <key>New Bookmarks</key>
  <array>
    <dict>
      <key>Guid</key>
      <string>default-guid</string>
      <key>Name</key>
      <string>Default</string>
      <key>BM Growl</key>
      <false/>
    </dict>
  </array>
</dict>
</plist>
`), 0o600); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := doctorAttention(strings.NewReader("y\n"), &out, []string{"TERM_PROGRAM=iTerm.app", "ITERM_PROFILE=Default"}); err != nil {
		t.Fatal(err)
	}
	enabled, err := iterm2NotificationAlertsEnabled(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("doctor attention should enable notifications")
	}
	text := out.String()
	for _, expected := range []string{
		"Issue: iTerm2 Notification Center alerts are disabled",
		"Apply this iTerm2 notification fix now?",
		"Updated iTerm2 profile \"Default\".",
		"Backup: ",
		"Open a new iTerm2 tab or window",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q:\n%s", expected, text)
		}
	}
}

func TestIterm2PlutilHelpersAddKeyMapping(t *testing.T) {
	requireMacPlutil(t)

	path := filepath.Join(t.TempDir(), "com.googlecode.iterm2.plist")
	if err := os.WriteFile(path, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Binary Payload</key>
  <data>AQID</data>
  <key>Default Bookmark Guid</key>
  <string>default-guid</string>
  <key>New Bookmarks</key>
  <array>
    <dict>
      <key>Guid</key>
      <string>default-guid</string>
      <key>Name</key>
      <string>Default</string>
    </dict>
  </array>
</dict>
</plist>
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/plutil", "-convert", "json", "-o", "-", path).CombinedOutput(); err == nil {
		t.Fatalf("test plist should not be JSON-convertible, got output:\n%s", out)
	}

	target, err := selectIterm2ProfileTarget(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Index != 0 || target.Name != "Default" {
		t.Fatalf("target = %+v, want Default at index 0", target)
	}

	configured, err := iterm2OptionBackspaceMappingConfigured(path, target.Index)
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("mapping should not be configured yet")
	}

	if err := updateIterm2OptionBackspaceMappingFile(path, target.Index); err != nil {
		t.Fatal(err)
	}
	configured, err = iterm2OptionBackspaceMappingConfigured(path, target.Index)
	if err != nil {
		t.Fatal(err)
	}
	if !configured {
		t.Fatal("mapping should be configured")
	}

	text, found, err := plistExtractRawOptional(path, "New Bookmarks.0.Keyboard Map.0x7f-0x80000-0x33.Text")
	if err != nil {
		t.Fatal(err)
	}
	if !found || text != iTerm2OptionBackspaceText {
		t.Fatalf("mapping text = %q found=%v, want %q", text, found, iTerm2OptionBackspaceText)
	}
	for keyPath, want := range map[string]string{
		"New Bookmarks.0.Option Key Sends":       iTerm2OptionEscValue,
		"New Bookmarks.0.Right Option Key Sends": iTerm2OptionEscValue,
	} {
		got, found, err := plistExtractRawOptional(path, keyPath)
		if err != nil {
			t.Fatal(err)
		}
		if !found || got != want {
			t.Fatalf("%s = %q found=%v, want %q", keyPath, got, found, want)
		}
	}
}

func TestIterm2FixRequiresOptionKeysEsc(t *testing.T) {
	requireMacPlutil(t)

	path := filepath.Join(t.TempDir(), "com.googlecode.iterm2.plist")
	if err := os.WriteFile(path, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Default Bookmark Guid</key>
  <string>default-guid</string>
  <key>New Bookmarks</key>
  <array>
    <dict>
      <key>Guid</key>
      <string>default-guid</string>
      <key>Name</key>
      <string>Default</string>
      <key>Option Key Sends</key>
      <integer>0</integer>
      <key>Right Option Key Sends</key>
      <integer>0</integer>
      <key>Keyboard Map</key>
      <dict>
        <key>0x7f-0x80000-0x33</key>
        <dict>
          <key>Action</key>
          <integer>11</integer>
          <key>Text</key>
          <string>0x1b 0x7f</string>
        </dict>
      </dict>
    </dict>
  </array>
</dict>
</plist>
`), 0o600); err != nil {
		t.Fatal(err)
	}

	configured, err := iterm2OptionBackspaceFixConfigured(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("mapping alone should not count as configured when Option keys are Normal")
	}
}

func TestOfferDoctorKeyFixExplainsConfiguredButStaleItermSession(t *testing.T) {
	requireMacPlutil(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	prefsDir := filepath.Join(home, "Library", "Preferences")
	if err := os.MkdirAll(prefsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(prefsDir, "com.googlecode.iterm2.plist")
	if err := os.WriteFile(path, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Default Bookmark Guid</key>
  <string>default-guid</string>
  <key>New Bookmarks</key>
  <array>
    <dict>
      <key>Guid</key>
      <string>default-guid</string>
      <key>Name</key>
      <string>Default</string>
      <key>Option Key Sends</key>
      <integer>2</integer>
      <key>Right Option Key Sends</key>
      <integer>2</integer>
      <key>Keyboard Map</key>
      <dict>
        <key>0x7f-0x80000-0x33</key>
        <dict>
          <key>Action</key>
          <integer>11</integer>
          <key>Text</key>
          <string>0x1b 0x7f</string>
        </dict>
      </dict>
    </dict>
  </array>
</dict>
</plist>
`), 0o600); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	err := offerDoctorKeyFix(nil, &out, []keyDoctorSample{
		{Name: "Backspace", Bytes: []byte{0x7f}, Label: "backspace"},
		{Name: "Option+Backspace", Bytes: []byte{0x7f}, Label: "backspace"},
	}, keyDoctorTerminal{Kind: "iterm2", Name: "iTerm2"}, []string{"ITERM_PROFILE=Default"})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{
		"Preferences: " + path,
		"Profile: Default",
		"Left Option Key: Esc+",
		"Right Option Key: Esc+",
		"already sets Left/Right Option Key to Esc+ and maps Option+Backspace to Esc DEL, but this terminal session is still sending plain Backspace",
		"Open a new iTerm2 tab or window with that profile",
		"restart iTerm2 so it reloads " + path,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "Apply this iTerm2 key fix now?") {
		t.Fatalf("should not prompt when preferences are already configured:\n%s", text)
	}
}

func TestOfferDoctorKeyFixExplainsCustomPrefsNeedRestart(t *testing.T) {
	requireMacPlutil(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	defaultPrefsDir := filepath.Join(home, "Library", "Preferences")
	customPrefsDir := filepath.Join(home, "configs", "iterm")
	if err := os.MkdirAll(defaultPrefsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(customPrefsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	defaultPath := filepath.Join(defaultPrefsDir, "com.googlecode.iterm2.plist")
	customPath := filepath.Join(customPrefsDir, "com.googlecode.iterm2.plist")
	if err := os.WriteFile(defaultPath, []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>LoadPrefsFromCustomFolder</key>
  <true/>
  <key>PrefsCustomFolder</key>
  <string>%s</string>
</dict>
</plist>
`, customPrefsDir)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(customPath, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Default Bookmark Guid</key>
  <string>default-guid</string>
  <key>New Bookmarks</key>
  <array>
    <dict>
      <key>Guid</key>
      <string>default-guid</string>
      <key>Name</key>
      <string>Default</string>
      <key>Option Key Sends</key>
      <integer>2</integer>
      <key>Right Option Key Sends</key>
      <integer>2</integer>
      <key>Keyboard Map</key>
      <dict>
        <key>0x7f-0x80000-0x33</key>
        <dict>
          <key>Action</key>
          <integer>11</integer>
          <key>Text</key>
          <string>0x1b 0x7f</string>
        </dict>
      </dict>
    </dict>
  </array>
</dict>
</plist>
`), 0o600); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	err := offerDoctorKeyFix(nil, &out, []keyDoctorSample{
		{Name: "Backspace", Bytes: []byte{0x7f}, Label: "backspace"},
		{Name: "Option+Backspace", Bytes: []byte{0x7f}, Label: "backspace"},
	}, keyDoctorTerminal{Kind: "iterm2", Name: "iTerm2"}, []string{"ITERM_PROFILE=Default"})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{
		"Preferences: " + customPath,
		"iTerm2 is loading settings from a custom folder",
		"Quit iTerm2 completely, reopen it, then rerun `weft doctor keys --clear`.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "Open a new iTerm2 tab or window") {
		t.Fatalf("custom prefs should recommend restart, not only a new tab:\n%s", text)
	}
}

func TestValidateWorkspaceAddPathRequiresExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	got, err := validateWorkspaceAddPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := state.NormalizeWorkspacePath(dir); got != want {
		t.Fatalf("validated path = %q, want %q", got, want)
	}

	filePath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(filePath, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateWorkspaceAddPath(filePath); err == nil || !strings.Contains(err.Error(), "workspace path is not a directory") {
		t.Fatalf("file path error = %v", err)
	}
	if _, err := validateWorkspaceAddPath(filepath.Join(dir, "missing")); err == nil || !strings.Contains(err.Error(), "workspace path does not exist") {
		t.Fatalf("missing path error = %v", err)
	}
}
