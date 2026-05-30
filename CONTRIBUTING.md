# Contributing

Weft is a Go project built around a local supervisor, Bubble Tea, Lip Gloss,
Bubbles, and supervisor-owned Codex PTYs.

## Local Setup

```sh
go test ./...
go run ./cmd/weft doctor
```

## Checks

Run the full local workflow before offering a change for review:

```sh
go test ./...
WEFT_RUN_INTEGRATION=1 go test ./...
go build ./cmd/weft
```

Live integration tests are opt-in because they start real supervisor and PTY
processes. Each test uses temporary `WEFT_HOME`, temporary `WEFT_WORKDIR`, and
a fake `codex_command`.

## Homebrew Publishing

Pushes to `main` run the `Publish Homebrew` workflow. The workflow infers the
semantic version bump from the shipped commit, updates `VERSION` and
`internal/version/version.go`, tags `vX.Y.Z`, creates a GitHub release, and
writes `Formula/weft.rb` to `edwmurph/homebrew-tap`.

GitHub release notes are generated from the shipped commits in the release
range. Use concise Conventional Commit-style subjects such as `feat: ...`,
`fix: ...`, `docs: ...`, `refactor: ...`, or `chore: ...`; add commit-body
`Release-Notes:` bullets when the release needs clearer user-facing wording
than the subject alone.

The formula builds the Go binary from source and depends on `go` at build time.

## Agent Guidance

Codex-agent workflow and maintenance instructions live in `AGENTS.md`.
