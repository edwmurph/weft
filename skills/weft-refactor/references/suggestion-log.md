# Weft Refactor Suggestion Log

Use this log to preserve concrete refactor suggestions, decisions, and evidence across sessions. Keep entries short, append new sessions at the top, and update an entry's outcome when the user accepts, rejects, or defers a suggestion.

## 2026-05-31 - MVP legacy prune

- Request: Implement a narrow legacy/dead-code prune while preserving supervisor upgrades, dashboard `U`, `upgrade_resume`, Codex session IDs, and Codex resume.
- Suggestions: Delete stale iTerm2 map-based plist editing helpers, remove unused wrapper/helper APIs that were only test-held, keep strict unsupported legacy-shape tests, and document the current-vs-legacy boundary in `spec.md` and `AGENTS.md`.
- Outcome: Implemented in `.worktrees/mvp-legacy-prune`; awaiting review.
- Evidence: `internal/app/doctor_keys.go`, `internal/config/config.go`, `internal/runtimebackup/backup.go`, `internal/state/state.go`, `internal/tui/client.go`, `internal/supervisor/supervisor.go`, `spec.md`, `AGENTS.md`.

## 2026-05-31 - Upgrade cutover prune

- Request: Delete stale upgrade compatibility code while preserving the guarded dashboard `U` upgrade/resume flow.
- Suggestions: Remove the client-local `upgrade_resume` fallback, drop the `restart_when_idle` queued restart command family and IPC field, keep supervisor-owned `upgrade_resume` plus Codex session resume, and document the hard-cut guidance for supervisors too old to support dashboard upgrade.
- Outcome: Implemented in `.worktrees/upgrade-cutover-prune`; awaiting review.
- Evidence: `internal/tui/client.go`, `internal/supervisor/supervisor.go`, `internal/ipc/ipc.go`, `internal/tui/model_test.go`, `spec.md`.

## 2026-05-31 - Codex console parity

- Request: Assess and fix why Codex Vim mode behaves differently inside Weft, while keeping `C-b` as the dashboard escape.
- Suggestions: Replace Codex-focus key reconstruction with raw byte forwarding except the configured drawer sequence, preserve terminal-generated C-c as Codex interrupt input while Codex reports active work, keep title-hook capture as best-effort metadata, and extend the framed terminal renderer to preserve DECSCUSR cursor shape modes.
- Outcome: Implemented in `.worktrees/codex-parity-console`; awaiting review.
- Evidence: `internal/tui/client_input.go`, `internal/tui/client.go`, `internal/tui/model.go`, `internal/tui/terminal_screen.go`, `tests/integration/dashboard_e2e_test.go`, `README.md`, `spec.md`.

## 2026-05-30 - Aggressive legacy prune

- Request: Reapply the breaking legacy cleanup in a fresh `.worktrees/aggressive-legacy-prune` worktree without reusing the dirty cleanup worktree.
- Suggestions: Remove redundant `start`, hidden `tui`, and `sessions` commands; make state/config parsing strict instead of migrating or ignoring stale shapes; rename PTY internals from tab to agent; replace `internal/sessions` with `internal/pathx`.
- Outcome: Implemented in `.worktrees/aggressive-legacy-prune`; awaiting review.
- Evidence: `internal/app`, `internal/state`, `internal/config`, `internal/ptyx`, `internal/tui`, `internal/pathx`, `README.md`, `spec.md`, `tests`.

## 2026-05-30 - Aggressive legacy cleanup

- Request: Remove old tmux/workdir/folder compatibility and normalize Weft around workspaces, groups, agents, and the supervisor.
- Suggestions: Make the cleanup intentionally breaking, move state JSON to v4 `workspaces`/`groups`/`workspace_id` names, archive legacy state instead of migrating it, drop legacy config/env/CLI/title-hook aliases, and update tests/docs to match the new contract.
- Outcome: Implemented in `.worktrees/aggressive-legacy-cleanup`; awaiting review.
- Evidence: `internal/state`, `internal/config`, `internal/app`, `internal/tui`, `internal/titlehook`, `internal/titles`, `tests/integration`, `README.md`, `spec.md`.

## 2026-05-30 - Safe LOC refactor

- Request: Safely reduce Weft LOC without changing CLI, TUI, supervisor, IPC, config, state, or spec behavior.
- Suggestions: Remove the unused direct/headless TUI socket path, keep only the supervisor-driven engine command helpers, drop the legacy TUI socket from `config.Runtime` while still clearing `weft-tui.sock`, share duplicated model/client TUI selection and prompt rendering helpers, and delete unused private layout helpers.
- Outcome: Implemented and verified in `.worktrees/safe-loc-refactor`; awaiting review.
- Evidence: `internal/tui/model.go`, `internal/tui/client.go`, `internal/tui/model_shared.go`, `internal/tui/engine.go`, `internal/config/config.go`, `internal/app/app.go`, `skills/weft-refactor/references/suggestion-log.md`.

## 2026-05-30 - Dashboard form UX consistency

- Request: Refactor dashboard prompts/forms so add workdir, rename, group, move, and delete confirmation flows share one consistent professional form UX.
- Suggestions: Promote the improved add-workdir prompt into shared prompt helpers, keep path autocomplete path-specific, add useful group-name autocomplete for move-agent, prevalidate form submissions so invalid input stays open, and use compact state-specific footer actions across text and confirmation prompts.
- Outcome: Implemented in `.worktrees/forms-ux-consistency`; awaiting review.
- Evidence: `internal/tui/path_prompt.go`, `internal/tui/model.go`, `internal/tui/client.go`, `internal/tui/model_test.go`, `tests/integration/dashboard_e2e_test.go`, `README.md`, `spec.md`.

## 2026-05-30 - Framed Codex keyboard passthrough

- Request: Diagnose why proxied Codex focus input differs from a normal Codex terminal, with Shift+Enter recognized by Weft but not inserted as a Codex newline.
- Suggestions: Keep Weft's framed Codex pane active, enable enhanced terminal keyboard reporting in the attached client, forward modified-key escape sequences to the supervisor-owned Codex PTY, and reserve only the drawer key to return to the dashboard.
- Outcome: Implemented in `.worktrees/codex-shift-enter`; awaiting review.
- Evidence: `internal/tui/terminal_keys.go`, `internal/tui/client.go`, `internal/tui/model.go`, `tests/integration/dashboard_e2e_test.go`.

## 2026-05-30 - Weft rebrand

- Request: Rebrand the repo to Weft, update Homebrew publishing, GitHub remote metadata, code references, docs, and logo assets.
- Suggestions: Treat the rename as a repo-wide product boundary change: update the Go module path, CLI command directories, runtime env vars and state paths, tmux metadata, release formula renderer, docs/spec/agent instructions, and the repo-local refactor skill in one verified change.
- Outcome: Implemented in `.worktrees/rebrand-weft`; GitHub repo and local origin remote were renamed to `edwmurph/weft`.
- Evidence: `go.mod`, `cmd/weft`, `cmd/weft-release`, `internal/config`, `internal/tmuxhost`, `internal/release`, `.github/workflows/publish-homebrew.yml`, `README.md`, `spec.md`, `AGENTS.md`, `assets/weft-logo.svg`.

## 2026-05-29 - Global dashboard layout

- Request: Implement `spec.md`, preserving the working Go/tmux/PTX building blocks while redoing and extending the layout.
- Suggestions: Keep the single tmux-hosted Bubble Tea pane, terminal screen renderer, IPC loop, startup loading, and TUI-owned Codex PTYs; replace the tab/column state and renderer with global workdirs, optional flat groups inside an Agents pane, top-level agents, dashboard navigation, and full Codex focus.
- Outcome: Implemented in the original spec layout worktree; awaiting review.
- Evidence: `internal/state`, `internal/tui`, `internal/titles`, `internal/config`, `internal/tmuxhost`, `tests/integration`, `README.md`.

## 2026-05-29 - Close-Weft shortcut rename

- Request: Make `C-c` interrupt Codex first and close Weft on the next press, remove `C-q` from the shortcut surface, and merge the close-Weft CLI into `weft close`.
- Suggestions: Rename the dashboard exit binding to `close_weft`, default it to `C-c`, forward the first `C-c` to a live Codex tab in CODEX focus, arm the next `C-c` to close Weft unless another key is pressed first, make `weft close` close Weft clients when no tab id is passed, and keep legacy command compatibility outside visible docs.
- Outcome: Implemented on `main` as an unpushed pending ship commit; awaiting review after the latest behavior change.
- Evidence: `internal/config/config.go`, `internal/tui/model.go`, `tests/integration/dashboard_e2e_test.go`, `README.md`.

## 2026-05-29 - Codex-focus shortcut simplification

- Request: Stop dashboard `s` from overriding Codex input, keep only the focus toggle and close-Weft shortcut active in CODEX focus, and remove the dashboard sessions pane.
- Suggestions: Treat the focus toggle and close-Weft shortcut as the only global CODEX-focus shortcuts, forward other active-tab input to the Codex PTY, remove the dashboard sessions modal/ticker/config key, and keep session management in the CLI.
- Outcome: Implemented in `.worktrees/codex-focus-shortcuts`; awaiting review.
- Evidence: `internal/tui/model.go`, `internal/config/config.go`, `tests/integration/dashboard_e2e_test.go`, `README.md`.

## 2026-05-28 - Refactor skill Go workflow alignment

- Request: Update the repo-local refactor skill after the single-pane Go rewrite made the old Python workflow stale.
- Suggestions: Replace `pyproject.toml`/Python guidance with `go.mod`/Go guidance and update verification to gofmt, unit tests, live tmux integration, and Go build.
- Outcome: Implemented in `.worktrees/single-pane-tui-dashboard`.
- Evidence: `skills/weft-refactor/SKILL.md`.

## 2026-05-28 - Single-pane Go TUI rewrite

- Request: Replace the Python/tmux pane-composition dashboard with a Go-first single-pane TUI.
- Suggestions: Make tmux only the durable host, move Codex sessions into TUI-owned PTYs, migrate state to version 2 without tmux pane ids, add IPC for external commands, and publish Homebrew from Go source instead of Python wheels.
- Outcome: Implemented in `.worktrees/single-pane-tui-dashboard`; follow-up restored the original framed NAV/CODEX visual model and fixed empty-dashboard nav key handling.
- Evidence: `cmd/weft`, `internal/tui`, `internal/ptyx`, `internal/ipc`, `internal/state`, `.github/workflows/publish-homebrew.yml`, `tests/integration/tmux_runtime_test.go`.

## 2026-05-28 - Homebrew dependency wheelhouse

- Request: Diagnose work-network failures downloading Homebrew Python dependency resources.
- Suggestions: Stop publishing one Homebrew resource per PyPI dependency; publish a single dependency wheelhouse on the Weft GitHub release and have the tap install all dependency wheels from that archive.
- Outcome: Implemented in `.worktrees/homebrew-wheelhouse`; awaiting ship to publish a new release.
- Evidence: `.github/workflows/publish-homebrew.yml`, `scripts/render_homebrew_formula.py`, `tests/test_release_scripts.py`.

## 2026-05-28 - Homebrew publishing workflow

- Request: Publish Weft through a custom Homebrew tap and make brew the only README getting-started install path.
- Suggestions: Add a `Publish Homebrew` workflow, infer semver bumps from shipped commit text with an explicit trailer override, render the tap formula from `uv.lock`, and update ship-it instructions so publish success is part of the landing gate.
- Outcome: Implemented in `.worktrees/homebrew-publish`.
- Evidence: `.github/workflows/publish-homebrew.yml`, `scripts/next_version.py`, `scripts/render_homebrew_formula.py`, `README.md`, `CONTRIBUTING.md`, `AGENTS.md`.

## 2026-05-27 - Native Codex pane refactor

- Request: Replace the Codex PTY proxy/host renderer with native tmux panes.
- Suggestions: Launch `codex_command` directly in the content pane, keep the Kanban nav as a top interactive pane, remove proxy/pyte code, keep the previous rounded frame boxes as lightweight tmux panes, and avoid forcing Codex theme or color hints so the CLI behaves like it does in a normal PTY.
- Outcome: Implemented in `.worktrees/native-codex-pane`.
- Evidence: `weft/tmux.py`, `README.md`, `tests/test_tmux.py`, `tests/test_render.py`.

## 2026-05-27 - Repo-local refactor skill

- Request: Create a weft refactor skill that minimizes code, simplifies implementation, updates docs/instructions, finds process inefficiencies, evaluates libraries, and learns over time.
- Suggestions: Store the skill at `skills/weft-refactor`, include a durable suggestion log, and point repo agents at the skill.
- Outcome: Implemented.
- Evidence: `skills/weft-refactor/SKILL.md`, `skills/weft-refactor/agents/openai.yaml`, `skills/weft-refactor/references/suggestion-log.md`.
