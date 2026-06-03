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
- provider-neutral live title, live status, resume id, and input-submitted metadata when available
- terminal cwd metadata when available

Runtime-only details such as process ids, PTY handles, socket clients, terminal size, and screen cache are not persisted. Normal task terminal output is kept in supervisor memory for the live console and preview, not in `state.json`.

During dashboard upgrade restart, idle terminal tasks can write temporary visible scrollback/cwd snapshots under `terminal-upgrade-snapshots/` in the runtime directory. The restarted supervisor restores those snapshots into read-only task history and removes the files.

## Agent Integrations And Commands

Integrated agent support is checked into Weft as task type definitions. A definition owns behavior for a task kind, including input mode, startup status, command construction, screen-derived status, loading rules, terminal cwd tracking, foreground-command tracking, exit footer behavior, screen resize behavior, and restartability during dashboard `U`. `codex` is currently the only supported agent kind and fills the provider-neutral live title/status and resume metadata through Codex-specific capture, interrupt routing, and upgrade resume behavior.

Configured shell command tasks use `kind = "terminal"`. They can start any shell command, but they do not get agent-specific resume or title/status behavior unless Weft adds a dedicated integration for that agent. During dashboard `U`, an idle terminal task can restart as a fresh shell with saved history/cwd. A terminal task running foreground work blocks upgrade until it becomes idle.

Task processes execute as the current user. PTY task commands run from the task workspace through the resolved user shell with `-lc`, inherit the parent environment with Weft-specific runtime variables and `NO_COLOR` removed, and can access the same files, credentials, and network as a command run directly in the terminal. Title hooks also run locally from the task workspace and inherit the environment with `SHELL` normalized.

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
