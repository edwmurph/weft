# Contributing

Codux is a Go project built around `tmux`, Bubble Tea, Lip Gloss, Bubbles, and
PTY-owned Codex child processes.

## Local Setup

```sh
go test ./...
go run ./cmd/codux doctor
```

## Checks

Run the full local workflow before offering a change for review:

```sh
go test ./...
CODUX_RUN_INTEGRATION=1 go test ./...
go build ./cmd/codux
```

Live tmux integration tests are opt-in because they start real tmux servers.
Each test uses temporary `CODUX_HOME`, temporary `CODUX_WORKDIR`, a unique tmux
socket, and a fake `codex_command`.

## Homebrew Publishing

Pushes to `main` run the `Publish Homebrew` workflow. The workflow infers the
semantic version bump from the shipped commit, updates `VERSION` and
`internal/version/version.go`, tags `vX.Y.Z`, creates a GitHub release, and
writes `Formula/codux.rb` to `edwmurph/homebrew-tap`.

The formula builds the Go binary from source and depends on `tmux` at runtime
and `go` at build time.

## Agent Guidance

Codex-agent workflow and maintenance instructions live in `AGENTS.md`.
