# Contributing

Codux is a small Python project built around `uv`, `tmux`, and the Codex CLI.
Keep changes focused and preserve the core model: Codux coordinates native tmux
panes instead of proxying Codex input or output.

## Local Setup

```sh
uv sync
uv run codux doctor
```

## Checks

Run the full local workflow before opening a PR:

```sh
uv run ruff format
uv run ruff check
uv run pytest
```

## Agent Guidance

Codex-agent workflow and maintenance instructions live in `AGENTS.md`. They are
intentionally centralized there rather than duplicated in this guide.
