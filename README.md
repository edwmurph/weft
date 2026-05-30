<h1 align="center"><img src="assets/codux-logo.svg" alt="" width="70" valign="middle"> Codux</h1>

<p align="center">
  <strong>Coordinate multiple Codex sessions from a single tmux-hosted TUI.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/go-1.23%2B-4b5563?style=flat-square" alt="Go 1.23+">
  <img src="https://img.shields.io/badge/license-MIT-4b5563?style=flat-square" alt="MIT license">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-4b5563?style=flat-square" alt="macOS and Linux">
</p>

Codux runs one tmux session with one full-screen pane. That pane hosts a Go
Bubble Tea dashboard, and the dashboard owns embedded Codex PTYs directly.
Detaching tmux leaves the dashboard and Codex processes alive.

## Getting Started

Install Codux with Homebrew:

```sh
brew install edwmurph/tap/codux
```

Then run Codux from the project directory you want to manage:

```sh
codux doctor
codux
```

For local development from this repository:

```sh
go run ./cmd/codux doctor
go run ./cmd/codux
```

From another current directory, point Go at the worktree module first:

```sh
go -C /path/to/codux-or-worktree run ./cmd/codux
```

## Usage

`codux` and `codux start` create or reattach the workdir-scoped tmux session.
`--no-attach` prepares the runtime without attaching.

```text
codux [--attach|--no-attach]
codux start [--attach|--no-attach]
codux refresh
codux status [--json]
codux close [id]
codux close --kill
codux sessions
codux clear
codux config info
```

Run `codux close` without an id to close Codux clients; pass an id to close a
Codex session.

Tab titles passed to `codux new` or `codux rename` can interpolate:

- `{codex}`: live Codex terminal title
- `{status}`: live Codex status (`ready` or `working`), falling back to tab
  lifecycle status (`starting`, `running`, `stopped`, or `error`)

For example, `codux rename "Codex {status}"` keeps a fixed title while showing
the current tab status.

When NAV is focused, press `?` for dashboard shortcuts. Defaults:

```toml
[key_bindings]
new = "n"
prev = "Left"
next = "Right"
move_left = "S-Left"
move_right = "S-Right"
rename = "r"
close = "c"
help = "?"
focus_toggle = "C-g"
close_codux = "C-c"
```

In CODEX focus, Codux keeps Codex-owned interrupts available while the active
Codex agent is working. Press `C-c` to interrupt that agent. Once Codex reports
ready, `C-c` closes Codux clients. Use `codux close` from another shell to close
Codux directly.

## Config And State

Codux scopes runtime files to the launch directory:

- `~/.codux/workdirs/<workdir-id>/config.toml`
- `~/.codux/workdirs/<workdir-id>/state.json`
- `~/.codux/workdirs/<workdir-id>/codux.sock`

`CODUX_WORKDIR` overrides the directory used for workdir scoping. `CODUX_HOME`
overrides the runtime directory directly for development and tests.

The config keys are stable: `tmux_session`, `codex_command`, `columns`, and
`key_bindings`. State is versioned. If Codux finds old tmux-pane state, it
archives it to `state.v1-tmux.json` and starts clean because native tmux panes
cannot be adopted into TUI-owned PTYs.

## Development

```sh
go test ./...
CODUX_RUN_INTEGRATION=1 go test ./...
go build ./cmd/codux
```

Live integration tests use temporary `CODUX_HOME`, `CODUX_WORKDIR`, a unique
tmux socket, and a fake `codex_command`. Use
`CODUX_RUN_INTEGRATION=1 go test ./tests/integration -run TestAttachedDashboardKeyboardAndRenderingE2E -v`
to see per-step dashboard timing logs.
