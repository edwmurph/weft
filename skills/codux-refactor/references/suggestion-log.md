# Codux Refactor Suggestion Log

Use this log to preserve concrete refactor suggestions, decisions, and evidence across sessions. Keep entries short, append new sessions at the top, and update an entry's outcome when the user accepts, rejects, or defers a suggestion.

## 2026-05-29 - Codex-focus shortcut simplification

- Request: Stop dashboard `s` from overriding Codex input, keep only `C-g` and `C-q` active in CODEX focus, and remove the dashboard sessions pane.
- Suggestions: Treat `C-g` and `C-q` as the only global CODEX-focus shortcuts, forward all other active-tab input to the Codex PTY, remove the dashboard sessions modal/ticker/config key, and keep session management in the CLI.
- Outcome: Implemented in `.worktrees/codex-focus-shortcuts`; awaiting review.
- Evidence: `internal/tui/model.go`, `internal/config/config.go`, `tests/integration/dashboard_e2e_test.go`, `README.md`.

## 2026-05-28 - Refactor skill Go workflow alignment

- Request: Update the repo-local refactor skill after the single-pane Go rewrite made the old Python workflow stale.
- Suggestions: Replace `pyproject.toml`/Python guidance with `go.mod`/Go guidance and update verification to gofmt, unit tests, live tmux integration, and Go build.
- Outcome: Implemented in `.worktrees/single-pane-tui-dashboard`.
- Evidence: `skills/codux-refactor/SKILL.md`.

## 2026-05-28 - Single-pane Go TUI rewrite

- Request: Replace the Python/tmux pane-composition dashboard with a Go-first single-pane TUI.
- Suggestions: Make tmux only the durable host, move Codex sessions into TUI-owned PTYs, migrate state to version 2 without tmux pane ids, add IPC for external commands, and publish Homebrew from Go source instead of Python wheels.
- Outcome: Implemented in `.worktrees/single-pane-tui-dashboard`; follow-up restored the original framed NAV/CODEX visual model and fixed empty-dashboard nav key handling.
- Evidence: `cmd/codux`, `internal/tui`, `internal/ptyx`, `internal/ipc`, `internal/state`, `.github/workflows/publish-homebrew.yml`, `tests/integration/tmux_runtime_test.go`.

## 2026-05-28 - Homebrew dependency wheelhouse

- Request: Diagnose work-network failures downloading Homebrew Python dependency resources.
- Suggestions: Stop publishing one Homebrew resource per PyPI dependency; publish a single dependency wheelhouse on the Codux GitHub release and have the tap install all dependency wheels from that archive.
- Outcome: Implemented in `.worktrees/homebrew-wheelhouse`; awaiting ship to publish a new release.
- Evidence: `.github/workflows/publish-homebrew.yml`, `scripts/render_homebrew_formula.py`, `tests/test_release_scripts.py`.

## 2026-05-28 - Homebrew publishing workflow

- Request: Publish Codux through a custom Homebrew tap and make brew the only README getting-started install path.
- Suggestions: Add a `Publish Homebrew` workflow, infer semver bumps from shipped commit text with an explicit trailer override, render the tap formula from `uv.lock`, and update ship-it instructions so publish success is part of the landing gate.
- Outcome: Implemented in `.worktrees/homebrew-publish`.
- Evidence: `.github/workflows/publish-homebrew.yml`, `scripts/next_version.py`, `scripts/render_homebrew_formula.py`, `README.md`, `CONTRIBUTING.md`, `AGENTS.md`.

## 2026-05-27 - Native Codex pane refactor

- Request: Replace the Codex PTY proxy/host renderer with native tmux panes.
- Suggestions: Launch `codex_command` directly in the content pane, keep the Kanban nav as a top interactive pane, remove proxy/pyte code, keep the previous rounded frame boxes as lightweight tmux panes, and avoid forcing Codex theme or color hints so the CLI behaves like it does in a normal PTY.
- Outcome: Implemented in `.worktrees/native-codex-pane`.
- Evidence: `codux/tmux.py`, `README.md`, `tests/test_tmux.py`, `tests/test_render.py`.

## 2026-05-27 - Repo-local refactor skill

- Request: Create a codux refactor skill that minimizes code, simplifies implementation, updates docs/instructions, finds process inefficiencies, evaluates libraries, and learns over time.
- Suggestions: Store the skill at `skills/codux-refactor`, include a durable suggestion log, and point repo agents at the skill.
- Outcome: Implemented.
- Evidence: `skills/codux-refactor/SKILL.md`, `skills/codux-refactor/agents/openai.yaml`, `skills/codux-refactor/references/suggestion-log.md`.
