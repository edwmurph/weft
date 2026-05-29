package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/edwmurph/codux/internal/config"
	"github.com/edwmurph/codux/internal/ipc"
	"github.com/edwmurph/codux/internal/sessions"
	"github.com/edwmurph/codux/internal/state"
	"github.com/edwmurph/codux/internal/tmuxhost"
	"github.com/edwmurph/codux/internal/tui"
	"github.com/edwmurph/codux/internal/version"
)

const helpText = `Start, inspect, or detach Codux tmux workspaces for Codex.

Usage:
  codux [--attach|--no-attach]
  codux start [--attach|--no-attach]
  codux quit [--kill]
  codux refresh
  codux status [--json]
  codux new [title]
  codux rename [id] <title>
  codux close [id]
  codux select <id>
  codux move-left
  codux move-right
  codux sessions
  codux delete-session <session>
  codux clear
  codux doctor
  codux config <info|path|show|init>

Codux is scoped to the directory where you run it. Each launch directory gets
a runtime directory under ~/.codux/workdirs/<workdir-id>, a config.toml, a
versioned state.json, an IPC socket, and one tmux session hosting one TUI pane.
`

func Run(args []string) error {
	if len(args) == 0 {
		return start([]string{})
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(helpText)
		return nil
	case "--version", "version":
		fmt.Println(version.Version)
		return nil
	case "start":
		return start(args[1:])
	case "tui":
		return runTUI()
	case "quit":
		return quit(args[1:])
	case "refresh":
		return callIPC("refresh", nil, false)
	case "status":
		return status(args[1:])
	case "new":
		title := ""
		if len(args) > 1 {
			title = strings.Join(args[1:], " ")
		}
		return callIPC("new", map[string]string{"title": title}, false)
	case "rename":
		return rename(args[1:])
	case "close":
		id := ""
		if len(args) > 1 {
			id = args[1]
		}
		return callIPC("close", map[string]string{"id": id}, false)
	case "select":
		if len(args) < 2 {
			return errors.New("select requires a tab id")
		}
		return callIPC("select", map[string]string{"id": args[1]}, false)
	case "move-left":
		return callIPC("move", map[string]string{"direction": "left"}, false)
	case "move-right":
		return callIPC("move", map[string]string{"direction": "right"}, false)
	case "sessions":
		return listSessions()
	case "delete-session":
		if len(args) < 2 {
			return errors.New("delete-session requires a session name")
		}
		return tmuxhost.New(args[1]).KillSession()
	case "clear":
		return clear()
	case "doctor":
		return doctor()
	case "config":
		return configCommand(args[1:])
	default:
		if strings.HasPrefix(args[0], "--") {
			return start(args)
		}
		return fmt.Errorf("unknown command %q\n\n%s", args[0], helpText)
	}
}

func start(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	attach := fs.Bool("attach", true, "attach to the tmux session")
	noAttach := fs.Bool("no-attach", false, "prepare the tmux session without attaching")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *noAttach {
		*attach = false
	}
	rt, cfg, _, err := resolveRuntime()
	if err != nil {
		return err
	}
	controller := tmuxhost.New(cfg.TmuxSession)
	if !*attach && !controller.HasSession() {
		if err := startHeadlessDaemon(rt); err != nil {
			return err
		}
	}
	if err := controller.EnsureSession(cfg, rt, !*attach); err != nil {
		return err
	}
	if *attach {
		return controller.Attach()
	}
	fmt.Printf("Prepared tmux session %s for %s.\n", cfg.TmuxSession, rt.Workdir)
	fmt.Printf("Config: %s\n", rt.ConfigPath)
	fmt.Printf("State: %s\n", rt.StatePath)
	return nil
}

func runTUI() error {
	rt, cfg, store, err := resolveRuntime()
	if err != nil {
		return err
	}
	logPath := filepath.Join(rt.Dir, "codux.log")
	_ = os.WriteFile(logPath, []byte("starting TUI\n"), 0o600)
	st, migration, err := store.Ensure()
	if err != nil {
		return err
	}
	if os.Getenv("CODUX_HEADLESS") == "1" {
		if err := tui.RunHeadless(rt, cfg, st, migration); err != nil {
			_ = os.WriteFile(logPath, []byte("headless error: "+err.Error()+"\n"), 0o600)
			return err
		}
		_ = os.WriteFile(logPath, []byte("headless exited cleanly\n"), 0o600)
		return nil
	}
	if err := tui.Run(rt, cfg, st, migration); err != nil {
		_ = os.WriteFile(logPath, []byte("TUI error: "+err.Error()+"\n"), 0o600)
		return err
	}
	_ = os.WriteFile(logPath, []byte("TUI exited cleanly\n"), 0o600)
	return nil
}

func quit(args []string) error {
	fs := flag.NewFlagSet("quit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	kill := fs.Bool("kill", false, "kill the Codux tmux session")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, cfg, _, err := resolveRuntime()
	if err != nil {
		return err
	}
	controller := tmuxhost.New(cfg.TmuxSession)
	if !controller.HasSession() {
		fmt.Printf("tmux session is not running: %s\n", cfg.TmuxSession)
		return nil
	}
	if *kill {
		_ = callIPC("shutdown", nil, true)
		return controller.KillSession()
	}
	if err := callIPC("quit", nil, true); err == nil {
		return nil
	}
	return controller.DetachClients()
}

func startHeadlessDaemon(rt config.Runtime) error {
	exe := os.Getenv("CODUX_EXECUTABLE")
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return err
		}
	}
	logFile, err := os.OpenFile(filepath.Join(rt.Dir, "codux.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "tui")
	cmd.Env = append(os.Environ(),
		config.AppDirEnv+"="+rt.Dir,
		config.WorkdirEnv+"="+rt.Workdir,
		"CODUX_HEADLESS=1",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	return nil
}

func status(args []string) error {
	jsonOutput := false
	if len(args) > 0 && args[0] == "--json" {
		jsonOutput = true
	}
	rt, cfg, store, err := loadRuntime()
	if err != nil {
		return err
	}
	response, err := ipc.Call(rt.SocketPath, ipc.Request{Command: "status"}, 500*time.Millisecond)
	if err == nil {
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(response.State)
		}
		fmt.Println(response.Message)
		return nil
	}
	st, _ := store.Read()
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(st)
	}
	controller := tmuxhost.New(cfg.TmuxSession)
	fmt.Printf("tmux session: %s (%s)\n", cfg.TmuxSession, upDown(controller.HasSession()))
	fmt.Printf("workdir: %s\nruntime dir: %s\nfocus: %s\ntabs: %d\n", rt.Workdir, rt.Dir, st.Focus, len(st.Tabs))
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

func listSessions() error {
	current := sessions.CurrentSessionFromRuntime()
	items := sessions.List(current)
	if len(items) == 0 {
		fmt.Println("No Codux sessions are running.")
		return nil
	}
	fmt.Printf("%-2s %-32s %7s %7s %s\n", "", "Session", "Clients", "Windows", "Workdir")
	for _, item := range items {
		marker := ""
		if item.Current {
			marker = "*"
		}
		fmt.Printf("%-2s %-32s %7d %7d %s\n", marker, item.Name, item.Clients, item.Windows, sessions.DisplayPath(item.Workdir))
	}
	return nil
}

func clear() error {
	current := sessions.CurrentSessionFromRuntime()
	items := sessions.List(current)
	workspaces := sessions.Workspaces()
	if len(items) == 0 && len(workspaces) == 0 {
		fmt.Println("No Codux workspaces or sessions found.")
		return nil
	}
	fmt.Println("This will delete all Codux tmux sessions and workspace runtimes:")
	for _, item := range items {
		suffix := ""
		if item.Current {
			suffix = " (current)"
		}
		fmt.Printf("- tmux session: %s%s %s\n", item.Name, suffix, sessions.DisplayPath(item.Workdir))
	}
	for _, workspace := range workspaces {
		fmt.Printf("- workspace: %s\n", sessions.DisplayPath(workspace))
	}
	fmt.Print("Delete all Codux workspaces and sessions? [y/N] ")
	var answer string
	_, _ = fmt.Scanln(&answer)
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		fmt.Println("Delete canceled.")
		return nil
	}
	deletedSessions := 0
	for _, item := range items {
		if tmuxhost.New(item.Name).KillSession() == nil {
			deletedSessions++
		}
	}
	deletedWorkspaces := 0
	for _, workspace := range workspaces {
		if sessions.DeleteWorkspace(workspace) {
			deletedWorkspaces++
		}
	}
	fmt.Printf("Deleted %d tmux session(s) and %d workspace(s).\n", deletedSessions, deletedWorkspaces)
	return nil
}

func doctor() error {
	rt, cfg, store, err := loadRuntime()
	if err != nil {
		return err
	}
	st, _ := store.Read()
	problems := 0
	if tmuxhost.Available() {
		fmt.Printf("ok tmux: %s\n", tmuxhost.VersionText())
	} else {
		fmt.Println("error tmux is not installed or not on PATH")
		problems++
	}
	binary := strings.Fields(cfg.CodexCommand)
	if len(binary) > 0 {
		if _, err := exec.LookPath(binary[0]); err == nil || strings.Contains(binary[0], "/") {
			fmt.Printf("ok Codex command: %s\n", cfg.CodexCommand)
		} else {
			fmt.Printf("warn Codex command is not on PATH: %s\n", cfg.CodexCommand)
		}
	}
	fmt.Printf("info workdir: %s\n", rt.Workdir)
	fmt.Printf("info runtime dir: %s\n", rt.Dir)
	fmt.Printf("ok config: %s\n", rt.ConfigPath)
	fmt.Printf("ok state: %s (%d tabs)\n", rt.StatePath, len(st.Tabs))
	fmt.Println("info tmux hosts one full-screen Codux TUI pane; Codex sessions run as TUI-owned PTYs.")
	if problems > 0 {
		return errors.New("doctor found problems")
	}
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
		force := len(args) > 1 && (args[1] == "--force" || args[1] == "-f")
		if _, err := os.Stat(rt.ConfigPath); err == nil && !force {
			return fmt.Errorf("config already exists: %s\nUse `codux config show` to inspect it or `codux config init --force`.", rt.ConfigPath)
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
		fmt.Println("Codux workdir runtime")
		fmt.Printf("Workdir: %s\n", rt.Workdir)
		fmt.Printf("Runtime dir: %s\n", rt.Dir)
		fmt.Printf("Config: %s\n", rt.ConfigPath)
		fmt.Printf("State: %s\n", rt.StatePath)
		fmt.Printf("IPC socket: %s\n", rt.SocketPath)
		fmt.Printf("tmux session: %s\n", cfg.TmuxSession)
		fmt.Printf("Codex command: %s\n", cfg.CodexCommand)
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
	rt, _, _, err := resolveRuntime()
	if err != nil {
		return err
	}
	response, err := ipc.Call(rt.SocketPath, ipc.Request{Command: command, Args: args}, 2*time.Second)
	if err != nil {
		return fmt.Errorf("Codux TUI is not accepting IPC requests; start it with `codux`: %w", err)
	}
	if !quiet && response.Message != "" {
		fmt.Println(response.Message)
	}
	return nil
}

func loadRuntime() (config.Runtime, config.Config, *state.Store, error) {
	rt, cfg, store, err := resolveRuntime()
	if err != nil {
		return config.Runtime{}, config.Config{}, nil, err
	}
	if _, _, err := store.Ensure(); err != nil {
		return config.Runtime{}, config.Config{}, nil, err
	}
	return rt, cfg, store, nil
}

func resolveRuntime() (config.Runtime, config.Config, *state.Store, error) {
	rt, err := config.ResolveRuntime()
	if err != nil {
		return config.Runtime{}, config.Config{}, nil, err
	}
	cfg, err := config.EnsureConfig(rt)
	if err != nil {
		return config.Runtime{}, config.Config{}, nil, err
	}
	store := state.NewStore(rt.StatePath)
	return rt, cfg, store, nil
}

func upDown(up bool) string {
	if up {
		return "up"
	}
	return "down"
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
