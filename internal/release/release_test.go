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

func TestRenderReleaseNotesGroupsConventionalCommitSubjects(t *testing.T) {
	notes := RenderReleaseNotes([]Commit{
		{Subject: "feat: add workspace cards"},
		{Subject: "fix: keep focused panes visible"},
		{Subject: "docs: document release notes"},
		{Subject: "chore: refresh generated formula"},
	})

	want := `## Features

- Add workspace cards

## Fixes

- Keep focused panes visible

## Documentation

- Document release notes

## Maintenance

- Refresh generated formula
`
	if notes != want {
		t.Fatalf("notes mismatch\nwant:\n%s\ngot:\n%s", want, notes)
	}
}

func TestRenderReleaseNotesUsesExplicitBulletsAndScopes(t *testing.T) {
	notes := RenderReleaseNotes([]Commit{
		{
			Subject: "feat(release): generate human release notes",
			Body: `Release-Notes:
- Publish concise GitHub release notes from ship commits.
- Keep Homebrew formula publishing unchanged.`,
		},
	})

	want := `## Features

- Publish concise GitHub release notes from ship commits.
- Keep Homebrew formula publishing unchanged.
`
	if notes != want {
		t.Fatalf("notes mismatch\nwant:\n%s\ngot:\n%s", want, notes)
	}
}

func TestRenderReleaseNotesLabelsBreakingChanges(t *testing.T) {
	notes := RenderReleaseNotes([]Commit{
		{Subject: "feat(config)!: require WEFT_HOME for supervisor state"},
		{Subject: "fix: repair stale release notes", Body: "BREAKING CHANGE: release notes are regenerated from commits."},
	})

	want := `## Breaking Changes

- Require WEFT_HOME for supervisor state
- Repair stale release notes
`
	if notes != want {
		t.Fatalf("notes mismatch\nwant:\n%s\ngot:\n%s", want, notes)
	}
}

func TestRenderReleaseNotesSkipsReleaseMetadataAndSkipCICommits(t *testing.T) {
	notes := RenderReleaseNotes([]Commit{
		{Subject: "Release v5.3.0 [skip ci]"},
		{Subject: "chore: update generated version [ci skip]"},
		{Subject: "fix: publish notes file"},
	})

	want := `## Fixes

- Publish notes file
`
	if notes != want {
		t.Fatalf("notes mismatch\nwant:\n%s\ngot:\n%s", want, notes)
	}
}

func TestRenderFormulaBuildsGoBinaryFromSource(t *testing.T) {
	formula := RenderFormula("weft", "https://example.test/weft.tar.gz", "abc123")

	for _, expected := range []string{
		"class Weft < Formula",
		`depends_on "go" => :build`,
		`system "go", "build"`,
		`BuildChannel=release`,
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
