package ipc

import (
	"testing"

	"github.com/edwmurph/weft/internal/state"
)

func TestRunningTaskCountHandlesNilState(t *testing.T) {
	if got := RunningTaskCount(nil); got != 0 {
		t.Fatalf("RunningTaskCount(nil) = %d, want 0", got)
	}
}

func TestRunningTaskCountCountsLiveTaskTerminalStatuses(t *testing.T) {
	tests := []struct {
		status state.TaskStatus
		want   int
	}{
		{status: state.StatusStarting, want: 1},
		{status: state.StatusRunning, want: 1},
		{status: state.StatusReady, want: 1},
		{status: state.StatusSitting, want: 1},
		{status: state.StatusShipping, want: 1},
		{status: state.StatusStopped, want: 0},
		{status: state.StatusKilled, want: 0},
		{status: state.StatusError, want: 0},
	}

	for _, tt := range tests {
		st := &state.State{Tasks: []state.Task{{ID: string(tt.status), Status: tt.status}}}
		if got := RunningTaskCount(st); got != tt.want {
			t.Fatalf("RunningTaskCount(%s) = %d, want %d", tt.status, got, tt.want)
		}
	}
}
