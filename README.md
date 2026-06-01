<h1 align="center"><img src="assets/weft-logo.svg" alt="Weft" width="360"></h1>

<p align="center">
  <strong>A terminal dashboard for Codex and shell tasks, workspaces, and groups.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/go-1.23%2B-4b5563?style=flat-square" alt="Go 1.23+">
  <img src="https://img.shields.io/badge/license-MIT-4b5563?style=flat-square" alt="MIT license">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-4b5563?style=flat-square" alt="macOS and Linux">
</p>

Weft makes parallel terminal work visible. Tasks can be integrated Codex
sessions or configured shell commands: editing, testing, waiting for review, or
blocked while other tasks keep moving. Weft keeps them in one terminal
dashboard so you can organize tasks by workspace and group, check status
quickly, and jump back into the right console without losing running PTYs.

## Why Weft

- See every Codex or shell task in one dashboard instead of hunting through
  terminal tabs.
- Organize tasks into optional groups inside each workspace, such as
  `release`, `review`, `bugs`, or `experiments`.
- Track workspace totals for `active` and `needs attention` tasks so finished
  or blocked work does not sit idle.
- Auto-name Codex tasks from their first chat message, or opted-in shell tasks
  from their first command, with a configurable title hook.
- Detach, upgrade, or reopen the UI while the local supervisor keeps task PTYs
  alive.
- Keep terminals framed: focus one task for direct input, then
  return to the dashboard to switch or reorganize.
- Use the mouse inside the framed `Task Console`: scroll history and drag-copy
  visually bounded text without copying terminal gutter padding.

## Getting Started

Install Weft with Homebrew:

```sh
brew install edwmurph/tap/weft
```

After `brew upgrade weft`, reopen the dashboard with `weft`. If only the client
needed to reopen, Weft will be current. If the older supervisor is still
running, the dashboard shows an upgrade banner. Weft waits until open Codex
tasks are idle and have saved Codex session IDs, then shows `U` as the upgrade
action. Confirming that modal closes the idle Codex terminals, restarts the
supervisor, and runs `codex resume <session-id>` for each task. Running shell
commands and unsubmitted terminal input are not preserved, so finish important
work first; the safer habit is still to upgrade after tasks are idle or closed.

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
weft status [--json]         Show supervisor, workspace, group, and task state.
weft version                 Show CLI, supervisor, and dashboard versions.
weft doctor                  Check local runtime and task command health.
weft doctor keys             Diagnose terminal key encoding.

Tasks and organization:
weft new [--type id] [title] Create a task.
weft select <id>             Make a task active.
weft rename [id] <title>     Rename the selected task or the given task.
weft close [id]              Close the active client or a task.
weft group add <name>        Add a group in the current workspace.
weft workspace add <path>    Add a workspace to the dashboard.
weft move-left               Move the selected task out of its group.
weft move-right              Move the selected task into the selected group.

Runtime and configuration:
weft close --kill [--yes]    Stop the supervisor and all task PTYs.
weft clear                   Prompt, then delete Weft runtime state.
weft backup create           Back up config, state, and logs.
weft backup list             List saved runtime backups.
weft backup restore <id>     Restore config and state from a backup.
weft config info             Show runtime paths and active config.
weft config show             Print config.toml.
weft config init [--force]   Write the default config.
```

Run `weft close` without an id to detach the active Weft client while the
supervisor and task PTYs keep running. Pass an id to close a task. Use `weft
close --kill` to stop the supervisor and all task PTYs after confirmation.

Use `weft doctor keys` when Option/Alt shortcuts do not behave like the rest of
your terminal. It compares Backspace, Option+Backspace, and Ctrl+Backspace and
prints the terminal setting needed when Option is being sent as plain
Backspace. In iTerm2, it can offer to add the needed Option+Backspace key
fix to the current/default profile after writing a plist backup: Left/Right
Option Key are set to Esc+, and a fallback Option+Backspace mapping is added.
If the profile is already configured but the current tab still sends plain
Backspace, it reports that the running iTerm2 session has not picked up the
preference yet. For custom iTerm2 settings folders, it tells the user to quit
and reopen iTerm2 because new tabs may keep using the in-memory profile.

## Task Types And Titles

Task rows in the dashboard are customizable. Each task has a configured type
with a label, badge, kind, command, and default title template. `codex` is the
only checked-in integrated task kind today. Generic `terminal` task types can
start any shell command, and future integrated kinds such as Claude require a
checked-in implementation instead of config alone.

The default config includes `codex` and `shell` task types:

```toml
default_task_type = "codex"

[task_types.codex]
label = "Codex"
kind = "codex"
command = "codex"
badge = "[codex]"
title_template = "{status} {auto}"

[task_types.shell]
label = "Shell"
kind = "terminal"
command = "exec \"$SHELL\" -l"
badge = "[shell]"
title_template = "Shell"
```

Task type badges should be plain bracketed text so rows stay readable and
aligned in terminal fonts. The built-in defaults are `[codex]` and `[shell]`.

Press `n` in the dashboard to choose a task type. `weft new` uses
`default_task_type` unless `--type <id>` is supplied.

Task titles can call a hook to generate an `{auto}` title. Codex tasks use the
first non-empty submitted chat message. Terminal task types opt in by using
`{auto}` in their `title_template`, and the hook input is the first typed shell
command. The hook is a plain shell command, so you can plug in your own naming
logic while keeping the dashboard focused on the saved title.

New tasks copy their type's `title_template` into their own title so the edit
pane opens with that editable template. Titles passed to `weft new`,
`weft new --type codex`, or `weft rename` can still include template variables:

- `{title}`: user-configured task title
- `{auto}`: generated title from the first submitted message
- `{codex}`: live Codex terminal title
- `{status}`: verbatim live Codex status, falling back to task lifecycle status
- `{workspace}`: task workspace path
- `{group}`: flat group name, when the task is in a group

For example, `weft rename "Codex {status}"` keeps a fixed title while showing
the current task status.

To generate `{auto}` titles from the first chat message, configure a hook
command:

```toml
title_hook_command = "/path/to/weft/hooks/auto-title-openai.sh"
title_hook_timeout_seconds = 10
```

When `title_hook_command` is configured, the first non-empty Codex message or
opted-in terminal command runs the hook from that task's workspace. Weft sends
JSON on stdin with `event`, `task_id`, `workspace`, `group`, `status`,
`title`, `type_id`, the task `title_template`, `codex_title` when available,
and `first_message`, then saves the first non-empty stdout line as the
generated title.

Set a task title to `{auto}` in the edit pane, or run `weft rename <id>
"{auto}"`, to display that saved generated title. To make new shell tasks start
with generated titles, set the shell task type `title_template = "{auto}"`.
Before the first message or command, `{auto}` renders as `waiting for first
message`; failed hooks render as `auto title failed`, show a footer error, and
keep the full error in the edit pane.

Weft treats hooks as generic shell commands. The checked-in
`hooks/auto-title-openai.sh` script is one real hook implementation; it reads
`OPENAI_API_KEY` from the environment or a local ignored `.env` file and uses
the OpenAI Responses API with `OPENAI_TITLE_MODEL`, defaulting to
`gpt-5.4-nano`, to return a short task title. The prompt only uses the first
message, so simple greetings like `hi` stay simple. You can replace it with any
command that follows the same stdin/stdout contract. Set
`WEFT_OPENAI_ENV_FILE=/path/to/.env` in the hook command when the API key lives
outside the task workspace.

The dashboard has `Workspaces`, `Tasks`, and `Task Live Preview` panes. Workspaces
and tasks live in the left-side navigation panes; the preview stays ready on
the right as a read-only live lens into the selected task, with cropped lines
marked at the right edge. Press `Enter` to open the selected task in the
focused `Task Console`, where terminal input is forwarded. Tasks can sit
directly in a workspace as top-level rows, or they can be organized into
optional collapsible groups inside the `Tasks` pane. Use groups for whatever
makes the work easier to scan: release tasks, experiments, review follow-ups,
bug fixes, or blocked investigations. `Enter` on a group opens or collapses it.
`Shift+Up`/`Shift+Down` reorders the selected workspace, task, or group.
Workspace cards move in the Workspaces pane. Task rows move within their
current group or top-level area, crossing into the adjacent group at
boundaries; group rows move as whole sections.

Shell rows use the same spinner as Codex rows while a foreground command is in
progress, then return to the ready marker when the shell regains control.

Dashboard forms use bordered inputs, compact validation/status lines, and
state-specific key hints. In the `Workspaces` pane, `w` opens an add-workspace
path prompt with a scrolling below-input autocomplete menu, arrow-key selection,
and compact path status. Moving a task autocompletes known group names after a
matching prefix. Prompt inputs support Option/Alt word movement and deletion
when the terminal sends Option as Meta/Esc. Alt-modified keys are also preserved
when forwarded into task consoles. `e` sets an optional card title and blank
input clears it back to the display path.

When the dashboard is open, press `?` for shortcuts. Defaults:

```toml
default_task_type = "codex"
title_hook_command = ""
title_hook_timeout_seconds = 10

[task_types.codex]
label = "Codex"
kind = "codex"
command = "codex"
badge = "[codex]"
title_template = "{status} {auto}"

[task_types.shell]
label = "Shell"
kind = "terminal"
command = "exec \"$SHELL\" -l"
badge = "[shell]"
title_template = "Shell"

[key_bindings]
drawer = "C-b"
focus_left = "Left"
focus_right = "Right"
select_prev = "k"
select_next = "j"
open = "Enter"
new_workspace = "w"
new_group = "g"
new_task = "n"
move_task = "m"
edit = "e"
delete = "Backspace"
help = "?"
quit = "C-c"
```

Configs with `delete = "d"` are rejected so typing `d` cannot also delete the
selected dashboard item. Remove that override or replace it with
`delete = "Backspace"`.

In task focus, Weft keeps the `Task Console` framed while forwarding terminal
input through the active PTY. The configured drawer key, `C-b` by default, is
the only dashboard key sequence Weft owns while a task is focused. For Codex
tasks, terminal-generated `C-c` is still delivered through Codex's interrupt
path while Codex reports active work, matching side-thread interruption
behavior instead of returning or closing the side thread. Other terminal task
behavior, including ordinary typing, readline controls such as `C-u`, Vim mode,
Esc timing, bracketed paste, Alt/Meta prefixes, and terminal clear via
Command-K, reaches the task like it would outside Weft. The
framed renderer also preserves cursor visibility and block/bar/underline cursor
shape requests. The client also captures mouse input in focused `Task Console`:
trackpad or wheel scrolling moves through Weft's captured scrollback, and drag
selection starts after the shared visual margin so the highlighted cells match
the clipboard text while preserving existing colors under the selection
overlay. A short toast in the console border confirms the copy. Press the
drawer key to return to the dashboard. If a task exits after receiving its own
input, Weft returns to the dashboard `Tasks` pane and marks the task as killed.

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

The config keys are strict: `default_task_type`, `task_types`,
`title_hook_command`, `title_hook_timeout_seconds`, and `key_bindings`.
Unknown config keys are rejected generically so stale local settings are
visible. State is strict v5 with `tasks`, `active_task_id`,
`selected_task_id`, task `type_id`, and focus values of `workspaces`, `tasks`,
or `console`. Unsupported old config or state files fail; state errors include
guidance to run `weft clear` when a reset is acceptable.

## Development

```sh
go test ./...
WEFT_RUN_INTEGRATION=1 go test ./...
go build ./cmd/weft
```

Live integration tests use temporary `WEFT_ROOT`, or separate temporary
`WEFT_HOME` and `WEFT_WORKSPACE` values when they need distinct paths, plus a
fake Codex task command. Use
`WEFT_RUN_INTEGRATION=1 go test ./tests/integration -run TestAttachedDashboardKeyboardAndRenderingE2E -v`
to see per-step dashboard timing logs.

The repository ignores `.weft/` so each worktree can keep an isolated local
runtime for manual testing. Use `scripts/create-worktree.sh <slug>` to create
or repair a detached worktree with the local `.env` and config links needed for
manual runtime checks.

When all auxiliary worktrees are disposable, run
`scripts/cleanup-worktrees.sh`. It targets only Git-registered worktrees under
this repo's `.worktrees/`, stops each worktree's `WEFT_ROOT` supervisor when one
is present, removes the worktree, and prunes stale Git worktree metadata. Pass
`--dry-run` to preview the plan or `--yes` to skip the confirmation prompt.
