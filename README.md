<h1 align="center"><img src="assets/codux-logo.svg" alt="" width="70" valign="middle"> Codux</h1>

<p align="center">
  <strong>Coordinate multiple Codex sessions from a single tmux-hosted TUI.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/go-1.23%2B-4b5563?style=flat-square" alt="Go 1.23+">
  <img src="https://img.shields.io/badge/license-MIT-4b5563?style=flat-square" alt="MIT license">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-4b5563?style=flat-square" alt="macOS and Linux">
</p>

Codux runs one global tmux session with one full-screen pane. That pane hosts a
Go Bubble Tea command center, and the command center owns embedded Codex PTYs
directly. Workdirs, optional flat groups, and agents are managed inside that
one global state file. Detaching tmux leaves the command center and Codex
processes alive.

## Getting Started

Install Codux with Homebrew:

```sh
brew install edwmurph/tap/codux
```

Then run Codux from a project directory. The launch directory is added as the
initial workdir:

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

`codux` and `codux start` create or reattach the global tmux session.
`--no-attach` prepares the runtime without attaching.

```text
codux [--attach|--no-attach]
codux start [--attach|--no-attach]
codux refresh
codux status [--json]
codux new [title]
codux group add <name>
codux workdir add <path>
codux rename [id] <title>
codux close [id]
codux close --kill
codux sessions
codux clear
codux config info
```

Run `codux close` without an id to close Codux clients; pass an id to close a
Codex agent.

Agent rows render through the global `title_template`, which defaults to
`{title}`. Titles passed to `codux new` or `codux rename` can still include
template variables for compatibility:

- `{title}`: user-configured agent title
- `{codex}`: live Codex terminal title
- `{status}`: live Codex status, falling back to agent lifecycle status
- `{workdir}`: agent workdir path
- `{group}`: flat group name, when the agent is in a group

For example, `codux rename "Codex {status}"` keeps a fixed title while showing
the current agent status.

The command center has `Workdirs` and `Agents` navigation panes. Agents can sit
directly in a workdir as top-level rows. Groups are optional collapsible
sections inside the `Agents` pane, and `Enter` on a group opens or collapses it.

When the command center is open, press `?` for shortcuts. Defaults:

```toml
title_template = "{title}"

[key_bindings]
drawer = "C-b"
focus_left = "Left"
focus_right = "Right"
select_prev = "k"
select_next = "j"
open = "Enter"
new_workdir = "w"
new_group = "g"
new_agent = "n"
move_agent = "m"
rename = "r"
delete = "d"
help = "?"
quit = "C-c"
```

In CODEX focus, Codux keeps Codex-owned interrupts available while the active
Codex agent is working. Press `C-c` to interrupt that agent. Once Codex reports
ready, `C-c` closes Codux clients. Use `codux close` from another shell to close
Codux directly.

## Config And State

Codux stores runtime files globally:

- `~/.codux/config.toml`
- `~/.codux/state.json`
- `~/.codux/codux.sock`

`CODUX_WORKDIR` overrides the launch directory that seeds the initial workdir.
`CODUX_HOME` overrides the runtime directory directly for development and tests.

The config keys are stable: `tmux_session`, `codex_command`, `title_template`,
and `key_bindings`. State is versioned. Old tabs/columns state is migrated into
workdirs, optional groups, and agents. Old tmux-pane state is archived to
`state.v1-tmux.json` because native tmux panes cannot be adopted into TUI-owned
PTYs.

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
