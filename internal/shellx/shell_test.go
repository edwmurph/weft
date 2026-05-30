package shellx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFromFallsBackWhenPreferredShellMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-zsh")
	got := ResolveFrom(missing)
	if got == "" || got == missing {
		t.Fatalf("ResolveFrom returned %q", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("fallback shell does not exist: %q", got)
	}
}

func TestEnvReplacesShell(t *testing.T) {
	env := Env([]string{"A=1", "SHELL=/missing/zsh"}, "/bin/sh")
	if len(env) != 2 || env[0] != "A=1" || env[1] != "SHELL=/bin/sh" {
		t.Fatalf("env = %#v", env)
	}
}
