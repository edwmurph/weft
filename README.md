# codux

`codux` is a small Python CLI that manages multiple Codex agents across parallel workflows in tmux. It keeps a lightweight Kanban-style nav pane above a native Codex pane, with one tmux window per Codex tab.

## Getting Started

```sh
uv sync
uv run codux doctor
```

Optional shell completion:

```sh
uv run codux --install-completion
```

Run the completion command from the shell you want to configure.

## Usage

```sh
uv --quiet --no-progress run start
uv run codux --help
uv run codux start
uv run codux doctor
uv run codux config info
uv run codux config path
uv run codux config show
uv run codux config init
uv run codux sessions
uv run codux delete-session SESSION
uv run codux clear
uv run codux quit
uv run codux quit --kill
```

`uv --quiet --no-progress run start` starts the dashboard without first-run `uv` setup chatter in fresh worktrees. It is equivalent to `uv run codux start`. User-facing shell commands are:

- `codux start`: create or attach to the dashboard for the current workdir
- `codux config info`: show the active workdir, runtime directory, config, state, and tmux session
- `codux config path`: print the current workdir's config path
- `codux config show`: create the config if needed, then print it
- `codux config init`: create the default config without starting the dashboard
- `codux doctor`: check local dependencies and runtime files
- `codux sessions`: list active Codux dashboard sessions
- `codux delete-session SESSION`: delete a tmux session without confirmation
- `codux clear`: delete all Codux tmux sessions and saved workspaces after confirmation
- `codux quit`: detach the dashboard and leave Codex tabs running
- `codux quit --kill`: stop the current dashboard tmux session

Create, rename, close, focus, move Codex tabs, and manage other dashboard sessions from inside Codux with the nav shortcuts below.

New tabs store their title as `{codex}` by default. In the nav pane, Codux replaces
that placeholder with the live terminal title from the Codex tmux pane, so Codex
`/title` updates appear without Codux proxying Codex IO. Until a live title is
available, the placeholder segment shows `...`. Manual titles can also include
the placeholder, for example by pressing `r` and entering `Task {codex}`; titles
without `{codex}` render exactly as entered.

Default nav shortcuts, active when the nav region is focused:

| Key | Action |
| --- | --- |
| `n` | new Codex tab |
| `r` | rename active tab |
| `c` | close active tab |
| `←`/`→`/`↑`/`↓` | switch tabs |
| `shift + ←`/`→` | move active tab left / right across columns |
| `s` | view and close other dashboard sessions |
| `Enter` | focus the active Codex pane |
| `?` | help popup |
| `C-d` | focus the other pane |
| `C-q` | detach dashboard and leave Codex tabs running |

The active pane footer shows its available shortcuts on the left. Inactive pane footers show `C-d focus` on the left.

`C-d` is scoped to the current Codux tmux session and configurable because it is intercepted before it reaches Codex.

## Config And State

Codux is scoped to the directory where you launch it. On first `codux start` from a directory, Codux creates:

- tmux workspace: one session named `codux-<workdir-id>`
- runtime directory: `~/.codux/workdirs/<workdir-id>/`
- config file: `~/.codux/workdirs/<workdir-id>/config.toml`
- state file: `~/.codux/workdirs/<workdir-id>/state.json`

Starting Codux again from the same directory reattaches to the same tmux workspace. Starting it from a different directory creates a different runtime directory, config, state file, and tmux session.

Use these commands to inspect the current launch directory's runtime:

```sh
codux config info
codux config path
codux config show
codux config init
codux config init --force
```

The default config:

```toml
# Codux runtime configuration for one launch directory.
# Run `codux config info` to see the workdir, runtime directory, state file, and
# generated tmux session. Set tmux_session only when you need to override it.

# Command launched directly inside each CODEX tmux pane.
codex_command = "codex"

# Ordered columns shown in the nav pane.
columns = ["inbox", "implement", "ship"]

[key_bindings]
new = "n"
prev = "Left"
next = "Right"
move_left = "S-Left"
move_right = "S-Right"
rename = "r"
close = "c"
sessions = "s"
help = "?"
focus_toggle = "C-d"
quit = "C-q"
```

Set `columns` to change the nav columns and their order. Existing tabs in removed columns are moved to the first configured column the next time Codux repairs runtime state.

The config file controls:

- `codex_command`: shell command launched directly in each CODEX pane
- `columns`: nav columns and their left-to-right order
- `[key_bindings]`: nav and pane-focus shortcuts

Codux computes the default tmux session name from the launch directory. Set `tmux_session` only when you need an explicit override.

`CODUX_WORKDIR` overrides the directory used for workdir scoping. `CODUX_HOME` overrides the runtime directory directly; use it only when you intentionally need isolated state for development or tests.

State writes are atomic and guarded by `state.lock` so rapid tmux keybindings do not corrupt the JSON file.

## tmux Notes

Each Codux dashboard uses one tmux session, one tmux window per Codex tab, and two native content panes per tab window:

- top pane: `NAV`, an interactive Kanban tab navigator
- lower pane: `CODEX`, the Codex process launched directly from `codex_command`

When no tabs exist, Codux keeps an empty dashboard window open with:

```text
No Codex tabs open
Press n to create one.
```

Codux keeps the same rounded `NAV` and `CODEX` frame boxes around those panes. The frames are lightweight tmux panes, while the NAV and CODEX interiors remain real interactive panes. The nav frame height follows the tallest configured column. The NAV top edge shows the workdir, the CODEX top edge shows the live Codex title, and the focused pane's bottom edge shows a `●` marker.

Codex runs as the tmux pane command. Codux does not proxy Codex IO, re-render Codex output, inject Codex hooks, or force a Codex theme. Terminal color and theme behavior stays with the real tmux PTY and the user's `codex_command`; Codux clears stale `CODUX_*` color hints left by older versions, keeps its runtime env out of Codex panes, and neutralizes inherited tmux pane/window color styles for Codux windows.

## Development

```sh
uv run ruff format
uv run ruff check
uv run pytest
```

Repo-specific Codex maintenance guidance lives in `AGENTS.md`. Broad refactor work should use the repo-local `$codux-refactor` skill in `skills/codux-refactor/`.
