package navigation

import (
	"testing"

	"github.com/edwmurph/codux/internal/state"
)

func TestSelectGridTabMovesAcrossColumnsAtSameRow(t *testing.T) {
	tabs := []state.Tab{
		{ID: "a", Column: "inbox"},
		{ID: "b", Column: "inbox"},
		{ID: "c", Column: "implement"},
		{ID: "d", Column: "implement"},
	}

	got := SelectGridTab(tabs, "b", []string{"inbox", "implement", "ship"}, 1, 0)

	if got != "d" {
		t.Fatalf("got %q", got)
	}
}

func TestSelectRelativeWraps(t *testing.T) {
	tabs := []state.Tab{{ID: "a"}, {ID: "b"}}

	if got := SelectRelative(tabs, "a", -1); got != "b" {
		t.Fatalf("got %q", got)
	}
}
