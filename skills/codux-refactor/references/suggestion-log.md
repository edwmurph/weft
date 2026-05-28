# Codux Refactor Suggestion Log

Use this log to preserve concrete refactor suggestions, decisions, and evidence across sessions. Keep entries short, append new sessions at the top, and update an entry's outcome when the user accepts, rejects, or defers a suggestion.

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
