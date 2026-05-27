# Agent Instructions (codux repo)

## Workflow

- For any requested change: implement it, run the full test/lint workflow, confirm it passes, then pause for user review.
- When the user says **"ship it"**: squash-merge the reviewed change into `main` and push to `origin/main`.

## Git / Worktrees

- Default to doing work on a detached worktree under `./.worktrees/<slug>` (create it if needed).
- Keep changes focused; avoid drive-by refactors.
- After tests pass, stop and wait (no commit/push) until the user explicitly says "ship it".

## Development Commands

- Install/sync: `uv sync`
- Format: `uv run ruff format`
- Lint: `uv run ruff check`
- Tests: `uv run pytest`

