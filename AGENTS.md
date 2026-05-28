# Agent Instructions (codux repo)

## Workflow

- For any requested change: implement it, run the full verification workflow, confirm it passes, then pause for user review.
- After implementing a change and completing verification, re-explain what changed and what changed functionality the user can verify before pausing for review.
- For implementation plans, explicitly include creating or using a detached worktree under `./.worktrees/<slug>` unless the user says otherwise.
- If an implementation change causes drift with docs or agent instructions, update the docs/instructions in the same change to keep them accurate.
- For broad refactor requests, use the repo-local `$codux-refactor` skill in `skills/codux-refactor/` and update its suggestion log.
- After verified implementation work, summarize what changed, what verification passed, exactly how the user can test it locally, and offer to ship it. Then stop for review unless the user explicitly says **"ship it"**.
- When the user says **"ship it"**, interpret it as: squash-merge the already-reviewed change to `main`, push `main` to `origin/main`, and report the commit plus verification status.

## Ship It Flow

- Use the fast path. Do one preflight on the main checkout: confirm branch is `main`, check `git status --short`, and compare `main...origin/main`.
- If the reviewed change lives in a detached worktree, copy that exact diff onto the main checkout, preferably with `git diff --binary ... > /tmp/<slug>.patch` and `git apply --check` before `git apply`.
- Do not re-run broad exploratory diffs or repeated status checks unless a command fails or the repo state changes unexpectedly.
- Do not re-run the full verification workflow during ship if the exact final diff was already verified after the user's last requested change. Re-run full verification only when verification is stale, missing, failed, or the landing step changes content.
- Commit only the relevant files as one squash commit on `main`, then push to `origin/main` over SSH.
- After pushing, verify `main...origin/main` is `0 0`, then report the commit hash, push result, and whether verification was reused or rerun.

## Git / Worktrees

- Default to doing work on a detached worktree under `./.worktrees/<slug>` (create it if needed).
- After implementing in a worktree, include a copy-paste command with the absolute worktree path for the user to run or inspect the change. For Codux runtime/UI changes, include the direct runnable command first, e.g. `uv --quiet --no-progress --directory /abs/path/to/repo/.worktrees/<slug> --project /abs/path/to/repo/.worktrees/<slug> run start`; a `git diff` command alone is not enough. Do not use root-relative `--project .worktrees/<slug>` together with `--directory .worktrees/<slug>` because uv resolves `--project` after applying `--directory`.
- Keep changes focused; avoid drive-by refactors.
- After tests pass, stop and wait (no commit/push) until the user explicitly says "ship it".

## Development Commands

- Install/sync: `uv sync`
- Format: `uv run ruff format`
- Lint: `uv run ruff check`
- Tests: `uv run pytest`
- Live tmux integration tests: `CODUX_RUN_INTEGRATION=1 uv run pytest -m integration`

## Verification Workflow

- Before asking the user whether to ship an implementation change, run `uv run ruff format`, `uv run ruff check`, `uv run pytest`, and `CODUX_RUN_INTEGRATION=1 uv run pytest -m integration`.
- If live tmux integration tests cannot run because `tmux` or `uv` is unavailable, call that out explicitly instead of treating skipped integration tests as full verification.
- Keep live integration coverage focused on primary operator flows that need real tmux/process/state behavior; prefer adding coverage to existing integration scenarios over creating one new live test per edge case.
- Use mocked unit tests for broad branches, formatting details, parser cases, and deterministic command construction unless the bug only reproduces across a real tmux boundary.
- The live integration suite may grow to roughly 2 minutes of wall time as part of normal verification. As it approaches that budget, regularly reassess whether new coverage should be added, consolidated into existing flows, or kept in faster unit tests.
- If integration coverage starts repeating the same setup/action path, consolidate assertions into a primary-flow scenario instead of accumulating many nearly identical live tmux tests.

## Dashboard Runtime Commands

- Dashboard/internal Codux commands must not shell through `cd`; keep the repo root and uv project path explicit with `uv --quiet --no-progress --directory /abs/path/to/repo-or-worktree --project /abs/path/to/repo-or-worktree ...` so commands are valid from any current directory and do not show first-run uv progress before the dashboard attaches.
- Prefer `uv --quiet --no-progress --directory /abs/path/to/repo-or-worktree --project /abs/path/to/repo-or-worktree run start` when giving the user a dashboard launch command.
- Use `uv --quiet --no-progress --directory /abs/path/to/repo-or-worktree --project /abs/path/to/repo-or-worktree run codux ...` for non-start Codux subcommands.
