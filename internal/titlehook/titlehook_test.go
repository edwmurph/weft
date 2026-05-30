package titlehook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edwmurph/weft/internal/state"
)

func TestBuildPayloadUsesAgentContext(t *testing.T) {
	agent := state.Agent{ID: "a", Title: "{auto}", CodexTitle: "Fake Codex Ready", Status: state.StatusRunning}

	payload := BuildPayload(agent, state.Workdir{Path: "/tmp/project"}, state.Folder{Path: "ship"}, "{auto}", "fix login")

	if payload.Version != 1 || payload.Event != EventFirstMessage {
		t.Fatalf("payload identity = %#v", payload)
	}
	if payload.AgentID != "a" || payload.Workspace != "/tmp/project" || payload.Workdir != "/tmp/project" || payload.Group != "ship" {
		t.Fatalf("payload context = %#v", payload)
	}
	if payload.Status != "ready" || payload.FirstMessage != "fix login" {
		t.Fatalf("payload values = %#v", payload)
	}
}

func TestCleanTitleUsesFirstNonEmptyLineAndCollapsesWhitespace(t *testing.T) {
	got := CleanTitle("\n  Fix   the login flow  \nIgnored\n")

	if got != "Fix the login flow" {
		t.Fatalf("got %q", got)
	}
}

func TestRunSendsJSONAndUsesStdoutTitle(t *testing.T) {
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "payload.json")
	scriptPath := filepath.Join(dir, "hook.sh")
	err := os.WriteFile(scriptPath, []byte("#!/bin/sh\ncat > "+shellQuote(capturePath)+"\nprintf '\\nGenerated title\\n'\n"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	payload := Payload{Version: 1, Event: EventFirstMessage, AgentID: "a", FirstMessage: "fix login"}

	got, err := Run(context.Background(), shellQuote(scriptPath), dir, 5*time.Second, payload)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Generated title" {
		t.Fatalf("got %q", got)
	}
	raw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"first_message":"fix login"`) {
		t.Fatalf("payload not captured:\n%s", raw)
	}
}

func TestRunFailsOnEmptyOutput(t *testing.T) {
	_, err := Run(context.Background(), "printf ''", t.TempDir(), time.Second, Payload{Version: 1})

	if err == nil {
		t.Fatal("expected empty output error")
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
