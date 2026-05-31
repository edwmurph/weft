package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/ipc"
	"github.com/edwmurph/weft/internal/pathx"
	"github.com/edwmurph/weft/internal/runtimebackup"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/supervisor"
	"github.com/edwmurph/weft/internal/tui"
	"github.com/edwmurph/weft/internal/version"
)

func Run(args []string) error {
	clearBeforeCommand, args := extractClearFlag(args)
	clearApplies := true
	var action func() error

	if len(args) == 0 {
		action = func() error { return start([]string{}) }
	} else {
		switch args[0] {
		case "-h", "--help", "help":
			clearApplies = false
			action = func() error {
				fmt.Print(cliHelpText())
				return nil
			}
		case "--version", "version":
			clearApplies = false
			action = func() error {
				fmt.Println(version.Version)
				return nil
			}
		case supervisor.CommandName:
			clearApplies = false
			action = runSupervisor
		case "refresh":
			action = func() error { return callIPC("refresh", nil, false) }
		case "status":
			action = func() error { return status(args[1:]) }
		case "new":
			action = func() error {
				title := ""
				if len(args) > 1 {
					title = strings.Join(args[1:], " ")
				}
				return callIPC("new", map[string]string{"title": title}, false)
			}
		case "group":
			action = func() error { return groupCommand(args[1:]) }
		case "workspace":
			action = func() error { return workspaceCommand(args[1:]) }
		case "rename":
			action = func() error { return rename(args[1:]) }
		case "close":
			action = func() error { return closeCommand(args[1:]) }
		case "select":
			action = func() error {
				if len(args) < 2 {
					return errors.New("select requires an agent id")
				}
				return callIPC("select", map[string]string{"id": args[1]}, false)
			}
		case "move-left":
			action = func() error { return callIPC("move", map[string]string{"direction": "left"}, false) }
		case "move-right":
			action = func() error { return callIPC("move", map[string]string{"direction": "right"}, false) }
		case "clear":
			clearApplies = false
			action = clear
		case "backup":
			clearApplies = false
			action = func() error { return backupCommand(args[1:]) }
		case "doctor":
			action = func() error { return doctor(args[1:]) }
		case "config":
			action = func() error { return configCommand(args[1:]) }
		default:
			if strings.HasPrefix(args[0], "--") {
				action = func() error { return start(args) }
			} else {
				return fmt.Errorf("unknown command %q\n\n%s", args[0], cliHelpText())
			}
		}
	}

	if clearBeforeCommand && clearApplies {
		if err := clearBeforeRunningCommand(); err != nil {
			return err
		}
	}
	return action()
}

func extractClearFlag(args []string) (bool, []string) {
	clear := false
	clean := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--clear" {
			clear = true
			continue
		}
		clean = append(clean, arg)
	}
	return clear, clean
}

func clearBeforeRunningCommand() error {
	rt, _, _, err := resolveRuntime()
	if err != nil {
		return err
	}
	return clearRuntime(rt, false)
}

func cliHelpText() string {
	lines := []string{""}
	for _, line := range tui.WeftLogoWithVersionLines() {
		if line == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, "  "+line)
	}
	lines = append(lines,
		"",
		"Terminal dashboard for Codex agent threads.",
		"",
		"Usage:",
		"  weft [--clear] [--attach|--no-attach]",
		"  weft <command> [--clear]",
		"",
		"Common commands:",
		"  weft                         Open the dashboard and attach to the supervisor.",
		"  weft --clear                 Clear runtime state, then open a fresh dashboard.",
		"  weft <command> --clear       Clear runtime state, then run the command.",
		"  weft --no-attach             Start or reuse the supervisor without opening the dashboard.",
		"  weft refresh                 Request a fresh dashboard snapshot.",
		"  weft status [--json]         Show supervisor, workspace, group, and agent state.",
		"  weft doctor                  Check local runtime and Codex command health.",
		"  weft doctor keys             Diagnose terminal key encoding.",
		"",
		"Agents and organization:",
		"  weft new [title]             Create a Codex agent.",
		"  weft select <id>             Make an agent active.",
		"  weft rename [id] <title>     Rename the selected agent or the given agent.",
		"  weft close [id]              Close the active client or a Codex agent.",
		"  weft group add <name>        Add a group in the current workspace.",
		"  weft workspace add <path>    Add a workspace to the dashboard.",
		"  weft move-left               Move the selected agent out of its group.",
		"  weft move-right              Move the selected agent into the selected group.",
		"",
		"Runtime and configuration:",
		"  weft close --kill [--yes]    Stop the supervisor and all Codex PTYs.",
		"  weft clear                   Prompt, then delete Weft runtime state.",
		"  weft backup create           Back up config, state, and logs.",
		"  weft backup list             List saved runtime backups.",
		"  weft backup restore <id>     Restore config and state from a backup.",
		"  weft config info             Show runtime paths and active config.",
		"  weft config show             Print config.toml.",
		"  weft config init [--force]   Write the default config.",
		"",
	)
	return strings.Join(lines, "\n")
}

func start(args []string) error {
	fs := flag.NewFlagSet("weft", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	attach := fs.Bool("attach", true, "attach to the Weft dashboard")
	noAttach := fs.Bool("no-attach", false, "start the Weft supervisor without attaching")
	clearBeforeStart := fs.Bool("clear", false, "delete runtime state before starting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *noAttach {
		*attach = false
	}
	rt, cfg, store, err := resolveRuntime()
	if err != nil {
		return err
	}
	if *clearBeforeStart {
		if err := clearRuntime(rt, false); err != nil {
			return err
		}
	}
	if _, err := store.Ensure(); err != nil {
		return err
	}
	result, err := supervisor.Ensure(rt)
	if err != nil {
		return err
	}
	if !*attach {
		action := "Using existing"
		if result.Started {
			action = "Started"
		}
		fmt.Printf("%s Weft supervisor.\n", action)
		fmt.Printf("Runtime: %s\n", rt.Dir)
		fmt.Printf("Socket: %s\n", rt.SocketPath)
		printUpgrade(result.Status.Upgrade)
		return nil
	}
	return tui.RunClient(rt, cfg)
}

func runSupervisor() error {
	rt, cfg, store, err := resolveRuntime()
	if err != nil {
		return err
	}
	return supervisor.Run(context.Background(), rt, cfg, store)
}

func closeCommand(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return closeWeft("close", args)
	}
	if len(args) > 1 {
		return errors.New("close accepts at most one agent id")
	}
	return callIPC("close", map[string]string{"id": args[0]}, false)
}

func closeWeft(command string, args []string) error {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	kill := fs.Bool("kill", false, "stop the Weft supervisor and all agent PTYs")
	yes := fs.Bool("yes", false, "confirm supervisor shutdown without prompting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%s accepts only --kill without an agent id", command)
	}
	rt, _, _, err := resolveRuntime()
	if err != nil {
		return err
	}
	if *kill {
		response, err := supervisor.Status(rt)
		if err == nil && runningAgentCount(response.State) > 0 && !*yes {
			fmt.Printf("Stopping the Weft supervisor will stop %d running Codex terminal(s). Saved layout and metadata remain.\n", runningAgentCount(response.State))
			if !confirm("Stop supervisor and running Codex terminals? [y/N] ") {
				fmt.Println("Close canceled.")
				return nil
			}
		}
		backup, err := runtimebackup.Create(rt, runtimebackup.Options{Reason: "pre-close kill", IncludeLogs: true})
		if err != nil {
			return fmt.Errorf("could not create pre-shutdown backup: %w", err)
		}
		fmt.Printf("Created backup: %s\n", backup.ID)
		if err := supervisor.Shutdown(rt); err != nil {
			fmt.Printf("Weft supervisor is not running: %v\n", err)
			return nil
		}
		fmt.Println("Weft supervisor stopped.")
		return nil
	}
	return callIPC("close_client", nil, false)
}

func status(args []string) error {
	jsonOutput := false
	if len(args) > 0 && args[0] == "--json" {
		jsonOutput = true
	}
	rt, _, store, err := resolveRuntime()
	if err != nil {
		return err
	}
	response, err := supervisor.Status(rt)
	if err == nil {
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(response.State)
		}
		fmt.Println(response.Message)
		printSupervisorStatus(response)
		printUpgrade(response.Upgrade)
		return nil
	}
	if supervisorResponded(response) {
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(response)
		}
		if response.Message != "" {
			fmt.Println(response.Message)
		}
		printSupervisorStatus(response)
		printUpgrade(response.Upgrade)
		return nil
	}
	st, err := store.Read()
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(st)
	}
	fmt.Printf("supervisor: down (%v)\n", err)
	fmt.Printf("launch workspace: %s\nruntime dir: %s\nfocus: %s\nworkspaces: %d\ngroups: %d\nagents: %d\n", rt.Workspace, rt.Dir, displayFocus(st.Focus), len(st.Workspaces), len(st.Groups), len(st.Agents))
	return nil
}

func rename(args []string) error {
	if len(args) == 0 {
		return errors.New("rename requires a title")
	}
	id := ""
	title := strings.Join(args, " ")
	if len(args) >= 2 && looksLikeID(args[0]) {
		id = args[0]
		title = strings.Join(args[1:], " ")
	}
	return callIPC("rename", map[string]string{"id": id, "title": title}, false)
}

func groupCommand(args []string) error {
	if len(args) < 2 || args[0] != "add" {
		return errors.New("group requires: add <name>")
	}
	return callIPC("add_group", map[string]string{"path": strings.Join(args[1:], " ")}, false)
}

func workspaceCommand(args []string) error {
	if len(args) < 2 || args[0] != "add" {
		return errors.New("workspace requires: add <path>")
	}
	path, err := validateWorkspaceAddPath(strings.Join(args[1:], " "))
	if err != nil {
		return err
	}
	return callIPC("add_workspace", map[string]string{"path": path}, false)
}

func validateWorkspaceAddPath(path string) (string, error) {
	path = state.NormalizeWorkspacePath(path)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("workspace path does not exist: %s", path)
		}
		return "", fmt.Errorf("cannot read workspace path %s: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace path is not a directory: %s", path)
	}
	return path, nil
}

func clear() error {
	rt, _, _, err := resolveRuntime()
	if err != nil {
		return err
	}
	return clearRuntime(rt, true)
}

func clearRuntime(rt config.Runtime, confirmDestructive bool) error {
	runtimeFiles := existingRuntimeFiles(rt)
	if len(runtimeFiles) == 0 {
		fmt.Println("No Weft runtime state found.")
		return nil
	}
	if confirmDestructive {
		fmt.Println("This will stop the Weft supervisor and delete Weft runtime state:")
		for _, path := range runtimeFiles {
			fmt.Printf("- runtime file: %s\n", pathx.Display(path))
		}
		if !confirm("Delete Weft runtime state? [y/N] ") {
			fmt.Println("Delete canceled.")
			return nil
		}
	}
	backup, err := runtimebackup.Create(rt, runtimebackup.Options{Reason: "pre-clear", IncludeLogs: true})
	if err != nil {
		return fmt.Errorf("could not create pre-clear backup: %w", err)
	}
	fmt.Printf("Created backup: %s\n", backup.ID)
	_ = supervisor.Shutdown(rt)
	waitForSupervisorStop(rt, 2*time.Second)
	deletedFiles := 0
	for _, path := range runtimeFiles {
		if os.Remove(path) == nil {
			deletedFiles++
		}
	}
	fmt.Printf("Deleted %d runtime file(s).\n", deletedFiles)
	return nil
}

func backupCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("backup requires one of: create, list, restore")
	}
	switch args[0] {
	case "create":
		return backupCreate(args[1:])
	case "list":
		if len(args) > 1 {
			return errors.New("backup list accepts no arguments")
		}
		return backupList()
	case "restore":
		return backupRestore(args[1:])
	default:
		return fmt.Errorf("unknown backup command %q", args[0])
	}
}

func backupCreate(args []string) error {
	fs := flag.NewFlagSet("backup create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("output", "", "directory for the backup")
	reason := fs.String("reason", "", "backup reason")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("backup create accepts only --output and --reason")
	}
	rt, _, _, err := resolveRuntime()
	if err != nil {
		return err
	}
	backup, err := runtimebackup.Create(rt, runtimebackup.Options{OutputDir: *output, Reason: *reason, IncludeLogs: true})
	if err != nil {
		return err
	}
	fmt.Printf("Created Weft backup: %s\n", backup.ID)
	fmt.Printf("Path: %s\n", backup.Path)
	return nil
}

func backupList() error {
	rt, _, _, err := resolveRuntime()
	if err != nil {
		return err
	}
	backups, err := runtimebackup.List(rt)
	if err != nil {
		return err
	}
	if len(backups) == 0 {
		fmt.Println("No Weft backups found.")
		return nil
	}
	fmt.Printf("%-28s %-20s %s\n", "ID", "Created", "Reason")
	for _, backup := range backups {
		fmt.Printf("%-28s %-20s %s\n", backup.ID, backup.CreatedAt, backup.Reason)
	}
	return nil
}

func backupRestore(args []string) error {
	idOrPath, yes, err := parseBackupRestoreArgs(args)
	if err != nil {
		return err
	}
	rt, _, _, err := resolveRuntime()
	if err != nil {
		return err
	}
	backup, err := runtimebackup.Resolve(rt, idOrPath)
	if err != nil {
		return err
	}
	response, statusErr := supervisor.Status(rt)
	supervisorRunning := statusErr == nil
	if supervisorRunning && !yes {
		running := runningAgentCount(response.State)
		if running > 0 {
			fmt.Printf("Restoring a backup requires stopping the Weft supervisor and %d running Codex terminal(s).\n", running)
		} else {
			fmt.Println("Restoring a backup requires stopping the Weft supervisor.")
		}
		if !confirm("Restore backup and stop the supervisor? [y/N] ") {
			fmt.Println("Restore canceled.")
			return nil
		}
	}
	pre, err := runtimebackup.Create(rt, runtimebackup.Options{Reason: "pre-restore " + backup.ID, IncludeLogs: true})
	if err != nil {
		return fmt.Errorf("could not create pre-restore backup: %w", err)
	}
	fmt.Printf("Created pre-restore backup: %s\n", pre.ID)
	if supervisorRunning {
		if err := supervisor.Shutdown(rt); err != nil {
			return err
		}
		waitForSupervisorStop(rt, 2*time.Second)
		if _, err := supervisor.Status(rt); err == nil {
			return errors.New("Weft supervisor did not stop; restore canceled")
		}
	}
	result, err := runtimebackup.RestoreWithPreRestore(rt, backup, &pre)
	if err != nil {
		return err
	}
	fmt.Printf("Restored Weft backup: %s\n", result.Backup.ID)
	return nil
}

func parseBackupRestoreArgs(args []string) (string, bool, error) {
	yes := false
	idOrPath := ""
	for _, arg := range args {
		switch arg {
		case "--yes", "-y":
			yes = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", false, fmt.Errorf("unknown backup restore flag %q", arg)
			}
			if idOrPath != "" {
				return "", false, errors.New("backup restore accepts exactly one backup id or path")
			}
			idOrPath = arg
		}
	}
	if idOrPath == "" {
		return "", false, errors.New("backup restore requires a backup id or path")
	}
	return idOrPath, yes, nil
}

func waitForSupervisorStop(rt config.Runtime, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := supervisor.Status(rt); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func doctor(args []string) error {
	if len(args) > 0 {
		if len(args) == 1 && args[0] == "keys" {
			return doctorKeys(os.Stdin, os.Stdout)
		}
		return fmt.Errorf("unknown doctor command %q", strings.Join(args, " "))
	}
	rt, cfg, store, err := resolveRuntime()
	if err != nil {
		return err
	}
	st, err := store.Read()
	if err != nil {
		return err
	}
	binary := strings.Fields(cfg.CodexCommand)
	if len(binary) > 0 {
		if _, err := exec.LookPath(binary[0]); err == nil || strings.Contains(binary[0], "/") {
			fmt.Printf("ok Codex command: %s\n", cfg.CodexCommand)
		} else {
			fmt.Printf("warn Codex command is not on PATH: %s\n", cfg.CodexCommand)
		}
	}
	fmt.Printf("info launch workspace: %s\n", rt.Workspace)
	fmt.Printf("info runtime dir: %s\n", rt.Dir)
	fmt.Printf("ok config: %s\n", rt.ConfigPath)
	fmt.Printf("ok state: %s (%d workspaces, %d groups, %d agents)\n", rt.StatePath, len(st.Workspaces), len(st.Groups), len(st.Agents))
	if _, err := supervisor.Status(rt); err == nil {
		fmt.Println("ok supervisor: running")
	} else {
		fmt.Println("info supervisor: not running")
	}
	fmt.Println("info supervisor owns Codex PTYs; terminal clients attach and detach without stopping agents.")
	return nil
}

func configCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("config requires one of: info, path, show, init")
	}
	if args[0] == "init" {
		rt, err := config.ResolveRuntime()
		if err != nil {
			return err
		}
		if err := guardRuntimeAccess(rt); err != nil {
			return err
		}
		force := len(args) > 1 && (args[1] == "--force" || args[1] == "-f")
		if _, err := os.Stat(rt.ConfigPath); err == nil && !force {
			return fmt.Errorf("config already exists: %s\nUse `weft config show` to inspect it or `weft config init --force`.", rt.ConfigPath)
		}
		if err := os.MkdirAll(filepath.Dir(rt.ConfigPath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(rt.ConfigPath, []byte(config.DefaultConfigText()), 0o600); err != nil {
			return err
		}
		fmt.Printf("Wrote config: %s\n", rt.ConfigPath)
		return nil
	}
	rt, cfg, _, err := loadRuntime()
	if err != nil {
		return err
	}
	switch args[0] {
	case "info":
		fmt.Println("Weft global runtime")
		fmt.Printf("Launch workspace: %s\n", rt.Workspace)
		fmt.Printf("Runtime dir: %s\n", rt.Dir)
		fmt.Printf("Config: %s\n", rt.ConfigPath)
		fmt.Printf("State: %s\n", rt.StatePath)
		fmt.Printf("Supervisor socket: %s\n", rt.SocketPath)
		fmt.Printf("Codex command: %s\n", cfg.CodexCommand)
		fmt.Printf("Default title template: %s\n", cfg.TitleTemplate)
	case "path":
		fmt.Println(rt.ConfigPath)
	case "show":
		data, err := os.ReadFile(rt.ConfigPath)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
	return nil
}

func callIPC(command string, args map[string]string, quiet bool) error {
	rt, _, store, err := resolveRuntime()
	if err != nil {
		return err
	}
	if _, err := store.Ensure(); err != nil {
		return err
	}
	args = cloneArgs(args)
	args["launch_workspace"] = rt.Workspace
	result, err := supervisor.Ensure(rt)
	if err != nil {
		return err
	}
	if !quiet {
		printUpgrade(result.Status.Upgrade)
	}
	response, err := ipc.Call(rt.SocketPath, ipc.Request{Command: command, Args: args}, 2*time.Second)
	if err != nil {
		if !response.OK && response.Message != "" {
			return errors.New(response.Message)
		}
		return fmt.Errorf("Weft supervisor is not accepting IPC requests; start it with `weft --no-attach`: %w", err)
	}
	if !quiet && response.Message != "" {
		fmt.Println(response.Message)
	}
	return nil
}

func cloneArgs(args map[string]string) map[string]string {
	next := make(map[string]string, len(args)+1)
	for key, value := range args {
		next[key] = value
	}
	return next
}

func loadRuntime() (config.Runtime, config.Config, *state.Store, error) {
	rt, cfg, store, err := resolveRuntime()
	if err != nil {
		return config.Runtime{}, config.Config{}, nil, err
	}
	if _, err := store.Ensure(); err != nil {
		return config.Runtime{}, config.Config{}, nil, err
	}
	return rt, cfg, store, nil
}

func resolveRuntime() (config.Runtime, config.Config, *state.Store, error) {
	rt, err := config.ResolveRuntime()
	if err != nil {
		return config.Runtime{}, config.Config{}, nil, err
	}
	if err := guardRuntimeAccess(rt); err != nil {
		return config.Runtime{}, config.Config{}, nil, err
	}
	cfg, err := config.EnsureConfig(rt)
	if err != nil {
		return config.Runtime{}, config.Config{}, nil, err
	}
	store := state.NewStore(rt.StatePath, rt.Workspace)
	return rt, cfg, store, nil
}

func guardRuntimeAccess(rt config.Runtime) error {
	if rt.HomeExplicit || strings.TrimSpace(version.BuildChannel) == "release" || os.Getenv(config.AllowMainRuntimeEnv) == "1" {
		return nil
	}
	return fmt.Errorf("source builds refuse to use the default Weft runtime at %s.\nUse an isolated dev runtime, for example:\n  %s\nTo intentionally use %s from source, set %s=1.", pathx.Display(rt.Dir), safeDevRuntimeCommand(rt), pathx.Display(rt.Dir), config.AllowMainRuntimeEnv)
}

func safeDevRuntimeCommand(rt config.Runtime) string {
	worktree := strings.TrimSpace(rt.Workspace)
	if worktree == "" {
		cwd, err := os.Getwd()
		if err != nil || strings.TrimSpace(cwd) == "" {
			cwd = "/abs/path/to/weft/.worktrees/<slug>"
		}
		worktree = cwd
	}
	return fmt.Sprintf("%s=%s go -C %s run ./cmd/weft --clear", config.RootEnv, worktree, worktree)
}

func runningAgentCount(st *state.State) int {
	if st == nil {
		return 0
	}
	count := 0
	for _, agent := range st.Agents {
		switch agent.Status {
		case state.StatusStarting, state.StatusRunning, state.StatusReady, state.StatusSitting, state.StatusShipping:
			count++
		}
	}
	return count
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	var answer string
	_, _ = fmt.Scanln(&answer)
	return strings.ToLower(strings.TrimSpace(answer)) == "y"
}

func existingRuntimeFiles(rt config.Runtime) []string {
	candidates := []string{
		rt.StatePath,
		rt.StatePath + ".lock",
		rt.SocketPath,
		supervisor.PIDPath(rt),
		supervisor.LockPath(rt),
		supervisor.LogPath(rt),
		filepath.Join(rt.Dir, "weft-client.log"),
	}
	var paths []string
	for _, path := range candidates {
		if _, err := os.Lstat(path); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
}

func looksLikeID(value string) bool {
	if len(value) < 4 || strings.Contains(value, " ") {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F' || ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}

func displayFocus(focus state.Focus) string {
	if focus == state.FocusWorkspaces {
		return "workspaces"
	}
	if focus == state.FocusAgents {
		return "agents"
	}
	return string(focus)
}

func printUpgrade(upgrade *ipc.Upgrade) {
	if upgrade == nil || strings.TrimSpace(upgrade.Message) == "" {
		return
	}
	fmt.Println(upgrade.Message)
}

func printSupervisorStatus(response ipc.Response) {
	fmt.Printf("client version: %s\n", supervisor.ReportedClientVersion())
	if response.SupervisorVersion != "" {
		fmt.Printf("supervisor version: %s\n", response.SupervisorVersion)
	}
	if response.ProtocolVersion != 0 {
		fmt.Printf("protocol: client %d, supervisor %d\n", ipc.ProtocolVersion, response.ProtocolVersion)
	}
	if response.Upgrade == nil {
		fmt.Println("upgrade: current")
		return
	}
	if response.Upgrade.AutoRestarted {
		fmt.Println("upgrade: supervisor restarted")
		return
	}
	if !response.Upgrade.Compatible {
		fmt.Println("upgrade: incompatible supervisor restart required")
		return
	}
	if response.Upgrade.RunningAgents > 0 {
		fmt.Printf("upgrade: restart pending, %d live Codex terminal(s)\n", response.Upgrade.RunningAgents)
		return
	}
	fmt.Println("upgrade: restart pending")
}

func supervisorResponded(response ipc.Response) bool {
	return response.SupervisorVersion != "" || response.ProtocolVersion != 0 || response.Error != nil || response.Upgrade != nil
}
