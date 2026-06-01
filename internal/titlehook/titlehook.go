package titlehook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/edwmurph/weft/internal/shellx"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/titles"
)

const (
	EventFirstMessage = "first_message"
	MaxTitleRunes     = 240
)

type Payload struct {
	Version       int    `json:"version"`
	Event         string `json:"event"`
	TaskID        string `json:"task_id"`
	TypeID        string `json:"type_id"`
	Workspace     string `json:"workspace"`
	Group         string `json:"group,omitempty"`
	Status        string `json:"status"`
	Title         string `json:"title"`
	TitleTemplate string `json:"title_template"`
	CodexTitle    string `json:"codex_title,omitempty"`
	FirstMessage  string `json:"first_message"`
}

func BuildPayload(task state.Task, workspace state.Workspace, group state.Group, titleTemplate string, firstMessage string) Payload {
	return Payload{
		Version:       2,
		Event:         EventFirstMessage,
		TaskID:        task.ID,
		TypeID:        state.TaskTypeID(task),
		Workspace:     workspace.Path,
		Group:         group.Path,
		Status:        titles.RenderStatus(task),
		Title:         task.Title,
		TitleTemplate: titleTemplate,
		CodexTitle:    titles.NormalizeCodexTitle(task.CodexTitle),
		FirstMessage:  firstMessage,
	}
}

func Run(ctx context.Context, command string, workspace string, timeout time.Duration, payload Payload) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("title hook command is empty")
	}
	if timeout <= 0 {
		return "", errors.New("title hook timeout must be greater than zero")
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shell := shellx.Resolve()
	cmd := exec.CommandContext(hookCtx, shell, "-c", command)
	cmd.Dir = workspace
	cmd.Env = shellx.Env(os.Environ(), shell)
	cmd.Stdin = bytes.NewReader(payloadBytes)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if hookCtx.Err() != nil {
			return "", hookCtx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("%w: %s", err, detail)
		}
		return "", err
	}
	title := CleanTitle(stdout.String())
	if title == "" {
		return "", errors.New("title hook produced no title")
	}
	return title, nil
}

func CleanTitle(output string) string {
	for _, line := range strings.Split(output, "\n") {
		title := strings.Join(strings.Fields(line), " ")
		if title == "" {
			continue
		}
		if utf8.RuneCountInString(title) <= MaxTitleRunes {
			return title
		}
		runes := []rune(title)
		return string(runes[:MaxTitleRunes])
	}
	return ""
}
