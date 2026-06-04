package codexskill

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	Name      = "weft"
	HomeEnv   = "CODEX_HOME"
	skillsDir = "skills"
)

//go:embed weft/SKILL.md
var bundled embed.FS

func Install(force bool) (string, error) {
	home, err := codexHome()
	if err != nil {
		return "", err
	}
	return InstallTo(home, force)
}

func InstallTo(home string, force bool) (string, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return "", fmt.Errorf("%s is empty", HomeEnv)
	}
	target := filepath.Join(home, skillsDir, Name)
	if _, err := os.Lstat(target); err == nil {
		if !force {
			return "", fmt.Errorf("Codex skill already exists: %s\nUse `weft skill install --force` to replace it.", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return "", err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return "", err
	}
	if err := fs.WalkDir(bundled, Name, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(Name, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		targetPath := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o700)
		}
		data, err := bundled.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o600)
	}); err != nil {
		return "", err
	}
	return target, nil
}

func codexHome() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(HomeEnv)); configured != "" {
		return expandHome(configured)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

func expandHome(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
