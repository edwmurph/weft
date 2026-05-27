# codux

`codux` is a small Python CLI that runs Codex sessions inside one tmux session. It keeps a lightweight Kanban-style nav pane above a native Codex pane, with one tmux window per Codex tab.

## Install

```sh
uv sync
uv run codux doctor
```

## Usage

```sh
uv run start
uv run codux start
uv run codux new "Fix parser"
uv run codux rename "Parser follow-up"
uv run codux status
```

`uv run start` is the shortest local start command; it is equivalent to `uv run codux start`.

Default nav shortcuts, active when the nav region is focused:

| Key | Action |
| --- | --- |
| `n` | new Codex session |
| arrow keys | move through the visible nav grid |
| `Shift` + arrow keys | move active tab left / right across columns |
| `Enter` | focus the active Codex pane |
| `r` | rename active tab |
| `x` | close active tab |
| `?` | help popup |
| `C-d` | toggle focus between nav and Codex |
| `C-q` | detach dashboard and leave sessions running |

`C-d` is session-scoped to the `codux` tmux session and configurable because it is intercepted before it reaches Codex.

## Config And State

On first run, Codux creates:

- installed/global runtime: `~/.codux/config.toml` and `~/.codux/state.json`
- source checkout/worktree runtime: `~/.codux/worktrees/<checkout-id>/config.toml` and `state.json`

The default config:

```toml
tmux_session = "codux"
codex_command = "codex"
columns = ["inbox", "implement", "ship"]

[key_bindings]
new = "n"
prev = "Left"
next = "Right"
move_left = "S-Left"
move_right = "S-Right"
rename = "r"
close = "x"
help = "?"
focus_toggle = "C-d"
quit = "C-q"
```

When Codux runs from a git checkout or worktree, the generated `tmux_session` includes the checkout id so multiple worktrees do not share dashboard state or tmux sessions. Set `CODUX_HOME` to force a specific runtime directory.

State writes are atomic and guarded by `state.lock` so rapid tmux keybindings do not corrupt the JSON file.

## tmux Notes

Codux uses one tmux session, one tmux window per Codex tab, and two native content panes per tab window:

- top pane: `NAV`, an interactive Kanban tab navigator
- lower pane: `CODEX`, the Codex process launched directly from `codex_command`

When no tabs exist, Codux keeps an empty dashboard window open with:

```text
No Codex sessions open
Press n to create one.
```

Codux keeps the same rounded `NAV` and `CODEX` frame boxes around those panes. The frames are lightweight tmux panes, while the NAV and CODEX interiors remain real interactive panes. The nav frame height follows the tallest configured column, and the active frame's bottom edge shows that pane's shortcuts.

Codex runs as the tmux pane command. Codux does not proxy Codex IO, re-render Codex output, inject Codex hooks, or force a Codex theme. Terminal color and theme behavior stays with the real tmux PTY and the user's `codex_command`; Codux only clears stale `CODUX_*` color hints left by older versions and neutralizes inherited tmux pane/window color styles for Codux windows.

## Development

```sh
uv run ruff format
uv run ruff check
uv run pytest
```

Repo-specific Codex maintenance guidance lives in `AGENTS.md`. Broad refactor work should use the repo-local `$codux-refactor` skill in `skills/codux-refactor/`.
