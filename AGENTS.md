# Agent Instructions (weft repo)

## Workflow

- For any requested implementation change: implement it, run the full verification workflow, confirm it passes, then pause for user review.
- For docs-only or agent-instruction-only changes with no runtime, generated-artifact, packaging, command-output, or user-facing behavior risk, use a streamlined verification path that fits the change, such as `git diff --check`, rendered-doc inspection, or no test run. Run targeted or full verification if the docs derive from code, generated help text, release packaging, or anything risky.
- After implementing a change and completing verification, re-explain what changed and what changed functionality the user can verify before pausing for review.
- For implementation plans, explicitly include creating or using a detached worktree under `./.worktrees/<slug>` with `scripts/create-worktree.sh <slug>` unless the user says otherwise.
- If a requested change appears to contradict `spec.md`, pause before implementation. Propose the change, identify the specific spec item it deviates from, and confirm that the user wants both the product behavior to deviate and `spec.md` updated to match.
- Treat `spec.md` as the living product contract. When accepted product behavior, UX, command semantics, state shape, or workflow expectations evolve, update `spec.md` in the same change.
- If an implementation requires changing an integration test expectation and the intended behavior cannot be safely inferred from the user's original request, proceed with the implementation using best judgment, but explicitly highlight the integration expectation as a contract change during the work and in the final report.
- If an implementation change causes drift with docs or agent instructions, update the docs/instructions in the same change to keep them accurate.
- Keep Markdown prose unwrapped: do not insert hard line breaks just to satisfy a column width in `.md` files. Let editors and renderers wrap paragraphs and list items naturally; preserve explicit line breaks only when the markdown syntax or readable examples require them.
- Keep `README.md` public-facing, clear, concise, and suitable for a high-quality open source project. It should quickly explain what Weft is, how to install and start using it, and where to learn more without exposing maintainer-only workflow, stale implementation detail, or broad background that belongs in deeper docs.
- When product or docs changes alter workflows shown in the README demo, explicitly call out whether the demo video should be regenerated; use `$weft-demo-video` in `skills/weft-demo-video/` for recording or regeneration work.
- Treat current Weft behavior as the workspace/group/task model with supervisor-owned PTYs, dashboard `U` upgrade/resume, supervisor-owned `upgrade_resume`, and Codex session ID capture/resume. Treat tmux pane state, tab/column/workdir/folder naming, hidden old commands, old config keys, migration paths, and alias support as legacy unless `spec.md` explicitly says otherwise.
- Task type badges should render as plain bracketed text such as `[codex]` or `[shell]`, usually derived from the task type ID. Avoid emoji because terminal width and fallback-font behavior is inconsistent. Keep badges in a fixed-width column, keep the ready/idle task marker subtle (`·`), and reserve the animated Braille spinner for active/loading rows.
- For broad refactor requests, use the repo-local `$weft-refactor` skill in `skills/weft-refactor/` and update its suggestion log.
- After verified implementation work, summarize what changed, what verification passed, exactly how the user can test it locally, include the exact command(s) to retest the changed behavior, and offer to ship it. Then stop for review unless the user explicitly says **"ship it"**.
- When the user says **"ship it"**, interpret it as: squash-merge the already-reviewed change to `main`, push `main` to `origin/main`, watch the `Publish Homebrew` workflow, and report the commit, release, tap publish status, verification status, and a brief re-explanation of the shipped user-visible change in the same message.

## Ship It Flow

- Use the fast path. Do one preflight on the main checkout: confirm branch is `main`, check `git status --short`, and compare `main...origin/main`.
- If the reviewed change lives in a detached worktree, copy that exact diff onto the main checkout, preferably with `git diff --binary ... > /tmp/<slug>.patch` and `git apply --check` before `git apply`.
- If the user interrupts or changes scope after a diff has been copied or committed on `main` but before it has been pushed, stop the ship flow. Move the full pending diff into a detached worktree under `./.worktrees/<slug>`, stage it there if the user asked for staged work, and restore the main checkout to `origin/main` before continuing.
- Do not re-run broad exploratory diffs or repeated status checks unless a command fails or the repo state changes unexpectedly.
- Do not re-run the full verification workflow during ship if the exact final diff was already verified after the user's last requested change. Re-run full verification only when verification is stale, missing, failed, or the landing step changes content.
- Commit only the relevant files as one squash commit on `main`, then push to `origin/main` over SSH. Use a Conventional Commit-style subject because the release workflow infers the semantic version bump and GitHub release notes from the shipped commit. Allowed ship commit types are `feat: ...`, `fix: ...`, `docs: ...`, `refactor: ...`, and `chore: ...`, with optional scopes such as `fix(tui): ...`; add a `Semver-Bump: major|minor|patch` trailer only when the automatic inference would be wrong.
- Weft is on the pre-1.0 release line until the user explicitly declares the official 1.0 release. While `VERSION` is `0.y.z`, inferred or explicit major bumps publish `0.(y+1).0`; breaking changes during pre-1.0 are minor releases in the `0.x` line. Do not set the workflow's `allow_stable_major` input or otherwise cross to `1.0.0` unless the user explicitly asks for the official 1.0 release. When intentionally seeding `VERSION` at `0.0.0`, use a patch-bump ship commit so the first fresh release is `v0.0.1`.
- Write the ship commit subject as a concise user-facing release-note bullet. If the subject alone is not enough, add a commit-body `Release-Notes:` section with one or more Markdown bullets; those bullets replace the derived subject in the GitHub release notes.
- For any breaking change, use a Conventional Commit breaking marker such as `feat!:` or a `BREAKING CHANGE:` footer so the release workflow places it in the `Breaking Changes` section. Add a user-facing `BREAKING CHANGE:` footer that explains the impact and a `Migration:` footer that tells users exactly what to do; use `Migration: No manual action required.` only when that is true.
- After pushing, watch the latest `Publish Homebrew` run for `main` with `gh run watch --exit-status`. Treat ship as complete only after the workflow succeeds, creates the GitHub release, publishes the tap bottle through the direct owner workflow or the tap PR workflow, updates the Homebrew tap formula, and writes a matching `bottle do` block.
- After the publish workflow succeeds, fetch `origin main --tags`, fast-forward local `main` if the workflow added a release commit, verify `main...origin/main` is `0 0`, then report the commit hash, release tag, push result, workflow result, whether the tap used direct or PR bottle publishing, tap bottle publish status, whether verification was reused or rerun, and what behavior was shipped.
- In the final ship report, summarize what the user can verify now in the live published version, using installed-user commands such as `weft ...` when appropriate, not only worktree or source commands.
- In the final ship report, print the published GitHub release notes for the shipped tag so the user can review the exact public release text.

## Git / Worktrees

- Default to doing work on a detached worktree under `./.worktrees/<slug>` (create it if needed).
- Create or repair detached worktrees with `scripts/create-worktree.sh <slug> [ref]`. The script creates/reuses `.worktrees/<slug>`, symlinks `.env` to the repo-root ignored file, and symlinks `.weft/config.toml` to the first available local config source: `WEFT_WORKTREE_CONFIG`, repo-root `.weft/config.toml`, `WEFT_HOME/config.toml`, or `~/.weft/config.toml`. The repo-root `.env` stays untracked and stores local secrets such as `OPENAI_API_KEY`.
- When launching or retesting Weft from a worktree with `go -C /abs/path/to/repo/.worktrees/<slug> run ./cmd/weft ...`, source-checkout auto-rooting uses `/abs/path/to/repo/.worktrees/<slug>/.weft-runtime` for config, state, socket, logs, and backups. Edit that runtime's `config.toml` when testing config behavior. Use explicit `WEFT_HOME` only when testing an intentionally custom runtime path.
- When the user asks to clean up or cleanup worktrees, use the repo-local cleanup script instead of manual `git worktree` or filesystem cleanup. Run `scripts/cleanup-worktrees.sh --dry-run` to show the plan, then run `scripts/cleanup-worktrees.sh --yes` only when the user explicitly confirms execution or clearly asks to delete the disposable worktrees. The script assumes targeted auxiliary `.worktrees/` checkouts are no longer needed, stops their `.weft-runtime` and `.weft` supervisors, removes the worktrees, and prunes stale Git worktree metadata.
- When the user asks for broad local Weft cleanup after they are done with all disposable worktrees and runtimes, use `$weft-cleanup` in `.agents/skills/weft-cleanup/`. That skill wraps worktree cleanup and stale temp supervisor cleanup. Always dry-run first and preserve the installed `~/.weft` runtime unless the user explicitly asks to stop it.
- If creating a worktree manually, reproduce the same `.env` link before running Weft from it. Source runs from the worktree use `.weft-runtime/config.toml`; copy or edit that file when local config features such as `title_hook_command` are needed.
- Keep all implementation follow-up work in a detached worktree until the user explicitly says `ship it`; do not continue editing `main` after a paused or interrupted ship attempt.
- After implementing in a worktree, include a copy-paste command with the absolute worktree path for the user to run or inspect the change. For Weft runtime/UI changes, include the direct runnable isolated fresh-dashboard command first, e.g. `go -C /abs/path/to/repo/.worktrees/<slug> run ./cmd/weft --clear`; then include the specific retest command or sequence for the changed behavior with the same worktree path, e.g. `go -C /abs/path/to/repo/.worktrees/<slug> run ./cmd/weft rename "Codex {status}"`. A `git diff` command alone is not enough.
- Whenever including a `go -C /abs/path/to/repo/.worktrees/<slug> run ./cmd/weft ...` command for worktree testing, include the instance's isolated config path beside it, e.g. `isolated config: /abs/path/to/repo/.worktrees/<slug>/.weft-runtime/config.toml`, so the user knows exactly which runtime config to inspect or edit.
- Before sending a final response for verified implementation work in a worktree, check that the response includes a fenced `sh` block with the direct runnable isolated fresh-dashboard `go -C /abs/path/to/repo/.worktrees/<slug> run ./cmd/weft --clear` command, followed by concrete behavior-specific retest steps. Do not rely on verification commands alone as the retest instructions.
- Always include the exact runnable command the user can paste to retest the current worktree after any implementation change. Put the runnable command before verification-only commands so the user can try the behavior immediately.
- Keep changes focused; avoid drive-by refactors.
- After tests pass, stop and wait (no commit/push) until the user explicitly says "ship it".

## Development Commands

- Format: `gofmt -w cmd internal tests`
- Tests: `go test ./...`
- Live supervisor integration tests: `WEFT_RUN_INTEGRATION=1 go test ./...`
- Build: `go build ./cmd/weft`

## Verification Workflow

- Before asking the user whether to ship an implementation change, run `gofmt -w cmd internal tests`, `go test ./...`, `WEFT_RUN_INTEGRATION=1 go test ./...`, and `go build ./cmd/weft`.
- If live integration tests cannot run because `go` is unavailable, call that out explicitly instead of treating skipped integration tests as full verification.
- Cover all dashboard-supported, user-facing functionality with live integration tests at the journey level. Add or extend integration scenarios whenever behavior changes workspace, group, task, focus/navigation, prompt, attach/detach, supervisor, or PTY interactions.
- Keep small live performance smoke assertions for user-visible dashboard latency, using generous budgets for launch, prompt, task startup, refresh, and reattach paths.
- Use unit tests for minute variations and pure logic details: validation branches, rendering/layout breakpoints, parser cases, prompt editing keystroke variants, and deterministic command construction.
- The live integration suite may grow to roughly 2 minutes of wall time as part of normal verification. As it approaches that budget, regularly reassess whether new coverage should be added, consolidated into existing flows, or kept in faster unit tests.
- If integration coverage starts repeating the same setup/action path, consolidate assertions into a primary-flow scenario instead of accumulating many nearly identical live supervisor tests.

## Dashboard Runtime Commands

- Prefer `go -C /abs/path/to/repo-or-worktree run ./cmd/weft --clear` when giving the user a worktree launch command before the binary is installed, unless preserving a specific runtime state is required for the test.
- Use `go -C /abs/path/to/repo-or-worktree run ./cmd/weft ...` for Weft subcommands in a worktree; source-checkout auto-rooting isolates the runtime in `/abs/path/to/repo-or-worktree/.weft-runtime`.
- For the current supervisor architecture worktree, the direct fresh-dashboard launch command is `go -C /Users/emurphy/code/personal/weft/.worktrees/ideal-architecture run ./cmd/weft --clear`.
- Installed-user and root/main dogfooding examples should use the release command directly, such as `weft`, `weft status`, and `weft backup create --reason pre-upgrade`.
