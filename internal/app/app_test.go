package app

import (
	"strings"
	"testing"

	"github.com/edwmurph/weft/internal/tui"
)

func TestCLIHelpIncludesLogoAndClearLaunch(t *testing.T) {
	help := cliHelpText()

	if !strings.HasPrefix(help, "\n  ") {
		t.Fatalf("help should leave breathing room above and left of the logo:\n%s", help)
	}
	for _, line := range tui.WeftLogoLines() {
		if !strings.Contains(help, line) {
			t.Fatalf("help missing logo line %q:\n%s", line, help)
		}
	}
	for _, expected := range []string{
		"Supervisor-backed Codex command center.",
		"weft [--attach|--no-attach] [--clear]",
		"weft --clear                 Clear runtime state, then open a fresh dashboard.",
		"weft close --kill [--yes]    Stop the supervisor and all Codex PTYs.",
	} {
		if !strings.Contains(help, expected) {
			t.Fatalf("help missing %q:\n%s", expected, help)
		}
	}
	for _, forbidden := range []string{
		"weft start",
		"Title templates:",
		"Weft uses one global runtime",
		"unless you use close --kill or clear",
	} {
		if strings.Contains(help, forbidden) {
			t.Fatalf("help should not contain %q:\n%s", forbidden, help)
		}
	}
}
