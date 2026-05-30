package navigation

import "testing"

func TestMoveIndexClamps(t *testing.T) {
	if got := MoveIndex(0, 3, -1); got != 0 {
		t.Fatalf("left clamp = %d", got)
	}
	if got := MoveIndex(1, 3, 1); got != 2 {
		t.Fatalf("move = %d", got)
	}
	if got := MoveIndex(2, 3, 1); got != 2 {
		t.Fatalf("right clamp = %d", got)
	}
}

func TestIndexByIDFallsBackToZero(t *testing.T) {
	if got := IndexByID([]string{"a", "b"}, "b"); got != 1 {
		t.Fatalf("index = %d", got)
	}
	if got := IndexByID([]string{"a", "b"}, "missing"); got != 0 {
		t.Fatalf("missing index = %d", got)
	}
}
