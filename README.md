<h1 align="center"><img src="assets/weft-logo.svg" alt="Weft" width="360"></h1>

<p align="center">
  <strong>Coordinate multiple Codex sessions from one supervisor-backed TUI.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/go-1.23%2B-4b5563?style=flat-square" alt="Go 1.23+">
  <img src="https://img.shields.io/badge/license-MIT-4b5563?style=flat-square" alt="MIT license">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-4b5563?style=flat-square" alt="macOS and Linux">
</p>

Weft runs one local supervisor that owns embedded Codex PTYs, state, and title
hooks. Terminal UI clients attach to that supervisor to render the command
center, then detach without stopping running Codex processes. Workdirs,
optional flat groups, and agents are managed inside one global state file.

## Getting Started

Install Weft with Homebrew:

```sh
brew install edwmurph/tap/weft
```

Then run Weft from a project directory. On first interactive launch, Weft asks
whether to add that directory as a workdir:

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
weft refresh
weft status [--json]
weft new [title]
weft group add <name>
weft workdir add <path>
weft rename [id] <title>
weft close [id]
weft close --kill
weft sessions
weft clear
weft doctor keys
weft doctor keys --clear
weft config info
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

Agent rows render through the global `title_template`, which defaults to
`{status} {auto}`. New agents default their base title to `{codex}`, so they
inherit the live Codex title until renamed. Titles passed to `weft new` or
`weft rename` can still include template variables for compatibility:

- `{title}`: user-configured agent title
- `{auto}`: generated title from the first submitted message
- `{codex}`: live Codex terminal title
- `{status}`: live Codex status, falling back to agent lifecycle status
- `{workdir}`: agent workdir path
- `{group}`: flat group name, when the agent is in a group

For example, `weft rename "Codex {status}"` keeps a fixed title while showing
the current agent status.

To generate titles, configure a hook command:

```toml
title_hook_command = "/path/to/weft/hooks/auto-title-openai.sh"
title_hook_timeout_seconds = 10
```

When `title_hook_command` is configured, the first non-empty message submitted
to each new Codex agent runs the hook from that agent's workdir. Weft sends
JSON on stdin with `event`, `agent_id`, `workdir`, `group`, `status`, `title`,
`title_template`, `codex_title`, and `first_message`, then saves the first
non-empty stdout line as the generated title.

Set an agent title to `{auto}` in the rename pane, or run
`weft rename <id> "{auto}"`, to display that saved generated title. To make
every row show generated titles, set `title_template = "{auto}"`. Before the
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
outside the agent workdir.

The command center has `Workdirs` and `Agents` navigation panes. Agents can sit
directly in a workdir as top-level rows. Groups are optional collapsible
sections inside the `Agents` pane, and `Enter` on a group opens or collapses it.
Dashboard forms use bordered inputs, compact validation/status lines, and
state-specific key hints. In the `Workdirs` pane, `w` opens an add-workdir path
prompt with a scrolling below-input autocomplete menu, arrow-key selection, and
compact path status. Moving an agent autocompletes known group names after a
matching prefix. Prompt inputs support Option/Alt word movement and deletion
when the terminal sends Option as Meta/Esc. Alt-modified keys are also
preserved when forwarded into Codex agent panes. `r` sets an optional card title
and blank input clears it back to the display path.

When the command center is open, press `?` for shortcuts. Defaults:

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
new_workdir = "w"
new_group = "g"
new_agent = "n"
move_agent = "m"
rename = "r"
delete = "d"
help = "?"
quit = "C-c"
```

In CODEX focus, Weft keeps the Codex pane framed while forwarding Codex input
through the active PTY. The attached client enables enhanced terminal keyboard
reporting so multiline shortcuts such as `Shift+Enter` are forwarded to Codex.
Press the drawer key, `C-b` by default, to return to the command center. `C-c`
stays with Codex while Codex has focus. To close Weft, return to the command
center and press `C-c`, or run `weft close` from another shell.

## Config And State

Weft stores runtime files globally:

- `~/.weft/config.toml`
- `~/.weft/state.json`
- `~/.weft/weft.sock`
- `~/.weft/weftd.pid`
- `~/.weft/weftd.log`

`WEFT_WORKDIR` overrides the launch directory used for attach-time workdir
selection and the first-run add prompt.
`WEFT_HOME` overrides the runtime directory directly for development and tests.

The config keys are stable: `codex_command`, `title_template`,
`title_hook_command`, `title_hook_timeout_seconds`, and `key_bindings`. Legacy
configs with `tmux_session` still load, but the setting is ignored by the
supervisor architecture and is not generated for new installs. State is
versioned. Old tabs/columns state is migrated into workdirs, optional groups,
and agents. Old tmux-pane state is archived to `state.v1-tmux.json` because
native tmux panes cannot be adopted into supervisor-owned PTYs.

## Development

```sh
go test ./...
WEFT_RUN_INTEGRATION=1 go test ./...
go build ./cmd/weft
```

Live integration tests use temporary `WEFT_HOME`, `WEFT_WORKDIR`, and a fake
`codex_command`. Use
`WEFT_RUN_INTEGRATION=1 go test ./tests/integration -run TestAttachedDashboardKeyboardAndRenderingE2E -v`
to see per-step dashboard timing logs.
