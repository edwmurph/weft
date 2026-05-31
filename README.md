<h1 align="center"><img src="assets/weft-logo.svg" alt="Weft" width="360"></h1>

<p align="center">
  <strong>A terminal dashboard for Codex agent threads, workspaces, and groups.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/go-1.23%2B-4b5563?style=flat-square" alt="Go 1.23+">
  <img src="https://img.shields.io/badge/license-MIT-4b5563?style=flat-square" alt="MIT license">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-4b5563?style=flat-square" alt="macOS and Linux">
</p>

Weft makes parallel Codex work visible. Codex sessions are long-running lines
of work: editing, testing, waiting for review, or blocked while other sessions
keep moving. Weft keeps them in one terminal dashboard so you can organize
agents by workspace and group, check status quickly, and jump back into the
right console without losing running PTYs.

## Why Weft

- See every Codex agent in one dashboard instead of hunting through terminal
  tabs.
- Organize agents into optional groups inside each workspace, such as
  `release`, `review`, `bugs`, or `experiments`.
- Track workspace totals for `active` and `needs attention` agents so finished
  or waiting work does not sit idle.
- Auto-name dashboard agents from their first chat message with a configurable
  title hook.
- Detach, upgrade, or reopen the UI while the local supervisor keeps Codex PTYs
  alive.
- Keep Codex framed in the terminal: focus one agent for direct input, then
  return to the dashboard to switch or reorganize.

## Getting Started

Install Weft with Homebrew:

```sh
brew install edwmurph/tap/weft
```

After `brew upgrade weft`, reopen the dashboard with `weft`. If only the client
needed to reopen, Weft will be current. If the older supervisor is still
running, the dashboard shows an upgrade banner; press `U` to queue a safe
restart when idle. Weft will not stop live Codex terminals for that queued
restart.

Then run Weft from a project directory. On first interactive launch, Weft asks
whether to add that directory as a workspace:

```sh
weft doctor
weft
```

For local development from this repository:

```sh
go run ./cmd/weft doctor
go run ./cmd/weft
```

From another current directory, point Go at the worktree module first:

```sh
go -C /path/to/weft-or-worktree run ./cmd/weft
```

## Usage

`weft` ensures the local supervisor is running and attaches a terminal UI
client. `--no-attach` starts or reuses the supervisor without attaching.
`--clear` deletes runtime state before the requested command runs, which is
useful for fresh dashboard or doctor-command testing.

```text
weft [--clear] [--attach|--no-attach]
weft <command> [--clear]

Common commands:
weft                         Open the dashboard and attach to the supervisor.
weft --clear                 Clear runtime state, then open a fresh dashboard.
weft <command> --clear       Clear runtime state, then run the command.
weft --no-attach             Start or reuse the supervisor without opening the dashboard.
weft refresh                 Request a fresh dashboard snapshot.
weft status [--json]         Show supervisor, workspace, group, and agent state.
weft version                 Show CLI, supervisor, and dashboard versions.
weft doctor                  Check local runtime and Codex command health.
weft doctor keys             Diagnose terminal key encoding.

Agents and organization:
weft new [title]             Create a Codex agent.
weft select <id>             Make an agent active.
weft rename [id] <title>     Rename the selected agent or the given agent.
weft close [id]              Close the active client or a Codex agent.
weft group add <name>        Add a group in the current workspace.
weft workspace add <path>    Add a workspace to the dashboard.
weft move-left               Move the selected agent out of its group.
weft move-right              Move the selected agent into the selected group.

Runtime and configuration:
weft close --kill [--yes]    Stop the supervisor and all Codex PTYs.
weft clear                   Prompt, then delete Weft runtime state.
weft backup create           Back up config, state, and logs.
weft backup list             List saved runtime backups.
weft backup restore <id>     Restore config and state from a backup.
weft config info             Show runtime paths and active config.
weft config show             Print config.toml.
weft config init [--force]   Write the default config.
```

Run `weft close` without an id to detach the active Weft client while the
supervisor and Codex PTYs keep running. Pass an id to close a Codex agent. Use
`weft close --kill` to stop the supervisor and all Codex PTYs after
confirmation.

Use `weft doctor keys` when Option/Alt shortcuts do not behave like the rest of
your terminal. It compares Backspace, Option+Backspace, and Ctrl+Backspace and
prints the terminal setting needed when Option is being sent as plain
Backspace. In iTerm2, it can offer to add the needed Option+Backspace key
fix to the current/default profile after writing a plist backup: Left/Right
Option Key are set to Esc+, a fallback Option+Backspace mapping is added, and
obsolete mappings from earlier Weft attempts are removed. If the profile is
already configured but the current tab still sends plain Backspace, it reports
that the running iTerm2 session has not picked up the preference yet. For
custom iTerm2 settings folders, it tells the user to quit and reopen iTerm2
because new tabs may keep using the in-memory profile.

## Agent Titles

Agent rows in the dashboard are customizable. Each agent renders from a title
template, and Weft can call a hook after the first non-empty chat message to
generate an `{auto}` title for that agent. The hook is a plain shell command, so
you can plug in your own naming logic while keeping the dashboard focused on the
saved title.

New agents copy the global `title_template`, which defaults to
`{status} {auto}`, into their own title so the rename pane opens with that
editable template. Titles passed to `weft new` or `weft rename` can still
include template variables:

- `{title}`: user-configured agent title
- `{auto}`: generated title from the first submitted message
- `{codex}`: live Codex terminal title
- `{status}`: live Codex status with Codex casing, falling back to agent lifecycle status
- `{workspace}`: agent workspace path
- `{group}`: flat group name, when the agent is in a group

For example, `weft rename "Codex {status}"` keeps a fixed title while showing
the current agent status.

To generate `{auto}` titles from the first chat message, configure a hook
command:

```toml
title_hook_command = "/path/to/weft/hooks/auto-title-openai.sh"
title_hook_timeout_seconds = 10
```

When `title_hook_command` is configured, the first non-empty message submitted
to each new Codex agent runs the hook from that agent's workspace. Weft sends
JSON on stdin with `event`, `agent_id`, `workspace`, `group`, `status`,
`title`, the agent `title_template`, `codex_title`, and `first_message`, then
saves the first non-empty stdout line as the generated title.

Set an agent title to `{auto}` in the rename pane, or run
`weft rename <id> "{auto}"`, to display that saved generated title. To make new
agents start with generated titles, set `title_template = "{auto}"`. Before the
first message, `{auto}` renders as `waiting for first message`; failed hooks
render as `auto title failed`, show a footer error, and keep the full error in
the rename pane.

Weft treats hooks as generic shell commands. The checked-in
`hooks/auto-title-openai.sh` script is one real hook implementation; it reads
`OPENAI_API_KEY` from the environment or a local ignored `.env` file and uses
the OpenAI Responses API with `OPENAI_TITLE_MODEL`, defaulting to
`gpt-5.4-nano`, to return a short task title. The prompt only uses the first
message, so simple greetings like `hi` stay simple. You can replace it with any
command that follows the same stdin/stdout contract. Set
`WEFT_OPENAI_ENV_FILE=/path/to/.env` in the hook command when the API key lives
outside the agent workspace.

The dashboard has `Workspaces`, `Agents`, and `Agent Live Preview` panes.
Workspaces and agents live in the left-side navigation panes; the preview stays
ready on the right as a read-only live lens into the selected agent, with
cropped lines marked at the right edge. Press
`Enter` to open the selected thread in the focused `Agent Console`, where Codex
input is forwarded. Agents can sit directly in a workspace as top-level rows,
or they can be organized into optional collapsible groups inside the `Agents`
pane. Use groups for whatever makes the work easier to scan: release tasks,
experiments, review follow-ups, bug fixes, or blocked investigations. `Enter`
on a group opens or collapses it. When an agent row is selected,
`Shift+Up`/`Shift+Down` moves it within its current group or top-level area.

Dashboard forms use bordered inputs, compact validation/status lines, and
state-specific key hints. In the `Workspaces` pane, `w` opens an add-workspace
path prompt with a scrolling below-input autocomplete menu, arrow-key selection,
and compact path status. Moving an agent autocompletes known group names after a
matching prefix. Prompt inputs support Option/Alt word movement and deletion
when the terminal sends Option as Meta/Esc. Alt-modified keys are also preserved
when forwarded into Codex agent panes. `r` sets an optional card title and blank
input clears it back to the display path.

When the dashboard is open, press `?` for shortcuts. Defaults:

```toml
title_template = "{status} {auto}"
title_hook_command = ""
title_hook_timeout_seconds = 10

[key_bindings]
drawer = "C-b"
focus_left = "Left"
focus_right = "Right"
select_prev = "k"
select_next = "j"
open = "Enter"
new_workspace = "w"
new_group = "g"
new_agent = "n"
move_agent = "m"
rename = "r"
delete = "d"
help = "?"
quit = "C-c"
```

In CODEX focus, Weft keeps the `Agent Console` framed while forwarding Codex
input through the active PTY. The attached client enables enhanced terminal
keyboard reporting so modified Codex shortcuts such as `Shift+Enter` and
`Shift+Tab` are forwarded to Codex. It leaves terminal mouse tracking off, so
normal drag selection still works over chat output. Press the drawer key, `C-b`
by default, to return to the dashboard. While `Agent Console` is focused, `C-c`
belongs to Codex and is not a Weft quit shortcut: active work is sent through
Codex's interrupt path, and an idle Codex may still exit naturally. If Codex
does exit after receiving `C-c`, Weft returns to the dashboard `Agents` pane and
marks the agent as killed.

## Config And State

Weft stores runtime files globally:

- `~/.weft/config.toml`
- `~/.weft/state.json`
- `~/.weft/weft.sock`
- `~/.weft/weftd.pid`
- `~/.weft/weftd.log`
- `~/.weft/backups/`

`WEFT_ROOT` is the short development/worktree override. It sets the launch
workspace to that path and stores runtime files in `$WEFT_ROOT/.weft`.
When source-built Weft runs from a Weft checkout or worktree without
`WEFT_ROOT` or `WEFT_HOME`, it uses the current directory the same way, so
`go -C /path/to/weft-or-worktree run ./cmd/weft` stores runtime files in that
worktree's `.weft`.
`WEFT_WORKSPACE` overrides only the launch directory used for attach-time
workspace selection and the first-run add prompt.
`WEFT_HOME` overrides only the runtime directory directly for development and
tests.
Source-built Weft defaults to a fail-closed mode: it refuses to use the default
`~/.weft` runtime unless it can infer a checkout-local runtime, `WEFT_ROOT` or
`WEFT_HOME` is set, or `WEFT_ALLOW_MAIN_RUNTIME=1` is set for an intentional
one-off. Release builds from Homebrew use `~/.weft` by default.

`weft backup create [--output <dir>] [--reason <text>]` writes a restorable
copy of `config.toml`, `state.json`, metadata, and logs when present. `weft
backup restore <id-or-path> [--yes]` creates a pre-restore backup before
replacing config and state, and stops the supervisor first when it is running.

The config keys are stable: `codex_command`, `title_template`,
`title_hook_command`, `title_hook_timeout_seconds`, and `key_bindings`.
Unknown config keys are rejected so stale local settings are visible. State is
strict v4 with workspace/group names: `workspaces`, `groups`,
`selected_workspace_id`, `selected_group_id`, `workspace_id`, and `group_id`.
Older or unknown state shapes fail with guidance to run `weft clear` when a
reset is acceptable.

## Development

```sh
go test ./...
WEFT_RUN_INTEGRATION=1 go test ./...
go build ./cmd/weft
```

Live integration tests use temporary `WEFT_ROOT`, or separate temporary
`WEFT_HOME` and `WEFT_WORKSPACE` values when they need distinct paths, plus a
fake `codex_command`. Use
`WEFT_RUN_INTEGRATION=1 go test ./tests/integration -run TestAttachedDashboardKeyboardAndRenderingE2E -v`
to see per-step dashboard timing logs.

The repository ignores `.weft/` so each worktree can keep an isolated local
runtime for manual testing. Use `scripts/create-worktree.sh <slug>` to create
or repair a detached worktree with the local `.env` and config links needed for
manual runtime checks.
