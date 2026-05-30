package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInferBumpAndBumpVersion(t *testing.T) {
	if got := InferBump("feat: add sessions command", nil); got != "minor" {
		t.Fatalf("got %q", got)
	}
	if got := InferBump("fix: repaint focused pane", []string{"internal/tui/model.go"}); got != "patch" {
		t.Fatalf("got %q", got)
	}
	if got := InferBump("refactor!: rewrite runtime", nil); got != "major" {
		t.Fatalf("got %q", got)
	}
	next, err := BumpVersion("1.2.3", "minor")
	if err != nil {
		t.Fatal(err)
	}
	if next != "1.3.0" {
		t.Fatalf("next = %q", next)
	}
}

func TestRenderFormulaBuildsGoBinaryFromSource(t *testing.T) {
	formula := RenderFormula("weft", "https://example.test/weft.tar.gz", "abc123")

	for _, expected := range []string{
		"class Weft < Formula",
		`depends_on "go" => :build`,
		`system "go", "build"`,
		`./cmd/weft`,
		`WEFT_HOME`,
		`weft doctor`,
	} {
		if !strings.Contains(formula, expected) {
			t.Fatalf("formula missing %q:\n%s", expected, formula)
		}
	}
	if strings.Contains(formula, `depends_on "tmux"`) {
		t.Fatalf("formula should not depend on tmux:\n%s", formula)
	}
}

func TestWriteGoVersionUpdatesVersionVariable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version.go")
	if err := os.WriteFile(path, []byte("package version\n\nvar Version = \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteGoVersion(path, "1.2.0"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `var Version = "1.2.0"`) {
		t.Fatalf("version not updated:\n%s", data)
	}
}
