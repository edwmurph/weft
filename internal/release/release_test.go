package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInferBumpAndBumpVersionWithOptions(t *testing.T) {
	if got := InferBump("feat: add sessions command", nil); got != "minor" {
		t.Fatalf("got %q", got)
	}
	if got := InferBump("fix: repaint focused pane", []string{"internal/tui/model.go"}); got != "patch" {
		t.Fatalf("got %q", got)
	}
	if got := InferBump("refactor!: rewrite runtime", nil); got != "major" {
		t.Fatalf("got %q", got)
	}
	next, err := BumpVersionWithOptions("1.2.3", "minor", BumpOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if next != "1.3.0" {
		t.Fatalf("next = %q", next)
	}
	next, err = BumpVersionWithOptions("0.0.0", "patch", BumpOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if next != "0.0.1" {
		t.Fatalf("initial patch release = %q", next)
	}
}

func TestBumpVersionKeepsZeroMajorUntilStableRelease(t *testing.T) {
	next, err := BumpVersionWithOptions("0.9.1", "major", BumpOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if next != "0.10.0" {
		t.Fatalf("pre-1 major bump = %q", next)
	}

	next, err = BumpVersionWithOptions("0.9.1", "major", BumpOptions{AllowStableMajor: true})
	if err != nil {
		t.Fatal(err)
	}
	if next != "1.0.0" {
		t.Fatalf("stable major bump = %q", next)
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
	commits := []Commit{
		{
			Subject: "feat(config)!: require WEFT_HOME for supervisor state",
			Body: `BREAKING CHANGE: Supervisor state no longer defaults to the old global path.
Migration: Set WEFT_HOME before launching Weft or run weft clear after backing up state.`,
		},
		{
			Subject: "fix: repair stale release notes",
			Body: `BREAKING CHANGE: release notes are regenerated from commits.
Migration: Add Release-Notes bullets to ship commits when the subject is not enough.`,
		},
	}
	notes := RenderReleaseNotes(commits)

	want := `## Breaking Changes

Review these before upgrading.

- **Require WEFT_HOME for supervisor state**
  - Impact: Supervisor state no longer defaults to the old global path.
  - Migration: Set WEFT_HOME before launching Weft or run weft clear after backing up state.
- **Repair stale release notes**
  - Impact: Release notes are regenerated from commits.
  - Migration: Add Release-Notes bullets to ship commits when the subject is not enough.
`
	if notes != want {
		t.Fatalf("notes mismatch\nwant:\n%s\ngot:\n%s", want, notes)
	}
	breakingChanges := BreakingChangesFromCommits(commits)
	if len(breakingChanges) != 2 {
		t.Fatalf("breaking changes count = %d", len(breakingChanges))
	}
	if breakingChanges[0].Text != "Require WEFT_HOME for supervisor state" {
		t.Fatalf("breaking change text = %q", breakingChanges[0].Text)
	}
	if breakingChanges[0].Impact != "Supervisor state no longer defaults to the old global path." {
		t.Fatalf("breaking change impact = %q", breakingChanges[0].Impact)
	}
	if breakingChanges[0].Migration != "Set WEFT_HOME before launching Weft or run weft clear after backing up state." {
		t.Fatalf("breaking change migration = %q", breakingChanges[0].Migration)
	}
}

func TestRenderReleaseNotesWarnsWhenBreakingMigrationIsMissing(t *testing.T) {
	notes := RenderReleaseNotes([]Commit{
		{Subject: "refactor!: remove legacy tab state"},
	})

	want := `## Breaking Changes

Review these before upgrading.

- **Remove legacy tab state**
  - Migration: Review this item before upgrading; no migration step was documented.
`
	if notes != want {
		t.Fatalf("notes mismatch\nwant:\n%s\ngot:\n%s", want, notes)
	}
}

func TestRenderReleaseNotesReadsMultilineBreakingMigrationFooter(t *testing.T) {
	notes := RenderReleaseNotes([]Commit{
		{
			Subject: "feat(config)!: reject legacy config files",
			Body: `Release-Notes:
- Reject legacy config files.

BREAKING-CHANGE: Legacy config keys are unsupported.
Migration:
- Remove retired keys from config.toml.
- Run weft doctor before launching.`,
		},
	})

	want := `## Breaking Changes

Review these before upgrading.

- **Reject legacy config files.**
  - Impact: Legacy config keys are unsupported.
  - Migration: Remove retired keys from config.toml. Run weft doctor before launching.
`
	if notes != want {
		t.Fatalf("notes mismatch\nwant:\n%s\ngot:\n%s", want, notes)
	}
}

func TestRenderReleaseNotesSkipsReleaseMetadataAndCISkipCommits(t *testing.T) {
	notes := RenderReleaseNotes([]Commit{
		{Subject: "Release v5.3.0"},
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
		`WEFT_ROOT`,
		`weft doctor`,
	} {
		if !strings.Contains(formula, expected) {
			t.Fatalf("formula missing %q:\n%s", expected, formula)
		}
	}
	if strings.Contains(formula, `depends_on "tmux"`) {
		t.Fatalf("formula should not depend on tmux:\n%s", formula)
	}
	if strings.Contains(formula, `def caveats`) {
		t.Fatalf("formula should not include caveats without breaking changes:\n%s", formula)
	}
	if strings.Contains(formula, "\n\n\n  test do") {
		t.Fatalf("formula should not include an extra blank line before test block:\n%s", formula)
	}
}

func TestRenderFormulaIncludesBreakingChangeCaveats(t *testing.T) {
	formula := RenderFormula(
		"weft",
		"https://example.test/weft.tar.gz",
		"abc123",
		BreakingChange{
			Text:      "Reject legacy config files.",
			Impact:    "Legacy config keys are unsupported.",
			Migration: "Remove retired keys from config.toml.",
		},
		BreakingChange{
			Text: "Remove legacy tab state",
		},
	)

	for _, expected := range []string{
		`def caveats`,
		`notes = <<~WEFT_CAVEATS`,
		`This Weft release includes breaking changes.`,
		`- Reject legacy config files.`,
		`Impact: Legacy config keys are unsupported.`,
		`Migration: Remove retired keys from config.toml.`,
		`- Remove legacy tab state`,
		`Migration: Review this item before upgrading; no migration step was documented.`,
		`https://github.com/edwmurph/weft/releases/tag/v#{version}`,
	} {
		if !strings.Contains(formula, expected) {
			t.Fatalf("formula missing %q:\n%s", expected, formula)
		}
	}
	if strings.Contains(formula, `<<~'WEFT_CAVEATS'`) {
		t.Fatalf("formula should not quote heredoc delimiter:\n%s", formula)
	}
	if strings.Contains(formula, "\n\n\n  test do") {
		t.Fatalf("formula should not include an extra blank line before test block:\n%s", formula)
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
