package sessions

import (
	"os"
	"path/filepath"
	"strings"
)

type WeftSession struct {
	Name       string
	Workdir    string
	RuntimeDir string
	Windows    int
	Clients    int
	Current    bool
}

func List(current string) []WeftSession {
	return nil
}

func Workspaces() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	root := filepath.Join(home, ".weft", "workdirs")
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
	root, err := filepath.Abs(filepath.Join(home, ".weft", "workdirs"))
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
	return ""
}
