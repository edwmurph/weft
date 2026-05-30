package tmuxhost

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/edwmurph/weft/internal/config"
)

const (
	WorkdirOption     = "@weft-workdir"
	RuntimeOption     = "@weft-runtime-dir"
	HostVersionOption = "@weft-host-version"
	HostVersion       = "go-tui-v3"
)

type Controller struct {
	Session string
}

func New(session string) Controller {
	return Controller{Session: session}
}

func Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func VersionText() string {
	out, err := exec.Command("tmux", "-V").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c Controller) HasSession() bool {
	err := exec.Command("tmux", "has-session", "-t", c.Session).Run()
	return err == nil
}

func (c Controller) EnsureSession(cfg config.Config, rt config.Runtime, headless bool) error {
	if c.HasSession() {
		if c.sessionOption(HostVersionOption) == HostVersion {
			return nil
		}
		if err := c.KillSession(); err != nil {
			return err
		}
	}
	exe := os.Getenv("WEFT_EXECUTABLE")
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return err
		}
	}
	headlessEnv := ""
	if headless {
		headlessEnv = " WEFT_HEADLESS=1"
	}
	inner := fmt.Sprintf(
		"env %s=%s %s=%s%s %s tui",
		config.AppDirEnv,
		shellQuote(rt.Dir),
		config.WorkdirEnv,
		shellQuote(rt.Workdir),
		headlessEnv,
		shellQuote(exe),
	)
	if headless {
		inner = ""
	}
	command := "sh -lc " + shellQuote(inner)
	if headless {
		command = "sleep 2147483647"
	}
	if err := run("new-session", "-d", "-s", c.Session, "-c", rt.Workdir, command); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"set-option", "-t", c.Session, "status", "off"},
		{"set-option", "-t", c.Session, "destroy-unattached", "off"},
		{"set-option", "-t", c.Session, "allow-rename", "off"},
		{"set-option", "-t", c.Session, "prefix", "None"},
		{"set-option", "-t", c.Session, "prefix2", "None"},
		{"unbind-key", "-T", "prefix", "C-b"},
		{"set-option", "-t", c.Session, WorkdirOption, rt.Workdir},
		{"set-option", "-t", c.Session, RuntimeOption, rt.Dir},
		{"set-option", "-t", c.Session, HostVersionOption, HostVersion},
		{"rename-window", "-t", c.Session + ":0", "weft"},
	} {
		_ = run(args...)
	}
	return nil
}

func (c Controller) sessionOption(option string) string {
	out, err := exec.Command("tmux", "show-option", "-qv", "-t", c.Session, option).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c Controller) Attach() error {
	cmd := exec.Command("tmux", "attach-session", "-t", c.Session)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c Controller) DetachClients() error {
	return run("detach-client", "-s", c.Session)
}

func (c Controller) KillSession() error {
	return run("kill-session", "-t", c.Session)
}

func (c Controller) PaneCount() (int, error) {
	out, err := exec.Command("tmux", "list-panes", "-t", c.Session, "-F", "#{pane_id}").Output()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count, nil
}

func run(args ...string) error {
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
