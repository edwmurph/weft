package shellx

import (
	"os"
	"os/exec"
	"strings"
)

func Resolve() string {
	return ResolveFrom(os.Getenv("SHELL"))
}

func ResolveFrom(preferred string) string {
	candidates := []string{strings.TrimSpace(preferred), "/bin/sh", "/usr/bin/sh", "sh"}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if strings.Contains(candidate, "/") {
			if executable(candidate) {
				return candidate
			}
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil && executable(path) {
			return path
		}
	}
	return "/bin/sh"
}

func Env(env []string, shell string) []string {
	next := make([]string, 0, len(env)+1)
	found := false
	for _, item := range env {
		if strings.HasPrefix(item, "SHELL=") {
			next = append(next, "SHELL="+shell)
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		next = append(next, "SHELL="+shell)
	}
	return next
}

func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
