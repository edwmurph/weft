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

Live tmux integration tests are opt-in because they start real tmux servers:

```sh
CODUX_RUN_INTEGRATION=1 uv run pytest -m integration
```

Each integration test uses a temporary `CODUX_HOME`, temporary `CODUX_WORKDIR`,
unique tmux session name, isolated tmux socket, and fake `codex_command`, so
multiple worktrees can run them at the same time without sharing runtime state.
Before offering to ship an implementation change, run format, lint, regular
pytest, and the live tmux integration command above.

## Homebrew Publishing

Pushes to `main` run the `Publish Homebrew` workflow. The workflow infers a
semantic version bump from the shipped commit, updates `pyproject.toml` and
`uv.lock`, tags `vX.Y.Z`, creates a GitHub release tarball, and writes
`Formula/codux.rb` to `edwmurph/homebrew-tap`.

One-time publishing prerequisites:

- Create the public `edwmurph/homebrew-tap` repository.
- Add a `HOMEBREW_TAP_TOKEN` repository secret in `edwmurph/codux` with write
  access to that tap.

The tap install command is:

```sh
brew install edwmurph/tap/codux
```

## Agent Guidance

Codex-agent workflow and maintenance instructions live in `AGENTS.md`. They are
intentionally centralized there rather than duplicated in this guide.
