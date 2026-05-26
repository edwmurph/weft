# codux

`codux` is a small Python CLI that runs Codex sessions inside one tmux session. It keeps a lightweight Kanban-style nav region above each Codex region, with one tmux window per Codex tab.

## Install

```sh
uv sync
uv run codux doctor
```

## Usage

```sh
uv run codux start
uv run codux new "Fix parser"
uv run codux rename "Parser follow-up"
uv run codux status
```

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
| `C-a` | toggle focus between nav and Codex |
| `C-q` | detach dashboard and leave sessions running |

`C-a` is session-scoped to the `codux` tmux session and configurable because it is intercepted before it reaches Codex.

## Config And State

On first run, Codux creates:

- `~/.codux/config.toml`
- `~/.codux/state.json`

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
focus_toggle = "C-a"
quit = "C-q"
```

State writes are atomic and guarded by `~/.codux/state.lock` so rapid tmux keybindings do not corrupt the JSON file.

## tmux Notes

Codux uses one tmux session named `codux`, one tmux window per Codex tab, and one host pane per window. The host pane renders:

- top box: `NAV`
- lower box: `CODEX`

When no tabs exist, Codux keeps an empty dashboard window open with:

```text
No Codex sessions open
Press n to create one.
```

Codux draws the visible rounded `NAV` and `CODEX` frames in the host renderer instead of using native tmux borders or decorative panes. This avoids tmux separator gaps, lets the active frame color update instantly, keeps the nav height tied to the tallest configured column, and embeds the active region's shortcuts in that region's bottom border.

Codex runs inside a child PTY owned by the host renderer. The host strips inherited CI/no-color overrides, enables truecolor hints, answers Codex's startup terminal background probe, and forwards Codex terminal-title updates back into the dashboard tab title.

## Development

```sh
uv run ruff format
uv run ruff check
uv run pytest
```
