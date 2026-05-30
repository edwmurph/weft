# Agent Instructions (codux repo)

## Workflow

- For any requested implementation change: implement it, run the full verification workflow, confirm it passes, then pause for user review.
- For docs-only or agent-instruction-only changes with no runtime, generated-artifact, packaging, command-output, or user-facing behavior risk, use a streamlined verification path that fits the change, such as `git diff --check`, rendered-doc inspection, or no test run. Run targeted or full verification if the docs derive from code, generated help text, release packaging, or anything risky.
- After implementing a change and completing verification, re-explain what changed and what changed functionality the user can verify before pausing for review.
- For implementation plans, explicitly include creating or using a detached worktree under `./.worktrees/<slug>` unless the user says otherwise.
- If a requested change appears to contradict `spec.md`, pause before implementation. Propose the change, identify the specific spec item it deviates from, and confirm that the user wants both the product behavior to deviate and `spec.md` updated to match.
- Treat `spec.md` as the living product contract. When accepted product behavior, UX, command semantics, state shape, or workflow expectations evolve, update `spec.md` in the same change.
- If an implementation change causes drift with docs or agent instructions, update the docs/instructions in the same change to keep them accurate.
- For broad refactor requests, use the repo-local `$codux-refactor` skill in `skills/codux-refactor/` and update its suggestion log.
- After verified implementation work, summarize what changed, what verification passed, exactly how the user can test it locally, include the exact command(s) to retest the changed behavior, and offer to ship it. Then stop for review unless the user explicitly says **"ship it"**.
- When the user says **"ship it"**, interpret it as: squash-merge the already-reviewed change to `main`, push `main` to `origin/main`, watch the `Publish Homebrew` workflow, and report the commit, release, tap publish status, verification status, and a brief re-explanation of the shipped user-visible change in the same message.

## Ship It Flow

- Use the fast path. Do one preflight on the main checkout: confirm branch is `main`, check `git status --short`, and compare `main...origin/main`.
- If the reviewed change lives in a detached worktree, copy that exact diff onto the main checkout, preferably with `git diff --binary ... > /tmp/<slug>.patch` and `git apply --check` before `git apply`.
- If the user interrupts or changes scope after a diff has been copied or committed on `main` but before it has been pushed, stop the ship flow. Move the full pending diff into a detached worktree under `./.worktrees/<slug>`, stage it there if the user asked for staged work, and restore the main checkout to `origin/main` before continuing.
- Do not re-run broad exploratory diffs or repeated status checks unless a command fails or the repo state changes unexpectedly.
- Do not re-run the full verification workflow during ship if the exact final diff was already verified after the user's last requested change. Re-run full verification only when verification is stale, missing, failed, or the landing step changes content.
- Commit only the relevant files as one squash commit on `main`, then push to `origin/main` over SSH. Use a truthful commit message because the release workflow infers `major`, `minor`, or `patch` from the shipped commit; add a `Semver-Bump: major|minor|patch` trailer only when the automatic inference would be wrong.
- After pushing, watch the latest `Publish Homebrew` run for `main` with `gh run watch --exit-status`. Treat ship as complete only after the workflow succeeds, creates the GitHub release, and updates the Homebrew tap.
- After the publish workflow succeeds, fetch `origin main --tags`, fast-forward local `main` if the workflow added a release commit, verify `main...origin/main` is `0 0`, then report the commit hash, release tag, push result, workflow result, whether verification was reused or rerun, and what behavior was shipped.

## Git / Worktrees

- Default to doing work on a detached worktree under `./.worktrees/<slug>` (create it if needed).
- Keep all implementation follow-up work in a detached worktree until the user explicitly says `ship it`; do not continue editing `main` after a paused or interrupted ship attempt.
- After implementing in a worktree, include a copy-paste command with the absolute worktree path for the user to run or inspect the change. For Codux runtime/UI changes, include the direct runnable command first, e.g. `go -C /abs/path/to/repo/.worktrees/<slug> run ./cmd/codux`; then include the specific retest command or sequence for the changed behavior, e.g. `go -C /abs/path/to/repo/.worktrees/<slug> run ./cmd/codux rename "Codex {status}"`. A `git diff` command alone is not enough.
- Keep changes focused; avoid drive-by refactors.
- After tests pass, stop and wait (no commit/push) until the user explicitly says "ship it".

## Development Commands

- Format: `gofmt -w cmd internal tests`
- Tests: `go test ./...`
- Live tmux integration tests: `CODUX_RUN_INTEGRATION=1 go test ./...`
- Build: `go build ./cmd/codux`

## Verification Workflow

- Before asking the user whether to ship an implementation change, run `gofmt -w cmd internal tests`, `go test ./...`, `CODUX_RUN_INTEGRATION=1 go test ./...`, and `go build ./cmd/codux`.
- If live tmux integration tests cannot run because `tmux` or `go` is unavailable, call that out explicitly instead of treating skipped integration tests as full verification.
- Keep live integration coverage focused on primary operator flows that need real tmux/process/state behavior; prefer adding coverage to existing integration scenarios over creating one new live test per edge case.
- Use mocked unit tests for broad branches, formatting details, parser cases, and deterministic command construction unless the bug only reproduces across a real tmux boundary.
- The live integration suite may grow to roughly 2 minutes of wall time as part of normal verification. As it approaches that budget, regularly reassess whether new coverage should be added, consolidated into existing flows, or kept in faster unit tests.
- If integration coverage starts repeating the same setup/action path, consolidate assertions into a primary-flow scenario instead of accumulating many nearly identical live tmux tests.

## Dashboard Runtime Commands

- Prefer `go -C /abs/path/to/repo-or-worktree run ./cmd/codux` when giving the user a worktree launch command before the binary is installed.
- Use `go -C /abs/path/to/repo-or-worktree run ./cmd/codux ...` for Codux subcommands in a worktree.
- Installed-user examples can use `codux ...` directly.
