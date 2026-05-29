package sessions

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/edwmurph/codux/internal/config"
	"github.com/edwmurph/codux/internal/tmuxhost"
)

type CoduxSession struct {
	Name       string
	Workdir    string
	RuntimeDir string
	Windows    int
	Clients    int
	Current    bool
}

func List(current string) []CoduxSession {
	out, err := exec.Command(
		"tmux",
		"list-sessions",
		"-F",
		fmt.Sprintf(
			"#{session_name}\t#{session_windows}\t#{session_attached}\t#{%s}\t#{%s}",
			tmuxhost.WorkdirOption,
			tmuxhost.RuntimeOption,
		),
	).Output()
	if err != nil {
		return nil
	}
	var sessions []CoduxSession
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		for len(parts) < 5 {
			parts = append(parts, "")
		}
		if parts[3] == "" && !strings.HasPrefix(parts[0], "codux") {
			continue
		}
		windows, _ := strconv.Atoi(parts[1])
		clients, _ := strconv.Atoi(parts[2])
		sessions = append(sessions, CoduxSession{
			Name: parts[0], Windows: windows, Clients: clients,
			Workdir: parts[3], RuntimeDir: parts[4], Current: parts[0] == current,
		})
	}
	return sessions
}

func Workspaces() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	root := filepath.Join(home, ".codux", "workdirs")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		paths = append(paths, filepath.Join(root, entry.Name()))
	}
	return paths
}

func DeleteWorkspace(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	root, err := filepath.Abs(filepath.Join(home, ".codux", "workdirs"))
	if err != nil {
		return false
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if target == root || !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return false
	}
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	return os.RemoveAll(target) == nil
}

func DisplayPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func CurrentSessionFromRuntime() string {
	rt, err := config.ResolveRuntime()
	if err != nil {
		return ""
	}
	cfg, err := config.EnsureConfig(rt)
	if err != nil {
		return ""
	}
	return cfg.TmuxSession
}
