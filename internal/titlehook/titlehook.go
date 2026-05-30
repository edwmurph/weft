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
	AgentID       string `json:"agent_id"`
	Workspace     string `json:"workspace"`
	Workdir       string `json:"workdir"`
	Group         string `json:"group,omitempty"`
	Status        string `json:"status"`
	Title         string `json:"title"`
	TitleTemplate string `json:"title_template"`
	CodexTitle    string `json:"codex_title,omitempty"`
	FirstMessage  string `json:"first_message"`
}

func BuildPayload(agent state.Agent, workdir state.Workdir, folder state.Folder, titleTemplate string, firstMessage string) Payload {
	return Payload{
		Version:       1,
		Event:         EventFirstMessage,
		AgentID:       agent.ID,
		Workspace:     workdir.Path,
		Workdir:       workdir.Path,
		Group:         folder.Path,
		Status:        titles.RenderStatus(agent),
		Title:         agent.Title,
		TitleTemplate: titleTemplate,
		CodexTitle:    titles.NormalizeCodexTitle(agent.CodexTitle),
		FirstMessage:  firstMessage,
	}
}

func Run(ctx context.Context, command string, workdir string, timeout time.Duration, payload Payload) (string, error) {
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
	cmd.Dir = workdir
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
