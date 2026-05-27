# Agent Instructions (codux repo)

## Workflow

- For any requested change: implement it, run the full test/lint workflow, confirm it passes, then pause for user review.
- If an implementation change causes drift with docs or agent instructions, update the docs/instructions in the same change to keep them accurate.
- For broad refactor requests, use the repo-local `$codux-refactor` skill in `skills/codux-refactor/` and update its suggestion log.
- When the user says **"ship it"**: squash-merge the reviewed change into `main` and push to `origin/main`.

## Git / Worktrees

- Default to doing work on a detached worktree under `./.worktrees/<slug>` (create it if needed).
- After implementing in a worktree, include a copy-paste command with the absolute worktree path for the user to run or inspect the change. For Codux runtime/UI changes, include the direct runnable command first, e.g. `uv --directory /abs/path/to/repo/.worktrees/<slug> --project /abs/path/to/repo/.worktrees/<slug> run start`; a `git diff` command alone is not enough. Do not use root-relative `--project .worktrees/<slug>` together with `--directory .worktrees/<slug>` because uv resolves `--project` after applying `--directory`.
- Keep changes focused; avoid drive-by refactors.
- After tests pass, stop and wait (no commit/push) until the user explicitly says "ship it".

## Development Commands

- Install/sync: `uv sync`
- Format: `uv run ruff format`
- Lint: `uv run ruff check`
- Tests: `uv run pytest`

## Dashboard Runtime Commands

- Dashboard/internal Codux commands must not shell through `cd`; use `uv --directory /abs/path/to/repo-or-worktree --project /abs/path/to/repo-or-worktree run codux ...` so the repo root and uv project path are explicit and valid from any current directory.
