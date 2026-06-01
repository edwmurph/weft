# Technical Notes

This document is for contributors and advanced users who want the implementation model. The README intentionally avoids this detail.

## Runtime Model

Weft ships one binary with two roles:

- a CLI/TUI client, started by `weft`
- a local supervisor process, referred to internally as `weftd`

The supervisor owns config loading, state persistence, task processes, terminal screen state, title hooks, local IPC, version negotiation, and attached-client coordination. It listens only on a Unix socket inside the Weft runtime directory.

Each task runs in a PTY owned by the supervisor. The interactive client can detach, restart, or be replaced without directly owning those PTYs.

## State

State is strict and current-version only. Unsupported old state shapes fail with reset guidance instead of being silently migrated.

Important persisted fields include:

- workspaces
- groups
- tasks
- active and selected task ids
- task type id
- task title template
- Codex title, status, session id, and input-submitted metadata when available

Runtime-only details such as process ids, PTY handles, socket clients, terminal size, and screen cache are not persisted.

## Agent Integrations And Commands

Integrated agent support is checked into Weft. `codex` is currently the only supported agent kind and gets Codex-specific status/title capture, session id capture, interrupt routing, and upgrade resume behavior.

Configured shell command tasks use `kind = "terminal"`. They can start any shell command, but they do not get agent-specific resume or title/status behavior unless Weft adds a dedicated integration for that agent.

## Source Builds

Release/Homebrew builds use the default `~/.weft` runtime.

Source builds fail closed before reading or mutating `~/.weft` unless they can infer a checkout-local runtime, or one of these is set:

- `WEFT_ROOT`
- `WEFT_HOME`
- `WEFT_ALLOW_MAIN_RUNTIME=1`

For isolated source testing, run:

```sh
go -C /path/to/weft-or-worktree run ./cmd/weft --clear
```

## Verification

Full implementation verification is:

```sh
gofmt -w cmd internal tests
go test ./...
WEFT_RUN_INTEGRATION=1 go test ./...
go build ./cmd/weft
```

Docs-only changes can use a narrower check when they do not change runtime, generated output, packaging, or user-facing behavior.
